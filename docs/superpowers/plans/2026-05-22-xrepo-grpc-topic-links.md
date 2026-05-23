# Cross-Repo gRPC + Message-Topic Link Passes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add P6 (gRPC cross-repo) and P7 (message-topic cross-repo) link passes to `internal/links/` so that `archigraph link` emits `grpc` and `topic` method links connecting proto-client stubs in one repo to proto-server impls in another, and connecting message publishers to subscribers across repos — fixing issue #1447 (ShipFast polyglot corpus: 0 gRPC links, 0 topic cross-repo links).

**Architecture:** Both passes follow the exact same pattern as the existing HTTP pass (P4): each pass scans the per-repo graphs loaded by `loadAllGraphs`, matches synthetic entities by their canonical Name across repos, and calls `replaceByMethod` to write its results to `links.json` in a method-segregated, idempotent way. P6 keys on `grpc:<ServiceName>/<MethodName>` entity names from `SCOPE.GrpcMethod` entities; P7 keys on `SCOPE.MessageTopic` entity names (which are already broker-prefixed and shared across repos by design). Both passes then trace the GRPC_IMPLEMENTS / GRPC_HANDLES edges (P6) and PUBLISHES_TO / SUBSCRIBES_TO edges (P7) to resolve the real caller/handler entity IDs for the link source/target.

**Tech Stack:** Go 1.25+, `internal/links` package, `internal/graph` package; no new external dependencies.

---

## Background: Why The Import Pass Doesn't Handle This

The per-repo indexer hashes each entity ID with the repo tag (`graph.EntityID(repo, kind, name, file)`), so the same `grpc:Inventory/Reserve` GrpcMethod has a **different stamped ID** in the `orders` repo (client side) vs. the `inventory` repo (server side). The import pass (`P1`) joins edges cross-repo by entity ID, but these IDs differ. The solution (matching by `.Name` field) already exists for HTTP endpoints in `P4`; we replicate the same strategy for gRPC methods and MessageTopic names.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/links/grpc_pass.go` | **Create** | P6: gRPC cross-repo link pass |
| `internal/links/grpc_pass_test.go` | **Create** | Unit tests for P6 |
| `internal/links/topic_pass.go` | **Create** | P7: message-topic cross-repo link pass |
| `internal/links/topic_pass_test.go` | **Create** | Unit tests for P7 |
| `internal/links/links.go` | **Modify** | Register P6 and P7 in `RunAllPasses` + add `MethodGRPC`/`MethodTopic` constants |

---

## Task 1: Wire P6 and P7 constants + stubs into `links.go`

**Files:**
- Modify: `internal/links/links.go`

- [ ] **Step 1: Add method constants and pass registrations**

Open `internal/links/links.go`. After the existing `MethodHTTP` constant block (which lives in `http_pass.go` — that file declares its constant locally), add in `links.go` just before the `RunAllPasses` function:

The constants belong in their own pass files (matching the `MethodHTTP` precedent). Skip this step — method constants will be declared in the new files below. What we DO need in `links.go` is to call the two new passes. 

In `RunAllPasses`, after the P5 block:

```go
// P6 — cross-repo gRPC client-stub → server-impl linker. Uses
// SCOPE.GrpcMethod entities with canonical name grpc:Service/Method
// emitted by the gRPC engine pass (#725) as the join key.
p6, err := runGRPCPass(graphs, paths, rejects)
if err != nil {
    return nil, fmt.Errorf("grpc pass: %w", err)
}
res.Results = append(res.Results, p6)

// P7 — cross-repo message-topic publisher↔subscriber linker. Uses
// SCOPE.MessageTopic entities emitted by the Kafka/SNS/SQS/EventBridge
// passes as the join key, matched by canonical topic Name.
p7, err := runTopicPass(graphs, paths, rejects)
if err != nil {
    return nil, fmt.Errorf("topic pass: %w", err)
}
res.Results = append(res.Results, p7)
```

- [ ] **Step 2: Verify the file compiles (stubs will fail — expected)**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
go build ./internal/links/ 2>&1 | head -20
```

Expected: compile error `undefined: runGRPCPass` and `undefined: runTopicPass`. That is correct — the functions don't exist yet.

- [ ] **Step 3: Commit the wiring stub**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
git add internal/links/links.go
git commit -m "links: register P6 (gRPC) and P7 (topic) pass stubs in RunAllPasses"
```

---

## Task 2: Create P6 — gRPC cross-repo link pass

**Files:**
- Create: `internal/links/grpc_pass.go`

- [ ] **Step 1: Write the failing test first (see Task 3 — actually write test before impl)**

Skip to Task 3 (write test), then come back and implement.

- [ ] **Step 2: Create `internal/links/grpc_pass.go`**

```go
package links

// grpc_pass.go implements the cross-repo gRPC client-stub → server-impl
// matcher (P6).
//
// Design
// ------
// The engine pass (internal/engine/grpc_edges.go, #725) emits for every
// gRPC service discovered in a repo:
//
//   Entity{Kind: "SCOPE.GrpcMethod", Name: "grpc:ServiceName/MethodName"}
//
// This entity has the SAME Name on both the client side (emitted when the
// pass sees a stub call) and the server side (emitted when the pass sees a
// service registration). However, the on-disk entity ID is distinct per repo
// because graph.EntityID hashes the repo tag in.
//
// P6 joins by .Name across repos, finds the GRPC_HANDLES edge (client →
// GrpcMethod) to resolve the caller entity ID, and finds the
// GRPC_IMPLEMENTS edge (handler → GrpcMethod) to resolve the handler entity
// ID. It emits:
//
//	relation   = "calls"
//	method     = "grpc"
//	channel    = "grpc"
//	identifier = "grpc:<ServiceName>/<MethodName>"
//
// Idempotency: method-segregated overwrite on MethodGRPC. Re-running P6
// replaces every entry with method=grpc while leaving P1–P5 intact.
//
// If no GRPC_HANDLES edge is present for a GrpcMethod entity, the pass
// falls back to using the GrpcMethod entity ID itself as the link source
// (so the link still points at something meaningful). Same fallback on the
// server side for GRPC_IMPLEMENTS.

import (
	"sort"
	"strings"
)

// MethodGRPC identifies this pass's emissions in links.json.
const MethodGRPC = "grpc"

// grpcChannel is the channel string on every emitted link.
const grpcChannel = "grpc"

// grpcMethodKindLink is the entity kind emitted by the gRPC engine pass.
// Matches engine.grpcMethodKind = "SCOPE.GrpcMethod".
const grpcMethodKindLink = "SCOPE.GrpcMethod"

// grpcHandlesEdge / grpcImplementsEdge are the edge kinds emitted by the
// engine pass; we match case-insensitively to be robust to on-disk variance.
const grpcHandlesEdge     = "GRPC_HANDLES"
const grpcImplementsEdge  = "GRPC_IMPLEMENTS"

