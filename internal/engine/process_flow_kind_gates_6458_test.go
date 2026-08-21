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
// The legacy kind is NOT unreachable. #6494 corrected the count: NINE
// live non-test sites still emit it — eight through the httpEndpointKind
// alias (http_endpoint_synthesis.go:65) in the FastAPI mount-point,
// Django urlconf/DRF/admin and Java annotation route synthesisers, plus
// webhooks_edges.go:111 writing the literal. All nine are producer-side,
// so :778 and :957 (which also require a consumer pattern_type /
// source_caller) stay inert.
//
// :1001 is therefore NOT repaired here. It keys on kind alone, so it
// already fires on all nine today — it is not a gate that rejects
// everything, and teaching it the split kinds would be a widening of a
// live, USER-VISIBLE path: `crosses_external_lib` is serialised onto every
// Process entity and rendered by webui-v2 as an "external lib" badge
// (routes/flows.tsx:254 and :1432, typed at data/types.ts:714). A purely
// first-party HTTP hop must not raise that badge. The tests below pin
// :1001's unchanged behaviour on both sides.
//
// None of these functions had any test before this file.
package engine

import (
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
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

// webhookEnt mirrors webhooks_edges.go:111 — one of the nine live
// producers of the bare legacy kind, and the only one that writes the
// literal rather than the httpEndpointKind alias.
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
			got := buildConsumerEndpointFileSet(doc, resolvedConsumerEndpoints(doc))[tc.ent.SourceFile]
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
			// #6494 — the load-bearing case. A first-party HTTP call site
			// with no third-party dependency anywhere must NOT raise the
			// dashboard's "external lib" badge. This assertion is what
			// fails if :1001 is widened to the #1217 split kinds.
			name: "http_endpoint_call step is first-party, not an external lib",
			ent:  consumerEnt("c1", "http_endpoint_call", "orders.ts"),
			want: false,
		},
		{
			// Same, producer side. In a real graph a definition usually
			// also carries an IMPLEMENTS/ROUTES_TO edge and IS caught by
			// the boundary set — that is a different, edge-based signal
			// and it stays. The KIND alone must not be enough.
			name: "http_endpoint_definition step without boundary edge",
			ent:  producerEnt("p1", "http_endpoint_definition", "orders.go"),
			want: false,
		},
		{
			name: "http_endpoint_call uppercase kind",
			ent:  consumerEnt("c2", "HTTP_ENDPOINT_CALL", "orders.ts"),
			want: false,
		},
		{
			// Pre-existing behaviour via the legacy arm — one of the nine
			// live emitters. Must not regress while narrowing.
			name: "webhook legacy http_endpoint step",
			ent:  webhookEnt("w1", "webhooks.go"),
			want: true,
		},
		{
			// The legacy arm is case-insensitive and must stay so.
			name: "legacy http_endpoint uppercase kind",
			ent:  graph.Entity{ID: "w2", Kind: "HTTP_ENDPOINT", SourceFile: "webhooks.go"},
			want: true,
		},
		// --- boundaries ---
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
	// :1001 — the endpoint-ish switch arm is deliberately NOT widened
	// (#6494). Nothing on this chain is a third-party dependency and no
	// step sits on an IMPLEMENTS / ROUTES_TO / SERVES edge, so the
	// dashboard's "external lib" badge must stay off. Widening :1001 to
	// the split kinds flips this to "true" and makes the badge lie.
	if props["crosses_external_lib"] != strconv.FormatBool(false) {
		t.Errorf("crosses_external_lib = %q, want \"false\" — a first-party HTTP call site is not an external lib",
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
	// Still true, but NOT because of the endpoint kinds: `def` sits on the
	// handler IMPLEMENTS edge, so buildHTTPBoundarySet already holds it.
	// That edge-based signal is the one :1001 has always had and it is
	// untouched by #6458.
	if props["crosses_external_lib"] != strconv.FormatBool(true) {
		t.Errorf("crosses_external_lib = %q, want \"true\" (via the IMPLEMENTS boundary set, not the kind switch)",
			props["crosses_external_lib"])
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

// ---------------------------------------------------------------------
// #6494 review probes — the per-endpoint continuation defect.
//
// #1639's handler-continuation rule was implemented chain-GLOBALLY: one
// continuation edge anywhere in the chain disabled the unresolved-consumer
// check for EVERY step. A chain that resolves one HTTP call in-repo and
// then makes a second, unresolved one was therefore reported intra-repo.
// The #754 file-coarse fallback was accidentally compensating for that;
// removing the compensation without fixing the root cause turns the bug
// into a false negative you can see in the emitted properties.
// ---------------------------------------------------------------------

// Probe A — BFF/gateway shape. The chain resolves callA into a same-repo
// handler, and that handler makes a SECOND, unresolved HTTP call. The
// consumer synthetics deliberately live outside the entry file so the
// #754 file-coarse fallback cannot participate: cross_stack here can only
// come from chainCrossesRepoBoundary.
func TestProcessFlow_6494_ProbeA_SecondUnresolvedCallAfterContinuation(t *testing.T) {
	doc := &graph.Document{Repo: "bff"}
	doc.Entities = []graph.Entity{
		{ID: "caller", Name: "submitOrder", Kind: "SCOPE.Function", SourceFile: "client.go"},
		consumerEnt("callA", "http_endpoint_call", "api.go"),
		producerEnt("defA", "http_endpoint_definition", "handler.go"),
		{ID: "handler", Name: "CreateOrder", Kind: "SCOPE.Function", SourceFile: "handler.go"},
		consumerEnt("callB", "http_endpoint_call", "api.go"),
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "caller", ToID: "callA", Kind: "FETCHES"},
		{ID: "2", FromID: "callA", ToID: "defA", Kind: "FETCHES"},
		{ID: "3", FromID: "handler", ToID: "defA", Kind: "IMPLEMENTS"},
		{ID: "4", FromID: "handler", ToID: "callB", Kind: "FETCHES"},
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	props := procProps(t, doc, "caller")

	if got := props["chain"]; got != "caller,callA,defA,handler,callB" {
		t.Fatalf("chain = %q — fixture no longer exercises the second unresolved call", got)
	}
	// The entry file holds no consumer synthetic, so the #754 fallback is
	// out of the picture by construction.
	if buildConsumerEndpointFileSet(doc, resolvedConsumerEndpoints(doc))["client.go"] {
		t.Fatalf("probe is not isolated: entry file client.go is in the consumer-endpoint file set")
	}
	if props["cross_stack"] != strconv.FormatBool(true) {
		t.Errorf("cross_stack = %q, want \"true\" — the chain terminates on callB, an unresolved consumer synthetic whose backend is in another repo",
			props["cross_stack"])
	}
	if props["cross_stack_reason"] == "" {
		t.Errorf("cross_stack_reason is empty, want the unresolved-consumer reason naming callB")
	}
	if got := props["cross_stack_reason"]; got != "" && !strings.Contains(got, "callB") {
		t.Errorf("cross_stack_reason = %q, want it to name the unresolved endpoint callB", got)
	}
}

// Probe B — fan-out. The caller resolves one endpoint into a same-repo
// handler and separately calls a SECOND, unresolved endpoint that is not
// on the primary chain but does sit in the entry file. That is exactly
// the shape the #754 file-coarse fallback exists for; it must still fire.
func TestProcessFlow_6494_ProbeB_FanOutUnresolvedSiblingInEntryFile(t *testing.T) {
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "caller", Name: "submitOrder", Kind: "SCOPE.Function", SourceFile: "client.go"},
		consumerEnt("callA", "http_endpoint_call", "client.go"),
		producerEnt("defA", "http_endpoint_definition", "handler.go"),
		{ID: "handler", Name: "CreateOrder", Kind: "SCOPE.Function", SourceFile: "handler.go"},
		{ID: "repo", Name: "saveOrder", Kind: "SCOPE.Function", SourceFile: "repo.go"},
		// Second call: no http_endpoint_definition in this doc, so it is
		// unresolved — its backend lives in another repo.
		consumerEnt("callB", "http_endpoint_call", "client.go"),
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "caller", ToID: "callA", Kind: "FETCHES"},
		{ID: "2", FromID: "callA", ToID: "defA", Kind: "FETCHES"},
		{ID: "3", FromID: "handler", ToID: "defA", Kind: "IMPLEMENTS"},
		{ID: "4", FromID: "handler", ToID: "repo", Kind: "CALLS"},
		{ID: "5", FromID: "caller", ToID: "callB", Kind: "FETCHES"},
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	props := procProps(t, doc, "caller")

	if got := props["chain"]; got != "caller,callA,defA,handler,repo" {
		t.Fatalf("chain = %q — fixture no longer exercises the fan-out shape", got)
	}
	if props["cross_stack"] != strconv.FormatBool(true) {
		t.Errorf("cross_stack = %q, want \"true\" — callB is an unresolved consumer synthetic in the entry file",
			props["cross_stack"])
	}
}

// The producer-only counterpart of probe A's isolation check, and the
// fixture the W1 widening mutant needs: the chain touches ONLY a producer
// http_endpoint_definition while the entry file separately holds a
// consumer synthetic that the BFS cannot reach. The #754 fallback is the
// only thing that can flip cross_stack here, and it must — a producer
// definition on the chain is not a cross-repo bridge.
func TestProcessFlow_6494_ProducerOnChainDoesNotSuppressFallback(t *testing.T) {
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "a", Name: "loadWidget", Kind: "SCOPE.Function", SourceFile: "widget.ts"},
		{ID: "b", Name: "render", Kind: "SCOPE.Function", SourceFile: "widget.ts"},
		producerEnt("def", "http_endpoint_definition", "widget.ts"),
		// Unreachable from the chain (fixture-e class-field-arrow shape).
		consumerEnt("ep", "http_endpoint_call", "widget.ts"),
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "a", ToID: "b", Kind: "CALLS"},
		{ID: "2", FromID: "b", ToID: "def", Kind: "CALLS"},
	}
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	props := procProps(t, doc, "a")

	if got := props["chain"]; got != "a,b,def" {
		t.Fatalf("chain = %q, want \"a,b,def\"", got)
	}
	if props["cross_stack"] != strconv.FormatBool(true) {
		t.Errorf("cross_stack = %q, want \"true\" — a producer definition on the chain is not a consumer bridge and must not suppress the #754 fallback",
			props["cross_stack"])
	}
}

