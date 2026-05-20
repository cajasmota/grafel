package enrichment

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

func mkDoc(es ...graph.Entity) *graph.Document {
	return &graph.Document{
		Version:       graph.SchemaVersion,
		Repo:          "testrepo",
		Entities:      es,
		Relationships: nil,
	}
}

// Test 1: an entity with no description triggers exactly one
// describe_entity candidate.
func TestEmitFor_DescribeEntity_NoDescription(t *testing.T) {
	doc := mkDoc(graph.Entity{ID: "e1", Name: "AuthService", Kind: "class"})
	cands := CollectCandidates(doc, []CandidateEmitter{describeEntityEmitter{}}, nil)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	if cands[0].Kind != KindDescribeEntity {
		t.Fatalf("kind = %q, want %q", cands[0].Kind, KindDescribeEntity)
	}
	if cands[0].SubjectID != "e1" {
		t.Fatalf("subject_id = %q, want e1", cands[0].SubjectID)
	}
	// Already-described entity → no candidate.
	doc2 := mkDoc(graph.Entity{
		ID: "e2", Name: "X", Kind: "class",
		Properties: map[string]string{"description": "already set"},
	})
	if got := CollectCandidates(doc2, []CandidateEmitter{describeEntityEmitter{}}, nil); len(got) != 0 {
		t.Fatalf("expected 0 candidates for described entity, got %d", len(got))
	}
}

// Test 2: a god node triggers a describe_role candidate.
func TestEmitFor_DescribeRole_GodNode(t *testing.T) {
	doc := mkDoc(graph.Entity{ID: "g1", Name: "Coordinator", Kind: "class", IsGodNode: true})
	cands := CollectCandidates(doc, []CandidateEmitter{describeRoleEmitter{}}, nil)
	if len(cands) != 1 {
		t.Fatalf("expected 1 describe_role candidate, got %d", len(cands))
	}
	if cands[0].Kind != KindDescribeRole {
		t.Fatalf("kind = %q, want %q", cands[0].Kind, KindDescribeRole)
	}

	// Articulation-point also qualifies.
	doc2 := mkDoc(graph.Entity{ID: "a1", Name: "Bridge", Kind: "class", IsArticulationPt: true})
	if got := CollectCandidates(doc2, []CandidateEmitter{describeRoleEmitter{}}, nil); len(got) != 1 {
		t.Fatalf("articulation point: expected 1 candidate, got %d", len(got))
	}

	// A vanilla entity should NOT trigger describe_role.
	doc3 := mkDoc(graph.Entity{ID: "v1", Name: "Plain", Kind: "function"})
	if got := CollectCandidates(doc3, []CandidateEmitter{describeRoleEmitter{}}, nil); len(got) != 0 {
		t.Fatalf("vanilla entity: expected 0, got %d", len(got))
	}
}

// Test 3: emitting twice produces identical IDs (idempotence).
func TestEmit_Idempotent(t *testing.T) {
	doc := mkDoc(
		graph.Entity{ID: "e1", Name: "A", Kind: "class"},
		graph.Entity{ID: "e2", Name: "B", Kind: "class", IsGodNode: true},
	)
	first := CollectCandidates(doc, DefaultEmitters(), nil)
	second := CollectCandidates(doc, DefaultEmitters(), nil)
	if len(first) != len(second) {
		t.Fatalf("len mismatch: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("idempotence violated at %d: %q vs %q", i, first[i].ID, second[i].ID)
		}
		if first[i].Kind != second[i].Kind || first[i].SubjectID != second[i].SubjectID {
			t.Fatalf("(kind, subject) mismatch at %d", i)
		}
	}
}

// Test 4: rejected (subject_id, kind) pairs are not re-emitted.
func TestEmit_SkipsRejected(t *testing.T) {
	dir := t.TempDir()
	// Pre-seed rejections file.
	rej := []Rejection{{
		ID:        candidateID("e1", KindDescribeEntity),
		SubjectID: "e1",
		Kind:      KindDescribeEntity,
		Reason:    "irrelevant",
	}}
	data, _ := json.MarshalIndent(rej, "", "  ")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "enrichment-rejections.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	doc := mkDoc(
		graph.Entity{ID: "e1", Name: "Rejected", Kind: "class"},
		graph.Entity{ID: "e2", Name: "Allowed", Kind: "class"},
	)
	cands := CollectCandidatesSkippingRejected(doc, []CandidateEmitter{describeEntityEmitter{}}, dir)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate after rejection filter, got %d", len(cands))
	}
	if cands[0].SubjectID != "e2" {
		t.Fatalf("expected e2 to survive, got %q", cands[0].SubjectID)
	}
}

