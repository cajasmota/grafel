package mcp

// effective_contract_nestjs.go — the NestJS effective-contract resolver
// (#4601). Composes the per-endpoint full contract for a NestJS controller from
// signals that ALREADY exist on the graph — it re-extracts nothing:
//
//   - REQUEST SHAPE: the handler's @Body/@Query/@Param DTO (the VALIDATES edge
//     the JS extractor emits to `dto:<TypeName>`, via=dto_extraction, carrying
//     the decorator in `method` — #4623/#4635) plus the DTO's FIELD members
//     (SCOPE.Schema subtype=field named "<DTO>.<field>", CONTAINS-linked from
//     the DTO class). The endpoint's `request_body_type` prop is a fallback
//     when the handler entity is unresolved.
//
//   - RESPONSE SHAPE + PER-BRANCH STATUS: the effects-branches facet
//     (#4423/#4666) — the per-language branch analyzer run over the handler
//     body, yielding {status, shape} per return/throw branch. NestJS named
//     exceptions (`throw new ConflictException()` → 409) and `HttpStatus.NAME`
//     are decoded by the analyzer (this ticket extended jstsStatusFromBody).
//
//   - AUTH POSTURE: the effective guard (#4667) — decoded by the shared
//     authposture registry's NestJS resolver from the engine-reconciled
//     auth_guard / require_page / … props on the handler/endpoint.
//
// The emitted effectiveContract is the SAME structure the DRF resolver returns
// (status set via ResponseBranches, request fields, auth), so the cross-group
// response_shape_diff / parity tools consume both DRF and NestJS contracts.

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/authposture"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/substrate"
)

// nestJSContractResolver composes effective contracts for NestJS controllers.
type nestJSContractResolver struct{}

func (nestJSContractResolver) Name() string { return "nestjs" }

// Resolve gathers every NestJS http_endpoint_definition whose owning controller
// leaf matches wantLeaf, composes each endpoint's request/response/auth
// contract, and groups them by (repo, controller). ok=false when no NestJS
// endpoint is attributed to the target (so the registry tries the next
// resolver).
func (n nestJSContractResolver) Resolve(lg *LoadedGroup, target, wantLeaf string) ([]effectiveContractGroup, bool) {
	type groupKey struct{ repo, class string }
	groups := map[groupKey]*effectiveContractGroup{}
	var order []groupKey

	for _, r := range reposToConsider(lg, nil) {
		if r.Doc == nil {
			continue
		}
		r.forEachEntity(func(e *graph.Entity) bool {
			if !isServerEndpointDefinition(e) {
				return true
			}
			if !isNestJSEndpoint(e) {
				return true
			}
			handler := nestHandlerEntity(r, e)
			controller := nestControllerLeaf(e, handler)
			if controller == "" || strings.ToLower(controller) != wantLeaf {
				return true
			}
			c := composeNestContract(r, e, handler)
			key := groupKey{repo: r.Repo, class: controller}
			g, exists := groups[key]
			if !exists {
				g = &effectiveContractGroup{
					Class:     controller,
					Framework: "nestjs",
					Repo:      r.Repo,
				}
				groups[key] = g
				order = append(order, key)
			}
			g.Handlers = append(g.Handlers, c)
			return true
		})
	}
	if len(groups) == 0 {
		return nil, false
	}

	sort.Slice(order, func(i, j int) bool {
		if order[i].repo != order[j].repo {
			return order[i].repo < order[j].repo
		}
		return order[i].class < order[j].class
	})
	out := make([]effectiveContractGroup, 0, len(order))
	for _, key := range order {
		g := groups[key]
		sortEffectiveContracts(g.Handlers)
		out = append(out, *g)
	}
	return out, true
}

// isNestJSEndpoint reports whether an endpoint definition belongs to NestJS.
// Recognised by an explicit framework prop OR by the NestJS auth-decorator /
// guard property signature the metadata pass stamps.
func isNestJSEndpoint(e *graph.Entity) bool {
	fw := endpointFramework(e)
	if strings.Contains(fw, "nest") {
		return true
	}
	// A TS endpoint carrying any Nest-characteristic prop is NestJS even when the
	// framework hint was not stamped (graphs frequently omit it).
	if fw == "typescript" || fw == "javascript" || fw == "ts" || fw == "js" {
		p := e.PropsSnapshot()
		if p["require_page"] != "" || p["require_action"] != "" || p["auth_guard"] != "" ||
			p["request_body_type"] != "" || p["operation_id"] != "" {
			return true
		}
	}
	return false
}

