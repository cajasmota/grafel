package mcp

// effective_contract_fw_common.go — signals shared by the non-DRF, non-NestJS
// framework resolvers (Spring #4708, FastAPI #4709, Express/Fastify #4710).
//
// The NestJS resolver (effective_contract_nestjs.go, #4601/#4711) established the
// pattern: compose the SAME effectiveContract shape (request fields, per-branch
// response shapes+statuses, auth posture) from signals that ALREADY exist on the
// graph — re-extract nothing. The Spring/FastAPI/Express resolvers reuse the same
// machinery; this file factors out the parts that are genuinely framework-neutral
// so each resolver is just (a) its framework predicate, (b) its request-field
// signal sourcing, and (c) its dispatch — keeping DRF + NestJS byte-for-byte
// unchanged.
//
// Three signals are uniform across the engine-resolved http_endpoint_definition
// graph regardless of stack and are reused verbatim here:
//
//   - HANDLER: the inbound IMPLEMENTS edge (handler → endpoint) the endpoint
//     resolution pass emits for annotation AND bare-name frameworks (NestJS,
//     Spring, JAX-RS, Express, Axum, …) — http_endpoint_resolve.go.
//   - REQUEST DTO: the endpoint's `request_body_type` prop, stamped uniformly by
//     the engine for every framework (the @RequestBody DTO, the Pydantic body
//     model, the validated schema). Its FIELD members are SCOPE.Schema/field
//     entities carrying field_name/field_type/parent_class/optional props — the
//     SAME property shape the JS (#4635), Java (#4613) and Python (#4613)
//     field-membership extractors emit.
//   - RESPONSE BRANCHES: the per-language branch analyzer (substrate, #4423) over
//     the handler body — already decodes ResponseEntity.status(NNN)/HttpStatus.*
//     (Java), HTTPException(status_code=)/HTTP_NNN (Python), res.status(NNN).json
//     / reply.code(NNN) (JSTS).
//
// composeFrameworkResponseBranches and the field/param helpers below are shared;
// auth posture is delegated to the framework's authposture resolver per stack.

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/authposture"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/substrate"
)

// frameworkHandlerEntity resolves the handler method backing an endpoint
// definition by following the inbound IMPLEMENTS edge (handler → endpoint) the
// endpoint resolution pass emits across frameworks. nil when unresolved
// (honest-partial: the resolver degrades to endpoint props). Mirrors
// nestHandlerEntity but is framework-neutral.
func frameworkHandlerEntity(r *LoadedRepo, ep *graph.Entity) *graph.Entity {
	adj := r.getAdjacency()
	byID := r.getByID()
	for _, ed := range adj.Incoming(ep.ID) {
		if !strings.EqualFold(ed.kind, "IMPLEMENTS") {
			continue
		}
		if h := byID[ed.target]; h != nil {
			return h
		}
	}
	return nil
}

// frameworkControllerLeaf returns the controller/class leaf an endpoint belongs
// to — the prefix of the handler's qualified name ("UserController.create" →
// "UserController"), falling back to the endpoint's controller/source_handler
// props. Framework-neutral version of nestControllerLeaf. For flat-function
// frameworks (FastAPI path-ops, Express handlers) the handler often has no
// dotted owner; callers pass a synthetic group name in that case.
func frameworkControllerLeaf(ep *graph.Entity, handler *graph.Entity) string {
	if handler != nil {
		qn := handler.QualifiedName
		if qn == "" {
			qn = handler.Name
		}
		if owning := prefixBeforeDot(qn); owning != "" {
			return leafAfterDot(owning)
		}
	}
	if c := ep.PropGet("controller"); c != "" {
		return leafAfterDot(c)
	}
	if sh := ep.PropGet("source_handler"); sh != "" {
		if i := strings.LastIndex(sh, ":"); i >= 0 {
			sh = sh[i+1:]
		}
		if owning := prefixBeforeDot(sh); owning != "" {
			return leafAfterDot(owning)
		}
	}
	return ""
}

