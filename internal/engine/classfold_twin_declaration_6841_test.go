package engine

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6841. FrameworkClassKindPriority's doc used to claim that every bare kind
// appears alongside its "SCOPE."-prefixed twin. Eleven rows violated it and
// nothing observed the claim, so the claim was cited twice as evidence that a
// pair exists and was wrong both times.
//
// The claim is now per-row and lives in DATA (FrameworkClassKindTwins), not in
// prose. These tests are what makes the data load-bearing: a row added to the
// priority map with no declared twin state fails, a declaration that disagrees
// with the map fails, and a declaration that says "nothing emits the opposite
// spelling" fails the moment the enum gains it.

// cfOpposite is the spelling the twin declaration is about, mirrored here
// rather than imported so the test does not inherit a bug in the helper it is
// grading.
func cfOpposite(kind string) string {
	if rest, ok := strings.CutPrefix(kind, "SCOPE."); ok {
		return rest
	}
	return "SCOPE." + kind
}

func cfSortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Layer 1+2 of #6834's vacuity ladder for the DECLARATION itself: every row of
// the map the fold actually consults must have a twin state, and no
// declaration may name a row that is not in that map. A one-sided addition —
// exactly what produced the eleven — cannot pass either direction.
func TestClassKindTwins6841_DeclaresEveryPriorityRowAndOnlyThose(t *testing.T) {
	for _, k := range cfSortedKeys(FrameworkClassKindPriority) {
		if _, ok := FrameworkClassKindTwins[k]; !ok {
			t.Errorf("FrameworkClassKindPriority has row %q with no FrameworkClassKindTwins declaration: "+
				"say whether its %q twin is in the map, unproduced, or produced-but-unmapped (#6841)", k, cfOpposite(k))
		}
	}
	for _, k := range cfSortedKeys(FrameworkClassKindTwins) {
		if _, ok := FrameworkClassKindPriority[k]; !ok {
			t.Errorf("FrameworkClassKindTwins declares %q, which is not a row of FrameworkClassKindPriority "+
				"(stale declaration — the fold never consults this kind)", k)
		}
	}
	if len(FrameworkClassKindTwins) == 0 {
		t.Fatal("FrameworkClassKindTwins is empty — the declaration set is the whole guard")
	}
}

// The state must agree with the map the fold reads. This is the call-site link
// (#6834 layer 5): the declaration is graded against FrameworkClassKindPriority
// itself, not against a restatement of it.
func TestClassKindTwins6841_StateAgreesWithTheMap(t *testing.T) {
	for _, k := range cfSortedKeys(FrameworkClassKindTwins) {
		_, twinPresent := FrameworkClassKindPriority[cfOpposite(k)]
		declaredPresent := FrameworkClassKindTwins[k].State == TwinInMap
		if twinPresent != declaredPresent {
			t.Errorf("%q: declared state %v says twin-in-map=%v, but FrameworkClassKindPriority[%q] present=%v",
				k, FrameworkClassKindTwins[k].State, declaredPresent, cfOpposite(k), twinPresent)
		}
	}
}

// TwinUnproduced asserts a negative — "nothing can emit the opposite
// spelling". types.IsValidEntityKind is the enum that decides what a producer
// is allowed to emit, so an enum addition turns the declaration false and this
// fires. That is the whole point: an accidental one-sided addition becomes
// distinguishable from a deliberate one.
func TestClassKindTwins6841_UnproducedTwinIsNotAValidEntityKind(t *testing.T) {
	checked := 0
	for _, k := range cfSortedKeys(FrameworkClassKindTwins) {
		if FrameworkClassKindTwins[k].State != TwinUnproduced {
			continue
		}
		checked++
		if types.IsValidEntityKind(cfOpposite(k)) {
			t.Errorf("%q is declared TwinUnproduced, but %q is now a valid entity kind — a producer may emit it, "+
				"so the fold needs a row for it or the declaration needs to change (#6841)", k, cfOpposite(k))
		}
	}
	if checked == 0 {
		t.Fatal("no TwinUnproduced rows were checked — the loop selected nothing")
	}
}

// A Produced* declaration names the files that mint the opposite spelling.
// Grading it: the anchor list itself must be non-empty (emptying it is a
// mutation this must catch), every named file must be read WHOLE (length
// checked against os.Stat, #6834's truncation caution), and the exact spelling
// must appear in it as a Go string literal.
func TestClassKindTwins6841_ProducedTwinFilesActuallyMintTheSpelling(t *testing.T) {
	root := filepath.Join("..", "..")
	checked := 0
	for _, k := range cfSortedKeys(FrameworkClassKindTwins) {
		d := FrameworkClassKindTwins[k]
		if d.State != TwinProducedNonClass && d.State != TwinProducedClassLike {
			continue
		}
		if len(d.Producers) == 0 {
			t.Errorf("%q is declared %v but names no producer file — the claim that something emits %q is unbacked",
				k, d.State, cfOpposite(k))
			continue
		}
		for _, rel := range d.Producers {
			checked++
			p := filepath.Join(root, filepath.FromSlash(rel))
			st, err := os.Stat(p)
			if err != nil {
				t.Errorf("%q names producer %s: %v", k, rel, err)
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				t.Errorf("%q names producer %s: %v", k, rel, err)
				continue
			}
			s := string(b)
			if int64(len(s)) != st.Size() {
				t.Errorf("%s: matching %d bytes of a %d-byte file — a truncated read would make the spelling "+
					"check pass or fail for the wrong reason", rel, len(s), st.Size())
				continue
			}
			if !strings.Contains(s, `"`+cfOpposite(k)+`"`) {
				t.Errorf("%q is declared %v with producer %s, but that file contains no %q string literal — "+
					"the producer moved or was renamed and the declaration went stale", k, d.State, rel, cfOpposite(k))
			}
		}
	}
	if checked == 0 {
		t.Fatal("no producer files were checked — the loop selected nothing")
	}
}

