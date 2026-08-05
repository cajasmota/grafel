// Package extractors — #6151 regression gate.
//
// THE INVARIANT THIS FILE EXISTS TO HOLD, stated for whoever adds the next
// incremental fixture: every other incremental fixture in this package is
// Python or Go, and BOTH of those extractors self-parse from Content when
// FileInput.TSTree is nil (python/extractor.go, golang/extractor.go). They are
// therefore blind by construction to a pipeline that hands the extractor no
// tree. Fourteen tree-sitter-backed extractors — csharp, dockerfile, elixir,
// groovy, java, javascript/typescript, kotlin, lua, php, proto, ruby, rust,
// scala, swift — instead `return nil, nil` unconditionally on a nil tree.
//
// TryIncremental evicts the changed file's entities in Step 5 and re-adds
// whatever Step 6's re-extraction produces. When Step 6 supplied TSTree: nil,
// those fourteen languages re-added NOTHING: total, silent data loss for the
// edited file, with Done=true, no fallback and no error.
//
// Kotlin is used deliberately: it is one of the fourteen, so this fixture fails
// the moment the incremental path stops supplying a real tree.
//
// The assertions are on entity IDENTITY BY CONTENT (kind/name/source_file) and
// they are BIDIRECTIONAL. A count-based assertion cannot distinguish
// "re-extracted correctly" from "deleted and never re-added" — that is exactly
// how this defect survived.
package extractors

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/indexer/diff"
	"github.com/cajasmota/grafel/internal/types"
)

const ntKotlinFile = "Billing.kt"

const ntKotlinBefore = `package com.example.billing

class Ledger {
    fun applyCharge(amount: Int): Boolean {
        return amount > 0
    }
}

fun voidEntry(id: String): Unit {
}
`

// The edit adds one class and one top-level function and touches nothing else.
const ntKotlinAfter = `package com.example.billing

class Ledger {
    fun applyCharge(amount: Int): Boolean {
        return amount > 0
    }
}

fun voidEntry(id: String): Unit {
}

class Reconciler {
    fun settleBatch(n: Int): Int {
        return n
    }
}
`

// ntWrite writes one file under repo, creating parents.
func ntWrite(t *testing.T, repo, rel, content string) {
	t.Helper()
	abs := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// ntSeed writes a baseline graph plus a manifest matching the repo's current
// contents — what makes the next TryIncremental a real incremental run rather
// than a fallback.
func ntSeed(t *testing.T, repo, stateDir string, ents []graph.Entity) {
	t.Helper()
	doc := &graph.Document{
		Version: graph.SchemaVersion, GeneratedAt: time.Now().UTC(), Repo: "test-repo",
		Entities: ents, Stats: graph.Stats{Entities: len(ents)},
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	var paths []string
	_ = filepath.WalkDir(repo, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(repo, path)
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	m := diff.LoadManifest(stateDir)
	diff.UpdateManifest(repo, paths, m)
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
}

// ntEntity builds a baseline graph entity with the same deterministic ID the
// indexer would have given it.
func ntEntity(kind, name, file string) graph.Entity {
	return graph.Entity{
		ID:         graph.EntityID("test-repo", kind, name, file),
		Name:       name,
		Kind:       kind,
		SourceFile: file,
		Language:   "kotlin",
	}
}

// ntIdentities returns the sorted "kind|name" identity set of every entity
// sourced from file.
func ntIdentities(doc *graph.Document, file string) []string {
	var out []string
	for i := range doc.Entities {
		if doc.Entities[i].SourceFile == file {
			out = append(out, doc.Entities[i].Kind+"|"+doc.Entities[i].Name)
		}
	}
	sort.Strings(out)
	return out
}

// TestIncremental_KotlinFile_EntitiesSurviveReExtraction_6151 is the gate.
//
// Before the fix this failed with an EMPTY identity set: Step 5 removed the
// four baseline entities and Step 6 handed the Kotlin extractor a nil tree, so
// it returned (nil, nil) and nothing was re-added — while the run reported
// Done=true with no fallback and no error.
func TestIncremental_KotlinFile_EntitiesSurviveReExtraction_6151(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	ntWrite(t, repo, ntKotlinFile, ntKotlinBefore)
	ntSeed(t, repo, stateDir, []graph.Entity{
		ntEntity("SCOPE.Component", ntKotlinFile, ntKotlinFile),
		ntEntity("SCOPE.Component", "Ledger", ntKotlinFile),
		ntEntity("SCOPE.Operation", "applyCharge", ntKotlinFile),
		ntEntity("SCOPE.Operation", "voidEntry", ntKotlinFile),
	})

	ntWrite(t, repo, ntKotlinFile, ntKotlinAfter)

	res := TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("incremental did not complete: fallback=%q", res.FallbackReason)
	}

	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}

	got := ntIdentities(doc, ntKotlinFile)
	want := []string{
		"SCOPE.Component|" + ntKotlinFile,
		"SCOPE.Component|Ledger",
		"SCOPE.Component|Reconciler",
		"SCOPE.Operation|applyCharge",
		"SCOPE.Operation|settleBatch",
		"SCOPE.Operation|voidEntry",
	}

	// Bidirectional, by content. Missing entries are the #6151 data loss;
	// extra entries are a duplication regression.
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w] = true
	}
	gotSet := map[string]bool{}
	for _, g := range got {
		gotSet[g] = true
	}
	for _, w := range want {
		if !gotSet[w] {
			t.Errorf("MISSING from re-extracted graph: %s", w)
		}
	}
	for _, g := range got {
		if !wantSet[g] {
			t.Errorf("UNEXPECTED entity in re-extracted graph: %s", g)
		}
	}
	if t.Failed() {
		t.Logf("got  = %v", got)
		t.Logf("want = %v", want)
	}
}