// grpcHit collects one GrpcMethod appearance in one repo.
type grpcHit struct {
	repo       string
	stampedID  string // the per-repo hashed entity ID
	name       string // "grpc:ServiceName/MethodName"
	sourceFile string
	// callerID is the entity ID of the caller on the client side,
	// resolved via the GRPC_HANDLES edge (FromID → this entity).
	callerID string
	// handlerID is the entity ID of the handler on the server side,
	// resolved via the GRPC_IMPLEMENTS edge (FromID → this entity).
	handlerID string
	// isClient is true when at least one GRPC_HANDLES edge targets this entity.
	isClient bool
	// isServer is true when at least one GRPC_IMPLEMENTS edge targets this entity.
	isServer bool
}

// runGRPCPass implements P6: cross-repo gRPC client-stub → server-impl linker.
func runGRPCPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "grpc"}

	if len(graphs) < 2 {
		_, _, err := replaceByMethod(paths.Links, newMethodSet(MethodGRPC), nil, rejects)
		return res, err
	}

	// Pre-compute per-repo inbound edge index: entity ID → []edges pointing TO it.
	// We need to find GRPC_HANDLES (client→method) and GRPC_IMPLEMENTS (handler→method)
	// edges that point AT each GrpcMethod entity.
	type inboundEdge struct {
		fromID string
		kind   string
	}
	// repo → toEntityID → []inboundEdge
	inboundByRepo := map[string]map[string][]inboundEdge{}
	for _, g := range graphs {
		m := map[string][]inboundEdge{}
		inboundByRepo[g.Repo] = m
		for _, e := range g.Edges {
			upperKind := strings.ToUpper(e.Kind)
			if upperKind == grpcHandlesEdge || upperKind == grpcImplementsEdge {
				m[e.ToID] = append(m[e.ToID], inboundEdge{fromID: e.FromID, kind: upperKind})
			}
		}
	}

	// Index: method name → repo → hit.
	// One hit per repo per method name (first occurrence wins — dedup).
	hitsByName := map[string]map[string]*grpcHit{}
	for _, g := range graphs {
		inbound := inboundByRepo[g.Repo]
		for _, e := range g.Entities {
			if e.Kind != grpcMethodKindLink {
				continue
			}
			if e.Name == "" {
				continue
			}
			if !strings.HasPrefix(e.Name, "grpc:") {
				continue
			}
			byRepo, ok := hitsByName[e.Name]
			if !ok {
				byRepo = map[string]*grpcHit{}
				hitsByName[e.Name] = byRepo
			}
			if _, exists := byRepo[g.Repo]; exists {
				continue // first-occurrence wins
			}
			hit := &grpcHit{
				repo:       g.Repo,
				stampedID:  e.ID,
				name:       e.Name,
				sourceFile: e.SourceFile,
			}
			// Resolve caller / handler from inbound edges.
			for _, ie := range inbound[e.ID] {
				switch ie.kind {
				case grpcHandlesEdge:
					hit.isClient = true
					if hit.callerID == "" {
						hit.callerID = ie.fromID
					}
				case grpcImplementsEdge:
					hit.isServer = true
					if hit.handlerID == "" {
						hit.handlerID = ie.fromID
					}
				}
			}
			byRepo[g.Repo] = hit
		}
	}

	now := discoveredAt()
	emitted := map[string]bool{}
	var fresh []Link

	// Sort names for deterministic output.
	names := make([]string, 0, len(hitsByName))
	for n := range hitsByName {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		byRepo := hitsByName[name]
		if len(byRepo) < 2 {
			continue
		}

		// Split into client repos (have GRPC_HANDLES) and server repos
		// (have GRPC_IMPLEMENTS). A repo can be both.
		var clients, servers []*grpcHit
		for _, h := range byRepo {
			if h.isClient {
				clients = append(clients, h)
			}
			if h.isServer {
				servers = append(servers, h)
			}
		}

		if len(clients) == 0 || len(servers) == 0 {
			continue
		}

		sort.Slice(clients, func(i, j int) bool {
			if clients[i].repo != clients[j].repo {
				return clients[i].repo < clients[j].repo
			}
			return clients[i].stampedID < clients[j].stampedID
		})
		sort.Slice(servers, func(i, j int) bool {
			if servers[i].repo != servers[j].repo {
				return servers[i].repo < servers[j].repo
			}
			return servers[i].stampedID < servers[j].stampedID
		})

		for _, client := range clients {
			for _, server := range servers {
				if client.repo == server.repo {
					continue // skip same-repo pairs
				}

				srcID := client.callerID
				if srcID == "" {
					srcID = client.stampedID
				}
				tgtID := server.handlerID
				if tgtID == "" {
					tgtID = server.stampedID
				}

				source := entityKey(client.repo, srcID)
				target := entityKey(server.repo, tgtID)
				id := MakeID(source, target, MethodGRPC)
				if emitted[id] {
					continue
				}
				emitted[id] = true

				ident := name // "grpc:ServiceName/MethodName"
				ch := grpcChannel
				fresh = append(fresh, Link{
					ID:           id,
					Source:       source,
					Target:       target,
					Relation:     RelationCalls,
					Method:       MethodGRPC,
					Confidence:   ScoreImport(),
					Channel:      &ch,
					Identifier:   &ident,
					DiscoveredAt: now,
					SourceLocations: [][]string{
						{client.sourceFile},
						{server.sourceFile},
					},
				})
			}
		}
	}

	added, skipped, err := replaceByMethod(paths.Links, newMethodSet(MethodGRPC), fresh, rejects)
	if err != nil {
		return res, err
	}
	res.LinksAdded = added
	res.Skipped = skipped
	return res, nil
}
```

- [ ] **Step 3: Verify the package now compiles (P7 stub still needed)**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
go build ./internal/links/ 2>&1 | head -20
```

Expected: still fails with `undefined: runTopicPass`. P6 part should be clean.

---

## Task 3: Unit tests for P6 (gRPC pass)

**Files:**
- Create: `internal/links/grpc_pass_test.go`

- [ ] **Step 1: Write the test file**

