// Package analytics computes, in ONE pass over one open graph, every number
// the daemon's post-rebuild metrics batch needs from a repo.
//
// #5954. The batch used to walk each repo's graph FOUR times, each walk
// materialising the whole corpus onto the Go heap:
//
//  1. audit.AuditPath           → graph.LoadGraphFromDir + a map[string]*Entity
//  2. graph.ComputeCoverage + graph.FindImportCycles → a second LoadGraphFromDir
//  3. countAuthUncoveredEndpoints → a third LoadGraphFromDir
//  4. buildAgentsMapStats       → a fourth LoadGraphFromDir
//
// graph.LoadGraphFromDir is the full-materialise loader: internal/graph/load.go
// puts a 325 MB graph.fb at roughly 608 MB of heap. Doing that four times per
// repo, in a goroutine that took no stage-gate token and was invisible to the
// scheduler's busy accounting, put a repeat of the largest object in the daemon
// alongside the very children the epic had just forked out of it.
//
// Every one of those numbers is a counting scan. So this package does not build
// a Document at all: it opens the graph once as an mmap-backed
// fbreader.GraphView and streams the rows. Entities are decoded one at a time
// and dropped; what survives a scan is id-keyed sets, not []Entity.
//
// The one structure that genuinely needs adjacency is import-cycle detection
// (Tarjan). It gets an id→id adjacency map — not a slice of materialised rows.
//
// ScanDocument is the reference implementation: it computes the same struct
// from a *graph.Document using the ORIGINAL functions (audit's rules,
// graph.ComputeCoverage, graph.FindImportCycles). It is the fallback for repos
// with no readable graph.fb, and it is the oracle the streaming path is tested
// against.
package analytics

import (
	"errors"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/quality/audit"
)

// RepoScan is the complete set of per-repo numbers the post-rebuild analytics
// batch consumes. It is a plain comparable struct so a test can assert the
// streaming and materialised paths agree on ALL of it with one ==.
type RepoScan struct {
	// Audit-derived (health-history orphan rate and bug rate).
	Entities        int
	Orphans         int
	ImportsTotal    int
	ImportsResolved int // hex + ext_qualified — the "good imports" numerator

	// Coverage-derived.
	TotalProduction   int
	CoveredProduction int

	// Import cycles.
	Cycles int

	// Kind tallies.
	Endpoints int // http_endpoint, http_endpoint_definition, http_endpoint_call
	Flows     int // process / SCOPE.Process / SCOPE.Process.*

	// Endpoints with no HAS_AUTH / REQUIRES_AUTH / auth edge.
	AuthUncovered int
}

// KindTally is the much smaller scan the AGENTS.md architecture-map injection
// needs. It is a separate entry point because it runs at a different time (per
// repo, inside the index loop) and would otherwise pay for coverage and cycle
// detection it never reads.
type KindTally struct {
	Entities      int
	Relationships int
	HTTPEndpoints int
	Queues        int
	Topics        int
	ProcessFlows  int
}

// graphView is the read surface a scan needs. Aliased so tests can substitute
// the opener without importing fbreader.
type graphView = fbreader.GraphView

// errNoView is returned by the opener when the repo has no mmap-readable
// graph (graph.json only, or a format version this binary cannot read). The
// caller then falls back to the Document path.
var errNoView = errors.New("analytics: no mmap-readable graph")

// openGraphView opens the repo's active graph as a streaming view. Indirected
// for tests, which assert a whole batch shares ONE open.
var openGraphView = func(stateDir string) (graphView, error) {
	v, err := graph.ReaderForDir(stateDir)
	if err != nil {
		return nil, err
	}
	// Apply the same format-version gate LoadGraphFromDir applies. Without it a
	// graph too old for this binary would be scanned as if it were current,
	// where the Document path correctly refuses and the caller skips the repo.
	if required, _ := graph.ReindexRequiredReason(stateDir); required {
		_ = v.Close()
		return nil, errNoView
	}
	return v, nil
}

// loadDocument is the full-materialise fallback. Indirected for tests, which
// assert the streaming path never reaches it.
var loadDocument = func(stateDir string) (*graph.Document, error) {
	return graph.LoadGraphFromDir(stateDir)
}

// ScanRepo computes every analytics number for one repo from a SINGLE open of
// its graph. It never materialises a graph.Document unless the repo has no
// mmap-readable graph at all.
func ScanRepo(stateDir string) (RepoScan, error) {
	v, err := openGraphView(stateDir)
	if err != nil {
		doc, lerr := loadDocument(stateDir)
		if lerr != nil {
			return RepoScan{}, lerr
		}
		return ScanDocument(doc), nil
	}
	defer v.Close()
	return scanView(v), nil
}

// TallyRepo computes the AGENTS.md map tally from a single open of the graph.
func TallyRepo(stateDir string) (KindTally, error) {
	v, err := openGraphView(stateDir)
	if err != nil {
		doc, lerr := loadDocument(stateDir)
		if lerr != nil {
			return KindTally{}, lerr
		}
		return TallyDocument(doc), nil
	}
	defer v.Close()

	t := KindTally{Entities: v.EntityCount(), Relationships: v.RelationshipCount()}
	v.IterateEntities(func(e *fb.Entity) bool {
		if e != nil {
			t.addKind(string(e.Kind()))
		}
		return true
	})
	return t, nil
}

func (t *KindTally) addKind(kind string) {
	switch kind {
	case "http_endpoint", "http_endpoint_definition", "http_endpoint_call":
		t.HTTPEndpoints++
	case "queue":
		t.Queues++
	case "topic", "pubsub_topic":
		t.Topics++
	}
	if strings.HasPrefix(kind, "SCOPE.Process") || kind == "process" {
		t.ProcessFlows++
	}
}