// Test 5: WriteCandidates and ReadResolutions / ApplyResolutions roundtrip.
func TestWriteCandidates_AndApplyResolutions(t *testing.T) {
	dir := t.TempDir()
	doc := mkDoc(graph.Entity{ID: "e1", Name: "A", Kind: "class"})
	cands := CollectCandidates(doc, DefaultEmitters(), nil)
	if err := WriteCandidates(dir, cands); err != nil {
		t.Fatalf("WriteCandidates: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "enrichment-candidates.json")); err != nil {
		t.Fatalf("candidates file not written: %v", err)
	}

	// Pre-seed a resolution and confirm ApplyResolutions writes it.
	resolutions := []Resolution{{
		ID:         "ec:any",
		SubjectID:  "e1",
		Kind:       "description",
		Value:      "An auth service.",
		Confidence: 0.9,
	}}
	doc2 := mkDoc(graph.Entity{ID: "e1", Name: "A", Kind: "class"})
	if got := ApplyResolutions(doc2, resolutions); got != 1 {
		t.Fatalf("ApplyResolutions = %d, want 1", got)
	}
	if doc2.Entities[0].Properties["description"] != "An auth service." {
		t.Fatalf("description not applied: %v", doc2.Entities[0].Properties)
	}
}

// Test 6 (issue #53): two consecutive WriteCandidates runs over the same
// input produce byte-identical output, even though emitters stamp
// DiscoveredAt with the wall clock between runs. This is the byte-stable
// idempotence guarantee the README and Pass 6 docstring promise.
func TestWriteCandidates_ByteIdenticalAcrossRuns(t *testing.T) {
	// Drive nowRFC3339 from a counter so each emit-pass produces a
	// different "current" timestamp. If WriteCandidates didn't preserve
	// the prior discovered_at, the second run's bytes would differ.
	origNow := nowRFC3339
	t.Cleanup(func() { nowRFC3339 = origNow })
	var ticks int
	nowRFC3339 = func() string {
		ticks++
		return time.Date(2026, 5, 9, 0, 0, ticks, 0, time.UTC).Format(time.RFC3339)
	}

	dir := t.TempDir()
	mkInput := func() *graph.Document {
		return mkDoc(
			graph.Entity{ID: "e1", Name: "AuthService", Kind: "class"},
			graph.Entity{ID: "g1", Name: "Coordinator", Kind: "class", IsGodNode: true},
			graph.Entity{ID: "a1", Name: "Bridge", Kind: "class", IsArticulationPt: true},
		)
	}

	// Run 1.
	first := CollectCandidates(mkInput(), DefaultEmitters(), nil)
	if err := WriteCandidates(dir, first); err != nil {
		t.Fatalf("first WriteCandidates: %v", err)
	}
	bytes1, err := os.ReadFile(filepath.Join(dir, "enrichment-candidates.json"))
	if err != nil {
		t.Fatalf("read after first run: %v", err)
	}

	// Run 2 — fresh emit pass, fresh "now" timestamps.
	second := CollectCandidates(mkInput(), DefaultEmitters(), nil)
	if err := WriteCandidates(dir, second); err != nil {
		t.Fatalf("second WriteCandidates: %v", err)
	}
	bytes2, err := os.ReadFile(filepath.Join(dir, "enrichment-candidates.json"))
	if err != nil {
		t.Fatalf("read after second run: %v", err)
	}

	if !bytes.Equal(bytes1, bytes2) {
		t.Fatalf("byte-stability violated:\n--- run 1 ---\n%s\n--- run 2 ---\n%s",
			string(bytes1), string(bytes2))
	}
}

// TestCollectCommunityCandidates verifies that one name_community candidate is
// emitted per community without an AgentName, and that communities that
// already have an AgentName are skipped.
func TestCollectCommunityCandidates(t *testing.T) {
	doc := &graph.Document{
		Communities: []graph.CommunityResult{
			{ID: 0, Size: 100, AutoName: "Order Fulfillment", TopEntities: []string{"e1", "e2"}},
			{ID: 1, Size: 50, AutoName: "Auth", AgentName: "AlreadyNamed", TopEntities: []string{"e3"}},
			{ID: 2, Size: 30, AutoName: "Payments", TopEntities: []string{"e4", "e5", "e6"}},
		},
	}
	cands := CollectCommunityCandidates(doc, nil)
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates (skip AlreadyNamed), got %d", len(cands))
	}
	for _, c := range cands {
		if c.Kind != KindNameCommunity {
			t.Errorf("kind = %q, want %q", c.Kind, KindNameCommunity)
		}
	}
	// IDs must be stable (deterministic).
	ids := map[string]bool{}
	for _, c := range cands {
		if ids[c.ID] {
			t.Errorf("duplicate candidate ID %q", c.ID)
		}
		ids[c.ID] = true
	}
	// Subject IDs use the "community:<id>" prefix.
	if cands[0].SubjectID != "community:0" {
		t.Errorf("subject_id = %q, want community:0", cands[0].SubjectID)
	}
	if cands[1].SubjectID != "community:2" {
		t.Errorf("subject_id = %q, want community:2", cands[1].SubjectID)
	}
}

