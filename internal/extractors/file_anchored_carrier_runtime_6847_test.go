// Package extractors — file_anchored_carrier_runtime_6847_test.go
//
// Issue #6847. A RUNTIME guard for the invariant #6815 fixed by hand in three
// languages: EVERY relationship whose FromID is the source file's own path must
// have some emitted record carrying that exact path as its Name or
// QualifiedName. internal/resolve/refs.go has no path→entity index, so a
// path-valued FromID resolves if and only if such a node exists; when none
// does, the raw path reaches the graph as the edge's FROM end and the edge
// points at nothing.
//
// WHY A SECOND GUARD, NEXT TO file_anchored_rels_guard_test.go. That file is a
// SOURCE SCAN, and it deliberately skips IMPORTS (the #120 convention). Every
// instance of this defect found so far — solidity, verilog, astro, svelte,
// erlang, nim, groovy — was an IMPORTS edge, so the one guard aimed at the
// class cannot see the kind that carries it. This file sidesteps the question
// entirely: it never asks what KIND an edge is. It asks only whether the FROM
// end resolves, which is the actual invariant, and it answers by RUNNING the
// registered extractors rather than by reading their source.
//
// WHAT #120 ACTUALLY SAYS, since the skip is justified by it. #120 is "Java
// cross-file receiver binding"; the convention lives in its closing comment:
// "Source-file-path FromIDs (every IMPORTS edge across every language) tagged
// Dynamic instead of bug-extractor". That is a decision about a METRIC — such
// an edge stops counting as an extractor bug — not a statement that the edge
// resolves. It does not. #6815 treated exactly this shape as a defect and fixed
// it in three languages, so the repo's own position is that a dangling
// path-anchored IMPORTS is a bug whose metric is merely suppressed. Nothing in
// #120 licenses skipping the invariant checked here.
//
// WHAT THIS COSTS AND WHAT IT CANNOT SEE, stated rather than implied:
//
//   - COVERAGE IS CORPUS-BOUND, AND THE GAP IS REAL. The corpus is every file
//     under a testdata/ directory in this repo, classified by the production
//     classifier and parsed by the production parser factory. A registered
//     language with no corpus file is NOT measured, and a new extractor is
//     therefore NOT automatically covered. That is the opposite of the
//     neighbouring source scan's property, and the source scan's stated reason
//     for preferring a derive-shaped check — that a corpus "grows only when
//     someone remembers to grow it" — is VINDICATED here, not superseded:
//     dockerfile, reasonml and rescript are offenders this corpus cannot see.
//     The two checks are complements. Neither replaces the other.
//   - exercisedLanguages6847 IS HAND-MAINTAINED AND IS NOT DERIVED FROM
//     extractor.List(). Stating that plainly, because a pinned slice that
//     relates to nothing outside itself makes every omission read as
//     deliberate (#6849's pattern). A List()-relative pin is not available:
//     List() returns 445 names, of which ~380 are framework extractors
//     (custom_*, python_*, lua_*) that no classifier language ever dispatches,
//     so "registered minus exercised" is not a meaningful coverage number.
//     What IS pinned against something external is noExtractorLanguages6847
//     below. The unexercised BASE languages, measured, are the 24:
//     assembly avro c commonlisp dockerfile elm erlang haskell hcl idris
//     jsonschema nim ocaml pony racket reasonml rescript scheme sml solidity
//     swift_package systemverilog verilog vhdl — three of which (dockerfile,
//     reasonml, rescript) are confirmed offenders.
//   - ONE CORPUS DROP IS NOT A MISSING FIXTURE. dockerfile has three corpus
//     files and reaches the extractor with NONE of them: sample.Dockerfile
//     (x2, under testdata/fixtures/dockerfile and .../sources/dockerfile) and
//     real-world/docker/Dockerfile.multi_stage all classify as
//     lang="" skip=true reason="unsupported_extension" — the classifier's
//     basename table maps the exact name "Dockerfile", not the
//     `*.Dockerfile` / `Dockerfile.<variant>` spellings the fixtures use. The
//     extractor is registered and the fixtures exist; the classifier is what
//     the corpus cannot cross. Filed separately as a classifier question.
//   - IT IS PER-FILE. A carrier emitted by some OTHER file's extraction would
//     satisfy the resolver's repo-wide byName index but not this check. No
//     in-tree extractor mints a node named after a file it is not extracting,
//     so the two coincide today; if one ever does, this guard reports it and
//     the entry belongs in the ledger with that reason.
//   - IT RUNS ONE PASS. Pass 2.5 engine rules and the custom-extractor
//     dispatch are not driven here, so a carrier minted by a later pass is not
//     seen. Same disposition as above: report, then justify in the ledger.
//
// MEASURED STATE WHEN THIS GUARD LANDED (2026-09-05, before any fix). The
// invariant does NOT hold today. Twelve registered languages violate it AS
// MEASURED BY THIS CORPUS — all on IMPORTS, and none of them emitting a file
// carrier at all:
//
//	astro bicep clojure cobol crystal dart fsharp html lua shell terraform zig
//
// TWELVE IS A LOWER BOUND, NOT A POPULATION. The count is corpus-bound, and the
// bound is not theoretical: driving the registered languages this corpus never
// reaches from hand-written minimal sources found THREE MORE, confirmed against
// the source the same way —
//
//	dockerfile (dockerfile/dockerfile.go:347 `FromID: file.Path`)
//	reasonml   (reasonml/extractor.go:325)
//	rescript   (rescript/extractor.go:296)
//
// none of which calls FileEntity or PrependFileCarrier either. So the known
// total is at least FIFTEEN offenders / EIGHTEEN instances counting the three
// #6815 fixed, and the population is unbounded above until every registered
// language is driven. All of it is tracked in #6852. None is fixed here: each
// needs the same per-language judgement #6815 made (conditional carrier vs. one
// orphan node per source file) and each moves the golden fixtures and
// cmd/grafel's whole-graph digest.
//
// THE LEDGER IS AN EXACT SET, NOT A FLOOR, IN BOTH DIRECTIONS. A thirteenth
// language fails this test; so does fixing one of the twelve without removing
// its entry. A guard that only checked "no NEW offender" would be blind to the
// permissive direction — a walk that silently stopped detecting anything would
// stay green under a floor and goes red here.
package extractors_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/types"
)

