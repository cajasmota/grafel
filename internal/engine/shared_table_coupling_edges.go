// Package engine — cross-service shared-table coupling (#5204, refs #3628 #13).
//
// ApplySharedTableCouplingEdges is a PROJECT-SCOPE post-pass that surfaces the
// single highest-risk hidden coupling for a microservices / rewrite-parity
// graph: two DISTINCT services that both touch the SAME physical table, with at
// least one of them WRITING it. That is the classic shared-database smell — the
// services are coupled through the datastore even though there is no call or
// import edge between them, and a clean service split (Django→NestJS) has to
// reconcile that shared ownership before it can break the table apart.
//
// AXIS — CROSS-SERVICE, NOT INTRA-REPO. ApplySharedDataCoupling
// (shared_db_coupling.go) already models intra-repo MODULE↔MODULE data coupling
// via SHARES_DATA. This pass is the orthogonal CROSS-SERVICE / CROSS-REPO view:
// it groups by RESOLVED physical table across repos and emits SHARES_TABLE_WITH
// between the SERVICE entities of distinct repos. Same-service multi-module
// access is explicitly NOT coupling here (that is SHARES_DATA's job) and is
// guarded out by keying accessors on the repo/service, not the module.
//
// INPUT. The pass consumes the already-assembled graph after document assembly.
// It reuses the SAME two converged data-access signals the SHARES_DATA pass
// reads, both of which encode the physical table by NAME and the read/write
// operation:
//
//   - SCOPE.DataAccess entities directly — each carries `table` and the SQL
//     `operation` (SELECT/INSERT/UPDATE/DELETE/UPSERT/TRUNCATE). The accessing
//     service is the entity's repo.
//   - ACCESSES_TABLE edges (function → SCOPE.DataAccess) — the accessing service
//     is the SOURCE entity's repo; the table/operation come from the edge
//     properties (or the target DataAccess node).
//
// OUTPUT. For every distinct NORMALISED physical table touched by ≥2 distinct
// services where ≥1 of them WRITES it, the pass emits one SHARES_TABLE_WITH edge
// per unordered service pair (lexicographically smaller service ID is FromID),
// carrying: table, access_from / access_to (the read/write kinds per side),
// writer (from|to|both), confidence=high, provenance=SHARED_TABLE_COUPLING. The
// canonical `service:<repo>` SCOPE.Service node is minted on demand (same
// convention deployment_topology_edges.go / api_gateway_routing_edges.go use) so
// the edge always lands on a real graph node.
//
// HONEST. Fires ONLY on resolved literal table identities — UNKNOWN / dynamic /
// unresolved tables are dropped (normTable returns ""). Requires ≥2 DISTINCT
// services (a single service touching a table, or one service across two of its
// own modules, never fires — the accessor key is the repo). Requires ≥1 WRITE
// (a pair that only ever reads the table is real sharing but not the mutable
// coupling this smell is about, so it is not emitted). Append-only,
// deterministic, and idempotent (re-running mints no duplicate edge).
package engine