// TestCollectCommunityCandidates_Rejected verifies that a rejected
// (community:<id>, name_community) pair produces no candidate.
func TestCollectCommunityCandidates_Rejected(t *testing.T) {
	doc := &graph.Document{
		Communities: []graph.CommunityResult{
			{ID: 0, Size: 10, AutoName: "Foo"},
		},
	}
	rejected := map[string]bool{
		"community:0|" + KindNameCommunity: true,
	}
	cands := CollectCommunityCandidates(doc, rejected)
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates after rejection, got %d", len(cands))
	}
}

// TestApplyCommunityNameResolutions verifies that matching resolutions are
// written onto the correct CommunityResult.AgentName and that non-matching
// kinds are ignored.
func TestApplyCommunityNameResolutions(t *testing.T) {
	doc := &graph.Document{
		Communities: []graph.CommunityResult{
			{ID: 0, AutoName: "Order Fulfillment"},
			{ID: 1, AutoName: "Auth"},
		},
	}
	resolutions := []Resolution{
		{SubjectID: "community:0", Kind: KindNameCommunity, Value: "OrderProcessing"},
		{SubjectID: "community:1", Kind: "describe_entity", Value: "should be ignored"},
		{SubjectID: "community:99", Kind: KindNameCommunity, Value: "no such community"},
	}
	n := ApplyCommunityNameResolutions(doc, resolutions)
	if n != 1 {
		t.Fatalf("applied = %d, want 1", n)
	}
	if doc.Communities[0].AgentName != "OrderProcessing" {
		t.Errorf("community 0 AgentName = %q, want OrderProcessing", doc.Communities[0].AgentName)
	}
	if doc.Communities[1].AgentName != "" {
		t.Errorf("community 1 AgentName = %q, want empty (wrong kind)", doc.Communities[1].AgentName)
	}
}

// ---------------------------------------------------------------------------
// Tests for issue #1130: noise-kind filtering + self-descriptive name skipping
// ---------------------------------------------------------------------------

// TestEmitFor_NoiseKinds verifies that entities whose kind is in the noise set
// produce no describe_entity candidate.
func TestEmitFor_NoiseKinds(t *testing.T) {
	noiseKinds := []string{
		"SCOPE.Pattern",
		"SCOPE.External",
		"SCOPE.Heading",
		"SCOPE.Stylesheet",
		"SCOPE.CodeBlock",
		"SCOPE.Document",
	}
	emitter := []CandidateEmitter{describeEntityEmitter{}}
	for _, kind := range noiseKinds {
		doc := mkDoc(graph.Entity{ID: "n1", Name: "SomeName", Kind: kind})
		got := CollectCandidates(doc, emitter, nil)
		if len(got) != 0 {
			t.Errorf("kind %q: expected 0 candidates (noise), got %d", kind, len(got))
		}
	}
}

// TestEmitFor_SelfDescriptiveOperation verifies that SCOPE.Operation entities
// whose name matches the self-descriptive pattern produce no candidate.
func TestEmitFor_SelfDescriptiveOperation(t *testing.T) {
	selfDescriptive := []string{
		"getUserById",
		"setUserName",
		"isAuthenticated",
		"hasPermission",
		"canDelete",
		"validateEmail",
		"parseToken",
		"formatDate",
		"createOrder",
		"deleteRecord",
		"fetchUser",
		"loadConfig",
		"saveSession",
		"sendNotification",
		"buildQuery",
		"renderPage",
		"onClick",
		"useEffect",
	}
	emitter := []CandidateEmitter{describeEntityEmitter{}}
	for _, name := range selfDescriptive {
		doc := mkDoc(graph.Entity{ID: "op1", Name: name, Kind: "SCOPE.Operation"})
		got := CollectCandidates(doc, emitter, nil)
		if len(got) != 0 {
			t.Errorf("name %q: expected 0 candidates (self-descriptive), got %d", name, len(got))
		}
	}
}

