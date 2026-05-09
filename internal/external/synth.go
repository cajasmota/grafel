// Package external synthesises placeholder entities for references that
// point at code outside the indexed corpus — third-party packages
// (django, react, lodash...), language stdlib (os, json, fmt...), and
// well-known stdlib symbols (Println, print...).
//
// PORT-EXT (issue #32). After Pass 3 + the resolver (PORT-2-FIX,
// PORT-2-FIX-3) finish, a meaningful fraction of relationships still
// have stub strings as ToID — by construction, because the target
// source isn't in the corpus. They are nonetheless real graph edges
// the agent should be able to traverse and stop cleanly at. This pass
// turns each unique unresolved external into a placeholder Entity with
// id "ext:<canonical-name>" and rewrites the relationship's ToID to
// point at it.
package external

import (
	"net/url"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

// KindExternal is the entity kind stamped on every synthesised
// placeholder. It joins the existing SCOPE.* taxonomy used elsewhere
// in the indexer. Kept as a string alias for callers; the source of
// truth is types.EntityKindExternal (Issue #77).
const KindExternal = string(types.EntityKindExternal)

// ExtIDPrefix is the deterministic prefix used by external-entity IDs.
// It is intentionally NOT a 16-char hex string so the resolver's
// isHexID heuristic continues to treat it as a stub-shaped value if a
// later pass ever encounters it.
const ExtIDPrefix = "ext:"

// Stats reports how the synthesis pass touched the document.
type Stats struct {
	// Synthesized is the number of NEW placeholder entities appended to
	// the document. Equal to UniqueExternals on a fresh run; zero on a
	// re-run because every external is already present.
	Synthesized int
	// RelationshipsResolved is the number of relationship endpoints
	// rewritten from a bare-name stub to "ext:<name>".
	RelationshipsResolved int
	// UniqueExternals is the number of distinct external names this
	// pass touched (including any that were already present from a
	// previous run).
	UniqueExternals int
}

// Synthesize scans every relationship in doc, looks for endpoints
// whose ToID is a still-unresolved string that matches an external
// reference heuristic, and appends placeholder entities for each
// unique external. The relationship's ToID is rewritten in-place to
// "ext:<canonical-name>". Idempotent: calling Synthesize twice on the
// same document is a no-op on the second call.
func Synthesize(doc *graph.Document) Stats {
	if doc == nil {
		return Stats{}
	}

	// Build a set of all known entity IDs so we don't re-synthesise an
	// external that already exists in the document. Re-runs of this
	// pass on the same document must be idempotent.
	known := make(map[string]bool, len(doc.Entities))
	for k := range doc.Entities {
		known[doc.Entities[k].ID] = true
	}

	// First pass — collect every unique external name we want to
	// synthesise. The placeholder carries a subtype hint
	// ("package"/"function") but the language field is left empty:
	// we don't reliably know the source language at this layer (a name
	// like "json" or "abc" exists in multiple ecosystems), and an
	// inaccurate language tag is worse than none at all. Language can
	// be populated by a downstream enrichment pass that has more
	// context (e.g. inspecting the import statement that produced the
	// edge).
	type externalInfo struct {
		canonical string
		subtype   string
		language  string
	}
	uniques := make(map[string]externalInfo) // ext-id -> info
	resolved := 0

	for k := range doc.Relationships {
		rel := &doc.Relationships[k]
		if rel.ToID == "" || isHexID(rel.ToID) || strings.HasPrefix(rel.ToID, ExtIDPrefix) {
			continue
		}
		canonical, subtype, ok := classifyExternal(rel.ToID, rel.Kind)
		if !ok {
			continue
		}
		extID := ExtIDPrefix + canonical
		if _, seen := uniques[extID]; !seen {
			uniques[extID] = externalInfo{
				canonical: canonical,
				subtype:   subtype,
				language:  "",
			}
		}
		rel.ToID = extID
		resolved++
	}

	// Sort canonical names for deterministic append order — keeps
	// graph.json byte-stable across runs on the same corpus.
	keys := make([]string, 0, len(uniques))
	for k := range uniques {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	synthesised := 0
	for _, extID := range keys {
		if known[extID] {
			continue // re-run path: placeholder already present
		}
		info := uniques[extID]
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:            extID,
			Name:          info.canonical,
			QualifiedName: info.canonical,
			Kind:          KindExternal,
			Subtype:       info.subtype,
			SourceFile:    "",
			Language:      info.language,
			Metadata: map[string]interface{}{
				"is_external":    true,
				"discovered_via": "ext-synthesis",
			},
		})
		known[extID] = true
		synthesised++
	}

	// Reflect the new entities + rewritten edges in the doc-level
	// stats. Relationships count is unchanged (we rewrote endpoints,
	// not added rows) but Entities grew by len(synthesised).
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)

	return Stats{
		Synthesized:           synthesised,
		RelationshipsResolved: resolved,
		UniqueExternals:       len(uniques),
	}
}

