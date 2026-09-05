package entkinds_test

// enum_member_corresponds_6818_test.go — the guard that an EntityKind enum
// member corresponds to something outside the enum (#6818).
//
// # The hole
//
// Every guard on the entity vocabulary before this one checked the enum against
// ITSELF, or against a set that an addition has already joined:
//
//   - TestNoUndeclaredRuleEntityKinds walks RULE DECLARATIONS and checks they
//     are accounted for. An enum member that declares nothing is not a rule
//     declaration, so it is never visited.
//   - TestEntityKindDeclarationsMatchAllEntityKindsExactly compares the constant
//     block with AllEntityKinds() — an addition updates both.
//   - pinnedEntityKindVocabulary compares a roster with the enum — likewise.
//   - The #6744 / #6776 ledgers count kinds that are NOT in the enum.
//
// So `EntityKindBogus6818 EntityKind = "Bogus6818"`, added to the constant
// block, to AllEntityKinds(), to the vocabulary pin and to both declaration
// counts, was green across internal/types, internal/entkinds and
// internal/graph/fbwriter. A MISSPELLING of a real kind was always caught (the
// real rule declaration then goes unaccounted-for); an ADDITION WITH NO
// COUNTERPART was not.
//
// # The question this guard asks, stated because the answer depends on it
//
// It asks: DOES ANY REFERENCE TO THIS KIND STRING EXIST OUTSIDE THE PACKAGE
// THAT DECLARES THE ENUM? Producer or consumer, write side or read side, Go or
// rule YAML — this guard does not distinguish them and does not claim to.
//
// That is a deliberate retreat from "does a PRODUCER emit this", and #6818
// records why in two independent ways:
//
//   - A reviewer and an implementer disagreed about seven kinds and were wrong
//     in OPPOSITE directions — one over-counted producers, the other reported
//     kinds as absent when they were live CONSUMER strings. Each had searched
//     half the space without saying which half. Naming the question is the
//     cheapest thing that stops a third repeat.
//   - "Producer" is not soundly answerable from source here. entkinds.ScanGo
//     reads the `Kind:` field of an Entity / EntityRecord composite literal and
//     is blind to `e.Kind = "X"`, to `emitEntity(id, someKind, ...)` — the shape
//     that hid a third SCOPE.ScheduledJob producer through #6776 arm B4 — and to
//     any other struct. A guard asking "producer" over that scanner reports
//     kinds as producer-less when they are merely invisible, and clause (c)
//     becomes a dumping ground for scanner limitations. Measured, that is not a
//     hypothetical: asking "producer or rule declaration" over the live tree
//     leaves 17 enum members unaccounted-for, of which only one is genuinely
//     enum-only. Sixteen allow-list rows would have been scanner blind spots
//     wearing a stated reason.
//
// # What this guard therefore does NOT assert
//
// It does not assert that every enum member is EMITTED. A kind read by a
// consumer and written by nobody passes here. That is out of this guard's reach
// and is not silently claimed: see #6776's runtime entity-kind counter (arm A)
// for the measurement that can answer it.
//
// # Why entkinds.ScanGo is not a fourth clause
//
// Because it would be graded by nothing: every (FILE, KIND) PAIR ScanGo
// resolves is also a Go reference, so a producer clause could never be the ONLY
// clause accepting a kind, and deleting it would change no verdict. Two guards
// that only ever fire together are indistinguishable from one guard.
//
// That subset is a measurement, not an argument. Review proved the first
// version of this paragraph FALSE — the reference scan pruned every selector
// whose qualifier was not a bare identifier, so three (file, kind) pairs ScanGo
// resolved were invisible to it and no verdict moved, which is precisely why
// nothing caught it. The scan is fixed and the claim is now observed by
// TestEnumEntityKinds6818_EveryScanGoFileKindPairIsAlsoAGoReference. If that test ever
// goes red, this paragraph is wrong again and a producer clause may be earning
// its keep. The three clauses below were each checked
// for the opposite property and each has inputs it alone accepts: 3 kinds are
// accepted by the rule-YAML clause alone, 66 by the Go-reference clause alone,
// and 1 by the allow-list alone.
//
// # Which way the scan errs
//
// ScanGoReferences resolves SOURCE values only, so a kind mentioned only
// through a run-time-assembled string is not seen. The error direction is
// therefore a spurious RED naming a real enum member, never a silent green for
// a fabricated one — the right way round for a guard whose passing condition is
// "referenced".

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/entkinds"
	"github.com/cajasmota/grafel/internal/types"
)

