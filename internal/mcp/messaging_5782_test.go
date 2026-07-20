package mcp

// messaging_5782_test.go — grafel #5782 Phase 1 (query-layer, framework-agnostic).
//
// Three cross-repo message-topic gaps, all in the MCP query layer over graph
// data that ALREADY exists (per-repo SCOPE.MessageTopic entities keyed by a
// broker-prefixed Name, intra-repo PUBLISHES_TO / SUBSCRIBES_TO / DELIVERS_TO
// edges, and cross-repo topic joins in lg.Links):
//
//   #3  grafel_related direction=messaging returns a topic's producers +
//       consumers ACROSS repos (was: empty, because the neighbor handlers
//       return on the first repo containing the resolved entity).
//   #2  grafel_impact_radius on a topic includes cross-repo consumers in other
//       repos (was: intra-repo only — walked one resolved repo's adjacency).
//   bug grafel_debt kind=stubs without group_v3+group_oracle returns a clear
//       "two-group comparison mode" gated error, not the bare
//       "group_v3 and group_oracle are both required".

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// hasFold reports whether s contains sub, case-insensitively.
func hasFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// newMessagingTestServer builds a two-repo group with the SAME broker-prefixed
// topic Name appearing as a per-repo SCOPE.MessageTopic entity in each repo:
//
//	repo "producer": OrderService.placeOrder --PUBLISHES_TO--> kafka:orders.placed
//	repo "consumer": InventoryConsumer.onOrder --SUBSCRIBES_TO--> kafka:orders.placed
//	                 kafka:orders.placed --DELIVERS_TO--> InventoryConsumer.onOrder
//
// plus the cross-repo topic join in lg.Links (Method="topic") the topic pass
// (internal/links/topic_pass.go P7) writes: publisher-entity → subscriber-entity.
func newMessagingTestServer(t *testing.T) *Server {
	t.Helper()

	const topicName = "kafka:orders.placed"

	// The ChannelBinding entity is stored under a content-HASH id (bindingHashID),
	// exactly like the real fbwriter graph — while its BINDS_CHANNEL/BINDS_TOPIC
	// edges reference it by the DANGLING synthetic "scope:channelbinding:..."
	// FromID the config recognizer emitted (which resolves to NO entity: the
	// resolver rewrites only the edge ToID, never the FromID, and the entity id
	// is re-hashed). This asymmetry is the #5782 live-corpus finding: a naive
	// adj.Outgoing(binding.ID) walk finds nothing because the edges are keyed
	// under the synthetic FromID, not the entity's hash id.
	const bindingHashID = "cb:db417080c9d5bd11"
	const bindingSyntheticID = "scope:channelbinding:spring_properties:application.properties:outgoing:orders-out"

	producerDoc := minDoc(
		[]graph.Entity{
			{ID: "op:placeOrder", Name: "OrderService.placeOrder", Kind: "SCOPE.Operation", SourceFile: "order_service.go", StartLine: 12, QualifiedName: "OrderService.placeOrder"},
			{ID: "topic:orders", Name: topicName, Kind: "SCOPE.MessageTopic", SourceFile: ""},
			graph.Entity{
				ID: bindingHashID, Name: "orders-out", Kind: "SCOPE.ChannelBinding", SourceFile: "application.properties", StartLine: 1,
				QualifiedName: "producer::application.properties#outgoing:orders-out",
			}.WithProperties(map[string]string{"channel": "orders-out", "direction": "outgoing", "connector": "smallrye-kafka", "topic": "orders.placed"}),
		},
		[]graph.Relationship{
			{ID: "pub:1", FromID: "op:placeOrder", ToID: "topic:orders", Kind: "PUBLISHES_TO"},
			// BINDS_* edges: dangling synthetic FromID (NOT bindingHashID), resolved ToID.
			graph.Relationship{ID: "bindch:1", FromID: bindingSyntheticID, ToID: "op:placeOrder", Kind: "BINDS_CHANNEL"}.WithProperties(map[string]string{"channel": "orders-out", "direction": "outgoing"}),
			graph.Relationship{ID: "bindtop:1", FromID: bindingSyntheticID, ToID: "topic:orders", Kind: "BINDS_TOPIC"}.WithProperties(map[string]string{"topic": "orders.placed"}),
		},
	)
	producerDoc.Repo = "producer"

	consumerDoc := minDoc(
		[]graph.Entity{
			{ID: "op:onOrder", Name: "InventoryConsumer.onOrder", Kind: "SCOPE.Operation", SourceFile: "inventory_consumer.go", StartLine: 30, QualifiedName: "InventoryConsumer.onOrder"},
			{ID: "topic:orders", Name: topicName, Kind: "SCOPE.MessageTopic", SourceFile: ""},
		},
		[]graph.Relationship{
			{ID: "sub:1", FromID: "op:onOrder", ToID: "topic:orders", Kind: "SUBSCRIBES_TO"},
			graph.Relationship{ID: "del:1", FromID: "topic:orders", ToID: "op:onOrder", Kind: "DELIVERS_TO"}.WithProperties(map[string]string{"trigger": "async", "broker": "kafka"}),
		},
	)
	consumerDoc.Repo = "consumer"

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{
			"producer": {Path: t.TempDir()},
			"consumer": {Path: t.TempDir()},
		}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	lg := &LoadedGroup{
		Name: "test",
		Repos: map[string]*LoadedRepo{
			"producer": {Repo: "producer", Doc: producerDoc, LabelIndex: BuildLabelIndex(producerDoc)},
			"consumer": {Repo: "consumer", Doc: consumerDoc, LabelIndex: BuildLabelIndex(consumerDoc)},
		},
		Links: []CrossRepoLink{
			{
				Source:     "producer::op:placeOrder",
				Target:     "consumer::op:onOrder",
				Relation:   "publishes_to",
				Method:     "topic",
				Channel:    "kafka",
				Identifier: topicName,
			},
		},
	}
	st.groups["test"] = lg
	st.mu.Unlock()

	return &Server{State: st, Tel: NewTelemetry(0)}
}

