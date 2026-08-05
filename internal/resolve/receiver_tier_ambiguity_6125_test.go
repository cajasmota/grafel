package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6125 — the Go receiver-type tier in ReferencesEmbeddedWithAllowlist
// (refs.go, the `Properties["receiver_type"]` branch) refused ambiguous
// `(pkgDir, receiverType, member)` tuples and then fell through to
// rewriteOneWithCaller → the global byName index, WHICH BINDS. The inline
// comment claimed the opposite ("preserve the stub").
//
// MEASURED before the fix, not inferred: with two `T11.Do11` methods in
// package `svc` and one unrelated `Do11` function in `other/`, the receiver
// tier returned ("", true) — refusal — and the edge still landed on
// other/z.go's Do11.
//
// # What the refusal actually reaches, and why a blanket refusal is wrong
//
// A first version of this fix terminated the ladder outright on refusal,
// justified by: byPackageMember keys come from splitting an entity Name at
// its LAST dot (refs.go:1207-1233), so declined candidates are named
// "<Recv>.<member>"; byName is keyed on the WHOLE Name (refs.go:1474, 1497);
// therefore a declined candidate can never come back and the downstream tier
// is "structurally incapable" of returning one.
//
// The byName half is true. The conclusion was FALSE, because byName is not
// the only thing downstream. rewriteOneWithCaller (refs.go:3036-3041) routes
// an unmatched bare CALLS stub into lookupBareWithLocality, whose CALLS arm
// probes lookupMemberByLeafName over byMember[callerFile] (refs.go:3129) and
// then lookupPackageMemberByLeafName over byPackageMember[pkgDir]
// (refs.go:3134) — and byMember is built by the SAME last-dot split, in the
// same loop, ten lines above byPackageMember. A declined candidate is not
// merely reachable there; byMember is where it canonically lives.
//
// Measured cost of the blanket version, on the Go build-tag shape (legal and
// common): two `T11.Do11` in package svc, caller in svc/a.go, no foreign
// `Do11` anywhere.
//
//	base 617aeba6c : {kind=Method name=T11.Do11 file=svc/a.go}  — correct
//	blanket refusal: dangled                                     — REGRESSION
//
// So the fix keeps the terminal refusal but hoists the SAME-FILE probe above
// it. The locality argument is the true one: the ambiguity that triggered the
// refusal is package-wide, and the caller's own file is strictly narrower.
// Under build tags exactly one of the colliding files compiles, so a caller
// inside a.go means a.go's method.
//
// Full measured delta (all four asserted below):
//
//	scenario                                   base            this branch
//	(a) 3rd-file caller, foreign global Do11    other/z.go      dangle    WIN
//	(b) caller in svc/a.go                      svc/a.go        svc/a.go  KEPT
//	(c) 3rd-file caller, sibling T22.Do11       dangle          dangle    same
//	(d) caller file has two scopes w/ Do11      dangle          dangle    same
//
// (c) is the one to not guess about: the package-wide leaf tier looks like a
// second mis-bind source but provably is not. scanLeafMembers returns
// ("", false) on meeting a blank ambiguity sentinel (refs.go:2884-2888,
// 2901-2906), and the refusal path is DEFINED by that sentinel being present
// in the very pkgBucket the scan walks. Cutting that tier is a no-op. The
// same-file scan is unaffected because the sentinel lives in byPackageMember,
// not byMember.
//
// The helper contract (refs.go:2757-2762, 2779-2781) said the call site should
// "leave the stub alone instead of falling back to global bare-name lookup".
// Read precisely, that names byName — which is exactly what this cuts, and
// nothing wider. internal/extractors/sresolver fixed the same misconception
// for #6098.
//
// Nothing below counts dangling edges. For this issue a count is actively
// deceptive: the candidate fixes move it in opposite directions.

const (
	recvPkg6125    = "svc"
	recvType6125   = "T11"
	recvMember6125 = "Do11"
)

