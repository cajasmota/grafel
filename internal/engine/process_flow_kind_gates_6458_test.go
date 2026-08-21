// process_flow_kind_gates_6458_test.go — issue #6458.
//
// Three consumer-side gates in process_flow.go compare e.Kind against the
// bare pre-#1217 literal "http_endpoint":
//
//	:778  buildConsumerEndpointFileSet
//	:957  isConsumerHTTPEndpoint
//	:1001 chainCrossesExternalLib's endpoint-ish switch arm
//
// #1217 split that kind into "http_endpoint_call" (consumer) and
// "http_endpoint_definition" (producer). Nothing in the synthesis path
// emits the bare kind any more (http_endpoint_synthesis.go stamps the
// split kinds), so the first two gates run and reject every entity they
// were written to accept, and the third silently under-reports.
//
// The legacy kind is NOT unreachable: webhooks_edges.go still mints
// Kind:"http_endpoint" with pattern_type="webhook_synthesis". That means
// :1001 fires TODAY on webhook entities, so widening it is a behaviour
// change on a live path, not the re-enabling of dead code. The tests
// below pin the widened behaviour AND the boundaries of the widening:
// every "must stay false" case is as load-bearing as every "must become
// true" case.
//
// None of these three functions had any test before this file.
package engine

import (
	"strconv"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// consumerEnt builds a consumer-side synthetic endpoint entity of the
// given kind, stamped the way http_endpoint_synthesis.go stamps them.
func consumerEnt(id, kind, file string) graph.Entity {
	return graph.Entity{
		ID: id, Name: "GET /api/orders", Kind: kind, SourceFile: file,
	}.WithProperties(map[string]string{
		"pattern_type":  "http_endpoint_client_synthesis",
		"source_caller": "fetchOrders",
	})
}

// producerEnt builds a producer-side synthetic endpoint entity.
func producerEnt(id, kind, file string) graph.Entity {
	return graph.Entity{
		ID: id, Name: "GET /api/orders", Kind: kind, SourceFile: file,
	}.WithProperties(map[string]string{
		"pattern_type": "http_endpoint_synthesis",
	})
}

// webhookEnt mirrors webhooks_edges.go:111 — the one live producer of the
// bare legacy kind.
func webhookEnt(id, file string) graph.Entity {
	return graph.Entity{
		ID: id, Name: "webhook stripe", Kind: "http_endpoint", SourceFile: file,
	}.WithProperties(map[string]string{
		"pattern_type": "webhook_synthesis",
	})
}

// ---------------------------------------------------------------------
// :778 buildConsumerEndpointFileSet
// ---------------------------------------------------------------------

func TestBuildConsumerEndpointFileSet_6458(t *testing.T) {
	tests := []struct {
		name string
		ent  graph.Entity
		want bool // want ent.SourceFile in the returned set
	}{
		{
			// The post-#1217 consumer kind. RED before the fix: the gate
			// compares against "http_endpoint" so this never reaches the
			// pattern_type check.
			name: "http_endpoint_call with client pattern_type",
			ent:  consumerEnt("c1", "http_endpoint_call", "orders.ts"),
			want: true,
		},
		{
			// Case-insensitivity is part of the existing contract
			// (strings.ToLower on the gate) and must survive the fix.
			name: "http_endpoint_call uppercase kind",
			ent:  consumerEnt("c2", "HTTP_ENDPOINT_CALL", "orders.ts"),
			want: true,
		},
		{
			// source_caller-only fallback for synthetics that lost the
			// pattern_type stamp — must work on the new kind too.
			name: "http_endpoint_call source_caller fallback only",
			ent: graph.Entity{ID: "c3", Kind: "http_endpoint_call", SourceFile: "orders.ts"}.
				WithProperties(map[string]string{"source_caller": "fetchOrders"}),
			want: true,
		},
		{
			// Pre-#1217 graphs on disk must keep working.
			name: "legacy http_endpoint with client pattern_type",
			ent:  consumerEnt("c4", "http_endpoint", "orders.ts"),
			want: true,
		},
		// --- boundaries of the widening: these must stay false ---
		{
			// Producer synthetics are excluded by design: their file is
			// the handler's own HTTP surface, not a cross-repo fetch.
			name: "http_endpoint_definition producer is excluded",
			ent:  producerEnt("p1", "http_endpoint_definition", "orders.go"),
			want: false,
		},
		{
			// The one entity kind that reaches this gate today. It must
			// keep being rejected by the property check.
			name: "webhook legacy http_endpoint is excluded",
			ent:  webhookEnt("w1", "webhooks.go"),
			want: false,
		},
		{
			// Deleting the kind gate outright would admit this.
			name: "non-endpoint kind with client pattern_type is excluded",
			ent:  consumerEnt("f1", "SCOPE.Function", "orders.ts"),
			want: false,
		},
		{
			// A near-miss string must not match a prefix/substring fix.
			name: "http_endpoint_calls near-miss kind is excluded",
			ent:  consumerEnt("f2", "http_endpoint_calls", "orders.ts"),
			want: false,
		},
		{
			name: "endpoint kind with no properties is excluded",
			ent:  graph.Entity{ID: "n1", Kind: "http_endpoint_call", SourceFile: "orders.ts"},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := &graph.Document{Repo: "r", Entities: []graph.Entity{tc.ent}}
			got := buildConsumerEndpointFileSet(doc)[tc.ent.SourceFile]
			if got != tc.want {
				t.Fatalf("buildConsumerEndpointFileSet: file %q in set = %v, want %v (kind=%q)",
					tc.ent.SourceFile, got, tc.want, tc.ent.Kind)
			}
		})
	}
}