// classifyExternal decides whether a stub-shaped ToID looks like an
// external reference, and if so returns the canonical name we should
// use for the placeholder entity.
//
// Heuristics, in order:
//
//  1. "Kind:Name" form where Name matches a well-known external —
//     canonicalise to Name (drop the kind prefix).
//  2. Bare names matching a stdlib stop-list (Println, print, etc.) —
//     canonicalise to the bare name.
//  3. Bare names matching a known third-party package allowlist
//     (django, react, lodash, ...).
//  4. Import-shaped paths whose first segment matches the allowlist
//     (e.g. "django.db.models" → "django").
//
// Returns ("", "", false) when the stub doesn't look external — those
// are left untouched and continue to count as "unmatched" in the
// resolver stats.
func classifyExternal(stub, relKind string) (canonical, subtype string, ok bool) {
	if stub == "" {
		return "", "", false
	}

	// Issue #89 — manifest extractor emits dependency stubs as
	// "scope:component:external_dep:<package_manager>:<package>". The
	// "external_dep" tag is the extractor's explicit signal that this is
	// an external (third-party) dependency; route it to a placeholder so
	// the post-synthesis classifier lands it in external-known/external-
	// unknown rather than bug-extractor.
	if strings.HasPrefix(stub, "scope:component:external_dep:") {
		rest := stub[len("scope:component:external_dep:"):]
		// rest is "<pm>:<package>" — drop the package-manager segment.
		if i := strings.IndexByte(rest, ':'); i > 0 && i < len(rest)-1 {
			pkg := strings.TrimSpace(rest[i+1:])
			if pkg != "" && !strings.ContainsAny(pkg, "/\\") {
				root := pkg
				if dot := strings.IndexByte(pkg, '.'); dot > 0 {
					root = pkg[:dot]
				}
				return root, "package", true
			}
		}
	}

	// Issue #89 — httpclient extractor emits external HTTP API references
	// as "scope:external_api:<url>". The URL is the unresolvable identity
	// of the external service; canonicalise to the host segment when we
	// can extract one (everything between "://" and the next "/"), and
	// fall back to a synthetic "external_api" bucket otherwise. Either
	// way it leaves bug-extractor.
	if strings.HasPrefix(stub, "scope:external_api:") {
		raw := stub[len("scope:external_api:"):]
		host := externalAPIHost(raw)
		if host != "" {
			return host, "external_api", true
		}
		// Bare URL fragment / non-URL identifier — bucket under a
		// stable "external_api" placeholder rather than leaving it as
		// bug-extractor.
		return "external_api", "external_api", true
	}

	// Pass 3 cross-language extractors emit external imports as
	// "scope:<kind>:import:external:<name>" — short structural-ref
	// form that the resolver leaves untouched (it expects 6 segments).
	// Recognise it explicitly here; the trailing segment is the
	// canonical package name.
	if strings.HasPrefix(stub, "scope:") && strings.Contains(stub, ":external:") {
		if idx := strings.LastIndex(stub, ":external:"); idx >= 0 {
			ext := stub[idx+len(":external:"):]
			ext = strings.TrimSpace(ext)
			if ext == "" || strings.ContainsAny(ext, "/\\") {
				return "", "", false
			}
			root := ext
			if dot := strings.IndexByte(ext, '.'); dot > 0 {
				root = ext[:dot]
			}
			// Trust the extractor's "external" tag — emit a placeholder
			// even when the package isn't on our static allowlist. The
			// extractor has already classified it as not-local.
			return root, "package", true
		}
	}

	// Issue #82: Format A structural-refs that survived the resolver are
	// dangling by definition (the resolver rewrites resolved endpoints
	// to hex IDs). For EXTENDS edges from cross/hierarchy, the tail is
	// the parent class name — when it looks like an external import
	// (dotted, e.g. "serializers.ModelSerializer" or "rest_framework.
	// generics.ListAPIView"), synthesise a placeholder for the package
	// root. Bare-name tails are intentionally NOT handled here because
	// they could be either a local class or an external base — the
	// existing allowlist branch below already catches the well-known
	// cases.
	//
	// Format A: scope:<kind>:<subtype>:<lang>:<file_path>:<name>
	// We pull the trailing segment after the last ':' (file paths in
	// archigraph entity refs are normalised to forward slashes, so the
	// last ':' is the kind/name separator, not part of the path).
	if strings.HasPrefix(stub, "scope:") {
		if idx := strings.LastIndexByte(stub, ':'); idx >= 0 && idx < len(stub)-1 {
			tail := stub[idx+1:]
			if looksLikeExternalImport(tail) {
				root := tail
				if dot := strings.IndexByte(tail, '.'); dot > 0 {
					root = tail[:dot]
				}
				return root, "package", true
			}
		}
	}

	// Strip a leading "Kind:" prefix if present — e.g. "Module:django"
	// or "Function:Println". The remainder is what we classify.
	name := stub
	if i := strings.IndexByte(stub, ':'); i > 0 {
		// Only treat the prefix as a kind hint when it's a short
		// alphabetic word; otherwise keep the whole stub (e.g.
		// "scope:..." structural-refs were already handled by the
		// resolver and shouldn't end up here).
		prefix := stub[:i]
		if isKindLikePrefix(prefix) {
			name = stub[i+1:]
		} else {
			return "", "", false
		}
	}
	if name == "" {
		return "", "", false
	}

	// Scoped npm packages — "@scope/pkg" or "@scope/pkg/subpath" — are
	// the only legitimate external shape that contains a '/'. Detect
	// them BEFORE the path-separator rejection below so they reach the
	// allowlist; everything else with a separator is a structural-ref
	// or local file path and is dropped (issue #71).
	if scope, ok := scopedNpmRoot(name); ok {
		// Collapse to "@scope/pkg" form (drop any deeper subpath) for
		// allowlist lookup, then to the scope itself if the full form
		// isn't catalogued. Either match yields a single placeholder
		// per scoped package.
		if isKnownExternalPackage(scope) {
			return scope, "package", true
		}
		return "", "", false
	}

	// Reject obviously non-external shapes: anything containing a path
	// separator was either a structural-ref or a local file path, both
	// already handled upstream.
	if strings.ContainsAny(name, "/\\") {
		return "", "", false
	}

	// Stdlib function stop-list — bare names like "Println", "print".
	if subtype, ok := stdlibFunction(name); ok {
		return name, subtype, true
	}

	// Dotted path → first segment is what we canonicalise to. Common
	// shape for Python imports ("django.db.models" -> "django") or
	// JS submodules ("lodash.debounce" -> "lodash").
	root := name
	if dot := strings.IndexByte(name, '.'); dot > 0 {
		root = name[:dot]
	}

	if isKnownExternalPackage(root) {
		// "package" subtype when the canonical name IS the root,
		// otherwise "module" — django.db.models is a module of the
		// django package.
		if root == name {
			return root, "package", true
		}
		// Per the PORT-EXT spec we collapse to the package level so
		// there's a single placeholder per third-party package, not
		// one per imported submodule. Submodule fan-out can be
		// re-introduced in a follow-up.
		return root, "package", true
	}

	return "", "", false
}

