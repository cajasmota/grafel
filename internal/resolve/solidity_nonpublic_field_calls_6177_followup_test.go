package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Follow-up fixtures for #6177's adversarial review. The base fixtures in
// solidity_nonpublic_field_calls_6177_test.go pin the eligibility overlay
// with every colliding candidate Solidity; these close three residual gaps
// the review found and that were closed here rather than sent back to the
// contributor.

// --- Gap 1: the sentinel overlay converts a dangle into a bind whose
// surviving candidate is not even the same language as the sentinel it
// replaces --------------------------------------------------------------

// javaMethodNamed builds a Java method entity shaped like the leaf-name
// tiers see any dotted-Name entity: Kind Operation, no Signature, no
// Language-specific marker that Index.uncallableMember could ever set for
// it (uncallableSolidityField gates on language=="solidity", so this
// entity's uncallableMember entry is always absent regardless of content).
func javaMethodNamed(id, name, file string) types.EntityRecord {
	return types.EntityRecord{
		ID: id, Kind: "Operation", Name: name, SourceFile: file, Language: "java",
	}
}

// TestMixedLanguageSentinel_JavaMethodSurvivesSolidityFieldCollision_6177
// reproduces the reviewer's residual gap: `contracts/b.sol` declares
// `contract Hidden { uint256 internal cap; }` and `contracts/Hidden.java`
// declares `class Hidden { void cap() {} }` — same package dir ("contracts"),
// same dotted key ("Hidden", "cap"), two different languages. Both land in
// byPackageMember["contracts"]["Hidden"]["cap"] and collide into the blank
// ambiguity sentinel, exactly like the same-language `Hidden`/`Hidden`
// shape in solidity_nonpublic_field_calls_6177_test.go — except here the
// surviving candidate under the eligible-subset overlay is the JAVA method,
// not a second Solidity field.
//
// Before #6182 this sentinel aborted the scan unconditionally and the bare
// call to `cap()` from contracts/a.sol dangled. After #6182 the overlay
// resolves the key over its eligible subset: the Solidity field is
// ineligible (internal, no getter), the Java method carries no
// uncallableMember entry at all (uncallableSolidityField only ever fires for
// language=="solidity"), so the subset is exactly one entity and the key
// binds through the sentinel to the Java method.
//
// TestMixedLanguageSentinelControl_DeletingSolidityFieldBindsSameJavaMethod_6177
// is the control: it removes the Solidity field so no collision exists at
// all, and asserts the SAME Java method still binds by the direct
// (non-colliding) route. Verified by running both fixtures against the
// current tree before writing this pin: both resolve to the identical
// entity, id=3333333333333333 / Operation / Hidden.cap /
// contracts/Hidden.java — which is the evidence that the sentinel-mediated
// bind here is a restoration of the correct answer, not a new mis-bind
// smuggled in by the eligibility overlay. A hypothetical predicate
// false-positive (uncallableSolidityField wrongly marking an eligible field
// ineligible) would, without this fixture, compound into a confident WRONG
// edge here instead of a safe dangle — which is why this shape needed its
// own pin distinct from the same-language sentinel tests.
func TestMixedLanguageSentinel_JavaMethodSurvivesSolidityFieldCollision_6177(t *testing.T) {
	records := []types.EntityRecord{
		solCaller("1111111111111111", "Consumer.run", "contracts/a.sol", "cap"),
		solField("2222222222222222", "Hidden.cap", "contracts/b.sol", "uint256 internal cap"),
		javaMethodNamed("3333333333333333", "Hidden.cap", "contracts/Hidden.java"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("bare CALLS `cap` left as stub %q; contracts/b.sol's `Hidden.cap` is an "+
			"internal Solidity field (ineligible) and contracts/Hidden.java's `Hidden.cap` "+
			"is a Java method (never marked uncallable), so the (\"contracts\",\"Hidden\","+
			"\"cap\") sentinel's eligible subset is exactly the Java method and the key "+
			"must bind through it rather than abort the scan", stub)
	}
	if got.id != "3333333333333333" {
		t.Fatalf("bound to id=%s kind=%s name=%s file=%s; want 3333333333333333 / "+
			"Hidden.cap [Java method, contracts/Hidden.java]",
			got.id, got.kind, got.name, got.file)
	}
}

func TestMixedLanguageSentinelControl_DeletingSolidityFieldBindsSameJavaMethod_6177(t *testing.T) {
	records := []types.EntityRecord{
		solCaller("1111111111111111", "Consumer.run", "contracts/a.sol", "cap"),
		javaMethodNamed("3333333333333333", "Hidden.cap", "contracts/Hidden.java"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("control: with the Solidity field removed entirely (no collision, no "+
			"sentinel), the bare CALLS `cap` still failed to bind to the sole remaining "+
			"candidate, stub=%q", stub)
	}
	if got.id != "3333333333333333" {
		t.Fatalf("control: bound to id=%s kind=%s name=%s; want 3333333333333333 / "+
			"Hidden.cap — the same entity the sentinel-mediated test above binds, which is "+
			"the evidence that the sentinel bind restores the correct answer rather than "+
			"introducing a new one", got.id, got.kind, got.name)
	}
}

// --- Gap 2: the fail-open on an empty Signature, pinned at the resolver
// level rather than only in the predicate table -------------------------

// TestSolidityFieldEmptySignatureFailsOpen_ResolverLevel_6177 is the
// resolver-level companion to the "" row in TestUncallableSolidityField_6177
// (predicate table). uncallableSolidityField returns "eligible" whenever
// Signature is empty — the state graphs written before #4881 lose that field
// on load (internal/graph/load.go:601) hit exactly this path, and reading
// "no declaration text" as "not public" would dangle every Solidity field
// bind such a graph holds.
//
// A single caller and a single field with Signature: "" is enough to make
// the fail-open direction observable independently of the predicate table:
// if uncallableSolidityField were flipped to treat an empty Signature as
// UNCALLABLE, idx.uncallableMember would gain an entry for this field, the
// per-candidate skip in scanLeafMembers would fire (this fixture has no
// collision, so the sentinel/overlay path is not involved at all — this is
// the plain per-candidate skip #6141 already exercises), and the bare call
// would dangle instead of binding.
func TestSolidityFieldEmptySignatureFailsOpen_ResolverLevel_6177(t *testing.T) {
	records := []types.EntityRecord{
		solCaller("1111111111111111", "Consumer.run", "contracts/a.sol", "legacy"),
		solField("2222222222222222", "Config.legacy", "contracts/b.sol", ""),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("bare CALLS `legacy` left as stub %q; a Solidity field with an EMPTY "+
			"Signature (the shape a pre-#4881 graph.fb load produces) must fail OPEN and "+
			"still bind, not be treated as non-public", stub)
	}
	if got.id != "2222222222222222" {
		t.Fatalf("bound to id=%s kind=%s name=%s; want 2222222222222222 / Config.legacy",
			got.id, got.kind, got.name)
	}
}