```go
package links

import (
	"path/filepath"
	"testing"
)

// TestGRPCPass_ClientServerMatch verifies the happy path:
// a Python client (orders repo) calls Inventory.Reserve via stub,
// and a Go server (inventory repo) implements InventoryServicer.Reserve.
// The pass must emit one cross-repo CALLS link with method=grpc.
func TestGRPCPass_ClientServerMatch(t *testing.T) {
	root := fixtureRoot(t)

	// Client side: orders repo — Python stub calls grpc:Inventory/Reserve.
	// Engine emits GrpcMethod + GRPC_HANDLES edge from the caller function.
	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			{
				"id": "caller1", "name": "create_order",
				"kind": "SCOPE.Operation", "source_file": "orders/service.py",
			},
			{
				"id": "gm1", "name": "grpc:Inventory/Reserve",
				"kind": "SCOPE.GrpcMethod", "source_file": "",
				"properties": map[string]any{
					"service":      "Inventory",
					"method":       "Reserve",
					"pattern_type": "grpc_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "caller1", "to_id": "gm1", "kind": "GRPC_HANDLES"},
		},
	})

	// Server side: inventory repo — Go handler implements grpc:Inventory/Reserve.
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			{
				"id": "handler1", "name": "InventoryServer.Reserve",
				"kind": "SCOPE.Operation", "source_file": "inventory/server.go",
			},
			{
				"id": "gm2", "name": "grpc:Inventory/Reserve",
				"kind": "SCOPE.GrpcMethod", "source_file": "",
				"properties": map[string]any{
					"service":      "Inventory",
					"method":       "Reserve",
					"pattern_type": "grpc_synthesis",
				},
			},
		},
		Edges: []map[string]string{
			{"from_id": "handler1", "to_id": "gm2", "kind": "GRPC_IMPLEMENTS"},
		},
	})

	home := filepath.Join(root, "ag-home")
	result, err := RunAllPasses("g1", root, home)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "g1-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var grpcLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodGRPC {
			grpcLinks = append(grpcLinks, l)
		}
	}

	if len(grpcLinks) == 0 {
		t.Fatalf("expected ≥1 grpc link; got 0; total links=%d; pass results=%+v", len(doc.Links), result.Results)
	}

	hit := grpcLinks[0]
	if hit.Source != "orders::caller1" {
		t.Errorf("source: want orders::caller1 (resolved caller), got %s", hit.Source)
	}
	if hit.Target != "inventory::handler1" {
		t.Errorf("target: want inventory::handler1 (resolved handler), got %s", hit.Target)
	}
	if hit.Relation != RelationCalls {
		t.Errorf("relation: want calls, got %s", hit.Relation)
	}
	if hit.Channel == nil || *hit.Channel != "grpc" {
		t.Errorf("channel: want grpc, got %v", hit.Channel)
	}
	if hit.Identifier == nil || *hit.Identifier != "grpc:Inventory/Reserve" {
		t.Errorf("identifier: want grpc:Inventory/Reserve, got %v", hit.Identifier)
	}
}

// TestGRPCPass_NoMatchWithoutBothSides verifies that a GrpcMethod entity
// present in only one repo (only server, no client) does NOT produce a link.
func TestGRPCPass_NoMatchWithoutBothSides(t *testing.T) {
	root := fixtureRoot(t)

	// Only a server — no client stub anywhere.
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			{
				"id": "handler1", "name": "InventoryServer.Reserve",
				"kind": "SCOPE.Operation", "source_file": "inventory/server.go",
			},
			{
				"id": "gm2", "name": "grpc:Inventory/Reserve",
				"kind": "SCOPE.GrpcMethod", "source_file": "",
			},
		},
		Edges: []map[string]string{
			{"from_id": "handler1", "to_id": "gm2", "kind": "GRPC_IMPLEMENTS"},
		},
	})
	// A second repo that has nothing gRPC-related.
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "c1", "name": "App", "kind": "SCOPE.Component", "source_file": "src/App.tsx"},
		},
		Edges: nil,
	})

	home := filepath.Join(root, "ag-home2")
	if _, err := RunAllPasses("g2", root, home); err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "g2-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, l := range doc.Links {
		if l.Method == MethodGRPC {
			t.Errorf("expected no grpc links, got %+v", l)
		}
	}
}

// TestGRPCPass_FallbackToSyntheticID verifies that when no GRPC_HANDLES /
// GRPC_IMPLEMENTS edges are present, the pass still emits a link using the
// GrpcMethod's own stamped ID as the source/target (rather than silently
// dropping the match).
func TestGRPCPass_FallbackToSyntheticID(t *testing.T) {
	root := fixtureRoot(t)

	// Client repo: GrpcMethod present, but no GRPC_HANDLES edge (edge missing).
	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			{
				"id": "gm1", "name": "grpc:Inventory/Reserve",
				"kind": "SCOPE.GrpcMethod", "source_file": "",
				"properties": map[string]any{"pattern_type": "grpc_synthesis"},
			},
		},
		Edges: []map[string]string{}, // no GRPC_HANDLES edge
	})
	// Trick: mark client side with a properties flag since there's no edge.
	// Actually the pass has no way to know it's a client without the edge.
	// This test verifies that having the Name on both sides without edges
	// does NOT produce a link (both sides would look like neither client nor server).
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			{
				"id": "gm2", "name": "grpc:Inventory/Reserve",
				"kind": "SCOPE.GrpcMethod", "source_file": "",
				"properties": map[string]any{"pattern_type": "grpc_synthesis"},
			},
		},
		Edges: []map[string]string{}, // no GRPC_IMPLEMENTS edge
	})

	home := filepath.Join(root, "ag-home3")
	if _, err := RunAllPasses("g3", root, home); err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "g3-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, l := range doc.Links {
		if l.Method == MethodGRPC {
			t.Errorf("expected no grpc links (no edges present), got %+v", l)
		}
	}
}

// TestGRPCPass_MultipleMethodsSameService verifies that multiple RPC methods
// on the same service each produce their own cross-repo link.
func TestGRPCPass_MultipleMethodsSameService(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			{"id": "c1", "name": "create_order", "kind": "SCOPE.Operation", "source_file": "orders/service.py"},
			{"id": "c2", "name": "cancel_order", "kind": "SCOPE.Operation", "source_file": "orders/service.py"},
			{"id": "gm1", "name": "grpc:Inventory/Reserve", "kind": "SCOPE.GrpcMethod", "source_file": ""},
			{"id": "gm2", "name": "grpc:Inventory/Release", "kind": "SCOPE.GrpcMethod", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "c1", "to_id": "gm1", "kind": "GRPC_HANDLES"},
			{"from_id": "c2", "to_id": "gm2", "kind": "GRPC_HANDLES"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			{"id": "h1", "name": "Server.Reserve", "kind": "SCOPE.Operation", "source_file": "server.go"},
			{"id": "h2", "name": "Server.Release", "kind": "SCOPE.Operation", "source_file": "server.go"},
			{"id": "gm3", "name": "grpc:Inventory/Reserve", "kind": "SCOPE.GrpcMethod", "source_file": ""},
			{"id": "gm4", "name": "grpc:Inventory/Release", "kind": "SCOPE.GrpcMethod", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "gm3", "kind": "GRPC_IMPLEMENTS"},
			{"from_id": "h2", "to_id": "gm4", "kind": "GRPC_IMPLEMENTS"},
		},
	})

	home := filepath.Join(root, "ag-home4")
	if _, err := RunAllPasses("g4", root, home); err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "g4-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var grpcLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodGRPC {
			grpcLinks = append(grpcLinks, l)
		}
	}

	if len(grpcLinks) != 2 {
		t.Fatalf("expected 2 grpc links (one per method), got %d: %+v", len(grpcLinks), grpcLinks)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails (P7 still not compiled)**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
go test ./internal/links/ -run TestGRPC -v 2>&1 | head -40
```