// scopedNpmRoot recognises the npm scoped-package shape "@scope/pkg"
// (optionally followed by "/subpath") and returns the "@scope/pkg"
// root. Returns ("", false) when s doesn't match the scoped-npm
// convention — typical reject cases are bare names, "./relative",
// "/absolute", or backslash-bearing paths.
//
// The scope and package segments must each be non-empty and may
// contain only word chars, '-', and '.' — the npm name grammar's
// safe subset (https://docs.npmjs.com/cli/v10/configuring-npm/package-json#name).
func scopedNpmRoot(s string) (string, bool) {
	if len(s) < 4 || s[0] != '@' {
		return "", false
	}
	slash := strings.IndexByte(s, '/')
	if slash <= 1 {
		// Need at least one char after '@' before the '/'.
		return "", false
	}
	scope := s[1:slash]
	rest := s[slash+1:]
	if !isNpmSegment(scope) {
		return "", false
	}
	// Trim any sub-path after the package name.
	pkg := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		pkg = rest[:i]
	}
	if !isNpmSegment(pkg) {
		return "", false
	}
	// Backslashes are never legal in an npm name.
	if strings.ContainsRune(s, '\\') {
		return "", false
	}
	return "@" + scope + "/" + pkg, true
}

// isNpmSegment reports whether s is a valid scope or package segment
// for the scoped-npm allowlist gate. Conservatively limited to
// [A-Za-z0-9_.-].
func isNpmSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-' || c == '.':
		default:
			return false
		}
	}
	return true
}

