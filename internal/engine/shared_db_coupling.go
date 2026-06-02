// Package engine — shared-database cross-service coupling analysis
// (#3628 area #13).
//
// SharedDataCoupling is a PROJECT-SCOPE pass (sibling to structural_coupling.go
// and dependency_hygiene.go) that detects when two or more distinct modules
// access the SAME logical table or collection. Shared mutable data is the
// classic service-boundary smell: two services that read/write the same table
// are coupled through the datastore even when there is no direct call or import
// edge between them, so a clean split / extraction has to reconcile that shared
// ownership first. This pass surfaces that otherwise-invisible coupling.
//
// AXIS — DATA, NOT CONTROL. structural_coupling.go measures import/dependency
// fan-in/fan-out (Ca/Ce/instability on Module nodes). commit_coupling_edges.go
// measures temporal git co-change. This pass measures DATA coupling: the shared
// persistence substrate. The three are orthogonal and answer different
// questions for rewrite-boundary planning.
//
// INPUT. The pass consumes the already-assembled graph after document assembly
// AND module aggregation (so synthetic Module nodes exist and every entity
// carries Properties["module"]). It looks at three converged data-access
// signals, all of which encode the table/collection by NAME:
//
//   - ACCESSES_TABLE edges (function → SCOPE.DataAccess). The accessed table is
//     the `table` property on the target SCOPE.DataAccess entity; the accessing
//     module is the module of the SOURCE (enclosing-function) entity, falling
//     back to the DataAccess entity's own module when the source is unresolved.
//   - JOINS_COLLECTION edges (collection → collection, Mongo $lookup). Both the
//     joining and joined collections are shared data; the accessing module is
//     the module of the edge's SOURCE entity (the aggregating collection's
//     call-site file).
//   - SCOPE.DataAccess entities directly (covers accesses whose ACCESSES_TABLE
//     source did not resolve to a real function entity — the DataAccess node
//     itself carries `table` + `module`).
//
// OUTPUT. For every distinct (repo, table) key touched by ≥2 distinct modules:
//
//  1. The SCOPE.DataAccess entities for that table are annotated with
//     shared=true, accessor_count=N, accessor_modules=<sorted csv>. A table
//     touched by exactly one module is annotated shared=false (explicit, not
//     unknown) with accessor_count=1.
//  2. A SHARES_DATA edge is emitted between every unordered pair of co-accessing
//     Module nodes (FromID = lexicographically smaller Module ID), carrying the
//     comma-joined sorted list of co-accessed tables and a shared_count. This
//     is the cross-service data-coupling edge.
//
// HONEST. A SHARES_DATA edge / shared=true annotation is emitted ONLY when the
// shared table entity genuinely exists AND ≥2 DISTINCT modules access it. The
// "_external" catch-all module and the empty module are ignored as accessors so
// unattributed access never fabricates coupling. Self-pairs are suppressed. The
// pass is deterministic and idempotent.
package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

// Kind / property constants for the shared-db-coupling pass.
const (
	sharedKindDataAccess  = "SCOPE.DataAccess"
	sharedKindModule      = "Module"
	sharedRelAccessTable  = "ACCESSES_TABLE"
	sharedRelJoinsColl    = "JOINS_COLLECTION"
	sharedRelSharesData   = "SHARES_DATA"
	sharedModuleExternal  = "_external"
	sharedProvenance      = "SHARED_DB_COUPLING"
	sharedCouplingKindTag = "shared_data"
)

// SharedDataStats summarises a SharedDataCoupling run.
type SharedDataStats struct {
	// TablesConsidered is the number of distinct (repo, table) keys that had at
	// least one resolved module accessor.
	TablesConsidered int
	// SharedTables is the number of those tables touched by ≥2 distinct modules.
	SharedTables int
	// DataAccessAnnotated is the number of SCOPE.DataAccess entities stamped
	// with shared / accessor_count / accessor_modules.
	DataAccessAnnotated int
	// CouplingEdges is the number of SHARES_DATA Module→Module edges emitted.
	CouplingEdges int
	// Skipped is true when there is nothing to analyse (no Module nodes or no
	// data-access signal). Honest: no edge / annotation is produced.
	Skipped bool
}

