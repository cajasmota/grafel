package routes

import (
	"strconv"
	"strings"
)

// contracts.go — the curated per-framework resourceful-route contract tables.
//
// Each framework registers a map of action-name → VerbContract. The action
// names match what the framework's own routing layer names the generated route
// handlers, so the synthesizer (which already knows the action it is emitting)
// can look the contract up directly.
//
// Status sources (curated, verified against framework docs):
//   - Rails  ActionController: create → 201 Created, destroy → 204 No Content,
//     index/show/update → 200 OK (the conventional REST scaffold responses;
//     new/edit are HTML form views → 200). Refs: Rails Guides "Action
//     Controller Overview" + "Rails Routing from the Outside In".
//   - Laravel ResourceRegistrar: store → 201 Created, destroy → 204 No Content,
//     index/show/update → 200 OK (create/edit are form views → 200). Refs:
//     Laravel "Controllers — Resource Controllers" (actions table).
//   - Spring Data REST: POST collection → 201 Created, PUT/PATCH item → 200 OK
//     (or 204 when no body returned — we curate the documented 200 default),
//     DELETE item → 204 No Content, GET → 200 OK. Refs: Spring Data REST
//     reference "The repository resources" (default status codes).
//   - NestJS @nestjsx/crud: createOne → 201, the rest → 200 (deleteOne → 200 by
//     default; the lib does not 204 unless configured — honest default 200).
//     Refs: @nestjsx/crud route docs.

// Framework keys. These match the `framework` property the Rails/Laravel/Spring/
// NestJS route synthesizers stamp on their emitted http_endpoint synthetics, so
// a consumer can resolve the contract straight from the entity.
const (
	FrameworkRailsResources  = "rails_resources"      // plural `resources :name`
	FrameworkRailsResource   = "rails_resource"       // singular `resource :name`
	FrameworkLaravelResource = "laravel_resource"     // Route::resource
	FrameworkLaravelAPIResrc = "laravel_api_resource" // Route::apiResource
	FrameworkSpringDataREST  = "spring_data_rest"     // @RepositoryRestResource
	FrameworkNestJSCrud      = "nestjsx_crud"         // @Crud() controller
)

// defining-class facility names: the framework facility that GENERATES the
// resourceful route family (the route-level analogue of the DRF defining mixin).
const (
	definingRails       = "ActionDispatch::Routing::Mapper::Resources"
	definingLaravel     = "Illuminate\\Routing\\ResourceRegistrar"
	definingSpringDREST = "org.springframework.data.rest.core.annotation.RepositoryRestResource"
	definingNestJSCrud  = "@nestjsx/crud.Crud"
)

func c(action, verb string, status int, errs []int, behaviour, defining string) VerbContract {
	return VerbContract{
		Action:        action,
		HTTPVerb:      verb,
		DefaultStatus: status,
		ErrorStatuses: errs,
		Behaviour:     behaviour,
		DefiningClass: defining,
	}
}

// railsActions is the contract for the 7 RESTful actions Rails `resources`
// generates. index/show/new/edit/update/destroy/create.
var railsActions = map[string]VerbContract{
	"index":   c("index", "GET", 200, nil, "lists the collection", definingRails),
	"create":  c("create", "POST", 201, []int{422}, "creates a record; 422 on a save failure that renders errors", definingRails),
	"new":     c("new", "GET", 200, nil, "renders the new-record form view", definingRails),
	"show":    c("show", "GET", 200, []int{404}, "shows one record; 404 when not found", definingRails),
	"edit":    c("edit", "GET", 200, []int{404}, "renders the edit form view; 404 when not found", definingRails),
	"update":  c("update", "PATCH", 200, []int{404, 422}, "updates a record; 404 when not found, 422 on a save failure", definingRails),
	"destroy": c("destroy", "DELETE", 204, []int{404}, "deletes a record; 404 when not found", definingRails),
}

// laravelActions is the contract for the Laravel resource controller actions.
// Laravel names the create-action `store` and the form-view action `create`.
var laravelActions = map[string]VerbContract{
	"index":   c("index", "GET", 200, nil, "lists the collection", definingLaravel),
	"store":   c("store", "POST", 201, []int{422}, "stores a new record; 422 on validation failure", definingLaravel),
	"create":  c("create", "GET", 200, nil, "renders the create form view", definingLaravel),
	"show":    c("show", "GET", 200, []int{404}, "shows one record; 404 when not found", definingLaravel),
	"edit":    c("edit", "GET", 200, []int{404}, "renders the edit form view; 404 when not found", definingLaravel),
	"update":  c("update", "PUT", 200, []int{404, 422}, "updates a record; 404 when not found, 422 on validation failure", definingLaravel),
	"destroy": c("destroy", "DELETE", 204, []int{404}, "deletes a record; 204 No Content, 404 when not found", definingLaravel),
}