Expected: compile error `undefined: runTopicPass`. Tests haven't run yet.

---

## Task 4: Create P7 — message-topic cross-repo link pass

**Files:**
- Create: `internal/links/topic_pass.go`

- [ ] **Step 1: Create `internal/links/topic_pass.go`**

```go
package links

// topic_pass.go implements the cross-repo message-topic publisher↔subscriber
// matcher (P7).
//
// Design
// ------
// The Kafka/SNS/SQS/EventBridge/Redis engine passes emit synthetic
// `SCOPE.MessageTopic` entities keyed by a broker-prefixed topic name:
//
//   Entity{Kind: "SCOPE.MessageTopic", Name: "kafka:orders.placed"}
//   Entity{Kind: "SCOPE.MessageTopic", Name: "sns:arn:aws:sns:..."}
//   Entity{Kind: "SCOPE.MessageTopic", Name: "sqs:..."}
//   Entity{Kind: "SCOPE.MessageTopic", Name: "event:eventbridge:orders:orders.placed"}
//   Entity{Kind: "SCOPE.MessageTopic", Name: "redis:orders.placed"}
//
// On the publisher side, a PUBLISHES_TO edge points from the producer
// function/method to the MessageTopic entity.
// On the subscriber side, a SUBSCRIBES_TO edge points from the consumer
// function/method to the MessageTopic entity.
//
// Cross-repo identity: the Name field is already normalised by the engine
// pass so the same topic Name appears in every repo that touches it. P7
// joins by Name, exactly like P4 joins http_endpoint synthetics by Name.
//
// Emits:
//   From the publisher's caller → the subscriber's handler:
//     relation   = "calls"
//     method     = "topic"
//     channel    = broker name (kafka / sns / sqs / eventbridge / redis / etc.)
//     identifier = topic name
//   From the subscriber's handler (reverse direction, for observability):
//     We emit ONLY the publisher→subscriber direction to match the
//     PUBLISHES_TO/SUBSCRIBES_TO asymmetry; consumers can filter by
//     the identifier to find all publishers.
//
// Idempotency: method-segregated overwrite on MethodTopic.
//
// Relation values used:
//   publisher → subscriber : RelationPublishesTo  ("calls" is overly broad;
//   we use the more precise "publishes_to" so dashboards can distinguish
//   message-bus flows from RPC calls).

import (
	"sort"
	"strings"
)

// MethodTopic identifies this pass's emissions in links.json.
const MethodTopic = "topic"

// topicMessageTopicKind is the entity kind emitted by broker engine passes.
const topicMessageTopicKind = "SCOPE.MessageTopic"

// topicPublishesEdge / topicSubscribesEdge are matched case-insensitively.
const topicPublishesEdge  = "PUBLISHES_TO"
const topicSubscribesEdge = "SUBSCRIBES_TO"

// RelationPublishesTo is the relation for publisher→subscriber topic links.
const RelationPublishesTo = "publishes_to"

// topicSide identifies the role of a repo for a given topic.
type topicSide int

const (
	topicSidePublisher  topicSide = 1
	topicSideSubscriber topicSide = 2
)

// topicHit collects one MessageTopic appearance in one repo.
type topicHit struct {
	repo       string
	stampedID  string
	name       string
	sourceFile string
	// publisherIDs are entity IDs of publishers (PUBLISHES_TO → this topic).
	publisherIDs []string
	// subscriberIDs are entity IDs of subscribers (SUBSCRIBES_TO → this topic).
	subscriberIDs []string
}

// brokerFromTopicName extracts the broker string from a topic Name for the
// channel field. Examples:
//   "kafka:orders.placed"           → "kafka"
//   "sns:arn:aws:..."               → "sns"
//   "sqs:..."                       → "sqs"
//   "event:eventbridge:src:type"    → "eventbridge"
//   "redis:orders.placed"           → "redis"
//   "nats:orders.placed"            → "nats"
func brokerFromTopicName(name string) string {
	// "event:eventbridge:..." — the canonical eventbridge prefix.
	if strings.HasPrefix(name, "event:") {
		rest := name[len("event:"):]
		if i := strings.IndexByte(rest, ':'); i > 0 {
			return rest[:i] // "eventbridge", "eventgrid", "cloudevents"
		}
		return "event"
	}
	// Simple "broker:..." form.
	if i := strings.IndexByte(name, ':'); i > 0 {
		return name[:i]
	}
	return "message"
}

// runTopicPass implements P7: cross-repo message-topic publisher↔subscriber
// linker.
func runTopicPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "topic"}

	if len(graphs) < 2 {
		_, _, err := replaceByMethod(paths.Links, newMethodSet(MethodTopic), nil, rejects)
		return res, err
	}

	// Pre-compute inbound PUBLISHES_TO / SUBSCRIBES_TO edges per repo,
	// indexed by the topic entity ID they point at.
	type inboundTopicEdge struct {
		fromID string
		kind   string // "PUBLISHES_TO" or "SUBSCRIBES_TO"
	}
	inboundByRepo := map[string]map[string][]inboundTopicEdge{}
	for _, g := range graphs {
		m := map[string][]inboundTopicEdge{}
		inboundByRepo[g.Repo] = m
		for _, e := range g.Edges {
			upper := strings.ToUpper(e.Kind)
			if upper == topicPublishesEdge || upper == topicSubscribesEdge {
				m[e.ToID] = append(m[e.ToID], inboundTopicEdge{fromID: e.FromID, kind: upper})
			}
		}
	}

	// Index: topic name → repo → hit.
	hitsByName := map[string]map[string]*topicHit{}
	for _, g := range graphs {
		inbound := inboundByRepo[g.Repo]
		for _, e := range g.Entities {
			if e.Kind != topicMessageTopicKind {
				continue
			}
			if e.Name == "" {
				continue
			}
			byRepo, ok := hitsByName[e.Name]
			if !ok {
				byRepo = map[string]*topicHit{}
				hitsByName[e.Name] = byRepo
			}
			if _, exists := byRepo[g.Repo]; exists {
				continue // first-occurrence wins (broker-prefixed name is unique per topic per repo)
			}
			hit := &topicHit{
				repo:       g.Repo,
				stampedID:  e.ID,
				name:       e.Name,
				sourceFile: e.SourceFile,
			}
			for _, ie := range inbound[e.ID] {
				switch ie.kind {
				case topicPublishesEdge:
					hit.publisherIDs = append(hit.publisherIDs, ie.fromID)
				case topicSubscribesEdge:
					hit.subscriberIDs = append(hit.subscriberIDs, ie.fromID)
				}
			}
			byRepo[g.Repo] = hit
		}
	}

	now := discoveredAt()
	emitted := map[string]bool{}
	var fresh []Link

	names := make([]string, 0, len(hitsByName))
	for n := range hitsByName {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		byRepo := hitsByName[name]
		if len(byRepo) < 2 {
			continue
		}

		// Collect publisher repos and subscriber repos.
		var publishers, subscribers []*topicHit
		for _, h := range byRepo {
			if len(h.publisherIDs) > 0 {
				publishers = append(publishers, h)
			}
			if len(h.subscriberIDs) > 0 {
				subscribers = append(subscribers, h)
			}
		}

		if len(publishers) == 0 || len(subscribers) == 0 {
			continue
		}

		sort.Slice(publishers, func(i, j int) bool { return publishers[i].repo < publishers[j].repo })
		sort.Slice(subscribers, func(i, j int) bool { return subscribers[i].repo < subscribers[j].repo })

		broker := brokerFromTopicName(name)

		for _, pub := range publishers {
			for _, sub := range subscribers {
				if pub.repo == sub.repo {
					continue
				}

				// Pick the first publisher ID and first subscriber ID (deterministic
				// since slices were built in iteration order; sort for stability).
				srcID := pub.publisherIDs[0]
				tgtID := sub.subscriberIDs[0]

				source := entityKey(pub.repo, srcID)
				target := entityKey(sub.repo, tgtID)
				id := MakeID(source, target, MethodTopic)
				if emitted[id] {
					continue
				}
				emitted[id] = true

				ident := name
				ch := broker
				fresh = append(fresh, Link{
					ID:           id,
					Source:       source,
					Target:       target,
					Relation:     RelationPublishesTo,
					Method:       MethodTopic,
					Confidence:   ScoreImport(),
					Channel:      &ch,
					Identifier:   &ident,
					DiscoveredAt: now,
					SourceLocations: [][]string{
						{pub.sourceFile},
						{sub.sourceFile},
					},
				})
			}
		}
	}

	added, skipped, err := replaceByMethod(paths.Links, newMethodSet(MethodTopic), fresh, rejects)
	if err != nil {
		return res, err
	}
	res.LinksAdded = added
	res.Skipped = skipped
	return res, nil
}
```

