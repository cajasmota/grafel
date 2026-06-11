package dashboard

// handlers_errorflow.go — group-level Error-flow surface (#4267, epic #4249).
//
// Route:
//
//	GET /api/errorflow/{group}  — exception types modelled in the group, each
//	                              listing the functions/endpoints that THROW it
//	                              and the handlers that CATCH it, with file refs
//	                              and an honest uncaught flag.
//
// What the graph genuinely models (verified against the shared exception-flow
// emitter internal/extractor/exception_flow.go and the per-language extractors
// internal/extractors/{python,golang,rust,elixir,...}/exception_flow.go, plus
// the per-endpoint reader internal/mcp/endpoint_posture.go::resolveErrorFlow):
//
//   - One SCOPE.ExceptionType / subtype="exception_type" node per distinct
//     (normalized, unqualified) type name, carried on a SYNTHETIC constant
//     SourceFile ("<exception>") so identical type names converge — across
//     files AND languages — to a SINGLE node. Its Name is "exception:<Type>"
//     (e.g. "exception:ValidationError"); we strip that prefix for display.
//
//   - THROWS and CATCHES are the two error-flow edge kinds
//     (types.RelationshipKindThrows / RelationshipKindCatches). The direction
//     is uniform across every emitter: the EDGE ORIGINATES AT THE CALLABLE and
//     points at the exception-type node —
//       THROWS  : raising function/method/endpoint → SCOPE.ExceptionType
//       CATCHES : handling function/method         → SCOPE.ExceptionType
//     Edge Properties carry `exception_type` (the bare type name) and, when the
//     detector recorded it, `pattern` (throw_new / raise / instanceof / …).
//
//   - Precision-first / honest-partial: only TYPED throws/catches are recorded.
//     Bare `except:` / untyped `catch(e){}` / anonymous inline errors emit NO
//     edge. So CATCHES edges are GENUINELY SPARSE in many repos.
//
// The per-endpoint Paths posture (#4263) already shows a single endpoint's
// throws/catches. This route is the GROUP-LEVEL ROLLUP, inverted around the
// exception type: for each exception, who can raise it and who handles it, so
// you can spot the type that is thrown in five places and caught nowhere in the
// indexed graph.
//
// HONESTY on "uncaught": a CATCHES edge is only emitted for a TYPED catch the
// indexer could see. An exception with THROWS but no CATCHES edge anywhere in
// the group therefore means "no typed catcher found in the indexed graph" —
// which may be a genuinely uncaught throw OR a throw caught by an untyped /
// out-of-scope / cross-repo handler the indexer did not model. We label that
// state honestly as `uncaught` with `uncaught_reason` = "no_catcher_in_graph"
// and NEVER assert it as fact in prose; the client renders it as a cautious
// warning, not a hard error. Exceptions with ≥1 CATCHES edge are `caught`.
// Exception nodes that are only ever CAUGHT (no THROWS in graph) are surfaced
// too but never flagged uncaught. A repo with no exception modelling yields an
// empty `exceptions` list and a clean empty state on the client.

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	fb "github.com/cajasmota/archigraph/internal/graph/fbgraph"
	"github.com/cajasmota/archigraph/internal/graph/fbreader"
	"github.com/cajasmota/archigraph/internal/types"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes — mirror webui-v2/src/data/types.ts (Error-flow surface)
// ─────────────────────────────────────────────────────────────────────────────

