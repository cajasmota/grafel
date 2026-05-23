package mcp

import (
	"context"
	"reflect"
	"sort"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// TestExtractIDsBareID verifies that a JSON result whose entity id field is the
// plain "id" key (as produced by serializeEntity) yields a node id. Before the
// fix, extractIDs only looked at "entity_id"/"node_id", so archigraph_inspect
// results glowed nothing.
func TestExtractIDsBareID(t *testing.T) {
	res := mcpapi.NewToolResultText(`{"id":"upvate-core::abc123","label":"Foo","kind":"function"}`)
	nodes, _ := extractIDs(res)
	if want := []string{"upvate-core::abc123"}; !reflect.DeepEqual(nodes, want) {
		t.Fatalf("extractIDs nodes = %v, want %v", nodes, want)
	}
}

// TestExtractIDsSliceBareID verifies the per-item "id" probe inside arrays.
func TestExtractIDsSliceBareID(t *testing.T) {
	res := mcpapi.NewToolResultText(`{"nodes":[{"id":"r::n1"},{"id":"r::n2"}]}`)
	nodes, _ := extractIDs(res)
	if want := []string{"r::n1", "r::n2"}; !reflect.DeepEqual(sortedCopy(nodes), want) {
		t.Fatalf("extractIDs slice nodes = %v, want %v", nodes, want)
	}
}

// TestIDCollector verifies that ids recorded through the per-call collector are
// drained back out, deduped. This is the path markdown-rendering tools use.
func TestIDCollector(t *testing.T) {
	ctx, c := withIDCollector(context.Background())
	recordNodeIDs(ctx, "r::a", "r::b", "r::a")
	recordEdgeIDs(ctx, "e1")
	nodes, edges := c.drain()
	if want := []string{"r::a", "r::b"}; !reflect.DeepEqual(nodes, want) {
		t.Fatalf("collector nodes = %v, want %v", nodes, want)
	}
	if want := []string{"e1"}; !reflect.DeepEqual(edges, want) {
		t.Fatalf("collector edges = %v, want %v", edges, want)
	}
}

// TestRecordNodeIDsNoCollector ensures recording is a safe no-op when the
// context carries no collector (e.g. direct handler calls in other tests).
func TestRecordNodeIDsNoCollector(t *testing.T) {
	recordNodeIDs(context.Background(), "r::x") // must not panic
	if c := collectorFrom(context.Background()); c != nil {
		t.Fatalf("expected nil collector, got %v", c)
	}
}

// TestIDsFromArgs verifies id-bearing arguments are harvested as a fallback,
// while free-text fields are excluded and bare (unprefixed) label_or_id values
// are skipped.
func TestIDsFromArgs(t *testing.T) {
	got := idsFromArgs(map[string]any{
		"node_id":          "r::n",
		"target_entity_id": "r::t",
		"question":         "where is auth", // must be ignored
		"label_or_id":      "PlainLabel",    // bare label → skipped
	})
	if want := []string{"r::n", "r::t"}; !reflect.DeepEqual(sortedCopy(got), want) {
		t.Fatalf("idsFromArgs = %v, want %v", got, want)
	}

	// Prefixed label_or_id IS an id.
	got2 := idsFromArgs(map[string]any{"label_or_id": "r::Foo"})
	if want := []string{"r::Foo"}; !reflect.DeepEqual(got2, want) {
		t.Fatalf("idsFromArgs prefixed = %v, want %v", got2, want)
	}
}

// TestMarkdownToolIDsReachEvent simulates the wire path of archigraph_find:
// a handler that returns markdown (no ids in the body) records its touched
// ids via the collector; emitActivity then publishes them. Before the fix,
// such events arrived with empty returned_node_ids → no WebUI glow.
func TestMarkdownToolIDsReachEvent(t *testing.T) {
	broker := NewMCPActivityBroker()
	ch, cancel := broker.SubscribeAll()
	defer cancel()
	s := &Server{activityBroker: broker}

	// Install the collector exactly as wrap() does, then run a markdown
	// "handler" that records ids the way the per-repo summary call site does.
	ctx, collector := withIDCollector(context.Background())
	recordNodeIDs(ctx, "upvate-core::abc", "upvate-core-frontend::def")
	res := mcpapi.NewToolResultText("# group: upvate — per-repo top hits\n")

	req := mcpapi.CallToolRequest{}
	req.Params.Name = "archigraph_find"
	req.Params.Arguments = map[string]any{"question": "auth"}

	s.emitActivity(ctx, "archigraph_find", req, res, collector)

	select {
	case ev := <-ch:
		got := sortedCopy(ev.ReturnedNodeIDs)
		want := []string{"upvate-core-frontend::def", "upvate-core::abc"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("event node ids = %v, want %v", got, want)
		}
		if ev.ToolName != "archigraph_find" {
			t.Fatalf("tool name = %q", ev.ToolName)
		}
	default:
		t.Fatal("no event published")
	}
}

// TestEmitActivityMergesSources verifies emitActivity unions collector ids,
// JSON-extracted ids, and arg-derived ids into the published event.
func TestEmitActivityMergesSources(t *testing.T) {
	broker := NewMCPActivityBroker()
	ch, cancel := broker.SubscribeAll()
	defer cancel()

	s := &Server{activityBroker: broker}

	ctx, collector := withIDCollector(context.Background())
	recordNodeIDs(ctx, "r::fromCollector")

	req := mcpapi.CallToolRequest{}
	req.Params.Name = "archigraph_inspect"
	req.Params.Arguments = map[string]any{"node_id": "r::fromArgs"}

	res := mcpapi.NewToolResultText(`{"id":"r::fromJSON"}`)

	s.emitActivity(ctx, "archigraph_inspect", req, res, collector)

	select {
	case ev := <-ch:
		got := sortedCopy(ev.ReturnedNodeIDs)
		want := []string{"r::fromArgs", "r::fromCollector", "r::fromJSON"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("event node ids = %v, want %v", got, want)
		}
	default:
		t.Fatal("no event published")
	}
}
