package dashboard

// handlers_flows_entrykind_test.go — Tests for entry-kind grouping metadata
// on GET /api/flows/{group} (issue #1148).

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// makeGroupWithEntries builds a DashGroup that contains both the Process
// entities and their referenced entry entities.
func makeGroupWithEntries(procEntities []graph.Entity, entryEntities []graph.Entity, rels []graph.Relationship) *DashGroup {
	all := append(procEntities, entryEntities...)
	doc := &graph.Document{
		Repo:          "backend",
		Entities:      all,
		Relationships: rels,
	}
	return &DashGroup{
		Name: "testgrp",
		Repos: map[string]*DashRepo{
			"backend": {Slug: "backend", Path: "/tmp/fake-backend", Doc: doc},
		},
	}
}

// procWithEntry creates a SCOPE.Process entity that points to a given entry id.
func procWithEntry(id, name, entryID, entryName, sourceFile string, stepCount int) graph.Entity {
	return graph.Entity{
		ID:         id,
		Name:       name,
		Kind:       processEntityKind,
		SourceFile: sourceFile,
	}.WithProperties(map[string]string{
		"entry_id":    entryID,
		"entry_name":  entryName,
		"terminal_id": id + "_terminal",
		"step_count":  itoa(stepCount),
		"cross_stack": "false",
	},
	)
}

// entryEntity creates a simple entry-point entity with the given kind.
func entryEntity(id, name, kind string) graph.Entity {
	return graph.Entity{
		ID:         id,
		Name:       name,
		Kind:       kind,
		SourceFile: "src/" + id + ".py",
	}.WithProperties(map[string]string{})
}

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests: inferEntryKind
// ─────────────────────────────────────────────────────────────────────────────

func TestInferEntryKind_HTTPHandler(t *testing.T) {
	entry := entryEntity("e-http", "InspectionController", "Controller")
	proc := procWithEntry("p-http", "handleInspection", entry.ID, entry.Name, "apps/api/handlers/inspections.py", 3)
	grp := makeGroupWithEntries([]graph.Entity{proc}, []graph.Entity{entry}, nil)

	got := inferEntryKind(grp, entry.ID)
	if got != "http_handler" {
		t.Errorf("inferEntryKind: want http_handler, got %q", got)
	}
}

func TestInferEntryKind_ScheduledTask(t *testing.T) {
	entry := entryEntity("e-sched", "cleanupCeleryTask", "ScheduledJob")
	proc := procWithEntry("p-sched", "cleanupFlow", entry.ID, entry.Name, "tasks/cleanup.py", 2)
	grp := makeGroupWithEntries([]graph.Entity{proc}, []graph.Entity{entry}, nil)

	got := inferEntryKind(grp, entry.ID)
	if got != "scheduled_task" {
		t.Errorf("inferEntryKind: want scheduled_task, got %q", got)
	}
}

func TestInferEntryKind_ComponentRender(t *testing.T) {
	entry := entryEntity("e-comp", "InspectionCard", "ReactComponent")
	proc := procWithEntry("p-comp", "renderInspectionCard", entry.ID, entry.Name, "src/components/InspectionCard.tsx", 2)
	grp := makeGroupWithEntries([]graph.Entity{proc}, []graph.Entity{entry}, nil)

	got := inferEntryKind(grp, entry.ID)
	if got != "component_render" {
		t.Errorf("inferEntryKind: want component_render, got %q", got)
	}
}

func TestInferEntryKind_MessageConsumer(t *testing.T) {
	entry := entryEntity("e-consumer", "processOrderQueue", "Function")
	proc := procWithEntry("p-consumer", "processOrder", entry.ID, entry.Name, "workers/orders.py", 4)
	// incoming SUBSCRIBES_TO edge towards entry
	subRel := graph.Relationship{
		ID:     "sub-to-consumer",
		FromID: "queue:orders",
		ToID:   entry.ID,
		Kind:   "SUBSCRIBES_TO",
	}
	grp := makeGroupWithEntries([]graph.Entity{proc}, []graph.Entity{entry}, []graph.Relationship{subRel})

	got := inferEntryKind(grp, entry.ID)
	if got != "message_consumer" {
		t.Errorf("inferEntryKind: want message_consumer, got %q", got)
	}
}