// nestHandlerEntity resolves the controller-method entity backing a NestJS
// endpoint definition. It follows the inbound IMPLEMENTS edge the endpoint
// resolution pass emits (handler → endpoint). nil when the handler is
// unresolved (honest-partial: the resolver degrades to endpoint props).
func nestHandlerEntity(r *LoadedRepo, ep *graph.Entity) *graph.Entity {
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

// nestControllerLeaf returns the controller leaf name an endpoint belongs to —
// the prefix of the handler's qualified name ("UsersController.create" →
// "UsersController"), falling back to the endpoint's own controller attribution
// props.
func nestControllerLeaf(ep *graph.Entity, handler *graph.Entity) string {
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
	// source_handler is "<HandlerKind>:<Controller.method>" or "<Controller.method>".
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

// composeNestContract builds the effectiveContract for one NestJS endpoint from
// its request DTO fields, per-branch response shapes, and auth posture.
func composeNestContract(r *LoadedRepo, ep *graph.Entity, handler *graph.Entity) effectiveContract {
	c := effectiveContract{
		Framework: "nestjs",
		Verb:      strings.ToUpper(ep.PropGet("verb")),
		Path:      ep.PropGet("path"),
		Kind:      "explicit",
	}
	if handler != nil {
		hq := handler.QualifiedName
		if hq == "" {
			hq = handler.Name
		}
		c.Handler = leafAfterDot(prefixBeforeDot(hq)) + "." + leafAfterDot(hq)
		c.SourceClass = leafAfterDot(prefixBeforeDot(hq))
	}

	c.RequestFields = composeNestRequestFields(r, ep, handler)
	c.ResponseBranches = composeNestResponseBranches(r, handler)
	applyNestAuthPosture(r, ep, handler, &c)

	// Mirror the DRF status split so the flat status fields stay populated for
	// consumers that read them: the lowest 2xx branch is the default success,
	// every >=400 branch is an error status.
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
	return c
}

// composeNestRequestFields resolves the endpoint's request-shape members: the
// handler's @Body/@Query/@Param DTO field members plus the DTO type fallback.
func composeNestRequestFields(r *LoadedRepo, ep *graph.Entity, handler *graph.Entity) []contractField {
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

	// Handler VALIDATES edges → dto:<Type>, decorator in the edge's `method`.
	if handler != nil {
		adj := r.getAdjacency()
		for _, ed := range adj.Outgoing(handler.ID) {
			if !strings.EqualFold(ed.kind, "VALIDATES") {
				continue
			}
			rel := relPropsFor(r, ed.relIdx)
			dtoType := rel["dto"]
			if dtoType == "" {
				dtoType = strings.TrimPrefix(ed.target, "dto:")
			}
			in := decoratorToIn(rel["method"])
			for _, mf := range nestDTOFields(r, dtoType) {
				mf.In = in
				mf.DTO = dtoType
				mf.Source = "dto_field"
				add(mf)
			}
		}
	}

	// Fallback: the endpoint's request_body_type prop names the @Body DTO even
	// when the handler entity was unresolved — surface its fields if findable.
	if len(fields) == 0 {
		if dtoType := ep.PropGet("request_body_type"); dtoType != "" {
			for _, mf := range nestDTOFields(r, dtoType) {
				mf.In = "body"
				mf.DTO = dtoType
				mf.Source = "dto_field"
				add(mf)
			}
		}
	}

	sort.Slice(fields, func(i, j int) bool {
		if fields[i].In != fields[j].In {
			return fields[i].In < fields[j].In
		}
		return fields[i].Name < fields[j].Name
	})
	return fields
}

// nestDTOFields returns the FIELD members of a DTO type — the SCOPE.Schema
// subtype=field entities named "<DTO>.<field>" the JS extractor emits for each
// class-validator field. The type annotation is lifted from the field's
// signature, and a trailing "?" on the field name marks it optional.
func nestDTOFields(r *LoadedRepo, dtoType string) []contractField {
	if r.Doc == nil || dtoType == "" {
		return nil
	}
	prefix := dtoType + "."
	var out []contractField
	r.forEachEntity(func(e *graph.Entity) bool {
		if !strings.EqualFold(e.Kind, "SCOPE.Schema") || e.Subtype != "field" {
			return true
		}
		if !strings.HasPrefix(e.Name, prefix) {
			return true
		}
		name := strings.TrimPrefix(e.Name, prefix)
		required := true
		if strings.HasSuffix(name, "?") {
			name = strings.TrimSuffix(name, "?")
			required = false
		}
		out = append(out, contractField{
			Name:     name,
			Type:     fieldTypeFromSignature(e.Signature),
			Required: required,
		})
		return true
	})
	return out
}

// fieldTypeFromSignature lifts the declared type from a field signature of the
// form "name: Type" (the form handlePublicFieldDefinition emits). Empty when
// the signature carries no annotation.
func fieldTypeFromSignature(sig string) string {
	if i := strings.Index(sig, ":"); i >= 0 {
		return strings.TrimSpace(sig[i+1:])
	}
	return ""
}

// decoratorToIn maps a NestJS DTO decorator ("@Body()", "@Query()", "@Param()")
// to the request location it binds.
func decoratorToIn(decorator string) string {
	d := strings.ToLower(decorator)
	switch {
	case strings.Contains(d, "query"):
		return "query"
	case strings.Contains(d, "param"):
		return "param"
	case strings.Contains(d, "body"):
		return "body"
	default:
		return "body"
	}
}

// composeNestResponseBranches runs the per-language branch analyzer (the #4423
// effects-branches facet) over the handler body and projects each return/throw
// branch into a {status, shape} response branch.
func composeNestResponseBranches(r *LoadedRepo, handler *graph.Entity) []contractResponseBranch {
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

// applyNestAuthPosture decodes the endpoint's effective guard (#4667) via the
// shared authposture NestJS resolver and stamps the posture onto the contract.
// The handler's source (when resolvable) is supplied as the decorator fallback.
func applyNestAuthPosture(r *LoadedRepo, ep *graph.Entity, handler *graph.Entity, c *effectiveContract) {
	sig := authposture.Signal{
		Framework: "nestjs",
		Props:     mergedAuthProps(ep, handler),
		Source:    nestHandlerSource(r, handler),
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

// mergedAuthProps merges the endpoint's auth props with the handler's (handler
// props win — they are the most-specific decorator metadata) into one bag for
// the authposture Signal.
func mergedAuthProps(ep *graph.Entity, handler *graph.Entity) map[string]string {
	out := map[string]string{}
	ep.
		PropRange(func(k, v string) bool { out[k] = v; return true })
	if handler != nil {
		handler.
			PropRange(func(k, v string) bool {
				if v != "" {
					out[k] = v
				}
				return true
			})
	}
	return out
}

// nestHandlerSource reads the handler's source window (for the auth-decorator
// fallback). Empty when unresolvable — the resolver degrades to props-only.
func nestHandlerSource(r *LoadedRepo, handler *graph.Entity) string {
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
	// Reach a few lines above the signature to capture method decorators.
	if start > 6 {
		start -= 6
	} else {
		start = 1
	}
	src, _ := readRawSourceWindow(abs, start, end)
	return src
}

// relPropsFor returns the Properties map of the relationship at relIdx in r's
// Doc, or nil when relIdx is synthetic (-1) or out of range.
func relPropsFor(r *LoadedRepo, relIdx int) map[string]string {
	if r.Doc == nil || relIdx < 0 || relIdx >= len(r.Doc.Relationships) {
		return nil
	}
	return r.Doc.Relationships[relIdx].PropsSnapshot()
}

// appendIntUnique appends v to s only when absent.
func appendIntUnique(s []int, v int) []int {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// appendStringUnique appends v to s only when absent and non-empty.
func appendStringUnique(s []string, v string) []string {
	if v == "" {
		return s
	}
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