// ambiguousReceiverFixture6125 builds the shape the issue names: two same-named
// methods on the same receiver type inside one Go package (so
// byPackageMember["svc"]["T11"]["Do11"] carries the blank ambiguity sentinel),
// plus a single globally-unique bare `Do11` in an unrelated package. The
// foreign function is the whole point — refusal is only meaningful when
// something wrong is available to bind to.
func ambiguousReceiverFixture6125() (a, b, foreign types.EntityRecord) {
	a = types.EntityRecord{
		ID: "a1a1a1a1a1a1a1a1", Kind: "Method", Name: recvType6125 + "." + recvMember6125,
		SourceFile: recvPkg6125 + "/a.go", Language: "go",
	}
	b = types.EntityRecord{
		ID: "b1b1b1b1b1b1b1b1", Kind: "Method", Name: recvType6125 + "." + recvMember6125,
		SourceFile: recvPkg6125 + "/b.go", Language: "go",
	}
	foreign = types.EntityRecord{
		ID: "f1f1f1f1f1f1f1f1", Kind: "Function", Name: recvMember6125,
		SourceFile: "other/z.go", Language: "go",
	}
	return a, b, foreign
}

// callerWithReceiverStamp6125 returns the caller record carrying the embedded
// CALLS edge exactly as the Go extractor emits it: bare-name ToID plus a
// Properties["receiver_type"] stamp, anchored on a file in the same package
// directory as the ambiguous candidates.
func callerWithReceiverStamp6125(recvStamp string) []types.EntityRecord {
	return []types.EntityRecord{{
		ID: "c1c1c1c1c1c1c1c1", Kind: "Method", Name: recvType6125 + ".Caller",
		SourceFile: recvPkg6125 + "/caller.go", Language: "go",
		Relationships: []types.RelationshipRecord{{
			FromID:     "c1c1c1c1c1c1c1c1",
			ToID:       recvMember6125,
			Kind:       "CALLS",
			Properties: types.Props{{K: "receiver_type", V: recvStamp}},
		}},
	}}
}

// TestReceiverTierRefusalIsTerminal6125 is the behavioural gate. Revert the
// fix and this fails on the ToID assertion — the edge binds to other/z.go's
// Do11 — which is the intended assertion, not a compile or collateral failure.
func TestReceiverTierRefusalIsTerminal6125(t *testing.T) {
	a, b, foreign := ambiguousReceiverFixture6125()
	all := []types.EntityRecord{a, b, foreign}
	idx := BuildIndex(all)

	// The precondition, asserted rather than assumed: the tier really does
	// refuse. ("", true) is the handled-but-ambiguous sentinel.
	if id, ok := idx.lookupPackageMember(recvPkg6125, recvType6125, recvMember6125); id != "" || !ok {
		t.Fatalf("lookupPackageMember(%q,%q,%q) = (%q, %v), want (\"\", true) — the "+
			"fixture no longer produces the ambiguity this test is about",
			recvPkg6125, recvType6125, recvMember6125, id, ok)
	}
	// And the collider really is bindable on its own: byName[member] is
	// unique and points at the foreign package. Without this, "did not
	// mis-bind" could just mean "nothing was reachable".
	if got, ok := idx.byName[recvMember6125]; !ok || got != foreign.ID {
		t.Fatalf("byName[%q] = (%q, %v), want the foreign Function %q — the "+
			"wrong-bind target must be reachable for the refusal to mean anything",
			recvMember6125, got, ok, foreign.ID)
	}

	recs := callerWithReceiverStamp6125(recvType6125)
	ReferencesEmbedded(recs, idx)
	got := recs[0].Relationships[0].ToID

	for _, e := range all {
		if e.ID == got {
			t.Fatalf("ambiguous receiver bound to {kind=%q name=%q source_file=%q}. "+
				"The caller is in a THIRD file, so no same-file candidate exists and "+
				"the only reachable binder is byName — which is keyed on the whole "+
				"entity Name, so it cannot hold any of the %q candidates the tier "+
				"declined between. This bind is by construction a non-candidate.",
				e.Kind, e.Name, e.SourceFile, recvType6125+"."+recvMember6125)
		}
	}
	if got != recvMember6125 {
		t.Fatalf("ambiguous receiver rewrote the ToID to %q; want the stub %q left "+
			"verbatim so the edge dangles honestly", got, recvMember6125)
	}
}