// TallyDocument is the materialised reference for TallyRepo.
func TallyDocument(doc *graph.Document) KindTally {
	t := KindTally{Entities: doc.Stats.Entities, Relationships: doc.Stats.Relationships}
	for i := range doc.Entities {
		t.addKind(doc.Entities[i].Kind)
	}
	return t
}

// scanView is the streaming implementation. Three iterations over ONE open
// mmap — cheap page reads, no heap retention of rows — replace four full
// materialisations.
func scanView(v graphView) RepoScan {
	var s RepoScan

	// ── entity pass: ids, kind tallies ───────────────────────────────────────
	// entityIDs is deduplicated exactly as audit's entByID map is, so a graph
	// carrying a duplicate id produces the same orphan count as before.
	entityIDs := make(map[string]struct{}, v.EntityCount())
	v.IterateEntities(func(e *fb.Entity) bool {
		if e == nil {
			return true
		}
		s.Entities++
		entityIDs[string(e.Id())] = struct{}{}
		countKinds(&s, string(e.Kind()))
		return true
	})

	// ── relationship pass: connectivity, imports, auth ───────────────────────
	touched := make(map[string]struct{}, len(entityIDs))
	authed := make(map[string]struct{}, 16)
	v.IterateRelationships(func(rel *fb.Relationship) bool {
		if rel == nil {
			return true
		}
		from := string(rel.FromId())
		to := string(rel.ToId())
		kind := string(rel.Kind())
		accumulateRel(&s, touched, authed, kind, from, to)
		return true
	})

	// ── orphans: entities with no non-structural edge ────────────────────────
	for id := range entityIDs {
		if _, ok := touched[id]; !ok {
			s.Orphans++
		}
	}

	// ── auth-uncovered endpoints: a second entity pass over the same mmap ────
	v.IterateEntities(func(e *fb.Entity) bool {
		if e == nil {
			return true
		}
		if isAuthScopedEndpointKind(string(e.Kind())) {
			if _, ok := authed[string(e.Id())]; !ok {
				s.AuthUncovered++
			}
		}
		return true
	})

	s.TotalProduction, s.CoveredProduction = graph.CoverageTotalsFromView(v)
	s.Cycles = graph.CountImportCyclesFromView(v)
	return s
}

// ScanDocument is the materialised reference implementation. It delegates every
// derived number to the function the daemon used to call, so it is the oracle
// the streaming path is proven against — and the correct path for a repo with
// only a graph.json.
func ScanDocument(doc *graph.Document) RepoScan {
	var s RepoScan

	entityIDs := make(map[string]struct{}, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		s.Entities++
		entityIDs[e.ID] = struct{}{}
		countKinds(&s, e.Kind)
	}

	touched := make(map[string]struct{}, len(doc.Entities))
	authed := make(map[string]struct{}, 16)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		accumulateRel(&s, touched, authed, r.Kind, r.FromID, r.ToID)
	}
	for id := range entityIDs {
		if _, ok := touched[id]; !ok {
			s.Orphans++
		}
	}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if isAuthScopedEndpointKind(e.Kind) {
			if _, ok := authed[e.ID]; !ok {
				s.AuthUncovered++
			}
		}
	}

	cov := graph.ComputeCoverage(doc)
	s.TotalProduction = cov.TotalProduction
	s.CoveredProduction = cov.CoveredProduction
	s.Cycles = len(graph.FindImportCycles(doc.Entities, doc.Relationships, nil))
	return s
}

// countKinds applies the endpoint and process-flow kind rules the health entry
// has always used.
func countKinds(s *RepoScan, kind string) {
	switch kind {
	case "http_endpoint", "http_endpoint_definition", "http_endpoint_call":
		s.Endpoints++
	}
	if isProcessFlowKind(kind) {
		s.Flows++
	}
}

// accumulateRel folds one edge into the connectivity, IMPORTS-hygiene and auth
// accumulators. Shared by both paths so the two cannot diverge on edge rules.
func accumulateRel(s *RepoScan, touched, authed map[string]struct{}, kind, from, to string) {
	upper := strings.ToUpper(kind)
	// Orphan rule (#1597, via audit): a node linked only by CONTAINS/DECLARES
	// is still an orphan.
	if !audit.IsStructuralRelKind(upper) {
		touched[from] = struct{}{}
		touched[to] = struct{}{}
	}
	if upper == "IMPORTS" {
		s.ImportsTotal++
		switch audit.ClassifyImportToID(to) {
		case audit.ImportFormatHex, audit.ImportFormatExtQualified:
			s.ImportsResolved++
		}
	}
	// The auth check matches on the RAW kind — "auth" is lowercase by design in
	// the original countAuthUncoveredEndpoints and must stay case-sensitive.
	switch kind {
	case "HAS_AUTH", "REQUIRES_AUTH", "auth":
		authed[from] = struct{}{}
	}
}

// isProcessFlowKind mirrors the health entry's process-flow rule.
func isProcessFlowKind(kind string) bool {
	switch kind {
	case "process", "SCOPE.Process":
		return true
	}
	return len(kind) > 14 && kind[:14] == "SCOPE.Process."
}

// isAuthScopedEndpointKind is the endpoint set the auth-coverage metric counts.
// Deliberately NARROWER than the endpoint tally above: http_endpoint_call is a
// client-side call site, not something that can carry an auth annotation.
func isAuthScopedEndpointKind(kind string) bool {
	switch kind {
	case "http_endpoint", "http_endpoint_definition":
		return true
	}
	return false
}