// ---------------------------------------------------------------------
// :957 isConsumerHTTPEndpoint
// ---------------------------------------------------------------------

func TestIsConsumerHTTPEndpoint_6458(t *testing.T) {
	tests := []struct {
		name string
		ent  *graph.Entity
		want bool
	}{
		{"nil entity", nil, false},
		{
			name: "http_endpoint_call with client pattern_type",
			ent:  entPtr(consumerEnt("c1", "http_endpoint_call", "orders.ts")),
			want: true,
		},
		{
			name: "http_endpoint_call uppercase kind",
			ent:  entPtr(consumerEnt("c2", "HTTP_ENDPOINT_CALL", "orders.ts")),
			want: true,
		},
		{
			name: "http_endpoint_call source_caller fallback only",
			ent: entPtr(graph.Entity{ID: "c3", Kind: "http_endpoint_call"}.
				WithProperties(map[string]string{"source_caller": "fetchOrders"})),
			want: true,
		},
		{
			name: "legacy http_endpoint with client pattern_type",
			ent:  entPtr(consumerEnt("c4", "http_endpoint", "orders.ts")),
			want: true,
		},
		// --- boundaries ---
		{
			// A definition entity mis-stamped with the consumer
			// pattern_type is still not a consumer bridge node. But the
			// existing contract discriminates on pattern_type alone once
			// the kind gate passes, so pin the producer-stamped case,
			// which is the one the synthesiser actually emits.
			name: "http_endpoint_definition producer pattern_type",
			ent:  entPtr(producerEnt("p1", "http_endpoint_definition", "orders.go")),
			want: false,
		},
		{
			name: "webhook legacy http_endpoint",
			ent:  entPtr(webhookEnt("w1", "webhooks.go")),
			want: false,
		},
		{
			name: "non-endpoint kind with client pattern_type",
			ent:  entPtr(consumerEnt("f1", "SCOPE.Function", "orders.ts")),
			want: false,
		},
		{
			name: "http_endpoint_calls near-miss kind",
			ent:  entPtr(consumerEnt("f2", "http_endpoint_calls", "orders.ts")),
			want: false,
		},
		{
			name: "endpoint kind with no properties",
			ent:  &graph.Entity{ID: "n1", Kind: "http_endpoint_call"},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isConsumerHTTPEndpoint(tc.ent); got != tc.want {
				t.Fatalf("isConsumerHTTPEndpoint = %v, want %v", got, tc.want)
			}
		})
	}
}

func entPtr(e graph.Entity) *graph.Entity { return &e }

