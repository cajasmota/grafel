package dashboard

// handlers_graphql.go — GraphQL resolver-effects surface (#4255, epic #4249).
//
// Route:
//
//	GET /api/graphql/{group}  — GraphQL resolvers grouped by schema type,
//	                            each with its operation verb, effects, auth,
//	                            and source ref.
//
// What the graph genuinely models (verified against internal/custom/*/*.go
// and internal/extractors/graphql/graphql.go):
//
//   - Every GraphQL framework (gqlgen / DGS / spring-graphql / pothos /
//     type-graphql / async-graphql / graphql-php / lighthouse / caliban /
//     graphql-kotlin / graphene / ariadne / strawberry) emits a canonical
//     GraphQL ENDPOINT entity carrying Properties:
//       http_method="GRAPHQL", verb="GRAPHQL",
//       graphql_operation = "query"|"mutation"|"subscription",
//       graphql_root      = the owning SDL object type / resolver class,
//       graphql_field     = the resolved field name,
//       resolver_method   = the underlying handler method,
//       framework         = the framework label.
//     This `verb=GRAPHQL` endpoint is the framework-agnostic resolver signal
//     we key on (the dedicated `graphql_resolver` subtype is Java/DGS-only).
//
//   - EFFECTS are stamped onto the endpoint entity by the link effect-
//     propagation pass (internal/links/effect_propagation.go) using the keys
//     in links.EffectPropertyKey* — "effects" (comma-joined db_read/db_write/
//     http_out/…), "effect_confidence" ("<eff>=<f>,…"), "effect_source"
//     ("endpoint"|"direct"|"transitive"|"pure"). Read directly off
//     entity.Properties, matching handlers_security.go. HONEST LIMIT: effect
//     properties only populate when the link effect pass has run for the
//     group; absent props ⇒ effects omitted (not fabricated).
//
//   - AUTH, when resolver-level auth is statically recoverable, is stamped on
//     the same endpoint as the flat auth contract (auth_required / auth_roles
//     / auth_method / auth_confidence). Omitted when not modeled.
//
//   - SCHEMA TYPES (SDL) are extracted separately as Kind="SCOPE.Schema"
//     entities (subtype type/interface/enum/union/input/scalar) by
//     internal/extractors/graphql/graphql.go. We surface a cheap roll-up of
//     the SDL object/interface types present in the group as context.
//
// Follows the same load pattern as handlers_security.go: prefer the cached
// group graph, fall back to a direct per-repo load; iterate entities via the
// mmap reader when available. Raw-JSON envelope (no v2 {ok,data}), matching
// the sibling /api/security/* and /api/groups/* routes.

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	fb "github.com/cajasmota/archigraph/internal/graph/fbgraph"
	"github.com/cajasmota/archigraph/internal/graph/fbreader"
	"github.com/cajasmota/archigraph/internal/links"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes — mirror webui-v2/src/data/types.ts (GraphQL surface)
// ─────────────────────────────────────────────────────────────────────────────

// GraphQLEffect is one classified effect on a resolver.
type GraphQLEffect struct {
	// Name is the canonical effect token, e.g. "db_read", "db_write",
	// "http_out", "mutation".
	Name string `json:"name"`
	// Confidence is the 0..1 score for this effect when the propagation pass
	// recorded one. Omitted (0) when unknown.
	Confidence float64 `json:"confidence,omitempty"`
}

// GraphQLResolver is one resolved GraphQL field resolver.
type GraphQLResolver struct {
	EntityID string `json:"entity_id"`
	Repo     string `json:"repo"`
	// Field is the GraphQL field this resolver answers (e.g. "users").
	Field string `json:"field"`
	// ParentType is the owning SDL object type / resolver class (graphql_root).
	ParentType string `json:"parent_type"`
	// Operation is "query" | "mutation" | "subscription" (graphql_operation).
	Operation string `json:"operation"`
	// Framework is the emitting GraphQL framework label.
	Framework string `json:"framework,omitempty"`
	// Method is the underlying handler method (resolver_method), when known.
	Method     string `json:"method,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`

	// Effects — read from the link effect-propagation props. Empty slice when
	// the effect pass has not run / no sinks were detected.
	Effects      []GraphQLEffect `json:"effects"`
	EffectSource string          `json:"effect_source,omitempty"`

	// Auth — populated only when resolver-level auth is statically modeled.
	AuthRequired bool     `json:"auth_required,omitempty"`
	AuthRoles    []string `json:"auth_roles,omitempty"`
	AuthMethod   string   `json:"auth_method,omitempty"`
}

