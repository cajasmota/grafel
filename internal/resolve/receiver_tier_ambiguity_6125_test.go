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
// # Why the fall-through, and not the comment, was the defect
//
// The two candidate indexes are keyed differently, and that asymmetry decides
// it:
//
//   - byPackageMember is built by splitting an entity Name at its LAST dot
//     (refs.go:1208-1228), so the candidates the receiver tier declines
//     between are named "<Recv>.<member>".
//   - byName is keyed on the WHOLE entity Name (refs.go:1474, 1497).
//
// So byName[member] can NEVER contain a declined candidate. The downstream
// tier is not making a better choice among the plausible targets — it is
// structurally incapable of returning any of them. Whatever it returns is by
// construction a different entity, usually in a foreign package. That is a
// confident wrong bind, and an honest dangle beats it.
//
// The contract was already documented on the helper itself, twice
// (refs.go:2757-2762 and 2779-2781): ("", true) exists "so the caller can
// leave the stub alone instead of falling back to global bare-name lookup
// (which would risk binding to a foreign-package method of the same name)".
// Two statements of intent on the helper, one contradicting call site — the
// call site was the outlier. internal/extractors/sresolver already fixed the
// identical misconception for #6098, so the full-rebuild and incremental
// paths disagreed on this exact input until now.
//
// Nothing below counts dangling edges. For this issue a count is actively
// deceptive: the two candidate fixes move it in opposite directions.

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
				"The receiver tier declined between %s/a.go and %s/b.go; every entity "+
				"it declined between is named %q, so byName[%q] cannot contain any of "+
				"them and this bind is by construction a non-candidate.",
				e.Kind, e.Name, e.SourceFile, recvPkg6125, recvPkg6125,
				recvType6125+"."+recvMember6125, recvMember6125)
		}
	}
	if got != recvMember6125 {
		t.Fatalf("ambiguous receiver rewrote the ToID to %q; want the stub %q left "+
			"verbatim so the edge dangles honestly", got, recvMember6125)
	}
}

// TestReceiverTierRefusalCountsAsAmbiguousNotResolved6125 pins the bookkeeping
// alongside the binding. A refusal that silently vanished from the stats would
// be indistinguishable from a resolution to any downstream consumer of Stats.
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
// byPackageComponent is keyed on the dot-FREE bare entity Name
// (refs.go:1419-1427). So when it is ambiguous, its candidates are indexed in
// byName under that identical string, and byName is necessarily ambiguous
// too. The fall-through therefore cannot produce a confident wrong bind — it
// dangles either way, and its inline comment says so accurately.
//
// If that keying ever changes, this test fails and the exemption must be
// re-argued rather than assumed.
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