// ---------------------------------------------------------------------
// :1001 chainCrossesExternalLib — the WIDENING site.
//
// Unlike the two above, this arm fires today (on webhook entities). The
// cases below pin both halves: what starts returning true, and what must
// keep returning false.
// ---------------------------------------------------------------------

func TestChainCrossesExternalLib_6458(t *testing.T) {
	tests := []struct {
		name     string
		ent      graph.Entity
		boundary map[string]bool
		want     bool
	}{
		{
			// RED: the consumer synthetic is the canonical "this process
			// calls out over HTTP" marker and it has no IMPLEMENTS /
			// ROUTES_TO / SERVES edge, so the boundary set never catches
			// it either. Today this chain reports crosses_external_lib
			// =false.
			name: "http_endpoint_call step",
			ent:  consumerEnt("c1", "http_endpoint_call", "orders.ts"),
			want: true,
		},
		{
			// RED. In a real graph a definition usually also carries an
			// IMPLEMENTS/ROUTES_TO edge and is caught by the boundary
			// set; when it does not, the kind must still match.
			name: "http_endpoint_definition step without boundary edge",
			ent:  producerEnt("p1", "http_endpoint_definition", "orders.go"),
			want: true,
		},
		{
			// Already true today via the legacy arm — must not regress.
			name: "webhook legacy http_endpoint step",
			ent:  webhookEnt("w1", "webhooks.go"),
			want: true,
		},
		{
			name: "http_endpoint_call uppercase kind",
			ent:  consumerEnt("c2", "HTTP_ENDPOINT_CALL", "orders.ts"),
			want: true,
		},
		// --- boundaries of the widening ---
		{
			name: "plain function step",
			ent:  graph.Entity{ID: "f1", Kind: "SCOPE.Function", SourceFile: "orders.ts"},
			want: false,
		},
		{
			name: "http_endpoint_calls near-miss kind",
			ent:  graph.Entity{ID: "f2", Kind: "http_endpoint_calls"},
			want: false,
		},
		{
			name: "http_client kind is not an endpoint kind",
			ent:  graph.Entity{ID: "f3", Kind: "http_client"},
			want: false,
		},
		{
			// The boundary set is an independent signal and must keep
			// working regardless of kind.
			name:     "plain function on the HTTP boundary set",
			ent:      graph.Entity{ID: "f4", Kind: "SCOPE.Function"},
			boundary: map[string]bool{"f4": true},
			want:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			byID := map[string]*graph.Entity{tc.ent.ID: &tc.ent}
			b := tc.boundary
			if b == nil {
				b = map[string]bool{}
			}
			if got := chainCrossesExternalLib([]string{tc.ent.ID}, byID, b); got != tc.want {
				t.Fatalf("chainCrossesExternalLib(kind=%q) = %v, want %v", tc.ent.Kind, got, tc.want)
			}
		})
	}
	t.Run("empty chain", func(t *testing.T) {
		if chainCrossesExternalLib(nil, map[string]*graph.Entity{}, map[string]bool{}) {
			t.Fatal("empty chain must not cross an external lib")
		}
	})
	t.Run("unknown id in chain", func(t *testing.T) {
		if chainCrossesExternalLib([]string{"missing"}, map[string]*graph.Entity{}, map[string]bool{}) {
			t.Fatal("chain step absent from byID must not cross an external lib")
		}
	})
}

// ---------------------------------------------------------------------
// End-to-end output delta.
//
// These two tests are the measurement the issue asks for: what actually
// changes in RunProcessFlow's emitted Process properties once the gates
// stop rejecting the split kinds.
// ---------------------------------------------------------------------

// procProps returns the properties of the single emitted Process whose
// entry_id is entryID.
func procProps(t *testing.T, doc *graph.Document, entryID string) map[string]string {
	t.Helper()
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == EntityKindProcess && e.PropGet("entry_id") == entryID {
			return e.PropsSnapshot()
		}
	}
	t.Fatalf("no Process entity with entry_id=%q (entities: %d)", entryID, len(doc.Entities))
	return nil
}