// modulePair is an unordered pair of Module entity IDs with the smaller ID
// first, used as a deterministic key when accumulating co-accessed tables.
type modulePair struct {
	a, b string // a < b lexicographically
}

// makePair normalises two module IDs into a modulePair (smaller first).
func makePair(x, y string) modulePair {
	if x < y {
		return modulePair{a: x, b: y}
	}
	return modulePair{a: y, b: x}
}

// ApplySharedDataCoupling runs the shared-database coupling analysis over the
// assembled doc. It MUST run after module.Aggregate (so Module nodes exist and
// every entity carries Properties["module"]). It returns stats describing the
// run. Deterministic and idempotent.
func ApplySharedDataCoupling(doc *graph.Document) SharedDataStats {
	if doc == nil {
		return SharedDataStats{Skipped: true}
	}

	// Index Module entities by their synthetic (repo, name) → Module ID so we
	// can resolve a module name to the real graph node, and confirm the node
	// exists before emitting an edge to it.
	moduleNodeID := make(map[moduleKeyLite]string)
	haveModuleNode := false
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind != sharedKindModule {
			continue
		}
		repo := entityRepo(doc, e)
		name := e.Properties["module"]
		if name == "" {
			name = e.Name
		}
		moduleNodeID[moduleKeyLite{repo: repo, name: name}] = e.ID
		haveModuleNode = true
	}
	if !haveModuleNode {
		// Module aggregation did not run / produced nothing — no module
		// attribution is possible, so we cannot honestly assert coupling.
		return SharedDataStats{Skipped: true}
	}

	// entityModule resolves any entity ID to its (repo, moduleName). Used to
	// attribute the source end of ACCESSES_TABLE / JOINS_COLLECTION edges.
	entityModule := make(map[string]moduleKeyLite, len(doc.Entities))
	// dataAccessByTable groups SCOPE.DataAccess entity indices by (repo, table).
	dataAccessByTable := make(map[tableKey][]int)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == sharedKindModule {
			continue
		}
		repo := entityRepo(doc, e)
		mk := moduleKeyLite{repo: repo, name: moduleNameOf(e)}
		entityModule[e.ID] = mk
		if e.Kind == sharedKindDataAccess {
			tbl := normTable(e.Properties["table"])
			if tbl != "" {
				dataAccessByTable[tableKey{repo: repo, table: tbl}] = append(
					dataAccessByTable[tableKey{repo: repo, table: tbl}], i)
			}
		}
	}
	if len(dataAccessByTable) == 0 {
		return SharedDataStats{Skipped: true}
	}

	// tableAccessors collects the DISTINCT accessor module names per table.
	tableAccessors := make(map[tableKey]map[string]struct{})
	addAccessor := func(tk tableKey, mk moduleKeyLite) {
		if mk.name == "" || mk.name == sharedModuleExternal {
			return // unattributed — never fabricate coupling
		}
		// Only count an accessor whose module resolves to a real Module node in
		// the same repo, so the SHARES_DATA endpoints are guaranteed to exist.
		if _, ok := moduleNodeID[mk]; !ok {
			return
		}
		set := tableAccessors[tk]
		if set == nil {
			set = make(map[string]struct{})
			tableAccessors[tk] = set
		}
		set[mk.name] = struct{}{}
	}

	// (1) Direct SCOPE.DataAccess attribution: each DataAccess node carries its
	// own module + table.
	for tk, idxs := range dataAccessByTable {
		for _, i := range idxs {
			addAccessor(tk, entityModule[doc.Entities[i].ID])
		}
	}

	// daTableOf resolves a SCOPE.DataAccess entity ID → its (repo, table) key,
	// so edge endpoints landing on a DataAccess node map back to a table.
	daTableOf := make(map[string]tableKey)
	for tk, idxs := range dataAccessByTable {
		for _, i := range idxs {
			daTableOf[doc.Entities[i].ID] = tk
		}
	}

	// (2) ACCESSES_TABLE edges: function(source) → SCOPE.DataAccess(target).
	// Attribute the table to the SOURCE entity's module (the caller), which is
	// the most precise accessor. The target's table key is taken from the
	// DataAccess node, or, when the edge carries a `table` property and the
	// target did not index as a DataAccess node, from the edge property.
	// (3) JOINS_COLLECTION edges: attribute both endpoints' shared collection to
	// the source module.
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		switch r.Kind {
		case sharedRelAccessTable:
			tk, ok := tableKeyForEdge(doc, r, daTableOf, entityModule)
			if !ok {
				continue
			}
			if mk, ok := entityModule[r.FromID]; ok {
				addAccessor(tk, mk)
			}
		case sharedRelJoinsColl:
			// The joined collection is shared data; attribute it to the source
			// module (the file performing the $lookup). Resolve the table key
			// from either endpoint that is a known DataAccess/collection.
			tk, ok := tableKeyForEdge(doc, r, daTableOf, entityModule)
			if !ok {
				continue
			}
			if mk, ok := entityModule[r.FromID]; ok {
				addAccessor(tk, mk)
			}
		}
	}

	stats := SharedDataStats{}

	// pairTables accumulates the sorted set of co-accessed tables per module
	// pair, so a single SHARES_DATA edge can carry every shared table.
	pairTables := make(map[modulePair]map[string]struct{})

	// Annotate SCOPE.DataAccess entities and build module pairs.
	for tk, set := range tableAccessors {
		stats.TablesConsidered++
		count := len(set)

		names := make([]string, 0, count)
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		csv := strings.Join(names, ",")

		shared := count >= 2
		if shared {
			stats.SharedTables++
		}

		// Annotate every DataAccess entity for this table.
		for _, i := range dataAccessByTable[tk] {
			e := &doc.Entities[i]
			if e.Properties == nil {
				e.Properties = make(map[string]string)
			}
			if shared {
				e.Properties["shared"] = "true"
			} else {
				e.Properties["shared"] = "false"
			}
			e.Properties["accessor_count"] = fmt.Sprintf("%d", count)
			e.Properties["accessor_modules"] = csv
			stats.DataAccessAnnotated++
		}

		if !shared {
			continue
		}
		// Record every unordered module pair co-accessing this table.
		repo := tk.repo
		for x := 0; x < len(names); x++ {
			for y := x + 1; y < len(names); y++ {
				idA := moduleNodeID[moduleKeyLite{repo: repo, name: names[x]}]
				idB := moduleNodeID[moduleKeyLite{repo: repo, name: names[y]}]
				if idA == "" || idB == "" || idA == idB {
					continue
				}
				p := makePair(idA, idB)
				ts := pairTables[p]
				if ts == nil {
					ts = make(map[string]struct{})
					pairTables[p] = ts
				}
				ts[tk.table] = struct{}{}
			}
		}
	}

	// Pre-index existing SHARES_DATA edge IDs so a re-run is idempotent (the
	// pass appends edges; without this guard a second invocation would
	// double-emit). Deterministic IDs make the guard exact.
	existingShares := make(map[string]struct{})
	for k := range doc.Relationships {
		if doc.Relationships[k].Kind == sharedRelSharesData {
			existingShares[doc.Relationships[k].ID] = struct{}{}
		}
	}

	// Emit one SHARES_DATA edge per module pair, deterministically ordered.
	pairs := make([]modulePair, 0, len(pairTables))
	for p := range pairTables {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].a != pairs[j].a {
			return pairs[i].a < pairs[j].a
		}
		return pairs[i].b < pairs[j].b
	})
	for _, p := range pairs {
		tbls := pairTables[p]
		names := make([]string, 0, len(tbls))
		for t := range tbls {
			names = append(names, t)
		}
		sort.Strings(names)
		relID := graph.RelationshipID(p.a, p.b, sharedRelSharesData)
		if _, ok := existingShares[relID]; ok {
			continue // already emitted on a prior run — stay idempotent
		}
		rel := graph.Relationship{
			ID:     relID,
			FromID: p.a,
			ToID:   p.b,
			Kind:   sharedRelSharesData,
			Properties: map[string]string{
				"coupling":      sharedCouplingKindTag,
				"shared_tables": strings.Join(names, ","),
				"shared_count":  fmt.Sprintf("%d", len(names)),
				"provenance":    sharedProvenance,
			},
		}
		doc.Relationships = append(doc.Relationships, rel)
		stats.CouplingEdges++
	}

	doc.Stats.Relationships = len(doc.Relationships)
	return stats
}