// enumDeclaringPackage is the source prefix whose references DO NOT COUNT: the
// package that declares the enum. kinds.go declares every kind string as a
// literal and AllEntityKinds() names every constant, so counting internal/types
// as a reference would make the guard accept every kind including a fabricated
// one — i.e. would restore exactly the hole #6818 reports.
//
// The whole package is excluded rather than only kinds.go: "the enum is
// corroborated from outside the package that declares it" is the stricter
// reading, and the narrower one would let a fabricated constant be vouched for
// by a single mention in a neighbouring file of the same package — #6818's hole
// one file over. That difference is graded by
// TestEnumEntityKinds6818_TheWholeDeclaringPackageIsExcluded, which also carries
// the positive control that the two readings are distinguishable on this tree
// at all; it was ungraded prose until review scored the narrowing mutant
// ALIVE.
const enumDeclaringPackage = "internal/types/"

// enumOnlyEntityKinds is clause (c): enum members that are legitimately
// referenced NOWHERE outside internal/types, with the reason each is kept.
//
// A row here is a decision, not a silence. TestEnumOnlyEntityKinds6818_...
// DoesNotRot deletes the excuse the moment the kind gains a reference, so the
// list cannot outlive its reasons.
var enumOnlyEntityKinds = map[string]string{
	"SCOPE.ScopeUnknown": "the published catch-all. internal/mcp/SCHEMA.md:1063 documents it as " +
		"\"catch-all when extractor cannot classify\", so IsValidEntityKind must accept it or a " +
		"stored graph carrying it — and an agent querying for it against the documented " +
		"vocabulary — is rejected. It has never had a producer: the only commit that ever " +
		"introduced the string outside internal/types is 76747f67c (PORT-1), and there it " +
		"appears solely in extractor-test tables that ENUMERATE the valid vocabulary, plus one " +
		"prose comment at internal/coverage/reachability.go:102 listing it among synthetic " +
		"kinds. Markdown is not scanned by either clause, which is why documentation alone " +
		"cannot corroborate a kind and this row exists. Retiring it is a KindVocabularyVersion " +
		"bump plus a SCHEMA.md edit, not a test change.",
}

// enumOnlyEntityKindsMax pins the list's EXACT size, the ratchet mechanism
// #6744 established: an author who trips the sweep must not be able to silence
// it by appending one line, and one who removes a row must lower the pin in the
// same change so the bar is never left slack.
const enumOnlyEntityKindsMax = 1

// enumEntityKindStrings returns every kind string types.AllEntityKinds() carries.
func enumEntityKindStrings() []string {
	all := types.AllEntityKinds()
	out := make([]string, 0, len(all))
	for _, k := range all {
		out = append(out, string(k))
	}
	sort.Strings(out)
	return out
}

// corroboration is one enum member's evidence, clause by clause.
type corroboration struct {
	ruleYAML []string // clause (a) — "file:line" of each rule-pack declaration
	goRef    []string // clause (b) — "file:line" of each Go reference outside internal/types
	// clause (c) — the row's STATED REASON, empty both when the kind is not
	// allow-listed and when its row states no reason. The two are deliberately
	// the same value: an allow-list row is an excuse, and an excuse with
	// nothing written on it is not one.
	allowedBy string
}

// corroborateEnumKinds runs both scans over the live tree and returns, per enum
// member, the evidence each clause found. It fails the test rather than
// returning if a scan read too little to mean anything: a walk that reaches no
// files produces no sites, and "no evidence anywhere" would then be reported as
// a vocabulary full of fabrications.
//
// Those two floors are a FAIL-SAFE, and nothing grades them. They cannot fire
// against the live tree — lowering one to zero was scored as a mutant and
// stayed ALIVE — so they are recorded here as unobserved rather than counted as
// coverage. What they buy is a named failure if the walk is ever narrowed to
// nothing, instead of a green sweep over an empty scan.
func corroborateEnumKinds(t *testing.T) map[string]corroboration {
	t.Helper()
	root := repoRoot(t)
	kinds := enumEntityKindStrings()

	yamlRes, err := entkinds.ScanRuleYAML(root)
	if err != nil {
		t.Fatalf("ScanRuleYAML: %v", err)
	}
	refRes, err := entkinds.ScanGoReferences(root, kinds)
	if err != nil {
		t.Fatalf("ScanGoReferences: %v", err)
	}
	if yamlRes.YAMLFilesParsed < 200 {
		t.Fatalf("the rule-YAML scan read only %d files; clause (a) cannot mean anything on a "+
			"walk that reached nothing", yamlRes.YAMLFilesParsed)
	}
	if refRes.GoFilesParsed < 500 {
		t.Fatalf("the Go reference scan read only %d files; clause (b) cannot mean anything on a "+
			"walk that reached nothing", refRes.GoFilesParsed)
	}

	out := map[string]corroboration{}
	for _, k := range kinds {
		out[k] = corroboration{allowedBy: enumOnlyEntityKinds[k]}
	}
	for _, s := range yamlRes.Sites {
		c, tracked := out[s.Kind]
		if !tracked {
			continue // a rule-declared kind outside the enum is #6744's ledger, not this guard's
		}
		c.ruleYAML = append(c.ruleYAML, fmt.Sprintf("%s:%d", s.File, s.Line))
		out[s.Kind] = c
	}
	for _, s := range refRes.Sites {
		if strings.HasPrefix(s.File, enumDeclaringPackage) {
			continue
		}
		c, tracked := out[s.Kind]
		if !tracked {
			continue
		}
		c.goRef = append(c.goRef, fmt.Sprintf("%s:%d", s.File, s.Line))
		out[s.Kind] = c
	}
	return out
}

