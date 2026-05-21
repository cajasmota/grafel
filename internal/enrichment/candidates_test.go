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

// Test 1: a qualifying entity with no description triggers exactly one
// describe_entity candidate. Positive selection — only entities that hit a
// research-validated signal are emitted (issue #1162).
func TestEmitFor_DescribeEntity_NoDescription(t *testing.T) {
	// http_endpoint qualifies (public API surface — always signal 1).
	doc := mkDoc(graph.Entity{ID: "e1", Name: "http:GET:/api/users", Kind: "http_endpoint"})
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
	if len(cands[0].QualificationSignals) == 0 {
		t.Fatalf("expected qualification_signals to be set, got empty")
	}

	// Already-described qualifying entity → no candidate.
	doc2 := mkDoc(graph.Entity{
		ID: "e2", Name: "http:POST:/api/orders", Kind: "http_endpoint",
		Properties: map[string]string{"description": "already set"},
	})
	if got := CollectCandidates(doc2, []CandidateEmitter{describeEntityEmitter{}}, nil); len(got) != 0 {
		t.Fatalf("expected 0 candidates for described entity, got %d", len(got))
	}

	// Generic "class" kind does NOT qualify under positive selection.
	doc3 := mkDoc(graph.Entity{ID: "e3", Name: "AuthService", Kind: "class"})
	if got := CollectCandidates(doc3, []CandidateEmitter{describeEntityEmitter{}}, nil); len(got) != 0 {
		t.Fatalf("expected 0 candidates for non-qualifying kind, got %d", len(got))
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
// Both entities are http_endpoints (qualifying kind) so the rejection filter
// is what determines the count, not positive selection.
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
		graph.Entity{ID: "e1", Name: "http:GET:/api/rejected", Kind: "http_endpoint"},
		graph.Entity{ID: "e2", Name: "http:GET:/api/allowed", Kind: "http_endpoint"},
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
// ambiguous names (short all-lowercase, no obvious verb+noun decomposition)
// DO produce a describe_entity candidate because an agent can add value.
// The ambiguous-name signal fires for names ≤ 9 chars, all-lowercase, not
// matching the self-descriptive verb+Noun pattern (issue #1162).
func TestEmitFor_AmbiguousOperation(t *testing.T) {
	ambiguous := []string{
		"process",   // 7 chars, all-lowercase
		"handle",    // 6 chars, all-lowercase
		"execute",   // 7 chars, all-lowercase
		"run",       // 3 chars, all-lowercase
		"apply",     // 5 chars, all-lowercase
		"transform", // 9 chars, all-lowercase
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

// TestEmitFor_PositiveSelection verifies that qualifying kinds emit candidates
// and non-qualifying kinds do not. Under positive selection (issue #1162) the
// default policy is NOT to enrich; an entity must hit a positive signal.
func TestEmitFor_PositiveSelection(t *testing.T) {
	emitter := []CandidateEmitter{describeEntityEmitter{}}

	// Qualifying kinds: must produce a candidate.
	qualifying := []struct {
		name string
		kind string
	}{
		{"http:GET:/api/users", "http_endpoint"},
		{"http:POST:/api/orders", "Route"},
		{"PaymentService", "Service"},
		{"OrderController", "Controller"},
		{"ReportScheduledJob", "SCOPE.ScheduledJob"},
		{"UserDataAccess", "SCOPE.DataAccess"},
	}
	for _, tc := range qualifying {
		doc := mkDoc(graph.Entity{ID: "e1", Name: tc.name, Kind: tc.kind})
		got := CollectCandidates(doc, emitter, nil)
		if len(got) != 1 {
			t.Errorf("qualifying kind %q name %q: expected 1 candidate, got %d", tc.kind, tc.name, len(got))
		}
		if len(got) == 1 && len(got[0].QualificationSignals) == 0 {
			t.Errorf("qualifying kind %q: expected qualification_signals to be set", tc.kind)
		}
	}

	// Non-qualifying kinds: must produce no candidates.
	nonQualifying := []struct {
		name string
		kind string
	}{
		{"AuthService", "SCOPE.Class"},
		{"helper", "SCOPE.Function"},
		{"getUserById", "SCOPE.Operation"}, // self-descriptive
		{"UserProfile", "SCOPE.Schema"},
	}
	for _, tc := range nonQualifying {
		doc := mkDoc(graph.Entity{ID: "e1", Name: tc.name, Kind: tc.kind})
		got := CollectCandidates(doc, emitter, nil)
		if len(got) != 0 {
			t.Errorf("non-qualifying kind %q name %q: expected 0 candidates, got %d", tc.kind, tc.name, len(got))
		}
	}
}

// ---------------------------------------------------------------------------
// Tests for issue #1162 — Positive-selection predicate
// ---------------------------------------------------------------------------

// TestQualifiesForEnrichment_Scenario verifies the four-entity scenario
// described in issue #1162: 1 HTTPEndpoint, 1 god node, 1 ambiguous-name Op,
// 1 trivial helper → exactly 3 candidates (helper not selected).
func TestQualifiesForEnrichment_Scenario(t *testing.T) {
	emitter := []CandidateEmitter{describeEntityEmitter{}}
	doc := mkDoc(
		// Signal 1 — HTTP endpoint: qualifies
		graph.Entity{ID: "ep1", Name: "http:POST:/api/orders", Kind: "http_endpoint"},
		// Signal 2 — god node: qualifies
		graph.Entity{ID: "gn1", Name: "Coordinator", Kind: "class", IsGodNode: true},
		// Signal 5 — ambiguous name: qualifies
		graph.Entity{ID: "op1", Name: "execute", Kind: "SCOPE.Operation"},
		// No signal — trivial helper: does NOT qualify
		graph.Entity{ID: "op2", Name: "getUserById", Kind: "SCOPE.Operation"},
	)
	got := CollectCandidates(doc, emitter, nil)
	if len(got) != 3 {
		t.Fatalf("expected 3 candidates (trivial helper excluded), got %d", len(got))
	}
	ids := map[string]bool{}
	for _, c := range got {
		ids[c.SubjectID] = true
	}
	if ids["op2"] {
		t.Errorf("trivial helper op2 (getUserById) should not have been selected")
	}
}

// TestQualifiesForEnrichment_Signals checks that QualificationSignals is
// populated and contains the correct signal name for each qualifying trigger.
func TestQualifiesForEnrichment_Signals(t *testing.T) {
	emitter := []CandidateEmitter{describeEntityEmitter{}}

	cases := []struct {
		entity graph.Entity
		want   string
	}{
		{graph.Entity{ID: "ep", Name: "http:GET:/api/x", Kind: "http_endpoint"}, "http_endpoint"},
		{graph.Entity{ID: "gn", Name: "Hub", Kind: "class", IsGodNode: true}, "god_node"},
		{graph.Entity{ID: "ap", Name: "Bridge", Kind: "class", IsArticulationPt: true}, "articulation_point"},
		{graph.Entity{ID: "svc", Name: "AuthService", Kind: "Service"}, "high_arch_kind:Service"},
		{graph.Entity{ID: "cmp", Name: "PaymentContextProvider", Kind: "SCOPE.Component"}, "complex_component"},
		{graph.Entity{ID: "amb", Name: "run", Kind: "SCOPE.Operation"}, "ambiguous_name"},
	}

	for _, tc := range cases {
		doc := mkDoc(tc.entity)
		got := CollectCandidates(doc, emitter, nil)
		if len(got) != 1 {
			t.Errorf("entity %q kind %q: expected 1 candidate, got %d", tc.entity.Name, tc.entity.Kind, len(got))
			continue
		}
		found := false
		for _, sig := range got[0].QualificationSignals {
			if sig == tc.want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("entity %q: expected signal %q in %v", tc.entity.Name, tc.want, got[0].QualificationSignals)
		}
	}
}

// TestQualifiesForEnrichment_NoiseKindDefaultsOut verifies that entities in
// the noise kind set are excluded even if they are god nodes.
func TestQualifiesForEnrichment_NoiseKindDefaultsOut(t *testing.T) {
	emitter := []CandidateEmitter{describeEntityEmitter{}}
	noiseGodNode := graph.Entity{
		ID: "n1", Name: "SomePattern", Kind: "SCOPE.Pattern", IsGodNode: true,
	}
	doc := mkDoc(noiseGodNode)
	got := CollectCandidates(doc, emitter, nil)
	if len(got) != 0 {
		t.Errorf("noise kind with god_node: expected 0 candidates, got %d", len(got))
	}
}

// TestQualifiesForEnrichment_AmbiguousNameBoundary checks boundary conditions
// for the ambiguous-name signal: exactly at 9 chars (qualifies), 10 chars
// (does not), camelCase (does not, has uppercase).
func TestQualifiesForEnrichment_AmbiguousNameBoundary(t *testing.T) {
	emitter := []CandidateEmitter{describeEntityEmitter{}}

	// 9 chars all-lowercase → qualifies
	doc9 := mkDoc(graph.Entity{ID: "o1", Name: "transform", Kind: "SCOPE.Operation"}) // exactly 9
	if got := CollectCandidates(doc9, emitter, nil); len(got) != 1 {
		t.Errorf("9-char lowercase name: expected 1 candidate, got %d", len(got))
	}

	// 10 chars all-lowercase → does not qualify via ambiguous-name signal
	// (unless another signal fires, which it won't here)
	doc10 := mkDoc(graph.Entity{ID: "o2", Name: "transforms2", Kind: "SCOPE.Operation"}) // 11 chars
	if got := CollectCandidates(doc10, emitter, nil); len(got) != 0 {
		t.Errorf("11-char name: expected 0 candidates (too long for ambiguous-name), got %d", len(got))
	}

	// CamelCase → does not qualify via ambiguous-name (has uppercase)
	docCC := mkDoc(graph.Entity{ID: "o3", Name: "runIt", Kind: "SCOPE.Operation"})
	if got := CollectCandidates(docCC, emitter, nil); len(got) != 0 {
		t.Errorf("camelCase name: expected 0 candidates, got %d", len(got))
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
// produces one task with one action. The entity is a Route (qualifying kind)
// but not a god-node/articulation-point so it gets only describe_entity.
func TestCollectTasks_OneAction(t *testing.T) {
	doc := mkDoc(graph.Entity{ID: "e1", Name: "http:GET:/api/payments", Kind: "http_endpoint"})

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
		graph.Entity{ID: "e1", Name: "http:GET:/api/plain", Kind: "http_endpoint"},                        // 1 action (describe_entity only)
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

// ---------------------------------------------------------------------------
// Tests for issue #1131: 0–100 confidence score on every Candidate
// ---------------------------------------------------------------------------

// TestComputeScore_GodHTTPEndpoint — a god-node HTTP endpoint is the highest
// possible signal: base 80 + god_node 20 = 100 (clamped).
func TestComputeScore_GodHTTPEndpoint(t *testing.T) {
	e := &graph.Entity{
		ID:        "e1",
		Name:      "http:POST:/api/orders",
		Kind:      "http_endpoint",
		IsGodNode: true,
		SourceFile: "handlers/orders.go",
	}
	score, breakdown, band := ComputeScore(e)
	if score != 100 {
		t.Errorf("god HTTP endpoint score = %d, want 100 (breakdown: %s)", score, breakdown)
	}
	if band != "critical" {
		t.Errorf("band = %q, want critical", band)
	}
}

// TestComputeScore_PrivateHelper — a short snake_case name with no source file
// should land in the low band.
func TestComputeScore_PrivateHelper(t *testing.T) {
	pr := 0.001 // low pagerank
	e := &graph.Entity{
		ID:       "e2",
		Name:     "__helper",
		Kind:     "SCOPE.Operation",
		PageRank: &pr,
		// SourceFile intentionally empty → -10
		// __helper: isPrivateHelper → -20, len=8 > 4 so no -15 short name
	}
	score, breakdown, band := ComputeScore(e)
	// base_operation:40 - private_helper:20 - no_source_file:10 = 10
	if score > 20 {
		t.Errorf("private helper score = %d, want ≤20 (breakdown: %s)", score, breakdown)
	}
	if band == "critical" || band == "high" {
		t.Errorf("private helper band = %q, want medium or low", band)
	}
}

// TestComputeScore_AmbiguousNameOperation — an ambiguous-name operation with an
// articulation point signal.
func TestComputeScore_AmbiguousNameOperation(t *testing.T) {
	e := &graph.Entity{
		ID:               "e3",
		Name:             "process",
		Kind:             "SCOPE.Operation",
		IsArticulationPt: true,
		SourceFile:       "core/handler.py",
	}
	score, _, _ := ComputeScore(e)
	// base_operation:40 + articulation:15 + ambiguous_name:15 = 70
	if score != 70 {
		t.Errorf("ambiguous-name articulation-point score = %d, want 70", score)
	}
}

// TestComputeScore_CriticalityBands — verify the four bands map correctly.
func TestComputeScore_CriticalityBands(t *testing.T) {
	cases := []struct {
		score int
		band  string
	}{
		{100, "critical"},
		{80, "critical"},
		{79, "high"},
		{60, "high"},
		{59, "medium"},
		{40, "medium"},
		{39, "low"},
		{0, "low"},
	}
	for _, tc := range cases {
		got := criticalityBand(tc.score)
		if got != tc.band {
			t.Errorf("criticalityBand(%d) = %q, want %q", tc.score, got, tc.band)
		}
	}
}

// TestComputeScore_ScoreOnEmittedCandidate — verify that Score, ScoreBreakdown,
// and CriticalityBand are populated on candidates emitted by the built-in
// emitters (end-to-end check).
func TestComputeScore_ScoreOnEmittedCandidate(t *testing.T) {
	pr := 0.05 // high pagerank → +10
	doc := mkDoc(graph.Entity{
		ID:         "e1",
		Name:       "http:GET:/api/users",
		Kind:       "http_endpoint",
		PageRank:   &pr,
		SourceFile: "handlers.go",
	})
	cands := CollectCandidates(doc, []CandidateEmitter{describeEntityEmitter{}}, nil)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	c := cands[0]
	if c.Score == 0 {
		t.Errorf("Score = 0, want > 0 (breakdown: %s)", c.ScoreBreakdown)
	}
	if c.ScoreBreakdown == "" {
		t.Errorf("ScoreBreakdown is empty")
	}
	if c.CriticalityBand == "" {
		t.Errorf("CriticalityBand is empty")
	}
	// http_endpoint base=80 + high_pagerank=10 = 90 → critical.
	if c.Score != 90 {
		t.Errorf("http_endpoint + high_pagerank score = %d, want 90 (breakdown: %s)", c.Score, c.ScoreBreakdown)
	}
	if c.CriticalityBand != "critical" {
		t.Errorf("band = %q, want critical", c.CriticalityBand)
	}
}

// TestComputeScore_ScoreClampZero — modifiers cannot push below 0.
func TestComputeScore_ScoreClampZero(t *testing.T) {
	e := &graph.Entity{
		ID:   "e4",
		Name: "_x",
		Kind: "SCOPE.Component",
		// len 2 → -15, no source file → -10, isPrivateHelper("_x")==true → -20
		// base 35 - 15 - 10 - 20 = -10 → clamped to 0
	}
	score, _, _ := ComputeScore(e)
	if score < 0 {
		t.Errorf("score below zero: %d", score)
	}
}

// TestComputeScore_NilEntity — nil entity must return (0, ..., "low") safely.
func TestComputeScore_NilEntity(t *testing.T) {
	score, _, band := ComputeScore(nil)
	if score != 0 {
		t.Errorf("nil entity score = %d, want 0", score)
	}
	if band != "low" {
		t.Errorf("nil entity band = %q, want low", band)
	}
}

// ---------------------------------------------------------------------------
// Tests for issue #1279 — Tighten enrichment selection
// ---------------------------------------------------------------------------

// TestSignal2_SelfDescriptiveOpArticulationPoint verifies that a SCOPE.Operation
// entity that is an articulation point but has a self-descriptive name does NOT
// qualify for describe_entity (Signal 2 guard, fix #1279).
func TestSignal2_SelfDescriptiveOpArticulationPoint(t *testing.T) {
	emitter := []CandidateEmitter{describeEntityEmitter{}}

	selfDescriptiveNames := []string{
		"setFilters",
		"fetchData",
		"useCreateNote",
		"getUserById",
		"validateToken",
		"parseResponse",
	}

	for _, name := range selfDescriptiveNames {
		doc := mkDoc(graph.Entity{
			ID:               "op1",
			Name:             name,
			Kind:             "SCOPE.Operation",
			IsArticulationPt: true,
			// IsGodNode is false — articulation-only
		})
		got := CollectCandidates(doc, emitter, nil)
		if len(got) != 0 {
			t.Errorf("self-descriptive articulation-pt op %q: expected 0 describe_entity candidates, got %d", name, len(got))
		}
	}

	// Also test plain "Operation" kind.
	doc := mkDoc(graph.Entity{
		ID:               "op2",
		Name:             "fetchData",
		Kind:             "Operation",
		IsArticulationPt: true,
	})
	if got := CollectCandidates(doc, emitter, nil); len(got) != 0 {
		t.Errorf("plain Operation kind fetchData articulation-pt: expected 0, got %d", len(got))
	}
}

// TestSignal2_GodNodeSelfDescriptiveOpStillQualifies verifies that a god node
// with a self-descriptive name still qualifies for describe_entity even though
// it also matches selfDescriptiveOperationRE. God nodes are architectural hubs
// that need description regardless of name pattern (fix #1279).
func TestSignal2_GodNodeSelfDescriptiveOpStillQualifies(t *testing.T) {
	emitter := []CandidateEmitter{describeEntityEmitter{}}

	doc := mkDoc(graph.Entity{
		ID:        "gn1",
		Name:      "fetchData",
		Kind:      "SCOPE.Operation",
		IsGodNode: true,
	})
	got := CollectCandidates(doc, emitter, nil)
	if len(got) != 1 {
		t.Fatalf("god node with self-descriptive name: expected 1 describe_entity candidate, got %d", len(got))
	}
	found := false
	for _, sig := range got[0].QualificationSignals {
		if sig == "god_node" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected god_node signal in %v", got[0].QualificationSignals)
	}
}

// TestSignal2_ArticulationPtNonOpStillQualifies verifies that non-Operation
// entities that are articulation points still qualify for describe_entity.
// The self-descriptive guard in Signal 2 targets only SCOPE.Operation / Operation
// kinds (fix #1279).
func TestSignal2_ArticulationPtNonOpStillQualifies(t *testing.T) {
	emitter := []CandidateEmitter{describeEntityEmitter{}}

	doc := mkDoc(graph.Entity{
		ID:               "svc1",
		Name:             "fetchData", // self-descriptive name but not an Operation kind
		Kind:             "Service",
		IsArticulationPt: true,
	})
	got := CollectCandidates(doc, emitter, nil)
	if len(got) != 1 {
		t.Fatalf("non-Op articulation pt: expected 1 candidate, got %d", len(got))
	}
}

// TestDescribeRoleEmitter_ArticulationPtSelfDescriptiveOp verifies that
// describeRoleEmitter still emits describe_role for an articulation-point
// operation with a self-descriptive name. The describe_entity guard must
// not affect describeRoleEmitter (fix #1279 — preserving describe_role).
func TestDescribeRoleEmitter_ArticulationPtSelfDescriptiveOp(t *testing.T) {
	emitter := []CandidateEmitter{describeRoleEmitter{}}

	// describeRoleEmitter already has its own selfDescriptiveOperationRE check
	// that filters self-descriptive names. Verify that a non-self-descriptive
	// name on an articulation-pt does emit describe_role.
	doc := mkDoc(graph.Entity{
		ID:               "ap1",
		Name:             "Bridge",
		Kind:             "class",
		IsArticulationPt: true,
	})
	got := CollectCandidates(doc, emitter, nil)
	if len(got) != 1 {
		t.Fatalf("describe_role articulation pt Bridge: expected 1 candidate, got %d", len(got))
	}
	if got[0].Kind != KindDescribeRole {
		t.Errorf("kind = %q, want %q", got[0].Kind, KindDescribeRole)
	}
}

// TestTemplateLiteralName_SkipsDescribeEntity verifies that entity names
// containing "${" are excluded from describe_entity enrichment entirely
// (template-literal URL guard, fix #1279).
func TestTemplateLiteralName_SkipsDescribeEntity(t *testing.T) {
	emitter := []CandidateEmitter{describeEntityEmitter{}}

	templateLiteralNames := []string{
		"${import.meta.env.VITE_CORE_API}/devices/?${queryParams.toString()}",
		"${baseURL}/api/v1",
		"${host}:${port}",
	}

	for _, name := range templateLiteralNames {
		// Use an http_endpoint kind so that without the guard it would qualify
		// via Signal 1 (ensuring the guard is what stops emission).
		doc := mkDoc(graph.Entity{
			ID:   "ext1",
			Name: name,
			Kind: "SCOPE.ExternalAPI",
		})
		got := CollectCandidates(doc, emitter, nil)
		if len(got) != 0 {
			t.Errorf("template-literal name %q: expected 0 candidates, got %d", name, len(got))
		}
	}
}