// Probe C — the boundaries of "resolves against a same-repo route".
//
// #6494 round 3: this probe used to assert that a definition with NO
// inbound IMPLEMENTS edge left the consumer UNresolved, i.e. that the
// process "leaves the repo". That was wrong, and pinning it canonised a
// false positive.
//
// `call --FETCHES--> definition` IS the in-repo resolution signal:
// http_endpoint_resolve.go stamps it pattern_type=
// "http_endpoint_split_resolved" / resolved="true" and emits it only when
// a matching definition was found in this document. The IMPLEMENTS edge
// answers a different question — whether HANDLER BINDING also succeeded —
// and that step fails routinely on real code (stats.NoHandlerProp,
// HandlerDropped, HandlerUnresolved, HandlerUnresolvedKept "the endpoint
// survived unenriched", plus the #4319 co-location fallback that exists
// because binding fails on real NestJS). Routes synthesised from regex /
// YAML / mount points never carry a source_handler at all
// (http_endpoint_synthesis.go gates it on refName != ""), so they can
// never acquire an IMPLEMENTS edge. Requiring one turned "the handler
// wasn't bound" into "the backend lives in another repo".
//
// What remains load-bearing: the edge must actually be a FETCHES edge,
// and it must land on an http_endpoint_definition in THIS document.
func TestProcessFlow_6494_ProbeC_ResolutionBoundaries(t *testing.T) {
	tests := []struct {
		name         string
		edgeKind     string
		target       graph.Entity
		wantResolved bool
	}{
		{
			// The correction. No handler is bound to the definition, but
			// the call still resolved against an in-repo route.
			name:         "definition with no handler IMPLEMENTS edge still resolves in-repo",
			edgeKind:     RelationshipKindFetches,
			target:       producerEnt("def", "http_endpoint_definition", "handler.go"),
			wantResolved: true,
		},
		{
			name:         "consumer edge is not FETCHES",
			edgeKind:     "REFERENCES",
			target:       producerEnt("def", "http_endpoint_definition", "handler.go"),
			wantResolved: false,
		},
		{
			// The TO-leg guard, far miss. A FETCHES edge out of a consumer
			// that does not land on an http_endpoint_definition is not a
			// resolution; counting it would make every consumer read as
			// intra-repo.
			name:         "FETCHES edge that does not land on a definition",
			edgeKind:     RelationshipKindFetches,
			target:       graph.Entity{ID: "def", Name: "OrderClient", Kind: "SCOPE.Class", SourceFile: "handler.go"},
			wantResolved: false,
		},
		{
			// The TO-leg guard, NEAR miss — the drift this PR makes most
			// likely, because it widens the other two gates from the exact
			// "http_endpoint_definition" comparison to
			// types.IsHTTPEndpointKind. Widening THIS one too would admit
			// the bare legacy kind (the webhook synthetics among the nine
			// live producers) as a definition, and a consumer FETCHES edge
			// landing on one would wrongly suppress cross_stack.
			name:         "FETCHES edge landing on a legacy bare http_endpoint is not a definition",
			edgeKind:     RelationshipKindFetches,
			target:       webhookEnt("def", "handler.go"),
			wantResolved: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			target := tc.target
			doc := &graph.Document{Repo: "r"}
			doc.Entities = []graph.Entity{
				{ID: "caller", Name: "submitOrder", Kind: "SCOPE.Function", SourceFile: "client.go"},
				consumerEnt("call", "http_endpoint_call", "api.go"),
				target,
				// An unrelated route this repo serves, connected to
				// nothing. Without it the predicate short-circuits on
				// "this document declares no definitions at all" and the
				// TO-leg is never reached — which is exactly how the
				// round-2 mutant on that leg survived.
				producerEnt("otherDef", "http_endpoint_definition", "other.go"),
			}
			doc.Relationships = []graph.Relationship{
				{ID: "1", FromID: "caller", ToID: "call", Kind: "FETCHES"},
				// The ONLY consumer → target edge in this fixture.
				{ID: "2", FromID: "call", ToID: "def", Kind: tc.edgeKind},
			}
			if got := resolvedConsumerEndpoints(doc)["call"]; got != tc.wantResolved {
				t.Fatalf("resolvedConsumerEndpoints[call] = %v, want %v (edge=%q targetKind=%q)",
					got, tc.wantResolved, tc.edgeKind, tc.target.Kind)
			}
			RunProcessFlow(doc, DefaultProcessFlowConfig())
			props := procProps(t, doc, "caller")
			if buildConsumerEndpointFileSet(doc, resolvedConsumerEndpoints(doc))["client.go"] {
				t.Fatalf("probe is not isolated: entry file client.go is in the consumer-endpoint file set")
			}
			// cross_stack is the negation of resolution here: the chain's
			// only cross-repo candidate is `call`.
			wantCross := strconv.FormatBool(!tc.wantResolved)
			if props["cross_stack"] != wantCross {
				t.Errorf("cross_stack = %q, want %q. chain=%q reason=%q",
					props["cross_stack"], wantCross, props["chain"], props["cross_stack_reason"])
			}
		})
	}
}

