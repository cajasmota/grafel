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
//     reasonml and rescript are offenders this corpus cannot see.
//     The two checks are complements. Neither replaces the other.
//
//     A FILE CAN ALSO BE INVISIBLE FOR A REASON THAT IS NOT THE CORPUS'S.
//     dockerfile was listed here as a third such offender and was NOT one:
//     three fixtures existed and the CLASSIFIER dropped all three, because its
//     basename table held the exact name `Dockerfile` while every fixture used
//     a `*.Dockerfile` / `Dockerfile.<variant>` spelling. #6854 widened the
//     router; all three files reported here as offenders on the next run, and
//     the #6852 arm below fixed them. So a language missing from
//     exercisedLanguages6847 has TWO possible causes and only one of them is a
//     missing fixture — check the classifier before concluding the corpus is
//     short.
//
//   - exercisedLanguages6847 IS HAND-MAINTAINED AND IS NOT DERIVED FROM
//     extractor.List(). Stating that plainly, because a pinned slice that
//     relates to nothing outside itself makes every omission read as
//     deliberate (#6849's pattern). A List()-relative pin is not available:
//     List() returns 445 names, of which ~380 are framework extractors
//     (custom_*, python_*, lua_*) that no classifier language ever dispatches,
//     so "registered minus exercised" is not a meaningful coverage number.
//     What IS pinned against something external is noExtractorLanguages6847
//     below. The unexercised BASE languages, measured, were the 24:
//     assembly avro c commonlisp dockerfile elm erlang haskell hcl idris
//     jsonschema nim ocaml pony racket reasonml rescript scheme sml solidity
//     swift_package systemverilog verilog vhdl — three of which (dockerfile,
//     reasonml, rescript) were confirmed offenders. dockerfile has since left
//     that list: #6854 made its three fixtures classify, so it is exercised,
//     anchoring, and fixed. The remaining two are unchanged.
//
//   - ONE CORPUS DROP IS NOT A MISSING FIXTURE, and dockerfile is the worked
//     example. Its three corpus files — sample.Dockerfile (x2, under
//     testdata/fixtures/dockerfile and .../sources/dockerfile) and
//     real-world/docker/Dockerfile.multi_stage — all classified as
//     lang="" skip=true reason="unsupported_extension" because the
//     classifier's basename table mapped the exact name "Dockerfile" and not
//     the `*.Dockerfile` / `Dockerfile.<variant>` spellings the fixtures use.
//     The extractor was registered and the fixtures existed; the classifier
//     was what the corpus could not cross. Fixed in #6854, landed together with
//     the #6852 dockerfile arm. The ordering constraint runs ONE WAY, stated
//     precisely because an earlier draft here claimed a circularity that does
//     not exist: the CLASSIFIER half cannot land first, since it turns all
//     three fixtures into new offenders and reddens this guard. The carrier
//     half CAN precede it — measured, not reasoned: with the carrier applied
//     and the classifier at its parent, this package is green and the carrier's
//     own unit tests still grade it fully, because they drive Extract with
//     synthetic paths rather than through the router.
//
//   - IT IS PER-FILE. A carrier emitted by some OTHER file's extraction would
//     satisfy the resolver's repo-wide byName index but not this check. No
//     in-tree extractor mints a node named after a file it is not extracting,
//     so the two coincide today; if one ever does, this guard reports it and
//     the entry belongs in the ledger with that reason.
//
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
// none of which called FileEntity or PrependFileCarrier either AT THAT TIME.
// dockerfile now does, and it was never really in this bucket: it turned out to
// have three corpus files the CLASSIFIER was dropping (#6854), so once the
// router was widened it was measured by the walk like any other language rather
// than by a hand-written source. reasonml and rescript are still hand-driven
// findings and still unfixed. So the known
// total is at least FIFTEEN offenders / EIGHTEEN instances counting the three
// #6815 fixed, and the population is unbounded above until every registered
// language is driven. All of it is tracked in #6852. None is fixed here: each
// needs the same per-language judgement #6815 made (conditional carrier vs. one
// orphan node per source file) and each moves the golden fixtures and
// cmd/grafel's whole-graph digest.
//
// THE LEDGER IS AN EXACT SET, NOT A FLOOR, IN BOTH DIRECTIONS. A language not
// in knownMissingCarrier6847 fails this test; so does fixing one that IS in it
// without removing its entry. THE SET SHRANK AS LANGUAGES WERE FIXED — it
// opened at twelve, #6852 removed one per landed arm (bicep first, zig last),
// and it is now EMPTY over this corpus. Read len(knownMissingCarrier6847), not
// a number in prose.
//
// AN EMPTY LEDGER CHANGES WHAT THIS FILE PROVES, and the change is written up
// at the declaration below rather than left implicit: the "still reproduces"
// direction now iterates nothing, so the permissive failure it used to catch —
// a walk that silently stopped detecting anything — is caught by
// TestFileAnchoredCarrier_EveryAnchoringFileCarries_6847 instead, which
// accounts for a non-empty population in the positive direction.
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
// against the extractor package: none of the REMAINING entries calls
// extractor.FileEntity or extractor.PrependFileCarrier anywhere in its
// non-test sources. The set opened at twelve (2026-09-05) and shrinks by one
// per landed #6852 arm — bicep was removed first — so the authoritative count
// is len(knownMissingCarrier6847), never a figure written in a comment.
//
// To REMOVE an entry: fix the extractor (see internal/extractor/file_carrier.go
// for the conditional-carrier shape #6815 and #6518 settled on) and delete the
// line. To ADD one: do not. A new offender is the defect this file exists to
// catch; adding a line to keep it green is the failure mode #6834 names.
// THE SET IS NOW EMPTY, AND THAT CHANGES WHICH ASSERTION IS LOAD-BEARING.
// While it had entries, the second loop of TestFileAnchoredCarrier_NoNewOffender_6847
// ("every known offender still reproduces") doubled as this file's strongest
// anti-vacuity check: a walk that silently stopped detecting anything went RED
// there. Empty, that loop iterates nothing and can no longer fail, so "zero
// offenders" became a conclusion a broken walk reaches just as easily as a
// fixed tree does — the #6908 shape, an anti-vacuity guard relaxed by the very
// change it exists to catch.
//
// The replacement is TestFileAnchoredCarrier_EveryAnchoringFileCarries_6847
// below, which states the SAME invariant in the positive direction over a
// population that is not empty and cannot become empty without failing:
// every one of the anchoringLanguages6847 files must be MATCHED to a carrier,
// per language and per file count. Deleting the detection step from the walk
// now fails there instead of passing here.
var knownMissingCarrier6847 = map[string]string{}