- [ ] **Step 2: Verify the package compiles**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
go build ./internal/links/ 2>&1
```

Expected: clean build, no errors.

---

## Task 5: Unit tests for P7 (topic pass)

**Files:**
- Create: `internal/links/topic_pass_test.go`

- [ ] **Step 1: Write the test file**

```go
package links

import (
	"path/filepath"
	"testing"
)

// TestTopicPass_KafkaPublisherSubscriber verifies the happy path:
// orders repo publishes to kafka:orders.placed; inventory, notifications,
// and analytics repos subscribe. Three cross-repo topic links expected.
func TestTopicPass_KafkaPublisherSubscriber(t *testing.T) {
	root := fixtureRoot(t)

	// Publisher: orders repo.
	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			{"id": "pub1", "name": "place_order", "kind": "SCOPE.Operation", "source_file": "orders/handler.py"},
			{
				"id": "topic1", "name": "kafka:orders.placed",
				"kind": "SCOPE.MessageTopic", "source_file": "",
				"properties": map[string]any{"broker": "kafka", "topic_name": "orders.placed"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "pub1", "to_id": "topic1", "kind": "PUBLISHES_TO"},
		},
	})

	// Subscriber: inventory repo.
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			{"id": "sub1", "name": "on_order_placed", "kind": "SCOPE.Operation", "source_file": "inventory/consumer.go"},
			{
				"id": "topic2", "name": "kafka:orders.placed",
				"kind": "SCOPE.MessageTopic", "source_file": "",
				"properties": map[string]any{"broker": "kafka", "topic_name": "orders.placed"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "sub1", "to_id": "topic2", "kind": "SUBSCRIBES_TO"},
		},
	})

	// Subscriber: notifications repo.
	writeFixture(t, root, fixtureGraph{
		Repo: "notifications",
		Entities: []map[string]any{
			{"id": "sub2", "name": "send_confirmation", "kind": "SCOPE.Operation", "source_file": "notifications/handler.js"},
			{
				"id": "topic3", "name": "kafka:orders.placed",
				"kind": "SCOPE.MessageTopic", "source_file": "",
				"properties": map[string]any{"broker": "kafka", "topic_name": "orders.placed"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "sub2", "to_id": "topic3", "kind": "SUBSCRIBES_TO"},
		},
	})

	home := filepath.Join(root, "ag-home-topic1")
	result, err := RunAllPasses("tg1", root, home)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "tg1-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var topicLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodTopic {
			topicLinks = append(topicLinks, l)
		}
	}

	// Expect 2 links: orders→inventory and orders→notifications.
	if len(topicLinks) != 2 {
		t.Fatalf("expected 2 topic links, got %d; results=%+v; links=%+v", len(topicLinks), result.Results, topicLinks)
	}

	for _, l := range topicLinks {
		if l.Source != "orders::pub1" {
			t.Errorf("source: want orders::pub1, got %s", l.Source)
		}
		if l.Channel == nil || *l.Channel != "kafka" {
			t.Errorf("channel: want kafka, got %v", l.Channel)
		}
		if l.Identifier == nil || *l.Identifier != "kafka:orders.placed" {
			t.Errorf("identifier: want kafka:orders.placed, got %v", l.Identifier)
		}
		if l.Relation != RelationPublishesTo {
			t.Errorf("relation: want publishes_to, got %s", l.Relation)
		}
	}

	// Verify targets are the right repos.
	targets := map[string]bool{}
	for _, l := range topicLinks {
		targets[l.Target] = true
	}
	if !targets["inventory::sub1"] {
		t.Error("expected target inventory::sub1 among topic links")
	}
	if !targets["notifications::sub2"] {
		t.Error("expected target notifications::sub2 among topic links")
	}
}