// TestEveryEnumEntityKind6818_CorrespondsToSomethingOutsideTheEnum is the
// sweep. Every member of types.AllEntityKinds() must be (a) declared by a rule
// pack, (b) referenced by Go source outside internal/types, or (c) on
// enumOnlyEntityKinds with a stated reason.
//
// The name says "outside the enum", not "has a producer", and the body observes
// exactly that: see the file header on which question is being asked.
func TestEveryEnumEntityKind6818_CorrespondsToSomethingOutsideTheEnum(t *testing.T) {
	ev := corroborateEnumKinds(t)

	var orphans []string
	for _, k := range enumEntityKindStrings() {
		c := ev[k]
		if len(c.ruleYAML) > 0 || len(c.goRef) > 0 || c.allowedBy != "" {
			continue
		}
		orphans = append(orphans, k)
	}
	if len(orphans) > 0 {
		t.Errorf("%d entity kind(s) in types.AllEntityKinds() correspond to NOTHING outside "+
			"internal/types:\n\n  %s\n\n"+
			"THE QUESTION THIS GUARD ASKS is \"is this kind string REFERENCED — by a rule pack "+
			"(entkinds.ScanRuleYAML) or by any non-test Go source outside %q "+
			"(entkinds.ScanGoReferences) — at all?\". It is NOT \"does a producer emit this\": "+
			"entkinds.ScanGo cannot answer that soundly (it reads composite-literal Kind: fields "+
			"and misses assignment and function-argument producers), and a consumer-only kind is "+
			"legitimately live. See this file's header.\n\n"+
			"If you have just ADDED a kind: it is not corroborated by anything. Declare it in a "+
			"rule pack, write or read it from Go, or — if it is legitimately enum-only — add it "+
			"to enumOnlyEntityKinds WITH A REASON and raise enumOnlyEntityKindsMax in the same "+
			"change.\n"+
			"If a kind LOST its last reference: that is the finding. Retire the kind (which is a "+
			"KindVocabularyVersion bump, see internal/types/kinds.go) rather than allow-listing "+
			"a kind nothing uses.",
			len(orphans), strings.Join(orphans, "\n  "), enumDeclaringPackage)
	}
}