import (
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Kind / property constants for the shared-table-coupling pass.
const (
	sharedTableRelSharesTableWith = "SHARES_TABLE_WITH"
	sharedTableServiceKind        = "SCOPE.Service"
	sharedTableProvenance         = "SHARED_TABLE_COUPLING"
)

// sharedTableWriteOps is the set of SCOPE.DataAccess `operation` values that
// MUTATE a table. Anything else (SELECT, or an unknown/missing op) is treated as
// a read. Matched case-insensitively against the operation property.
var sharedTableWriteOps = map[string]bool{
	"insert": true, "update": true, "delete": true,
	"upsert": true, "truncate": true, "merge": true,
	"create": true, "drop": true, "alter": true,
}

// sharedTableServiceID is the canonical service node key for a repo, matching
// the `service:<name>` convention the IaC/topology passes already use.
func sharedTableServiceID(repo string) string { return "service:" + repo }

// SharedTableStats summarises an ApplySharedTableCouplingEdges run.
type SharedTableStats struct {
	// TablesConsidered is the number of distinct normalised physical tables that
	// had at least one resolved service accessor.
	TablesConsidered int
	// SharedTables is the number of those tables touched by ≥2 distinct services
	// with ≥1 writer (i.e. tables that produced at least one edge).
	SharedTables int
	// CouplingEdges is the number of SHARES_TABLE_WITH service↔service edges
	// emitted.
	CouplingEdges int
	// ServicesMinted is the number of synthetic service:<repo> nodes created to
	// anchor the edges.
	ServicesMinted int
	// Skipped is true when there is nothing to analyse (no data-access signal).
	Skipped bool
}

// ApplySharedTableCouplingEdges runs the cross-service shared-table coupling
// analysis over the assembled doc. Registered after the table-attribution /
// SHARES_DATA passes. Deterministic, idempotent, append-only.
func ApplySharedTableCouplingEdges(doc *graph.Document) SharedTableStats {
	if doc == nil {
		return SharedTableStats{Skipped: true}
	}

	// tableAccess collects per-service access kinds for each normalised table:
	// table -> service -> {reads, writes}.
	type rw struct{ read, write bool }
	tableAccess := make(map[string]map[string]*rw)

	add := func(table, service string, write bool) {
		if table == "" || service == "" {
			return
		}
		svcMap := tableAccess[table]
		if svcMap == nil {
			svcMap = make(map[string]*rw)
			tableAccess[table] = svcMap
		}
		r := svcMap[service]
		if r == nil {
			r = &rw{}
			svcMap[service] = r
		}
		if write {
			r.write = true
		} else {
			r.read = true
		}
	}

	// Index SCOPE.DataAccess nodes by ID so ACCESSES_TABLE targets resolve to a
	// table + operation, and record direct (service, table) access from each.
	daTable := make(map[string]string) // DataAccess ID -> normalised table
	daWrite := make(map[string]bool)   // DataAccess ID -> is a write op
	daRepo := make(map[string]string)  // DataAccess ID -> repo/service
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind != sharedKindDataAccess {
			continue
		}
		tbl := normTable(e.PropGet("table"))
		if tbl == "" {
			continue
		}
		repo := entityRepo(doc, e)
		write := sharedTableWriteOps[strings.ToLower(strings.TrimSpace(e.PropGet("operation")))]
		daTable[e.ID] = tbl
		daWrite[e.ID] = write
		daRepo[e.ID] = repo
		add(tbl, repo, write)
	}

	// entityRepoByID resolves any entity ID → its repo, for attributing the
	// SOURCE (caller) end of an ACCESSES_TABLE edge to the right service.
	entityRepoByID := make(map[string]string, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		entityRepoByID[e.ID] = entityRepo(doc, e)
	}

	// ACCESSES_TABLE edges: function(source) → SCOPE.DataAccess(target). The
	// caller's service is the SOURCE entity's repo; the table/operation come
	// from the edge property or the target DataAccess node.
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		if r.Kind != sharedRelAccessTable {
			continue
		}
		tbl := ""
		write := false
		if t, ok := daTable[r.ToID]; ok {
			tbl = t
			write = daWrite[r.ToID]
		} else if r.PropLen() > 0 {
			tbl = normTable(r.PropGet("table"))
			write = sharedTableWriteOps[strings.ToLower(strings.TrimSpace(r.PropGet("operation")))]
		}
		if tbl == "" {
			continue
		}
		// Prefer the caller's repo; fall back to the DataAccess node's repo.
		service := entityRepoByID[r.FromID]
		if service == "" {
			service = daRepo[r.ToID]
		}
		add(tbl, service, write)
	}

	if len(tableAccess) == 0 {
		return SharedTableStats{Skipped: true}
	}

	// Pre-index existing entities + SHARES_TABLE_WITH edges for idempotency and
	// to avoid re-minting service nodes that already exist.
	haveEntity := make(map[string]bool, len(doc.Entities))
	for i := range doc.Entities {
		haveEntity[doc.Entities[i].ID] = true
	}
	existingEdge := make(map[string]bool)
	for k := range doc.Relationships {
		if doc.Relationships[k].Kind == sharedTableRelSharesTableWith {
			existingEdge[doc.Relationships[k].ID] = true
		}
	}

	stats := SharedTableStats{}

	mintService := func(repo string) string {
		id := sharedTableServiceID(repo)
		if !haveEntity[id] {
			haveEntity[id] = true
			doc.Entities = append(doc.Entities, graph.Entity{
				ID:            id,
				Name:          repo,
				QualifiedName: id,
				Kind:          sharedTableServiceKind,
			}.WithProperties(map[string]string{
				"repo":      repo,
				"synthesis": "shared_table_coupling",
			},
			))
			stats.ServicesMinted++
		}
		return id
	}

	// accessKinds renders an rw into a sorted comma-joined "read,write" string.
	accessKinds := func(r *rw) string {
		var parts []string
		if r.read {
			parts = append(parts, "read")
		}
		if r.write {
			parts = append(parts, "write")
		}
		return strings.Join(parts, ",")
	}

	// Walk tables deterministically.
	tables := make([]string, 0, len(tableAccess))
	for t := range tableAccess {
		tables = append(tables, t)
	}
	sort.Strings(tables)

	for _, tbl := range tables {
		svcMap := tableAccess[tbl]
		// Distinct services only — same-service multi-module access collapses to
		// one entry here (the accessor key is the repo), so it cannot fabricate
		// cross-service coupling.
		services := make([]string, 0, len(svcMap))
		for s := range svcMap {
			if s != "" {
				services = append(services, s)
			}
		}
		if len(services) < 2 {
			continue
		}
		sort.Strings(services)
		stats.TablesConsidered++

		tableEmitted := false
		for x := 0; x < len(services); x++ {
			for y := x + 1; y < len(services); y++ {
				ra := svcMap[services[x]]
				rb := svcMap[services[y]]
				// Real mutable coupling requires ≥1 writer on the pair.
				if !ra.write && !rb.write {
					continue
				}
				idA := mintService(services[x])
				idB := mintService(services[y])
				if idA == idB {
					continue // defensive: same canonical service node
				}
				// Order endpoints deterministically (smaller ID is FromID) and
				// keep access kinds aligned with the chosen direction.
				fromID, toID := idA, idB
				fromRW, toRW := ra, rb
				if idB < idA {
					fromID, toID = idB, idA
					fromRW, toRW = rb, ra
				}
				writer := "from"
				switch {
				case fromRW.write && toRW.write:
					writer = "both"
				case toRW.write:
					writer = "to"
				}
				relID := graph.RelationshipID(fromID, toID, sharedTableRelSharesTableWith)
				if existingEdge[relID] {
					continue // idempotent across re-runs
				}
				existingEdge[relID] = true
				doc.Relationships = append(doc.Relationships, graph.Relationship{
					ID:     relID,
					FromID: fromID,
					ToID:   toID,
					Kind:   sharedTableRelSharesTableWith,
				}.WithProperties(map[string]string{
					"table":       tbl,
					"access_from": accessKinds(fromRW),
					"access_to":   accessKinds(toRW),
					"writer":      writer,
					"confidence":  "high",
					"provenance":  sharedTableProvenance,
				},
				))
				stats.CouplingEdges++
				tableEmitted = true
			}
		}
		if tableEmitted {
			stats.SharedTables++
		}
	}

	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)
	return stats
}

// compile-time assertion that the SHARES_TABLE_WITH kind is registered.
var _ = types.RelationshipKindSharesTableWith
