package rust

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_rust_utoipa", &utoipaExtractor{})
}

type utoipaExtractor struct{}

func (e *utoipaExtractor) Language() string { return "custom_rust_utoipa" }

var (
	// `#[utoipa::path(` — start of an OpenAPI operation annotation. The full
	// argument list is read with a balanced-paren scan because it nests
	// `responses((status = 200, body = User))`, `params(...)`, etc. The handler
	// fn name is captured from the `fn <name>` that follows the attribute block.
	reUtoipaPathStart = regexp.MustCompile(`#\[\s*utoipa::path\s*\(`)
	// `#[path(` form, used when `use utoipa::path;` is in scope. Required `get,`
	// /`post,` … leading method keyword disambiguates it from unrelated macros.
	reUtoipaPathBare = regexp.MustCompile(
		`#\[\s*path\s*\(\s*(?:get|post|put|delete|patch|head|options|trace|connect)\b`)

	// HTTP method keyword as the first positional arg of utoipa::path.
	reUtoipaMethod = regexp.MustCompile(
		`(?i)\b(get|post|put|delete|patch|head|options|trace|connect)\b`)
	// path = "/users/{id}"
	reUtoipaPathArg = regexp.MustCompile(`\bpath\s*=\s*"([^"]+)"`)
	// request_body = CreateUser  /  request_body = inline(CreateUser)  /
	// request_body(content = CreateUser, ...)
	reUtoipaRequestBody = regexp.MustCompile(
		`\brequest_body\s*(?:=\s*(?:inline\s*\(\s*)?|\(\s*content\s*=\s*)([A-Za-z_]\w*)`)
	// responses( (status = 200, body = User), (status = 404, body = ApiError) )
	// Captures each `body = <Type>` (optionally `inline(<Type>)`) inside responses.
	reUtoipaResponseBody = regexp.MustCompile(
		`\bbody\s*=\s*(?:inline\s*\(\s*)?\[?\s*([A-Za-z_]\w*)`)
	// status = 201  /  status = StatusCode::CREATED — numeric form captured.
	reUtoipaStatus = regexp.MustCompile(`\bstatus\s*=\s*(\d{3})`)
	// fn name following the attribute block.
	reUtoipaFnName = regexp.MustCompile(`\bfn\s+(\w+)`)

	// #[derive(... ToSchema ...)] — the OpenAPI schema marker.
	reUtoipaDerive = regexp.MustCompile(`#\[derive\(([^)]*)\)\]`)
	// #[derive(OpenApi)] aggregator + its #[openapi(...)] attribute.
	reUtoipaOpenAPIStart = regexp.MustCompile(`#\[\s*openapi\s*\(`)
	// paths(handler_a, crate::users::get_user, ...) inside #[openapi(...)]
	reUtoipaOpenAPIPaths = regexp.MustCompile(`\bpaths\s*\(([^)]*)\)`)
	// components(schemas(User, CreateUser, ...)) inside #[openapi(...)]
	reUtoipaOpenAPISchemas = regexp.MustCompile(`\bschemas\s*\(([^)]*)\)`)
	// struct ApiDoc; following an #[openapi(...)] aggregator.
	reUtoipaAggName = regexp.MustCompile(`\bstruct\s+(\w+)`)
	// IntoParams params struct marker.
	reUtoipaIntoParams = regexp.MustCompile(`\bIntoParams\b`)
)

// rustBalancedParens returns the substring inside the paren that opens at or
// after openParenOff (which must point at the '(' itself), excluding the outer
// parens, doing a depth-counted scan so nested `(...)` groups are kept intact.
// Returns the inner text and the index just past the closing ')'.
func rustBalancedParens(src string, openParenOff int) (inner string, endOff int, ok bool) {
	if openParenOff >= len(src) || src[openParenOff] != '(' {
		return "", openParenOff, false
	}
	depth := 0
	for i := openParenOff; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[openParenOff+1 : i], i + 1, true
			}
		}
	}
	return "", openParenOff, false
}