// TestResolvedConsumerEndpoints_6494_HandlerBindingIsSeparate is the F2
// correction stated as its own regression, at the unit level and free of
// the BFS.
//
// Two consumers in one document. Both resolve against an in-repo
// definition over a FETCHES edge; only one of the two definitions has a
// handler bound to it by an IMPLEMENTS edge. Under the old predicate the
// unbound one was reported unresolved — which the emitted Process then
// explained as "backend in another repo", about a route declared twenty
// lines away.
func TestResolvedConsumerEndpoints_6494_HandlerBindingIsSeparate(t *testing.T) {
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		consumerEnt("callBound", "http_endpoint_call", "api.ts"),
		consumerEnt("callUnbound", "http_endpoint_call", "api.ts"),
		producerEnt("defBound", "http_endpoint_definition", "routes.py"),
		// The regex/YAML/mount-derived shape: synthesised without a
		// source_handler, so no IMPLEMENTS edge can ever exist for it.
		producerEnt("defUnbound", "http_endpoint_definition", "urls.py"),
		{ID: "handler", Name: "get_orders", Kind: "SCOPE.Function", SourceFile: "routes.py"},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "callBound", ToID: "defBound", Kind: "FETCHES"},
		{ID: "2", FromID: "handler", ToID: "defBound", Kind: "IMPLEMENTS"},
		{ID: "3", FromID: "callUnbound", ToID: "defUnbound", Kind: "FETCHES"},
	}
	got := resolvedConsumerEndpoints(doc)
	for _, id := range []string{"callBound", "callUnbound"} {
		if !got[id] {
			t.Errorf("resolvedConsumerEndpoints[%s] = false, want true — the call resolved against an in-repo route; whether a handler was bound to that route is a different question", id)
		}
	}
}

