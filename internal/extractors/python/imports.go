// imports.go — IMPORTS to_id resolution for the Python extractor.
//
// Analog of #642 for Python. The Python extractor previously emitted
// IMPORTS edges whose ToID was either the bare imported name
// ("requests") or the dotted form ("requests.exceptions.ConnectionError").
// Neither shape carries the `ext:<package>` prefix the resolver's
// external-disposition gate (refs.go: stubPrefixExternal) keys on, so
// every imported leaf from a known external package (django, flask,
// requests, numpy, pandas, ...) had to round-trip through the bare-
// name resolver, miss, and fall back to ExternalUnknown / bug-extractor
// — driving the 26-46% orphan rates on the Python real-world corpora.
//
// The fix mirrors #642: AFTER extractImports has emitted the IMPORTS
// edges, walk every edge and rewrite the ToID for edges whose
// source_module points at a known external Python package:
//
//	from django.db import models      → ToID = "ext:django:models"
//	import requests                   → ToID = "ext:requests"
//	from rest_framework import serializers
//	                                  → ToID = "ext:rest_framework:serializers"
//
// In-tree imports (source_module that matches a project file's dotted
// path) are NOT touched here — the resolver's
// ResolveDottedImportTarget path already binds them via the
// source_module + imported_name properties.
//
// We can't distinguish in-tree from external at extraction time without
// access to the full project file tree (the extractor runs per-file).
// The conservative bias is: ONLY rewrite when the top-level module of
// the dotted source_module matches a hard-coded list of well-known
// external Python packages. Anything else stays as-is and the resolver
// has the chance to bind it cross-file.
//
// The hard-coded list mirrors the prefix of internal/external/synth.go's
// knownExternalPackages but is reproduced here because the extractor
// must not depend on the external package (cyclic-import risk and a
// design rule: extractors are leaf packages).

package python

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// pythonKnownExternalRoots is the set of top-level Python package names
// that the resolver's external-disposition gate already classifies as
// ExternalKnown via the `ext:<pkg>` prefix. When the Python extractor
// sees an IMPORTS edge whose source_module's root segment is on this
// list, it rewrites the ToID to `ext:<root>:<imported_name>` (or just
// `ext:<root>` for plain `import pkg`) so the edge bypasses the bare-
// name resolver and lands on ExternalKnown directly.
//
// Keep in sync with internal/external/synth.go knownExternalPackages —
// this list need not be exhaustive (any miss stays as-is, which is the
// pre-fix shape), but every entry must also be present in the
// authoritative allowlist or the resolver will misclassify the edge as
// ExternalUnknown.
var pythonKnownExternalRoots = map[string]struct{}{
	// Web / API
	"django":         {},
	"rest_framework": {},
	"drf":            {},
	"flask":          {},
	"fastapi":        {},
	"starlette":      {},
	"uvicorn":        {},
	"gunicorn":       {},
	"werkzeug":       {},
	"jinja2":         {},
	"sanic":          {},
	"tornado":        {},
	"bottle":         {},
	"falcon":         {},
	"connexion":      {},
	"aiohttp":        {},
	"httpx":          {},
	"requests":       {},
	"urllib3":        {},
	// Database / ORM
	"sqlalchemy":  {},
	"alembic":     {},
	"psycopg2":    {},
	"psycopg":     {},
	"asyncpg":     {},
	"pymongo":     {},
	"motor":       {},
	"redis":       {},
	"peewee":      {},
	"tortoise":    {},
	"databases":   {},
	"mongoengine": {},
	// Data science
	"numpy":      {},
	"pandas":     {},
	"scipy":      {},
	"matplotlib": {},
	"seaborn":    {},
	"plotly":     {},
	"sklearn":    {},
	"torch":      {},
	"tensorflow": {},
	"keras":      {},
	"xgboost":    {},
	"lightgbm":   {},
	"pyarrow":    {},
	"cython":     {},
	"numba":      {},
	"dask":       {},
	// Validation / config / typing
	"pydantic":          {},
	"attrs":             {},
	"marshmallow":       {},
	"typing_extensions": {},
	"dataclasses":       {},
	// Testing / quality
	"pytest":      {},
	"mypy":        {},
	"unittest2":   {},
	"hypothesis":  {},
	"freezegun":   {},
	"responses":   {},
	"factory":     {},
	"factory_boy": {},
	"faker":       {},
	"mock":        {},
	// CLI / utilities
	"click":     {},
	"typer":     {},
	"argparse":  {},
	"docopt":    {},
	"rich":      {},
	"tqdm":      {},
	"colorama":  {},
	"tabulate":  {},
	"prompt_toolkit": {},
	// Async / queue / task
	"celery":      {},
	"kombu":       {},
	"rq":          {},
	"dramatiq":    {},
	"apscheduler": {},
	"asyncio":     {}, // stdlib but commonly imported
	// AWS / cloud
	"boto3":         {},
	"botocore":      {},
	"awswrangler":   {},
	"google":        {},
	"azure":         {},
	// Misc
	"yaml":     {},
	"toml":     {},
	"jsonschema": {},
	"cryptography": {},
	"jwt":      {},
	"PyJWT":    {},
	"bcrypt":   {},
	"argon2":   {},
	"passlib":  {},
	"oauthlib": {},
	"lxml":     {},
	"bs4":      {},
	"beautifulsoup4": {},
	"selenium": {},
	"playwright": {},
	// Image / PDF processing (wave-8 fixture-a residual)
	"cv2":        {}, // OpenCV
	"PIL":        {}, // Python Imaging Library
	"pil":        {}, // PIL alternate import
	"pdf2image":  {}, // PDF to image
	"pdfplumber": {}, // PDF extraction
	// Django REST (wave-8 fixture-a residual)
	"coreapi": {}, // Django REST coreapi client
}

