package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
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