// TestProcessFlow_6458_Delta_ChainReachesConsumerCall measures the delta
// for :957 (cross_stack) and :1001 (crosses_external_lib) on a chain that
// structurally reaches a post-#1217 consumer synthetic via a FETCHES edge.
// The synthetic deliberately lives in a DIFFERENT file from the entry, so
// the :778 file-coarse fallback cannot be what flips cross_stack.
func TestProcessFlow_6458_Delta_ChainReachesConsumerCall(t *testing.T) {
	doc := &graph.Document{
		Repo: "fe",
		Entities: []graph.Entity{
			{ID: "a", Name: "loadDashboard", Kind: "SCOPE.Function", SourceFile: "dashboard.ts"},
			{ID: "b", Name: "buildView", Kind: "SCOPE.Function", SourceFile: "dashboard.ts"},
			{ID: "c", Name: "fetchOrders", Kind: "SCOPE.Function", SourceFile: "dashboard.ts"},
			// api.ts, not dashboard.ts — isolates :778 out of this test.
			consumerEnt("ep", "http_endpoint_call", "api.ts"),
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "a", ToID: "b", Kind: "CALLS"},
			{ID: "r2", FromID: "b", ToID: "c", Kind: "CALLS"},
			{ID: "r3", FromID: "c", ToID: "ep", Kind: "FETCHES"},
		},
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	props := procProps(t, doc, "a")

	if got := props["chain"]; got != "a,b,c,ep" {
		t.Fatalf("chain = %q, want %q — fixture no longer reaches the consumer synthetic", got, "a,b,c,ep")
	}
	// :957 — the chain lands on an unresolved consumer synthetic, so the
	// process leaves this repo.
	if props["cross_stack"] != strconv.FormatBool(true) {
		t.Errorf("cross_stack = %q, want \"true\" (:957 gate rejects http_endpoint_call)", props["cross_stack"])
	}
	if props["cross_stack_reason"] == "" {
		t.Errorf("cross_stack_reason is empty, want the unresolved-consumer reason")
	}
	// :1001 — the endpoint-ish switch arm.
	if props["crosses_external_lib"] != strconv.FormatBool(true) {
		t.Errorf("crosses_external_lib = %q, want \"true\" (:1001 arm omits http_endpoint_call)",
			props["crosses_external_lib"])
	}
}

// TestProcessFlow_6458_Delta_EntryFileFallback measures the delta for :778
// alone: the chain never touches the consumer synthetic, so only the
// file-coarse fallback can flip cross_stack.
func TestProcessFlow_6458_Delta_EntryFileFallback(t *testing.T) {
	doc := &graph.Document{
		Repo: "fe",
		Entities: []graph.Entity{
			{ID: "a", Name: "loadDashboard", Kind: "SCOPE.Function", SourceFile: "widget.ts"},
			{ID: "b", Name: "buildView", Kind: "SCOPE.Function", SourceFile: "widget.ts"},
			{ID: "c", Name: "render", Kind: "SCOPE.Function", SourceFile: "widget.ts"},
			// Same file as the entry, unreachable from the chain — this is
			// the fixture-e / class-field-arrow shape #754 describes.
			consumerEnt("ep", "http_endpoint_call", "widget.ts"),
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "a", ToID: "b", Kind: "CALLS"},
			{ID: "r2", FromID: "b", ToID: "c", Kind: "CALLS"},
		},
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	props := procProps(t, doc, "a")

	if got := props["chain"]; got != "a,b,c" {
		t.Fatalf("chain = %q, want %q", got, "a,b,c")
	}
	if props["cross_stack"] != strconv.FormatBool(true) {
		t.Errorf("cross_stack = %q, want \"true\" (:778 gate rejects http_endpoint_call)", props["cross_stack"])
	}
	// The chain itself touches no endpoint entity and no boundary edge,
	// so the file-coarse fallback must NOT bleed into this property.
	if props["crosses_external_lib"] != strconv.FormatBool(false) {
		t.Errorf("crosses_external_lib = %q, want \"false\" — the chain contains no endpoint step",
			props["crosses_external_lib"])
	}
}