// TestEmitFor_AmbiguousOperation verifies that SCOPE.Operation entities with
// ambiguous names (single-word, no obvious verb+noun decomposition) DO produce
// a describe_entity candidate because an agent can add value.
func TestEmitFor_AmbiguousOperation(t *testing.T) {
	ambiguous := []string{
		"process",
		"handle",
		"execute",
		"run",
		"apply",
		"transform",
	}
	emitter := []CandidateEmitter{describeEntityEmitter{}}
	for _, name := range ambiguous {
		doc := mkDoc(graph.Entity{ID: "op2", Name: name, Kind: "SCOPE.Operation"})
		got := CollectCandidates(doc, emitter, nil)
		if len(got) != 1 {
			t.Errorf("name %q: expected 1 candidate (ambiguous), got %d", name, len(got))
		}
	}
}

// TestEmitFor_OperationWithDescription verifies that a SCOPE.Operation that
// already has a description set (self-descriptive or not) emits no candidate.
func TestEmitFor_OperationWithDescription(t *testing.T) {
	doc := mkDoc(graph.Entity{
		ID:   "op3",
		Name: "process",
		Kind: "SCOPE.Operation",
		Properties: map[string]string{
			"description": "Processes the incoming payload through the validation pipeline.",
		},
	})
	got := CollectCandidates(doc, []CandidateEmitter{describeEntityEmitter{}}, nil)
	if len(got) != 0 {
		t.Fatalf("Operation with description: expected 0 candidates, got %d", len(got))
	}
}

// TestEmitFor_NonNoiseKindStillEmits verifies that a non-noise kind (e.g.
// SCOPE.Class) still produces a candidate after the noise filter is applied,
// confirming the filter is not over-broad.
func TestEmitFor_NonNoiseKindStillEmits(t *testing.T) {
	normalKinds := []string{
		"SCOPE.Class",
		"SCOPE.Function",
		"SCOPE.Component",
		"SCOPE.Service",
	}
	emitter := []CandidateEmitter{describeEntityEmitter{}}
	for _, kind := range normalKinds {
		doc := mkDoc(graph.Entity{ID: "e1", Name: "AuthService", Kind: kind})
		got := CollectCandidates(doc, emitter, nil)
		if len(got) != 1 {
			t.Errorf("kind %q: expected 1 candidate (normal kind), got %d", kind, len(got))
		}
	}
}

// ---------------------------------------------------------------------------
// Tests for issue #1134 — EnrichmentTask (1-per-entity aggregated view)
// ---------------------------------------------------------------------------

// TestCollectTasks_ThreeActions verifies that an entity needing all three
// enrichment actions (describe_entity + classify_domain + describe_role) produces
// exactly ONE EnrichmentTask with THREE PendingActions.
func TestCollectTasks_ThreeActions(t *testing.T) {
	// God-node with no properties → qualifies for all three emitters.
	pr := 0.05
	doc := mkDoc(graph.Entity{
		ID:        "g1",
		Name:      "Coordinator",
		Kind:      "class",
		IsGodNode: true,
		PageRank:  &pr,
	})

	tasks := CollectTasks(doc, DefaultEmitters(), nil, nil)

	if len(tasks) != 1 {
		t.Fatalf("expected 1 EnrichmentTask (1 entity), got %d", len(tasks))
	}
	task := tasks[0]
	if task.SubjectID != "g1" {
		t.Errorf("SubjectID = %q, want g1", task.SubjectID)
	}
	if len(task.PendingActions) != 3 {
		t.Errorf("PendingActions count = %d, want 3; got %v", len(task.PendingActions), task.PendingActions)
	}
	// No action should be marked completed.
	for _, a := range task.PendingActions {
		if a.Completed {
			t.Errorf("action %q unexpectedly marked completed", a.Kind)
		}
	}
}

// TestCollectTasks_OneAction verifies that an entity needing only describe_entity
// produces one task with one action.
func TestCollectTasks_OneAction(t *testing.T) {
	doc := mkDoc(graph.Entity{ID: "e1", Name: "PaymentHandler", Kind: "class"})

	tasks := CollectTasks(doc, DefaultEmitters(), nil, nil)

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if len(tasks[0].PendingActions) != 1 {
		t.Errorf("expected 1 action (describe_entity), got %d", len(tasks[0].PendingActions))
	}
	if tasks[0].PendingActions[0].Kind != KindDescribeEntity {
		t.Errorf("action kind = %q, want %q", tasks[0].PendingActions[0].Kind, KindDescribeEntity)
	}
}