// knownMissingCarrier6847 is the exact set of registered languages measured to
// emit a path-anchored FromID with no record carrying that path. Every entry
// was produced by the walk below, not by reading source, and each was confirmed
// against the extractor package: none of the twelve calls extractor.FileEntity
// or extractor.PrependFileCarrier anywhere in its non-test sources.
//
// To REMOVE an entry: fix the extractor (see internal/extractor/file_carrier.go
// for the conditional-carrier shape #6815 and #6518 settled on) and delete the
// line. To ADD one: do not. A new offender is the defect this file exists to
// catch; adding a line to keep it green is the failure mode #6834 names.
var knownMissingCarrier6847 = map[string]string{
	"astro":     "extractor.go:328 `FromID: filePath` on IMPORTS; no carrier emitted",
	"bicep":     "extractor.go:296 `FromID: path` on IMPORTS; `.bicep` is also absent from refs.go sourceFileExtensions, so it counts as bug-extractor rather than Dynamic",
	"clojure":   "path-anchored IMPORTS; no carrier emitted",
	"cobol":     "path-anchored IMPORTS (COPY members); no carrier emitted",
	"crystal":   "extractor.go:161 `FromID: filePath` on IMPORTS; no carrier emitted",
	"dart":      "path-anchored IMPORTS; no carrier emitted",
	"fsharp":    "extractor.go:673 `FromID: filePath` on IMPORTS; no carrier emitted",
	"html":      "path-anchored IMPORTS (script/link refs); no carrier emitted",
	"lua":       "lua.go:540 `FromID: file.Path` on IMPORTS; no carrier emitted",
	"shell":     "shell.go:260 `FromID: file.Path` on IMPORTS (source/.); no carrier emitted",
	"terraform": "hcl/relationships.go:160 and :180 `FromID: path` on IMPORTS; no carrier emitted",
	"zig":       "zig.go:272 `FromID: filePath` on IMPORTS; no carrier emitted",
}