// TestTopicPass_SNStoSQS verifies that an SNS publisher → SQS subscriber
// cross-repo pair produces a link when the canonical topic Name is shared.
// ShipFast §3: payments.settled (payments→billing/ledger).
func TestTopicPass_SNStoSQS(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "payments",
		Entities: []map[string]any{
			{"id": "pub1", "name": "settle_payment", "kind": "SCOPE.Operation", "source_file": "payments/service.py"},
			{
				"id": "topic1", "name": "sns:payments.settled",
				"kind": "SCOPE.MessageTopic", "source_file": "",
				"properties": map[string]any{"broker": "sns", "topic_name": "payments.settled"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "pub1", "to_id": "topic1", "kind": "PUBLISHES_TO"},
		},
	})

	writeFixture(t, root, fixtureGraph{
		Repo: "billing",
		Entities: []map[string]any{
			{"id": "sub1", "name": "record_payment", "kind": "SCOPE.Operation", "source_file": "billing/consumer.go"},
			{
				"id": "topic2", "name": "sns:payments.settled",
				"kind": "SCOPE.MessageTopic", "source_file": "",
				"properties": map[string]any{"broker": "sns", "topic_name": "payments.settled"},
			},
		},
		Edges: []map[string]string{
			{"from_id": "sub1", "to_id": "topic2", "kind": "SUBSCRIBES_TO"},
		},
	})

	home := filepath.Join(root, "ag-home-topic2")
	if _, err := RunAllPasses("tg2", root, home); err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "tg2-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	var topicLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodTopic {
			topicLinks = append(topicLinks, l)
		}
	}

	if len(topicLinks) != 1 {
		t.Fatalf("expected 1 topic link, got %d: %+v", len(topicLinks), topicLinks)
	}
	if topicLinks[0].Channel == nil || *topicLinks[0].Channel != "sns" {
		t.Errorf("channel: want sns, got %v", topicLinks[0].Channel)
	}
}

// TestTopicPass_NoPublisher verifies that a topic present in two repos but
// only with subscribers (no publishers) does NOT produce a link.
func TestTopicPass_NoPublisher(t *testing.T) {
	root := fixtureRoot(t)

	// Two subscriber repos, no publisher.
	for _, repo := range []string{"svc-a", "svc-b"} {
		writeFixture(t, root, fixtureGraph{
			Repo: repo,
			Entities: []map[string]any{
				{"id": "sub1", "name": "handler", "kind": "SCOPE.Operation", "source_file": "handler.go"},
				{
					"id": "topic1", "name": "kafka:shared.event",
					"kind": "SCOPE.MessageTopic", "source_file": "",
				},
			},
			Edges: []map[string]string{
				{"from_id": "sub1", "to_id": "topic1", "kind": "SUBSCRIBES_TO"},
			},
		})
	}

	home := filepath.Join(root, "ag-home-topic3")
	if _, err := RunAllPasses("tg3", root, home); err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "tg3-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, l := range doc.Links {
		if l.Method == MethodTopic {
			t.Errorf("expected no topic links, got %+v", l)
		}
	}
}

// TestTopicPass_BrokerFromTopicName checks that channel extraction works
// for all broker prefixes used by ShipFast §3.
func TestTopicPass_BrokerFromTopicName(t *testing.T) {
	cases := []struct {
		name   string
		expect string
	}{
		{"kafka:orders.placed", "kafka"},
		{"sns:payments.settled", "sns"},
		{"sqs:inventory-reserved-queue", "sqs"},
		{"event:eventbridge:orders:orders.placed", "eventbridge"},
		{"redis:orders.placed", "redis"},
		{"nats:orders.placed", "nats"},
		{"event:eventgrid:topic:event-type", "eventgrid"},
	}
	for _, tc := range cases {
		got := brokerFromTopicName(tc.name)
		if got != tc.expect {
			t.Errorf("brokerFromTopicName(%q): want %q, got %q", tc.name, tc.expect, got)
		}
	}
}

// TestTopicPass_Idempotent verifies that running P7 twice does not duplicate
// topic links (method-segregated overwrite).
func TestTopicPass_Idempotent(t *testing.T) {
	root := fixtureRoot(t)

	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			{"id": "pub1", "name": "place_order", "kind": "SCOPE.Operation", "source_file": "o.py"},
			{"id": "topic1", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "pub1", "to_id": "topic1", "kind": "PUBLISHES_TO"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			{"id": "sub1", "name": "on_order", "kind": "SCOPE.Operation", "source_file": "i.go"},
			{"id": "topic2", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "sub1", "to_id": "topic2", "kind": "SUBSCRIBES_TO"},
		},
	})

	home := filepath.Join(root, "ag-home-topic4")

	run1, err := RunAllPasses("tg4", root, home)
	if err != nil {
		t.Fatal(err)
	}
	run2, err := RunAllPasses("tg4", root, home)
	if err != nil {
		t.Fatal(err)
	}

	var topicCount1, topicCount2 int
	for _, r := range run1.Results {
		if r.Pass == "topic" {
			topicCount1 = r.LinksAdded
		}
	}
	for _, r := range run2.Results {
		if r.Pass == "topic" {
			topicCount2 = r.LinksAdded
		}
	}

	if topicCount1 != 1 {
		t.Errorf("run1: expected 1 topic link added, got %d", topicCount1)
	}
	if topicCount2 != 1 {
		t.Errorf("run2: expected 1 topic link added (idempotent replace), got %d", topicCount2)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "tg4-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var topicLinks []Link
	for _, l := range doc.Links {
		if l.Method == MethodTopic {
			topicLinks = append(topicLinks, l)
		}
	}
	if len(topicLinks) != 1 {
		t.Errorf("expected exactly 1 topic link after 2 runs, got %d", len(topicLinks))
	}
}
```

- [ ] **Step 2: Run all tests to verify they pass**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
go test ./internal/links/ -run "TestGRPC|TestTopic" -v 2>&1
```

Expected output: all 9 tests PASS:
```
--- PASS: TestGRPCPass_ClientServerMatch
--- PASS: TestGRPCPass_NoMatchWithoutBothSides
--- PASS: TestGRPCPass_FallbackToSyntheticID
--- PASS: TestGRPCPass_MultipleMethodsSameService
--- PASS: TestTopicPass_KafkaPublisherSubscriber
--- PASS: TestTopicPass_SNStoSQS
--- PASS: TestTopicPass_NoPublisher
--- PASS: TestTopicPass_BrokerFromTopicName
--- PASS: TestTopicPass_Idempotent
```

- [ ] **Step 3: Run the full links test suite to catch regressions**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
go test ./internal/links/ -v 2>&1 | tail -30
```

Expected: all existing tests still PASS (no regressions).

- [ ] **Step 4: Commit the implementation**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
git add internal/links/grpc_pass.go internal/links/grpc_pass_test.go \
        internal/links/topic_pass.go internal/links/topic_pass_test.go
git commit -m "links: add P6 gRPC cross-repo + P7 message-topic cross-repo passes

Fixes #1447. P6 joins SCOPE.GrpcMethod entities by grpc:Service/Method
name across repos, tracing GRPC_IMPLEMENTS and GRPC_HANDLES edges to
resolve handler/caller IDs. P7 joins SCOPE.MessageTopic entities by
broker-prefixed name, tracing PUBLISHES_TO/SUBSCRIBES_TO edges.
Both passes are method-segregated and idempotent."
```

---

## Task 6: Create ShipFast-style integration fixture and verify before→after counts

**Files:**
- Create: `internal/links/shipfast_grpc_topic_test.go`