// TestEnumEntityKinds6818_EveryScanGoFileKindPairIsAlsoAGoReference is the measurement
// behind this file's reason for NOT making entkinds.ScanGo a fourth clause.
//
// The argument is that a producer clause would be graded by nothing, because
// every composite-literal `Kind:` value ScanGo resolves is also a MENTION and
// so is already accepted by clause (b). That is a subset claim about two
// scanners, and review proved the first version of it FALSE: ScanGoReferences
// pruned every selector whose qualifier was not a bare identifier, so
// `graph.Entity{Kind: "SCOPE.X"}.WithProperties(p)` — 40 sites in 22 files —
// was invisible to it, and three (file, kind) pairs ScanGo resolved were not
// references at all. No verdict changed, which is exactly why nothing caught
// it.
//
// So the claim is observed here rather than argued. It is a claim about SITES,
// not about kinds: a kind whose only reference is a second producer would
// satisfy a kind-level subset check while the site-level one still found the
// gap.
func TestEnumEntityKinds6818_EveryScanGoFileKindPairIsAlsoAGoReference(t *testing.T) {
	root := repoRoot(t)
	prod, err := entkinds.ScanGo(root)
	if err != nil {
		t.Fatalf("ScanGo: %v", err)
	}
	if len(prod.Sites) == 0 {
		t.Fatal("ScanGo found no sites at all; the subset below would hold vacuously")
	}
	refs, err := entkinds.ScanGoReferences(root, prod.Kinds())
	if err != nil {
		t.Fatalf("ScanGoReferences: %v", err)
	}
	seen := map[string]bool{}
	for _, s := range refs.Sites {
		seen[s.File+"\x00"+s.Kind] = true
	}
	var missing []string
	for _, s := range prod.Sites {
		if !seen[s.File+"\x00"+s.Kind] {
			missing = append(missing, fmt.Sprintf("%s at %s:%d", s.Kind, s.File, s.Line))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d (file, kind) pair(s) that ScanGo resolves are NOT reported by "+
			"ScanGoReferences:\n  %s\n\n"+
			"This file's header says a producer clause would be graded by nothing because every "+
			"(file, kind) pair ScanGo resolves is also a reference. That is only true while this "+
			"passes. If "+
			"the reference scan has a new blind spot, widen it; if the gap is deliberate, the "+
			"header's argument has to change with it — a producer clause may now be earning its "+
			"keep.",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestEnumEntityKinds6818_TheWholeDeclaringPackageIsExcluded observes the
// choice enumDeclaringPackage makes, which is otherwise prose: references are
// discounted for ALL of internal/types, not only for kinds.go.
//
// The narrower reading is live-fire dangerous rather than merely untidy. Under
// it, a fabricated constant in kinds.go plus one mention from any other file of
// the same package corroborates itself — the #6818 hole, reopened one file
// over. Review scored the narrowing mutant ALIVE against everything else in
// this package, so this test exists to grade it.
//
// The positive control is the load-bearing half: the assertion below is
// vacuous unless internal/types really does mention enum kinds outside
// kinds.go, so that is asserted first and named if it stops being true.
func TestEnumEntityKinds6818_TheWholeDeclaringPackageIsExcluded(t *testing.T) {
	root := repoRoot(t)
	refs, err := entkinds.ScanGoReferences(root, enumEntityKindStrings())
	if err != nil {
		t.Fatalf("ScanGoReferences: %v", err)
	}
	var insideBesidesKindsGo []string
	for _, s := range refs.Sites {
		if strings.HasPrefix(s.File, enumDeclaringPackage) && s.File != "internal/types/kinds.go" {
			insideBesidesKindsGo = append(insideBesidesKindsGo, fmt.Sprintf("%s at %s:%d", s.Kind, s.File, s.Line))
		}
	}
	if len(insideBesidesKindsGo) == 0 {
		t.Fatalf("no file of %s other than kinds.go mentions an enum kind, so excluding the "+
			"whole package and excluding only kinds.go cannot be told apart here. Re-cut this "+
			"test against whatever the exclusion now has to discount, or the difference it "+
			"defends is unobserved.", enumDeclaringPackage)
	}

	ev := corroborateEnumKinds(t)
	var leaked []string
	for _, k := range enumEntityKindStrings() {
		for _, site := range ev[k].goRef {
			if strings.HasPrefix(site, enumDeclaringPackage) {
				leaked = append(leaked, k+" <- "+site)
			}
		}
	}
	sort.Strings(leaked)
	if len(leaked) > 0 {
		t.Errorf("clause (b) accepted %d reference(s) from inside %s:\n  %s\n\n"+
			"The declaring package cannot corroborate its own enum. Narrowing the exclusion to "+
			"kinds.go alone lets a fabricated constant be vouched for by one mention in a "+
			"neighbouring file of the same package, which is #6818's hole one file over.",
			len(leaked), enumDeclaringPackage, strings.Join(leaked, "\n  "))
	}
	t.Logf("exclusion is load-bearing: %d enum-kind reference(s) inside %s outside kinds.go are "+
		"discounted, e.g. %s", len(insideBesidesKindsGo), enumDeclaringPackage, insideBesidesKindsGo[0])
}

// TestEnumOnlyEntityKinds6818_AllowListDoesNotRot is the other direction: an
// allow-list row is an excuse for an ABSENCE, so it must expire when the
// absence does. It fails when a row names a kind that is no longer in the enum,
// and — the rot case — when a row's kind has since gained a rule declaration or
// a Go reference.
//
// It does NOT separately check that a row carries a reason, and that is not an
// omission. The sweep's clause (c) is `c.allowedBy != ""`, and allowedBy IS the
// row's reason: a row with an empty string excuses nothing, so its kind is
// reported as uncorroborated by the sweep. A second check here could never be
// the only thing firing — it would be a guard graded by nothing, the shape this
// repository keeps finding in pairs. Blanking the one row's reason is scored as
// a mutant and dies on the sweep.
func TestEnumOnlyEntityKinds6818_AllowListDoesNotRot(t *testing.T) {
	ev := corroborateEnumKinds(t)

	var rows []string
	for k := range enumOnlyEntityKinds {
		rows = append(rows, k)
	}
	sort.Strings(rows)
	for _, k := range rows {
		c, inEnum := ev[k]
		if !inEnum {
			t.Errorf("enumOnlyEntityKinds[%q] names a kind types.AllEntityKinds() does not carry. "+
				"The allow-list excuses enum members only; delete the row and lower "+
				"enumOnlyEntityKindsMax.", k)
			continue
		}
		if len(c.ruleYAML) > 0 || len(c.goRef) > 0 {
			t.Errorf("enumOnlyEntityKinds[%q] says the kind is referenced nowhere outside %q, but "+
				"the live scan finds: rule-pack declarations %v, Go references %v.\n"+
				"Delete the row and lower enumOnlyEntityKindsMax by one — the sweep now accounts "+
				"for this kind on its own.",
				k, enumDeclaringPackage, c.ruleYAML, c.goRef)
		}
	}

	if len(enumOnlyEntityKinds) > enumOnlyEntityKindsMax {
		t.Errorf("enumOnlyEntityKinds has GROWN to %d rows (pin: %d). Raising the pin is a "+
			"reviewed act: each new row must state why the kind is legitimately enum-only, and "+
			"NOT because a scanner could not see its producer.",
			len(enumOnlyEntityKinds), enumOnlyEntityKindsMax)
	}
	if len(enumOnlyEntityKinds) < enumOnlyEntityKindsMax {
		t.Errorf("enumOnlyEntityKinds has shrunk to %d rows but the pin still reads %d — lower "+
			"enumOnlyEntityKindsMax to %d in the same change, or the bar is left slack for a "+
			"later append to slip under.",
			len(enumOnlyEntityKinds), enumOnlyEntityKindsMax, len(enumOnlyEntityKinds))
	}
}

// TestEnumEntityKinds6818_NoDeferredLedgerOverlapsTheEnum observes why this
// guard has no "deferred kinds" exclusion.
//
// #6818 asks for every NON-DEFERRED enum member to be corroborated. There is no
// such thing as a deferred enum member: a deferral ledger holds kinds the enum
// does NOT carry, and a kind leaves the ledger by being DECLARED. So the
// exclusion would be empty by construction, and the sweep above deliberately
// has none.
//
// WHICH LEDGER THIS TEST OBSERVES: #6744's ruleDeclaredKindsDeferred, and only
// that one. It is the only ledger in reach — #6776's goPrefixedKindsDeferred
// and goUnprefixedKindsDeferred are unexported test variables of
// internal/types, and this test does not read them. Their half of the property
// is carried where they live, by
// TestProducerEntityKinds6776_UnprefixedLedgerIsExact (a ledgered kind that has
// entered the enum fails it) and its prefixed twin, whose ledger is currently
// EMPTY and whose half is therefore vacuous today.
//
// The disjointness is observed rather than asserted: if this ledger ever grows
// an entry the enum also carries, "non-deferred enum member" stops being a
// synonym for "enum member" and the sweep needs the exclusion it currently does
// without.
func TestEnumEntityKinds6818_NoDeferredLedgerOverlapsTheEnum(t *testing.T) {
	inEnum := map[string]bool{}
	for _, k := range enumEntityKindStrings() {
		inEnum[k] = true
	}
	if len(inEnum) == 0 {
		t.Fatal("types.AllEntityKinds() is empty; the disjointness below would hold vacuously")
	}
	if len(ruleDeclaredKindsDeferred) == 0 {
		t.Fatal("ruleDeclaredKindsDeferred is empty; the disjointness below would hold vacuously")
	}
	for k := range ruleDeclaredKindsDeferred {
		if inEnum[k] {
			t.Errorf("ruleDeclaredKindsDeferred holds %q, which types.AllEntityKinds() now also "+
				"carries. A kind cannot be both deferred and declared: remove the ledger row (and "+
				"lower ruleDeclaredKindsDeferredMax), or — if deferral is meant to survive "+
				"declaration — give the #6818 sweep the deferred-member exclusion it does not "+
				"have, because it is currently sweeping this kind.", k)
		}
	}
}
