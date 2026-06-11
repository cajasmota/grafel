package dashboard

// handlers_di.go — Dependency-Injection surface (#4266, epic #4249).
//
// Route:
//
//	GET /api/di/{group}  — DI providers grouped by framework, each provider
//	                       listing the consumers it is INJECTED_INTO, with file
//	                       refs and the DI framework.
//
// What the graph genuinely models (verified against the INJECTED_INTO emitters:
// internal/extractors/javascript/angular.go, internal/custom/javascript/
// nestjs_di.go, internal/custom/java/di_graph.go, internal/custom/python/
// di_graph.go, internal/custom/golang/di_graph.go, internal/custom/php/
// di_graph.go, internal/custom/csharp/dotnet_di.go, and the engine framework
// rules under internal/engine/rules/*/frameworks/*.yaml):
//
//   - INJECTED_INTO is the ONE DI edge kind (types.RelationshipKindInjectedInto).
//     The convention is uniform across every emitter: provider INJECTED_INTO
//     consumer — i.e. FromID is the provider/service/token that gets injected,
//     ToID is the consumer (controller / other service / handler) that declares
//     it as a constructor or field dependency.
//
//   - Edge Properties carry, by convention, `provider`, `consumer` and
//     `framework` (nestjs / angular / spring / micronaut / quarkus / guice /
//     fastapi / dependency-injector / asp_net_mvc / …), and sometimes `via`
//     (constructor / field / param) and `qualifier` (Spring @Qualifier / DI
//     token). We surface the framework, via and qualifier when present.
//
// The force-graph already renders INJECTED_INTO edges (PR #4252/#4260), but a
// force-graph is a poor surface for "which providers inject into which
// consumers": you cannot scan a provider's full consumer fan-out, nor group by
// framework. This dedicated route walks the graph for INJECTED_INTO edges and
// returns a provider→consumers map grouped by framework — a clean, scannable DI
// map. Mirrors handlers_iac.go / handlers_graphql.go exactly: prefer the cached
// group graph, fall back to a direct per-repo load; iterate entities AND
// relationships via the mmap reader when available; raw-JSON envelope.
//
// HONESTY: only genuine INJECTED_INTO edges are surfaced. Endpoints that resolve
// to a collected entity get its real name + file ref; unresolved endpoints fall
// back to the edge's `provider`/`consumer` property or the raw key tail — never
// an invented name. A repo with no DI yields an empty `groups` and a clean empty
// state on the client.

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
// Wire shapes — mirror webui-v2/src/data/types.ts (DI surface)
// ─────────────────────────────────────────────────────────────────────────────

// DIConsumer is one consumer a provider is injected into (the ToID side of an
// INJECTED_INTO edge), resolved to an entity where possible.
type DIConsumer struct {
	// EntityID is the repo-qualified entity ID when the consumer resolves to a
	// collected entity, else the raw edge ToID.
	EntityID string `json:"entity_id"`
	// Name is the consumer's display name (entity name, or the edge `consumer`
	// property, or the raw key tail).
	Name string `json:"name"`
	// Kind is the consumer entity Kind when resolved (e.g. SCOPE.Controller,
	// SCOPE.Service, SCOPE.Class), else empty.
	Kind       string `json:"kind,omitempty"`
	Repo       string `json:"repo,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	// Via is the injection mechanism when the edge records it (constructor /
	// field / param).
	Via string `json:"via,omitempty"`
	// Qualifier is the DI qualifier / token disambiguator when present.
	Qualifier string `json:"qualifier,omitempty"`
}

// DIProvider is one injectable provider (the FromID side of INJECTED_INTO edges)
// together with every consumer it injects into.
type DIProvider struct {
	// EntityID is the repo-qualified entity ID when the provider resolves to a
	// collected entity, else the raw edge FromID.
	EntityID string `json:"entity_id"`
	// Name is the provider's display name.
	Name string `json:"name"`
	// Kind is the provider entity Kind when resolved, else empty.
	Kind       string `json:"kind,omitempty"`
	Repo       string `json:"repo,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	// Framework is the DI framework this provider's injections belong to.
	Framework string `json:"framework,omitempty"`
	// Consumers are the consumers this provider is INJECTED_INTO (≥1).
	Consumers []DIConsumer `json:"consumers"`
}