// exercisedLanguages6847 is the exact set of registered languages the corpus
// actually drives. It is pinned so that a corpus file being deleted, renamed
// out of a testdata/ directory, or emptied cannot quietly shrink this guard's
// reach while it keeps reporting success — vacuity layer 2 of #6834 ("did it
// read the RIGHT things"), which a bare count of files cannot establish.
var exercisedLanguages6847 = []string{
	"astro", "bicep", "clojure", "cobol", "commonlisp", "cpp", "crystal",
	"csharp", "css", "dart", "elixir", "fish", "fsharp", "go", "graphql",
	"groovy", "html", "java", "javascript", "jcl", "just", "kotlin", "lua",
	"markdown", "php", "protobuf", "python", "razor", "ruby", "rust", "scala",
	"shell", "sql", "svelte", "swift", "terraform", "typescript", "vbnet",
	"vue", "yaml", "zig",
}

// anchoringLanguages6847 is the exact set of exercised languages whose corpus
// files actually PRODUCE a path-anchored FromID. This is vacuity layer 3 ("the
// FULL content"): a truncated or gutted corpus file still classifies and still
// extracts, so it would leave exercisedLanguages6847 intact — but the import
// statements would be gone and the language would drop out of this set.
var anchoringLanguages6847 = []string{
	"astro", "bicep", "clojure", "cobol", "cpp", "crystal", "csharp", "dart",
	"elixir", "fish", "fsharp", "go", "graphql", "groovy", "html", "java",
	"javascript", "just", "kotlin", "lua", "markdown", "php", "protobuf",
	"python", "ruby", "rust", "scala", "shell", "swift", "terraform",
	"typescript", "vue", "yaml", "zig",
}

// noExtractorLanguages6847 is the exact set of languages the classifier DOES
// produce over this corpus but for which no extractor is registered, so the
// walk drops the file before the invariant can be checked. It is pinned to
// relate the coverage sets to something OUTSIDE themselves: registering an
// extractor for either language moves it into scope and this pin goes red,
// which is the moment to add it to exercisedLanguages6847 rather than to
// discover months later that it was never covered.
var noExtractorLanguages6847 = []string{"objective_c", "prisma"}

// minCorpusFiles6847 is a floor on the number of candidate files discovered.
// A floor is vacuity layer 1 ONLY — it establishes that the walk read
// something, and nothing more. The two exact sets above are what establish
// WHAT it read; neither is derivable from this number.
//
// MEASURED, not asserted: gutting this constant to 0 is a mutant that SURVIVES
// the whole suite. That is the honest grade of a floor and the reason it is not
// load-bearing here. The corpus-shrink mutant (repo-root testdata/ dropped from
// the walk) and the content-truncation mutant (every file cut to 120 bytes) are
// both killed — by exercisedLanguages6847 and anchoringLanguages6847
// respectively, not by this number.
const minCorpusFiles6847 = 700

