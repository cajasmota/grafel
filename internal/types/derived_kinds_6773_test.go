package types

import (
	"sort"
	"testing"
)

// derived_kinds_6773_test.go — the guard on the DERIVED relationship-kind
// vocabulary (#6773).
//
// #6757 arm C measured the write path and found COMMIT_COUPLED to be 99.1% of
// every edge whose kind the Go enum does not list. The decision taken on #6773
// was option 2: declare it, but in a SEPARATE class, so a consumer can include
// or exclude the statistical population deliberately instead of discovering it
// by accident.
//
// A separate class is only worth anything if the separation is real, so the
// tests below assert it in BOTH directions. Recall — "COMMIT_COUPLED is in the
// derived list" — cannot detect a derived list that has quietly swallowed the
// structural vocabulary too.

// derivedKindsSourceFile is the single file that declares the derived
// vocabulary, read rather than hand-listed for the same reason
// kindsSourceFile is.
const derivedKindsSourceFile = "derived_kinds.go"

// TestDerivedKindsExtraction_NonVacuous is the floor under every guard in this
// file: if the AST walk stops reading derived_kinds.go it would find zero
// constants and the completeness guard below would pass by examining nothing.
func TestDerivedKindsExtraction_NonVacuous(t *testing.T) {
	declared := declaredRelationshipKindsIn(t, derivedKindsSourceFile)
	if minDeclared := len(AllDerivedRelationshipKinds()); len(declared) < minDeclared {
		t.Fatalf("extracted %d RelationshipKind constants from %s, but AllDerivedRelationshipKinds() "+
			"returns %d; the extraction is no longer reading the declarations and the completeness "+
			"guard would be vacuous", len(declared), derivedKindsSourceFile, minDeclared)
	}
	byName := map[string]string{}
	for _, d := range declared {
		byName[d.Name] = d.Value
	}
	if got, ok := byName["RelationshipKindCommitCoupled"]; !ok || got != "COMMIT_COUPLED" {
		t.Errorf("extraction read RelationshipKindCommitCoupled = %q (present=%v), want %q",
			got, ok, "COMMIT_COUPLED")
	}
}

// TestAllDerivedRelationshipKinds_CoversEveryDeclaredConstant is the derived
// half of #6757 arm A. Without it, derived_kinds.go is the one file in this
// package where a RelationshipKind constant can be declared, emitted, and left
// out of every vocabulary accessor — the exact hole arm A closed for kinds.go.
func TestAllDerivedRelationshipKinds_CoversEveryDeclaredConstant(t *testing.T) {
	declared := declaredRelationshipKindsIn(t, derivedKindsSourceFile)
	if len(declared) == 0 {
		t.Fatalf("no RelationshipKind constants extracted from %s; guard would be vacuous",
			derivedKindsSourceFile)
	}
	registered := map[RelationshipKind]bool{}
	for _, k := range AllDerivedRelationshipKinds() {
		registered[k] = true
	}
	var missing []string
	for _, d := range declared {
		if !registered[RelationshipKind(d.Value)] {
			missing = append(missing, d.Name+" ("+d.Value+")")
			continue
		}
		if !IsDerivedRelationshipKind(d.Value) {
			t.Errorf("IsDerivedRelationshipKind(%q) = false although %s is registered", d.Value, d.Name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d RelationshipKind constant(s) declared in %s but missing from "+
			"AllDerivedRelationshipKinds(): %v", len(missing), derivedKindsSourceFile, missing)
	}
}

// TestDerivedAndStructuralVocabulariesAreDisjoint is the over-firing control.
// The whole value of option 2 is that a consumer can EXCLUDE the statistical
// population; a derived list that also contains structural kinds, or a
// structural list that has absorbed the derived ones, gives them no way to.
func TestDerivedAndStructuralVocabulariesAreDisjoint(t *testing.T) {
	structural := map[RelationshipKind]bool{}
	for _, k := range AllRelationshipKinds() {
		structural[k] = true
	}
	derived := map[RelationshipKind]bool{}
	for _, k := range AllDerivedRelationshipKinds() {
		if derived[k] {
			t.Errorf("AllDerivedRelationshipKinds() lists %q twice", k)
		}
		derived[k] = true
	}
	if len(derived) == 0 {
		t.Fatal("AllDerivedRelationshipKinds() is empty; every assertion below is vacuous")
	}

	// Direction 1: no derived kind is structural.
	for k := range derived {
		if structural[k] {
			t.Errorf("%q is in BOTH AllRelationshipKinds() and AllDerivedRelationshipKinds(); "+
				"a consumer excluding the derived class would still traverse it", k)
		}
		if IsValidRelationshipKind(string(k)) {
			t.Errorf("IsValidRelationshipKind(%q) = true; the structural predicate must not accept "+
				"a derived kind, or #6757's ledger and this vocabulary disagree about what it is", k)
		}
	}

	// Direction 2: no structural kind is derived. This is the half recall
	// cannot see — a derived list containing every kind would satisfy every
	// "COMMIT_COUPLED is derived" assertion in this file.
	for k := range structural {
		if derived[k] {
			t.Errorf("structural kind %q also appears in AllDerivedRelationshipKinds()", k)
		}
		if IsDerivedRelationshipKind(string(k)) {
			t.Errorf("IsDerivedRelationshipKind(%q) = true for a structural kind", k)
		}
	}
}

// TestIsDeclaredRelationshipKindIsExactlyTheUnion pins the single definition of
// "declared" that #6773 hands to the arm-C write-path counter and to #6757's
// arm-B ledger. If the two ever measure against different sets, one of them
// reports a kind as unknown that the other has blessed.
func TestIsDeclaredRelationshipKindIsExactlyTheUnion(t *testing.T) {
	for _, k := range AllRelationshipKinds() {
		if !IsDeclaredRelationshipKind(string(k)) {
			t.Errorf("IsDeclaredRelationshipKind(%q) = false for a structural kind", k)
		}
	}
	for _, k := range AllDerivedRelationshipKinds() {
		if !IsDeclaredRelationshipKind(string(k)) {
			t.Errorf("IsDeclaredRelationshipKind(%q) = false for a derived kind", k)
		}
	}
	// The negative half: "declared" must still exclude something, or the
	// predicate is a constant-true function and the counter it feeds can never
	// report anything again.
	for _, undeclared := range []string{"STEP_IN_PROCESS", "ENTRY_POINT_OF", "NOT_A_KIND_AT_ALL", ""} {
		if IsDeclaredRelationshipKind(undeclared) {
			t.Errorf("IsDeclaredRelationshipKind(%q) = true; it is in neither vocabulary", undeclared)
		}
	}
}

// TestAllDerivedRelationshipKindsIsNotAliasedToTheStructuralSlice guards the
// cheapest wrong implementation: returning the same backing array. A caller
// that appends to one result would then corrupt the other vocabulary.
func TestAllDerivedRelationshipKindsIsNotAliasedToTheStructuralSlice(t *testing.T) {
	a := AllDerivedRelationshipKinds()
	if len(a) == 0 {
		t.Fatal("AllDerivedRelationshipKinds() is empty")
	}
	a[0] = "MUTATED_BY_A_CALLER"
	b := AllDerivedRelationshipKinds()
	if b[0] == "MUTATED_BY_A_CALLER" {
		t.Error("AllDerivedRelationshipKinds() hands out a shared slice; one caller can rewrite the " +
			"vocabulary for every other")
	}
}