// DIFrameworkGroup is the providers observed under one DI framework.
type DIFrameworkGroup struct {
	Framework string       `json:"framework"`
	Count     int          `json:"count"`
	Providers []DIProvider `json:"providers"`
}

// DIReport is the wire shape for GET /api/di/{group}.
type DIReport struct {
	Group string `json:"group"`

	// Totals.
	TotalProviders  int `json:"total_providers"`
	TotalConsumers  int `json:"total_consumers"`
	TotalInjections int `json:"total_injections"`

	// Frameworks observed, sorted.
	Frameworks []string `json:"frameworks"`

	// Groups — providers grouped by framework, frameworks by provider count desc.
	Groups []DIFrameworkGroup `json:"groups"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Pure fold — testable without a live server
// ─────────────────────────────────────────────────────────────────────────────

// diEntMeta is the resolved-entity metadata the fold uses to turn a raw
// INJECTED_INTO endpoint ID into a named, file-referenced provider/consumer.
type diEntMeta struct {
	name, kind, sourceFile, repoSlug string
	startLine                        int
}

// diEdge is one INJECTED_INTO edge: provider (FromID) → consumer (ToID) plus the
// edge's properties (provider/consumer/framework/via/qualifier).
type diEdge struct {
	fromID, toID string
	props        map[string]string
}

// diAccum accumulates the fold across repos before assembly.
type diAccum struct {
	// entByID resolves an endpoint ID → its entity metadata.
	entByID map[string]diEntMeta
	// providers keyed by framework+providerID; consumers de-duped per provider.
	provByKey       map[string]*diProvAccum
	provOrder       []string // insertion order, for deterministic assembly
	frameworks      map[string]bool
	totalInjections int
}

type diProvAccum struct {
	prov      *DIProvider
	seenCons  map[string]struct{}
	consumers []DIConsumer
}

func newDIAccum() *diAccum {
	return &diAccum{
		entByID:    map[string]diEntMeta{},
		provByKey:  map[string]*diProvAccum{},
		frameworks: map[string]bool{},
	}
}

// addEdge folds one INJECTED_INTO edge into the accumulator. filterFramework, if
// non-empty (already lower-cased), restricts to edges of that framework.
func (a *diAccum) addEdge(e diEdge, filterFramework string) {
	framework := strings.TrimSpace(e.props["framework"])
	if filterFramework != "" && strings.ToLower(framework) != filterFramework {
		return
	}

	provMeta, provResolved := a.entByID[e.fromID]
	consMeta, consResolved := a.entByID[e.toID]

	providerName := e.props["provider"]
	if provResolved && provMeta.name != "" {
		providerName = provMeta.name
	}
	if providerName == "" {
		providerName = idTail(e.fromID)
	}
	consumerName := e.props["consumer"]
	if consResolved && consMeta.name != "" {
		consumerName = consMeta.name
	}
	if consumerName == "" {
		consumerName = idTail(e.toID)
	}

	key := framework + "\x00" + e.fromID
	acc := a.provByKey[key]
	if acc == nil {
		prov := &DIProvider{
			EntityID:  e.fromID,
			Name:      providerName,
			Framework: framework,
			Consumers: []DIConsumer{},
		}
		if provResolved {
			prov.EntityID = provMeta.repoSlug + "/" + e.fromID
			prov.Kind = provMeta.kind
			prov.Repo = provMeta.repoSlug
			prov.SourceFile = provMeta.sourceFile
			prov.StartLine = provMeta.startLine
		}
		acc = &diProvAccum{prov: prov, seenCons: map[string]struct{}{}}
		a.provByKey[key] = acc
		a.provOrder = append(a.provOrder, key)
	}

	// De-duplicate consumers per provider (multiple edges can land the same
	// provider→consumer pair across passes/repos).
	if _, dup := acc.seenCons[e.toID]; dup {
		return
	}
	acc.seenCons[e.toID] = struct{}{}

	cons := DIConsumer{
		EntityID:  e.toID,
		Name:      consumerName,
		Via:       strings.TrimSpace(e.props["via"]),
		Qualifier: strings.TrimSpace(e.props["qualifier"]),
	}
	if consResolved {
		cons.EntityID = consMeta.repoSlug + "/" + e.toID
		cons.Kind = consMeta.kind
		cons.Repo = consMeta.repoSlug
		cons.SourceFile = consMeta.sourceFile
		cons.StartLine = consMeta.startLine
	}
	acc.consumers = append(acc.consumers, cons)

	if framework != "" {
		a.frameworks[framework] = true
	}
	a.totalInjections++
}

// assemble produces the final report: providers grouped by framework (DI hubs
// first), totals, and the sorted framework list.
func (a *diAccum) assemble(group string) DIReport {
	report := DIReport{Group: group, TotalInjections: a.totalInjections}

	byFramework := map[string]*DIFrameworkGroup{}
	distinctConsumers := map[string]struct{}{}
	for _, key := range a.provOrder {
		acc := a.provByKey[key]
		sort.SliceStable(acc.consumers, func(i, j int) bool {
			if acc.consumers[i].Name != acc.consumers[j].Name {
				return acc.consumers[i].Name < acc.consumers[j].Name
			}
			return acc.consumers[i].EntityID < acc.consumers[j].EntityID
		})
		acc.prov.Consumers = acc.consumers
		for _, c := range acc.consumers {
			distinctConsumers[c.EntityID] = struct{}{}
		}
		report.TotalProviders++

		fw := acc.prov.Framework
		g := byFramework[fw]
		if g == nil {
			g = &DIFrameworkGroup{Framework: fw}
			byFramework[fw] = g
		}
		g.Providers = append(g.Providers, *acc.prov)
	}
	report.TotalConsumers = len(distinctConsumers)

	for _, g := range byFramework {
		g.Count = len(g.Providers)
		sort.SliceStable(g.Providers, func(i, j int) bool {
			// Providers with more consumers first (the DI hubs), then by name.
			if len(g.Providers[i].Consumers) != len(g.Providers[j].Consumers) {
				return len(g.Providers[i].Consumers) > len(g.Providers[j].Consumers)
			}
			return g.Providers[i].Name < g.Providers[j].Name
		})
		report.Groups = append(report.Groups, *g)
	}
	sort.SliceStable(report.Groups, func(i, j int) bool {
		if report.Groups[i].Count != report.Groups[j].Count {
			return report.Groups[i].Count > report.Groups[j].Count
		}
		return report.Groups[i].Framework < report.Groups[j].Framework
	})

	for f := range a.frameworks {
		report.Frameworks = append(report.Frameworks, f)
	}
	sort.Strings(report.Frameworks)

	return report
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: GET /api/di/{group}
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleDI(w http.ResponseWriter, r *http.Request) {
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

	q := r.URL.Query()
	filterFramework := strings.ToLower(strings.TrimSpace(q.Get("framework")))

	acc := newDIAccum()

	cachedGrp, _ := s.graphs.GetGroupCached(groupName)

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

		// Pass 1: index every entity so INJECTED_INTO endpoints resolve to a
		// real name / Kind / file ref.
		iterEntities(func(id, name, kind, sourceFile string, startLine int) {
			acc.entByID[id] = diEntMeta{name: name, kind: kind, sourceFile: sourceFile, repoSlug: rp.Slug, startLine: startLine}
		})

		injectedInto := string(types.RelationshipKindInjectedInto)

		// Pass 2: fold INJECTED_INTO edges → provider→consumers map.
		iterRelationships(func(fromID, toID, kind string, props map[string]string) {
			if kind != injectedInto {
				return
			}
			acc.addEdge(diEdge{fromID: fromID, toID: toID, props: props}, filterFramework)
		})
	}

	report := acc.assemble(groupName)

	writeReportJSON(w, report)
}