// collectNames flattens a []any of neighbor maps into a repo-keyed name set.
func collectNames(rows any) map[string]string {
	out := map[string]string{}
	rr, _ := rows.([]any)
	for _, r := range rr {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		repo, _ := m["repo"].(string)
		out[name] = repo
	}
	return out
}

// ---------------------------------------------------------------------------
// ITEM #3 — grafel_related direction=messaging
// ---------------------------------------------------------------------------

// TestMessagingRelated_CrossRepoProducersConsumers is the #3 assertion:
// direction=messaging on a topic surfaces its producers AND consumers even when
// they live in sibling repos. Resolved by topic Name.
func TestMessagingRelated_CrossRepoProducersConsumers(t *testing.T) {
	srv := newMessagingTestServer(t)
	out := callFlowTool(t, srv.handleCoreRelated, map[string]any{
		"direction": "messaging",
		"entity_id": "kafka:orders.placed",
	})
	if out == nil {
		t.Fatal("nil/non-JSON result")
	}

	producers := collectNames(out["producers"])
	consumers := collectNames(out["consumers"])

	if repo := producers["OrderService.placeOrder"]; repo != "producer" {
		t.Errorf("expected producer OrderService.placeOrder in repo producer, got producers=%+v", producers)
	}
	if repo := consumers["InventoryConsumer.onOrder"]; repo != "consumer" {
		t.Errorf("expected consumer InventoryConsumer.onOrder in repo consumer, got consumers=%+v", consumers)
	}
}

// TestMessagingRelated_ResolveByPrefixedID resolves the topic by its prefixed
// entity_id (repo::localID) rather than by Name, and still folds the sibling
// repo's subscriber.
func TestMessagingRelated_ResolveByPrefixedID(t *testing.T) {
	srv := newMessagingTestServer(t)
	out := callFlowTool(t, srv.handleCoreRelated, map[string]any{
		"direction": "messaging",
		"entity_id": "producer::topic:orders",
	})
	if out == nil {
		t.Fatal("nil/non-JSON result")
	}
	consumers := collectNames(out["consumers"])
	if _, ok := consumers["InventoryConsumer.onOrder"]; !ok {
		t.Errorf("expected cross-repo consumer InventoryConsumer.onOrder, got consumers=%+v", consumers)
	}
}