// ErrorFlowSite is one callable that throws or catches an exception type (the
// FromID side of a THROWS / CATCHES edge), resolved to an entity where possible.
type ErrorFlowSite struct {
	// EntityID is the repo-qualified entity ID when the callable resolves to a
	// collected entity, else the raw edge FromID.
	EntityID string `json:"entity_id"`
	// Name is the callable's display name (entity name, else the raw key tail).
	Name string `json:"name"`
	// Kind is the callable entity Kind when resolved (SCOPE.Function /
	// SCOPE.Method / SCOPE.Endpoint / …), else empty.
	Kind       string `json:"kind,omitempty"`
	Repo       string `json:"repo,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	// Pattern is the detector label the edge recorded (throw_new / raise /
	// instanceof / errors_is / …) when present.
	Pattern string `json:"pattern,omitempty"`
}

// ErrorFlowException is one exception type with everything that throws and
// catches it across the group.
type ErrorFlowException struct {
	// Type is the bare exception type name (no "exception:" prefix), e.g.
	// "ValidationError".
	Type string `json:"type"`
	// Throwers are the callables that can raise this type (THROWS edges).
	Throwers []ErrorFlowSite `json:"throwers"`
	// Catchers are the handlers that catch this type (CATCHES edges).
	Catchers []ErrorFlowSite `json:"catchers"`
	// Uncaught is true when this type is thrown at least once but has NO
	// CATCHES edge anywhere in the indexed group. See UncaughtReason —
	// honestly this means "no typed catcher in graph", not a proven leak.
	Uncaught bool `json:"uncaught"`
	// UncaughtReason qualifies an uncaught flag so the client never over-claims:
	// "no_catcher_in_graph" — thrown but no typed CATCHES edge was indexed
	// (may be genuinely uncaught OR caught by an untyped / out-of-scope handler).
	UncaughtReason string `json:"uncaught_reason,omitempty"`
	// ThrowCount / CatchCount are the resolved edge counts.
	ThrowCount int `json:"throw_count"`
	CatchCount int `json:"catch_count"`
}

// ErrorFlowReport is the wire shape for GET /api/errorflow/{group}.
type ErrorFlowReport struct {
	Group string `json:"group"`

	// Totals.
	TotalExceptions int `json:"total_exceptions"` // exception types with ≥1 throw or catch
	TotalUncaught   int `json:"total_uncaught"`   // thrown-but-no-catcher-in-graph
	TotalThrows     int `json:"total_throws"`     // THROWS edges resolved
	TotalCatches    int `json:"total_catches"`    // CATCHES edges resolved

	// Exceptions — uncaught first (most throwers first within), then caught.
	Exceptions []ErrorFlowException `json:"exceptions"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Pure fold — testable without a live server
// ─────────────────────────────────────────────────────────────────────────────

// efEntMeta is the resolved-entity metadata the fold uses to turn a raw THROWS /
// CATCHES endpoint ID into a named, file-referenced thrower/catcher, and to turn
// an exception-type node ID into its bare type name.
type efEntMeta struct {
	name, kind, sourceFile, repoSlug string
	startLine                        int
}

// efEdge is one THROWS or CATCHES edge: callable (FromID) → exception-type node
// (ToID) plus the edge's properties (exception_type / pattern).
type efEdge struct {
	fromID, toID string
	catch        bool
	props        map[string]string
}

// efAccum accumulates the fold across repos before assembly.
type efAccum struct {
	// entByID resolves any entity ID → its metadata (callables AND exception
	// nodes; exception nodes carry the synthetic "<exception>" SourceFile).
	entByID map[string]efEntMeta
	// excByKey keyed by bare type name; throwers/catchers de-duped per type.
	excByKey   map[string]*efExcAccum
	excOrder   []string // insertion order, for deterministic assembly
	totalThrow int
	totalCatch int
}

type efExcAccum struct {
	exc         *ErrorFlowException
	seenThrower map[string]struct{}
	seenCatcher map[string]struct{}
	throwers    []ErrorFlowSite
	catchers    []ErrorFlowSite
}

func newEFAccum() *efAccum {
	return &efAccum{
		entByID:  map[string]efEntMeta{},
		excByKey: map[string]*efExcAccum{},
	}
}

// exceptionTypeKey resolves a THROWS/CATCHES edge target to the bare exception
// type name. Prefers the resolved exception node's Name (stripped of the
// "exception:" prefix); falls back to the edge's `exception_type` property, then
// the raw target tail (also prefix-stripped). Never invents a name.
func (a *efAccum) exceptionTypeKey(toID string, props map[string]string) string {
	if meta, ok := a.entByID[toID]; ok && meta.name != "" {
		return strings.TrimPrefix(meta.name, "exception:")
	}
	if t := strings.TrimSpace(props["exception_type"]); t != "" {
		return t
	}
	return strings.TrimPrefix(idTail(toID), "exception:")
}

// siteFor builds an ErrorFlowSite for a callable edge endpoint, resolving entity
// metadata where possible and falling back to the raw key tail otherwise.
func (a *efAccum) siteFor(fromID string, props map[string]string) ErrorFlowSite {
	site := ErrorFlowSite{
		EntityID: fromID,
		Name:     idTail(fromID),
		Pattern:  strings.TrimSpace(props["pattern"]),
	}
	if meta, ok := a.entByID[fromID]; ok {
		if meta.name != "" {
			site.Name = meta.name
		}
		site.EntityID = meta.repoSlug + "/" + fromID
		site.Kind = meta.kind
		site.Repo = meta.repoSlug
		site.SourceFile = meta.sourceFile
		site.StartLine = meta.startLine
	}
	return site
}

// addEdge folds one THROWS / CATCHES edge into the accumulator.
func (a *efAccum) addEdge(e efEdge) {
	typeName := a.exceptionTypeKey(e.toID, e.props)
	if typeName == "" {
		return
	}

	acc := a.excByKey[typeName]
	if acc == nil {
		acc = &efExcAccum{
			exc:         &ErrorFlowException{Type: typeName, Throwers: []ErrorFlowSite{}, Catchers: []ErrorFlowSite{}},
			seenThrower: map[string]struct{}{},
			seenCatcher: map[string]struct{}{},
		}
		a.excByKey[typeName] = acc
		a.excOrder = append(a.excOrder, typeName)
	}

	if e.catch {
		if _, dup := acc.seenCatcher[e.fromID]; dup {
			return
		}
		acc.seenCatcher[e.fromID] = struct{}{}
		acc.catchers = append(acc.catchers, a.siteFor(e.fromID, e.props))
		a.totalCatch++
		return
	}
	if _, dup := acc.seenThrower[e.fromID]; dup {
		return
	}
	acc.seenThrower[e.fromID] = struct{}{}
	acc.throwers = append(acc.throwers, a.siteFor(e.fromID, e.props))
	a.totalThrow++
}

// assemble produces the final report: exceptions with the highest-signal
// (uncaught, then widest thrower fan-out) first, totals, and stable ordering.
func (a *efAccum) assemble(group string) ErrorFlowReport {
	report := ErrorFlowReport{
		Group:        group,
		TotalThrows:  a.totalThrow,
		TotalCatches: a.totalCatch,
		Exceptions:   []ErrorFlowException{},
	}

	for _, typeName := range a.excOrder {
		acc := a.excByKey[typeName]
		sortSites(acc.throwers)
		sortSites(acc.catchers)
		acc.exc.Throwers = acc.throwers
		acc.exc.Catchers = acc.catchers
		acc.exc.ThrowCount = len(acc.throwers)
		acc.exc.CatchCount = len(acc.catchers)

		// Honest uncaught: thrown at least once, but no typed catcher anywhere
		// in the indexed graph. NOT asserted as a proven leak.
		if acc.exc.ThrowCount > 0 && acc.exc.CatchCount == 0 {
			acc.exc.Uncaught = true
			acc.exc.UncaughtReason = "no_catcher_in_graph"
			report.TotalUncaught++
		}

		report.TotalExceptions++
		report.Exceptions = append(report.Exceptions, *acc.exc)
	}

	sort.SliceStable(report.Exceptions, func(i, j int) bool {
		ei, ej := report.Exceptions[i], report.Exceptions[j]
		// Uncaught (thrown-but-unhandled) first — the actionable signal.
		if ei.Uncaught != ej.Uncaught {
			return ei.Uncaught
		}
		// Then widest thrower fan-out.
		if ei.ThrowCount != ej.ThrowCount {
			return ei.ThrowCount > ej.ThrowCount
		}
		if ei.CatchCount != ej.CatchCount {
			return ei.CatchCount > ej.CatchCount
		}
		return ei.Type < ej.Type
	})

	return report
}

// sortSites orders thrower/catcher sites deterministically: named first, then by
// name, repo, file/line, entity id.
func sortSites(sites []ErrorFlowSite) {
	sort.SliceStable(sites, func(i, j int) bool {
		if sites[i].Name != sites[j].Name {
			return sites[i].Name < sites[j].Name
		}
		if sites[i].Repo != sites[j].Repo {
			return sites[i].Repo < sites[j].Repo
		}
		if sites[i].SourceFile != sites[j].SourceFile {
			return sites[i].SourceFile < sites[j].SourceFile
		}
		if sites[i].StartLine != sites[j].StartLine {
			return sites[i].StartLine < sites[j].StartLine
		}
		return sites[i].EntityID < sites[j].EntityID
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: GET /api/errorflow/{group}
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleErrorFlow(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	if groupName == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	repoPaths, err := repoPathsForGroup(groupName)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q: %v", groupName, err))
		return
	}
	if len(repoPaths) == 0 {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q has no repos", groupName))
		return
	}

	acc := newEFAccum()

	cachedGrp, _ := s.graphs.GetGroupCached(groupName)

	throws := string(types.RelationshipKindThrows)
	catches := string(types.RelationshipKindCatches)

	for _, rp := range repoPaths {
		var doc *graph.Document
		var rdr *fbreader.Reader
		if cachedGrp != nil {
			if dr, ok := cachedGrp.Repos[rp.Slug]; ok && dr != nil {
				doc = dr.Doc
				rdr = dr.Reader
			}
		}
		if doc == nil && rdr == nil {
			stateDir := daemon.StateDirForRepo(rp.Path)
			var loadErr error
			doc, loadErr = graph.LoadGraphFromDir(stateDir)
			if loadErr != nil {
				continue
			}
		}

		iterEntities := func(visit func(id, name, kind, sourceFile string, startLine int)) {
			if rdr != nil {
				rdr.IterateEntities(func(e *fb.Entity) bool {
					visit(string(e.Id()), string(e.Name()), string(e.Kind()), string(e.SourceFile()), int(e.SourceLine()))
					return true
				})
				return
			}
			for i := range doc.Entities {
				ent := &doc.Entities[i]
				visit(ent.ID, ent.Name, ent.Kind, ent.SourceFile, ent.StartLine)
			}
		}

		iterRelationships := func(visit func(fromID, toID, kind string, props map[string]string)) {
			if rdr != nil {
				rdr.IterateRelationships(func(rel *fb.Relationship) bool {
					props := make(map[string]string, rel.PropertiesLength())
					var pe fb.PropertyEntry
					for i := 0; i < rel.PropertiesLength(); i++ {
						if rel.Properties(&pe, i) {
							props[string(pe.Key())] = string(pe.Value())
						}
					}
					visit(string(rel.FromId()), string(rel.ToId()), string(rel.Kind()), props)
					return true
				})
				return
			}
			for i := range doc.Relationships {
				rl := &doc.Relationships[i]
				visit(rl.FromID, rl.ToID, rl.Kind, rl.Properties)
			}
		}

		// Pass 1: index every entity so THROWS/CATCHES callable endpoints and
		// the exception-type target nodes resolve to a real name / Kind / ref.
		iterEntities(func(id, name, kind, sourceFile string, startLine int) {
			acc.entByID[id] = efEntMeta{name: name, kind: kind, sourceFile: sourceFile, repoSlug: rp.Slug, startLine: startLine}
		})

		// Pass 2: fold THROWS / CATCHES edges → per-exception thrower/catcher map.
		iterRelationships(func(fromID, toID, kind string, props map[string]string) {
			switch kind {
			case throws:
				acc.addEdge(efEdge{fromID: fromID, toID: toID, catch: false, props: props})
			case catches:
				acc.addEdge(efEdge{fromID: fromID, toID: toID, catch: true, props: props})
			}
		})
	}

	report := acc.assemble(groupName)

	writeReportJSON(w, report)
}