// GraphQLTypeGroup is the resolvers grouped under one parent SDL type, split
// by operation root where relevant.
type GraphQLTypeGroup struct {
	// ParentType is the grouping key — the SDL object type / resolver class.
	ParentType string            `json:"parent_type"`
	Resolvers  []GraphQLResolver `json:"resolvers"`
}

// GraphQLSchemaType is one SDL type definition surfaced as schema context.
type GraphQLSchemaType struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"` // "type"|"interface"|"enum"|"union"|"input"|"scalar"
	Repo       string `json:"repo"`
	SourceFile string `json:"source_file,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	Federated  bool   `json:"federated,omitempty"`
}

// GraphQLReport is the wire shape for GET /api/graphql/{group}.
type GraphQLReport struct {
	Group string `json:"group"`
	// Totals.
	TotalResolvers     int `json:"total_resolvers"`
	TotalTypes         int `json:"total_types"`
	QueryCount         int `json:"query_count"`
	MutationCount      int `json:"mutation_count"`
	SubscriptionCount  int `json:"subscription_count"`
	WithEffectsCount   int `json:"with_effects_count"`
	WithAuthCount      int `json:"with_auth_count"`
	ResolversWithDBOps int `json:"resolvers_with_db_ops"`

	// Frameworks observed across the group's resolvers.
	Frameworks []string `json:"frameworks"`

	// Groups — resolvers grouped by parent SDL type.
	Groups []GraphQLTypeGroup `json:"groups"`

	// SchemaTypes — SDL object/interface/enum/… type roll-up (context).
	SchemaTypes []GraphQLSchemaType `json:"schema_types"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: GET /api/graphql/{group}
// ─────────────────────────────────────────────────────────────────────────────

// isGraphQLEndpoint reports whether an entity is a GraphQL resolver endpoint.
// The framework-agnostic signal is verb/http_method = "GRAPHQL"; the Java/DGS
// dedicated subtype "graphql_resolver" is also accepted.
func isGraphQLEndpoint(subtype string, props map[string]string) bool {
	if subtype == "graphql_resolver" {
		return true
	}
	if strings.EqualFold(props["verb"], "GRAPHQL") || strings.EqualFold(props["http_method"], "GRAPHQL") {
		return true
	}
	return false
}

// parseEffectConfidences decodes the "<eff>=<float>,…" form stamped onto
// entity.Properties[effect_confidence] by stampEffectProperties.
func parseEffectConfidences(raw string) map[string]float64 {
	out := map[string]float64{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq <= 0 || eq == len(part)-1 {
			continue
		}
		var v float64
		if _, err := fmt.Sscanf(part[eq+1:], "%f", &v); err != nil {
			continue
		}
		out[part[:eq]] = v
	}
	return out
}