// TestCoreRelated_ExistingDirectionsUnchanged is the guard: adding the
// messaging direction must not break the existing discriminator values.
func TestCoreRelated_ExistingDirectionsUnchanged(t *testing.T) {
	srv := newTestServer(t, buildChainDoc())
	// direction=callers on FuncB should still return FuncA.
	out := callFlowTool(t, srv.handleCoreRelated, map[string]any{
		"direction": "callers",
		"entity_id": "ent-b",
	})
	callers, _ := out["callers"].([]any)
	if len(callers) == 0 {
		t.Fatalf("callers direction regressed: expected FuncA, got %+v", out)
	}
}

// ---------------------------------------------------------------------------
// #5782 (issue #5782 asks #3 / #5) — agent-facing query-surface gaps.
// ---------------------------------------------------------------------------

// TestRelated_NeighborsDefaultResolvesMessageTopic is fix A: an agent that
// calls grafel_related with NO direction (or direction=neighbors) on a
// SCOPE.MessageTopic must not see the empty {callers:[],callees:[]} the
// generic CALLS-only neighbors handler returns for a topic — it should get
// the same cross-repo producers/consumers/handlers view as direction=messaging.
func TestRelated_NeighborsDefaultResolvesMessageTopic(t *testing.T) {
	srv := newMessagingTestServer(t)

	// direction=neighbors — an agent's natural "what's connected to this?"
	// call, previously an empty {callers:[],callees:[]} for a topic.
	out := callFlowTool(t, srv.handleCoreRelated, map[string]any{
		"direction": "neighbors",
		"entity_id": "kafka:orders.placed",
	})
	if got, _ := out["direction"].(string); got != "neighbors" {
		t.Errorf("expected direction=neighbors echoed back (not messaging), got %q: %+v", got, out)
	}
	producers := collectNames(out["producers"])
	consumers := collectNames(out["consumers"])
	if repo := producers["OrderService.placeOrder"]; repo != "producer" {
		t.Errorf("direction=neighbors on a MessageTopic must surface producers, got %+v", out)
	}
	if repo := consumers["InventoryConsumer.onOrder"]; repo != "consumer" {
		t.Errorf("direction=neighbors on a MessageTopic must surface cross-repo consumers, got %+v", out)
	}
	if n, _ := out["count"].(float64); n <= 0 {
		t.Errorf("direction=neighbors on a MessageTopic must not return an empty result, got %+v", out)
	}

	// direction=both takes the same path (the two discriminator values are
	// synonyms upstream of the messaging-aware resolution).
	out2 := callFlowTool(t, srv.handleCoreRelated, map[string]any{
		"direction": "both",
		"entity_id": "kafka:orders.placed",
	})
	if n, _ := out2["count"].(float64); n <= 0 {
		t.Errorf("direction=both on a MessageTopic must not return an empty result, got %+v", out2)
	}
}

// TestRelated_NeighborsDefaultResolvesChannelBinding is fix A's ChannelBinding
// case: direction=neighbors on a SCOPE.ChannelBinding surfaces its bound
// channel (BINDS_CHANNEL) and topic (BINDS_TOPIC).
func TestRelated_NeighborsDefaultResolvesChannelBinding(t *testing.T) {
	srv := newMessagingTestServer(t)
	out := callFlowTool(t, srv.handleCoreRelated, map[string]any{
		"direction": "neighbors",
		"entity_id": "producer::cb:db417080c9d5bd11",
	})
	if got, _ := out["kind"].(string); got != "channel_binding" {
		t.Fatalf("expected kind=channel_binding, got %+v", out)
	}
	channels := collectNames(out["channels"])
	topics := collectNames(out["topics"])
	if _, ok := channels["OrderService.placeOrder"]; !ok {
		t.Errorf("expected bound channel OrderService.placeOrder, got channels=%+v", channels)
	}
	if _, ok := topics["kafka:orders.placed"]; !ok {
		t.Errorf("expected bound topic kafka:orders.placed, got topics=%+v", topics)
	}
	if n, _ := out["count"].(float64); n != 2 {
		t.Errorf("expected count=2 (one channel + one topic), got %v", out["count"])
	}
}