// TestChainCrossesRepoBoundary_6494_EntryStepGuard covers the SECOND
// consultation site of resolvedConsumers — the entry-step check after the
// pairwise walk. A one-element chain runs zero pairwise iterations, so it
// exercises that site and nothing else. Probes A and B both perturb both
// sites at once and so prove nothing about this one.
func TestChainCrossesRepoBoundary_6494_EntryStepGuard(t *testing.T) {
	consumer := consumerEnt("call", "http_endpoint_call", "api.ts")
	byID := map[string]*graph.Entity{"call": &consumer}

	t.Run("unresolved entry consumer crosses the boundary", func(t *testing.T) {
		cross, reason := chainCrossesRepoBoundary([]string{"call"}, byID, nil, map[string]bool{})
		if !cross {
			t.Fatal("cross = false, want true — an unresolved consumer synthetic as the entry leaves the repo")
		}
		if !strings.Contains(reason, "step 0") {
			t.Fatalf("reason = %q, want the step-0 entry reason", reason)
		}
	})
	t.Run("resolved entry consumer stays intra-repo", func(t *testing.T) {
		cross, reason := chainCrossesRepoBoundary([]string{"call"}, byID, nil, map[string]bool{"call": true})
		if cross {
			t.Fatalf("cross = true (reason %q), want false — the entry consumer resolved against an in-repo route", reason)
		}
	})
}

