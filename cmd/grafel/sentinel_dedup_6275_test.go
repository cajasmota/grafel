package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractors/cross/ormlink"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6275 (third piece) — the #4406 dedup-by-ID path in buildDocument
// picks its property-authoritative survivor by first-writer-wins order.
// sortEntityRecords (cmd/grafel/determinism.go, mirrors types.
// SortEntityRecordsCanonical) orders same-(Kind,Name,SourceFile) records by
// StartLine ascending; ormlink's SubtypeSentinel is always emitted with
// StartLine 0 (internal/extractors/cross/ormlink/extractor.go:626), so it
// deterministically sorts BEFORE the real class node and becomes the
// survivor whose Subtype/QualifiedName win. ormlink's own doc comment
// (extractor.go:46-49) says the sentinel is "intentionally low-quality...
// so it doesn't compete with the real class entity" — but until this fix
// nothing enforced that at the dedup boundary, so a real class record
// colliding with the sentinel on graph.EntityID was permanently mislabeled
// subtype=orm_model_sentinel even after it gained real content (edges,
// span, signature) via gap-fill.
func TestBuildDocument_RealClassWinsIdentityOverOrmSentinel(t *testing.T) {
	const (
		repo = "test_repo"
		file = "com/example/demo/model/User.java"
		name = "User"
	)

	// Sentinel arrives FIRST in the pre-sorted slice (sortEntityRecords would
	// always place it first anyway, since its StartLine is 0) — same
	// (Kind, Name, SourceFile) as the real class, so it collides on
	// graph.EntityID and becomes the #4406 survivor.
	sentinel := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype:       ormlink.SubtypeSentinel,
		QualifiedName: "scope:ormmodel:" + file + "#" + name,
		StartLine:     0, EndLine: 0,
	}
	realClass := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype:       "class",
		QualifiedName: "com.example.demo.model.User",
		StartLine:     7, EndLine: 20,
		Signature: "@Entity class User",
	}

	idx := &Indexer{repoTag: repo}
	pass1 := []types.EntityRecord{sentinel, realClass}
	doc := idx.buildDocument(&pass1, nil, nil, nil)

	got := findEntity(t, doc.Entities, graph.EntityID(repo, "SCOPE.Component", name, file))
	if got.Subtype == ormlink.SubtypeSentinel {
		t.Errorf("survivor Subtype = %q; the real class record must win identity over the sentinel", got.Subtype)
	}
	if got.Subtype != "class" {
		t.Errorf("survivor Subtype = %q; want %q (the real class record's own subtype)", got.Subtype, "class")
	}
	// Positive promotion, not merely "differs from the sentinel's" — an empty
	// QualifiedName also differs from the sentinel's and would wrongly pass a
	// weaker assertion (see TestBuildDocument_RealClassQualifiedNameNotLostWhenDuplicateHasNone
	// for the case where there is nothing to promote).
	if got.QualifiedName != realClass.QualifiedName {
		t.Errorf("survivor QualifiedName = %q; want promoted to the real class record's %q",
			got.QualifiedName, realClass.QualifiedName)
	}
	if got.StartLine != 7 || got.EndLine != 20 {
		t.Errorf("survivor span = %d-%d; want 7-20 gap-filled from the real class record", got.StartLine, got.EndLine)
	}
}

// TestBuildDocument_RealClassQualifiedNameNotLostWhenDuplicateHasNone — the
// sentinel's QualifiedName is blanked ONLY when the colliding real record has
// one to replace it with. QualifiedName drives byQualifiedName resolution and
// cross-repo joins (see the #4406 comment above the dedup branch), so if the
// real record carries none, blanking the sentinel's synthetic
// "scope:ormmodel:..." value would leave the survivor with NO QualifiedName
// at all once the gap-fill below finds nothing to restore it with — strictly
// worse than the sentinel's synthetic value. Subtype still promotes: this
// branch's own guard already requires the colliding record's Subtype to be
// non-empty, so Subtype's gap-fill always has a real value.
func TestBuildDocument_RealClassQualifiedNameNotLostWhenDuplicateHasNone(t *testing.T) {
	const (
		repo = "test_repo"
		file = "com/example/demo/model/Account.java"
		name = "Account"
	)
	sentinelQName := "scope:ormmodel:" + file + "#" + name
	sentinel := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype:       ormlink.SubtypeSentinel,
		QualifiedName: sentinelQName,
	}
	// Real class record with NO QualifiedName — the extractor didn't stamp
	// one (a real, reachable shape: not every language extractor sets it).
	realClassNoQName := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype: "class", StartLine: 4, EndLine: 10,
	}

	idx := &Indexer{repoTag: repo}
	pass1 := []types.EntityRecord{sentinel, realClassNoQName}
	doc := idx.buildDocument(&pass1, nil, nil, nil)

	got := findEntity(t, doc.Entities, graph.EntityID(repo, "SCOPE.Component", name, file))
	if got.Subtype != "class" {
		t.Errorf("survivor Subtype = %q; want %q (still promoted)", got.Subtype, "class")
	}
	if got.QualifiedName != sentinelQName {
		t.Errorf("survivor QualifiedName = %q; want the sentinel's synthetic %q PRESERVED "+
			"(the colliding record had none to promote, so blanking it would lose it entirely)",
			got.QualifiedName, sentinelQName)
	}
}