// springDataRESTActions is the contract for the routes Spring Data REST exports
// for a `@RepositoryRestResource` repository. Spring Data REST names them by
// HTTP semantics rather than Rails-style action verbs; we key on the same
// action vocabulary the synthesizer uses.
var springDataRESTActions = map[string]VerbContract{
	"list":   c("list", "GET", 200, nil, "GET /<collection> — paged list of the entity resources", definingSpringDREST),
	"get":    c("get", "GET", 200, []int{404}, "GET /<collection>/{id} — one entity; 404 when not found", definingSpringDREST),
	"create": c("create", "POST", 201, []int{400}, "POST /<collection> — creates an entity; 400 on a malformed body", definingSpringDREST),
	"update": c("update", "PUT", 200, []int{400, 404}, "PUT /<collection>/{id} — replaces an entity; 404 when not found", definingSpringDREST),
	"patch":  c("patch", "PATCH", 200, []int{400, 404}, "PATCH /<collection>/{id} — partial update; 404 when not found", definingSpringDREST),
	"delete": c("delete", "DELETE", 204, []int{404}, "DELETE /<collection>/{id} — deletes the entity; 204 No Content", definingSpringDREST),
}

// nestjsCrudActions is the contract for the 5 base routes a NestJS `@Crud()`
// controller exposes (the @nestjsx/crud default route set).
var nestjsCrudActions = map[string]VerbContract{
	"getMany":   c("getMany", "GET", 200, nil, "GET /<resource> — list (paginated when configured)", definingNestJSCrud),
	"getOne":    c("getOne", "GET", 200, []int{404}, "GET /<resource>/{id} — one entity; 404 when not found", definingNestJSCrud),
	"createOne": c("createOne", "POST", 201, []int{400}, "POST /<resource> — creates an entity; 400 on validation failure", definingNestJSCrud),
	"updateOne": c("updateOne", "PATCH", 200, []int{400, 404}, "PATCH /<resource>/{id} — partial update; 404 when not found", definingNestJSCrud),
	"deleteOne": c("deleteOne", "DELETE", 200, []int{404}, "DELETE /<resource>/{id} — deletes the entity; 404 when not found", definingNestJSCrud),
}

// frameworkTables maps a framework key to its action→contract table. The
// laravel_api_resource variant shares the laravel table (it is a 5-action
// subset; the synthesizer only emits the API actions).
var frameworkTables = map[string]map[string]VerbContract{
	FrameworkRailsResources:  railsActions,
	FrameworkRailsResource:   railsActions,
	FrameworkLaravelResource: laravelActions,
	FrameworkLaravelAPIResrc: laravelActions,
	FrameworkSpringDataREST:  springDataRESTActions,
	FrameworkNestJSCrud:      nestjsCrudActions,
}

// IsSynthesizedFramework reports whether the given framework key denotes a
// resourceful-route synthesis this package contracts (so a consumer can gate
// the provenance/contract stamp on it).
func IsSynthesizedFramework(framework string) bool {
	_, ok := frameworkTables[framework]
	return ok
}

// Lookup resolves the per-verb contract for a (framework, action) pair. The
// boolean reports whether a curated contract exists.
func Lookup(framework, action string) (VerbContract, bool) {
	tbl, ok := frameworkTables[framework]
	if !ok {
		return VerbContract{}, false
	}
	v, ok := tbl[action]
	return v, ok
}

// Property keys stamped onto a synthesized route's Properties map. They mirror
// the DRF effective-contract namespace (#3835) so the archigraph_effective_contract
// MCP query (T6 #3836) surfaces convention-framework routes with the SAME shape
// as DRF: `provenance` + `effective_kind` + `effective_status` + the rest.
const (
	PropProvenance         = "provenance"
	PropEffectiveKind      = "effective_kind"
	PropEffectiveAction    = "effective_action"
	PropEffectiveStatus    = "effective_status"
	PropEffectiveErrors    = "effective_error_statuses"
	PropEffectiveBehaviour = "effective_behaviour"
	PropEffectiveSourceCls = "effective_source_class"
	PropDefiningClass      = "defining_class"
)

// Stamp writes the framework-synthesized route PROVENANCE + per-verb effective
// CONTRACT onto props for a route the framework auto-generated. It is the single
// cross-framework entry point the Rails / Laravel / Spring Data REST / NestJS
// route synthesizers call after they emit each resourceful route.
//
// HONEST-PARTIAL: when no curated contract exists for (framework, action) it
// still stamps the provenance + kind + action + defining facility (the route IS
// framework-synthesized — that fact is known), but stamps NO status. A status is
// never fabricated.
//
// It never overwrites a status a prior pass already resolved from the handler
// body (props[PropEffectiveStatus] already set) — the body override wins.
func Stamp(props map[string]string, framework, action string) {
	if props == nil {
		return
	}
	props[PropProvenance] = Provenance
	props[PropEffectiveKind] = EffectiveKind
	if action != "" {
		props[PropEffectiveAction] = action
	}

	v, ok := Lookup(framework, action)
	if !ok {
		return
	}
	if v.DefiningClass != "" {
		props[PropDefiningClass] = v.DefiningClass
		props[PropEffectiveSourceCls] = v.DefiningClass
	}
	if v.Behaviour != "" {
		props[PropEffectiveBehaviour] = v.Behaviour
	}
	// Body-override guard: a handler-body pass may already have resolved a
	// concrete status — never clobber it.
	if v.IsKnown() {
		if _, already := props[PropEffectiveStatus]; !already {
			props[PropEffectiveStatus] = strconv.Itoa(v.DefaultStatus)
		}
	}
	if len(v.ErrorStatuses) > 0 {
		if _, already := props[PropEffectiveErrors]; !already {
			parts := make([]string, len(v.ErrorStatuses))
			for i, s := range v.ErrorStatuses {
				parts[i] = strconv.Itoa(s)
			}
			props[PropEffectiveErrors] = strings.Join(parts, ",")
		}
	}
}
