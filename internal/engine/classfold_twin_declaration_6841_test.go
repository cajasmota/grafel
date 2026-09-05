package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/cajasmota/grafel/internal/entkinds"
	"github.com/cajasmota/grafel/internal/types"
)

// #6841. FrameworkClassKindPriority's doc used to claim that every bare kind
// appears alongside its "SCOPE."-prefixed twin. Thirteen rows violated it and
// nothing observed the claim, so the claim was cited twice as evidence that a
// pair exists and was wrong both times.
//
// The claim is now per-row and lives in DATA (FrameworkClassKindTwins), not in
// prose. These tests are what makes the data load-bearing.
//
// THE SCAN IS internal/entkinds, NOT A LOCAL MATCHER. The first version of
// this guard rolled its own walk over internal/ and cmd/ looking for
// double-quoted spellings. That was blind to the DOMINANT producer of exactly
// these kinds: rule YAML writes them as UNQUOTED scalars (`entity_type:
// Controller`, ×37 for Controller alone), so a rule pack that started minting
// `SCOPE.Repository` left "Repository: TwinUnproduced" green — measured ALIVE.
// entkinds.ScanRuleYAML reads those scalars and entkinds.ScanGoReferences
// resolves `string(types.EntityKindX)` conversions, which a literal scan also
// misses; both holes are the ones that package was built for (#6744, #6818).

// cfRepoRoot is the tree the scans read. runtime.Caller pins it to THIS source
// file's directory, so the answer cannot depend on the working directory — and
// it is the worktree that compiled this test, never a sibling checkout.
func cfRepoRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("scan root %s has no go.mod: %v", root, err)
	}
	if _, err := os.Stat(filepath.Join(root, "internal", "engine", "classfold.go")); err != nil {
		t.Fatalf("scan root %s is not this tree: %v", root, err)
	}
	return root
}

var (
	cfDeclOnce sync.Once
	cfDeclRes  entkinds.Result
	cfDeclErr  error
	cfRefOnce  sync.Once
	cfRefRes   entkinds.Result
	cfRefErr   error
)

// cfDeclarations is every entity kind the tree DECLARES — Go composite-literal
// `Kind:` fields plus rule-YAML `entity_type:`/`entity_mapping:` scalars.
func cfDeclarations(t *testing.T) entkinds.Result {
	t.Helper()
	cfDeclOnce.Do(func() { cfDeclRes, cfDeclErr = entkinds.Scan(cfRepoRoot(t)) })
	if cfDeclErr != nil {
		t.Fatalf("entkinds.Scan: %v", cfDeclErr)
	}
	return cfDeclRes
}

// cfGoMentions is every Go MENTION — producer or consumer — of an unpaired
// row's opposite spelling. Unlike Scan it sees a kind passed as a function
// ARGUMENT (makeEntity(name, "SCOPE.Interface", …)) and a
// `string(types.EntityKindPlugin)` conversion, which is where all three
// produced twins actually live.
func cfGoMentions(t *testing.T) entkinds.Result {
	t.Helper()
	cfRefOnce.Do(func() {
		cfRefRes, cfRefErr = entkinds.ScanGoReferences(cfRepoRoot(t), cfUnpairedOpposites())
	})
	if cfRefErr != nil {
		t.Fatalf("entkinds.ScanGoReferences: %v", cfRefErr)
	}
	return cfRefRes
}

// cfOpposite is the spelling a twin declaration is about, mirrored here rather
// than imported so the test does not inherit a bug in the helper it grades.
func cfOpposite(kind string) string {
	if rest, ok := strings.CutPrefix(kind, "SCOPE."); ok {
		return rest
	}
	return "SCOPE." + kind
}

// cfUnpairedOpposites is the opposite spelling of every row NOT declared
// TwinInMap. The paired rows are excluded on purpose: their opposite is in the
// map, so its mentions are not evidence of anything and enumerating every
// mention of "SCOPE.Model" would bury the rows this issue is about.
func cfUnpairedOpposites() []string {
	out := make([]string, 0, len(FrameworkClassKindTwins))
	for k, d := range FrameworkClassKindTwins {
		if d.State != TwinInMap {
			out = append(out, cfOpposite(k))
		}
	}
	sort.Strings(out)
	return out
}