// stdlibFunction returns the subtype for a bare stdlib function name
// (e.g. "Println" → "function") or ("", false) when the name isn't on
// the small per-language stop-list. Kept deliberately small — v1.0
// catches the highest-volume bare-name calls without ballooning into
// a full stdlib catalogue.
func stdlibFunction(name string) (string, bool) {
	if _, ok := stdlibBareNames[name]; ok {
		return "function", true
	}
	return "", false
}

// stdlibBareNames is the v1.0 stop-list of stdlib functions and
// builtins whose bare-name calls we want to surface as external
// nodes. The list is curated rather than exhaustive — only names
// that (a) appear with high frequency in real codebases and (b) are
// extremely unlikely to collide with a user-defined identifier are
// included. False positives synthesise a placeholder for a name that
// might have been a real local entity, which is worse than missing
// one.
var stdlibBareNames = map[string]struct{}{
	// Go fmt / built-in calls
	"Println": {},
	"Printf":  {},
	"Print":   {},
	"Sprintf": {},
	"Errorf":  {},
	"Fatal":   {},
	"Fatalf":  {},
	"Panic":   {},
	"Panicf":  {},
	// Python builtins (PEP 3102 / builtins module). Keep
	// alphabetical for review.
	"abs":       {},
	"all":       {},
	"any":       {},
	"bool":      {},
	"callable":  {},
	"chr":       {},
	"dict":      {},
	"enumerate": {},
	"filter":    {},
	"float":     {},
	"format":    {},
	"frozenset": {},
	// Reflection builtins (getattr/setattr/hasattr/delattr/eval/exec/
	// compile/__import__) deliberately excluded — they are dynamic-
	// dispatch primitives, not external imports. The resolver classifier
	// matches them against the per-language dynamic-pattern catalog and
	// tags them DispositionDynamic (Refs #95).
	"hash":       {},
	"id":         {},
	"int":        {},
	"isinstance": {},
	"issubclass": {},
	"iter":       {},
	"len":        {},
	"list":       {},
	"map":        {},
	"max":        {},
	"min":        {},
	"next":       {},
	"object":     {},
	"open":       {},
	"ord":        {},
	"print":      {},
	"property":   {},
	"range":      {},
	"repr":       {},
	"reversed":   {},
	"round":      {},
	"set":        {},
	"slice":      {},
	"sorted":     {},
	"str":        {},
	"sum":        {},
	"super":      {},
	"tuple":      {},
	"type":       {},
	"vars":       {},
	"zip":        {},
	// Python stdlib exceptions (extremely unlikely to collide).
	"Exception":           {},
	"ValueError":          {},
	"TypeError":           {},
	"KeyError":            {},
	"IndexError":          {},
	"AttributeError":      {},
	"RuntimeError":        {},
	"NotImplementedError": {},
	"StopIteration":       {},
	"FileNotFoundError":   {},
	// Django / DRF / Python framework symbols seen at high volume in
	// real codebases. Collisions with user code are possible but rare
	// (these are conventionally instantiated, not redefined).
	"Response":        {},
	"ValidationError": {},
	"NotFound":        {},
	"BeautifulSoup":   {},
	"BytesIO":         {},
	"StringIO":        {},
	"ObjectId":        {},
	// JS / browser
	"console": {},
	"fetch":   {},
	// Issue #89 — high-volume Python str/list/dict/set/file methods.
	// These bare-name calls arrive at the resolver after the extractor
	// strips the receiver (`s.append(x)` → `append`). Without this list
	// they all land in bug-extractor; with it they correctly classify as
	// external-known builtins.
	//
	// Issue #94 follow-up: removed names that collide with common
	// user-defined method identifiers — write/read/close/index/copy/
	// replace/items/keys/values/update/pop/clear/extend/append/remove.
	// Misclassifying a real local method as a stdlib bare-name turns a
	// genuine bug into a synthesised placeholder, hiding the real fix.
	// Kept: names that are unambiguously built-in across mainstream
	// Python codebases (no/extremely rare user-method collisions).
	"insert":     {},
	"setdefault": {},
	"startswith": {},
	"endswith":   {},
	"strip":      {},
	"lstrip":     {},
	"rstrip":     {},
	"split":      {},
	"rsplit":     {},
	"splitlines": {},
	"join":       {},
	"lower":      {},
	"upper":      {},
	"title":      {},
	"encode":     {},
	"decode":     {},
	"isdigit":    {},
	"isalpha":    {},
	"isalnum":    {},
	"readline":   {},
	"readlines":  {},
	"writelines": {},
	"flush":      {},
	"seek":       {},
	"tell":       {},
	// Python os/path/io stdlib functions seen at high volume in real
	// codebases — bare-name when accessed without a module qualifier.
	"getcwd":          {},
	"listdir":         {},
	"makedirs":        {},
	"deepcopy":        {},
	"deque":           {},
	"defaultdict":     {},
	"OrderedDict":     {},
	"Counter":         {},
	"namedtuple":      {},
	"RawConfigParser": {},
	"ConfigParser":    {},
}

