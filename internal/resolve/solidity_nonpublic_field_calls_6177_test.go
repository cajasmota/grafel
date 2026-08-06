package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6177 — the residual of #6141. The leaf-name CALLS tiers bound a bare
// Solidity call to a state variable that HAS NO GETTER, so the edge described a
// call that cannot exist in the source program.
//
// Solidity synthesises a getter for a state variable if and only if it is
// declared `public`. An internal / private / default-visibility variable is not
// callable from anywhere, including from inside its own contract. #6141 made
// famOperation a PREFERENCE, which is right — a preference must lose to nothing
// rather than drop a correct field bind — but a preference falls through, and
// the unrestricted second pass then accepted the field.
//
// The fix is ELIGIBILITY, not preference: uncallableSolidityField classifies
// the candidate at index time and scanLeafMembers skips it in BOTH passes.
// The two questions stay in separate tables on purpose; see
// Index.uncallableMember.
//
// A per-candidate skip is not the whole rule, because a (scope, member)
// collision is stored as a blank sentinel that has erased the IDs the skip
// needs. Those keys resolve over the eligible SUBSET instead; see
// Index.eligibleMember and the sentinel section below.
//
// The rule's scope is load-bearing in two directions — Solidity only, and CALLS
// only — and the guards at the bottom of this file pin both.

// solCaller builds a Solidity contract function carrying one bare CALLS edge,
// the shape ReferencesEmbedded resolves with caller (file, pkgDir) context.
func solCaller(id, name, file, target string) types.EntityRecord {
	return types.EntityRecord{
		ID: id, Kind: "SCOPE.Operation", Name: name, SourceFile: file,
		Language: "solidity",
		Relationships: []types.RelationshipRecord{{
			FromID: id, ToID: target, Kind: "CALLS",
		}},
	}
}

// solField mirrors internal/extractors/solidity/extractor.go's state-variable
// arm: Kind SCOPE.Schema, Language "solidity", and the declaration text in
// Signature (#6137), which is the only place visibility survives.
func solField(id, name, file, signature string) types.EntityRecord {
	return types.EntityRecord{
		ID: id, Kind: "SCOPE.Schema", Subtype: "field", Name: name,
		SourceFile: file, Language: "solidity", Signature: signature,
	}
}

// --- the impossible bind now dangles honestly ------------------------------

