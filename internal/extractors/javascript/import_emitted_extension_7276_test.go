// Package javascript — issue #7276 regression suite.
//
// TypeScript under Node16/NodeNext requires a relative import specifier to
// carry the EMITTED extension while the file on disk carries the SOURCE one:
//
//	import { CategoriesService } from './categories.service.js';
//	// real file: src/categories.service.ts
//
// resolveRelativeImport used to return the specifier's extension verbatim, so
// importBinding.resolvedFile became "src/categories.service.js" — a path no
// entity is keyed under (internal/resolve keys byLocation on the entity's real
// SourceFile). Every consumer of resolvedFile then addressed a location that
// does not exist.
//
// The asymmetry is why this was invisible: the EXTENSIONLESS spelling falls
// through to `joined + ".ts"` and works. The bug fires only on the spelling
// Node16/NodeNext actually requires.
package javascript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/jsext"
	"github.com/cajasmota/grafel/internal/types"
)

// writeFile7276 writes content at repo-relative rel under root, creating
// parent directories.
func writeFile7276(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// importsResolvedFiles7276 returns every resolved_file property value found on
// IMPORTS edges, keyed by the import_path that produced it.
func importsResolvedFiles7276(entities []types.EntityRecord) map[string]string {
	out := map[string]string{}
	for i := range entities {
		for j := range entities[i].Relationships {
			r := &entities[i].Relationships[j]
			if r.Kind != "IMPORTS" || r.Properties == nil {
				continue
			}
			if rf := r.Properties.Get("resolved_file"); rf != "" {
				out[r.Properties.Get("import_path")] = rf
			}
		}
	}
	return out
}

// callsToIDs7276 returns every CALLS edge ToID in the record set.
func callsToIDs7276(entities []types.EntityRecord) []string {
	var out []string
	for i := range entities {
		for j := range entities[i].Relationships {
			if entities[i].Relationships[j].Kind == "CALLS" {
				out = append(out, entities[i].Relationships[j].ToID)
			}
		}
	}
	return out
}

// TestEmittedJsExtensionResolvesToTsSource_7276 is the headline regression.
//
// It asserts TARGET IDENTITY, not edge existence: the CALLS structural ref
// must name `src/categories.service.ts`, the path the resolver's byLocation
// index is actually keyed under. A test that only asserted "a CALLS edge
// exists" would pass on the broken build too — the edge is emitted either
// way, it just addresses a location no entity occupies.
func TestEmittedJsExtensionResolvesToTsSource_7276(t *testing.T) {
	resetAliasMapCache()
	dir := t.TempDir()

	// Only the TypeScript source exists on disk. There is deliberately no
	// `categories.service.js` — so the `.ts` and `.js` candidates give
	// DIFFERENT answers and this fixture discriminates.
	writeFile7276(t, dir, "src/categories.service.ts",
		"export function findAllCategories() { return []; }\n")

	src := []byte("import { findAllCategories } from './categories.service.js';\n" +
		"export function list() { return findAllCategories(); }\n")
	tree := parseTSForAlias(t, src)
	defer tree.Close()
	ents := extractWithRepo(t, dir, "src/categories.controller.ts", src, tree)

	got := importsResolvedFiles7276(ents)["./categories.service.js"]
	if got != "src/categories.service.ts" {
		t.Errorf("resolved_file = %q, want %q — the `.js` specifier names the "+
			"EMITTED file; the entity is keyed under the source path", got,
			"src/categories.service.ts")
	}

	wantRef := "scope:operation:ref:typescript:src/categories.service.ts:findAllCategories"
	found := false
	for _, id := range callsToIDs7276(ents) {
		if id == wantRef {
			found = true
		}
	}
	if !found {
		t.Errorf("no CALLS edge with ToID %q; got %q — the structural ref must "+
			"name the real source file or byLocation cannot bind it",
			wantRef, callsToIDs7276(ents))
	}
}

// TestEmittedJsExtensionForbiddenRows_7276 carries the forbidden rows for the
// widening. Each row plants a fixture where the WRONG answer is reachable and
// asserts it is not taken.
func TestEmittedJsExtensionForbiddenRows_7276(t *testing.T) {
	rows := []struct {
		name string
		// files created on disk, repo-relative
		files []string
		// importer path and specifier
		importer string
		spec     string
		want     string
		why      string
	}{
		{
			name:     "js_spec_prefers_real_js_over_absent_ts",
			files:    []string{"src/legacy.helper.js"},
			importer: "src/app.ts",
			spec:     "./legacy.helper.js",
			want:     "src/legacy.helper.js",
			why: "a real .js carrier exists and no .ts does; fabricating " +
				"src/legacy.helper.ts addresses a file that is not there",
		},
		{
			name:     "js_spec_prefers_ts_when_both_exist",
			files:    []string{"src/dual.mod.ts", "src/dual.mod.js"},
			importer: "src/app.ts",
			spec:     "./dual.mod.js",
			want:     "src/dual.mod.ts",
			why:      "tsc resolution order prefers the TypeScript source",
		},
		{
			name:     "mjs_spec_must_not_reach_ts",
			files:    []string{"src/esmonly.ts"},
			importer: "src/app.ts",
			spec:     "./esmonly.mjs",
			want:     "src/esmonly.mjs",
			why: "`./x.mjs` can only mean x.mts or x.mjs; reaching x.ts would " +
				"mint an edge to a file the author never referenced",
		},
		{
			name:     "cjs_spec_must_not_reach_ts",
			files:    []string{"src/cjsonly.ts"},
			importer: "src/app.ts",
			spec:     "./cjsonly.cjs",
			want:     "src/cjsonly.cjs",
			why:      "`./x.cjs` can only mean x.cts or x.cjs",
		},
		{
			name:     "mjs_spec_reaches_mts",
			files:    []string{"src/esmreal.mts"},
			importer: "src/app.ts",
			spec:     "./esmreal.mjs",
			want:     "src/esmreal.mts",
			why:      "the ESM family replacement is the point of the fix",
		},
		{
			name:     "no_cross_directory_rebind",
			files:    []string{"src/thing.ts"},
			importer: "src/app.ts",
			spec:     "./sub/thing.js",
			want:     "src/sub/thing.js",
			why: "src/thing.ts is a same-name file one directory up; the " +
				"resolution must never climb out of the specified directory",
		},
		{
			name:     "extensionless_still_defaults_to_ts",
			files:    []string{"src/plain.js"},
			importer: "src/app.ts",
			spec:     "./plain",
			want:     "src/plain.ts",
			why: "the extensionless arm is untouched by this fix; changing it " +
				"is a separate widening with its own blast radius",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range row.files {
				writeFile7276(t, dir, f, "export const x = 1;\n")
			}
			got := resolveRelativeImport(dir, row.importer, row.spec)
			if got != row.want {
				t.Errorf("resolveRelativeImport(<root>, %q, %q) = %q, want %q — %s",
					row.importer, row.spec, got, row.want, row.why)
			}
		})
	}
}