// resolveImportToIDs walks every IMPORTS edge on every entity in
// entities and, when the source_module's top-level segment matches a
// known external Python package, rewrites the ToID to the
// `ext:<root>[:<imported_name>]` form. Idempotent — ToIDs already
// carrying the `ext:` prefix are left alone.
//
// Issue #693: the filter previously limited to SCOPE.Component/module
// entities. After #693, IMPORTS edges live on the file entity
// (SCOPE.Component/file). The filter is removed so the rewrite fires for
// any entity carrying an IMPORTS edge (file entity, or any other carrier).
//
// Mutates the entities slice's relationships in place.
func resolveImportToIDs(entities []types.EntityRecord) {
	for i := range entities {
		e := &entities[i]
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			if r.Properties == nil {
				continue
			}
			if strings.HasPrefix(r.ToID, "ext:") {
				continue // already external-tagged
			}
			mod := r.Properties["source_module"]
			if mod == "" {
				continue
			}
			// Skip relative imports — `from .foo import bar` carries a
			// source_module starting with "." which is never an external
			// package. Leave the in-tree resolver to handle those.
			if strings.HasPrefix(mod, ".") {
				continue
			}
			root := mod
			if dot := strings.IndexByte(mod, '.'); dot > 0 {
				root = mod[:dot]
			}
			lower := strings.ToLower(root)
			if _, ok := pythonKnownExternalRoots[lower]; !ok {
				continue
			}
			// Build ext: ToID. Use the LOWERCASED root for parity with
			// the resolver's case-folded allowlist lookup.
			imported := r.Properties["imported_name"]
			// For plain module imports the extractor sets imported_name
			// to the dotted module path (e.g. "requests.exceptions").
			// Distinguish between:
			//   from x import y          → source_module="x", imported_name="y"
			//   import x.y               → source_module="x.y", imported_name="x.y"
			// The first shape becomes `ext:x:y`; the second `ext:x`.
			if imported == mod || imported == "" {
				r.ToID = "ext:" + lower
			} else {
				// Strip any module-prefix duplication from imported_name
				// so `ext:django:models` not `ext:django:django.db.models`.
				leaf := imported
				if idx := strings.LastIndexByte(leaf, '.'); idx >= 0 {
					leaf = leaf[idx+1:]
				}
				r.ToID = "ext:" + lower + ":" + leaf
			}
		}
	}
}
