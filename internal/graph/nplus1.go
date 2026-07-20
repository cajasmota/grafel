// Package graph — nplus1.go implements the N+1 query anti-pattern detector.
//
// The detector walks the entity+relationship graph produced by grafel index
// and flags function/method entities that contain ORM query calls inside an
// apparent loop body. It operates entirely on the on-disk graph.Document, so
// it runs after indexing without re-parsing source files.
//
// # Algorithm
//
//  1. Build adjacency maps from Relationship slices (CONTAINS, CALLS, QUERIES).
//  2. For every entity whose Properties["orm"] is set (i.e. emitted by the
//     ORM-query pass, #723), collect the set of query-call entity IDs.
//  3. Walk each Function/Class entity's CONTAINS sub-graph; when we find a
//     node whose subtype indicates a loop ("for_loop", "while_loop",
//     "list_comprehension", etc.) and that node has a CALLS/QUERIES edge to a
//     known query entity, flag the enclosing function.
//  4. Alternatively (and more robustly for graph shapes where loops are not
//     explicit entities), inspect each QUERIES edge: when the caller entity's
//     Properties["loop_context"] is "true" — set by the ORM enricher — it is
//     already a known N+1 site.
//  5. As a belt-and-suspenders pass, any entity with subtype in the loop-subtype
//     set that directly emits a QUERIES edge is itself an N+1 site.
//
// # False-positive suppression
//
// Queries inside prefetch/eager-load calls are excluded via an ORM-safe-method
// allowlist. Developers may also annotate a call site with the comment
// "grafel:nplus1-safe" in the source (recorded as
// Properties["nplus1_safe"]="true" by the ORM extractor) to opt out.
//
// # Output
//
// Each finding is a Finding value. Callers may attach the property
// anti_pattern="n_plus_1" to the flagged Relationship or Entity, or emit the
// findings through the /api/quality/anti-patterns/{group} endpoint.
package graph