func TestInferEntryKind_Internal(t *testing.T) {
	entry := entryEntity("e-util", "formatAddress", "Function")
	proc := procWithEntry("p-util", "formatAddressFlow", entry.ID, entry.Name, "utils/address.py", 2)
	grp := makeGroupWithEntries([]graph.Entity{proc}, []graph.Entity{entry}, nil)

	got := inferEntryKind(grp, entry.ID)
	if got != "function" {
		t.Errorf("inferEntryKind: want function (internal), got %q", got)
	}
}

func TestInferEntryKind_EmptyEntryID(t *testing.T) {
	grp := makeGroupWithEntries(nil, nil, nil)
	got := inferEntryKind(grp, "")
	if got != "function" {
		t.Errorf("inferEntryKind('') = %q, want function", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests: entryModuleFromPath
// ─────────────────────────────────────────────────────────────────────────────

func TestEntryModuleFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"apps/api/handlers/inspections.py", "inspections"},
		{"src/components/InspectionCard.tsx", "InspectionCard"},
		{"workers/orders.py", "orders"},
		{"main.go", "main"},
		{"", ""},
	}
	for _, tc := range cases {
		got := entryModuleFromPath(tc.path)
		if got != tc.want {
			t.Errorf("entryModuleFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests: priorityHint
// ─────────────────────────────────────────────────────────────────────────────

func TestPriorityHint(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{"http_handler", "high"},
		{"message_consumer", "medium"},
		{"scheduled_task", "medium"},
		{"component_render", "low"},
		{"cli_command", "low"},
		{"test", "low"},
		{"function", "low"},
	}
	for _, tc := range cases {
		got := priorityHint(tc.kind)
		if got != tc.want {
			t.Errorf("priorityHint(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration smoke: entry_kind fields in GET /api/flows/{group}
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleFlowsList_EntryKindFields(t *testing.T) {
	// Build a group with 3 flows — one HTTP handler, one Celery task, one utility.
	httpEntry := entryEntity("entry-http", "InspectionHandler", "Handler")
	taskEntry := entryEntity("entry-task", "cleanupTask", "ScheduledJob")
	utilEntry := entryEntity("entry-util", "formatData", "Function")

	procHTTP := procWithEntry("proc-http", "handleInspection", httpEntry.ID, httpEntry.Name,
		"apps/api/handlers/inspections.py", 5)
	procTask := procWithEntry("proc-task", "cleanupData", taskEntry.ID, taskEntry.Name,
		"tasks/cleanup.py", 3)
	procUtil := procWithEntry("proc-util", "formatDataFlow", utilEntry.ID, utilEntry.Name,
		"utils/data.py", 2)

	grp := makeGroupWithEntries(
		[]graph.Entity{procHTTP, procTask, procUtil},
		[]graph.Entity{httpEntry, taskEntry, utilEntry},
		nil,
	)
	grp.Name = "testgrp"

	ts := newFlowQualityTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/testgrp?min_steps=0")
	if err != nil {
		t.Fatalf("GET /api/flows/testgrp: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d — body: %s", resp.StatusCode, b)
	}

	b, _ := io.ReadAll(resp.Body)
	var body struct {
		Processes []struct {
			ProcessID        string      `json:"process_id"`
			EntryKind        string      `json:"entry_kind"`
			EntryModule      string      `json:"entry_module"`
			PriorityHint     string      `json:"priority_hint"`
			DominantStepKind interface{} `json:"dominant_step_kind"`
		} `json:"processes"`
		Count           int              `json:"count"`
		EntryKindGroups []EntryKindGroup `json:"entry_kind_groups"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}

	if body.Count != 3 {
		t.Errorf("count: want 3, got %d", body.Count)
	}
	if len(body.EntryKindGroups) == 0 {
		t.Fatal("entry_kind_groups must be non-empty")
	}

	// Index processes by process_id suffix.
	byID := map[string]struct {
		EntryKind        string
		EntryModule      string
		PriorityHint     string
		DominantStepKind interface{}
	}{}
	for _, p := range body.Processes {
		byID[p.ProcessID] = struct {
			EntryKind        string
			EntryModule      string
			PriorityHint     string
			DominantStepKind interface{}
		}{p.EntryKind, p.EntryModule, p.PriorityHint, p.DominantStepKind}
	}

	// Verify http_handler.
	httpPID := "backend::proc-http"
	if p, ok := byID[httpPID]; !ok {
		t.Fatalf("process %q not in response", httpPID)
	} else {
		if p.EntryKind != "http_handler" {
			t.Errorf("%s entry_kind: want http_handler, got %q", httpPID, p.EntryKind)
		}
		if p.EntryModule != "inspections" {
			t.Errorf("%s entry_module: want inspections, got %q", httpPID, p.EntryModule)
		}
		if p.PriorityHint != "high" {
			t.Errorf("%s priority_hint: want high, got %q", httpPID, p.PriorityHint)
		}
		if p.DominantStepKind != nil {
			t.Errorf("%s dominant_step_kind: want null, got %v", httpPID, p.DominantStepKind)
		}
	}

	// Verify scheduled_task.
	taskPID := "backend::proc-task"
	if p, ok := byID[taskPID]; !ok {
		t.Fatalf("process %q not in response", taskPID)
	} else {
		if p.EntryKind != "scheduled_task" {
			t.Errorf("%s entry_kind: want scheduled_task, got %q", taskPID, p.EntryKind)
		}
		if p.PriorityHint != "medium" {
			t.Errorf("%s priority_hint: want medium, got %q", taskPID, p.PriorityHint)
		}
	}

	// Verify function (internal utility).
	utilPID := "backend::proc-util"
	if p, ok := byID[utilPID]; !ok {
		t.Fatalf("process %q not in response", utilPID)
	} else {
		if p.EntryKind != "function" {
			t.Errorf("%s entry_kind: want function, got %q", utilPID, p.EntryKind)
		}
		if p.PriorityHint != "low" {
			t.Errorf("%s priority_hint: want low, got %q", utilPID, p.PriorityHint)
		}
	}

	// Verify entry_kind_groups correctness — one entry per kind, correct counts.
	kindCount := map[string]int{}
	for _, g := range body.EntryKindGroups {
		kindCount[g.Kind] = g.Count
	}
	wantKinds := map[string]int{
		"http_handler":   1,
		"scheduled_task": 1,
		"function":       1,
	}
	for k, wc := range wantKinds {
		if gc := kindCount[k]; gc != wc {
			t.Errorf("entry_kind_groups[%q]: want count %d, got %d", k, wc, gc)
		}
	}

	// Verify entry_kind_groups is sorted by count descending (all 1s here, so
	// just verify no panic / empty).
	if len(body.EntryKindGroups) != 3 {
		t.Errorf("entry_kind_groups len: want 3, got %d", len(body.EntryKindGroups))
	}
}

// TestHandleFlowsList_EntryKindGroups_Sorting verifies that entry_kind_groups
// is sorted by count descending when kinds have different counts.
func TestHandleFlowsList_EntryKindGroups_Sorting(t *testing.T) {
	httpEntry1 := entryEntity("entry-http1", "Handler1", "Handler")
	httpEntry2 := entryEntity("entry-http2", "Handler2", "Controller")
	httpEntry3 := entryEntity("entry-http3", "Handler3", "Route")
	utilEntry := entryEntity("entry-util", "helper", "Function")

	proc1 := procWithEntry("proc-h1", "flow1", httpEntry1.ID, httpEntry1.Name, "handlers/h1.py", 2)
	proc2 := procWithEntry("proc-h2", "flow2", httpEntry2.ID, httpEntry2.Name, "handlers/h2.py", 2)
	proc3 := procWithEntry("proc-h3", "flow3", httpEntry3.ID, httpEntry3.Name, "handlers/h3.py", 2)
	proc4 := procWithEntry("proc-u1", "flow4", utilEntry.ID, utilEntry.Name, "utils/u1.py", 2)

	grp := makeGroupWithEntries(
		[]graph.Entity{proc1, proc2, proc3, proc4},
		[]graph.Entity{httpEntry1, httpEntry2, httpEntry3, utilEntry},
		nil,
	)
	grp.Name = "testgrp"
	ts := newFlowQualityTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/flows/testgrp?min_steps=0")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var body struct {
		EntryKindGroups []EntryKindGroup `json:"entry_kind_groups"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}

	if len(body.EntryKindGroups) < 2 {
		t.Fatalf("entry_kind_groups: want >=2 groups, got %d", len(body.EntryKindGroups))
	}

	// First group must have the highest count (http_handler with 3).
	if body.EntryKindGroups[0].Kind != "http_handler" {
		t.Errorf("entry_kind_groups[0].kind: want http_handler (count 3), got %q", body.EntryKindGroups[0].Kind)
	}
	if body.EntryKindGroups[0].Count != 3 {
		t.Errorf("entry_kind_groups[0].count: want 3, got %d", body.EntryKindGroups[0].Count)
	}
}