// isKnownExternalPackage reports whether s matches our small allowlist
// of well-known third-party packages and stdlib top-level modules. The
// allowlist is intentionally narrow for v1.0 — false positives turn a
// local name into a placeholder, which is worse than missing one.
func isKnownExternalPackage(s string) bool {
	lower := strings.ToLower(s)
	if _, ok := knownExternalPackages[lower]; ok {
		return true
	}
	// Scoped npm fallback: a full "@scope/pkg" key matches if the bare
	// "@scope" key is on the allowlist. This lets us keep the existing
	// scope-level entries (@radix-ui, @tanstack, ...) functional for
	// every package they ship without enumerating each one. The scope
	// must be non-empty and start with '@' (issue #71).
	if strings.HasPrefix(lower, "@") {
		if slash := strings.IndexByte(lower, '/'); slash > 1 {
			scope := lower[:slash]
			if _, ok := knownExternalPackages[scope]; ok {
				return true
			}
		}
	}
	return false
}

// IsKnownExternalPackage is the exported form of the allowlist check.
// VERIFY-2-PREP / issue #56: the resolver consults this to decide
// whether an "ext:<pkg>" placeholder should be tagged ExternalKnown
// (allowlisted, expected) or ExternalUnknown (real external dep we
// haven't catalogued yet). Comparison is case-folded.
func IsKnownExternalPackage(s string) bool {
	return isKnownExternalPackage(s)
}

