package dashboard

// handlers_iac.go — IaC / Infrastructure surface (#4256, epic #4249).
//
// Route:
//
//	GET /api/iac/{group}  — IaC resources grouped by tool (and by stack/module
//	                        where topology exists), each with its typed config
//	                        properties and its relationships (IAM grants,
//	                        event-source wiring, dependencies, containment,
//	                        outputs/exports, cross-stack refs).
//
// What the graph genuinely models (verified against internal/engine/cdk_edges.go,
// pulumi_edges.go, kubernetes_edges.go, iac_cloudformation_edges.go,
// serverless_*_edges.go, internal/extractors/hcl/*, internal/extractors/bicep/*,
// and internal/types/iac_resource_category.go):
//
//   - RESOURCES. Every IaC tool emits a resource entity carrying an `iac_tool`
//     property — aws-cdk / pulumi / bicep / cloudformation / sam /
//     serverless-framework — EXCEPT Terraform/OpenTofu, whose HCL extractor keeps
//     Kind=SCOPE.Component subtype=resource and stamps resource_type /
//     resource_category into Metadata (not Properties) with Language=terraform|hcl.
//     We therefore classify a resource as IaC when it has an `iac_tool` property,
//     OR when it is a SCOPE.Component/resource emitted by the terraform/hcl
//     language. CDK/Pulumi/Bicep use Kind=SCOPE.InfraResource; CloudFormation/SAM
//     derive the semantic Kind (Datastore/Queue/ServerlessFunction/InfraResource)
//     from the shared classifier via types.IaCKindForCategory.
//
//   - resource_category is the ONE cross-tool join key (types.IaCResourceCategory):
//     datastore/queue/topic/stream/function/cache/secret/network/compute/storage/
//     other. Present in Properties for CDK/Pulumi/Bicep/CFN. HONEST LIMIT: for
//     Terraform it lives in Metadata, which the dashboard reader does not expose,
//     so we recompute it from the resource_type the entity name encodes.
//
//   - TYPED CONFIG PROPERTIES. The epic-#4194 passes stamp a curated, bounded
//     allow-list of literal scalar attributes onto the resource entity Properties
//     (instance_type/machine_type/sku/tier/memory_size/timeout/runtime/engine/
//     version/count/replicas/port/protocol/allocated_storage/…). We surface
//     whatever curated config keys are present, excluding the structural keys the
//     emit pass sets (iac_tool/construct_type/resource_type/resource_category/…).
//
//   - RELATIONSHIPS. The session's edge passes emit, between resource entities:
//       DEPENDS_ON  reason=grant        grant=<method>   (IAM grant: grantee→res)
//       DEPENDS_ON  reason=event_source                  (fn→event-source res)
//       DEPENDS_ON  reason=props_ref / (none)            (plain dependency)
//       USES / IMPORTS / CONTAINS / JOINS                (stack/app/module topology)
//       TRIGGERS / SERVES / ROUTES_TO / SUBSCRIBES_TO    (serverless/SAM wiring)
//     CloudFormation outputs/exports surface as SCOPE.Config / SCOPE.Schema
//     entities with export_name + cross_stack/nested_stack edge props. We attach
//     each edge to its source resource as a typed relationship facet, and tag the
//     facet kind from the edge's reason/props so the UI can render grants vs
//     event-sources vs plain dependencies distinctly.
//
// Follows handlers_graphql.go / handlers_security.go exactly: prefer the cached
// group graph, fall back to a direct per-repo load; iterate entities AND
// relationships via the mmap reader when available; raw-JSON envelope.

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
// Wire shapes — mirror webui-v2/src/data/types.ts (IaC surface)
// ─────────────────────────────────────────────────────────────────────────────