// moduleKeyLite is a (repo, module-name) key local to this pass.
type moduleKeyLite struct {
	repo string
	name string
}

// tableKey identifies a logical table/collection within a repo.
type tableKey struct {
	repo  string
	table string
}

// entityRepo returns the per-entity repo tag, falling back to the document repo
// (mirrors module.Aggregate's resolution for multi-repo group documents).
func entityRepo(doc *graph.Document, e *graph.Entity) string {
	if e.Properties != nil {
		if v, ok := e.Properties["repo"]; ok && v != "" {
			return v
		}
	}
	return doc.Repo
}

// moduleNameOf returns the entity's module label, or "_external" when absent
// (mirrors module.Aggregate).
func moduleNameOf(e *graph.Entity) string {
	if e.Properties != nil {
		if v, ok := e.Properties["module"]; ok && v != "" {
			return v
		}
	}
	return sharedModuleExternal
}

// normTable canonicalises a table/collection name for converge-by-name grouping:
// trim, lowercase, and strip a schema/db qualifier prefix (e.g. "public.orders"
// → "orders", "mydb.orders" → "orders"). UNKNOWN dynamic tables are dropped.
func normTable(t string) string {
	s := strings.ToLower(strings.TrimSpace(t))
	if s == "" || s == "unknown" {
		return ""
	}
	if i := strings.LastIndex(s, "."); i >= 0 && i+1 < len(s) {
		s = s[i+1:]
	}
	// Strip surrounding quotes/backticks an ORM might leave on.
	s = strings.Trim(s, "`\"'[]")
	return s
}

