// NestJS endpoint Parameters + Response-type surfacing (#4325).
//
// Before #4325 the nestjs extractor emitted endpoint→DTO ACCEPTS_INPUT/RETURNS
// edges but never populated the `parameters` and `response_type` entity
// properties that the dashboard Paths panel actually reads
// (internal/dashboard/v2_paths.go → handleV2PathDetail). The live-repro on the
// real upvate-v3 controllers confirmed:
//
//   - GET /filters (device.controller.ts `filters`): Parameters (0) despite two
//     @Query('group_id')/@Query('building_id') params → @Query never surfaced.
//   - POST /notes/create (building.controller.ts `createNote`): Parameters (0)
//     despite @Body() BuildingNoteCreateBody → @Body not surfaced as a param.
//   - Both: Response shows "(none)" — the RETURNS edge was emitted (so the panel
//     COUNTED a response) but `response_type` was never set, so the type name
//     resolved to nothing. This was a property/edge split.
//
// This pass parses the handler parameter list and the trailing return-type
// annotation, then writes:
//   - `parameters` — a JSON []nestParam list (path/query/body kinds), the exact
//     wire shape the dashboard's engine.DecodeJavaParameters consumes.
//   - `response_type` — the resolved response DTO name (unwrapped from
//     Promise/Observable/Array), the property the Response row renders.
//
// The shape mirrors engine.JavaParam (name/in/type/required/default_value/
// annotations) so the dashboard renders NestJS params identically to Spring /
// JAX-RS params with no dashboard change. We deliberately re-declare the wire
// struct locally rather than importing internal/engine, to avoid a package
// dependency edge from a custom extractor into the engine.
package javascript

import (
	"encoding/json"
	"regexp"
	"strings"
)

// nestParam is the JSON wire shape consumed by the dashboard Parameters table
// (engine.JavaParam). Keep the json tags byte-identical.
type nestParam struct {
	Name         string   `json:"name"`
	In           string   `json:"in"` // path|query|body|header|cookie
	Type         string   `json:"type"`
	Required     bool     `json:"required"`
	DefaultValue string   `json:"default_value,omitempty"`
	Annotations  []string `json:"annotations,omitempty"`
	// QuotedKey is true when the decorator had a quoted binding key
	// (e.g. `@Query('id')`), meaning the param selects a single field rather
	// than the whole DTO object. Not serialized — used only to gate the
	// #4464 handler→DTO ACCEPTS_INPUT edge (a keyed param is a primitive
	// field, not a DTO type). json:"-" keeps the dashboard wire shape stable.
	QuotedKey bool `json:"-"`
}

var (
	// reNestParamDecorator matches a single param decorator + the parameter
	// declaration it annotates within a handler parameter list. Handles:
	//   @Param('id') id: string
	//   @Query('group_id', ParseIntPipe) groupId: number
	//   @Query() optional: SomeQueryDto
	//   @Body() body: SomeBodyDto
	//   @Body('field') field: string
	// Group 1 = decorator (Param|Query|Body|Headers|Header|Req|Request|...),
	// group 2 = the decorator's first quoted key (binding name) when present,
	// group 3 = the parameter identifier, group 4 = the TS type (when annotated).
	reNestParamDecorator = regexp.MustCompile(
		`@(Param|Query|Body|Headers|Header)\s*\(\s*(?:['"]([^'"]*)['"])?[^)]*\)\s*(\w+)\??\s*(?::\s*([A-Za-z_][\w<>\[\], |.]*))?`,
	)
)

// nestDecoratorIn maps a NestJS param decorator to its OpenAPI `in` location.
var nestDecoratorIn = map[string]string{
	"Param":   "path",
	"Query":   "query",
	"Body":    "body",
	"Header":  "header",
	"Headers": "header",
}

// extractNestHandlerParams parses a handler parameter block (the text between
// the method's `(` and matching `)`) into the ordered parameter list the
// dashboard renders. Decorator-less parameters (e.g. `@Request() req`) and
// framework-internal injections are skipped — only request-shaping decorators
// (@Param/@Query/@Body/@Header) become Parameters rows.
//
// For @Query()/@Body() with NO quoted key, the binding name falls back to the
// parameter identifier (the conventional spelling for a whole-DTO query/body).
// For @Body the Type is the DTO so the Parameters table and the ACCEPTS_INPUT
// edge agree.
func extractNestHandlerParams(paramsBlock string) []nestParam {
	if paramsBlock == "" {
		return nil
	}
	var out []nestParam
	for _, m := range reNestParamDecorator.FindAllStringSubmatch(paramsBlock, -1) {
		dec := m[1]
		key := strings.TrimSpace(m[2])
		ident := strings.TrimSpace(m[3])
		rawType := nestCleanParamType(m[4])

		in := nestDecoratorIn[dec]
		if in == "" {
			continue
		}
		name := key
		if name == "" {
			// @Query()/@Body() whole-object: name after the binding identifier.
			name = ident
		}
		if name == "" {
			continue
		}

		p := nestParam{
			Name:        name,
			In:          in,
			Annotations: []string{"@" + dec},
			QuotedKey:   key != "",
		}
		// Type: prefer the user DTO (unwrapped); fall back to the raw TS type
		// for primitives so the row still shows `number`/`string`.
		if rawType != "" {
			if dto := nestUnwrapType(rawType); dto != "" {
				p.Type = dto
			} else {
				p.Type = strings.TrimSpace(strings.SplitN(rawType, "|", 2)[0])
			}
		}
		// Path params are always required; a `?:` parameter (optional) or a
		// query param is best-effort optional unless we can prove otherwise.
		if in == "path" {
			p.Required = true
		}
		out = append(out, p)
	}
	return out
}

// nestCleanParamType trims a captured TS type annotation to the single
// parameter's type. The capture char class permits `,` (for generic args like
// Map<string, number>) which can over-consume into the next parameter, so we
// cut at the first top-level comma (depth-0, outside <>/[]). Trailing
// whitespace/commas are stripped.
func nestCleanParamType(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	depth := 0
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '<', '[':
			depth++
		case '>', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				raw = raw[:i]
				goto done
			}
		}
	}
done:
	return strings.TrimRight(strings.TrimSpace(raw), ", ")
}

// encodeNestParams marshals the parameter list to the canonical JSON the
// dashboard decodes. Empty input → "" so the property is omitted.
func encodeNestParams(ps []nestParam) string {
	if len(ps) == 0 {
		return ""
	}
	b, err := json.Marshal(ps)
	if err != nil {
		return ""
	}
	return string(b)
}