// TestRelated_MessagingDirectionStillDocumentedAndWorks guards that adding
// the neighbors auto-resolution did not regress the explicit
// direction=messaging path.
func TestRelated_MessagingDirectionStillDocumentedAndWorks(t *testing.T) {
	srv := newMessagingTestServer(t)
	out := callFlowTool(t, srv.handleCoreRelated, map[string]any{
		"direction": "messaging",
		"entity_id": "kafka:orders.placed",
	})
	if got, _ := out["direction"].(string); got != "messaging" {
		t.Errorf("explicit direction=messaging must echo back direction=messaging, got %q", got)
	}
}

// TestRelated_MsgAliasResolvesLikeMessaging is #5782 follow-up 1: the "msg"
// abbreviation advertised in the tool's top-level description must resolve
// identically to direction=messaging (an agent copying the summary value must
// not hit a validation error).
func TestRelated_MsgAliasResolvesLikeMessaging(t *testing.T) {
	srv := newMessagingTestServer(t)
	msg := callFlowTool(t, srv.handleCoreRelated, map[string]any{
		"direction": "msg",
		"entity_id": "kafka:orders.placed",
	})
	full := callFlowTool(t, srv.handleCoreRelated, map[string]any{
		"direction": "messaging",
		"entity_id": "kafka:orders.placed",
	})
	if p1, p2 := collectNames(msg["producers"]), collectNames(full["producers"]); len(p1) == 0 || len(p1) != len(p2) {
		t.Errorf("direction=msg must resolve like messaging: producers msg=%+v messaging=%+v", p1, p2)
	}
	if c1, c2 := collectNames(msg["consumers"]), collectNames(full["consumers"]); len(c1) == 0 || len(c1) != len(c2) {
		t.Errorf("direction=msg must resolve like messaging: consumers msg=%+v messaging=%+v", c1, c2)
	}
}

// TestRelated_NeighborsBothEchoesBoth is #5782 follow-up 2: the messaging-aware
// neighbors path must echo the caller's original direction, not a hard-coded
// "neighbors", when the caller passed "both".
func TestRelated_NeighborsBothEchoesBoth(t *testing.T) {
	srv := newMessagingTestServer(t)
	out := callFlowTool(t, srv.handleCoreRelated, map[string]any{
		"direction": "both",
		"entity_id": "kafka:orders.placed",
	})
	if got, _ := out["direction"].(string); got != "both" {
		t.Errorf("direction=both must echo back %q, got %q", "both", got)
	}
}

// TestRelated_NeighborsNonTopicFallsThrough is #5782 follow-up 3(a), the
// fall-through regression guard: a NON-topic entity that merely has a
// PUBLISHES_TO edge (here the OrderService.placeOrder Operation) under
// direction=neighbors must take the GENERIC neighbors path — returning the
// {callers,callees} shape — and must NOT be served the messaging view
// (producers/consumers/handlers). Guards against tryMessagingNeighbors
// over-matching.
func TestRelated_NeighborsNonTopicFallsThrough(t *testing.T) {
	srv := newMessagingTestServer(t)
	out := callFlowTool(t, srv.handleCoreRelated, map[string]any{
		"direction": "neighbors",
		"entity_id": "producer::op:placeOrder",
	})
	if _, ok := out["producers"]; ok {
		t.Errorf("a non-topic Operation must NOT get the messaging view, got %+v", out)
	}
	if _, ok := out["consumers"]; ok {
		t.Errorf("a non-topic Operation must NOT get the messaging view, got %+v", out)
	}
	// The generic neighbors path returns callers and callees keys.
	_, hasCallers := out["callers"]
	_, hasCallees := out["callees"]
	if !hasCallers && !hasCallees {
		t.Errorf("expected the generic {callers,callees} neighbors shape, got keys %v", mapKeys(out))
	}
}