// TestResolvedConsumerEndpointsMulti_6494_SpansCompanions pins the F3
// asymmetry. Every other sibling set built in RunProcessFlowWithCompanions
// spans doc + companions (byID, the HTTP boundary set, the CALLS
// adjacency); this one was the only doc-only set, yet it is consulted
// against byID, which does span companions. A consumer synthetic living in
// a companion could therefore never be found resolved.
//
// Classification stays per-document: a definition in repo A must not
// resolve a call in repo B.
func TestResolvedConsumerEndpointsMulti_6494_SpansCompanions(t *testing.T) {
	companion := &graph.Document{Repo: "bff"}
	companion.Entities = []graph.Entity{
		consumerEnt("bffCall", "http_endpoint_call", "bff/api.ts"),
		producerEnt("bffDef", "http_endpoint_definition", "bff/routes.ts"),
	}
	companion.Relationships = []graph.Relationship{
		{ID: "c1", FromID: "bffCall", ToID: "bffDef", Kind: "FETCHES"},
	}
	// The frontend holds a consumer whose backend is elsewhere, plus a
	// definition of its own that must not resolve the companion's call.
	doc := &graph.Document{Repo: "fe"}
	doc.Entities = []graph.Entity{
		{ID: "a", Name: "loadDashboard", Kind: "SCOPE.Function", SourceFile: "fe/app.ts"},
		consumerEnt("feCall", "http_endpoint_call", "fe/api.ts"),
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "a", ToID: "feCall", Kind: "FETCHES"},
		// A consumer FETCHES edge whose TARGET is a definition in the OTHER
		// document. Per-document collection never sees it resolve: the
		// frontend declares no definitions of its own, so its pass
		// short-circuits before the edge walk. Merging the documents and
		// classifying once would resolve it — this edge is what makes the
		// feCall assertion below discriminate that mutant. The pipeline
		// never produces such an edge (cross-repo links are materialised as
		// Kind:"CALLS" phantoms with cross_repo=true, and graph.EntityID
		// salts the hash with the repo tag so IDs cannot collide across
		// documents), so this pins an invariant, not a live bug.
		{ID: "2", FromID: "feCall", ToID: "bffDef", Kind: "FETCHES"},
	}

	got := resolvedConsumerEndpointsMulti(doc, []*graph.Document{companion})
	if !got["bffCall"] {
		t.Errorf("resolvedConsumerEndpointsMulti[bffCall] = false, want true — the companion's call resolves against the companion's own definition")
	}
	if got["feCall"] {
		t.Errorf("resolvedConsumerEndpointsMulti[feCall] = true, want false — feCall has no definition in its own document, and its FETCHES edge into the companion's bffDef must not resolve it")
	}

	// Cross-document resolution must NOT happen: move the definition into
	// the companion only and the frontend call stays unresolved (asserted
	// above). Now prove the same in the other direction — a doc-local
	// definition does not resolve a companion call.
	doc.Entities = append(doc.Entities, producerEnt("feDef", "http_endpoint_definition", "fe/routes.ts"))
	doc.Relationships = append(doc.Relationships,
		graph.Relationship{ID: "3", FromID: "bffCall", ToID: "feDef", Kind: "FETCHES"})
	if resolvedConsumerEndpointsMulti(doc, nil)["bffCall"] {
		t.Error("a definition in the frontend document must not resolve a consumer that lives in another document")
	}
}