// dtoFieldsByProperty returns the FIELD members of a DTO type by scanning for
// SCOPE.Schema subtype=field entities whose `parent_class` property (or whose
// "<DTO>." name prefix, as a fallback) names dtoType. The field type, name and
// optionality are read from the uniformly-stamped field_name / field_type /
// optional / required properties — the SAME shape the JS, Java and Python
// field-membership extractors emit — so this works across all three stacks
// without per-language signature parsing. Empty when the DTO has no field
// members on the graph (honest-partial).
func dtoFieldsByProperty(r *LoadedRepo, dtoType string) []contractField {
	if r == nil || r.Doc == nil || dtoType == "" {
		return nil
	}
	prefix := dtoType + "."
	var out []contractField
	for i := range r.Doc.Entities {
		e := &r.Doc.Entities[i]
		if !strings.EqualFold(e.Kind, "SCOPE.Schema") || e.Subtype != "field" {
			continue
		}
		owner := strings.TrimSpace(e.PropGet("parent_class"))
		if owner == "" {
			owner = strings.TrimSpace(e.PropGet("owner_class"))
		}
		name := strings.TrimSpace(e.PropGet("field_name"))
		switch {
		case owner != "" && owner == dtoType:
			// matched by parent_class property — preferred.
		case strings.HasPrefix(e.Name, prefix):
			if name == "" {
				name = strings.TrimSuffix(strings.TrimPrefix(e.Name, prefix), "?")
			}
		default:
			continue
		}
		if name == "" {
			name = strings.TrimSuffix(strings.TrimPrefix(e.Name, prefix), "?")
		}
		if name == "" {
			continue
		}
		typ := strings.TrimSpace(e.PropGet("field_type"))
		if typ == "" {
			typ = fieldTypeFromSignature(e.Signature)
		}
		out = append(out, contractField{
			Name:     name,
			Type:     typ,
			Required: fieldRequiredFromProps(e),
			Source:   "dto_field",
		})
	}
	return out
}

// fieldRequiredFromProps decodes a field's required/optional posture from the
// stamped props: an explicit optional="true" wins (optional), else required is
// the default unless the entity name carries the JS `?` optional marker.
func fieldRequiredFromProps(e *graph.Entity) bool {
	if strings.EqualFold(strings.TrimSpace(e.PropGet("optional")), "true") {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(e.PropGet("required")), "true") {
		return true
	}
	if strings.HasSuffix(e.Name, "?") {
		return false
	}
	return true
}

// scalarParamsFromProps surfaces the endpoint's scalar path/query parameters
// from the uniformly-stamped `path_params` (and `query_params`, where present)
// comma-separated props the route extractors emit (Spring @PathVariable /
// @RequestParam → path_params, Micronaut, etc.). Each becomes a contractField
// with In=param|query, Source=scalar_param. Type is left empty (the flat props
// carry names only — an honest gap noted per framework).
func scalarParamsFromProps(ep *graph.Entity) []contractField {
	var out []contractField
	add := func(csv, in string) {
		for _, raw := range strings.Split(csv, ",") {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			out = append(out, contractField{
				Name:     name,
				In:       in,
				Required: in == "param", // path params are always required
				Source:   "scalar_param",
			})
		}
	}
	add(ep.PropGet("path_params"), "param")
	add(ep.PropGet("query_params"), "query")
	return out
}