// exercisedLanguages6847 is the exact set of registered languages the corpus
// actually drives. It is pinned so that a corpus file being deleted, renamed
// out of a testdata/ directory, or emptied cannot quietly shrink this guard's
// reach while it keeps reporting success — vacuity layer 2 of #6834 ("did it
// read the RIGHT things"), which a bare count of files cannot establish.
var exercisedLanguages6847 = []string{
	"astro", "bicep", "clojure", "cobol", "commonlisp", "cpp", "crystal",
	"csharp", "css", "dart", "dockerfile", "elixir", "fish", "fsharp", "go",
	"graphql", "groovy", "html", "java", "javascript", "jcl", "just", "kotlin",
	"lua", "markdown", "php", "protobuf", "python", "razor", "ruby", "rust",
	"scala", "shell", "sql", "svelte", "swift", "terraform", "typescript",
	"vbnet", "vue", "yaml", "zig",
}

// anchoringLanguages6847 is the exact set of exercised languages whose corpus
// files actually PRODUCE a path-anchored FromID. This is vacuity layer 3 ("the
// FULL content"): a truncated or gutted corpus file still classifies and still
// extracts, so it would leave exercisedLanguages6847 intact — but the import
// statements would be gone and the language would drop out of this set.
var anchoringLanguages6847 = []string{
	"astro", "bicep", "clojure", "cobol", "cpp", "crystal", "csharp", "dart",
	"dockerfile", "elixir", "fish", "fsharp", "go", "graphql", "groovy",
	"html", "java", "javascript", "just", "kotlin", "lua", "markdown", "php",
	"protobuf", "python", "ruby", "rust", "scala", "shell", "swift",
	"terraform", "typescript", "vue", "yaml", "zig",
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
	carried          map[string]int
	detected         map[string]int
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
		carried:          map[string]int{},
		detected:         map[string]int{},
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
			// PER-FILE POSITIVE CONTROL FOR THE MATCHER, run through the walk's
			// own call rather than beside it. Take the carrier away and the file
			// becomes a genuine offender; if the matcher does NOT report it, the
			// matcher is not detecting anything and the empty offender list
			// below is a no-op that reads like a guard. This is the check that
			// replaces what an entry in knownMissingCarrier6847 used to provide:
			// with the ledger empty, nothing else here fails when the detection
			// step is neutered.
			//
			// THE CARRIER IS UN-NAMED, NOT DELETED, and the distinction is not
			// cosmetic — deleting it was measured and was WRONG. For java,
			// python, typescript, javascript and graphql the record that carries
			// the path is the same record that OWNS the path-anchored edges, so
			// removing it removes the anchor too and the matcher correctly
			// reports nothing. Blanking the two name fields leaves the anchor in
			// place and isolates exactly the property under test.
			stripped := make([]types.EntityRecord, len(recs))
			copy(stripped, recs)
			for i := range stripped {
				if stripped[i].Name == vrel || stripped[i].QualifiedName == vrel {
					stripped[i].Name = ""
					stripped[i].QualifiedName = ""
				}
			}
			if len(pathAnchoredKindsMissingCarrier(vrel, stripped)) > 0 {
				res.detected[cr.Language]++
			}
			// The POSITIVE half of the invariant, derived DIRECTLY from the
			// records and NOT from the offender list below. Deriving it from
			// `kinds` was measured and was wrong: a walk that stopped calling
			// the matcher would then count every anchoring file as carried and
			// report no offenders, so a real regression would produce a green
			// run twice over. Read straight from the records, that same walk
			// mutant leaves this count intact and the regression still fails.
			for i := range recs {
				if recs[i].Name == vrel || recs[i].QualifiedName == vrel {
					res.carried[cr.Language]++
					break
				}
			}
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

// TestFileAnchoredCarrier_EveryAnchoringFileCarries_6847 is the invariant said
// in the POSITIVE direction, and it exists because knownMissingCarrier6847 is
// now empty.
//
// "No offender" is a claim about an empty set, and an empty set is what a walk
// that reads nothing, extracts nothing, or no longer applies the matcher also
// produces. While the ledger had entries, the "still reproduces" loop above
// caught that; empty, it iterates nothing. This test asks instead for a
// NON-EMPTY population to be positively accounted for: for every language whose
// corpus files anchor on their own path, the number of files MATCHED to a
// carrier must equal the number that anchor — per language, per file, not as a
// floor.
//
// It fails on all three of the shapes the empty ledger stopped catching:
// deleting the matcher call, an extractor losing its carrier, and a walk that
// silently stops extracting. It is deliberately NOT derived from
// res.offenders — the count it reads is incremented in the same branch as the
// offender list, so the two cannot disagree about what was measured.
func TestFileAnchoredCarrier_EveryAnchoringFileCarries_6847(t *testing.T) {
	res := carrierScan(t)

	if diff := diffStringSets(anchoringLanguages6847, sortedStringKeys(res.carried)); diff != "" {
		t.Errorf("the set of languages whose anchored corpus files ALL carry changed:\n%s\n"+
			"A language that leaves this set has anchored files with no carrier — the #6815 "+
			"defect class, now reported here as well as in the offender test, because an "+
			"empty knownMissingCarrier6847 can no longer report it by disappearing.", diff)
	}
	total := 0
	for _, lang := range anchoringLanguages6847 {
		total += res.carried[lang]
		if res.carried[lang] != res.anchoring[lang] {
			t.Errorf("language %q: %d corpus files anchor on their own path but only %d carry — "+
				"the %d-file difference is the dangling-FROM-end defect (#6815/#6847), one "+
				"file at a time", lang, res.anchoring[lang], res.carried[lang],
				res.anchoring[lang]-res.carried[lang])
		}
	}
	// A floor on the accounted population, so this test cannot pass by having
	// measured an empty corpus: sets that are equal because both are empty
	// would satisfy every comparison above.
	const minCarriedFiles6847 = 100
	if total < minCarriedFiles6847 {
		t.Errorf("only %d corpus files were positively accounted for, want at least %d — "+
			"below that this test is comparing near-empty sets and the assertion is vacuous",
			total, minCarriedFiles6847)
	}

	// THE MATCHER'S OWN POSITIVE CONTROL, measured inside the walk. Every
	// anchoring file must become a REPORTED offender once its carrier is
	// stripped. Without this, a pathAnchoredKindsMissingCarrier that returned
	// nil unconditionally — or a walk that stopped calling it — would produce
	// an empty offender list, an intact carried count, and a clean run.
	for _, lang := range anchoringLanguages6847 {
		if res.detected[lang] != res.anchoring[lang] {
			t.Errorf("language %q: %d anchoring corpus files, but stripping the carrier made "+
				"only %d of them REPORT as offenders — the detection step is not detecting, so "+
				"an empty offender list is not evidence of anything", lang,
				res.anchoring[lang], res.detected[lang])
		}
	}
}

// TestFileAnchoredCarrier_CorpusCoverage_6847 pins what the walk read. Without
// it, deleting every corpus file for a language would leave
// TestFileAnchoredCarrier_NoNewOffender_6847 green for the wrong reason. That
// used to be softened by the pinned offenders in knownMissingCarrier6847, whose
// disappearance failed the offender test directly — but that set is empty now,
// so nothing about a shrinking corpus is caught there any more.
// The nine remaining #6834 shapes are not otherwise protected, which is why the
// two exact sets below are asserted rather than a file count alone.
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