// cfScanRoots are the trees a kind spelling can be minted in. Deliberately NOT
// the repo root: .claude/worktrees/ holds full checkouts and a walk from there
// would read other branches' copies of these very files.
var cfScanRoots = []string{"internal", "cmd"}

// cfSpellingSites walks cfScanRoots and returns every non-test source file that
// contains any of the wanted spellings as a Go/YAML string literal, plus the
// number of files and bytes it read (the caller grades those: a walk that read
// nothing must not read as "no occurrences").
func cfSpellingSites(t *testing.T, wanted []string) (map[string][]string, int, int64) {
	t.Helper()
	sites := make(map[string][]string, len(wanted))
	files, bytesRead := 0, int64(0)
	for _, root := range cfScanRoots {
		p := filepath.Join("..", "..", root)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("scan root %s: %v", root, err)
		}
		err := filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			ext := filepath.Ext(name)
			if ext != ".go" && ext != ".yaml" && ext != ".yml" && ext != ".json" {
				return nil
			}
			if strings.HasSuffix(name, "_test.go") {
				return nil
			}
			// classfold.go names every spelling in the declarations
			// themselves; counting it would make each row cite itself.
			if filepath.ToSlash(path) == "../../internal/engine/classfold.go" {
				return nil
			}
			st, serr := os.Stat(path)
			if serr != nil {
				return serr
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			s := string(b)
			// Length is asserted on the string the matcher actually reads,
			// not on the byte slice: a truncation between read and match
			// would otherwise slip past a check on b alone (#6834 layer 3).
			if int64(len(s)) != st.Size() {
				t.Errorf("%s: matching %d of %d bytes — a truncated read hides occurrences", path, len(s), st.Size())
				return nil
			}
			files++
			bytesRead += int64(len(s))
			for _, w := range wanted {
				if strings.Contains(s, `"`+w+`"`) {
					rel := strings.TrimPrefix(filepath.ToSlash(path), "../../")
					sites[w] = append(sites[w], rel)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return sites, files, bytesRead
}

// TwinUnproduced asserts a negative about the WHOLE TREE, so the guard has to
// read the whole tree. Every file mentioning the opposite spelling must be
// declared as a known consumer site; an undeclared one is either a new producer
// (the declaration is now false) or a new consumer nobody classified.
//
// This is the check that distinguishes "deliberately unpaired" from "drifted":
// downgrading a Produced* row to TwinUnproduced fails here even when the
// spelling is outside types.AllEntityKinds, which is exactly the state
// "SCOPE.Middleware" and "SCOPE.Interface" are in today.
func TestClassKindTwins6841_UnproducedTwinHasNoUndeclaredSite(t *testing.T) {
	wanted := make([]string, 0, len(FrameworkClassKindTwins))
	declared := make(map[string]map[string]bool, len(FrameworkClassKindTwins))
	for _, k := range cfSortedKeys(FrameworkClassKindTwins) {
		if FrameworkClassKindTwins[k].State != TwinUnproduced {
			continue
		}
		op := cfOpposite(k)
		wanted = append(wanted, op)
		declared[op] = make(map[string]bool)
		for _, s := range FrameworkClassKindTwins[k].KnownSites {
			declared[op][s] = true
		}
	}
	if len(wanted) == 0 {
		t.Fatal("no TwinUnproduced rows — this test grades that state and selected nothing")
	}

	sites, files, bytesRead := cfSpellingSites(t, wanted)
	// Grade the scan itself (#6834 layer 1): a walk that read nothing would
	// report every spelling as absent and pass.
	if files < 500 || bytesRead < 1<<20 {
		t.Fatalf("scan read %d files / %d bytes — too little to have covered internal/ and cmd/", files, bytesRead)
	}
	// Grade the scan's ability to FIND things (layer 4) against a spelling
	// that is certainly present in the trees walked.
	if control, _, _ := cfSpellingSites(t, []string{"SCOPE.Component"}); len(control["SCOPE.Component"]) < 10 {
		t.Fatalf(`scan found "SCOPE.Component" in %d files — the matcher is not working`, len(control["SCOPE.Component"]))
	}

	for _, op := range wanted {
		for _, f := range sites[op] {
			if !declared[op][f] {
				t.Errorf("%q occurs in %s, which is not a declared KnownSite. If that file EMITS the kind, the "+
					"row is no longer TwinUnproduced and the fold needs a decision; if it only reads it, add the "+
					"file to KnownSites with a note saying so (#6841)", op, f)
			}
		}
		for f := range declared[op] {
			found := false
			for _, g := range sites[op] {
				if g == f {
					found = true
				}
			}
			if !found {
				t.Errorf("%q declares KnownSite %s, but the spelling no longer occurs there — stale declaration", op, f)
			}
		}
	}
}

// Positive control for the matcher above (#6834 layer 4): the containment test
// must be able to both find and miss the spelling. Without this a matcher that
// always returns true would pass the guard.
func TestClassKindTwins6841_SpellingMatcherDetects(t *testing.T) {
	hit := "ent := makeEntity(name, \"SCOPE.Interface\", \"trait\", file.Path)"
	miss := "ent := makeEntity(name, \"SCOPE.Component\", \"trait\", file.Path)"
	if !strings.Contains(hit, `"SCOPE.Interface"`) {
		t.Error("matcher failed to detect a spelling that is present")
	}
	if strings.Contains(miss, `"SCOPE.Interface"`) {
		t.Error("matcher detected a spelling that is absent")
	}
}

// The cost of a TwinProducedClassLike row, EXHIBITED rather than asserted in
// prose. #6841 recorded that no failing case had ever been shown for a missing
// twin; this is it. A Scala `trait Repo` reaches the fold twice — the language
// AST's SCOPE.Component/trait (a fold source, "trait" is in
// ClassLikeComponentSubtypes) and internal/custom/scala/type_system.go's
// SCOPE.Interface. Because SCOPE.Interface is not a survivor candidate the
// pair does NOT fold, leaving two nodes for one class — the #1613 invariant
// this table exists to enforce.
//
// This pins the CURRENT behaviour, deliberately. Adding "SCOPE.Interface" to
// the priority map would fix the node count but make the survivor carry a kind
// types.IsValidEntityKind rejects while deleting the valid SCOPE.Component;
// the fix belongs at the producer or in the enum, not here. When it lands,
// this test fails and says so.
func TestClassKindTwins6841_ProducedClassLikeTwinMissesTheFold(t *testing.T) {
	classLike := make([]string, 0, 2)
	for _, k := range cfSortedKeys(FrameworkClassKindTwins) {
		if FrameworkClassKindTwins[k].State == TwinProducedClassLike {
			classLike = append(classLike, k)
		}
	}
	if len(classLike) == 0 {
		t.Fatal("no TwinProducedClassLike row declared — this test grades that state and selected nothing")
	}
	for _, k := range classLike {
		twin := cfOpposite(k)
		generic := types.EntityRecord{
			Kind: "SCOPE.Component", Subtype: "trait", Name: "Repo",
			SourceFile: "a.scala", Language: "scala", StartLine: 3,
		}
		typed := types.EntityRecord{
			Kind: twin, Subtype: "trait", Name: "Repo",
			SourceFile: "a.scala", Language: "scala", StartLine: 3,
		}
		out, folded := FoldFrameworkClassKinds([]types.EntityRecord{generic, typed}, nil)
		if folded != 0 {
			t.Errorf("%s: folded=%d, want 0 — the unmapped twin became a survivor, so the declared gap is closed "+
				"and FrameworkClassKindTwins[%q] must stop saying TwinProducedClassLike", twin, folded, k)
		}
		cfAssertRows(t, cfRows(out), []string{
			"SCOPE.Component|Repo|trait|a.scala",
			twin + "|Repo|trait|a.scala",
		})

		// The bare spelling is the control: same records, same fold, one node.
		// It is what makes the two-node result above a property of the MISSING
		// ROW rather than of the record shape.
		typed.Kind = k
		outBare, foldedBare := FoldFrameworkClassKinds([]types.EntityRecord{generic, typed}, nil)
		if foldedBare != 1 {
			t.Errorf("%s (bare control): folded=%d, want 1", k, foldedBare)
		}
		cfAssertRows(t, cfRows(outBare), []string{k + "|Repo|trait|a.scala"})
	}
}

// FrameworkClassKindCanonRank is only ever consulted to break a tie between
// two rows of FrameworkClassKindPriority, so a rank for a kind that is not in
// that map can never be read. #6841 asked whether the sibling map has the same
// unpaired shape: it does, on exactly the rows the priority map declares
// unpaired, which this keeps true.
func TestClassKindTwins6841_CanonRankRanksOnlyPriorityRows(t *testing.T) {
	if len(FrameworkClassKindCanonRank) == 0 {
		t.Fatal("FrameworkClassKindCanonRank is empty")
	}
	for _, k := range cfSortedKeys(FrameworkClassKindCanonRank) {
		if _, ok := FrameworkClassKindPriority[k]; !ok {
			t.Errorf("FrameworkClassKindCanonRank ranks %q, which is not a survivor candidate "+
				"(absent from FrameworkClassKindPriority) — the rank is unreachable", k)
		}
	}
}