// knownExternalPackages is the v1.1 allowlist. Lowercase keys; lookups
// are case-folded. Curated from real codebases — Python web/data
// stacks, JS/TS frontend + node, Go services, JVM enterprise. False
// positives synthesise a placeholder for what might have been a local
// name; bias toward names extremely unlikely to collide.
var knownExternalPackages = map[string]struct{}{
	// Python ecosystem (third-party)
	"django":            {},
	"rest_framework":    {},
	"drf":               {},
	"flask":             {},
	"fastapi":           {},
	"sqlalchemy":        {},
	"alembic":           {},
	"pydantic":          {},
	"celery":            {},
	"requests":          {},
	"httpx":             {},
	"numpy":             {},
	"pandas":            {},
	"scipy":             {},
	"pytest":            {},
	"mypy":              {},
	"attrs":             {},
	"click":             {},
	"redis":             {},
	"boto3":             {},
	"awswrangler":       {},
	"typing_extensions": {},
	// Python stdlib top-level
	"os":              {},
	"sys":             {},
	"json":            {},
	"re":              {},
	"typing":          {},
	"datetime":        {},
	"collections":     {},
	"asyncio":         {},
	"concurrent":      {},
	"multiprocessing": {},
	"threading":       {},
	"queue":           {},
	"weakref":         {},
	"logging":         {},
	"pathlib":         {},
	"functools":       {},
	"itertools":       {},
	"operator":        {},
	"builtins":        {},
	"unittest":        {},
	"abc":             {},
	"enum":            {},
	"uuid":            {},
	"hashlib":         {},
	"dataclasses":     {},
	"contextlib":      {},
	"warnings":        {},
	"tempfile":        {},
	"subprocess":      {},
	"argparse":        {},
	"socket":          {},
	"ssl":             {},
	"urllib":          {},
	// JS / TS ecosystem (unscoped)
	"react":        {},
	"vue":          {},
	"angular":      {},
	"lodash":       {},
	"ramda":        {},
	"immer":        {},
	"dayjs":        {},
	"date-fns":     {},
	"axios":        {},
	"ky":           {},
	"express":      {},
	"next":         {},
	"jest":         {},
	"vitest":       {},
	"mocha":        {},
	"chai":         {},
	"sinon":        {},
	"supertest":    {},
	"typescript":   {},
	"zod":          {},
	"prisma":       {},
	"redux":        {},
	"rxjs":         {},
	"tanstack":     {},
	"nodemailer":   {},
	"bcrypt":       {},
	"jsonwebtoken": {},
	"helmet":       {},
	"multer":       {},
	// JS / TS scoped packages (kept lowercase per case-folded lookup;
	// only the leading "@scope" segment is matched).
	"@radix-ui":        {},
	"@tanstack":        {},
	"@reduxjs":         {},
	"@testing-library": {},
	"@types":           {},
	"@nestjs":          {},
	"@apollo":          {},
	"@mui":             {},
	"@emotion":         {},
	"@chakra-ui":       {},
	"@headlessui":      {},
	"@hookform":        {},
	"@trpc":            {},
	"@storybook":       {},
	"@vitejs":          {},
	"@babel":           {},
	"@swc":             {},
	"@sentry":          {},
	"@auth0":           {},
	"@aws-sdk":         {},
	"@azure":           {},
	"@google-cloud":    {},
	"@graphql-tools":   {},
	"@vue":             {},
	"@angular":         {},
	// Go stdlib top-level
	"fmt":           {},
	"strings":       {},
	"strconv":       {},
	"errors":        {},
	"context":       {},
	"net":           {},
	"http":          {},
	"io":            {},
	"bytes":         {},
	"sort":          {},
	"sync":          {},
	"time":          {},
	"path":          {},
	"regexp":        {},
	"testing":       {},
	"encoding/json": {},
	// Go third-party (high-volume)
	"testify": {},
	"viper":   {},
	"cobra":   {},
	"zap":     {},
	"logrus":  {},
	"sqlx":    {},
	"gorm":    {},
	"gorilla": {},
	// Java / Kotlin
	"java":                  {},
	"javax":                 {},
	"kotlin":                {},
	"kotlinx":               {},
	"org.springframework":   {},
	"com.fasterxml.jackson": {},
	"com.google.guava":      {},
	"org.apache.commons":    {},
	"junit":                 {},
	"mockito":               {},
	"slf4j":                 {},
	"log4j":                 {},
	"lombok":                {},
	// Ruby
	"rails":        {},
	"activerecord": {},
}