// TestSolidityInternalField_PackageScopedCallDangles_6177 is the issue's own
// example: `Consumer.run` calls bare `threshold()` and the only member of that
// leaf name in the directory is `Config.threshold`, declared internal in a
// sibling file. No getter exists, so nothing in Consumer can reach it and the
// edge must be left as a stub.
func TestSolidityInternalField_PackageScopedCallDangles_6177(t *testing.T) {
	records := []types.EntityRecord{
		solCaller("1111111111111111", "Consumer.run", "contracts/a.sol", "threshold"),
		solField("2222222222222222", "Config.threshold", "contracts/b.sol", "uint256 internal threshold"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "threshold" {
		t.Fatalf("bare CALLS `threshold` bound to id=%s kind=%s name=%s file=%s; "+
			"`uint256 internal threshold` has no synthesised getter, so no call to it "+
			"can exist and the edge must dangle",
			got.id, got.kind, got.name, got.file)
	}
}

// TestSolidityInternalField_SameFileCallDangles_6177 is the same rule on the
// file-scoped tier (refs.go:3129), which is a separate probe reached before the
// package-scoped one. Both route through scanLeafMembersPreferring, so both
// need the gate; a fix applied to only one of them passes the sibling test
// above and fails this one.
func TestSolidityInternalField_SameFileCallDangles_6177(t *testing.T) {
	records := []types.EntityRecord{
		solCaller("1111111111111111", "Consumer.run", "contracts/a.sol", "threshold"),
		solField("2222222222222222", "Config.threshold", "contracts/a.sol", "uint256 internal threshold"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "threshold" {
		t.Fatalf("same-file bare CALLS `threshold` bound to id=%s kind=%s name=%s; "+
			"the file-scoped tier needs the same eligibility gate as the package-scoped one",
			got.id, got.kind, got.name)
	}
}

// --- the regression that must not happen ----------------------------------

// TestSolidityPublicField_CallStillBinds_6177 is the control that made a
// blanket kind filter the wrong answer on #6135: a `public` state variable DOES
// get a synthesised getter, so a call to it is real and the edge is correct.
//
// One signature only. `public constant` and `public immutable` also get a
// getter, but they reach this bind through the identical `\bpublic\b` match, so
// they are rows in TestUncallableSolidityField_6177 and declSignature cases in
// internal/extractors/solidity rather than repeats of this resolver path.
func TestSolidityPublicField_CallStillBinds_6177(t *testing.T) {
	records := []types.EntityRecord{
		solCaller("1111111111111111", "Consumer.run", "contracts/a.sol", "totalAssets"),
		solField("2222222222222222", "Vault.totalAssets", "contracts/b.sol", "uint256 public totalAssets"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("`uint256 public totalAssets` HAS a synthesised getter, so the call is "+
			"real; the edge was left as stub %q", stub)
	}
	if got.id != "2222222222222222" {
		t.Fatalf("bound to id=%s kind=%s name=%s; want 2222222222222222 / Vault.totalAssets",
			got.id, got.kind, got.name)
	}
}

// TestSolidityIneligibleFieldIsSkippedNotFatal_6177 pins WHERE the rule lives.
// One internal field and one public field share the leaf name in the caller's
// package. The internal one must be SKIPPED the way a family-rejected candidate
// already is, leaving the public one as the single candidate.
//
// A post-check on the ID scanLeafMembersPreferring returned would fail this:
// the scan would see two distinct IDs, trip the cross-scope ambiguity guard and
// return nothing, turning a correct bind into a dangle. That is why the gate is
// inside scanLeafMembers.
func TestSolidityIneligibleFieldIsSkippedNotFatal_6177(t *testing.T) {
	records := []types.EntityRecord{
		solCaller("1111111111111111", "Consumer.run", "contracts/a.sol", "cap"),
		solField("2222222222222222", "Hidden.cap", "contracts/b.sol", "uint256 private cap"),
		solField("3333333333333333", "Exposed.cap", "contracts/c.sol", "uint256 public cap"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("bare CALLS `cap` left as stub %q; the ineligible `private cap` must be "+
			"SKIPPED rather than counted as a competing candidate, leaving `public cap` "+
			"as the single answer", stub)
	}
	if got.id != "3333333333333333" {
		t.Fatalf("bound to id=%s kind=%s name=%s; want 3333333333333333 / Exposed.cap "+
			"[uint256 public cap]", got.id, got.kind, got.name)
	}
}

// --- the ambiguity sentinel, which has no ID to judge ----------------------

// solCapCollision builds the shape that reaches the sentinel: TWO files in one
// directory declaring a contract of the SAME name, which is legal Solidity and
// ordinary in mock and test trees, beside a third contract declaring the same
// leaf name. The duplicate contract name collides in
// byPackageMember["contracts"]["Hidden"]["cap"], and a collision there is stored
// as the blank sentinel that has erased both IDs.
//
// hiddenB and hiddenD are the two `Hidden.cap` declarations; the caller is
// always contracts/a.sol calling bare `cap`.
func solCapCollision(hiddenB, hiddenD, exposed string) []types.EntityRecord {
	return []types.EntityRecord{
		solCaller("1111111111111111", "Consumer.run", "contracts/a.sol", "cap"),
		solField("2222222222222222", "Hidden.cap", "contracts/b.sol", hiddenB),
		solField("3333333333333333", "Hidden.cap", "contracts/d.sol", hiddenD),
		solField("4444444444444444", "Exposed.cap", "contracts/c.sol", exposed),
	}
}

// TestSolidityIneligibleSentinel_ScanContinuesToEligibleSibling_6177 is the
// sentinel hole. Both `Hidden.cap` declarations are internal, so the eligible
// subset of that key is EMPTY and the key must read as ABSENT — the scan has to
// carry on and bind `Exposed.cap`.
//
// The per-candidate skip cannot do this. It is guarded on a non-blank ID, so a
// sentinel reaches the unconditional "sentinel ⇒ abort" arm and abandons every
// remaining scope, not just the colliding one. internal/extractors/sresolver
// binds this today because it WITHHOLDS an ineligible entity before the write,
// so no sentinel is created; that divergence is what
// Index.eligibleMember closes, and the cross-resolver shape
// solidity_colliding_ineligible_scopes_6177 in
// internal/extractors/sresolver/resolver_agreement_6141_test.go pins it.
func TestSolidityIneligibleSentinel_ScanContinuesToEligibleSibling_6177(t *testing.T) {
	records := solCapCollision(
		"uint256 internal cap", "uint256 internal cap", "uint256 public cap")
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("bare CALLS `cap` left as stub %q. Two sibling files declaring "+
			"`contract Hidden { uint256 internal cap; }` collide into the blank sentinel; "+
			"an ineligible-only key must read as ABSENT so the scan reaches "+
			"`Exposed.cap`, not abort the whole scan", stub)
	}
	if got.id != "4444444444444444" {
		t.Fatalf("bound to id=%s kind=%s name=%s file=%s; want 4444444444444444 / "+
			"Exposed.cap [uint256 public cap]", got.id, got.kind, got.name, got.file)
	}
}

// TestSolidityIneligibleSentinel_SingleEligibleUnderSentinelBinds_6177 is the
// second outcome the overlay has to express: the colliding key itself holds one
// public and one internal declaration, so exactly ONE entity of that key is
// callable and it must bind THROUGH the sentinel. `Exposed.cap` is removed from
// the fixture so nothing else can supply the answer.
func TestSolidityIneligibleSentinel_SingleEligibleUnderSentinelBinds_6177(t *testing.T) {
	records := solCapCollision(
		"uint256 internal cap", "uint256 public cap", "uint256 internal cap")
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("bare CALLS `cap` left as stub %q; the colliding key has exactly one "+
			"callable entity (contracts/d.sol's `uint256 public cap`) and must bind it "+
			"even though the raw bucket holds the sentinel", stub)
	}
	if got.id != "3333333333333333" {
		t.Fatalf("bound to id=%s kind=%s name=%s file=%s; want 3333333333333333 / "+
			"contracts/d.sol", got.id, got.kind, got.name, got.file)
	}
}

// TestSolidityIneligibleSentinel_TwoEligibleStayAmbiguous_6177 is the guard on
// the other side: resolving over the eligible subset must not turn the ambiguity
// guard off. Both `Hidden.cap` declarations are public, so the eligible subset
// has two distinct entities and the stub must be left verbatim exactly as it was
// before #6177.
func TestSolidityIneligibleSentinel_TwoEligibleStayAmbiguous_6177(t *testing.T) {
	records := solCapCollision(
		"uint256 public cap", "uint256 public cap", "uint256 internal cap")
	got, stub := resolveEdge(t, records, 0)
	if stub != "cap" {
		t.Fatalf("bare CALLS `cap` bound to id=%s kind=%s name=%s file=%s; two distinct "+
			"CALLABLE entities share the key, so the stub must stay verbatim",
			got.id, got.kind, got.name, got.file)
	}
}

// --- scope guards ---------------------------------------------------------

// TestJavaPrivateFieldCallUnaffected_6177 is the language guard. Java's
// `private` says nothing about callability — a private field is legitimately
// readable inside its own class — so the #6141 precision gap stays exactly as
// it was for every language but Solidity. This test asserts the OLD behaviour
// on purpose; widening the rule beyond Solidity breaks it.
func TestJavaPrivateFieldCallUnaffected_6177(t *testing.T) {
	records := []types.EntityRecord{
		{
			ID: "1111111111111111", Kind: "Operation", Name: "Service.run",
			SourceFile: "src/Service.java", Language: "java",
			Relationships: []types.RelationshipRecord{{
				FromID: "1111111111111111", ToID: "cap", Kind: "CALLS",
			}},
		},
		{
			ID: "2222222222222222", Kind: "SCOPE.Schema", Name: "Config.cap",
			SourceFile: "src/Config.java", Language: "java", Signature: "private int cap",
		},
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("a Java `private int cap` stopped binding (stub %q). The visibility rule is "+
			"Solidity-only: only there does visibility decide whether a call can exist at all",
			stub)
	}
	if got.id != "2222222222222222" {
		t.Fatalf("bound to id=%s name=%s; want the pre-#6177 behaviour, 2222222222222222",
			got.id, got.name)
	}
}

// TestJavaFieldHintStubReachesSolidityField_6177 is the CALLS guard, and it is
// the reason the gate is an explicit parameter rather than something derived
// from the `prefer` mask.
//
// lookupPackageMemberByLeafName's other caller is the #667 Java inherited-field
// hint, the Java-gated scope:schema:ref arm. It resolves a REFERENCES stub,
// where a field being
// readable is the whole point and callability is beside the point, so it passes
// callableOnly = false. The fixture makes that argument observable by putting
// the ONLY candidate in a directory that mixes languages: mutating the `false`
// at that call site to `true` makes this test fail.
//
// The cross-package decoy is what makes the package tier load-bearing — without
// it lookupUniqueSchemaFieldByName would bind the stub anyway and the mutation
// would be invisible. Same construction as
// TestLeafNameTier_JavaFieldHintStubStillBinds_6141.
func TestJavaFieldHintStubReachesSolidityField_6177(t *testing.T) {
	records := []types.EntityRecord{
		{
			ID: "1111111111111111", Kind: "Operation", Name: "Child.use",
			SourceFile: "src/Child.java", Language: "java",
			Relationships: []types.RelationshipRecord{{
				FromID: "1111111111111111",
				ToID:   "scope:schema:ref:java:src/Child.java:Child.parentField",
				Kind:   "REFERENCES",
			}},
		},
		solField("2222222222222222", "Parent.parentField", "src/Parent.sol", "uint256 internal parentField"),
		entAt("3333333333333333", "SCOPE.Schema", "Elsewhere.parentField", "other/Elsewhere.java"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("#667 inherited-field hint stub left verbatim (%q). The visibility rule must "+
			"not reach the #667 hint's call site — that site resolves a REFERENCES stub and passes "+
			"callableOnly = false", stub)
	}
	if got.id != "2222222222222222" {
		t.Fatalf("bound to id=%s kind=%s name=%s; want 2222222222222222 / Parent.parentField",
			got.id, got.kind, got.name)
	}
}

// --- the predicate itself -------------------------------------------------

// TestUncallableSolidityField_6177 tests the predicate directly, because the
// end-to-end fixtures above cannot reach every arm: the substring traps and the
// fail-open arm have no natural resolver shape, and a token test that mistakes
// `publicKey` for the keyword would still pass every test above.
func TestUncallableSolidityField_6177(t *testing.T) {
	cases := []struct {
		language, kind, signature string
		want                      bool
	}{
		// Callable: the getter exists.
		{"solidity", "SCOPE.Schema", "uint256 public totalAssets", false},
		{"solidity", "SCOPE.Schema", "uint8 public constant PRE_DEPOSIT = 1", false},
		{"solidity", "SCOPE.Schema", "IERC20 public immutable asset", false},
		// Legal Solidity may omit the space before the keyword.
		{"solidity", "SCOPE.Schema", "mapping(address=>bool)public allowed", false},
		// Not callable: no getter is synthesised.
		{"solidity", "SCOPE.Schema", "uint256 internal threshold", true},
		{"solidity", "SCOPE.Schema", "uint256 private _cap", true},
		{"solidity", "SCOPE.Schema", "address currentActor", true},
		{"solidity", "SCOPE.Schema", "IERC20 asset", true},
		// Whole-word traps: an identifier or type name that merely CONTAINS
		// the keyword's letters is not the keyword.
		{"solidity", "SCOPE.Schema", "bytes32 publicKey", true},
		{"solidity", "SCOPE.Schema", "uint256 public_total", true},
		{"solidity", "SCOPE.Schema", "PublicRegistry registry", true},
		// Fail OPEN with no declaration text: a graph written before #4881 lost
		// Signature on load, and reading that as "not public" would dangle
		// every Solidity field call in it.
		{"solidity", "SCOPE.Schema", "", false},
		// Not a state variable. The Solidity extractor's only Schema-family
		// emission IS the state variable, so nothing else may be judged.
		{"solidity", "SCOPE.Operation", "function totalAssets() external view", false},
		{"solidity", "SCOPE.Component", "contract Vault", false},
		// Not Solidity. Visibility does not gate callability anywhere else.
		{"java", "SCOPE.Schema", "private int cap", false},
		{"kotlin", "SCOPE.Schema", "private val cap: Int", false},
		{"", "SCOPE.Schema", "private int cap", false},
	}
	for _, c := range cases {
		if got := uncallableSolidityField(c.language, c.kind, c.signature); got != c.want {
			t.Errorf("uncallableSolidityField(%q, %q, %q) = %v, want %v",
				c.language, c.kind, c.signature, got, c.want)
		}
	}
}

// TestUncallableMember_ModuleIndexParity_6177 pins the new side-table on the
// lazy/module index path. indexParityDiff compares it, but the representative
// fixture holds no Solidity, so without a Solidity fixture the comparison is
// vacuous and a missing population site in insertModuleEntry would go unnoticed
// until an incremental build silently bound the impossible edge again.
//
// NOT a duplicate of TestEligibleLeafOverlay_ModuleIndexParity_6177: this is the
// only fixture that guarantees uncallableMember parity cover on its own. That
// one happens to populate the table too, but incidentally — reshaping it would
// drop the cover here silently, which is the failure mode #6177 exists to remove.
func TestUncallableMember_ModuleIndexParity_6177(t *testing.T) {
	assertFullIndexParity(t, []types.EntityRecord{
		solCaller("1111111111111111", "Consumer.run", "contracts/a.sol", "threshold"),
		solField("2222222222222222", "Config.threshold", "contracts/b.sol", "uint256 internal threshold"),
		solField("3333333333333333", "Vault.totalAssets", "contracts/b.sol", "uint256 public totalAssets"),
	})
}

// TestEligibleLeafOverlay_ModuleIndexParity_6177 does the same for
// Index.eligibleMember / eligiblePackageMember, and needs its own fixture: the
// overlay only exists for a COLLIDED key, so the fixture above leaves both maps
// empty and its parity check over them is vacuous. A missing foldEligibleLeaf
// call in insertModuleEntry would otherwise surface only as an incremental build
// dangling a call a full rebuild binds.
//
// All three eligible-subset sizes are present: `Hidden.cap` is empty (both
// internal), `Shared.cap` is a single callable under a sentinel, and
// `Twin.cap` is two callables under one.
//
// `Flat.cap` collides inside ONE FILE, which is what covers eligibleMember —
// the other rows only reach eligiblePackageMember, since byMember is file-keyed
// and sibling files never collide there. A flattened contract (hardhat flatten,
// truffle-flattener) is the shape that produces it: concatenating the import
// graph can emit the same contract body twice into one file, and the regex
// extractor indexes what is on disk.
func TestEligibleLeafOverlay_ModuleIndexParity_6177(t *testing.T) {
	assertFullIndexParity(t, []types.EntityRecord{
		solCaller("1111111111111111", "Consumer.run", "contracts/a.sol", "cap"),
		solField("2222222222222222", "Hidden.cap", "contracts/b.sol", "uint256 internal cap"),
		solField("3333333333333333", "Hidden.cap", "contracts/d.sol", "uint256 internal cap"),
		solField("4444444444444444", "Shared.cap", "contracts/b.sol", "uint256 internal cap"),
		solField("5555555555555555", "Shared.cap", "contracts/d.sol", "uint256 public cap"),
		solField("6666666666666666", "Twin.cap", "contracts/b.sol", "uint256 public cap"),
		solField("7777777777777777", "Twin.cap", "contracts/d.sol", "uint256 public cap"),
		solField("8888888888888888", "Twin.cap", "contracts/e.sol", "uint256 internal cap"),
		solField("9999999999999999", "Flat.cap", "contracts/f.sol", "uint256 internal cap"),
		solField("aaaaaaaaaaaaaaaa", "Flat.cap", "contracts/f.sol", "uint256 public cap"),
	})
}