// IaCProperty is one stamped typed config property on a resource.
type IaCProperty struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// IaCRelation is one relationship facet attached to a resource: an IAM grant,
// an event-source wiring, a plain dependency, a topology containment, a
// serverless trigger/route, or an output/export reference.
type IaCRelation struct {
	// Facet is the UI-facing relationship class derived from the edge kind +
	// reason: "grant" | "event_source" | "dependency" | "topology" |
	// "trigger" | "output".
	Facet string `json:"facet"`
	// Kind is the raw graph edge kind (DEPENDS_ON / USES / IMPORTS / CONTAINS /
	// JOINS / TRIGGERS / SERVES / ROUTES_TO / SUBSCRIBES_TO).
	Kind string `json:"kind"`
	// Direction is "out" (this resource → target) or "in" (source → this
	// resource), relative to the resource the relation is attached to.
	Direction string `json:"direction"`
	// Target is the human-readable name (logical id) of the other endpoint.
	Target string `json:"target"`
	// TargetResolved is true when Target is a graph-resolved entity name, false
	// when it is a fallback derived from the raw entity id (the endpoint was not
	// found among the indexed entities). The UI uses this to render a friendlier
	// label + an id tooltip instead of a meaningless hash. (#4495)
	TargetResolved bool `json:"target_resolved"`
	// TargetID is the raw graph entity id of the other endpoint, always set so
	// the UI can show it on hover regardless of resolution. (#4495)
	TargetID string `json:"target_id"`
	// TargetEntityID is the slug-prefixed entity id (`<repo>/<rawId>`) of the
	// other endpoint WHEN that endpoint is itself a rendered IaC resource node;
	// empty otherwise. This is the graph-joinable key the architecture-diagram
	// view (#4526) uses to draw an edge between two rendered resource nodes —
	// IaCResource.EntityID carries the same slug prefix, whereas TargetID does
	// not, so the raw id alone cannot be joined client-side.
	TargetEntityID string `json:"target_entity_id,omitempty"`
	// Detail is the grant method (for grants) or other edge qualifier, when set.
	Detail string `json:"detail,omitempty"`
}