// TestInspect_MessageTopic_FoldedEdgesDedupedExactlyOnce is #5782 follow-up
// 3(b), the dedup regression guard: each folded semantic edge kind on a
// MessageTopic must appear EXACTLY once (not double-counted between the local
// adjacency walk and the cross-repo collectTopicNeighbors fold).
func TestInspect_MessageTopic_FoldedEdgesDedupedExactlyOnce(t *testing.T) {
	srv := newMessagingTestServer(t)
	out := callFlowTool(t, srv.handleGetNode, map[string]any{
		"entity_id": "producer::topic:orders",
	})
	sem, _ := out["semantic_edges"].([]any)
	counts := map[string]int{}
	for _, row := range sem {
		if m, ok := row.(map[string]any); ok {
			if k, _ := m["kind"].(string); k != "" {
				counts[k]++
			}
		}
	}
	for _, kind := range []string{"PUBLISHES_TO", "SUBSCRIBES_TO", "DELIVERS_TO", "BINDS_TOPIC"} {
		if counts[kind] != 1 {
			t.Errorf("expected exactly ONE %s edge on the topic, got %d (semantic_edges=%v)", kind, counts[kind], sem)
		}
	}
}

// TestInspect_MessageTopic_IncludesCrossRepoSemanticEdges is fix B's topic
// case: inspecting the topic from the PRODUCER repo must include the
// consumer repo's SUBSCRIBES_TO/DELIVERS_TO, not just the local PUBLISHES_TO.
func TestInspect_MessageTopic_IncludesCrossRepoSemanticEdges(t *testing.T) {
	srv := newMessagingTestServer(t)
	out := callFlowTool(t, srv.handleGetNode, map[string]any{
		"entity_id": "producer::topic:orders",
	})
	sem, _ := out["semantic_edges"].([]any)
	if len(sem) == 0 {
		t.Fatalf("expected semantic_edges on the topic, got none: %+v", out)
	}
	var haveSub, haveDeliver, havePub bool
	for _, row := range sem {
		m, _ := row.(map[string]any)
		kind, _ := m["kind"].(string)
		switch kind {
		case "SUBSCRIBES_TO":
			haveSub = true
		case "DELIVERS_TO":
			haveDeliver = true
		case "PUBLISHES_TO":
			havePub = true
		}
	}
	if !havePub {
		t.Errorf("expected local PUBLISHES_TO edge, semantic_edges=%v", sem)
	}
	if !haveSub {
		t.Errorf("expected cross-repo SUBSCRIBES_TO edge folded in, semantic_edges=%v", sem)
	}
	if !haveDeliver {
		t.Errorf("expected cross-repo DELIVERS_TO edge folded in, semantic_edges=%v", sem)
	}
}

