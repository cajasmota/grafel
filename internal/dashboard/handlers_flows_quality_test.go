package dashboard

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
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func makeFlowGroup(entities []graph.Entity, rels []graph.Relationship) *DashGroup {
	doc := &graph.Document{
		Repo:          "backend",
		Entities:      entities,
		Relationships: rels,
	}
	return &DashGroup{
		Name: "testgrp",
		Repos: map[string]*DashRepo{
			"backend": {Slug: "backend", Path: "/tmp/fake-backend", Doc: doc},
		},
	}
}

// processEntity builds a SCOPE.Process graph.Entity with the given id,
// step_count, and cross_stack flag.
func processEntity(id, name string, stepCount int, crossStack bool) graph.Entity {
	cs := "false"
	if crossStack {
		cs = "true"
	}
	return graph.Entity{
		ID:   id,
		Name: name,
		Kind: processEntityKind,
		Properties: map[string]string{
			"entry_name":  name + "_entry",
			"entry_id":    id + "_entry",
			"terminal_id": id + "_terminal",
			"step_count":  itoa(stepCount),
			"cross_stack": cs,
		},
	}
}

// stepEntity builds a plain function entity that acts as a flow step.
func stepEntity(id, name, kind string) graph.Entity {
	return graph.Entity{
		ID:         id,
		Name:       name,
		Kind:       kind,
		SourceFile: "src/" + id + ".go",
		StartLine:  1,
		Properties: map[string]string{},
	}
}

// stepRel builds a STEP_IN_PROCESS relationship from processID to stepID.
func stepRel(processID, stepID string, idx int) graph.Relationship {
	return graph.Relationship{
		ID:     "step-" + processID + "-" + stepID,
		FromID: processID,
		ToID:   stepID,
		Kind:   stepInProcessEdge,
		Properties: map[string]string{
			"step_index": itoa(idx),
		},
	}
}

// outRel builds an outgoing relationship from a step entity.
func outRel(fromID, toID, kind string) graph.Relationship {
	return graph.Relationship{
		ID:     "out-" + fromID + "-" + kind,
		FromID: fromID,
		ToID:   toID,
		Kind:   kind,
	}
}

