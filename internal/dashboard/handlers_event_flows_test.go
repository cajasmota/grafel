// Integration tests for the Event Flows dashboard handlers (#1944 Phase 1).
package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// eventFlowEntity builds a synthetic SCOPE.EventFlow entity with the
// minimum Properties keys the list/detail handlers consume.
func eventFlowEntity(id, label, seedID, terminalID string, chain []string, chainLabels []string, channelCount int) graph.Entity {
	props := map[string]string{
		"entry_id":       seedID,
		"entry_name":     seedID,
		"terminal_id":    terminalID,
		"step_count":     itoa(len(chain)),
		"channel_count":  itoa(channelCount),
		"chain":          joinComma(chain),
		"chain_labels":   joinArrow(chainLabels),
		"entry_kind":     "channel",
		"dag_node_count": itoa(len(chain)),
		"branch_count":   "0",
		"is_dag":         "false",
		"branches_dag":   `{"entity_id":"` + chain[0] + `"}`,
	}
	return graph.Entity{
		ID:   id,
		Name: label,
		Kind: eventFlowEntityKind,
	}.WithProperties(props)
}

func joinComma(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}

func joinArrow(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += " → "
		}
		out += x
	}
	return out
}

func efStepRel(flowID, stepID string, idx int) graph.Relationship {
	return graph.Relationship{
		ID:     "efstep-" + flowID + "-" + stepID,
		FromID: flowID,
		ToID:   stepID,
		Kind:   stepInEventFlowEdge,
	}.WithProperties(map[string]string{
		"step_index": itoa(idx),
	},
	)
}

// newEventFlowTestServer wires a small dashboard server pre-loaded with
// the given group. Mirrors newFlowDetailTestServer.
func newEventFlowTestServer(t *testing.T, grp *DashGroup) *httptest.Server {
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

// TestEventFlowsList_BasicShape verifies the list endpoint returns
// EventFlow entities with the expected wire fields.
func TestEventFlowsList_BasicShape(t *testing.T) {
	chain := []string{"topic.A", "svc.subA", "topic.B"}
	chainLabels := []string{"kafka:a", "handleA", "kafka:b"}
	ef := eventFlowEntity("evflow:abc", "kafka:a → kafka:b", "topic.A", "topic.B", chain, chainLabels, 2)

	// Channel entities so the detail endpoint can resolve the IDs.
	topicA := graph.Entity{ID: "topic.A", Name: "kafka:a", Kind: "SCOPE.MessageTopic"}
	topicB := graph.Entity{ID: "topic.B", Name: "kafka:b", Kind: "SCOPE.MessageTopic"}
	subA := stepEntity("svc.subA", "handleA", "SCOPE.Function")

	rels := []graph.Relationship{
		efStepRel("evflow:abc", "topic.A", 0),
		efStepRel("evflow:abc", "svc.subA", 1),
		efStepRel("evflow:abc", "topic.B", 2),
	}
	grp := makeFlowGroup([]graph.Entity{ef, topicA, topicB, subA}, rels)
	ts := newEventFlowTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/event-flows/testgrp")
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
		EventFlows []EventFlowListItem `json:"event_flows"`
		Count      int                 `json:"count"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, b)
	}
	if body.Count != 1 || len(body.EventFlows) != 1 {
		t.Fatalf("want 1 event flow, got count=%d items=%d", body.Count, len(body.EventFlows))
	}
	item := body.EventFlows[0]
	if item.SeedID != "topic.A" {
		t.Errorf("seed_id = %q, want topic.A", item.SeedID)
	}
	if item.ChannelCount != 2 {
		t.Errorf("channel_count = %d, want 2", item.ChannelCount)
	}
	if item.EntryKind != "channel" {
		t.Errorf("entry_kind = %q, want channel", item.EntryKind)
	}
	// chain_labels is serialised as a single " → "-joined string by the
	// engine and split on "," by the dashboard's splitChainLabels (the
	// same long-standing shape used for ProcessFlow), so the round-trip
	// keeps the full chain in one slice element. We just assert the
	// arrow-joined label survives intact.
	if len(item.ChainLabels) != 1 || item.ChainLabels[0] != "kafka:a → handleA → kafka:b" {
		t.Errorf("chain_labels = %v, want one arrow-joined element", item.ChainLabels)
	}
}

func TestEventFlowsDetail_StepsAndChannelFlag(t *testing.T) {
	chain := []string{"topic.A", "svc.subA", "topic.B"}
	chainLabels := []string{"kafka:a", "handleA", "kafka:b"}
	ef := eventFlowEntity("evflow:abc", "kafka:a → kafka:b", "topic.A", "topic.B", chain, chainLabels, 2)

	topicA := graph.Entity{ID: "topic.A", Name: "kafka:a", Kind: "SCOPE.MessageTopic"}
	topicB := graph.Entity{ID: "topic.B", Name: "kafka:b", Kind: "SCOPE.MessageTopic"}
	subA := stepEntity("svc.subA", "handleA", "SCOPE.Function")

	rels := []graph.Relationship{
		efStepRel("evflow:abc", "topic.A", 0),
		efStepRel("evflow:abc", "svc.subA", 1),
		efStepRel("evflow:abc", "topic.B", 2),
	}
	grp := makeFlowGroup([]graph.Entity{ef, topicA, topicB, subA}, rels)
	ts := newEventFlowTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/event-flows/testgrp/backend::evflow:abc")
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
		Steps        []EventFlowStep `json:"steps"`
		ChannelCount int             `json:"channel_count"`
		StepCount    int             `json:"step_count"`
		BranchesDAG  string          `json:"branches_dag"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, b)
	}
	if len(body.Steps) != 3 {
		t.Fatalf("want 3 steps, got %d", len(body.Steps))
	}
	if !body.Steps[0].IsChannel {
		t.Errorf("step 0 (topic) is_channel=false, want true")
	}
	if body.Steps[1].IsChannel {
		t.Errorf("step 1 (function) is_channel=true, want false")
	}
	if !body.Steps[2].IsChannel {
		t.Errorf("step 2 (topic) is_channel=false, want true")
	}
	if body.BranchesDAG == "" {
		t.Error("branches_dag is empty — renderer needs DAG JSON")
	}
}

func TestEventFlowsDetail_NotFound(t *testing.T) {
	grp := makeFlowGroup(nil, nil)
	ts := newEventFlowTestServer(t, grp)
	resp, err := http.Get(ts.URL + "/api/event-flows/testgrp/backend::evflow:missing")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}