// TestInspect_ChannelBinding_HasSemanticEdges is fix B's ChannelBinding case:
// inspecting a ChannelBinding must include its BINDS_CHANNEL/BINDS_TOPIC edges.
func TestInspect_ChannelBinding_HasSemanticEdges(t *testing.T) {
	srv := newMessagingTestServer(t)
	out := callFlowTool(t, srv.handleGetNode, map[string]any{
		"entity_id": "producer::cb:db417080c9d5bd11",
	})
	sem, _ := out["semantic_edges"].([]any)
	if len(sem) == 0 {
		t.Fatalf("expected semantic_edges on the ChannelBinding, got none: %+v", out)
	}
	// Each edge kind must appear EXACTLY once (dedup guard), pointing at the
	// specific RESOLVED bound entity — not the dangling synthetic FromID.
	counts := map[string]int{}
	otherByKind := map[string]string{}
	for _, row := range sem {
		m, _ := row.(map[string]any)
		k, _ := m["kind"].(string)
		if k == "BINDS_CHANNEL" || k == "BINDS_TOPIC" {
			counts[k]++
			otherByKind[k], _ = m["other"].(string)
			if dir, _ := m["direction"].(string); dir != "outbound" {
				t.Errorf("%s must be outbound from the binding, got direction=%q", k, dir)
			}
		}
	}
	if counts["BINDS_CHANNEL"] != 1 {
		t.Errorf("expected exactly one BINDS_CHANNEL edge, got %d: %v", counts["BINDS_CHANNEL"], sem)
	}
	if counts["BINDS_TOPIC"] != 1 {
		t.Errorf("expected exactly one BINDS_TOPIC edge, got %d: %v", counts["BINDS_TOPIC"], sem)
	}
	// The bound channel is the @Outgoing Operation; the bound topic is the
	// broker-prefixed MessageTopic — both by resolved entity id, not synthetic.
	// scopeIsOne is false here (the group has two repos), so `other` is
	// repo-prefixed — and critically it is the RESOLVED entity id, not the
	// dangling synthetic FromID the edge was stored with.
	if got := otherByKind["BINDS_CHANNEL"]; got != "producer::op:placeOrder" {
		t.Errorf("BINDS_CHANNEL should resolve to producer::op:placeOrder, got %q", got)
	}
	if got := otherByKind["BINDS_TOPIC"]; got != "producer::topic:orders" {
		t.Errorf("BINDS_TOPIC should resolve to producer::topic:orders, got %q", got)
	}
}

// TestBM25_FindsChannelBindingByNaturalLanguage is fix C: a natural-language
// query mentioning the binding's direction/connector/topic (not just the bare
// channel name) must rank the ChannelBinding entity via BM25 — previously
// only search=substring + kind_filter=SCOPE.ChannelBinding could find it.
func TestBM25_FindsChannelBindingByNaturalLanguage(t *testing.T) {
	srv := newMessagingTestServer(t)
	r := srv.State.groups["test"].Repos["producer"]
	idx := r.getBM25()
	if idx == nil {
		t.Fatal("nil BM25 index")
	}
	hits := idx.Search("smallrye kafka outgoing channel binding", 10)
	found := false
	for _, h := range hits {
		if h.Entity != nil && h.Entity.Kind == "SCOPE.ChannelBinding" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected the ChannelBinding entity to be found via bm25 natural-language search, hits=%+v", hits)
	}
}