// TestReceiverTierSameFileCandidateStillBinds6125 is scenario (b) — the
// regression the first version of this fix introduced, now a permanent test.
//
// Two `T11.Do11` in package svc (so the receiver tier refuses, package-wide)
// and the CALLER ITSELF in svc/a.go. There is no foreign `Do11`, so byName is
// absent and cannot rescue anything: the only mechanism that can produce the
// right answer is the same-file probe. Go build-tag variants produce exactly
// this shape. Base bound it correctly and a blanket refusal dangled it.
func TestReceiverTierSameFileCandidateStillBinds6125(t *testing.T) {
	a, b, _ := ambiguousReceiverFixture6125()
	all := []types.EntityRecord{a, b}
	idx := BuildIndex(all)

	if id, ok := idx.lookupPackageMember(recvPkg6125, recvType6125, recvMember6125); id != "" || !ok {
		t.Fatalf("lookupPackageMember = (%q, %v), want (\"\", true) — the fixture must "+
			"still trigger the package-wide refusal", id, ok)
	}
	if id, ok := idx.byName[recvMember6125]; ok {
		t.Fatalf("byName[%q] = %q, want absent — this fixture must isolate the "+
			"same-file probe as the ONLY mechanism that can bind", recvMember6125, id)
	}

	// Caller anchored in svc/a.go, the same file as candidate `a`.
	recs := callerWithReceiverStamp6125(recvType6125)
	recs[0].SourceFile = a.SourceFile
	ReferencesEmbedded(recs, idx)

	got := recs[0].Relationships[0].ToID
	if got == recvMember6125 {
		t.Fatalf("same-file caller dangled. The package-wide ambiguity must not "+
			"suppress the caller's OWN FILE candidate %s in %s — under Go build tags "+
			"exactly one of %s/a.go and %s/b.go compiles, and a caller inside a.go "+
			"means a.go's method", a.Name, a.SourceFile, recvPkg6125, recvPkg6125)
	}
	if got != a.ID {
		t.Fatalf("same-file caller bound to %q, want %q (%s in %s)",
			got, a.ID, a.Name, a.SourceFile)
	}
}

// TestReceiverTierSameFileRescueDeclinesOnCollision6125 is scenario (d), the
// false-negative control on the rescue: the rescue must not guess either. Two
// scopes inside the caller's own file both declare the leaf `Do11`, so
// lookupMemberByLeafName declines (refs.go:2884-2888) and the refusal stands.
func TestReceiverTierSameFileRescueDeclinesOnCollision6125(t *testing.T) {
	a, b, _ := ambiguousReceiverFixture6125()
	twinScope := types.EntityRecord{
		ID: "d1d1d1d1d1d1d1d1", Kind: "Method", Name: "T22." + recvMember6125,
		SourceFile: a.SourceFile, Language: "go",
	}
	all := []types.EntityRecord{a, b, twinScope}
	idx := BuildIndex(all)

	recs := callerWithReceiverStamp6125(recvType6125)
	recs[0].SourceFile = a.SourceFile
	ReferencesEmbedded(recs, idx)

	got := recs[0].Relationships[0].ToID
	for _, e := range all {
		if e.ID == got {
			t.Fatalf("same-file rescue picked {kind=%q name=%q source_file=%q} out of "+
				"two scopes in one file that both declare %q — it must decline, not "+
				"choose", e.Kind, e.Name, e.SourceFile, recvMember6125)
		}
	}
	if got != recvMember6125 {
		t.Fatalf("ToID = %q, want the stub %q left verbatim", got, recvMember6125)
	}
}

// TestReceiverTierRefusalCountsAsAmbiguousNotResolved6125 pins the bookkeeping
// alongside the binding. A refusal that silently vanished from the stats would
// be indistinguishable from a resolution to any downstream consumer of Stats.
//
// The disposition half is the one that matters. classifyDispositionLang calls
// this bug-resolver — "the resolver couldn't disambiguate it" — which feeds
// Stats.BugRate (refs.go:817-818). Measured on this fixture, that put BugRate
// at 0.5 for an endpoint the resolver refused CORRECTLY, meaning the headline
// quality metric moves the wrong way exactly where this fix does its job.
// Dynamic is documented as "not a bug; the call cannot be resolved statically
// by design" (refs.go:163-166), and the framework-DSL receiver gate in the
// same function already sets the precedent of a deliberate tier-level decline
// recording Dynamic.
func TestReceiverTierRefusalCountsAsAmbiguousNotResolved6125(t *testing.T) {
	a, b, foreign := ambiguousReceiverFixture6125()
	idx := BuildIndex([]types.EntityRecord{a, b, foreign})

	stats := ReferencesEmbedded(callerWithReceiverStamp6125(recvType6125), idx)
	if stats.ToAmbiguous != 1 {
		t.Fatalf("stats.ToAmbiguous = %d, want 1", stats.ToAmbiguous)
	}
	if stats.ToRewritten != 0 {
		t.Fatalf("stats.ToRewritten = %d, want 0 — a refused endpoint must not be "+
			"counted as rewritten", stats.ToRewritten)
	}
	if n := stats.DispositionCounts[DispositionBugResolver] +
		stats.DispositionCounts[DispositionBugExtractor]; n != 0 {
		t.Fatalf("a deliberate, correct refusal landed in %d bug-* disposition(s): %v. "+
			"Those feed Stats.BugRate, so this would RAISE the headline quality metric "+
			"precisely where the fix works", n, stats.DispositionCounts)
	}
	if stats.DispositionCounts[DispositionDynamic] != 1 {
		t.Fatalf("disposition counts = %v, want exactly one %v", stats.DispositionCounts,
			DispositionDynamic)
	}
	if stats.BugRate != 0 {
		t.Fatalf("BugRate = %v, want 0", stats.BugRate)
	}
}