// cfFileMentions reports whether a repo-relative file contains lit. The file is
// read WHOLE and the length asserted against os.Stat on the string the matcher
// reads — a truncation between read and match would otherwise answer "no"
// convincingly.
func cfFileMentions(t *testing.T, rel, lit string) bool {
	t.Helper()
	p := filepath.Join(cfRepoRoot(t), filepath.FromSlash(rel))
	st, err := os.Stat(p)
	if err != nil {
		t.Errorf("producer %s: %v", rel, err)
		return false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Errorf("producer %s: %v", rel, err)
		return false
	}
	s := string(b)
	if int64(len(s)) != st.Size() {
		t.Errorf("%s: matching %d of %d bytes — a truncated read hides the literal", rel, len(s), st.Size())
		return false
	}
	return strings.Contains(s, lit)
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
// exactly what produced the thirteen — cannot pass either direction.
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
// spelling". types.IsValidEntityKind is the enum that decides what a Go
// producer is allowed to emit, so an enum addition turns the declaration false
// and this fires.
//
// It is NOT sufficient on its own: the rule layer mints kinds that were never
// in the enum (that is #6744's whole finding), which is what the scan below is
// for.
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

// The scan's own health, as a local canary. Its own weakening is NOT observed
// by another test here — neutering the floors below is a measured-ALIVE mutant
// — and that is deliberate rather than overlooked: what actually anchors the
// walk is internal/entkinds' own TestRepoSweepIsNotVacuous, which requires the
// scan to have parsed EXACTLY the files an independent walk of the repository
// finds. That is a derived floor, not a magic number, and it lives in the
// package that owns the scanner.
//
// What this adds locally is the FIND-CONTROLS, which a file count cannot give:
// a kind the Go half must see in quantity, and a kind only the YAML half can
// see. If either half stops resolving while still parsing every file, one of
// these goes to zero.
func TestClassKindTwins6841_ScanReadsBothHalvesOfTheTree(t *testing.T) {
	res := cfDeclarations(t)
	if res.GoFilesParsed < 1000 {
		t.Errorf("Go half parsed %d files — too few to be this repository", res.GoFilesParsed)
	}
	if res.YAMLFilesParsed < 200 {
		t.Errorf("YAML half parsed %d files — too few to be this rule tree", res.YAMLFilesParsed)
	}
	// Go find-control: a kind declared by Go composite literals all over the
	// extractor tree. Zero here means the Go half is not resolving.
	if n := len(res.SitesFor("SCOPE.Component")); n < 50 {
		t.Errorf(`Go find-control: %d declaration sites for "SCOPE.Component", want >= 50`, n)
	}
	// YAML find-control, and the one that matters for #6841: "Controller" is
	// declared ONLY as an unquoted rule-YAML scalar. A scan that reads Go but
	// not YAML — the exact hole this guard's first version had — reports zero.
	ctrl := res.SitesFor("Controller")
	if len(ctrl) < 10 {
		t.Errorf(`YAML find-control: %d declaration sites for "Controller", want >= 10 (rule YAML unread?)`, len(ctrl))
	}
	yaml := 0
	for _, s := range ctrl {
		if s.Origin == entkinds.OriginRuleYAML {
			yaml++
		}
	}
	if yaml == 0 {
		t.Errorf(`YAML find-control: none of the %d "Controller" sites came from rule YAML`, len(ctrl))
	}

	refs := cfGoMentions(t)
	if refs.GoFilesParsed < 1000 {
		t.Errorf("reference scan parsed %d Go files — too few to be this repository", refs.GoFilesParsed)
	}
}

// The claim TwinUnproduced actually makes: NOTHING IN THE TREE DECLARES the
// opposite spelling. entkinds.Scan is both halves — Go composite-literal
// `Kind:` fields and rule-YAML `entity_type:`/`entity_mapping:` scalars — so a
// rule pack that starts minting `entity_type: SCOPE.Repository` fails here.
// That is the failure this mechanism exists to prevent, in the surface where it
// is most likely: the rule tree declares Controller ×37, Middleware ×29,
// Task ×11, Implementation ×6, Plugin ×5 and Interface ×4 today.
func TestClassKindTwins6841_UnproducedTwinIsDeclaredNowhere(t *testing.T) {
	res := cfDeclarations(t)
	checked := 0
	for _, k := range cfSortedKeys(FrameworkClassKindTwins) {
		if FrameworkClassKindTwins[k].State != TwinUnproduced {
			continue
		}
		checked++
		op := cfOpposite(k)
		for _, s := range res.SitesFor(op) {
			t.Errorf("%q is declared TwinUnproduced, but %s declares it: %s. If that site EMITS the kind the "+
				"row is no longer unpaired-by-vacuity and the fold needs a decision (#6841)", k, s.File, s)
		}
	}
	if checked == 0 {
		t.Fatal("no TwinUnproduced rows were checked — the loop selected nothing")
	}
}

// Every Go MENTION of an unpaired row's opposite spelling must be accounted
// for by that row — as a Producer (it mints the kind) or a KnownSite (it only
// reads one). Both directions: an undeclared mention fails, and a declared
// file the spelling has left fails as stale.
//
// This is what holds the Produced* rows up. entkinds.ScanGoReferences resolves
// `string(types.EntityKindPlugin)` and a kind passed as a function argument,
// so deleting internal/engine/plugin_system_edges.go's emission — or Scala's
// SCOPE.Interface — now fails here. A double-quote matcher saw the argument
// form and missed the conversion entirely.
//
// classfold.go is itself in the scanned tree, so writing one of these spellings
// as a bare Go literal HERE also has to be declared. That is the intended
// reading: a row added to FrameworkClassKindPriority is a mention like any
// other. It does not catch the Why/NotClassShaped prose, which mentions the
// spellings inside longer strings — ScanGoReferences matches whole string
// values, not substrings.
func TestClassKindTwins6841_EveryGoMentionOfAnUnpairedTwinIsDeclared(t *testing.T) {
	refs := cfGoMentions(t)
	rows := 0
	for _, k := range cfSortedKeys(FrameworkClassKindTwins) {
		d := FrameworkClassKindTwins[k]
		if d.State == TwinInMap {
			continue
		}
		rows++
		op := cfOpposite(k)
		declared := map[string]bool{}
		for _, f := range d.Producers {
			declared[f] = true
		}
		for _, f := range d.KnownSites {
			declared[f] = true
		}
		seen := map[string]bool{}
		for _, s := range refs.SitesFor(op) {
			seen[s.File] = true
			if !declared[s.File] {
				t.Errorf("%q is mentioned at %s:%d, which %q declares neither as a Producer nor a KnownSite. "+
					"If that file EMITS the kind it is a Producer and the state may be wrong; if it only reads "+
					"one, add it to KnownSites (#6841)", op, s.File, s.Line, k)
			}
		}
		for f := range declared {
			if !seen[f] {
				t.Errorf("%q declares %s for %q, but the reference scan finds no mention of it there — "+
					"stale declaration, or the producer was deleted", k, f, op)
			}
		}
	}
	if rows == 0 {
		t.Fatal("no unpaired rows were checked — the loop selected nothing")
	}
}

// Each state's evidence must be COMPLETE, so that flipping a state is not a
// one-character edit that nothing notices. These fields pin that the
// classification was STATED, not that it is true; truth rests on the reading
// recorded in Why. FoldSourceSubtype is not decoration — the exhibit below
// folds with it.
func TestClassKindTwins6841_EachStateCarriesItsRequiredEvidence(t *testing.T) {
	states := map[ClassKindTwinState]int{}
	for _, k := range cfSortedKeys(FrameworkClassKindTwins) {
		d := FrameworkClassKindTwins[k]
		states[d.State]++
		switch d.State {
		case TwinInMap:
			if len(d.Producers) != 0 || len(d.KnownSites) != 0 {
				t.Errorf("%q is TwinInMap and needs no evidence files, but names some", k)
			}
		case TwinUnproduced:
			if len(d.Producers) != 0 {
				t.Errorf("%q is TwinUnproduced but names producers %v — nothing emits the spelling, so "+
					"there is nothing to produce it", k, d.Producers)
			}
			if d.FoldSourceSubtype != "" || d.NotClassShaped != "" {
				t.Errorf("%q is TwinUnproduced: nothing emits the spelling, so it can be neither "+
					"class-shaped nor not-class-shaped", k)
			}
		case TwinProducedNonClass:
			if len(d.Producers) == 0 {
				t.Errorf("%q is TwinProducedNonClass but names no producer — the claim that something "+
					"emits %q is unbacked", k, cfOpposite(k))
			}
			if d.NotClassShaped == "" {
				t.Errorf("%q is TwinProducedNonClass but does not say WHY the emitted records are never "+
					"class-shaped. That reason is the whole difference from TwinProducedClassLike, which "+
					"means there is a live fold miss", k)
			}
			if d.FoldSourceSubtype != "" {
				t.Errorf("%q is TwinProducedNonClass but declares FoldSourceSubtype %q", k, d.FoldSourceSubtype)
			}
		case TwinProducedClassLike:
			if len(d.Producers) == 0 {
				t.Errorf("%q is TwinProducedClassLike but names no producer", k)
			}
			if !ClassLikeComponentSubtypes[d.FoldSourceSubtype] {
				t.Errorf("%q is TwinProducedClassLike with FoldSourceSubtype %q, which is not in "+
					"ClassLikeComponentSubtypes — then the paired SCOPE.Component is not a fold source and "+
					"there is no miss to declare", k, d.FoldSourceSubtype)
			}
			if d.NotClassShaped != "" {
				t.Errorf("%q is TwinProducedClassLike but also says it is not class-shaped: %q", k, d.NotClassShaped)
			}
			// The subtype is a claim about what the PRODUCER emits, so it is
			// checked against the producer, not just against the allowlist:
			// any class-like subtype would satisfy the check above.
			for _, rel := range d.Producers {
				if !cfFileMentions(t, rel, `"`+d.FoldSourceSubtype+`"`) {
					t.Errorf("%q declares FoldSourceSubtype %q, but %s never writes that subtype literal — "+
						"the exhibit folds a record shape the producer does not emit", k, d.FoldSourceSubtype, rel)
				}
			}
		}
	}
	for _, s := range []ClassKindTwinState{TwinInMap, TwinUnproduced, TwinProducedNonClass, TwinProducedClassLike} {
		if states[s] == 0 {
			t.Errorf("no row declares %v — this test grades every state and one was never exercised", s)
		}
	}
}

// The cost of a TwinProducedClassLike row, EXHIBITED rather than asserted in
// prose. #6841 recorded that no failing case had ever been shown for a missing
// twin; this is it. A Scala `trait Repo` reaches the fold twice — the language
// AST's SCOPE.Component/trait (a fold source, "trait" is in
// ClassLikeComponentSubtypes) and internal/custom/scala/type_system.go's
// SCOPE.Interface. Because SCOPE.Interface is not a survivor candidate the pair
// does NOT fold, leaving two nodes for one class — the #1613 invariant this
// table exists to enforce.
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
		sub := FrameworkClassKindTwins[k].FoldSourceSubtype
		twin := cfOpposite(k)
		generic := types.EntityRecord{
			Kind: "SCOPE.Component", Subtype: sub, Name: "Repo",
			SourceFile: "a.scala", Language: "scala", StartLine: 3,
		}
		if !IsClassFoldSource(&generic) {
			t.Fatalf("%q: SCOPE.Component subtype %q is not a fold source, so this exhibit proves nothing", k, sub)
		}
		typed := types.EntityRecord{
			Kind: twin, Subtype: sub, Name: "Repo",
			SourceFile: "a.scala", Language: "scala", StartLine: 3,
		}
		out, folded := FoldFrameworkClassKinds([]types.EntityRecord{generic, typed}, nil)
		if folded != 0 {
			t.Errorf("%s: folded=%d, want 0 — the unmapped twin became a survivor, so the declared gap is closed "+
				"and FrameworkClassKindTwins[%q] must stop saying TwinProducedClassLike", twin, folded, k)
		}
		cfAssertRows(t, cfRows(out), []string{
			"SCOPE.Component|Repo|" + sub + "|a.scala",
			twin + "|Repo|" + sub + "|a.scala",
		})

		// The bare spelling is the control: same records, same fold, one node.
		// It is what makes the two-node result above a property of the MISSING
		// ROW rather than of the record shape.
		typed.Kind = k
		outBare, foldedBare := FoldFrameworkClassKinds([]types.EntityRecord{generic, typed}, nil)
		if foldedBare != 1 {
			t.Errorf("%s (bare control): folded=%d, want 1", k, foldedBare)
		}
		cfAssertRows(t, cfRows(outBare), []string{k + "|Repo|" + sub + "|a.scala"})
	}
}