// TestChannelBinding_ProfileVariantNoCrossBind is the #5782 over-match guard:
// two bindings that share the SAME (direction, channel) but live in DIFFERENT
// config files (a Spring/SmallRye profile-variant pattern:
// application.properties + application-prod.properties) must each resolve to
// ONLY their own topic. Before the fix, when SourceFile was trimmed by fbwrite
// the file disambiguator was skipped and binding A cross-bound BOTH topics.
// Exercised in three storage modes: SourceFile present, SourceFile empty
// (QN-only recovery), and — the RED case on the old code — the empty variant.
func TestChannelBinding_ProfileVariantNoCrossBind(t *testing.T) {
	const dirCh = "outgoing:completion-out"
	synthA := "scope:channelbinding:spring_properties:application.properties:" + dirCh
	synthB := "scope:channelbinding:spring_properties:application-prod.properties:" + dirCh

	// build constructs a one-repo LoadedRepo with two same-(direction,channel)
	// bindings in different files, each BINDS_TOPIC to its own topic via the
	// dangling synthetic FromID. withFiles=false blanks SourceFile to force the
	// QualifiedName recovery path (mirrors fbwrite trimming the field).
	build := func(withFiles bool) *LoadedRepo {
		fileA, fileB := "application.properties", "application-prod.properties"
		srcA, srcB := fileA, fileB
		if !withFiles {
			srcA, srcB = "", ""
		}
		doc := minDoc(
			[]graph.Entity{
				{ID: "topic:A", Name: "kafka:completion.a", Kind: "SCOPE.MessageTopic"},
				{ID: "topic:B", Name: "kafka:completion.b", Kind: "SCOPE.MessageTopic"},
				graph.Entity{
					ID: "cb:hashA", Name: "completion-out", Kind: "SCOPE.ChannelBinding", SourceFile: srcA, Subtype: "outgoing",
					QualifiedName: "svc::" + fileA + "#" + dirCh,
				}.WithProperties(map[string]string{"channel": "completion-out", "direction": "outgoing", "topic": "completion.a"}),
				graph.Entity{
					ID: "cb:hashB", Name: "completion-out", Kind: "SCOPE.ChannelBinding", SourceFile: srcB, Subtype: "outgoing",
					QualifiedName: "svc::" + fileB + "#" + dirCh,
				}.WithProperties(map[string]string{"channel": "completion-out", "direction": "outgoing", "topic": "completion.b"}),
			},
			[]graph.Relationship{
				graph.Relationship{ID: "btA", FromID: synthA, ToID: "topic:A", Kind: "BINDS_TOPIC"}.WithProperties(map[string]string{"topic": "completion.a"}),
				graph.Relationship{ID: "btB", FromID: synthB, ToID: "topic:B", Kind: "BINDS_TOPIC"}.WithProperties(map[string]string{"topic": "completion.b"}),
			},
		)
		doc.Repo = "svc"
		return &LoadedRepo{Repo: "svc", Doc: doc, LabelIndex: BuildLabelIndex(doc)}
	}

	check := func(t *testing.T, r *LoadedRepo, bindingID, wantTopic string) {
		t.Helper()
		_, topics := collectChannelBindingTargets(r, r.getByID()[bindingID])
		if len(topics) != 1 {
			t.Fatalf("%s: expected exactly ONE bound topic (no cross-bind), got %d: %+v", bindingID, len(topics), topics)
		}
		if topics[0].Name != wantTopic {
			t.Errorf("%s: expected bound topic %q, got %q", bindingID, wantTopic, topics[0].Name)
		}
	}

	t.Run("SourceFile present", func(t *testing.T) {
		r := build(true)
		check(t, r, "cb:hashA", "kafka:completion.a")
		check(t, r, "cb:hashB", "kafka:completion.b")
	})
	// The RED case on the pre-fix code: SourceFile trimmed → old guard skipped
	// the file check → binding A matched BOTH synthetic FromIDs → count==2.
	t.Run("SourceFile empty (QN-only recovery)", func(t *testing.T) {
		r := build(false)
		check(t, r, "cb:hashA", "kafka:completion.a")
		check(t, r, "cb:hashB", "kafka:completion.b")
	})
}

// TestInspect_ChannelBinding_LegacyIDMode_NoDoubleRow guards the reviewer's
// double-row concern: in the storage mode where the edge FromID EQUALS the
// binding's entity id, inspectSemanticEdges's adjacency walk emits a BINDS_* row
// AND foldTopicNeighborRows emits one — they must dedup to exactly one row (the
// fold's normalized `other` resolves to the same id the walk used). Checked in
// both scope modes.
func TestInspect_ChannelBinding_LegacyIDMode_NoDoubleRow(t *testing.T) {
	doc := minDoc(
		[]graph.Entity{
			{ID: "topic:X", Name: "kafka:x", Kind: "SCOPE.MessageTopic"},
			{ID: "op:X", Name: "Svc.onX", Kind: "SCOPE.Operation", SourceFile: "svc.go"},
			graph.Entity{
				ID: "cb:legacy", Name: "x-out", Kind: "SCOPE.ChannelBinding", SourceFile: "application.properties", Subtype: "outgoing",
				QualifiedName: "svc::application.properties#outgoing:x-out",
			}.WithProperties(map[string]string{"channel": "x-out", "direction": "outgoing"}),
		},
		[]graph.Relationship{
			// FromID == the binding's own entity id (legacy/naive storage mode).
			{ID: "bc", FromID: "cb:legacy", ToID: "op:X", Kind: "BINDS_CHANNEL"},
			{ID: "bt", FromID: "cb:legacy", ToID: "topic:X", Kind: "BINDS_TOPIC"},
		},
	)
	doc.Repo = "svc"
	r := &LoadedRepo{Repo: "svc", Doc: doc, LabelIndex: BuildLabelIndex(doc)}
	binding := r.getByID()["cb:legacy"]

	for _, scopeIsOne := range []bool{true, false} {
		rows := inspectSemanticEdgesFull(nil, r, binding, scopeIsOne)
		counts := map[string]int{}
		for _, m := range rows {
			if k, _ := m["kind"].(string); k == "BINDS_CHANNEL" || k == "BINDS_TOPIC" {
				counts[k]++
			}
		}
		if counts["BINDS_CHANNEL"] != 1 || counts["BINDS_TOPIC"] != 1 {
			t.Errorf("scopeIsOne=%v: expected each BINDS_* exactly once (no double row), got %v (rows=%v)", scopeIsOne, counts, rows)
		}
	}
}