// TestReceiverTierRefusalIsTerminalForQualifiedStamp6125 covers the #364 arm.
// The tier tries the stamp as-is first, then its leaf after the last dot; a
// refusal on EITHER attempt must terminate the ladder. Without the fix this
// binds to the foreign function just as the bare stamp does.
func TestReceiverTierRefusalIsTerminalForQualifiedStamp6125(t *testing.T) {
	a, b, foreign := ambiguousReceiverFixture6125()
	idx := BuildIndex([]types.EntityRecord{a, b, foreign})

	recs := callerWithReceiverStamp6125("pkgq." + recvType6125)
	ReferencesEmbedded(recs, idx)
	if got := recs[0].Relationships[0].ToID; got != recvMember6125 {
		t.Fatalf("package-qualified receiver stamp %q resolved to %q; the leaf retry "+
			"hit the same ambiguity and must terminate the ladder, leaving the stub %q",
			"pkgq."+recvType6125, got, recvMember6125)
	}
}

// TestReceiverTierPackageWideLeafTierNeverBoundThisAnyway6125 is scenario (c),
// and it exists to stop a plausible-sounding claim from being asserted without
// evidence — the exact failure this branch already made once.
//
// The package-wide leaf tier (refs.go:3134) looks like a second mis-bind
// source: a sibling `T22.Do11` in the package, bound despite an explicit
// receiver_type stamp of T11. It is not, and base does not bind it either.
// scanLeafMembers returns ("", false) the moment it meets a blank ambiguity
// sentinel (refs.go:2884-2888, 2901-2906), and the refusal path is defined by
// that sentinel sitting in the very pkgBucket the scan walks. So cutting that
// tier is a NO-OP, and this branch claims no credit for it.
func TestReceiverTierPackageWideLeafTierNeverBoundThisAnyway6125(t *testing.T) {
	a, b, _ := ambiguousReceiverFixture6125()
	sibling := types.EntityRecord{
		ID: "e1e1e1e1e1e1e1e1", Kind: "Method", Name: "T22." + recvMember6125,
		SourceFile: recvPkg6125 + "/c.go", Language: "go",
	}
	all := []types.EntityRecord{a, b, sibling}
	idx := BuildIndex(all)

	if id, ok := idx.lookupPackageMemberByLeafName(recvPkg6125, recvMember6125, famOperation); ok || id != "" {
		t.Fatalf("lookupPackageMemberByLeafName(%q,%q) = (%q, %v), want (\"\", false). "+
			"The blank sentinel from the (pkg, T11, Do11) collision is supposed to "+
			"poison this scan; if it no longer does, the package-wide tier CAN now "+
			"mis-bind on the refusal path and this branch's reasoning needs redoing",
			recvPkg6125, recvMember6125, id, ok)
	}

	recs := callerWithReceiverStamp6125(recvType6125)
	ReferencesEmbedded(recs, idx)
	if got := recs[0].Relationships[0].ToID; got != recvMember6125 {
		t.Fatalf("ToID = %q, want the stub %q", got, recvMember6125)
	}
}

// TestReceiverTierStillBindsWhenUnambiguous6125 is the false-negative control
// for the fix itself. Terminating on refusal must not turn into terminating on
// a MISS: with a single `T11.Do11` in the package the tier must still bind it,
// and it must be the local one rather than the foreign collider.
func TestReceiverTierStillBindsWhenUnambiguous6125(t *testing.T) {
	a, _, foreign := ambiguousReceiverFixture6125()
	all := []types.EntityRecord{a, foreign}
	idx := BuildIndex(all)

	recs := callerWithReceiverStamp6125(recvType6125)
	ReferencesEmbedded(recs, idx)
	got := recs[0].Relationships[0].ToID
	if got != a.ID {
		t.Fatalf("unambiguous receiver bound to %q, want the same-package method %q "+
			"(%s in %s). Issue #148's resolution must survive the #6125 fix",
			got, a.ID, a.Name, a.SourceFile)
	}
}