// IaCResource is one extracted IaC resource.
type IaCResource struct {
	EntityID string `json:"entity_id"`
	Repo     string `json:"repo"`
	// ModulePath is the monorepo module sub-path owning this resource's source
	// file (#4698), derived from the repo's configured module roots. Distinct
	// from Module below (a source-dir grouping heuristic): ModulePath is the
	// scope-selector key. Empty for single-repo groups / files under no root.
	ModulePath string `json:"module_path,omitempty"`
	// Name is the logical id / resource name.
	Name string `json:"name"`
	// Tool is the normalized iac_tool (aws-cdk/pulumi/bicep/cloudformation/sam/
	// serverless-framework/terraform).
	Tool string `json:"tool"`
	// ResourceType is the tool-native type string (construct_type / resource_type).
	ResourceType string `json:"resource_type,omitempty"`
	// Category is the cross-tool resource_category join key.
	Category string `json:"category,omitempty"`
	// LogicalID, when distinct from Name (CDK/CFN).
	LogicalID  string `json:"logical_id,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	// Module is the grouping key for the architecture diagram (#4526): the
	// module / construct / stack the resource belongs to, derived from the
	// source-file directory (e.g. `infra/terraform/modules/network`). A
	// modularized stack flattens to many resources sharing a Module, which the
	// diagram renders as a grouped container. Empty when no source path is known.
	Module string `json:"module,omitempty"`

	// Env is the environment a resource belongs to (#4657): for a module
	// INSTANCE it is derived from the instance file's path (envs/<env>/…); for a
	// definition resource it is propagated from the instance(s) that instantiate
	// the definition this resource lives in. Drives the env tabs. Empty for
	// env-less resources (shared definitions not yet linked to any env).
	Env string `json:"env,omitempty"`

	// DefinitionDir, for a module INSTANCE only (#4657): the repo-relative
	// directory of the module DEFINITION this instance instantiates
	// (e.g. `modules/worker-service`). The diagram joins it to the definition's
	// resources (those whose Module == DefinitionDir) to project them into the
	// env and draw the INSTANTIATES edge between rendered nodes.
	DefinitionDir string `json:"definition_dir,omitempty"`

	// ModuleSource, for a module INSTANCE only (#4657): the raw `source` value
	// (e.g. `../../modules/worker-service`, or a registry/git source).
	ModuleSource string `json:"module_source,omitempty"`

	// Properties — curated typed config props (empty when none stamped).
	Properties []IaCProperty `json:"properties"`
	// Relations — grants / event-sources / dependencies / topology / triggers /
	// outputs touching this resource (empty when none).
	Relations []IaCRelation `json:"relations"`
}

// IaCToolGroup is the resources grouped under one iac_tool.
type IaCToolGroup struct {
	Tool      string        `json:"tool"`
	Count     int           `json:"count"`
	Resources []IaCResource `json:"resources"`
}

// IaCReport is the wire shape for GET /api/iac/{group}.
type IaCReport struct {
	Group string `json:"group"`

	// Totals.
	TotalResources    int `json:"total_resources"`
	TotalGrants       int `json:"total_grants"`
	TotalEventSources int `json:"total_event_sources"`
	TotalDependencies int `json:"total_dependencies"`
	TotalOutputs      int `json:"total_outputs"`
	WithPropsCount    int `json:"with_props_count"`

	// Tools observed.
	Tools []string `json:"tools"`
	// Envs observed across module instances (#4657), sorted; drives the env
	// tabs in the architecture view. Empty when no env-scoped stacks exist.
	Envs []string `json:"envs"`
	// CountsByCategory — resource_category → count across all tools.
	CountsByCategory map[string]int `json:"counts_by_category"`

	// Groups — resources grouped by iac_tool, tools by resource count desc.
	Groups []IaCToolGroup `json:"groups"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Resource classification helpers
// ─────────────────────────────────────────────────────────────────────────────

// iacStructuralPropKeys are the keys the emit passes set for bookkeeping; they
// are NOT user-facing config and are excluded from the surfaced Properties.
var iacStructuralPropKeys = map[string]struct{}{
	"iac_tool":          {},
	"construct_type":    {},
	"resource_type":     {},
	"resource_category": {},
	"resource_scope":    {},
	"logical_id":        {},
	"pattern_type":      {},
	"synthesis":         {},
	"language":          {},
	"provider":          {},
	"function_name":     {},
	"cfn_section":       {},
	"export_name":       {},
	"deployment_role":   {},
	"reason":            {},
	"join":              {},
	// #4657 module-instantiation bookkeeping — surfaced as dedicated
	// IaCResource fields (Env / DefinitionDir / ModuleSource), not config props.
	"env":            {},
	"definition_dir": {},
	"module_source":  {},
}

// iacResourceKinds are the entity Kinds an IaC resource can carry. CFN derives
// the semantic Kind from the shared classifier; CDK/Pulumi/Bicep use
// InfraResource; Terraform/HCL uses Component (filtered by subtype below).
var iacResourceKinds = map[string]bool{
	"SCOPE.InfraResource":      true,
	"SCOPE.Datastore":          true,
	"SCOPE.Queue":              true,
	"SCOPE.ServerlessFunction": true,
	"SCOPE.Component":          true,
}

// iacToolForEntity returns the normalized iac_tool for an entity and whether it
// is an IaC resource at all. Resources with an explicit iac_tool property are
// authoritative; Terraform/OpenTofu resources have no iac_tool property, so we
// recognise them by Kind=SCOPE.Component / subtype=resource emitted under the
// terraform|hcl language.
func iacToolForEntity(kind, subtype, language string, props map[string]string) (string, bool) {
	if t := strings.TrimSpace(props["iac_tool"]); t != "" {
		return t, true
	}
	if kind == "SCOPE.Component" &&
		(language == "terraform" || language == "hcl") {
		// Issue #4625 — render `module` instances as diagram nodes too, not just
		// `resource` blocks. A resource that consumes another module's output
		// (module.<m>.<out>) draws a cross-module USES edge whose target is the
		// module block; without rendering the module node that edge would surface
		// only as an unresolved relation + a disconnected box. Modules are the
		// natural cloud-architecture aggregate node for the child stack.
		if subtype == "resource" || subtype == "module" {
			return "terraform", true
		}
	}
	return "", false
}

// iacResourceTypeOf returns the tool-native resource type string. CDK/Pulumi use
// construct_type; CFN/HCL/Bicep use resource_type. Terraform encodes it in the
// entity name as `<type>.<name>` — we recover the leading type segment.
func iacResourceTypeOf(name string, props map[string]string) string {
	if t := strings.TrimSpace(props["construct_type"]); t != "" {
		return t
	}
	if t := strings.TrimSpace(props["resource_type"]); t != "" {
		return t
	}
	// Issue #4625 — a module instance is named `module.<name>`; report its
	// type as "module" so the diagram can render it as a child-stack aggregate.
	if strings.HasPrefix(name, "module.") {
		return "module"
	}
	// Terraform self-ref form: `aws_db_instance.main`.
	if i := strings.IndexByte(name, '.'); i > 0 {
		return name[:i]
	}
	return ""
}

// iacCategoryOf returns the cross-tool resource_category. When present in
// Properties (CDK/Pulumi/Bicep/CFN) we use it directly; otherwise we recompute
// from the resource type via the ONE shared classifier (covers Terraform, whose
// category lives in Metadata the dashboard reader does not expose).
func iacCategoryOf(resourceType string, props map[string]string) string {
	if c := strings.TrimSpace(props["resource_category"]); c != "" {
		return c
	}
	if c := strings.TrimSpace(props["resource_scope"]); c != "" {
		return c
	}
	// Issue #4625 — a Terraform module instance is its own diagram category so
	// the cloud-tier grouping can place child-stack aggregates distinctly.
	if resourceType == "module" {
		return "module"
	}
	if resourceType != "" {
		return types.IaCResourceCategory(resourceType)
	}
	return ""
}

// iacRelationFacet maps a raw edge (kind + properties) to the UI relation facet
// and a human detail string.
func iacRelationFacet(kind string, props map[string]string) (facet, detail string) {
	reason := props["reason"]
	switch {
	case reason == "grant":
		return "grant", props["grant"]
	case reason == "event_source":
		return "event_source", ""
	}
	// Issue #4625 — cross-module output reference (module.<m>.<out>) carries a
	// derived semantic verb (consumes / redrive / logs-to / assumes / grants /
	// reads). Surface it as the facet so the diagram renders the edge with its
	// cloud-architecture meaning; the consumed output is the detail.
	if props["dataflow"] == "cross_module" {
		if sem := strings.TrimSpace(props["semantic"]); sem != "" && sem != "dependency" {
			detail := strings.TrimSpace(props["module_output"])
			return sem, detail
		}
		return "dependency", strings.TrimSpace(props["module_output"])
	}
	// Issue #4657 — module instantiation: an env stack's module instance
	// instantiates a module definition. Surfaced as its own facet so the
	// diagram draws the env→definition link distinctly from plain dependencies.
	if kind == "INSTANTIATES" {
		return "instantiates", strings.TrimSpace(props["definition_dir"])
	}
	switch kind {
	case "TRIGGERS", "SERVES", "ROUTES_TO", "SUBSCRIBES_TO":
		// serverless / SAM wiring. Surface the most descriptive qualifier.
		for _, k := range []string{"trigger", "http_method", "route_path", "schedule"} {
			if v := strings.TrimSpace(props[k]); v != "" {
				return "trigger", v
			}
		}
		return "trigger", ""
	case "CONTAINS", "JOINS", "IMPORTS":
		return "topology", strings.TrimSpace(props["join"])
	}
	// DEPENDS_ON / USES without a special reason ⇒ plain dependency.
	if reason == "props_ref" {
		return "dependency", props["props_ref"]
	}
	return "dependency", ""
}

// iacIsOutputEntity reports whether an entity is a CloudFormation/Terraform
// output/export surfaced as a Config/Schema entity, so the report can count and
// (later) relate them. CFN stamps export_name; HCL marks subtype=output.
func iacIsOutputEntity(kind, subtype string, props map[string]string) bool {
	if props["export_name"] != "" {
		return true
	}
	if kind == "SCOPE.Schema" && subtype == "output" {
		return true
	}
	return false
}

// iacModuleOf derives the architecture-diagram grouping key (#4526) for a
// resource from its source-file path: the containing directory, which for a
// modularized stack is the module / construct / stack root (e.g.
// `infra/terraform/modules/network/main.tf` → `infra/terraform/modules/network`).
// Falls back to the repo slug when there is no directory component, and to ""
// when no source file is known.
func iacModuleOf(slug, sourceFile string) string {
	sf := strings.TrimSpace(sourceFile)
	if sf == "" {
		return ""
	}
	sf = strings.ReplaceAll(sf, "\\", "/")
	if i := strings.LastIndexByte(sf, '/'); i > 0 {
		return sf[:i]
	}
	// No directory component — the file sits at the repo root.
	if slug != "" {
		return slug
	}
	return "(root)"
}

// splitEnv splits a (possibly comma-joined) env field into its individual env
// names, trimming blanks. A module definition instantiated by several envs
// carries a comma-joined Env (e.g. "dev,prod"); a plain instance carries one.
func splitEnv(env string) []string {
	env = strings.TrimSpace(env)
	if env == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(env, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// mergeEnv adds one env to a comma-joined env list, keeping it sorted and
// de-duplicated. Used when a shared module definition is instantiated by more
// than one environment (#4657).
func mergeEnv(existing, add string) string {
	set := map[string]bool{}
	for _, e := range splitEnv(existing) {
		set[e] = true
	}
	for _, e := range splitEnv(add) {
		set[e] = true
	}
	out := make([]string, 0, len(set))
	for e := range set {
		out = append(out, e)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// idTail returns the last path segment of a graph entity ID (Kind:Name or
// repo/Kind:Name), used as a readable relation target when no entity name is
// resolvable.
func idTail(id string) string {
	if i := strings.LastIndexByte(id, ':'); i >= 0 && i+1 < len(id) {
		return id[i+1:]
	}
	return id
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: GET /api/iac/{group}
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleIaC(w http.ResponseWriter, r *http.Request) {
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
	filterTool := strings.ToLower(strings.TrimSpace(q.Get("tool")))
	filterCategory := strings.ToLower(strings.TrimSpace(q.Get("category")))

	report := IaCReport{
		Group:            groupName,
		CountsByCategory: map[string]int{},
	}

	// #4698 — module roots per repo so each resource can carry its module_path.
	moduleRoots := moduleRootsByRepo(repoPaths)

	// resources keyed by entity ID so relationship attachment is O(1).
	byID := map[string]*IaCResource{}
	// resource display name by entity ID, for resolving relation targets.
	nameByID := map[string]string{}
	tools := map[string]bool{}

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

		iterEntities := func(visit func(id, name, kind, subtype, sourceFile, language string, startLine int, props map[string]string)) {
			if rdr != nil {
				rdr.IterateEntities(func(e *fb.Entity) bool {
					props := make(map[string]string, e.PropertiesLength())
					var pe fb.PropertyEntry
					for i := 0; i < e.PropertiesLength(); i++ {
						if e.Properties(&pe, i) {
							props[string(pe.Key())] = string(pe.Value())
						}
					}
					visit(string(e.Id()), string(e.Name()), string(e.Kind()), string(e.Subtype()), string(e.SourceFile()), string(e.Language()), int(e.SourceLine()), props)
					return true
				})
				return
			}
			for i := range doc.Entities {
				ent := &doc.Entities[i]
				visit(ent.ID, ent.Name, ent.Kind, ent.Subtype, ent.SourceFile, ent.Language, ent.StartLine, ent.Properties)
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

		// Pass 1: collect IaC resource entities (+ count outputs).
		iterEntities(func(id, name, kind, subtype, sourceFile, language string, startLine int, props map[string]string) {
			// Index a readable name for EVERY entity, not just collected IaC
			// resources, so relation targets resolve to a display name even when
			// the target endpoint is not itself rendered as a resource row
			// (e.g. a Terraform variable, an output, or a resource excluded by
			// the tool/category filter). #4495: without this, such targets fall
			// back to idTail() — which surfaces a raw entity-id hash to the user.
			if name != "" {
				nameByID[id] = name
			}
			if iacIsOutputEntity(kind, subtype, props) {
				report.TotalOutputs++
				// outputs are counted but not rendered as standalone rows.
			}
			if !iacResourceKinds[kind] {
				return
			}
			tool, ok := iacToolForEntity(kind, subtype, language, props)
			if !ok {
				return
			}
			rtype := iacResourceTypeOf(name, props)
			category := iacCategoryOf(rtype, props)

			if filterTool != "" && strings.ToLower(tool) != filterTool {
				return
			}
			if filterCategory != "" && strings.ToLower(category) != filterCategory {
				return
			}

			var cfgProps []IaCProperty
			for k, v := range props {
				if _, structural := iacStructuralPropKeys[k]; structural {
					continue
				}
				if v == "" {
					continue
				}
				cfgProps = append(cfgProps, IaCProperty{Key: k, Value: v})
			}
			sort.SliceStable(cfgProps, func(i, j int) bool { return cfgProps[i].Key < cfgProps[j].Key })

			res := &IaCResource{
				EntityID:      rp.Slug + "/" + id,
				Repo:          rp.Slug,
				ModulePath:    modulePathFor(rp.Slug, sourceFile, moduleRoots),
				Name:          name,
				Tool:          tool,
				ResourceType:  rtype,
				Category:      category,
				LogicalID:     props["logical_id"],
				SourceFile:    sourceFile,
				StartLine:     startLine,
				Module:        iacModuleOf(rp.Slug, sourceFile),
				Env:           strings.TrimSpace(props["env"]),
				DefinitionDir: strings.TrimSpace(props["definition_dir"]),
				ModuleSource:  strings.TrimSpace(props["module_source"]),
				Properties:    cfgProps,
				Relations:     []IaCRelation{},
			}
			byID[id] = res
			nameByID[id] = name
			tools[tool] = true
			if category != "" {
				report.CountsByCategory[category]++
			}
			if len(cfgProps) > 0 {
				report.WithPropsCount++
			}
			report.TotalResources++
		})

		// Pass 2: attach relationships whose endpoints are collected resources.
		iterRelationships(func(fromID, toID, kind string, props map[string]string) {
			fromRes := byID[fromID]
			toRes := byID[toID]
			if fromRes == nil && toRes == nil {
				return
			}
			facet, detail := iacRelationFacet(kind, props)

			// targetName resolves an endpoint id to a display name. resolved is
			// false when no indexed entity name was found and we fell back to a
			// segment of the raw id (#4495).
			targetName := func(id string) (name string, resolved bool) {
				if n, ok := nameByID[id]; ok && n != "" {
					return n, true
				}
				return idTail(id), false
			}

			// joinableID returns the slug-prefixed entity id of the other endpoint
			// when it is itself a collected (rendered) resource, so the diagram can
			// draw an edge between two rendered nodes (#4526); "" otherwise.
			joinableID := func(other *IaCResource) string {
				if other != nil {
					return other.EntityID
				}
				return ""
			}

			if fromRes != nil {
				name, resolved := targetName(toID)
				fromRes.Relations = append(fromRes.Relations, IaCRelation{
					Facet:          facet,
					Kind:           kind,
					Direction:      "out",
					Target:         name,
					TargetResolved: resolved,
					TargetID:       toID,
					TargetEntityID: joinableID(toRes),
					Detail:         detail,
				})
			}
			if toRes != nil && toRes != fromRes {
				name, resolved := targetName(fromID)
				toRes.Relations = append(toRes.Relations, IaCRelation{
					Facet:          facet,
					Kind:           kind,
					Direction:      "in",
					Target:         name,
					TargetResolved: resolved,
					TargetID:       fromID,
					TargetEntityID: joinableID(fromRes),
					Detail:         detail,
				})
			}

			// Totals — count once per edge (on the "out" side semantics).
			switch facet {
			case "grant":
				report.TotalGrants++
			case "event_source":
				report.TotalEventSources++
			case "dependency":
				report.TotalDependencies++
			}
		})

		// Pass 3 (#4657) — resolve module instantiation by DIRECTORY. The
		// INSTANTIATES edge from Pass 2 targets `tfmodule-def:<dir>`, which is
		// not itself a rendered resource (a Terraform module definition is a
		// directory of .tf files, not one entity). Here we join each module
		// INSTANCE's DefinitionDir to the definition's resources — every
		// resource whose Module (source-file directory) equals that dir — and:
		//
		//   1. draw an INSTANTIATES edge between the instance node and each of
		//      the definition's resources (so dev/staging/prod connect to the
		//      worker-service / sqs-queue definitions as rendered nodes), and
		//   2. propagate the instance's env onto those definition resources, so
		//      the env tabs can scope to "this env's instances + what they
		//      instantiate" even though the shared definition files carry no env.
		//
		// This is the directory-join the resolver cannot do (no entity to bind
		// a directory to) and is what clears the bulk of the unresolved relations.
		defByDir := map[string][]*IaCResource{}
		for _, r := range byID {
			if r.Repo != rp.Slug || r.Module == "" {
				continue
			}
			defByDir[r.Module] = append(defByDir[r.Module], r)
		}
		for _, inst := range byID {
			if inst.Repo != rp.Slug || inst.DefinitionDir == "" {
				continue
			}
			defs := defByDir[inst.DefinitionDir]
			if len(defs) == 0 {
				continue
			}
			for _, def := range defs {
				if def == inst {
					continue
				}
				// Propagate env onto the (env-less) shared definition resource so
				// it surfaces under the instantiating env's tab. A definition
				// instantiated by multiple envs accumulates a comma-joined list.
				if inst.Env != "" {
					def.Env = mergeEnv(def.Env, inst.Env)
				}
				inst.Relations = append(inst.Relations, IaCRelation{
					Facet:          "instantiates",
					Kind:           "INSTANTIATES",
					Direction:      "out",
					Target:         def.Name,
					TargetResolved: true,
					TargetID:       def.EntityID,
					TargetEntityID: def.EntityID,
					Detail:         inst.DefinitionDir,
				})
				def.Relations = append(def.Relations, IaCRelation{
					Facet:          "instantiates",
					Kind:           "INSTANTIATES",
					Direction:      "in",
					Target:         inst.Name,
					TargetResolved: true,
					TargetID:       inst.EntityID,
					TargetEntityID: inst.EntityID,
					Detail:         inst.DefinitionDir,
				})
			}
		}
	}

	// Collect the env set across all module instances (post-projection so
	// definition resources have inherited an env too).
	envSet := map[string]bool{}
	for _, r := range byID {
		for _, e := range splitEnv(r.Env) {
			envSet[e] = true
		}
	}
	for e := range envSet {
		report.Envs = append(report.Envs, e)
	}
	sort.Strings(report.Envs)

	// Assemble groups by tool.
	byTool := map[string]*IaCToolGroup{}
	for _, res := range byID {
		g := byTool[res.Tool]
		if g == nil {
			g = &IaCToolGroup{Tool: res.Tool}
			byTool[res.Tool] = g
		}
		// Stable per-resource relation order: facet, then target.
		sort.SliceStable(res.Relations, func(i, j int) bool {
			if res.Relations[i].Facet != res.Relations[j].Facet {
				return res.Relations[i].Facet < res.Relations[j].Facet
			}
			return res.Relations[i].Target < res.Relations[j].Target
		})
		g.Resources = append(g.Resources, *res)
	}
	for _, g := range byTool {
		g.Count = len(g.Resources)
		sort.SliceStable(g.Resources, func(i, j int) bool {
			if g.Resources[i].Category != g.Resources[j].Category {
				return g.Resources[i].Category < g.Resources[j].Category
			}
			return g.Resources[i].Name < g.Resources[j].Name
		})
		report.Groups = append(report.Groups, *g)
	}
	sort.SliceStable(report.Groups, func(i, j int) bool {
		if report.Groups[i].Count != report.Groups[j].Count {
			return report.Groups[i].Count > report.Groups[j].Count
		}
		return report.Groups[i].Tool < report.Groups[j].Tool
	})

	for t := range tools {
		report.Tools = append(report.Tools, t)
	}
	sort.Strings(report.Tools)

	writeReportJSON(w, report)
}