// TestProcessFlow_6458_FallbackRespects1639Continuation is the regression
// the widening exposed, pinned in its own right.
//
// The #754 file-coarse fallback predates #1639. Its premise — "the entry
// file holds a consumer synthetic the BFS could not reach, so assume the
// process leaves the repo" — was written when landing on a consumer
// synthetic unconditionally meant cross-repo. #1639 later established the
// opposite for calls that resolve into a SAME-repo handler, and encoded it
// as the handler-continuation guard in chainCrossesRepoBoundary. The
// fallback never got that guard, because by then the :778 kind gate had
// already made it inert and there was nothing to reconcile.
//
// So un-breaking :778 revives pre-#1639 behaviour unless the fallback is
// also gated: here the chain structurally reaches the consumer synthetic
// AND continues into the same-repo handler, yet the fallback would stamp
// cross_stack=true with the reason "BFS chain didn't reach the bridge
// structurally" — a claim the chain itself contradicts.
func TestProcessFlow_6458_FallbackRespects1639Continuation(t *testing.T) {
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "caller", Name: "submitOrder", Kind: "SCOPE.Function", SourceFile: "client.go"},
		consumerEnt("call", "http_endpoint_call", "client.go"),
		producerEnt("def", "http_endpoint_definition", "handler.go"),
		{ID: "handler", Name: "CreateOrder", Kind: "SCOPE.Function", SourceFile: "handler.go"},
		{ID: "repo", Name: "saveOrder", Kind: "SCOPE.Function", SourceFile: "repo.go"},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "caller", ToID: "call", Kind: "FETCHES"},
		{ID: "2", FromID: "call", ToID: "def", Kind: "FETCHES"},
		{ID: "3", FromID: "handler", ToID: "def", Kind: "IMPLEMENTS"},
		{ID: "4", FromID: "handler", ToID: "repo", Kind: "CALLS"},
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	props := procProps(t, doc, "caller")

	if got := props["chain"]; got != "caller,call,def,handler,repo" {
		t.Fatalf("chain = %q — fixture no longer exercises the #1639 continuation", got)
	}
	if props["cross_stack"] != strconv.FormatBool(false) {
		t.Errorf("cross_stack = %q, want \"false\" — the call resolves into a same-repo handler (#1639). reason=%q",
			props["cross_stack"], props["cross_stack_reason"])
	}
	// The chain does touch HTTP endpoint entities, so this stays true —
	// that is the :1001 widening working as intended, and it is a
	// different question from whether the process leaves the repo.
	if props["crosses_external_lib"] != strconv.FormatBool(true) {
		t.Errorf("crosses_external_lib = %q, want \"true\"", props["crosses_external_lib"])
	}
}

// TestProcessFlow_6458_Delta_ProducerOnlyFileStaysIntraRepo is the
// negative half of the :778 measurement: a producer-only file must NOT
// gain cross_stack. This is the property that a careless "swap in
// IsHTTPEndpointKind and drop the pattern_type check" fix would break.
func TestProcessFlow_6458_Delta_ProducerOnlyFileStaysIntraRepo(t *testing.T) {
	doc := &graph.Document{
		Repo: "be",
		Entities: []graph.Entity{
			{ID: "a", Name: "Handler.Get", Kind: "SCOPE.Operation", SourceFile: "orders.go"},
			{ID: "b", Name: "Service.List", Kind: "SCOPE.Operation", SourceFile: "orders.go"},
			{ID: "c", Name: "Repo.Query", Kind: "SCOPE.Operation", SourceFile: "orders.go"},
			producerEnt("ep", "http_endpoint_definition", "orders.go"),
			webhookEnt("wh", "orders.go"),
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "a", ToID: "b", Kind: "CALLS"},
			{ID: "r2", FromID: "b", ToID: "c", Kind: "CALLS"},
		},
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	props := procProps(t, doc, "a")

	if props["cross_stack"] != strconv.FormatBool(false) {
		t.Errorf("cross_stack = %q, want \"false\" — producer + webhook synthetics are not cross-repo bridges",
			props["cross_stack"])
	}
}