func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
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
	filterOp := strings.ToLower(q.Get("operation")) // query|mutation|subscription
	filterFile := q.Get("file")
	onlyEffects := q.Get("only_effects") == "true"

	report := GraphQLReport{Group: groupName}

	// Group resolvers by parent SDL type; preserve insertion order for stable
	// output via a parallel ordered key slice.
	byType := map[string]*GraphQLTypeGroup{}
	var typeOrder []string
	frameworks := map[string]bool{}

	// S8 (#2159): prefer the cached group; fall back to a direct load.
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

		iterEntities := func(visit func(id, name, kind, subtype, sourceFile string, startLine int, props map[string]string)) {
			if rdr != nil {
				rdr.IterateEntities(func(e *fb.Entity) bool {
					props := make(map[string]string, e.PropertiesLength())
					var pe fb.PropertyEntry
					for i := 0; i < e.PropertiesLength(); i++ {
						if e.Properties(&pe, i) {
							props[string(pe.Key())] = string(pe.Value())
						}
					}
					visit(string(e.Id()), string(e.Name()), string(e.Kind()), string(e.Subtype()), string(e.SourceFile()), int(e.SourceLine()), props)
					return true
				})
				return
			}
			for i := range doc.Entities {
				ent := &doc.Entities[i]
				visit(ent.ID, ent.Name, ent.Kind, ent.Subtype, ent.SourceFile, ent.StartLine, ent.Properties)
			}
		}

		iterEntities(func(id, name, kind, subtype, sourceFile string, startLine int, props map[string]string) {
			// SDL schema types (context roll-up).
			if kind == "SCOPE.Schema" && props["language"] == "graphql" {
				switch subtype {
				case "type", "interface", "enum", "union", "input", "scalar":
					report.SchemaTypes = append(report.SchemaTypes, GraphQLSchemaType{
						Name:       name,
						Kind:       subtype,
						Repo:       rp.Slug,
						SourceFile: sourceFile,
						StartLine:  startLine,
						Federated:  props["federated"] == "true",
					})
				}
				return
			}

			// GraphQL resolver endpoints.
			if !isGraphQLEndpoint(subtype, props) {
				return
			}

			operation := strings.ToLower(props["graphql_operation"])
			parent := firstNonEmpty(props["graphql_root"], props["resolver_class"], "(root)")
			field := firstNonEmpty(props["graphql_field"], name)

			if filterOp != "" && operation != filterOp {
				return
			}
			if filterFile != "" && !strings.Contains(sourceFile, filterFile) {
				return
			}

			// Effects — read directly off the propagation props (links pkg keys).
			var effects []GraphQLEffect
			rawEffs := props[links.EffectPropertyKeyList]
			confs := parseEffectConfidences(props[links.EffectPropertyKeyConfidence])
			for _, e := range strings.Split(rawEffs, ",") {
				e = strings.TrimSpace(e)
				if e == "" {
					continue
				}
				effects = append(effects, GraphQLEffect{Name: e, Confidence: confs[e]})
			}
			sort.SliceStable(effects, func(i, j int) bool { return effects[i].Name < effects[j].Name })

			if onlyEffects && len(effects) == 0 {
				return
			}

			res := GraphQLResolver{
				EntityID:     rp.Slug + "/" + id,
				Repo:         rp.Slug,
				Field:        field,
				ParentType:   parent,
				Operation:    operation,
				Framework:    props["framework"],
				Method:       props["resolver_method"],
				SourceFile:   sourceFile,
				StartLine:    startLine,
				Effects:      effects,
				EffectSource: props[links.EffectPropertyKeySource],
			}

			// Auth — only when statically modeled.
			if props["auth_required"] == "true" {
				res.AuthRequired = true
				res.AuthMethod = props["auth_method"]
				if roles := strings.TrimSpace(props["auth_roles"]); roles != "" {
					for _, rr := range strings.Split(roles, ",") {
						if rr = strings.TrimSpace(rr); rr != "" {
							res.AuthRoles = append(res.AuthRoles, rr)
						}
					}
				}
				report.WithAuthCount++
			}

			// Totals.
			report.TotalResolvers++
			switch operation {
			case "query":
				report.QueryCount++
			case "mutation":
				report.MutationCount++
			case "subscription":
				report.SubscriptionCount++
			}
			if len(effects) > 0 {
				report.WithEffectsCount++
			}
			for _, e := range effects {
				if strings.HasPrefix(e.Name, "db_") {
					report.ResolversWithDBOps++
					break
				}
			}
			if fw := props["framework"]; fw != "" {
				frameworks[fw] = true
			}

			g := byType[parent]
			if g == nil {
				g = &GraphQLTypeGroup{ParentType: parent}
				byType[parent] = g
				typeOrder = append(typeOrder, parent)
			}
			g.Resolvers = append(g.Resolvers, res)
		})
	}

	report.TotalTypes = len(report.SchemaTypes)

	// Assemble groups in stable order: by resolver count desc, then name.
	sort.SliceStable(typeOrder, func(i, j int) bool {
		gi, gj := byType[typeOrder[i]], byType[typeOrder[j]]
		if len(gi.Resolvers) != len(gj.Resolvers) {
			return len(gi.Resolvers) > len(gj.Resolvers)
		}
		return typeOrder[i] < typeOrder[j]
	})
	for _, k := range typeOrder {
		g := byType[k]
		sort.SliceStable(g.Resolvers, func(i, j int) bool {
			if g.Resolvers[i].Operation != g.Resolvers[j].Operation {
				return g.Resolvers[i].Operation < g.Resolvers[j].Operation
			}
			return g.Resolvers[i].Field < g.Resolvers[j].Field
		})
		report.Groups = append(report.Groups, *g)
	}

	for fw := range frameworks {
		report.Frameworks = append(report.Frameworks, fw)
	}
	sort.Strings(report.Frameworks)

	sort.SliceStable(report.SchemaTypes, func(i, j int) bool {
		if report.SchemaTypes[i].Kind != report.SchemaTypes[j].Kind {
			return report.SchemaTypes[i].Kind < report.SchemaTypes[j].Kind
		}
		return report.SchemaTypes[i].Name < report.SchemaTypes[j].Name
	})

	writeReportJSON(w, report)
}

// firstNonEmpty returns the first non-empty string argument, or "" if none.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
