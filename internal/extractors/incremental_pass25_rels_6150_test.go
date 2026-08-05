// Package extractors — #6150 unit coverage for the Pass-2.5 standalone
// relationship binder.
//
// IN-PACKAGE (`extractors`, not `extractors_test`) because
// `bindPass25StubEndpoints` is unexported. The end-to-end proof that these
// edges reach the persisted graph is the #6129 content-parity gate in
// cmd/grafel; what is asserted HERE is the part the gate cannot separate: WHICH
// stub binds, which one is refused, and that a refusal drops the row rather
// than emitting an unbound endpoint.
//
// The refusal half is the load-bearing one. #6148's first attempt at consuming
// Detect's relationships emitted them verbatim and landed
// `«unbound»Controller:X → «unbound»Service:y` plus duplicate DEPENDS_ON rows,
// because the stubs are `Kind:Name` structural refs that the full path binds in
// a corpus-wide resolver pass this path does not run. Binding in-file and
// DROPPING the rest is what makes consuming them an improvement rather than a
// second defect, so a change that starts emitting unbound rows again must fail
// here.
package extractors

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

func p25rec(kind, name, file string) types.EntityRecord {
	return types.EntityRecord{Kind: kind, Name: name, SourceFile: file}
}

// p25find returns the record with the given kind+name, or nil.
func p25find(recs []types.EntityRecord, kind, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind == kind && recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

// TestBindPass25StubEndpoints_BindsBothEndsInFile is the #6150 headline: the
// falcon `app.add_route(...)` REGISTERED_ON rule emits
// `Controller:CpPlainProbe → Service:cp_app`, both of which name records in the
// file being re-extracted. The edge must land OWNED by the source record, with
// the TARGET rewritten to the target record's deterministic entity id.
func TestBindPass25StubEndpoints_BindsBothEndsInFile(t *testing.T) {
	recs := []types.EntityRecord{
		p25rec("Controller", "Probe", "h.py"),
		p25rec("Service", "app", "h.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID: "Controller:Probe",
		ToID:   "Service:app",
		Kind:   "REGISTERED_ON",
	}}

	out, bound, dropped := bindPass25StubEndpoints(recs, rels, "test-repo")
	if bound != 1 || dropped != 0 {
		t.Fatalf("bound=%d dropped=%d; want 1/0", bound, dropped)
	}
	src := p25find(out, "Controller", "Probe")
	if src == nil || len(src.Relationships) != 1 {
		t.Fatalf("source record did not receive the edge: %+v", src)
	}
	got := src.Relationships[0]
	if got.Kind != "REGISTERED_ON" {
		t.Errorf("kind = %q, want REGISTERED_ON", got.Kind)
	}
	// FromID must be EMPTY, meaning "my owner" — relRecordToGraphRel
	// substitutes the owning entity's id, and that substitution is what makes
	// the edge visible to the FromID-keyed stale-edge eviction (#6094).
	if got.FromID != "" {
		t.Errorf("FromID = %q, want empty (owner substitution)", got.FromID)
	}
	wantTo := graph.EntityID("test-repo", "Service", "app", "h.py")
	if got.ToID != wantTo {
		t.Errorf("ToID = %q, want the Service record's entity id %q", got.ToID, wantTo)
	}
	// The target record must NOT also carry a copy.
	if tgt := p25find(out, "Service", "app"); tgt != nil && len(tgt.Relationships) != 0 {
		t.Errorf("target record carries %d edge(s); the edge is owned by the source only",
			len(tgt.Relationships))
	}
}

// TestBindPass25StubEndpoints_DropsUnbindableStub: a stub naming an entity that
// is not among this file's records is DROPPED, not emitted with the raw text as
// an endpoint. This is the assertion that keeps the #6148 measurement — unbound
// `«unbound»Controller:X` rows in the persisted graph — from coming back.
func TestBindPass25StubEndpoints_DropsUnbindableStub(t *testing.T) {
	recs := []types.EntityRecord{p25rec("Controller", "Probe", "h.py")}
	rels := []types.RelationshipRecord{
		{FromID: "Controller:Probe", ToID: "Service:elsewhere", Kind: "REGISTERED_ON"},
		{FromID: "Controller:absent", ToID: "Controller:Probe", Kind: "REGISTERED_ON"},
	}

	out, bound, dropped := bindPass25StubEndpoints(recs, rels, "test-repo")
	if bound != 0 || dropped != 2 {
		t.Fatalf("bound=%d dropped=%d; want 0/2", bound, dropped)
	}
	for i := range out {
		if len(out[i].Relationships) != 0 {
			t.Fatalf("record %s|%s received %d edge(s); an unbindable stub must emit nothing",
				out[i].Kind, out[i].Name, len(out[i].Relationships))
		}
	}
}

// TestBindPass25StubEndpoints_RewritesEmbeddedStubs covers the OTHER stub
// producer on this path: engine.ResolveHTTPEndpointHandlers appends the
// endpoint→handler IMPLEMENTS bridge as an EMBEDDED edge whose endpoints are
// the same `Kind:Name` form. Those must be rewritten in place, or the bridge
// the full rebuild binds lands unbound here.
func TestBindPass25StubEndpoints_RewritesEmbeddedStubs(t *testing.T) {
	handler := p25rec("SCOPE.Operation", "Probe.on_get", "h.py")
	handler.Relationships = []types.RelationshipRecord{{
		FromID: "SCOPE.Operation:Probe.on_get",
		ToID:   "http_endpoint_definition:http:GET:/things",
		Kind:   "IMPLEMENTS",
	}}
	recs := []types.EntityRecord{
		handler,
		p25rec("http_endpoint_definition", "http:GET:/things", "h.py"),
	}

	out, _, _ := bindPass25StubEndpoints(recs, nil, "test-repo")
	got := p25find(out, "SCOPE.Operation", "Probe.on_get").Relationships[0]
	if want := graph.EntityID("test-repo", "SCOPE.Operation", "Probe.on_get", "h.py"); got.FromID != want {
		t.Errorf("FromID = %q, want %q", got.FromID, want)
	}
	// The endpoint's NAME itself contains colons ("http:GET:/things"). The
	// lookup key is the whole `Kind + ":" + Name` string, so this resolves; a
	// binder that split on the first colon would look up kind
	// "http_endpoint_definition", name "http" and miss.
	if want := graph.EntityID("test-repo", "http_endpoint_definition", "http:GET:/things", "h.py"); got.ToID != want {
		t.Errorf("ToID = %q, want %q", got.ToID, want)
	}
}

// TestBindPass25StubEndpoints_LeavesNonStubEndpointsAlone: the binder must be
// invisible to every edge that is not a `Kind:Name` stub matching a record of
// this file. A bare call target ("len"), a dotted import ("cfg.SETTING") and an
// already-stamped hex id all pass through untouched — the scoped resolver owns
// those, and rewriting them here would take the binding away from the pass that
// knows how to do it corpus-wide.
func TestBindPass25StubEndpoints_LeavesNonStubEndpointsAlone(t *testing.T) {
	caller := p25rec("SCOPE.Operation", "cp_use", "h.py")
	stamped := graph.EntityID("test-repo", "Module", "other", "other.py")
	caller.Relationships = []types.RelationshipRecord{
		{ToID: "len", Kind: "CALLS"},
		{ToID: "cfg.SETTING", Kind: "IMPORTS"},
		{ToID: stamped, Kind: "REFERENCES"},
		{ToID: "", Kind: "CALLS"},
	}
	recs := []types.EntityRecord{caller, p25rec("Controller", "len", "h.py")}

	out, _, _ := bindPass25StubEndpoints(recs, nil, "test-repo")
	got := p25find(out, "SCOPE.Operation", "cp_use").Relationships
	want := []string{"len", "cfg.SETTING", stamped, ""}
	for i, w := range want {
		if got[i].ToID != w {
			t.Errorf("rel %d ToID = %q, want %q (unchanged)", i, got[i].ToID, w)
		}
	}
}

// TestBindPass25StubEndpoints_NoCrossFileBind: two records with the same
// Kind:Name in DIFFERENT files are both present (the caller hands this function
// one file's records, but nothing in the type system enforces it). The bind is
// keyed on file as well as kind/name, so a stub can only reach a record of the
// file that produced the relationship — the source record's own file.
func TestBindPass25StubEndpoints_NoCrossFileBind(t *testing.T) {
	recs := []types.EntityRecord{
		p25rec("Controller", "Probe", "a.py"),
		p25rec("Service", "app", "b.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID: "Controller:Probe", ToID: "Service:app", Kind: "REGISTERED_ON",
	}}
	_, bound, dropped := bindPass25StubEndpoints(recs, rels, "test-repo")
	if bound != 0 || dropped != 1 {
		t.Fatalf("bound=%d dropped=%d; want 0/1 (target lives in another file)", bound, dropped)
	}
}

// TestBindPass25StubEndpoints_AmbiguousStubIsRefused: when two records of the
// SAME file share a Kind:Name, the stub names no single entity. Binding it
// would be a coin flip that a full rebuild's resolver (which has an ambiguity
// sentinel) refuses, so this refuses too — a wrong bind reads as an improvement
// on every count-based metric (#6123).
func TestBindPass25StubEndpoints_AmbiguousStubIsRefused(t *testing.T) {
	a := p25rec("Service", "app", "h.py")
	a.StartLine = 1
	b := p25rec("Service", "app", "h.py")
	b.StartLine = 9
	recs := []types.EntityRecord{p25rec("Controller", "Probe", "h.py"), a, b}
	rels := []types.RelationshipRecord{{
		FromID: "Controller:Probe", ToID: "Service:app", Kind: "REGISTERED_ON",
	}}
	_, bound, dropped := bindPass25StubEndpoints(recs, rels, "test-repo")
	if bound != 0 || dropped != 1 {
		t.Fatalf("bound=%d dropped=%d; want 0/1 (ambiguous target)", bound, dropped)
	}
}

// TestEntityRecordToGraphEntity_DerivesIDIgnoringRecordID pins the record→graph
// seam's id rule (#6150).
//
// engine's http_endpoint synthesis stamps EntityRecord.ID with the ROUTABLE
// FORM of the endpoint — literally "http:GET:/things" (#1725, where the same
// string is also used as the QualifiedName). buildDocument overwrites it: every
// entity it emits gets graph.EntityID(repo, kind, name, source_file) and the
// record's own ID is never read. This path honoured it, so the SAME endpoint
// carried a deterministic hex id after a full rebuild and a raw routable string
// after an incremental one.
//
// That is not cosmetic. Entity ids are what every edge endpoint, the
// FlatBuffers `(key)` binary search behind LookupEntityByID (#5974) and the
// flow passes' entry_id/chain/branches_dag properties join on — the last of
// which made it visible: the incremental SCOPE.Process node described its entry
// as "http:GET:/cpthings" where the full rebuild had the hex.
func TestEntityRecordToGraphEntity_DerivesIDIgnoringRecordID(t *testing.T) {
	rec := types.EntityRecord{
		ID:         "http:GET:/things",
		Kind:       "http_endpoint_definition",
		Name:       "http:GET:/things",
		SourceFile: "h.py",
	}
	got := entityRecordToGraphEntity(rec, "test-repo")
	want := graph.EntityID("test-repo", "http_endpoint_definition", "http:GET:/things", "h.py")
	if got.ID != want {
		t.Errorf("ID = %q, want the derived %q — the record's own ID must not win", got.ID, want)
	}
	if got.ID == rec.ID {
		t.Errorf("ID is the record's stamped routable form %q; buildDocument never emits that", rec.ID)
	}
}