// composeFrameworkResponseBranches runs the per-language branch analyzer (the
// #4423 effects-branches facet) over the handler body and projects each
// return/throw branch into a {status, shape} response branch. Framework-neutral:
// the analyzer is selected by the handler's source language, so Spring (Java),
// FastAPI (Python) and Express/Fastify (JSTS) all flow through here. Identical in
// behaviour to composeNestResponseBranches — factored out for reuse.
func composeFrameworkResponseBranches(r *LoadedRepo, handler *graph.Entity) []contractResponseBranch {
	if handler == nil || handler.StartLine <= 0 {
		return nil
	}
	lang := substrate.LanguageForPath(handler.SourceFile)
	analyzer := substrate.BranchAnalyzerFor(lang)
	if analyzer == nil {
		return nil
	}
	start, end := branchSourceSpan(handler)
	if start <= 0 {
		return nil
	}
	abs := handler.SourceFile
	if !filepath.IsAbs(abs) && r.Path != "" {
		abs = filepath.Join(r.Path, handler.SourceFile)
	}
	src, err := readRawSourceWindow(abs, start, end)
	if err != nil || src == "" {
		return nil
	}
	src = substrate.ClampToFunctionBody(src, lang)
	facets := analyzer(src, start)

	var out []contractResponseBranch
	seen := map[int]bool{}
	for _, f := range facets {
		if f.Returns == nil {
			continue
		}
		status := 0
		if f.Returns.Status != "" {
			status, _ = strconv.Atoi(f.Returns.Status)
		}
		if status == 0 && f.Returns.Shape == "" {
			continue
		}
		if status != 0 {
			if seen[status] {
				continue
			}
			seen[status] = true
		}
		out = append(out, contractResponseBranch{
			Status:  status,
			Shape:   f.Returns.Shape,
			Outcome: string(f.Outcome),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status != out[j].Status {
			return out[i].Status < out[j].Status
		}
		return out[i].Shape < out[j].Shape
	})
	return out
}

// frameworkHandlerSource reads the handler's source window with a few lines of
// lead-in (to capture leading method decorators/annotations for the auth
// fallback). Empty when unresolvable. Framework-neutral version of
// nestHandlerSource.
func frameworkHandlerSource(r *LoadedRepo, handler *graph.Entity) string {
	if handler == nil || handler.StartLine <= 0 {
		return ""
	}
	start, end := branchSourceSpan(handler)
	if start <= 0 {
		return ""
	}
	abs := handler.SourceFile
	if !filepath.IsAbs(abs) && r.Path != "" {
		abs = filepath.Join(r.Path, handler.SourceFile)
	}
	if start > 6 {
		start -= 6
	} else {
		start = 1
	}
	src, _ := readRawSourceWindow(abs, start, end)
	return src
}

// applyFrameworkStatusSplit mirrors the DRF status split onto the flat contract
// fields from the per-branch response set: the lowest 2xx branch is the default
// success, every >=400 branch is an error status. Shared by all framework
// resolvers (identical to the tail of composeNestContract).
func applyFrameworkStatusSplit(c *effectiveContract) {
	for _, b := range c.ResponseBranches {
		switch {
		case b.Status >= 200 && b.Status < 300:
			if c.DefaultStatus == 0 || b.Status < c.DefaultStatus {
				c.DefaultStatus = b.Status
			}
		case b.Status >= 400:
			c.ErrorStatuses = appendIntUnique(c.ErrorStatuses, b.Status)
		}
	}
	sort.Ints(c.ErrorStatuses)
}

// applyFrameworkAuthPosture decodes the endpoint's auth posture via the shared
// authposture registry for the given framework and stamps it onto the contract.
// The handler source (with decorator lead-in) is supplied as the fallback.
// Mirrors applyNestAuthPosture but parameterised by framework slug.
func applyFrameworkAuthPosture(r *LoadedRepo, framework string, ep, handler *graph.Entity, c *effectiveContract) {
	sig := authposture.Signal{
		Framework: framework,
		Props:     mergedAuthProps(ep, handler),
		Source:    frameworkHandlerSource(r, handler),
	}
	p, _ := authposture.NewRegistry().Resolve(sig)
	if p.Kind == "" {
		return
	}
	c.AuthKind = string(p.Kind)
	c.AuthLiteral = p.Literal
	c.AuthRequired = p.Kind != authposture.KindPublic && p.Kind != authposture.KindUnknown
	if p.Literal != "" {
		c.Permissions = appendStringUnique(c.Permissions, p.Literal)
	}
}

// composeFrameworkRequestFields composes the request-shape members for a
// non-NestJS framework endpoint: the `request_body_type` DTO's field members
// (uniform request signal) plus the endpoint's scalar path/query params. Body
// fields are marked In=body. Deduped + sorted (body, then param, then query).
func composeFrameworkRequestFields(r *LoadedRepo, ep *graph.Entity, handler *graph.Entity) []contractField {
	var fields []contractField
	seen := map[string]bool{}
	add := func(f contractField) {
		k := f.In + "\x00" + f.Name
		if f.Name == "" || seen[k] {
			return
		}
		seen[k] = true
		fields = append(fields, f)
	}

	if dtoType := ep.PropGet("request_body_type"); dtoType != "" {
		for _, mf := range dtoFieldsByProperty(r, dtoType) {
			mf.In = "body"
			mf.DTO = dtoType
			add(mf)
		}
	}
	for _, p := range scalarParamsFromProps(ep) {
		add(p)
	}

	sort.Slice(fields, func(i, j int) bool {
		if fields[i].In != fields[j].In {
			return fields[i].In < fields[j].In
		}
		return fields[i].Name < fields[j].Name
	})
	return fields
}