// ntStubExtractor stands in for a registered language extractor so the failure
// branches can be driven deterministically.
type ntStubExtractor struct {
	lang     string
	err      error
	panicMsg string
}

func (s *ntStubExtractor) Language() string { return s.lang }

func (s *ntStubExtractor) Extract(_ context.Context, _ extractor.FileInput) ([]types.EntityRecord, error) {
	if s.panicMsg != "" {
		panic(s.panicMsg)
	}
	return nil, s.err
}

// ntSwapExtractor replaces the registered extractor for lang and restores the
// original on cleanup.
func ntSwapExtractor(t *testing.T, lang string, repl Extractor) {
	t.Helper()
	orig, ok := Get(lang)
	if !ok {
		t.Fatalf("no extractor registered for %s — fixture assumption broken", lang)
	}
	Register(lang, repl)
	t.Cleanup(func() { Register(lang, orig) })
}

// ntBaselineRepo builds the standard Kotlin baseline: file on disk, matching
// manifest, four seeded entities.
func ntBaselineRepo(t *testing.T) (repo, stateDir string) {
	t.Helper()
	repo = t.TempDir()
	stateDir = t.TempDir()
	ntWrite(t, repo, ntKotlinFile, ntKotlinBefore)
	ntSeed(t, repo, stateDir, []graph.Entity{
		ntEntity("SCOPE.Component", ntKotlinFile, ntKotlinFile),
		ntEntity("SCOPE.Component", "Ledger", ntKotlinFile),
		ntEntity("SCOPE.Operation", "applyCharge", ntKotlinFile),
		ntEntity("SCOPE.Operation", "voidEntry", ntKotlinFile),
	})
	return repo, stateDir
}

// TestIncremental_ExtractFailureIsNotSwallowed_6151 pins the second half of
// #6151. Step 5 has already evicted the file's entities by the time the
// extractor runs, so an extraction that fails or panics is NOT a "use partial
// results" situation — continuing persists the deletion and reports success.
// Both must fall back to a full reindex with the reason recorded.
func TestIncremental_ExtractFailureIsNotSwallowed_6151(t *testing.T) {
	cases := []struct {
		name string
		stub *ntStubExtractor
	}{
		{"returns error", &ntStubExtractor{lang: "kotlin", err: errors.New("nt synthetic extract failure")}},
		// The panic case additionally pins the safeExtract switch: TryIncremental
		// used to call ext.Extract directly, outside registry.go's recover().
		{"panics", &ntStubExtractor{lang: "kotlin", panicMsg: "nt synthetic panic"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, stateDir := ntBaselineRepo(t)
			ntSwapExtractor(t, "kotlin", tc.stub)
			ntWrite(t, repo, ntKotlinFile, ntKotlinAfter)

			res := TryIncremental(context.Background(), repo, stateDir, nil, nil)
			if res.Done {
				t.Fatalf("a failed extraction reported success — the #6151 swallow is back")
			}
			if !strings.Contains(res.FallbackReason, "extract-error") ||
				!strings.Contains(res.FallbackReason, ntKotlinFile) {
				t.Errorf("fallback reason must name the failure and the file, got %q", res.FallbackReason)
			}
		})
	}
}

// TestIncremental_UnparseableFileFallsBack_6151 pins the paired guard. A file
// whose parse yields no usable tree for a tree-sitter-backed language is
// indistinguishable, from records alone, from a file that genuinely has no
// entities — so the guard checks the PARSE OUTCOME, not just len(records)==0.
func TestIncremental_UnparseableFileFallsBack_6151(t *testing.T) {
	repo, stateDir := ntBaselineRepo(t)
	// Dense syntactic garbage, measured at a 12.8% ERROR-node ratio for the
	// kotlin grammar — over the #5963 10% ceiling, so ParserFactory.Parse
	// returns ErrHighSyntaxErrorRate and a nil tree, which is precisely the
	// state in which all fourteen affected extractors return (nil, nil).
	// (Unbalanced brackets alone are not enough: they measure 7.7%.)
	ntWrite(t, repo, ntKotlinFile,
		"@@@ !!! ### $$$ %%% ^^^ &&& *** ((( ))) --- +++ === ~~~ ```\n@@@ !!! ### $$$\n")

	res := TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if res.Done {
		t.Fatalf("an unparseable file silently kept its entities deleted (Done=true)")
	}
	if !strings.Contains(res.FallbackReason, "no-tree-no-records") {
		t.Errorf("want a no-tree-no-records fallback, got %q", res.FallbackReason)
	}
}

// TestIncremental_EmptyFileDoesNotFallBack_6151 is the counterweight. The guard
// above must not turn every legitimately-empty answer into a full reindex:
// truncating a file to zero bytes is a real edit with a real (empty) result,
// and Parse returns a nil tree with NO error for empty input on both paths.
func TestIncremental_EmptyFileDoesNotFallBack_6151(t *testing.T) {
	repo, stateDir := ntBaselineRepo(t)
	ntWrite(t, repo, ntKotlinFile, "")

	res := TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("emptying a file must not cost a full reindex: fallback=%q", res.FallbackReason)
	}
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	if got := ntIdentities(doc, ntKotlinFile); len(got) != 0 {
		t.Errorf("emptied file must leave no entities behind, got %v", got)
	}
}
