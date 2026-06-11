package dashboard

// handlers_flows_annotation_test.go — unit + integration tests for step-kind
// annotation and side-effect classification (issue #1147).

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests: classifyStepKind
// ─────────────────────────────────────────────────────────────────────────────

func TestClassifyStepKind_HTTPFetch(t *testing.T) {
	got := classifyStepKind("Function", map[string]bool{"FETCHES": true}, nil, false)
	if got != StepKindHTTPFetch {
		t.Errorf("want %q, got %q", StepKindHTTPFetch, got)
	}
}

func TestClassifyStepKind_HTTPFetchByEntityKind(t *testing.T) {
	got := classifyStepKind("HTTPClient", map[string]bool{}, nil, false)
	if got != StepKindHTTPFetch {
		t.Errorf("want %q, got %q", StepKindHTTPFetch, got)
	}
}

func TestClassifyStepKind_DBWrite(t *testing.T) {
	got := classifyStepKind("Function", map[string]bool{"WRITES_TO": true}, nil, false)
	if got != StepKindDBWrite {
		t.Errorf("want %q, got %q", StepKindDBWrite, got)
	}
}

func TestClassifyStepKind_DBWriteInsert(t *testing.T) {
	got := classifyStepKind("Function", map[string]bool{"INSERTS_INTO": true}, nil, false)
	if got != StepKindDBWrite {
		t.Errorf("want %q, got %q", StepKindDBWrite, got)
	}
}

func TestClassifyStepKind_DBQuery(t *testing.T) {
	got := classifyStepKind("Function", map[string]bool{"READS_FROM": true}, nil, false)
	if got != StepKindDBQuery {
		t.Errorf("want %q, got %q", StepKindDBQuery, got)
	}
}

// TestClassifyStepKind_MongoPipelineBuilderTerminal is the #4337 regression: a
// FUNCTIONAL Mongo aggregation-pipeline builder terminal (a SCOPE.DataAccess
// node reached via a JOINS_COLLECTION $lookup edge) must classify as db_query —
// the data-access terminal kind — NOT `render`. Before the fix it fell through
// to the entity-kind substring matchers and was mis-kinded `render`.
func TestClassifyStepKind_MongoPipelineBuilderTerminal(t *testing.T) {
	got := classifyStepKind("SCOPE.DataAccess", map[string]bool{"JOINS_COLLECTION": true}, nil, false)
	if got != StepKindDBQuery {
		t.Errorf("Mongo $lookup pipeline-builder terminal: want %q, got %q", StepKindDBQuery, got)
	}
}

// TestClassifyStepKind_DataAccessKindUnresolvedJoin covers the honest-unresolved
// case: a SCOPE.DataAccess stage whose `from:` collection is non-static, so no
// JOINS_COLLECTION edge is emitted. It must still classify as db_query by KIND,
// never `render`.
func TestClassifyStepKind_DataAccessKindUnresolvedJoin(t *testing.T) {
	got := classifyStepKind("SCOPE.DataAccess", map[string]bool{}, nil, false)
	if got != StepKindDBQuery {
		t.Errorf("DataAccess terminal (no edge): want %q, got %q", StepKindDBQuery, got)
	}
}

// TestClassifyStepKind_AccessesTableBuilderTerminal generalises #4337 to the SQL
// fluent-builder family (knex / TypeORM QueryBuilder), whose data-access
// terminal carries an ACCESSES_TABLE edge rather than an imperative read edge.
func TestClassifyStepKind_AccessesTableBuilderTerminal(t *testing.T) {
	got := classifyStepKind("SCOPE.DataAccess", map[string]bool{"ACCESSES_TABLE": true}, nil, false)
	if got != StepKindDBQuery {
		t.Errorf("ACCESSES_TABLE builder terminal: want %q, got %q", StepKindDBQuery, got)
	}
}