// isKindLikePrefix reports whether s is a short, alphabetic kind name
// like "Module" or "Function" — used to decide whether a "Foo:Bar"
// stub should be treated as Kind:Name. The structural-ref shape
// "scope:..." has multiple ':'s and a long prefix; this filter avoids
// claiming those.
//
// '.' is intentionally allowed in the prefix character class to admit
// dotted-kind shapes like "a.b.c:Symbol" that some extractors emit
// (e.g. fully-qualified Python or JVM kind hints). The trade-off is
// that "java.util.Map:put" looks kind-like and gets treated as a
// Kind:Name pair — that's what we want for these external lookups.
func isKindLikePrefix(s string) bool {
	if len(s) == 0 || len(s) > 24 {
		return false
	}
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '.') {
			return false
		}
	}
	return true
}

// looksLikeExternalImport reports whether a bare name has the shape of
// a dotted external import — at least one dot, terminal segment is a
// capitalised identifier, and no path separators. Used by Pass 4.5
// (issue #82) to synthesise placeholders for dangling EXTENDS targets
// like "serializers.ModelSerializer" that cross/hierarchy emits as
// structural-refs without an :external: marker.
func looksLikeExternalImport(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, "/\\ \t") {
		return false
	}
	dot := strings.IndexByte(s, '.')
	if dot <= 0 || dot >= len(s)-1 {
		return false
	}
	// Every segment must be a valid identifier (letters, digits, '_'),
	// non-empty, and not start with a digit.
	for _, seg := range strings.Split(s, ".") {
		if !isIdentSegment(seg) {
			return false
		}
	}
	// Terminal segment must start with an uppercase letter — the
	// convention for class/type names that show up as EXTENDS targets.
	last := s[strings.LastIndexByte(s, '.')+1:]
	if last == "" {
		return false
	}
	c := last[0]
	if !(c >= 'A' && c <= 'Z') {
		return false
	}
	return true
}

// isIdentSegment reports whether s is a non-empty identifier segment
// (ASCII letters/digits/underscore, not starting with a digit).
func isIdentSegment(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// externalAPIHost extracts the host segment from a URL-shaped string.
// Returns "" when raw doesn't look like a URL with a recognisable host.
// Issue #89.
//
// Issue #94 follow-up: the original byte-scanning implementation broke
// on IPv6 hosts ("https://[::1]:8080" canonicalised to "[" because the
// port-stripping ran before the bracket-balanced host was extracted).
// Switched to net/url which understands bracketed IPv6 hosts and gives
// a clean Hostname() without brackets or port. Falls back to "" on
// parse error or any URL without a host.
func externalAPIHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Require an explicit scheme to keep behaviour close to the prior
	// "://" gate; net/url is permissive about scheme-less inputs.
	if !strings.Contains(raw, "://") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	// Reject obviously malformed hosts (e.g. percent-encoded garbage
	// that survived parsing).
	for _, r := range host {
		if r == '%' {
			return ""
		}
	}
	return host
}

// isHexID mirrors resolve.isHexID — a 16-char lower-hex string is
// already an entity ID and must never be treated as a stub.
func isHexID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