import (
	"fmt"
	"sort"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// Public types
// ─────────────────────────────────────────────────────────────────────────────

// NPlusOneFinding describes a single N+1 query instance.
type NPlusOneFinding struct {
	// CallerEntityID is the Function/Method entity that contains the loop.
	CallerEntityID string `json:"caller_entity_id"`
	// CallerName is the human-readable name of that entity.
	CallerName string `json:"caller_name"`
	// CallerFile is the source file of the caller entity.
	CallerFile string `json:"caller_file"`
	// CallerStartLine is the start line of the enclosing function.
	CallerStartLine int `json:"caller_start_line"`

	// QueryEntityID is the entity ID of the ORM query call site.
	QueryEntityID string `json:"query_entity_id"`
	// QueryName is the name of the query call (e.g. "User.objects.get").
	QueryName string `json:"query_name"`
	// QueryFile is the source file of the query call site.
	QueryFile string `json:"query_file"`
	// QueryLine is the line of the query call site.
	QueryLine int `json:"query_line"`

	// ORM is the ORM framework (e.g. "django", "sqlalchemy", "activerecord").
	ORM string `json:"orm"`
	// Language is the programming language.
	Language string `json:"language"`

	// LoopEntityID is the entity ID of the intermediate loop entity, if any.
	// Empty when the loop context was inferred from Properties["loop_context"].
	LoopEntityID string `json:"loop_entity_id,omitempty"`
	// LoopSubtype is the loop kind ("for_loop", "while_loop", etc.).
	LoopSubtype string `json:"loop_subtype,omitempty"`

	// Suggestion is the recommended remediation.
	Suggestion string `json:"suggestion"`
}

// NPlusOneReport is the complete output of DetectNPlusOne.
type NPlusOneReport struct {
	// Findings is the list of detected N+1 sites, sorted by file+line.
	Findings []NPlusOneFinding `json:"findings"`
	// EntitiesScanned is the number of entities examined.
	EntitiesScanned int `json:"entities_scanned"`
	// RelationshipsScanned is the number of relationships examined.
	RelationshipsScanned int `json:"relationships_scanned"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Detector
// ─────────────────────────────────────────────────────────────────────────────

// loopSubtypes is the set of entity subtypes that represent loop bodies. The
// list deliberately includes list comprehensions and generator expressions
// because a Python `[User.objects.get(id=i) for i in ids]` is semantically
// equivalent to a for-loop with an ORM query inside.
var loopSubtypes = map[string]bool{
	"for_loop":               true,
	"while_loop":             true,
	"for_in_loop":            true,
	"for_of_loop":            true,
	"foreach_loop":           true,
	"list_comprehension":     true,
	"set_comprehension":      true,
	"dict_comprehension":     true,
	"generator_expression":   true,
	"do_while_loop":          true,
	"enhanced_for_statement": true, // Java
	"for_statement":          true,
	"while_statement":        true,
}

// ormSafeMethods is the set of ORM method names that perform batch/eager
// loading and therefore do NOT constitute an N+1 problem even when called
// inside a loop body. Presence of any of these methods on the same query chain
// suppresses the finding.
var ormSafeMethods = map[string]bool{
	// Django
	"prefetch_related":         true,
	"select_related":           true,
	"prefetch_related_objects": true,
	// SQLAlchemy
	"joinedload":     true,
	"subqueryload":   true,
	"selectinload":   true,
	"lazyload":       true,
	"immediateload":  true,
	"contains_eager": true,
	"raiseload":      true,
	// ActiveRecord / Rails
	"includes":   true,
	"eager_load": true,
	"preload":    true,
	// Eloquent (Laravel)
	"with":        true,
	"load":        true,
	"loadMissing": true,
	// Sequelize (Node)
	"include": true,
	"findAll": true, // only safe when include: [] is used; approximation
	// Hibernate / JPA
	"fetchJoin": true,
	"fetch":     true,
	// GORM
	"Preload": true,
	"Joins":   true,
}

// ormQueryMethods is the set of ORM method names that issue individual DB
// queries and are therefore N+1 candidates when called in a loop.
var ormQueryMethods = map[string]bool{
	// Django
	"get":    true,
	"filter": true,
	"all":    true,
	"first":  true,
	"last":   true,
	"save":   true,
	"create": true,
	"update": true,
	"delete": true,
	// SQLAlchemy
	"query":       true,
	"execute":     true,
	"scalar":      true,
	"scalars":     true,
	"one":         true,
	"one_or_none": true,
	"fetchone":    true,
	"fetchall":    true,
	// ActiveRecord
	"find":    true,
	"find_by": true,
	"where":   true,
	"order":   true,
	// Eloquent
	"findOrFail": true,
	// Sequelize
	"findOne":  true,
	"findByPk": true,
	"count":    true,
	// GORM
	"Find":   true,
	"First":  true,
	"Last":   true,
	"Take":   true,
	"Create": true,
	"Save":   true,
}

// ormSuggestion returns the framework-specific remediation string.
func ormSuggestion(orm, lang string) string {
	switch strings.ToLower(orm) {
	case "django":
		return "Use QuerySet.select_related() or prefetch_related() to batch-load related objects before the loop."
	case "sqlalchemy":
		return "Use joinedload(), selectinload(), or subqueryload() in the query options to eager-load related rows before the loop."
	case "activerecord":
		return "Use includes() or eager_load() to batch-load associations before iterating."
	case "eloquent":
		return "Use Eloquent with() or load() to eager-load relationships before the loop."
	case "sequelize":
		return "Use Sequelize include option on findAll() to eager-load associations in one query."
	case "gorm":
		return "Use GORM Preload() or Joins() to eager-load associations before the loop."
	case "hibernate", "jpa":
		return "Use JPQL JOIN FETCH or @EntityGraph to eager-load associations."
	}
	// Generic fallback
	_ = lang
	return "Batch-load related objects before the loop using your ORM's eager-loading mechanism to avoid N+1 queries."
}

// isORMQueryEntity reports whether the entity looks like an ORM query call
// site: it has Properties["orm"] set, and its name contains a known query
// method (or the orm property is set without a safe-method suffix).
func isORMQueryEntity(e Entity) bool {
	if e.PropLen() == 0 {
		return false
	}
	if e.PropGet("nplus1_safe") == "true" {
		return false
	}
	orm := e.PropGet("orm")
	if orm == "" {
		return false
	}
	// Check if the entity name ends in a known safe method — if so, skip it.
	lname := strings.ToLower(e.Name)
	for safe := range ormSafeMethods {
		if strings.HasSuffix(lname, "."+strings.ToLower(safe)) ||
			strings.HasSuffix(lname, strings.ToLower(safe)) {
			return false
		}
	}
	return true
}

// entityKey uniquely identifies a finding by caller+query pair so we don't
// emit duplicates when multiple paths reach the same loop.
type nplus1Key struct {
	callerID string
	queryID  string
}

// DetectNPlusOne scans doc for N+1 query anti-patterns and returns a report.
// It runs in O(E + R) time where E = entity count and R = relationship count.
func DetectNPlusOne(doc *Document) *NPlusOneReport {
	if doc == nil {
		return &NPlusOneReport{}
	}

	// ── Index entities by ID ─────────────────────────────────────────────────
	entByID := make(map[string]*Entity, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		entByID[e.ID] = e
	}

	// ── Build adjacency: CONTAINS parent → children ──────────────────────────
	// containsChildren[parentID] = set of child entity IDs
	containsChildren := make(map[string]map[string]bool, len(doc.Entities)/4)
	// containsParent[childID] = parentID  (first parent wins; entities
	// should have a single CONTAINS parent in practice)
	containsParent := make(map[string]string, len(doc.Entities))

	// ── Build adjacency: CALLS/QUERIES from → to ─────────────────────────────
	callsEdges := make(map[string][]string, len(doc.Relationships)/4)

	for _, r := range doc.Relationships {
		switch r.Kind {
		case "CONTAINS":
			if containsChildren[r.FromID] == nil {
				containsChildren[r.FromID] = make(map[string]bool)
			}
			containsChildren[r.FromID][r.ToID] = true
			if _, already := containsParent[r.ToID]; !already {
				containsParent[r.ToID] = r.FromID
			}
		case "CALLS", "QUERIES":
			callsEdges[r.FromID] = append(callsEdges[r.FromID], r.ToID)
		}
	}

	// ── Identify ORM query entities ───────────────────────────────────────────
	ormQueryEntityIDs := make(map[string]bool, 64)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if isORMQueryEntity(*e) {
			ormQueryEntityIDs[e.ID] = true
		}
	}

	// ── Walk the graph to find N+1 sites ─────────────────────────────────────
	seen := make(map[nplus1Key]bool)
	var findings []NPlusOneFinding

	// Pass 1: entities with Properties["loop_context"]="true" that are ORM
	// query entities — the ORM extractor (#723) already tagged them.
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.PropLen() == 0 {
			continue
		}
		if e.PropGet("loop_context") != "true" {
			continue
		}
		if !ormQueryEntityIDs[e.ID] {
			continue
		}
		// Find the enclosing function by walking containsParent.
		callerID := nearestFunction(e.ID, containsParent, entByID)
		key := nplus1Key{callerID: callerID, queryID: e.ID}
		if seen[key] {
			continue
		}
		seen[key] = true

		caller := entByID[callerID]
		findings = append(findings, buildFinding(caller, e, "", ""))
	}

	// Pass 2: loop-subtype entities that have a direct CALLS/QUERIES edge to
	// an ORM query entity.
	for i := range doc.Entities {
		loop := &doc.Entities[i]
		if !loopSubtypes[strings.ToLower(loop.Subtype)] {
			continue
		}
		// Check all CALLS/QUERIES edges from this loop entity.
		for _, targetID := range callsEdges[loop.ID] {
			target, ok := entByID[targetID]
			if !ok || !ormQueryEntityIDs[targetID] {
				continue
			}
			callerID := nearestFunction(loop.ID, containsParent, entByID)
			key := nplus1Key{callerID: callerID, queryID: targetID}
			if seen[key] {
				continue
			}
			seen[key] = true
			caller := entByID[callerID]
			findings = append(findings, buildFinding(caller, target, loop.ID, loop.Subtype))
		}

		// Also check entities CONTAINED BY the loop for ORM calls.
		for childID := range containsChildren[loop.ID] {
			child, ok := entByID[childID]
			if !ok || !ormQueryEntityIDs[childID] {
				continue
			}
			callerID := nearestFunction(loop.ID, containsParent, entByID)
			key := nplus1Key{callerID: callerID, queryID: childID}
			if seen[key] {
				continue
			}
			seen[key] = true
			caller := entByID[callerID]
			findings = append(findings, buildFinding(caller, child, loop.ID, loop.Subtype))
		}
	}

	// Pass 3: for any CALLS/QUERIES edge where the caller's subtype is a loop.
	for i := range doc.Entities {
		caller := &doc.Entities[i]
		if !loopSubtypes[strings.ToLower(caller.Subtype)] {
			continue
		}
		for _, targetID := range callsEdges[caller.ID] {
			target, ok := entByID[targetID]
			if !ok || !ormQueryEntityIDs[targetID] {
				continue
			}
			enclosingID := nearestFunction(caller.ID, containsParent, entByID)
			key := nplus1Key{callerID: enclosingID, queryID: targetID}
			if seen[key] {
				continue
			}
			seen[key] = true
			enclosing := entByID[enclosingID]
			findings = append(findings, buildFinding(enclosing, target, caller.ID, caller.Subtype))
		}
	}

	// ── Sort findings by file+line for deterministic output ───────────────────
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].QueryFile != findings[j].QueryFile {
			return findings[i].QueryFile < findings[j].QueryFile
		}
		return findings[i].QueryLine < findings[j].QueryLine
	})

	return &NPlusOneReport{
		Findings:             findings,
		EntitiesScanned:      len(doc.Entities),
		RelationshipsScanned: len(doc.Relationships),
	}
}

// nearestFunction walks the containsParent chain from startID and returns the
// ID of the first ancestor whose kind is Function, Operation, or Method. If
// none is found, startID itself is returned as a fallback.
func nearestFunction(startID string, containsParent map[string]string, entByID map[string]*Entity) string {
	visited := make(map[string]bool, 8)
	cur := startID
	for {
		if visited[cur] {
			break
		}
		visited[cur] = true
		parent, ok := containsParent[cur]
		if !ok {
			break
		}
		pe, ok := entByID[parent]
		if !ok {
			break
		}
		k := strings.ToLower(pe.Kind)
		if strings.Contains(k, "function") ||
			strings.Contains(k, "operation") ||
			strings.Contains(k, "method") ||
			pe.Kind == "SCOPE.Function" ||
			pe.Kind == "SCOPE.Operation" {
			return parent
		}
		cur = parent
	}
	// Fallback: return startID's own parent or startID.
	if p, ok := containsParent[startID]; ok {
		return p
	}
	return startID
}

// buildFinding constructs a NPlusOneFinding from caller and query entities.
// loopEntityID and loopSubtype are optional (may be empty strings).
func buildFinding(caller *Entity, query *Entity, loopEntityID, loopSubtype string) NPlusOneFinding {
	var callerName, callerFile string
	var callerStartLine int
	if caller != nil {
		callerName = caller.Name
		callerFile = caller.SourceFile
		callerStartLine = caller.StartLine
	}

	orm := ""
	lang := ""
	if query != nil && query.PropLen() > 0 {
		orm = query.PropGet("orm")
		lang = query.Language
	}
	if lang == "" && caller != nil {
		lang = caller.Language
	}

	queryName := ""
	queryFile := ""
	queryLine := 0
	queryID := ""
	if query != nil {
		queryName = query.Name
		queryFile = query.SourceFile
		queryLine = query.StartLine
		queryID = query.ID
	}

	callerID := ""
	if caller != nil {
		callerID = caller.ID
	}

	return NPlusOneFinding{
		CallerEntityID:  callerID,
		CallerName:      callerName,
		CallerFile:      callerFile,
		CallerStartLine: callerStartLine,
		QueryEntityID:   queryID,
		QueryName:       queryName,
		QueryFile:       queryFile,
		QueryLine:       queryLine,
		ORM:             orm,
		Language:        lang,
		LoopEntityID:    loopEntityID,
		LoopSubtype:     loopSubtype,
		Suggestion:      ormSuggestion(orm, lang),
	}
}

// AnnotateDocument mutates doc in-place, setting Properties["anti_pattern"]
// = "n_plus_1" on every Relationship whose FromID is involved in a finding
// and whose Kind is CALLS or QUERIES. Returns the count of annotated edges.
//
// This is the "stamp the graph" step that makes N+1 findings visible to
// downstream consumers (MCP agents, the web dashboard) without requiring
// them to call DetectNPlusOne independently.
func AnnotateDocument(doc *Document, report *NPlusOneReport) int {
	if doc == nil || report == nil || len(report.Findings) == 0 {
		return 0
	}

	// Build a set of (callerID, queryID) pairs for fast lookup.
	type pair struct{ from, to string }
	flagged := make(map[pair]bool, len(report.Findings))
	for _, f := range report.Findings {
		flagged[pair{f.CallerEntityID, f.QueryEntityID}] = true
		// Also flag the query entity's direct parent as a loop context.
		if f.LoopEntityID != "" {
			flagged[pair{f.LoopEntityID, f.QueryEntityID}] = true
		}
	}

	count := 0
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != "CALLS" && r.Kind != "QUERIES" {
			continue
		}
		if !flagged[pair{r.FromID, r.ToID}] {
			continue
		}
		if r.PropLen() == 0 {
			r.PropsReplace(make(map[string]string, 2))
		}
		r.PropSet("anti_pattern", "n_plus_1")
		count++
	}

	// Also annotate the query entities themselves.
	queryIDs := make(map[string]bool, len(report.Findings))
	for _, f := range report.Findings {
		queryIDs[f.QueryEntityID] = true
	}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if !queryIDs[e.ID] {
			continue
		}
		if e.PropLen() == 0 {
			e.PropsReplace(make(map[string]string, 2))
		}
		e.PropSet("anti_pattern", "n_plus_1")
	}

	return count
}

// SummariseFindingsText returns a compact human-readable summary of an
// NPlusOneReport, suitable for terminal output.
func SummariseFindingsText(r *NPlusOneReport) string {
	if r == nil || len(r.Findings) == 0 {
		return "N+1 detector: no findings (scanned " +
			fmt.Sprintf("%d entities, %d relationships", r.EntitiesScanned, r.RelationshipsScanned) + ")"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "N+1 detector: %d finding(s) (scanned %d entities, %d relationships)\n",
		len(r.Findings), r.EntitiesScanned, r.RelationshipsScanned)
	for i, f := range r.Findings {
		loc := f.QueryFile
		if f.QueryLine > 0 {
			loc = fmt.Sprintf("%s:%d", f.QueryFile, f.QueryLine)
		}
		orm := f.ORM
		if orm == "" {
			orm = "unknown ORM"
		}
		fmt.Fprintf(&sb, "  [%d] %s — ORM call %q at %s (%s)\n",
			i+1, f.CallerName, f.QueryName, loc, orm)
		fmt.Fprintf(&sb, "       Suggestion: %s\n", f.Suggestion)
	}
	return sb.String()
}