// tableKeyForEdge resolves the (repo, table) key an ACCESSES_TABLE /
// JOINS_COLLECTION edge points at. Resolution order: (1) the target entity is a
// known SCOPE.DataAccess node → use its table key; (2) the edge carries a
// `table` property → use it with the source entity's repo. Returns false when
// neither yields a non-empty table.
func tableKeyForEdge(
	doc *graph.Document,
	r *graph.Relationship,
	daTableOf map[string]tableKey,
	entityModule map[string]moduleKeyLite,
) (tableKey, bool) {
	if tk, ok := daTableOf[r.ToID]; ok {
		return tk, true
	}
	if tk, ok := daTableOf[r.FromID]; ok {
		return tk, true
	}
	if r.Properties != nil {
		tbl := normTable(r.Properties["table"])
		if tbl == "" {
			tbl = normTable(r.Properties["from"]) // JOINS_COLLECTION `from`
		}
		if tbl != "" {
			repo := doc.Repo
			if mk, ok := entityModule[r.FromID]; ok && mk.repo != "" {
				repo = mk.repo
			}
			return tableKey{repo: repo, table: tbl}, true
		}
	}
	return tableKey{}, false
}

// compile-time assertion that the SHARES_DATA kind is registered.
var _ = types.RelationshipKindSharesData