// FrameworkClassKindCanonRank is only ever consulted to break a tie between two
// rows of FrameworkClassKindPriority, so a rank for a kind that is not in that
// map can never be read.
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

// #6841 asked whether the sibling map carries the same defect. It does not, and
// this is the sentence in FrameworkClassKindCanonRank's doc that says so, made
// observable: every row CanonRank leaves unpaired is a row FrameworkClassKindTwins
// declares unpaired. Dropping "SCOPE.Model" from CanonRank while "Model" stays
// makes the doc false, and this is what notices.
//
// The containment is one-way on purpose: Twins declares TestClass, Plugin,
// Implementation, Interface and Task unpaired and CanonRank does not rank them
// at all, which is not a pairing statement.
func TestClassKindTwins6841_CanonRankUnpairedRowsAreDeclaredUnpaired(t *testing.T) {
	checked := 0
	for _, k := range cfSortedKeys(FrameworkClassKindCanonRank) {
		if _, paired := FrameworkClassKindCanonRank[cfOpposite(k)]; paired {
			continue
		}
		checked++
		d, ok := FrameworkClassKindTwins[k]
		if !ok {
			continue // reported by DeclaresEveryPriorityRowAndOnlyThose
		}
		if d.State == TwinInMap {
			t.Errorf("FrameworkClassKindCanonRank ranks %q without %q, but FrameworkClassKindTwins declares "+
				"that pair TwinInMap — the two maps now disagree about the same pair, which is the claim in "+
				"FrameworkClassKindCanonRank's doc (#6841)", k, cfOpposite(k))
		}
	}
	if checked == 0 {
		t.Fatal("no unpaired CanonRank rows were checked — the loop selected nothing")
	}
}