func (e *utoipaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.utoipa_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "utoipa"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// -----------------------------------------------------------------------
	// 1. route_extraction / request+response shape — #[utoipa::path(...)]
	// The attribute is the documented OpenAPI contract for a handler. It is the
	// authoritative source for verb + path + request/response DTOs and is what
	// enriches the bare axum/actix route with the wire contract.
	// -----------------------------------------------------------------------
	var pathStarts [][]int
	for _, m := range reUtoipaPathStart.FindAllStringIndex(src, -1) {
		// m[1]-1 is the '(' that opens the utoipa::path argument list.
		pathStarts = append(pathStarts, []int{m[0], m[1] - 1})
	}
	for _, m := range reUtoipaPathBare.FindAllStringIndex(src, -1) {
		// The bare `#[path(` form: locate its '(' (the one after `path`).
		open := strings.IndexByte(src[m[0]:], '(')
		if open < 0 {
			continue
		}
		pathStarts = append(pathStarts, []int{m[0], m[0] + open})
	}

	for _, ps := range pathStarts {
		attrOff, openOff := ps[0], ps[1]
		inner, endOff, ok := rustBalancedParens(src, openOff)
		if !ok {
			continue
		}

		method := ""
		if mm := reUtoipaMethod.FindStringSubmatch(inner); mm != nil {
			method = strings.ToUpper(mm[1])
		}
		routePath := ""
		if pm := reUtoipaPathArg.FindStringSubmatch(inner); pm != nil {
			routePath = rustNormalizePath(pm[1])
		}
		if method == "" || routePath == "" {
			// Not a usable operation contract; skip rather than emit a partial.
			continue
		}

		// Handler fn name: the `fn <name>` that follows the attribute block.
		handler := ""
		if fm := reUtoipaFnName.FindStringSubmatchIndex(src[endOff:]); fm != nil {
			handler = src[endOff+fm[2] : endOff+fm[3]]
		}

		name := method + " " + routePath
		ent := makeEntity(name, "SCOPE.Operation", "endpoint",
			file.Path, file.Language, lineOf(src, attrOff))
		setProps(&ent,
			"framework", "utoipa",
			"provenance", "INFERRED_FROM_UTOIPA_PATH",
			"http_method", method,
			"route_path", routePath,
		)
		if handler != "" {
			setProps(&ent, "handler_name", handler)
		}

		// request_body = <DTO> -> request shape + DTO reference.
		if rm := reUtoipaRequestBody.FindStringSubmatch(inner); rm != nil {
			reqDTO := rm[1]
			setProps(&ent, "request_body", reqDTO)
			reqEnt := makeEntity("utoipa_request:"+reqDTO, "SCOPE.Schema", "request_dto",
				file.Path, file.Language, lineOf(src, attrOff))
			setProps(&reqEnt,
				"framework", "utoipa",
				"provenance", "INFERRED_FROM_UTOIPA_REQUEST_BODY",
				"type_param", reqDTO,
				"http_method", method,
				"route_path", routePath,
			)
			add(reqEnt)
		}

		// responses((status = N, body = <DTO>), ...) -> response shapes.
		// Scan each `body = <DTO>` (with the nearest preceding status, if any).
		var respDTOs []string
		for _, bm := range reUtoipaResponseBody.FindAllStringSubmatchIndex(inner, -1) {
			respDTO := inner[bm[2]:bm[3]]
			status := ""
			// Nearest status keyword before this body, within the same response
			// tuple, is a good-enough association for the contract.
			if sm := reUtoipaStatus.FindStringSubmatchIndex(inner[:bm[0]]); sm != nil {
				// take the LAST status before this body
				all := reUtoipaStatus.FindAllStringSubmatchIndex(inner[:bm[0]], -1)
				last := all[len(all)-1]
				status = inner[last[2]:last[3]]
			}
			respDTOs = append(respDTOs, respDTO)
			respEnt := makeEntity("utoipa_response:"+respDTO, "SCOPE.Schema", "response_dto",
				file.Path, file.Language, lineOf(src, attrOff))
			setProps(&respEnt,
				"framework", "utoipa",
				"provenance", "INFERRED_FROM_UTOIPA_RESPONSE_BODY",
				"type_param", respDTO,
				"http_method", method,
				"route_path", routePath,
			)
			if status != "" {
				setProps(&respEnt, "status_code", status)
			}
			add(respEnt)
		}
		if len(respDTOs) > 0 {
			setProps(&ent, "response_bodies", strings.Join(respDTOs, ","))
		}
		add(ent)
	}

	// -----------------------------------------------------------------------
	// 2. schema/dto_extraction — #[derive(ToSchema)] structs.
	// Each becomes a SCOPE.Schema DTO with deep fields, mirroring serde DTOs.
	// -----------------------------------------------------------------------
	for _, dm := range reUtoipaDerive.FindAllStringSubmatchIndex(src, -1) {
		deriveList := src[dm[2]:dm[3]]
		isSchema := strings.Contains(deriveList, "ToSchema")
		isParams := reUtoipaIntoParams.MatchString(deriveList)
		if !isSchema && !isParams {
			continue
		}
		// struct name follows the derive/attribute block.
		tail := src[dm[1]:]
		if len(tail) > 800 {
			tail = tail[:800]
		}
		sm := reStructNameV.FindStringSubmatchIndex(tail)
		if sm == nil {
			continue
		}
		sname := tail[sm[2]:sm[3]]
		structKwOff := dm[1] + sm[0]

		subtype := "schema"
		if isParams {
			subtype = "params_schema"
		}

		var fields []dtoField
		if body, bodyStart, ok := rustStructBody(src, structKwOff); ok {
			fields = parseDTOFields(body, bodyStart, "")
		}

		ent := makeEntity("utoipa_schema:"+sname, "SCOPE.Schema", subtype,
			file.Path, file.Language, lineOf(src, dm[0]))
		prov := "INFERRED_FROM_UTOIPA_TOSCHEMA"
		if isParams {
			prov = "INFERRED_FROM_UTOIPA_INTOPARAMS"
		}
		setProps(&ent,
			"framework", "utoipa",
			"provenance", prov,
			"struct_name", sname,
			"field_count", itoa(len(fields)),
		)
		add(ent)

		for _, f := range fields {
			fieldEnt := makeEntity(
				"utoipa_field:"+sname+"."+f.name,
				"SCOPE.Schema", "schema_field",
				file.Path, file.Language, lineOf(src, f.offset))
			setProps(&fieldEnt,
				"framework", "utoipa",
				"provenance", "INFERRED_FROM_UTOIPA_SCHEMA_FIELD",
				"struct_name", sname,
				"field_name", f.name,
				"field_type", f.typ,
				"wire_name", f.wireName,
			)
			add(fieldEnt)
		}
	}

	// -----------------------------------------------------------------------
	// 3. The OpenApi aggregator — #[derive(OpenApi)] #[openapi(paths(...),
	// components(schemas(...)))] struct ApiDoc; — links operations + schemas.
	// -----------------------------------------------------------------------
	for _, m := range reUtoipaOpenAPIStart.FindAllStringIndex(src, -1) {
		openOff := m[1] - 1 // the '(' of #[openapi(
		inner, endOff, ok := rustBalancedParens(src, openOff)
		if !ok {
			continue
		}
		aggName := "ApiDoc"
		if am := reUtoipaAggName.FindStringSubmatchIndex(src[endOff:]); am != nil {
			aggName = src[endOff+am[2] : endOff+am[3]]
		}
		ent := makeEntity("utoipa_openapi:"+aggName, "SCOPE.Component", "openapi_doc",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "utoipa",
			"provenance", "INFERRED_FROM_UTOIPA_OPENAPI",
			"struct_name", aggName,
		)
		if pm := reUtoipaOpenAPIPaths.FindStringSubmatch(inner); pm != nil {
			paths := splitIdentList(pm[1])
			if len(paths) > 0 {
				setProps(&ent, "registered_paths", strings.Join(paths, ","),
					"path_count", itoa(len(paths)))
			}
		}
		if cm := reUtoipaOpenAPISchemas.FindStringSubmatch(inner); cm != nil {
			schemas := splitIdentList(cm[1])
			if len(schemas) > 0 {
				setProps(&ent, "registered_schemas", strings.Join(schemas, ","),
					"schema_count", itoa(len(schemas)))
			}
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// splitIdentList parses a comma-separated list of (possibly path-qualified)
// identifiers, returning the LAST path segment of each (e.g.
// `crate::users::get_user` -> `get_user`). Empty entries are dropped.
func splitIdentList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if idx := strings.LastIndex(p, "::"); idx >= 0 {
			p = p[idx+2:]
		}
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