// TestClassifyStepKind_GenuineRenderUnaffected is the #4337 regression guard: an
// actual UI render terminal (a Component/View entity with no data-access edge or
// kind) must STILL classify as render — the fix must not over-reach.
func TestClassifyStepKind_GenuineRenderUnaffected(t *testing.T) {
	for _, k := range []string{"SCOPE.View", "SCOPE.Component", "ReactComponent", "Widget"} {
		if got := classifyStepKind(k, map[string]bool{}, nil, false); got != StepKindRender {
			t.Errorf("genuine render terminal %q: want %q, got %q", k, StepKindRender, got)
		}
	}
}

func TestClassifyStepKind_MessagePublish(t *testing.T) {
	got := classifyStepKind("Function", map[string]bool{"PUBLISHES_TO": true}, nil, false)
	if got != StepKindMessagePublish {
		t.Errorf("want %q, got %q", StepKindMessagePublish, got)
	}
}

func TestClassifyStepKind_MessageConsume(t *testing.T) {
	// isEntry=true + incoming SUBSCRIBES_TO → message_consume
	got := classifyStepKind("Function", map[string]bool{}, map[string]bool{"SUBSCRIBES_TO": true}, true)
	if got != StepKindMessageConsume {
		t.Errorf("want %q, got %q", StepKindMessageConsume, got)
	}
}

func TestClassifyStepKind_TestAssert(t *testing.T) {
	got := classifyStepKind("Function", map[string]bool{"ASSERTS": true}, nil, false)
	if got != StepKindTestAssert {
		t.Errorf("want %q, got %q", StepKindTestAssert, got)
	}
}

func TestClassifyStepKind_Render(t *testing.T) {
	got := classifyStepKind("Component", map[string]bool{}, nil, false)
	if got != StepKindRender {
		t.Errorf("want %q, got %q", StepKindRender, got)
	}
}

func TestClassifyStepKind_FunctionCallFallback(t *testing.T) {
	got := classifyStepKind("Function", map[string]bool{"CALLS": true}, nil, false)
	if got != StepKindFunctionCall {
		t.Errorf("want %q, got %q", StepKindFunctionCall, got)
	}
}