// TestBuildDocument_SentinelVsSentinelNeverBlanked — issue #6275 mutant guard.
// A mutation that widens the dedup branch's condition to
// `surv.Subtype == ormlink.SubtypeSentinel` alone (dropping the r.Subtype
// checks) survives every other test in this file, because none of them feed
// TWO sentinels or a sentinel colliding with a record whose Subtype is empty.
// Two ormlink sentinels for the same (Kind, Name, SourceFile) is reachable —
// nothing upstream guarantees ormlink emits at most one — and under the
// widened mutant the second one would blank the first's Subtype/QualifiedName
// even though it has nothing real to replace them with, since r.Subtype ==
// ormlink.SubtypeSentinel too (not the "" the guard's r.Subtype != ""
// condition is there to reject as "nothing to promote").
func TestBuildDocument_SentinelVsSentinelNeverBlanked(t *testing.T) {
	const (
		repo = "test_repo"
		file = "com/example/demo/model/Widget.java"
		name = "Widget"
	)
	qname := "scope:ormmodel:" + file + "#" + name
	first := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype: ormlink.SubtypeSentinel, QualifiedName: qname,
	}
	// second carries a DIFFERENT QualifiedName than first's — a real (if
	// unlikely) shape if ormlink somehow double-emitted with a distinct ref
	// string. Deliberately distinct from first's, not merely equal: an
	// identical second record would round-trip through
	// "blank then gap-fill from r" to the SAME value under both the correct
	// guard and the widened mutant, so it would kill nothing. Distinct values
	// make the two paths diverge and observable.
	second := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype: ormlink.SubtypeSentinel, QualifiedName: qname + "-DUPLICATE",
	}

	idx := &Indexer{repoTag: repo}
	pass1 := []types.EntityRecord{first, second}
	doc := idx.buildDocument(&pass1, nil, nil, nil)

	got := findEntity(t, doc.Entities, graph.EntityID(repo, "SCOPE.Component", name, file))
	if got.Subtype != ormlink.SubtypeSentinel {
		t.Errorf("survivor Subtype = %q; want unchanged %q — a second sentinel is not a real collision partner",
			got.Subtype, ormlink.SubtypeSentinel)
	}
	// Must keep the FIRST (survivor's own) QualifiedName, never the second
	// sentinel's — a sentinel is never a legitimate promotion source.
	if got.QualifiedName != qname {
		t.Errorf("survivor QualifiedName = %q; want unchanged %q (the survivor's own, not the second sentinel's %q)",
			got.QualifiedName, qname, second.QualifiedName)
	}
}

// TestBuildDocument_SentinelVsEmptySubtypeNeverBlanked — issue #6275 mutant
// guard, the second reachable shape the widened mutant in the comment above
// mishandles: a colliding record whose Subtype is legitimately empty (no
// subtype stamped at all). The current guard's r.Subtype != "" condition
// exists precisely so this case is left alone — there is nothing for the
// gap-fill to promote, so blanking the sentinel's identity here would strand
// the survivor with an empty Subtype AND an empty QualifiedName, losing
// identity entirely rather than gaining a real one.
func TestBuildDocument_SentinelVsEmptySubtypeNeverBlanked(t *testing.T) {
	const (
		repo = "test_repo"
		file = "com/example/demo/model/Gizmo.java"
		name = "Gizmo"
	)
	qname := "scope:ormmodel:" + file + "#" + name
	sentinel := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype: ormlink.SubtypeSentinel, QualifiedName: qname,
	}
	blankSubtype := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype: "", QualifiedName: "some.other.Name", StartLine: 2,
	}

	idx := &Indexer{repoTag: repo}
	pass1 := []types.EntityRecord{sentinel, blankSubtype}
	doc := idx.buildDocument(&pass1, nil, nil, nil)

	got := findEntity(t, doc.Entities, graph.EntityID(repo, "SCOPE.Component", name, file))
	if got.Subtype != ormlink.SubtypeSentinel {
		t.Errorf("survivor Subtype = %q; want unchanged %q — the colliding record's Subtype is empty, nothing to promote",
			got.Subtype, ormlink.SubtypeSentinel)
	}
	if got.QualifiedName != qname {
		t.Errorf("survivor QualifiedName = %q; want unchanged %q", got.QualifiedName, qname)
	}
}

// TestBuildDocument_SentinelAloneIsUnaffected — when the sentinel is the ONLY
// record for its (Kind, Name, SourceFile) — no real class node collided with
// it — it must be emitted unchanged. The #6275 fix only demotes the
// sentinel's identity fields when a genuine non-sentinel duplicate exists.
func TestBuildDocument_SentinelAloneIsUnaffected(t *testing.T) {
	const (
		repo = "test_repo"
		file = "com/example/demo/model/Order.java"
		name = "Order"
	)
	sentinel := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype:       ormlink.SubtypeSentinel,
		QualifiedName: "scope:ormmodel:" + file + "#" + name,
	}

	idx := &Indexer{repoTag: repo}
	pass1 := []types.EntityRecord{sentinel}
	doc := idx.buildDocument(&pass1, nil, nil, nil)

	got := findEntity(t, doc.Entities, graph.EntityID(repo, "SCOPE.Component", name, file))
	if got.Subtype != ormlink.SubtypeSentinel {
		t.Errorf("survivor Subtype = %q; want unchanged %q (no collision partner)", got.Subtype, ormlink.SubtypeSentinel)
	}
	if got.QualifiedName != sentinel.QualifiedName {
		t.Errorf("survivor QualifiedName = %q; want unchanged %q", got.QualifiedName, sentinel.QualifiedName)
	}
}