// pathAnchoredKindsMissingCarrier returns the sorted, de-duplicated
// relationship kinds in records whose FromID is exactly path, when NO record in
// records carries path as its Name or QualifiedName. It returns nil when the
// carrier exists, when nothing is anchored on path, or when path is empty.
//
// This is the whole invariant, in one function, deliberately independent of
// relationship kind — that independence is what lets it see the IMPORTS edges
// the #120 convention hides from the source scan.
func pathAnchoredKindsMissingCarrier(path string, records []types.EntityRecord) []string {
	if path == "" {
		return nil
	}
	seen := map[string]bool{}
	for i := range records {
		if records[i].Name == path || records[i].QualifiedName == path {
			return nil
		}
		for j := range records[i].Relationships {
			if records[i].Relationships[j].FromID == path {
				seen[records[i].Relationships[j].Kind] = true
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// carrierFinding is one (language, path) pair the walk reports.
type carrierFinding struct {
	lang  string
	path  string
	kinds []string
}

// carrierScanResult is everything one walk produces.
//
// The two tests that consume it go through carrierScan, which memoises the walk
// in a sync.Once, so they observe ONE traversal and cannot disagree about what
// was measured. The doc comment here previously asserted that property while
// the code performed two independent traversals with no caching at all — the
// claim is now made true rather than merely written down.
type carrierScanResult struct {
	candidates       int
	exercised        map[string]int
	anchoring        map[string]int
	noExtractorLangs map[string]int
	offenders        []carrierFinding
}

var (
	carrierScanOnce sync.Once
	carrierScanVal  carrierScanResult
	carrierScanErr  error
)

// carrierScan returns the single memoised corpus walk.
func carrierScan(t *testing.T) carrierScanResult {
	t.Helper()
	carrierScanOnce.Do(func() {
		carrierScanVal, carrierScanErr = scanCorpusForMissingCarriers()
	})
	if carrierScanErr != nil {
		t.Fatalf("corpus scan failed: %v", carrierScanErr)
	}
	return carrierScanVal
}

// scanCorpusForMissingCarriers drives every corpus file through the production
// classifier, the production parser factory and the production extractor
// dispatch, then applies pathAnchoredKindsMissingCarrier to the records.
//
// The corpus path is rewritten from `.../testdata/...` to `.../src/...` before
// classification: classifier.depDirs skips `/testdata/` outright, so the
// unrewritten path classifies as Skip and the walk would exercise almost
// nothing. Path is rewritten consistently everywhere — the value handed to the
// extractor is the value the anchor and the carrier are compared against — so
// the rewrite cannot manufacture or mask a finding.
func scanCorpusForMissingCarriers() (carrierScanResult, error) {
	root, err := filepath.Abs("../..")
	if err != nil {
		return carrierScanResult{}, fmt.Errorf("repo root: %w", err)
	}
	var files []string
	if err := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		if !strings.Contains("/"+filepath.ToSlash(rel), "/testdata/") {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		return carrierScanResult{}, fmt.Errorf("walk %s: %w", root, err)
	}
	sort.Strings(files)

	res := carrierScanResult{
		candidates:       len(files),
		exercised:        map[string]int{},
		anchoring:        map[string]int{},
		noExtractorLangs: map[string]int{},
	}
	cls := classifier.New(nil)
	pf := treesitter.NewParserFactory(nil)
	ctx := context.Background()

	for _, rel := range files {
		st, statErr := os.Stat(filepath.Join(root, rel))
		if statErr != nil {
			continue
		}
		vrel := strings.TrimPrefix(strings.ReplaceAll("/"+rel, "/testdata/", "/src/"), "/")
		cr := cls.ClassifyWithSize(ctx, vrel, st.Size())
		if cr.Skip || cr.Language == "" {
			continue
		}
		if _, ok := extractors.Get(cr.Language); !ok {
			res.noExtractorLangs[cr.Language]++
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(root, rel))
		if readErr != nil {
			continue
		}
		in := extractors.FileInput{
			Path:     vrel,
			Content:  content,
			Language: cr.Language,
			RepoRoot: root,
		}
		if pr, perr := pf.Parse(ctx, content, cr.Language); perr == nil && pr != nil {
			in.TSTree = pr.TSTree
		}
		recs, extErr := extractors.Extract(ctx, in)
		if extErr != nil {
			continue
		}
		res.exercised[cr.Language]++
		anchored := false
		for i := range recs {
			for j := range recs[i].Relationships {
				if recs[i].Relationships[j].FromID == vrel {
					anchored = true
				}
			}
		}
		if anchored {
			res.anchoring[cr.Language]++
		}
		if kinds := pathAnchoredKindsMissingCarrier(vrel, recs); len(kinds) > 0 {
			res.offenders = append(res.offenders, carrierFinding{lang: cr.Language, path: vrel, kinds: kinds})
		}
	}
	return res, nil
}

// TestFileAnchoredCarrier_NoNewOffender_6847 is the ratchet. The set of
// languages whose extractors leave a path-anchored FromID with no carrier must
// equal knownMissingCarrier6847 EXACTLY.
func TestFileAnchoredCarrier_NoNewOffender_6847(t *testing.T) {
	res := carrierScan(t)

	got := map[string][]string{}
	for _, f := range res.offenders {
		got[f.lang] = append(got[f.lang], fmt.Sprintf("%s kinds=%v", f.path, f.kinds))
	}
	for _, lang := range sortedStringKeys(got) {
		if _, known := knownMissingCarrier6847[lang]; !known {
			t.Errorf("NEW path-anchored FromID with no file carrier — language %q:\n  %s\n"+
				"This is the #6815 defect class: internal/resolve/refs.go has no path→entity "+
				"index, so this edge's FROM end resolves to nothing and the raw path reaches "+
				"the graph. Emit a file carrier (internal/extractor/file_carrier.go — "+
				"PrependFileCarrier is the conditional form, FileEntity the unconditional one). "+
				"Do NOT add an entry to knownMissingCarrier6847 to make this green.",
				lang, strings.Join(got[lang], "\n  "))
		}
	}
	for _, lang := range sortedStringKeys(knownMissingCarrier6847) {
		if _, still := got[lang]; !still {
			t.Errorf("knownMissingCarrier6847[%q] no longer reproduces (%s).\n"+
				"Either the extractor was fixed — delete the entry — or this guard stopped "+
				"detecting, which is the failure this exact-set comparison exists to catch.",
				lang, knownMissingCarrier6847[lang])
		}
	}
}

// TestFileAnchoredCarrier_CorpusCoverage_6847 pins what the walk read. Without
// it, deleting every corpus file for a language would leave
// TestFileAnchoredCarrier_NoNewOffender_6847 green for the wrong reason —
// except for the twelve pinned offenders, whose disappearance already fails it.
// The nine remaining #6834 shapes are not so protected, which is why the two
// exact sets below are asserted rather than a file count alone.
func TestFileAnchoredCarrier_CorpusCoverage_6847(t *testing.T) {
	res := carrierScan(t)

	if res.candidates < minCorpusFiles6847 {
		t.Errorf("corpus shrank: %d candidate files under testdata/, want >= %d. "+
			"This floor establishes only that the walk read SOMETHING; the two set "+
			"comparisons below are what establish that it read the right things.",
			res.candidates, minCorpusFiles6847)
	}
	if diff := diffStringSets(exercisedLanguages6847, sortedStringKeys(res.exercised)); diff != "" {
		t.Errorf("the set of languages this guard actually exercises changed:\n%s\n"+
			"A language that leaves this set is no longer covered AT ALL by #6847, "+
			"whatever the offender test reports.", diff)
	}
	if diff := diffStringSets(noExtractorLanguages6847, sortedStringKeys(res.noExtractorLangs)); diff != "" {
		t.Errorf("the set of corpus languages with NO registered extractor changed:\n%s\n"+
			"A language leaving this set means an extractor was registered for it — add it "+
			"to exercisedLanguages6847 (and to anchoringLanguages6847 if its corpus files "+
			"anchor). A language ENTERING it means corpus files are now being dropped "+
			"before the invariant is checked.", diff)
	}
	if diff := diffStringSets(anchoringLanguages6847, sortedStringKeys(res.anchoring)); diff != "" {
		t.Errorf("the set of languages whose corpus files produce a path-anchored FromID "+
			"changed:\n%s\nA language that leaves this set is exercised but no longer "+
			"exercises the INVARIANT — its corpus file has lost the import statements the "+
			"check needs, and the guard is vacuous for it.", diff)
	}
}

// TestPathAnchoredKindsMissingCarrier_Detects_6847 is the positive control for
// the matcher itself (#6834 layer 4). Each case varies exactly ONE axis against
// the first; everything else — the path, the anchoring relationship, the kind —
// is held constant.
func TestPathAnchoredKindsMissingCarrier_Detects_6847(t *testing.T) {
	const p = "pkg/mod/thing.zig"
	anchoredRel := []types.RelationshipRecord{{FromID: p, ToID: "std", Kind: "IMPORTS"}}

	cases := []struct {
		name string
		// varies: what the axis under test changes relative to the base case
		varies  string
		path    string
		records []types.EntityRecord
		want    []string
	}{{
		name:   "base: anchored edge, no record named after the path",
		varies: "nothing — this is the base case",
		path:   p,
		records: []types.EntityRecord{
			{Name: "std", QualifiedName: "std", Kind: "SCOPE.Component", Relationships: anchoredRel},
		},
		want: []string{"IMPORTS"},
	}, {
		name:   "carrier by Name suppresses the finding",
		varies: "ONE extra record whose Name is the path; same path, same anchored edge",
		path:   p,
		records: []types.EntityRecord{
			{Name: p, QualifiedName: p, Kind: "SCOPE.Component", Subtype: "file"},
			{Name: "std", QualifiedName: "std", Kind: "SCOPE.Component", Relationships: anchoredRel},
		},
		want: nil,
	}, {
		name:   "carrier by QualifiedName alone suppresses the finding",
		varies: "the carrier's Name is NOT the path — only its QualifiedName is",
		path:   p,
		records: []types.EntityRecord{
			{Name: "thing", QualifiedName: p, Kind: "SCOPE.Component", Subtype: "file"},
			{Name: "std", QualifiedName: "std", Kind: "SCOPE.Component", Relationships: anchoredRel},
		},
		want: nil,
	}, {
		name:   "a carrier named after a DIFFERENT path does not count",
		varies: "the carrier's path is a sibling file's, not this one's",
		path:   p,
		records: []types.EntityRecord{
			{Name: "pkg/mod/other.zig", QualifiedName: "pkg/mod/other.zig", Kind: "SCOPE.Component", Subtype: "file"},
			{Name: "std", QualifiedName: "std", Kind: "SCOPE.Component", Relationships: anchoredRel},
		},
		want: []string{"IMPORTS"},
	}, {
		name:   "no anchored edge, no finding",
		varies: "the single relationship's FromID is a structural ref, not the path",
		path:   p,
		records: []types.EntityRecord{
			{Name: "std", Kind: "SCOPE.Component", Relationships: []types.RelationshipRecord{
				{FromID: "scope:module:" + p, ToID: "std", Kind: "IMPORTS"},
			}},
		},
		want: nil,
	}, {
		name:   "the check is kind-blind: a non-IMPORTS anchor reports too",
		varies: "the anchored edge's Kind, IMPORTS -> DEPENDS_ON",
		path:   p,
		records: []types.EntityRecord{
			{Name: "std", Kind: "SCOPE.Component", Relationships: []types.RelationshipRecord{
				{FromID: p, ToID: "std", Kind: "DEPENDS_ON"},
			}},
		},
		want: []string{"DEPENDS_ON"},
	}, {
		name:   "several kinds on one path are all reported, sorted",
		varies: "a second anchored edge of a different kind on the same path",
		path:   p,
		records: []types.EntityRecord{
			{Name: "std", Kind: "SCOPE.Component", Relationships: []types.RelationshipRecord{
				{FromID: p, ToID: "std", Kind: "IMPORTS"},
				{FromID: p, ToID: "b", Kind: "DEPENDS_ON"},
			}},
		},
		want: []string{"DEPENDS_ON", "IMPORTS"},
	}, {
		name: "an empty path is not an anchor",
		varies: "path is \"\" and the edge's FromID is \"\" — the graph-assembly " +
			"substitution case. The record carries a NON-EMPTY QualifiedName on " +
			"purpose: with QualifiedName \"\" the carrier branch matches \"\" == path " +
			"and returns nil first, so the case would pass for a reason unrelated to " +
			"its name and the `path == \"\"` clause it exists to grade would be free " +
			"to be deleted. That is the shape file_carrier.go calls out as clause 1, " +
			"\"tested separately and not folded into clause 2\"; folded, it grades " +
			"nothing.",
		path: "",
		records: []types.EntityRecord{
			{Name: "std", QualifiedName: "std", Kind: "SCOPE.Component", Relationships: []types.RelationshipRecord{
				{FromID: "", ToID: "std", Kind: "IMPORTS"},
			}},
		},
		want: nil,
	}, {
		name:    "no records at all",
		varies:  "the record slice is empty",
		path:    p,
		records: nil,
		want:    nil,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pathAnchoredKindsMissingCarrier(tc.path, tc.records)
			// Exact ORDERED comparison, not a set diff: the "several kinds"
			// case asserts sortedness, and a set comparison would let the sort
			// be deleted while the fixture's label kept claiming it.
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("varies: %s\n  want %v\n  got  %v", tc.varies, tc.want, got)
			}
		})
	}
}

func sortedStringKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// diffStringSets returns "" when want and got hold the same elements, and a
// human-readable missing/unexpected report otherwise.
func diffStringSets(want, got []string) string {
	w := map[string]bool{}
	for _, s := range want {
		w[s] = true
	}
	g := map[string]bool{}
	for _, s := range got {
		g[s] = true
	}
	var missing, extra []string
	for s := range w {
		if !g[s] {
			missing = append(missing, s)
		}
	}
	for s := range g {
		if !w[s] {
			extra = append(extra, s)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return fmt.Sprintf("  missing (expected, not observed): %v\n  unexpected (observed, not expected): %v", missing, extra)
}