// ---------------------------------------------------------------------------
// ITEM #2 — grafel_impact_radius on a topic
// ---------------------------------------------------------------------------

// TestImpactRadius_TopicCrossRepoConsumer is the #2 assertion: the blast radius
// of a topic must include a cross-repo consumer that subscribes in ANOTHER repo.
func TestImpactRadius_TopicCrossRepoConsumer(t *testing.T) {
	srv := newMessagingTestServer(t)
	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id": "producer::topic:orders",
		"hops":      float64(2),
	})
	if out == nil {
		t.Fatal("nil result")
	}
	names := collectNames(out["affected"])
	if repo := names["InventoryConsumer.onOrder"]; repo != "consumer" {
		t.Errorf("expected cross-repo consumer InventoryConsumer.onOrder (repo consumer) in blast radius, got affected=%+v", names)
	}
	// The local publisher must remain in the radius too.
	if _, ok := names["OrderService.placeOrder"]; !ok {
		t.Errorf("expected local publisher OrderService.placeOrder in blast radius, got affected=%+v", names)
	}
}

// TestImpactRadius_NonTopicUnchanged guards the code-entity path: a plain CALLS
// chain still resolves intra-repo without regression.
func TestImpactRadius_NonTopicUnchanged(t *testing.T) {
	srv := newTestServer(t, buildChainDoc())
	// FuncC is called by FuncB, which is called by FuncA — both are affected.
	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id": "ent-c",
		"hops":      float64(2),
	})
	names := collectNames(out["affected"])
	if _, ok := names["FuncB"]; !ok {
		t.Errorf("non-topic impact regressed: expected FuncB affected, got %+v", names)
	}
	if _, ok := names["FuncA"]; !ok {
		t.Errorf("non-topic impact regressed: expected FuncA affected, got %+v", names)
	}
}

// ---------------------------------------------------------------------------
// bug — grafel_debt kind=stubs gating
// ---------------------------------------------------------------------------

// TestDebtStubs_GatedErrorWithoutGroups asserts the clear two-group gate error.
func TestDebtStubs_GatedErrorWithoutGroups(t *testing.T) {
	srv := newTestServer(t, buildChainDoc())
	msg := callFlowToolError(t, srv.handleAnalysisDebt, map[string]any{
		"kind": "stubs",
	})
	for _, want := range []string{"group_v3", "group_oracle", "two-group"} {
		if !hasFold(msg, want) {
			t.Errorf("stub gate error must mention %q; got: %s", want, msg)
		}
	}
}

// TestDebtStubs_BothGroupsPassesGate asserts that supplying both group args
// clears the gate (the two-group path still runs; here it fails later with a
// distinct "not loaded" error, proving the gate no longer fires).
func TestDebtStubs_BothGroupsPassesGate(t *testing.T) {
	srv := newTestServer(t, buildChainDoc())
	msg := callFlowToolError(t, srv.handleAnalysisDebt, map[string]any{
		"kind":         "stubs",
		"group_v3":     "nope_v3",
		"group_oracle": "nope_oracle",
	})
	if hasFold(msg, "two-group") {
		t.Errorf("gate should not fire when both groups are provided; got: %s", msg)
	}
	if !hasFold(msg, "not loaded") {
		t.Errorf("expected the two-group path to proceed to a load error, got: %s", msg)
	}
}