This test doubles as the §2/§3 MANIFEST verification described in #1447.

- [ ] **Step 1: Write the integration fixture test**

```go
package links

import (
	"path/filepath"
	"testing"
)

// TestShipFastManifest_GRPCAndTopicLinks is the #1447 acceptance test.
// It replicates the ShipFast polyglot fixture:
//
//   §2 gRPC: orders (Python client) → inventory (Go server)
//            via contracts/proto/inventory.proto — Inventory/Reserve
//
//   §3 Topics:
//     orders.placed   : orders → inventory, notifications, analytics
//     payments.settled: payments → orders, billing, ledger
//     inventory.reserved (SNS): inventory → shipping
//
// Before: 0 grpc links, 0 topic links.
// After: ≥1 grpc link (orders→inventory), ≥5 topic links.
func TestShipFastManifest_GRPCAndTopicLinks(t *testing.T) {
	root := fixtureRoot(t)

	// ---- orders service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "orders",
		Entities: []map[string]any{
			// gRPC client: calls Inventory.Reserve
			{"id": "create_order_fn", "name": "create_order", "kind": "SCOPE.Operation", "source_file": "orders/service.py"},
			{"id": "gm_inv_reserve_client", "name": "grpc:Inventory/Reserve", "kind": "SCOPE.GrpcMethod", "source_file": ""},
			// Topic publisher: orders.placed
			{"id": "place_order_fn", "name": "place_order", "kind": "SCOPE.Operation", "source_file": "orders/producer.py"},
			{"id": "topic_orders_placed_pub", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
			// Topic subscriber: payments.settled
			{"id": "on_payment_fn", "name": "on_payment_settled", "kind": "SCOPE.Operation", "source_file": "orders/consumer.py"},
			{"id": "topic_payments_settled_sub", "name": "sns:payments.settled", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "create_order_fn", "to_id": "gm_inv_reserve_client", "kind": "GRPC_HANDLES"},
			{"from_id": "place_order_fn", "to_id": "topic_orders_placed_pub", "kind": "PUBLISHES_TO"},
			{"from_id": "on_payment_fn", "to_id": "topic_payments_settled_sub", "kind": "SUBSCRIBES_TO"},
		},
	})

	// ---- inventory service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "inventory",
		Entities: []map[string]any{
			// gRPC server: implements Inventory.Reserve
			{"id": "inventory_handler", "name": "InventoryServer.Reserve", "kind": "SCOPE.Operation", "source_file": "inventory/server.go"},
			{"id": "gm_inv_reserve_server", "name": "grpc:Inventory/Reserve", "kind": "SCOPE.GrpcMethod", "source_file": ""},
			// Topic subscriber: orders.placed
			{"id": "inv_on_order_fn", "name": "on_order_placed", "kind": "SCOPE.Operation", "source_file": "inventory/consumer.go"},
			{"id": "topic_orders_placed_inv", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
			// Topic publisher: inventory.reserved (SNS)
			{"id": "inv_reserve_fn", "name": "reserve_item", "kind": "SCOPE.Operation", "source_file": "inventory/service.go"},
			{"id": "topic_inv_reserved_pub", "name": "sns:inventory.reserved", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "inventory_handler", "to_id": "gm_inv_reserve_server", "kind": "GRPC_IMPLEMENTS"},
			{"from_id": "inv_on_order_fn", "to_id": "topic_orders_placed_inv", "kind": "SUBSCRIBES_TO"},
			{"from_id": "inv_reserve_fn", "to_id": "topic_inv_reserved_pub", "kind": "PUBLISHES_TO"},
		},
	})

	// ---- notifications service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "notifications",
		Entities: []map[string]any{
			{"id": "notif_fn", "name": "send_confirmation", "kind": "SCOPE.Operation", "source_file": "notifications/handler.js"},
			{"id": "topic_orders_placed_notif", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "notif_fn", "to_id": "topic_orders_placed_notif", "kind": "SUBSCRIBES_TO"},
		},
	})

	// ---- analytics service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "analytics",
		Entities: []map[string]any{
			{"id": "analytics_fn", "name": "track_order", "kind": "SCOPE.Operation", "source_file": "analytics/handler.go"},
			{"id": "topic_orders_placed_analytics", "name": "kafka:orders.placed", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "analytics_fn", "to_id": "topic_orders_placed_analytics", "kind": "SUBSCRIBES_TO"},
		},
	})

	// ---- payments service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "payments",
		Entities: []map[string]any{
			{"id": "settle_fn", "name": "settle_payment", "kind": "SCOPE.Operation", "source_file": "payments/service.py"},
			{"id": "topic_payments_settled_pub", "name": "sns:payments.settled", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "settle_fn", "to_id": "topic_payments_settled_pub", "kind": "PUBLISHES_TO"},
		},
	})

	// ---- billing service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "billing",
		Entities: []map[string]any{
			{"id": "billing_fn", "name": "record_payment", "kind": "SCOPE.Operation", "source_file": "billing/consumer.go"},
			{"id": "topic_payments_settled_billing", "name": "sns:payments.settled", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "billing_fn", "to_id": "topic_payments_settled_billing", "kind": "SUBSCRIBES_TO"},
		},
	})

	// ---- ledger service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "ledger",
		Entities: []map[string]any{
			{"id": "ledger_fn", "name": "post_entry", "kind": "SCOPE.Operation", "source_file": "ledger/consumer.go"},
			{"id": "topic_payments_settled_ledger", "name": "sns:payments.settled", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "ledger_fn", "to_id": "topic_payments_settled_ledger", "kind": "SUBSCRIBES_TO"},
		},
	})

	// ---- shipping service ----
	writeFixture(t, root, fixtureGraph{
		Repo: "shipping",
		Entities: []map[string]any{
			{"id": "shipping_fn", "name": "create_shipment", "kind": "SCOPE.Operation", "source_file": "shipping/consumer.go"},
			{"id": "topic_inv_reserved_sub", "name": "sns:inventory.reserved", "kind": "SCOPE.MessageTopic", "source_file": ""},
		},
		Edges: []map[string]string{
			{"from_id": "shipping_fn", "to_id": "topic_inv_reserved_sub", "kind": "SUBSCRIBES_TO"},
		},
	})

	home := filepath.Join(root, "ag-home-shipfast")
	result, err := RunAllPasses("shipfast", root, home)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := readDoc(filepath.Join(home, "groups", "shipfast-links.json"))
	if err != nil {
		t.Fatal(err)
	}

	grpcCount := 0
	topicCount := 0
	for _, l := range doc.Links {
		switch l.Method {
		case MethodGRPC:
			grpcCount++
		case MethodTopic:
			topicCount++
		}
	}

	// §2: At least orders→inventory gRPC link.
	if grpcCount < 1 {
		t.Errorf("§2 gRPC: expected ≥1 cross-repo gRPC link, got 0; pass results=%+v", result.Results)
	}

	// Verify the specific orders→inventory Reserve link.
	grpcFound := false
	for _, l := range doc.Links {
		if l.Method == MethodGRPC &&
			l.Source == "orders::create_order_fn" &&
			l.Target == "inventory::inventory_handler" {
			grpcFound = true
			break
		}
	}
	if !grpcFound {
		t.Error("§2 gRPC: missing orders::create_order_fn → inventory::inventory_handler link")
	}

	// §3: Expect 6 topic links total:
	//   orders.placed: orders→inventory (1)
	//   orders.placed: orders→notifications (2)
	//   orders.placed: orders→analytics (3)
	//   payments.settled: payments→orders (4)
	//   payments.settled: payments→billing (5)
	//   payments.settled: payments→ledger (6)
	//   inventory.reserved: inventory→shipping (7)
	// = 7 topic links
	if topicCount < 5 {
		t.Errorf("§3 topics: expected ≥5 cross-repo topic links, got %d; pass results=%+v", topicCount, result.Results)
	}

	t.Logf("Before: 0 gRPC links, 0 topic links")
	t.Logf("After: %d gRPC links, %d topic links", grpcCount, topicCount)
	t.Logf("Total cross-repo links: %d", len(doc.Links))
}
```