func TestClassifyStepKind_NeverEmpty(t *testing.T) {
	// Even with no edges and a generic kind the result must not be empty.
	got := classifyStepKind("Unknown", map[string]bool{}, nil, false)
	if got == "" {
		t.Error("step_kind must never be empty string")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests: inferEntryKindFromKind (lower-level helper, #1147)
// ─────────────────────────────────────────────────────────────────────────────

func TestInferEntryKindFromKind_HTTPHandler(t *testing.T) {
	got := inferEntryKindFromKind("HTTPHandler", nil)
	if got != "http_handler" {
		t.Errorf("want http_handler, got %q", got)
	}
}

func TestInferEntryKindFromKind_MessageConsumer(t *testing.T) {
	got := inferEntryKindFromKind("Consumer", nil)
	if got != "message_consumer" {
		t.Errorf("want message_consumer, got %q", got)
	}
}

func TestInferEntryKindFromKind_ScheduledTask(t *testing.T) {
	got := inferEntryKindFromKind("CronJob", nil)
	if got != "scheduled_task" {
		t.Errorf("want scheduled_task, got %q", got)
	}
}

func TestInferEntryKindFromKind_Component(t *testing.T) {
	got := inferEntryKindFromKind("Component", nil)
	if got != "component" {
		t.Errorf("want component, got %q", got)
	}
}

func TestInferEntryKindFromKind_Function(t *testing.T) {
	got := inferEntryKindFromKind("Function", nil)
	if got != "function" {
		t.Errorf("want function, got %q", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests: annotateFlowSteps
// ─────────────────────────────────────────────────────────────────────────────

// fixture: handler reads DB + publishes message
// Expected: step_kinds = [db_query, message_publish]
//
//	flow_side_effects = [message_publish]  (db_query is read, not a side-effect)
//	Actually: effectKinds includes db_write and message_publish but NOT db_query.
//	So flow_side_effects = ["message_publish"]
func TestAnnotateFlowSteps_DBReadAndPublish(t *testing.T) {
	proc := processEntity("proc-rp", "readAndPublish", 2, false)
	stepRead := stepEntity("step-read", "loadUser", "Function")
	stepPub := stepEntity("step-pub", "emitEvent", "Function")

	entities := []graph.Entity{proc, stepRead, stepPub}
	rels := []graph.Relationship{
		stepRel("proc-rp", "step-read", 0),
		stepRel("proc-rp", "step-pub", 1),
		outRel("step-read", "db:users", "READS_FROM"),
		outRel("step-pub", "topic_X", "PUBLISHES_TO"),
	}

	grp := makeFlowGroup(entities, rels)
	raw := []rawStep{
		{EntityID: "backend::step-read", Label: "loadUser", Repo: "backend", StepIndex: 0, EdgeKind: stepInProcessEdge, EntityKind: "Function"},
		{EntityID: "backend::step-pub", Label: "emitEvent", Repo: "backend", StepIndex: 1, EdgeKind: stepInProcessEdge, EntityKind: "Function"},
	}

	annotated, meta := annotateFlowSteps(raw, grp, "backend", "")

	if len(annotated) != 2 {
		t.Fatalf("want 2 annotated steps, got %d", len(annotated))
	}
	if annotated[0].StepKind != StepKindDBQuery {
		t.Errorf("step[0] kind: want %q, got %q", StepKindDBQuery, annotated[0].StepKind)
	}
	if annotated[1].StepKind != StepKindMessagePublish {
		t.Errorf("step[1] kind: want %q, got %q", StepKindMessagePublish, annotated[1].StepKind)
	}
	// flow_side_effects: message_publish is an effect, db_query is not.
	if len(meta.FlowSideEffects) != 1 || meta.FlowSideEffects[0] != StepKindMessagePublish {
		t.Errorf("flow_side_effects: want [%q], got %v", StepKindMessagePublish, meta.FlowSideEffects)
	}
	// publishes_to target should be in per-step side effects.
	if len(annotated[1].SideEffects) == 0 || annotated[1].SideEffects[0] != "topic_X" {
		t.Errorf("step[1].side_effects: want [topic_X], got %v", annotated[1].SideEffects)
	}
}

// fixture: pure transform — no external edges
// Expected: step_kinds = [function_call, function_call]
//
//	flow_side_effects = []
func TestAnnotateFlowSteps_PureTransform(t *testing.T) {
	proc := processEntity("proc-tr", "transform", 2, false)
	stepA := stepEntity("step-a", "parseInput", "Function")
	stepB := stepEntity("step-b", "formatOutput", "Function")

	entities := []graph.Entity{proc, stepA, stepB}
	rels := []graph.Relationship{
		stepRel("proc-tr", "step-a", 0),
		stepRel("proc-tr", "step-b", 1),
		outRel("step-a", "step-b", "CALLS"),
	}

	grp := makeFlowGroup(entities, rels)
	raw := []rawStep{
		{EntityID: "backend::step-a", Label: "parseInput", Repo: "backend", StepIndex: 0, EdgeKind: stepInProcessEdge, EntityKind: "Function"},
		{EntityID: "backend::step-b", Label: "formatOutput", Repo: "backend", StepIndex: 1, EdgeKind: stepInProcessEdge, EntityKind: "Function"},
	}

	annotated, meta := annotateFlowSteps(raw, grp, "backend", "")

	for i, s := range annotated {
		if s.StepKind != StepKindFunctionCall {
			t.Errorf("step[%d] kind: want %q, got %q", i, StepKindFunctionCall, s.StepKind)
		}
	}
	if len(meta.FlowSideEffects) != 0 {
		t.Errorf("flow_side_effects: want empty, got %v", meta.FlowSideEffects)
	}
}

// fixture: cross-repo flow — steps in different repos
// Expected: is_cross_repo = true
func TestAnnotateFlowSteps_CrossRepo(t *testing.T) {
	proc := processEntity("proc-cr", "crossRepoFlow", 2, true)
	stepA := stepEntity("step-a", "callFrontend", "Function")
	stepB := stepEntity("step-b", "callBackend", "Function")

	docA := &graph.Document{
		Repo:          "frontend",
		Entities:      []graph.Entity{proc, stepA},
		Relationships: []graph.Relationship{stepRel("proc-cr", "step-a", 0)},
	}
	docB := &graph.Document{
		Repo:          "backend",
		Entities:      []graph.Entity{stepB},
		Relationships: []graph.Relationship{stepRel("proc-cr", "step-b", 1)},
	}

	grp := &DashGroup{
		Name: "testgrp",
		Repos: map[string]*DashRepo{
			"frontend": {Slug: "frontend", Path: "/tmp/frontend", Doc: docA},
			"backend":  {Slug: "backend", Path: "/tmp/backend", Doc: docB},
		},
	}

	raw := []rawStep{
		{EntityID: "frontend::step-a", Label: "callFrontend", Repo: "frontend", StepIndex: 0, EdgeKind: stepInProcessEdge, EntityKind: "Function"},
		{EntityID: "backend::step-b", Label: "callBackend", Repo: "backend", StepIndex: 1, EdgeKind: stepInProcessEdge, EntityKind: "Function"},
	}

	_, meta := annotateFlowSteps(raw, grp, "frontend", "")
	if !meta.IsCrossRepo {
		t.Error("is_cross_repo: want true for steps spanning multiple repos")
	}
}

// fixture: DB write step — complexity_score reflects kind diversity
func TestAnnotateFlowSteps_ComplexityScore(t *testing.T) {
	proc := processEntity("proc-cs", "complexFlow", 3, false)
	stepR := stepEntity("step-r", "readUser", "Function")
	stepW := stepEntity("step-w", "saveUser", "Function")
	stepP := stepEntity("step-p", "notify", "Function")

	entities := []graph.Entity{proc, stepR, stepW, stepP}
	rels := []graph.Relationship{
		stepRel("proc-cs", "step-r", 0),
		stepRel("proc-cs", "step-w", 1),
		stepRel("proc-cs", "step-p", 2),
		outRel("step-r", "db:users", "READS_FROM"),
		outRel("step-w", "db:users", "WRITES_TO"),
		outRel("step-p", "topic:notifications", "PUBLISHES_TO"),
	}

	grp := makeFlowGroup(entities, rels)
	raw := []rawStep{
		{EntityID: "backend::step-r", Label: "readUser", Repo: "backend", StepIndex: 0, EdgeKind: stepInProcessEdge, EntityKind: "Function"},
		{EntityID: "backend::step-w", Label: "saveUser", Repo: "backend", StepIndex: 1, EdgeKind: stepInProcessEdge, EntityKind: "Function"},
		{EntityID: "backend::step-p", Label: "notify", Repo: "backend", StepIndex: 2, EdgeKind: stepInProcessEdge, EntityKind: "Function"},
	}

	_, meta := annotateFlowSteps(raw, grp, "backend", "")
	// 3 steps × 3 kinds = 9.
	want := float64(3 * 3)
	if meta.ComplexityScore != want {
		t.Errorf("complexity_score: want %.0f, got %.0f", want, meta.ComplexityScore)
	}
}

// fixture: data lineage — a step that reads and writes contributes a pair.
func TestAnnotateFlowSteps_DataLineage(t *testing.T) {
	proc := processEntity("proc-dl", "lineageFlow", 1, false)
	stepRW := stepEntity("step-rw", "readAndWrite", "Function")

	entities := []graph.Entity{proc, stepRW}
	rels := []graph.Relationship{
		stepRel("proc-dl", "step-rw", 0),
		outRel("step-rw", "db:source_table", "READS_FROM"),
		outRel("step-rw", "db:sink_table", "WRITES_TO"),
	}

	grp := makeFlowGroup(entities, rels)
	raw := []rawStep{
		{EntityID: "backend::step-rw", Label: "readAndWrite", Repo: "backend", StepIndex: 0, EdgeKind: stepInProcessEdge, EntityKind: "Function"},
	}

	_, meta := annotateFlowSteps(raw, grp, "backend", "")
	if len(meta.DataLineage) != 1 {
		t.Fatalf("data_lineage: want 1 pair, got %d", len(meta.DataLineage))
	}
	if meta.DataLineage[0].ReadSource != "db:source_table" {
		t.Errorf("lineage.read_source: want db:source_table, got %q", meta.DataLineage[0].ReadSource)
	}
	if meta.DataLineage[0].WriteSink != "db:sink_table" {
		t.Errorf("lineage.write_sink: want db:sink_table, got %q", meta.DataLineage[0].WriteSink)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration: GET /api/flows/{group}/{processId} includes Flows v2 fields
// ─────────────────────────────────────────────────────────────────────────────

func newFlowDetailTestServer(t *testing.T, grp *DashGroup) *httptest.Server {
	t.Helper()
	st := newFakeStore()
	st.groups["testgrp"] = GroupSummary{
		Name:       "testgrp",
		ConfigPath: "/tmp/testgrp.json",
		Repos:      []string{"backend"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["testgrp"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts
}

// TestFlowDetail_StepKindAndSideEffects verifies that the detail endpoint
// returns step_kind on each step and flow_side_effects at the process root.
func TestFlowDetail_StepKindAndSideEffects(t *testing.T) {
	proc := processEntity("proc-det", "detailFlow", 2, false)
	stepLoad := stepEntity("step-load", "loadUser", "Function")
	stepSave := stepEntity("step-save", "saveResult", "Function")

	entities := []graph.Entity{proc, stepLoad, stepSave}
	rels := []graph.Relationship{
		stepRel("proc-det", "step-load", 0),
		stepRel("proc-det", "step-save", 1),
		outRel("step-load", "db:users", "READS_FROM"),
		outRel("step-save", "db:results", "WRITES_TO"),
	}

	grp := makeFlowGroup(entities, rels)
	grp.Name = "testgrp"
	ts := newFlowDetailTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/testgrp/backend::proc-det")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d — body: %s", resp.StatusCode, b)
	}

	b, _ := io.ReadAll(resp.Body)
	var body struct {
		Process struct {
			FlowSideEffects []string `json:"flow_side_effects"`
			EntryKind       string   `json:"entry_kind"`
			ComplexityScore float64  `json:"complexity_score"`
			IsCrossRepo     bool     `json:"is_cross_repo"`
			DataLineage     []struct {
				ReadSource string `json:"read_source"`
				WriteSink  string `json:"write_sink"`
			} `json:"data_lineage"`
			Steps []struct {
				StepKind string `json:"step_kind"`
			} `json:"steps"`
		} `json:"process"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}

	// step_kind must be present and correct on each step.
	if len(body.Process.Steps) != 2 {
		t.Fatalf("steps: want 2, got %d", len(body.Process.Steps))
	}
	if body.Process.Steps[0].StepKind != StepKindDBQuery {
		t.Errorf("step[0].step_kind: want %q, got %q", StepKindDBQuery, body.Process.Steps[0].StepKind)
	}
	if body.Process.Steps[1].StepKind != StepKindDBWrite {
		t.Errorf("step[1].step_kind: want %q, got %q", StepKindDBWrite, body.Process.Steps[1].StepKind)
	}

	// flow_side_effects must include db_write (WRITES_TO is an effect).
	foundDBWrite := false
	for _, se := range body.Process.FlowSideEffects {
		if se == StepKindDBWrite {
			foundDBWrite = true
		}
	}
	if !foundDBWrite {
		t.Errorf("flow_side_effects: want %q in list, got %v", StepKindDBWrite, body.Process.FlowSideEffects)
	}

	// entry_kind must be non-empty.
	if body.Process.EntryKind == "" {
		t.Error("entry_kind must not be empty")
	}

	// is_cross_repo must be false (single-repo fixture).
	if body.Process.IsCrossRepo {
		t.Error("is_cross_repo: want false for single-repo fixture")
	}
}

// TestFlowDetail_UnclassifiedStepGetsDefault verifies that steps with only a
// CALLS edge receive "function_call" — never an empty string.
func TestFlowDetail_UnclassifiedStepGetsDefault(t *testing.T) {
	proc := processEntity("proc-unk", "unknownFlow", 1, false)
	stepA := stepEntity("step-unk", "doSomething", "Function")

	entities := []graph.Entity{proc, stepA}
	rels := []graph.Relationship{
		stepRel("proc-unk", "step-unk", 0),
		outRel("step-unk", "step-unk2", "CALLS"),
	}

	grp := makeFlowGroup(entities, rels)
	grp.Name = "testgrp"
	ts := newFlowDetailTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/testgrp/backend::proc-unk")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var body struct {
		Process struct {
			Steps []struct {
				StepKind string `json:"step_kind"`
			} `json:"steps"`
		} `json:"process"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}

	if len(body.Process.Steps) == 0 {
		t.Fatal("expected at least 1 step")
	}
	if body.Process.Steps[0].StepKind == "" {
		t.Error("step_kind must never be empty")
	}
	if body.Process.Steps[0].StepKind != StepKindFunctionCall {
		t.Errorf("want %q, got %q", StepKindFunctionCall, body.Process.Steps[0].StepKind)
	}
}

// TestFlowDetail_FlowSideEffectsEmptyForPureTransform verifies that a
// transform-only flow has an empty (not null) flow_side_effects list.
func TestFlowDetail_FlowSideEffectsEmptyForPureTransform(t *testing.T) {
	proc := processEntity("proc-pure", "pureTransform", 2, false)
	stepA := stepEntity("step-pa", "parse", "Function")
	stepB := stepEntity("step-pb", "format", "Function")

	entities := []graph.Entity{proc, stepA, stepB}
	rels := []graph.Relationship{
		stepRel("proc-pure", "step-pa", 0),
		stepRel("proc-pure", "step-pb", 1),
		outRel("step-pa", "step-pb", "CALLS"),
	}

	grp := makeFlowGroup(entities, rels)
	grp.Name = "testgrp"
	ts := newFlowDetailTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/testgrp/backend::proc-pure")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}

	proc2, _ := body["process"].(map[string]any)
	if proc2 == nil {
		t.Fatal("process key missing")
	}
	se, ok := proc2["flow_side_effects"]
	if !ok {
		t.Fatal("flow_side_effects key missing from process")
	}
	arr, ok := se.([]any)
	if !ok {
		t.Fatalf("flow_side_effects: want array, got %T", se)
	}
	if len(arr) != 0 {
		t.Errorf("flow_side_effects: want empty for pure transform, got %v", arr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// #1905: bridge steps in cross-repo flows carry correct repo prefix + metadata
// ─────────────────────────────────────────────────────────────────────────────

// TestFlowDetail_BridgeStepMetadata_1905 verifies that GET /api/flows/{group}/{pid}
// returns all steps for a cross-repo flow, with bridge steps carrying the companion
// repo's slug in their entity_id and with name/file populated from that repo.
func TestFlowDetail_BridgeStepMetadata_1905(t *testing.T) {
	// Process lives in the frontend repo; its chain spans frontend + backend.
	proc := graph.Entity{
		ID:         "proc-xr",
		Name:       "loadDashboard → getSummary",
		Kind:       processEntityKind,
		SourceFile: "dashboard.ts",
		StartLine:  10,
		Properties: map[string]string{
			"entry_id":    "fe_entry",
			"entry_name":  "loadDashboard",
			"terminal_id": "be_handler",
			"step_count":  "3",
			"cross_stack": "true",
			"chain":       "fe_entry,fe_caller,be_handler",
		},
	}
	feEntry := graph.Entity{ID: "fe_entry", Name: "loadDashboard", Kind: "SCOPE.Function", SourceFile: "dashboard.ts", StartLine: 10}
	feCaller := graph.Entity{ID: "fe_caller", Name: "fetchSummary", Kind: "SCOPE.Function", SourceFile: "dashboard.ts", StartLine: 20}

	frontendDoc := &graph.Document{
		Repo:     "frontend",
		Entities: []graph.Entity{proc, feEntry, feCaller},
		Relationships: []graph.Relationship{
			// STEP_IN_PROCESS for the cross-repo chain.
			{ID: "s0", FromID: "proc-xr", ToID: "fe_entry", Kind: stepInProcessEdge, Properties: map[string]string{"step_index": "0"}},
			{ID: "s1", FromID: "proc-xr", ToID: "fe_caller", Kind: stepInProcessEdge, Properties: map[string]string{"step_index": "1"}},
			// Bridge step: ToID lives in the backend doc.
			{ID: "s2", FromID: "proc-xr", ToID: "be_handler", Kind: stepInProcessEdge, Properties: map[string]string{"step_index": "2"}},
		},
	}
	beHandler := graph.Entity{
		ID:         "be_handler",
		Name:       "OrdersController.getSummary",
		Kind:       "SCOPE.Operation",
		SourceFile: "OrdersController.java",
		StartLine:  42,
	}
	backendDoc := &graph.Document{
		Repo:     "backend",
		Entities: []graph.Entity{beHandler},
	}

	grp := &DashGroup{
		Name: "testgrp",
		Repos: map[string]*DashRepo{
			"frontend": {Slug: "frontend", Path: "/tmp/frontend", Doc: frontendDoc},
			"backend":  {Slug: "backend", Path: "/tmp/backend", Doc: backendDoc},
		},
	}
	ts := newFlowDetailTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/testgrp/frontend::proc-xr")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d — body: %s", resp.StatusCode, b)
	}

	b, _ := io.ReadAll(resp.Body)
	var body struct {
		Process struct {
			Steps []struct {
				EntityID  string `json:"entity_id"`
				Label     string `json:"label"`
				StepIndex int    `json:"step_index"`
			} `json:"steps"`
		} `json:"process"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}

	// All 3 steps must be present.
	if len(body.Process.Steps) != 3 {
		t.Fatalf("steps: want 3, got %d\nbody: %s", len(body.Process.Steps), b)
	}

	// Find the bridge step (index 2).
	var bridgeStep *struct {
		EntityID  string `json:"entity_id"`
		Label     string `json:"label"`
		StepIndex int    `json:"step_index"`
	}
	for i := range body.Process.Steps {
		if body.Process.Steps[i].StepIndex == 2 {
			bridgeStep = &body.Process.Steps[i]
			break
		}
	}
	if bridgeStep == nil {
		t.Fatalf("bridge step (step_index=2) missing\nbody: %s", b)
	}

	// entity_id must carry the backend:: prefix.
	if !strings.HasPrefix(bridgeStep.EntityID, "backend::") {
		t.Errorf("bridge step entity_id must carry backend:: prefix, got %q\nbody: %s", bridgeStep.EntityID, b)
	}
	// label (name) must be populated from the backend entity.
	if bridgeStep.Label != "OrdersController.getSummary" {
		t.Errorf("bridge step label want OrdersController.getSummary, got %q\nbody: %s", bridgeStep.Label, b)
	}

	// Seed-repo steps must carry the frontend:: prefix.
	for _, s := range body.Process.Steps {
		if s.StepIndex == 2 {
			continue
		}
		if !strings.HasPrefix(s.EntityID, "frontend::") {
			t.Errorf("seed step[%d] entity_id should carry frontend:: prefix, got %q", s.StepIndex, s.EntityID)
		}
	}
}