// TestProcessFlow_6494_CompanionConsumerResolves is the end-to-end half of
// F3: the map is consulted through byID, so a chain that steps onto a
// companion-resident consumer synthetic must see that companion's own
// resolution. With the doc-only build this reports cross_stack=true with
// the reason "backend in another repo" about a call the companion resolves
// against its own route.
func TestProcessFlow_6494_CompanionConsumerResolves(t *testing.T) {
	companion := &graph.Document{Repo: "bff"}
	companion.Entities = []graph.Entity{
		consumerEnt("bffCall", "http_endpoint_call", "bff/api.ts"),
		producerEnt("bffDef", "http_endpoint_definition", "bff/routes.ts"),
	}
	companion.Relationships = []graph.Relationship{
		{ID: "c1", FromID: "bffCall", ToID: "bffDef", Kind: "FETCHES"},
	}
	doc := &graph.Document{Repo: "fe"}
	doc.Entities = []graph.Entity{
		{ID: "a", Name: "loadDashboard", Kind: "SCOPE.Function", SourceFile: "fe/app.ts"},
		{ID: "b", Name: "buildView", Kind: "SCOPE.Function", SourceFile: "fe/app.ts"},
		{ID: "c", Name: "callBff", Kind: "SCOPE.Function", SourceFile: "fe/app.ts"},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "1", FromID: "a", ToID: "b", Kind: "CALLS"},
		{ID: "2", FromID: "b", ToID: "c", Kind: "CALLS"},
		{ID: "3", FromID: "c", ToID: "bffCall", Kind: "FETCHES"},
	}
	RunProcessFlowWithCompanions(doc, []*graph.Document{companion}, DefaultProcessFlowConfig())
	props := procProps(t, doc, "a")

	if got := props["chain"]; got != "a,b,c,bffCall,bffDef" {
		t.Fatalf("chain = %q — fixture no longer reaches the companion consumer synthetic", got)
	}
	if props["cross_stack"] != strconv.FormatBool(false) {
		t.Errorf("cross_stack = %q, want \"false\" — bffCall resolves against bffDef inside the companion document. reason=%q",
			props["cross_stack"], props["cross_stack_reason"])
	}
}

// TestLookupHandler_RouteEquivalence_6494 covers the fourth stale-kind
// site the #6458 audit missed: response_shape_corpus.go's cross-kind
// handler-lookup table listed the bare pre-#1217 literal for "Route", so
// a Route entity whose handler is a post-split endpoint synthetic fell
// through to the by-name last-ditch fallback (which explicitly skips
// endpoint synthetics) and resolved to nothing.
func TestLookupHandler_RouteEquivalence_6494(t *testing.T) {
	for _, kind := range []string{httpEndpointKind, httpEndpointDefinitionKind, httpEndpointCallKind} {
		t.Run(kind, func(t *testing.T) {
			ent := &types.EntityRecord{Name: "GetOrders", Kind: kind}
			idx := map[handlerKey]*types.EntityRecord{{kind, "GetOrders"}: ent}
			got := lookupHandler("Route", "GetOrders", idx, map[string][]*types.EntityRecord{})
			if got != ent {
				t.Fatalf("lookupHandler(Route, GetOrders) = %v, want the %s entity", got, kind)
			}
		})
	}
	t.Run("unrelated kind is not reached from Route", func(t *testing.T) {
		ent := &types.EntityRecord{Name: "GetOrders", Kind: "SCOPE.Datastore"}
		idx := map[handlerKey]*types.EntityRecord{{"SCOPE.Datastore", "GetOrders"}: ent}
		if got := lookupHandler("Route", "GetOrders", idx, map[string][]*types.EntityRecord{}); got != nil {
			t.Fatalf("lookupHandler(Route, GetOrders) = %v, want nil", got)
		}
	})
}
