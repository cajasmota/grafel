package mcp

import "testing"

// Issue #6010: a repo whose graph failed to load carries Doc == nil and
// Reader == nil (reloadLocked records loadErr and `continue`s before either is
// set). A keepReader evict copies that repo into a cold shell verbatim, and the
// revive calls rematerializeFromReader on it. With Reader nil the else branch
// runs BuildLabelIndex(nil), which dereferences doc.Entities and panics — taking
// down `serve` on a routine evict/revive cycle.
//
// MUTATION ORACLE: drop the `doc == nil` guard from BuildLabelIndex (or the
// nil-Doc branch in rematerializeFromReader) → this test panics with
// "invalid memory address or nil pointer dereference" instead of failing cleanly.
func TestEvictRevive_RepoWithFailedGraphLoadDoesNotPanic(t *testing.T) {
	s := NewState(&Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{"broken": {}}},
	}})
	// Exactly the shape reloadLocked leaves behind when readDocumentFromDir
	// fails: no Doc, no Reader, a recorded load error.
	lr := &LoadedRepo{Repo: "broken", loadErr: "read graph.fb: unexpected EOF"}
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"broken": lr}}

	if !s.EvictGroup("g", true) {
		t.Fatal("EvictGroup(keepReader) returned false")
	}

	// The revive path; on d2a7f28ae this panics inside BuildLabelIndex(nil).
	got := s.Group("g")
	if got == nil {
		t.Fatal("revive returned a nil group")
	}
	rr := got.Repos["broken"]
	if rr == nil {
		t.Fatal("revive dropped the broken repo from the group")
	}
	// The repo must come back usable-but-empty rather than half-built: a nil
	// LabelIndex would move the same panic to the first lookup.
	if rr.LabelIndex == nil {
		t.Fatal("revive left LabelIndex nil for a repo with no Doc")
	}
	// And an actual lookup against it must be a clean miss, not a crash.
	if e := rr.LabelIndex.ByID("anything"); e != nil {
		t.Fatalf("empty LabelIndex reported a hit for an unknown id: %v", e)
	}
}

// BuildLabelIndex(nil) is reachable from any caller that has not yet parsed a
// graph; it must yield a usable empty index rather than panicking.
func TestBuildLabelIndex_NilDocument(t *testing.T) {
	idx := BuildLabelIndex(nil)
	if idx == nil {
		t.Fatal("BuildLabelIndex(nil) returned nil")
	}
	if e := idx.ByID("x"); e != nil {
		t.Fatalf("nil-doc index reported a hit: %v", e)
	}
	if got := idx.LookupAll("x"); len(got) != 0 {
		t.Fatalf("nil-doc index LookupAll returned %d entries, want 0", len(got))
	}
}