// TestReceiverTierMissStillFallsThrough6125 is the other half of that control.
// A tuple the receiver tier never had an opinion about — ("", false), a plain
// miss — must still reach the downstream tiers. Only REFUSAL is terminal.
func TestReceiverTierMissStillFallsThrough6125(t *testing.T) {
	_, _, foreign := ambiguousReceiverFixture6125()
	all := []types.EntityRecord{foreign}
	idx := BuildIndex(all)

	if id, ok := idx.lookupPackageMember(recvPkg6125, recvType6125, recvMember6125); ok {
		t.Fatalf("lookupPackageMember returned handled=true (id=%q) for a tuple with "+
			"no entry; this control needs a plain miss", id)
	}

	recs := callerWithReceiverStamp6125(recvType6125)
	ReferencesEmbedded(recs, idx)
	got := recs[0].Relationships[0].ToID
	if got != foreign.ID {
		t.Fatalf("a receiver-tier MISS resolved to %q, want the global bare-name "+
			"match %q (%s in %s). #6125 makes refusal terminal, not absence",
			got, foreign.ID, foreign.Name, foreign.SourceFile)
	}
}

// TestPackageComponentTierFallThroughCannotMisbind6125 records why the
// SIBLING tier below the receiver branch — the Refs #44 byPackageComponent
// fall-through — is deliberately left alone even though it shares the same
// ("", true) refusal contract and looks like the same defect.
//
// The exemption holds on TWO independent properties, and the first one alone
// is not enough — that was the error corrected in this file's header:
//
//  1. byPackageComponent is keyed on the dot-FREE bare entity Name
//     (refs.go:1419-1427), so when it is ambiguous its candidates sit in
//     byName under that identical string and byName is ambiguous too.
//  2. More decisively, the leaf-name tiers that made a blanket refusal wrong
//     for the receiver branch are not reachable from here at all:
//     lookupBareWithLocality's switch (refs.go:3111-3144) puts
//     lookupMemberByLeafName / lookupPackageMemberByLeafName ONLY on the
//     CALLS arm. The EXTENDS/IMPLEMENTS arm has no leaf tier, and DEPENDS_ON
//     — the dominant kind for this tier, per isComponentTargetKind — is not
//     in the switch at all. Its refusals reach byName and stop.
//
// Property 2 is what makes this safe; property 1 is corroboration. If either
// changes this test fails and the exemption must be re-argued, not assumed.
func TestPackageComponentTierFallThroughCannotMisbind6125(t *testing.T) {
	all := []types.EntityRecord{
		{ID: "a2a2a2a2a2a2a2a2", Kind: "Component", Name: "Server", SourceFile: "svc/a.go", Language: "go"},
		{ID: "b2b2b2b2b2b2b2b2", Kind: "Component", Name: "Server", SourceFile: "svc/b.go", Language: "go"},
		{ID: "f2f2f2f2f2f2f2f2", Kind: "Component", Name: "Server", SourceFile: "other/z.go", Language: "go"},
	}
	idx := BuildIndex(all)

	if id, ok := idx.lookupPackageComponent("svc", "Server"); id != "" || !ok {
		t.Fatalf("lookupPackageComponent(svc, Server) = (%q, %v), want (\"\", true)", id, ok)
	}
	if id, ok := idx.byName["Server"]; ok && id != "" {
		t.Fatalf("byName[\"Server\"] = %q — the component tier's declined candidates "+
			"are supposed to poison the global bare-name entry too. If they no longer "+
			"do, its fall-through can now mis-bind and needs the #6125 treatment", id)
	}

	recs := []types.EntityRecord{{
		ID: "c2c2c2c2c2c2c2c2", Kind: "Method", Name: "Server.Run",
		SourceFile: "svc/caller.go", Language: "go",
		Relationships: []types.RelationshipRecord{{
			FromID: "c2c2c2c2c2c2c2c2", ToID: "Server", Kind: "DEPENDS_ON",
		}},
	}}
	ReferencesEmbedded(recs, idx)
	if got := recs[0].Relationships[0].ToID; got != "Server" {
		t.Fatalf("ambiguous package-component fall-through bound to %q; want the stub "+
			"%q left verbatim", got, "Server")
	}
}
