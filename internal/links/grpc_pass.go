package links

// grpc_pass.go implements the cross-repo gRPC client-stub → server-impl
// matcher (P6).
//
// Design
// ------
// The engine pass (internal/engine/grpc_edges.go, #725) emits for every
// gRPC service discovered in a repo:
//
//	Entity{Kind: "SCOPE.GrpcMethod", Name: "grpc:ServiceName/MethodName"}
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
// replaces every entry whose method is "grpc" while leaving P1–P5 intact.
//
// Fallback: if no GRPC_HANDLES edge is present for a GrpcMethod entity, the
// pass falls back to using the GrpcMethod entity ID itself as the link
// source (so the link still points at something meaningful). Same fallback
// on the server side for GRPC_IMPLEMENTS. When neither edge is present on
// either side, no link is emitted for that name (we require at least one
// GRPC_HANDLES + one GRPC_IMPLEMENTS to produce a cross-repo edge).

import (
	"sort"
	"strings"
)

// MethodGRPC identifies this pass's emissions in links.json.
const MethodGRPC = "grpc"

// grpcChannel is the channel string written to every emitted link.
const grpcChannel = "grpc"

// grpcMethodKindLink is the entity kind emitted by the gRPC engine pass.
// Matches engine.grpcMethodKind = "SCOPE.GrpcMethod".
const grpcMethodKindLink = "SCOPE.GrpcMethod"

// grpcHandlesEdge / grpcImplementsEdge are the edge kinds emitted by the
// engine pass; compared case-insensitively to be robust to on-disk variance.
const grpcHandlesEdgeKindLink    = "GRPC_HANDLES"
const grpcImplementsEdgeKindLink = "GRPC_IMPLEMENTS"

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
		// Method-segregated overwrite still runs so a previous group of
		// ≥ 2 repos that shrunk to 1 cleans up its prior gRPC entries.
		_, _, err := replaceByMethod(paths.Links, newMethodSet(MethodGRPC), nil, rejects)
		return res, err
	}

	// Pre-compute per-repo inbound edge index: entity ID → []edges pointing TO it.
	// We need to find GRPC_HANDLES (client→method) and GRPC_IMPLEMENTS (handler→method)
	// edges that point AT each GrpcMethod entity.
	type inboundEdge struct {
		fromID string
		kind   string // "GRPC_HANDLES" or "GRPC_IMPLEMENTS"
	}
	// repo → toEntityID → []inboundEdge
	inboundByRepo := map[string]map[string][]inboundEdge{}
	for _, g := range graphs {
		m := map[string][]inboundEdge{}
		inboundByRepo[g.Repo] = m
		for _, e := range g.Edges {
			upperKind := strings.ToUpper(e.Kind)
			if upperKind == grpcHandlesEdgeKindLink || upperKind == grpcImplementsEdgeKindLink {
				m[e.ToID] = append(m[e.ToID], inboundEdge{fromID: e.FromID, kind: upperKind})
			}
		}
	}

	// Index: method name → repo → hit.
	// One hit per (repo, name) pair — first occurrence wins (dedup).
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
				case grpcHandlesEdgeKindLink:
					hit.isClient = true
					if hit.callerID == "" {
						hit.callerID = ie.fromID
					}
				case grpcImplementsEdgeKindLink:
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

		// Sort for deterministic ordering.
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
					continue // never emit a self-pair as a cross-repo edge
				}

				// Source: caller entity on client side; fall back to synthetic ID.
				srcID := client.callerID
				if srcID == "" {
					srcID = client.stampedID
				}
				// Target: handler entity on server side; fall back to synthetic ID.
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