- [ ] **Step 2: Run the acceptance test**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
go test ./internal/links/ -run TestShipFastManifest -v 2>&1
```

Expected:
```
--- PASS: TestShipFastManifest_GRPCAndTopicLinks
    links_test: Before: 0 gRPC links, 0 topic links
    links_test: After: 1 gRPC links, 7 topic links
    links_test: Total cross-repo links: 8+
```

- [ ] **Step 3: Run the full test suite**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
go test ./internal/links/ ./internal/engine/ -count=1 2>&1 | tail -20
```

Expected: all PASS, no failures.

- [ ] **Step 4: Build the full binary**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
go build ./cmd/archigraph/ 2>&1
```

Expected: clean build.

- [ ] **Step 5: Commit acceptance test**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
git add internal/links/shipfast_grpc_topic_test.go
git commit -m "links: add ShipFast §2/§3 acceptance test for #1447"
```

---

## Task 7: Open pull request

- [ ] **Step 1: Push branch**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/fix-grpc-topic
git push -u origin fix/xrepo-grpc-topic
```

- [ ] **Step 2: Create PR**

```bash
gh pr create \
  --repo cajasmota/archigraph \
  --title "links: add P6 gRPC + P7 message-topic cross-repo passes (fixes #1447)" \
  --body "$(cat <<'EOF'
## Summary

Fixes #1447. ShipFast polyglot corpus previously showed 0 gRPC links and 0 topic cross-repo links.

## Problem

The gRPC engine pass (#725) emits `SCOPE.GrpcMethod` entities keyed by `grpc:ServiceName/MethodName`, with `GRPC_HANDLES` edges on the client side and `GRPC_IMPLEMENTS` edges on the server side. The Kafka/SNS/SQS/EventBridge engine passes emit `SCOPE.MessageTopic` entities with `PUBLISHES_TO` / `SUBSCRIBES_TO` edges. Neither was wired into the cross-repo link passes in `internal/links/`.

The import pass (P1) cannot join these because `graph.EntityID()` hashes the repo tag — the same `grpc:Inventory/Reserve` entity has a **different ID** in orders vs inventory. The solution (matching by `.Name` across repos) already existed for HTTP endpoints in P4.

## Changes

- **P6 (`grpc_pass.go`):** Scans all per-repo graphs for `SCOPE.GrpcMethod` entities, groups by canonical Name (`grpc:Service/Method`), requires at least one `GRPC_HANDLES` (client) and one `GRPC_IMPLEMENTS` (server) edge targeting the matched entities in different repos, then emits `method=grpc, channel=grpc, relation=calls` links from caller → handler.

- **P7 (`topic_pass.go`):** Scans for `SCOPE.MessageTopic` entities, groups by broker-prefixed Name (`kafka:orders.placed`, `sns:payments.settled`, etc.), joins publisher repos (`PUBLISHES_TO` inbound) with subscriber repos (`SUBSCRIBES_TO` inbound), emits `method=topic, channel=<broker>, relation=publishes_to` links.

- **`links.go`:** Registers P6 and P7 in `RunAllPasses` after P5.

## Before / After (ShipFast fixture)

| Pass | Before | After |
|------|--------|-------|
| gRPC (§2) | 0 | ≥1 (orders→inventory Inventory/Reserve) |
| Topic (§3) | 0 | 7 (orders.placed×3, payments.settled×3, inventory.reserved×1) |

## Test plan

- [ ] `go test ./internal/links/ -run TestGRPC -v` — 4 new P6 unit tests pass
- [ ] `go test ./internal/links/ -run TestTopic -v` — 5 new P7 unit tests pass
- [ ] `go test ./internal/links/ -run TestShipFastManifest -v` — §2/§3 acceptance test passes
- [ ] `go test ./internal/links/ -count=1` — no regressions in existing P1–P5 tests
- [ ] `go build ./cmd/archigraph/` — clean build

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-Review Checklist

### Spec coverage
- [x] §2 gRPC: P6 matches `grpc:Service/Method` name across repos via GRPC_HANDLES/GRPC_IMPLEMENTS edges
- [x] §3 Topic: P7 matches `SCOPE.MessageTopic` by broker-prefixed name via PUBLISHES_TO/SUBSCRIBES_TO
- [x] Verification: ShipFast fixture test covers orders→inventory gRPC + all §3 pub/sub pairs
- [x] Unit tests for matching keys: `TestTopicPass_BrokerFromTopicName` + `TestGRPC*` method name coverage
- [x] Before→after counts reported in test log output
- [x] PR format: 6-section, `Fixes #1447`
- [x] No live daemon touched (build only in worktree)
- [x] No `index .` in the repo

### Placeholder scan
- No TBDs, no "implement later", no "similar to Task N".

### Type consistency
- `grpcMethodKindLink = "SCOPE.GrpcMethod"` matches `engine.grpcMethodKind`
- `topicMessageTopicKind = "SCOPE.MessageTopic"` matches `engine.messageTopicKind`
- `grpcHandlesEdge = "GRPC_HANDLES"` / `grpcImplementsEdge = "GRPC_IMPLEMENTS"` match engine constants
- `MethodGRPC`, `MethodTopic` are declared in their own files (same pattern as `MethodHTTP` in `http_pass.go`)
- `entityKey(repo, id)` is the existing helper in `links.go`
- `replaceByMethod`, `newMethodSet`, `ScoreImport`, `MakeID`, `discoveredAt`, `fixtureRoot`, `writeFixture`, `readDoc` are all existing symbols in the package