// TestEmittedJsExtensionNoRepoRootIsVerbatim_7276 pins the degraded path: with
// no repo root the extractor cannot stat, so the specifier's own extension is
// kept verbatim rather than guessed at. This is the pre-#7276 behaviour and
// unit callers (which pass no root) depend on it.
func TestEmittedJsExtensionNoRepoRootIsVerbatim_7276(t *testing.T) {
	if got, want := resolveRelativeImport("", "src/app.ts", "./x.js"), "src/x.js"; got != want {
		t.Errorf("resolveRelativeImport(\"\", ...) = %q, want %q", got, want)
	}
	if got, want := resolveRelativeImport("", "src/app.ts", "./x"), "src/x.ts"; got != want {
		t.Errorf("resolveRelativeImport(\"\", ...) = %q, want %q", got, want)
	}
}

// TestEmittedJsExtensionMissingCarrierIsVerbatim_7276 pins the negative space:
// when NOTHING in the family exists on disk the specifier is kept verbatim, so
// the widening can only ever move a path onto a file that is really there.
func TestEmittedJsExtensionMissingCarrierIsVerbatim_7276(t *testing.T) {
	dir := t.TempDir()
	writeFile7276(t, dir, "src/unrelated.ts", "export const x = 1;\n")
	if got, want := resolveRelativeImport(dir, "src/app.ts", "./ghost.js"), "src/ghost.js"; got != want {
		t.Errorf("resolveRelativeImport = %q, want %q — nothing in the .js "+
			"family exists, so nothing may be invented", got, want)
	}
	if strings.HasSuffix(resolveRelativeImport(dir, "src/app.ts", "./ghost.js"), ".ts") {
		t.Error("a missing carrier resolved to a .ts path")
	}
}

// TestEveryImportExtensionHasAFamily_7276 grades the INERTNESS direction of
// the fix rather than its correctness.
//
// resolveEmittedExtension consults jsext.ReplacementsFor, which returns nil
// for an extension with no family entry — and a nil candidate list makes the
// helper return the specifier verbatim, i.e. silently back to the pre-#7276
// bug for that extension only. jsImportExtensions is itself derived from the
// classifier (TestJSImportExtensionsAgreeWithClassifier_7272), so a new
// JS/TS extension arrives here automatically and would land in exactly that
// hole with every other test still green.
//
// Each family must also contain the extension itself, or a specifier naming a
// file that really exists on disk under its own extension would be walked
// past.
func TestEveryImportExtensionHasAFamily_7276(t *testing.T) {
	if len(jsImportExtensions) == 0 {
		t.Fatal("jsImportExtensions is empty — this guard would be vacuous")
	}
	for _, ext := range jsImportExtensions {
		fam := jsext.ReplacementsFor(ext)
		if len(fam) == 0 {
			t.Errorf("jsImportExtensions lists %q but jsext has no family for it — "+
				"resolveEmittedExtension falls back to the specifier verbatim, which "+
				"is the #7276 defect reinstated for that extension alone", ext)
			continue
		}
		self := false
		for _, c := range fam {
			if c == ext {
				self = true
			}
		}
		if !self {
			t.Errorf("jsext.ReplacementsFor(%q) = %v, which does not contain %q itself — "+
				"a specifier naming a file that really exists under its own extension "+
				"would be walked past", ext, fam, ext)
		}
	}
}