// newFlowQualityTestServer builds an httptest.Server pre-loaded with grp.
func newFlowQualityTestServer(t *testing.T, grp *DashGroup) *httptest.Server {
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

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests: classifyFlowDeadEnds
// ─────────────────────────────────────────────────────────────────────────────

// TestDeadEnd_DBWrite — a flow whose steps include a WRITES_TO relationship
// must NOT appear in dead-ends.
func TestDeadEnd_DBWrite(t *testing.T) {
	proc := processEntity("proc-db", "saveUser", 3, false)
	step1 := stepEntity("step1", "validate", "Function")
	step2 := stepEntity("step2", "persist", "Function")
	step3 := stepEntity("step3", "audit", "Function")

	entities := []graph.Entity{proc, step1, step2, step3}
	rels := []graph.Relationship{
		stepRel("proc-db", "step1", 0),
		stepRel("proc-db", "step2", 1),
		stepRel("proc-db", "step3", 2),
		// step2 writes to the DB — this is the useful sink.
		outRel("step2", "db:users", "WRITES_TO"),
	}

	grp := makeFlowGroup(entities, rels)
	items := classifyFlowDeadEnds(grp)

	for _, item := range items {
		if item.ProcessID == "backend::proc-db" {
			t.Errorf("flow with WRITES_TO must not be a dead-end, but found: %+v", item)
		}
	}
}

// TestDeadEnd_NoUsefulSink — a flow with 3 steps and no observable side
// effects must appear with reason "no_useful_sink".
func TestDeadEnd_NoUsefulSink(t *testing.T) {
	proc := processEntity("proc-noop", "doNothing", 3, false)
	step1 := stepEntity("noop1", "log", "Function")
	step2 := stepEntity("noop2", "validate", "Function")
	step3 := stepEntity("noop3", "pass", "Function") // if x: pass style

	entities := []graph.Entity{proc, step1, step2, step3}
	rels := []graph.Relationship{
		stepRel("proc-noop", "noop1", 0),
		stepRel("proc-noop", "noop2", 1),
		stepRel("proc-noop", "noop3", 2),
		// Only a CALLS edge — not a useful sink.
		outRel("noop1", "noop2", "CALLS"),
	}

	grp := makeFlowGroup(entities, rels)
	items := classifyFlowDeadEnds(grp)

	var found *DeadEndItem
	for i := range items {
		if items[i].ProcessID == "backend::proc-noop" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected proc-noop to appear as dead-end, but it was not found")
	}
	if found.Reason != "no_useful_sink" {
		t.Errorf("reason: want no_useful_sink, got %q", found.Reason)
	}
	if found.StepCount != 3 {
		t.Errorf("step_count: want 3, got %d", found.StepCount)
	}
}

// TestDeadEnd_SingleStep — a flow with step_count == 1 must appear with
// reason "single_step".
func TestDeadEnd_SingleStep(t *testing.T) {
	proc := processEntity("proc-one", "trivial", 1, false)
	step1 := stepEntity("one1", "onlyStep", "Function")

	entities := []graph.Entity{proc, step1}
	rels := []graph.Relationship{
		stepRel("proc-one", "one1", 0),
	}

	grp := makeFlowGroup(entities, rels)
	items := classifyFlowDeadEnds(grp)

	var found *DeadEndItem
	for i := range items {
		if items[i].ProcessID == "backend::proc-one" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected proc-one to appear as dead-end (single_step), but it was not found")
	}
	if found.Reason != "single_step" {
		t.Errorf("reason: want single_step, got %q", found.Reason)
	}
}

// TestDeadEnd_ZeroStep — a flow with step_count == 0 must appear with
// reason "single_step" (same bucket as single-step).
func TestDeadEnd_ZeroStep(t *testing.T) {
	proc := processEntity("proc-zero", "empty", 0, false)

	grp := makeFlowGroup([]graph.Entity{proc}, nil)
	items := classifyFlowDeadEnds(grp)

	var found *DeadEndItem
	for i := range items {
		if items[i].ProcessID == "backend::proc-zero" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected proc-zero to appear as dead-end (single_step), but not found")
	}
	if found.Reason != "single_step" {
		t.Errorf("reason: want single_step, got %q", found.Reason)
	}
}

// TestDeadEnd_HTTPResponse — a flow ending in a step whose entity kind
// contains "Response" must NOT appear as dead-end.
func TestDeadEnd_HTTPResponse(t *testing.T) {
	proc := processEntity("proc-http", "serveUser", 2, false)
	step1 := stepEntity("http1", "loadUser", "Function")
	step2 := stepEntity("http2", "writeResponse", "HTTPResponse") // kind contains "Response"

	entities := []graph.Entity{proc, step1, step2}
	rels := []graph.Relationship{
		stepRel("proc-http", "http1", 0),
		stepRel("proc-http", "http2", 1),
	}

	grp := makeFlowGroup(entities, rels)
	items := classifyFlowDeadEnds(grp)

	for _, item := range items {
		if item.ProcessID == "backend::proc-http" {
			t.Errorf("flow with HTTPResponse step must not be a dead-end, but found: %+v", item)
		}
	}
}

// TestDeadEnd_Publishes — a flow with a PUBLISHES_TO edge must NOT be dead-end.
func TestDeadEnd_Publishes(t *testing.T) {
	proc := processEntity("proc-pub", "notifyUser", 2, false)
	step1 := stepEntity("pub1", "prepare", "Function")
	step2 := stepEntity("pub2", "emit", "Function")

	entities := []graph.Entity{proc, step1, step2}
	rels := []graph.Relationship{
		stepRel("proc-pub", "pub1", 0),
		stepRel("proc-pub", "pub2", 1),
		outRel("pub2", "topic:user-events", "PUBLISHES_TO"),
	}

	grp := makeFlowGroup(entities, rels)
	items := classifyFlowDeadEnds(grp)

	for _, item := range items {
		if item.ProcessID == "backend::proc-pub" {
			t.Errorf("flow with PUBLISHES_TO must not be a dead-end, but found: %+v", item)
		}
	}
}

// TestDeadEnd_Asserts — a flow with an ASSERTS edge must NOT be dead-end.
func TestDeadEnd_Asserts(t *testing.T) {
	proc := processEntity("proc-test", "TestSomething", 2, false)
	step1 := stepEntity("tst1", "arrange", "Function")
	step2 := stepEntity("tst2", "assert", "Function")

	entities := []graph.Entity{proc, step1, step2}
	rels := []graph.Relationship{
		stepRel("proc-test", "tst1", 0),
		stepRel("proc-test", "tst2", 1),
		outRel("tst2", "value", "ASSERTS"),
	}

	grp := makeFlowGroup(entities, rels)
	items := classifyFlowDeadEnds(grp)

	for _, item := range items {
		if item.ProcessID == "backend::proc-test" {
			t.Errorf("flow with ASSERTS edge must not be a dead-end, but found: %+v", item)
		}
	}
}

// TestDeadEnd_EmptyGroup — empty group returns no dead-ends.
func TestDeadEnd_EmptyGroup(t *testing.T) {
	grp := &DashGroup{
		Name:  "empty",
		Repos: map[string]*DashRepo{},
	}
	items := classifyFlowDeadEnds(grp)
	if len(items) != 0 {
		t.Errorf("expected 0 items for empty group, got %d", len(items))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration smoke: HTTP endpoint shape
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleFlowDeadEnds_HTTPSmoke(t *testing.T) {
	proc := processEntity("proc-smoke", "smokeFlow", 3, false)
	step1 := stepEntity("sm1", "log", "Function")
	step2 := stepEntity("sm2", "check", "Function")
	step3 := stepEntity("sm3", "idle", "Function")

	entities := []graph.Entity{proc, step1, step2, step3}
	rels := []graph.Relationship{
		stepRel("proc-smoke", "sm1", 0),
		stepRel("proc-smoke", "sm2", 1),
		stepRel("proc-smoke", "sm3", 2),
	}
	grp := makeFlowGroup(entities, rels)
	grp.Name = "testgrp"

	ts := newFlowQualityTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/testgrp/dead-ends")
	if err != nil {
		t.Fatalf("GET dead-ends: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d — body: %s", resp.StatusCode, b)
	}

	b, _ := io.ReadAll(resp.Body)
	var body struct {
		DeadEnds []DeadEndItem `json:"dead_ends"`
		Total    int           `json:"total"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}

	if body.Total != 1 {
		t.Errorf("total: want 1, got %d", body.Total)
	}
	if len(body.DeadEnds) != 1 {
		t.Fatalf("dead_ends len: want 1, got %d", len(body.DeadEnds))
	}

	item := body.DeadEnds[0]
	if item.ProcessID != "backend::proc-smoke" {
		t.Errorf("process_id: want backend::proc-smoke, got %q", item.ProcessID)
	}
	if item.Reason != "no_useful_sink" {
		t.Errorf("reason: want no_useful_sink, got %q", item.Reason)
	}
	if item.StepCount != 3 {
		t.Errorf("step_count: want 3, got %d", item.StepCount)
	}
	if item.Repo != "backend" {
		t.Errorf("repo: want backend, got %q", item.Repo)
	}
}

func TestHandleFlowDeadEnds_UnknownGroup(t *testing.T) {
	grp := makeFlowGroup(nil, nil)
	grp.Name = "testgrp"
	ts := newFlowQualityTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/nosuchgroup/dead-ends")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", resp.StatusCode)
	}
}

func TestHandleFlowDeadEnds_EmptyResult(t *testing.T) {
	// A group with only a DB-writing flow returns an empty list (not null).
	proc := processEntity("proc-clean", "cleanFlow", 2, false)
	step1 := stepEntity("cl1", "load", "Function")
	step2 := stepEntity("cl2", "save", "Function")

	entities := []graph.Entity{proc, step1, step2}
	rels := []graph.Relationship{
		stepRel("proc-clean", "cl1", 0),
		stepRel("proc-clean", "cl2", 1),
		outRel("cl2", "db:records", "WRITES_TO"),
	}
	grp := makeFlowGroup(entities, rels)
	grp.Name = "testgrp"

	ts := newFlowQualityTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/testgrp/dead-ends")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}

	arr, ok := body["dead_ends"].([]any)
	if !ok {
		t.Fatalf("dead_ends should be an array, got %T", body["dead_ends"])
	}
	if len(arr) != 0 {
		t.Errorf("expected empty dead_ends for clean flow, got %d items", len(arr))
	}
	if total, _ := body["total"].(float64); total != 0 {
		t.Errorf("total: want 0, got %v", total)
	}
}

func TestHandleFlowDeadEnds_SingleStepReason(t *testing.T) {
	proc := processEntity("proc-tiny", "tinyFlow", 1, false)
	step1 := stepEntity("tiny1", "onlyStep", "Function")

	entities := []graph.Entity{proc, step1}
	rels := []graph.Relationship{stepRel("proc-tiny", "tiny1", 0)}
	grp := makeFlowGroup(entities, rels)
	grp.Name = "testgrp"

	ts := newFlowQualityTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/testgrp/dead-ends")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var body struct {
		DeadEnds []DeadEndItem `json:"dead_ends"`
		Total    int           `json:"total"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}

	if body.Total != 1 {
		t.Errorf("total: want 1, got %d", body.Total)
	}
	if body.DeadEnds[0].Reason != "single_step" {
		t.Errorf("reason: want single_step, got %q", body.DeadEnds[0].Reason)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests: classifyFlowTruncated
// ─────────────────────────────────────────────────────────────────────────────

// callsRel builds a CALLS relationship from a step entity to a target ID.
func callsRel(fromID, toID string) graph.Relationship {
	return graph.Relationship{
		ID:     "calls-" + fromID + "-" + toID,
		FromID: fromID,
		ToID:   toID,
		Kind:   "CALLS",
	}
}

// dynamicStepEntity builds a step entity with dynamic="true".
func dynamicStepEntity(id, name string, dynamicTarget string) graph.Entity {
	return graph.Entity{
		ID:         id,
		Name:       name,
		Kind:       "Function",
		SourceFile: "src/" + id + ".go",
		StartLine:  1,
		Properties: map[string]string{
			"dynamic":        "true",
			"dynamic_target": dynamicTarget,
		},
	}
}

// crossStackStepEntity builds a step entity with cross_stack="true" and an
// unresolved terminal_id.
func crossStackStepEntity(id, name, terminalID string) graph.Entity {
	return graph.Entity{
		ID:         id,
		Name:       name,
		Kind:       "Function",
		SourceFile: "src/" + id + ".go",
		StartLine:  1,
		Properties: map[string]string{
			"cross_stack": "true",
			"terminal_id": terminalID,
		},
	}
}

// TestTruncated_UnresolvedCallee — a flow whose intermediate step has a CALLS
// edge to an entity ID not present in any repo must appear with reason
// "unresolved_callee" and severity "warn".
func TestTruncated_UnresolvedCallee(t *testing.T) {
	proc := processEntity("proc-trunc", "fetchData", 3, false)
	step1 := stepEntity("tr1", "parse", "Function")
	step2 := stepEntity("tr2", "callExternal", "Function")
	step3 := stepEntity("tr3", "respond", "Function")

	entities := []graph.Entity{proc, step1, step2, step3}
	rels := []graph.Relationship{
		stepRel("proc-trunc", "tr1", 0),
		stepRel("proc-trunc", "tr2", 1),
		stepRel("proc-trunc", "tr3", 2),
		// tr2 calls an entity that is not in the graph.
		callsRel("tr2", "unknown-lib::parseJSON"),
	}

	grp := makeFlowGroup(entities, rels)
	items := classifyFlowTruncated(grp)

	var found *TruncatedFlowItem
	for i := range items {
		if items[i].ProcessID == "backend::proc-trunc" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected proc-trunc to appear as truncated, but it was not found")
	}
	if found.Reason != "unresolved_callee" {
		t.Errorf("reason: want unresolved_callee, got %q", found.Reason)
	}
	if found.Severity != "warn" {
		t.Errorf("severity: want warn, got %q", found.Severity)
	}
	if found.TruncationStep != "tr2" {
		t.Errorf("truncation_step: want tr2, got %q", found.TruncationStep)
	}
	if found.TruncationIndex != 1 {
		t.Errorf("truncation_point: want 1, got %d", found.TruncationIndex)
	}
	if found.UnresolvedTarget == "" {
		t.Error("unresolved_target must not be empty")
	}
	if !found.IsTruncated {
		t.Error("is_truncated must be true")
	}
}

// TestTruncated_DynamicDispatch — a flow with a step whose dynamic="true"
// property set must appear with reason "dynamic_dispatch" and severity "info".
func TestTruncated_DynamicDispatch(t *testing.T) {
	proc := processEntity("proc-dyn", "dispatchCmd", 2, false)
	step1 := stepEntity("dyn1", "prepare", "Function")
	step2 := dynamicStepEntity("dyn2", "dispatch", "cmd.Handler")

	entities := []graph.Entity{proc, step1, step2}
	rels := []graph.Relationship{
		stepRel("proc-dyn", "dyn1", 0),
		stepRel("proc-dyn", "dyn2", 1),
	}

	grp := makeFlowGroup(entities, rels)
	items := classifyFlowTruncated(grp)

	var found *TruncatedFlowItem
	for i := range items {
		if items[i].ProcessID == "backend::proc-dyn" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected proc-dyn to appear as truncated (dynamic_dispatch), but not found")
	}
	if found.Reason != "dynamic_dispatch" {
		t.Errorf("reason: want dynamic_dispatch, got %q", found.Reason)
	}
	if found.Severity != "info" {
		t.Errorf("severity: want info, got %q", found.Severity)
	}
	if found.TruncationStep != "dyn2" {
		t.Errorf("truncation_step: want dyn2, got %q", found.TruncationStep)
	}
	if found.UnresolvedTarget != "cmd.Handler" {
		t.Errorf("unresolved_target: want cmd.Handler, got %q", found.UnresolvedTarget)
	}
}

// TestTruncated_CrossRepoUnindexed — a flow that has a cross-stack step whose
// terminal_id is not in any indexed repo must appear with reason
// "cross_repo_unindexed" and severity "error".
func TestTruncated_CrossRepoUnindexed(t *testing.T) {
	proc := processEntity("proc-cs", "callService", 2, true)
	step1 := stepEntity("cs1", "prepare", "Function")
	step2 := crossStackStepEntity("cs2", "rpcCall", "remote-svc::handleOrder")

	entities := []graph.Entity{proc, step1, step2}
	rels := []graph.Relationship{
		stepRel("proc-cs", "cs1", 0),
		stepRel("proc-cs", "cs2", 1),
	}

	grp := makeFlowGroup(entities, rels)
	items := classifyFlowTruncated(grp)

	var found *TruncatedFlowItem
	for i := range items {
		if items[i].ProcessID == "backend::proc-cs" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected proc-cs to appear as truncated (cross_repo_unindexed), but not found")
	}
	if found.Reason != "cross_repo_unindexed" {
		t.Errorf("reason: want cross_repo_unindexed, got %q", found.Reason)
	}
	if found.Severity != "error" {
		t.Errorf("severity: want error, got %q", found.Severity)
	}
	if found.UnresolvedTarget != "remote-svc::handleOrder" {
		t.Errorf("unresolved_target: want remote-svc::handleOrder, got %q", found.UnresolvedTarget)
	}
}

// TestTruncated_FullyResolvedFlow — a flow whose every CALLS edge points to a
// known entity must NOT appear as truncated.
func TestTruncated_FullyResolvedFlow(t *testing.T) {
	proc := processEntity("proc-clean-tr", "cleanFlow", 3, false)
	step1 := stepEntity("rc1", "validate", "Function")
	step2 := stepEntity("rc2", "process", "Function")
	step3 := stepEntity("rc3", "save", "Function")

	entities := []graph.Entity{proc, step1, step2, step3}
	rels := []graph.Relationship{
		stepRel("proc-clean-tr", "rc1", 0),
		stepRel("proc-clean-tr", "rc2", 1),
		stepRel("proc-clean-tr", "rc3", 2),
		// All CALLS edges point to known entities in the graph.
		callsRel("rc1", "rc2"),
		callsRel("rc2", "rc3"),
	}

	grp := makeFlowGroup(entities, rels)
	items := classifyFlowTruncated(grp)

	for _, item := range items {
		if item.ProcessID == "backend::proc-clean-tr" {
			t.Errorf("fully-resolved flow must NOT appear as truncated, but found: %+v", item)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration smoke: HTTP endpoint shape for truncated flows
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleFlowTruncated_HTTPSmoke(t *testing.T) {
	proc := processEntity("proc-smoke-tr", "smokeFlow", 2, false)
	step1 := stepEntity("sm-tr1", "prepare", "Function")
	step2 := stepEntity("sm-tr2", "callUnknown", "Function")

	entities := []graph.Entity{proc, step1, step2}
	rels := []graph.Relationship{
		stepRel("proc-smoke-tr", "sm-tr1", 0),
		stepRel("proc-smoke-tr", "sm-tr2", 1),
		callsRel("sm-tr2", "nowhere::missingFn"),
	}
	grp := makeFlowGroup(entities, rels)
	grp.Name = "testgrp"

	ts := newFlowQualityTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/testgrp/truncated")
	if err != nil {
		t.Fatalf("GET truncated: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d — body: %s", resp.StatusCode, b)
	}

	b, _ := io.ReadAll(resp.Body)
	var body struct {
		TruncatedFlows []TruncatedFlowItem `json:"truncated_flows"`
		Total          int                 `json:"total"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}

	if body.Total != 1 {
		t.Errorf("total: want 1, got %d", body.Total)
	}
	if len(body.TruncatedFlows) == 0 {
		t.Fatal("truncated_flows must not be empty")
	}

	item := body.TruncatedFlows[0]
	if item.ProcessID != "backend::proc-smoke-tr" {
		t.Errorf("process_id: want backend::proc-smoke-tr, got %q", item.ProcessID)
	}
	if item.Reason != "unresolved_callee" {
		t.Errorf("reason: want unresolved_callee, got %q", item.Reason)
	}
	if item.Severity != "warn" {
		t.Errorf("severity: want warn, got %q", item.Severity)
	}
	if !item.IsTruncated {
		t.Error("is_truncated must be true")
	}
	if item.TruncationStep == "" {
		t.Error("truncation_step must not be empty")
	}
	if item.UnresolvedTarget == "" {
		t.Error("unresolved_target must not be empty")
	}
}

func TestHandleFlowTruncated_UnknownGroup(t *testing.T) {
	grp := makeFlowGroup(nil, nil)
	grp.Name = "testgrp"
	ts := newFlowQualityTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/nosuchgroup/truncated")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", resp.StatusCode)
	}
}

func TestHandleFlowTruncated_EmptyResult(t *testing.T) {
	// A group with only a fully-resolved flow returns an empty list (not null).
	proc := processEntity("proc-ok", "resolvedFlow", 2, false)
	step1 := stepEntity("ok1", "stepA", "Function")
	step2 := stepEntity("ok2", "stepB", "Function")

	entities := []graph.Entity{proc, step1, step2}
	rels := []graph.Relationship{
		stepRel("proc-ok", "ok1", 0),
		stepRel("proc-ok", "ok2", 1),
		callsRel("ok1", "ok2"),
	}
	grp := makeFlowGroup(entities, rels)
	grp.Name = "testgrp"

	ts := newFlowQualityTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/testgrp/truncated")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}

	arr, ok := body["truncated_flows"].([]any)
	if !ok {
		t.Fatalf("truncated_flows should be an array, got %T", body["truncated_flows"])
	}
	if len(arr) != 0 {
		t.Errorf("expected empty truncated_flows for fully-resolved flow, got %d items", len(arr))
	}
	if total, _ := body["total"].(float64); total != 0 {
		t.Errorf("total: want 0, got %v", total)
	}
}

// TestHandleFlowsList_ShortFlowFilter verifies the #1639 default short-flow
// filter: the default list excludes 2-3 step trivial flows but keeps >=4-step
// flows; min_steps=0 returns everything; cross-repo short flows are exempt.
func TestHandleFlowsList_ShortFlowFilter(t *testing.T) {
	e := entryEntity("e", "handler", "Handler")
	long := procWithEntry("p-long", "longFlow", e.ID, e.Name, "a.go", 6)
	short := procWithEntry("p-short", "shortFlow", e.ID, e.Name, "b.go", 2)
	crossShort := procWithEntry("p-cross", "crossFlow", e.ID, e.Name, "c.go", 2)
	crossShort.Properties["cross_stack"] = "true"

	grp := makeGroupWithEntries(
		[]graph.Entity{long, short, crossShort},
		[]graph.Entity{e},
		nil,
	)
	ts := newFlowQualityTestServer(t, grp)

	get := func(path string) []string {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		var body struct {
			Processes []struct {
				ProcessID string `json:"process_id"`
			} `json:"processes"`
		}
		b, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatalf("decode %s: %v\n%s", path, err, b)
		}
		var ids []string
		for _, p := range body.Processes {
			ids = append(ids, p.ProcessID)
		}
		return ids
	}

	def := get("/api/flows/testgrp")
	hasID := func(ids []string, sub string) bool {
		for _, id := range ids {
			if strings.Contains(id, sub) {
				return true
			}
		}
		return false
	}
	if !hasID(def, "p-long") {
		t.Errorf("default list must include >=4-step flow; got %v", def)
	}
	if hasID(def, "p-short") {
		t.Errorf("default list must exclude 2-step trivial flow; got %v", def)
	}
	if !hasID(def, "p-cross") {
		t.Errorf("default list must keep short cross-repo flow; got %v", def)
	}

	all := get("/api/flows/testgrp?min_steps=0")
	if !hasID(all, "p-short") {
		t.Errorf("min_steps=0 must include short flow; got %v", all)
	}
}