// TestCollectTasks_CompletedActionRemains verifies that completing one action
// leaves the others pending — the task is NOT removed when a partial resolution
// is present.
func TestCollectTasks_CompletedActionRemains(t *testing.T) {
	pr := 0.05
	doc := mkDoc(graph.Entity{
		ID:        "g1",
		Name:      "Coordinator",
		Kind:      "class",
		IsGodNode: true,
		PageRank:  &pr,
	})

	// Mark describe_entity as resolved; classify_domain and describe_role still pending.
	resolved := map[string]bool{
		"g1|" + KindDescribeEntity: true,
	}

	tasks := CollectTasks(doc, DefaultEmitters(), nil, resolved)

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task (entity still has pending actions), got %d", len(tasks))
	}
	var completed, pending int
	for _, a := range tasks[0].PendingActions {
		if a.Completed {
			completed++
		} else {
			pending++
		}
	}
	if completed != 1 {
		t.Errorf("completed actions = %d, want 1", completed)
	}
	if pending != 2 {
		t.Errorf("pending actions = %d, want 2", pending)
	}
}

// TestCollectTasks_UniqueSubjectCount verifies that CollectTasks returns one
// task per unique entity regardless of how many actions it needs.
func TestCollectTasks_UniqueSubjectCount(t *testing.T) {
	pr := 0.05
	doc := mkDoc(
		graph.Entity{ID: "e1", Name: "Plain", Kind: "class"},                                              // 1 action
		graph.Entity{ID: "g1", Name: "God", Kind: "class", IsGodNode: true, PageRank: &pr},                // 3 actions
		graph.Entity{ID: "a1", Name: "Bridge", Kind: "class", IsArticulationPt: true},                    // 2 actions (describe+role)
	)

	tasks := CollectTasks(doc, DefaultEmitters(), nil, nil)

	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks (unique entities), got %d", len(tasks))
	}

	// Verify the flat candidate count would be > unique entity count.
	flat := CollectCandidates(doc, DefaultEmitters(), nil)
	if len(flat) <= len(tasks) {
		t.Errorf("flat candidates (%d) should be > unique tasks (%d)", len(flat), len(tasks))
	}
}

// TestCollectTasks_RejectedActionSkipped verifies that a rejected (subject, kind)
// pair does not appear in the task's PendingActions.
func TestCollectTasks_RejectedActionSkipped(t *testing.T) {
	doc := mkDoc(graph.Entity{
		ID:        "g1",
		Name:      "Coordinator",
		Kind:      "class",
		IsGodNode: true,
	})

	rejected := map[string]bool{
		"g1|" + KindDescribeEntity: true,
	}

	tasks := CollectTasks(doc, DefaultEmitters(), rejected, nil)

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	for _, a := range tasks[0].PendingActions {
		if a.Kind == KindDescribeEntity {
			t.Errorf("rejected action %q should not appear", KindDescribeEntity)
		}
	}
}

// TestCandidatesFromTasks verifies that the flat backward-compat adapter
// returns only pending (not completed) actions as Candidate records.
func TestCandidatesFromTasks(t *testing.T) {
	tasks := []EnrichmentTask{
		{
			SubjectID:   "e1",
			SubjectKind: "class",
			PendingActions: []EnrichmentAction{
				{Kind: KindDescribeEntity, CandidateID: "ec:aaa", Score: 0.6, Completed: false},
				{Kind: KindClassifyDomain, CandidateID: "ec:bbb", Score: 0.5, Completed: true},
			},
		},
	}
	flat := CandidatesFromTasks(tasks)
	if len(flat) != 1 {
		t.Fatalf("expected 1 candidate (completed excluded), got %d", len(flat))
	}
	if flat[0].Kind != KindDescribeEntity {
		t.Errorf("candidate kind = %q, want %q", flat[0].Kind, KindDescribeEntity)
	}
}

// TestUniqueSubjectCount verifies the helper counts distinct SubjectIDs.
func TestUniqueSubjectCount(t *testing.T) {
	cs := []Candidate{
		{ID: "a", SubjectID: "e1", Kind: KindDescribeEntity},
		{ID: "b", SubjectID: "e1", Kind: KindClassifyDomain},
		{ID: "c", SubjectID: "e2", Kind: KindDescribeEntity},
	}
	if n := UniqueSubjectCount(cs); n != 2 {
		t.Errorf("UniqueSubjectCount = %d, want 2", n)
	}
}
