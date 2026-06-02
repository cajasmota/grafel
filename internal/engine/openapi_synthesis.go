package engine

import (
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"gopkg.in/yaml.v3"
)

// openAPIEmit is the callback shape used by synthesizeOpenAPI. It hands the
// caller everything needed to emit a canonical http_endpoint_definition plus
// spec-only provenance properties:
//
//   - method        — upper-cased HTTP verb (GET, POST, …)
//   - canonicalPath — the path already run through httproutes.Canonicalize, so
//     the resulting synthetic ID converges with code-extracted routes
//   - opID          — operationId (may be empty)
//   - summary       — operation summary (may be empty)
//   - reqRef        — schema name referenced by requestBody (may be empty),
//     e.g. "CreateUser" from `$ref: '#/components/schemas/CreateUser'`
//   - respRefs      — sorted, de-duplicated schema names referenced by
//     responses (may be empty)
type openAPIEmit func(method, canonicalPath, opID, summary, reqRef string, respRefs []string)

// openAPIMethods are the HTTP verbs recognised as operation keys under a
// `paths.<path>` object. `trace`/`head`/`options` are included for
// completeness; anything else under a path (parameters, summary, $ref,
// servers, x-*) is skipped.
var openAPIMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// isOpenAPISpec reports whether the raw document looks like an OpenAPI 3.x or
// Swagger 2.0 specification. The cheap content sniff requires a top-level
// `openapi:`/`swagger:` version key AND a `paths:` block — the two anchors that
// together are extremely unlikely to co-occur in a non-spec YAML/JSON file.
// This keeps the synthesizer a no-op for ordinary config YAML (CI files,
// docker-compose, k8s manifests) that the yaml/json classifier also routes
// here.
func isOpenAPISpec(root map[string]any) bool {
	if root == nil {
		return false
	}
	_, hasPaths := root["paths"]
	if !hasPaths {
		return false
	}
	if _, ok := root["openapi"]; ok {
		return true
	}
	if _, ok := root["swagger"]; ok {
		return true
	}
	return false
}

// synthesizeOpenAPI parses an OpenAPI 3.x / Swagger 2.0 spec document and
// invokes emit once per `paths.<path>.<method>` operation. Each path is
// canonicalised through httproutes (FastAPI shares OpenAPI's `{name}`
// curly-brace param syntax) so the synthetic ID built downstream
// (`http:<VERB>:<canonical-path>`) is IDENTICAL to the one a code extractor
// produces for the same route — the spec endpoint therefore CONVERGES on,
// rather than duplicates, the code-extracted endpoint.
//
// yaml.v3 parses JSON too (JSON is a YAML subset), so a single decode path
// covers openapi.yaml / openapi.json / swagger.json / swagger.yaml. Files that
// don't sniff as a spec are a clean no-op.
func synthesizeOpenAPI(content string, emit openAPIEmit) {
	if !strings.Contains(content, "paths") {
		return
	}
	var root map[string]any
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return
	}
	if !isOpenAPISpec(root) {
		return
	}
	paths, ok := root["paths"].(map[string]any)
	if !ok {
		return
	}

	// Deterministic emission order so re-indexing the same spec produces a
	// stable entity sequence (and stable StartLine attribution downstream).
	pathKeys := make([]string, 0, len(paths))
	for p := range paths {
		pathKeys = append(pathKeys, p)
	}
	sort.Strings(pathKeys)

	for _, rawPath := range pathKeys {
		item, ok := paths[rawPath].(map[string]any)
		if !ok {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, rawPath)
		if canonical == "" || canonical == "/" && rawPath != "/" {
			continue
		}

		methodKeys := make([]string, 0, len(item))
		for m := range item {
			methodKeys = append(methodKeys, m)
		}
		sort.Strings(methodKeys)

		for _, m := range methodKeys {
			method := strings.ToLower(m)
			if !openAPIMethods[method] {
				continue
			}
			op, ok := item[m].(map[string]any)
			if !ok {
				continue
			}
			opID, _ := op["operationId"].(string)
			summary, _ := op["summary"].(string)
			reqRef := requestBodySchemaRef(op)
			respRefs := responseSchemaRefs(op)
			emit(strings.ToUpper(method), canonical, opID, summary, reqRef, respRefs)
		}
	}
}

// requestBodySchemaRef returns the schema name referenced by an operation's
// requestBody, e.g. "CreateUser" from
// `requestBody.content."application/json".schema.$ref:
// '#/components/schemas/CreateUser'`. Returns "" when there is no $ref.
func requestBodySchemaRef(op map[string]any) string {
	body, ok := op["requestBody"].(map[string]any)
	if !ok {
		return ""
	}
	content, ok := body["content"].(map[string]any)
	if !ok {
		return ""
	}
	// Deterministic: prefer the lexicographically-first media type carrying a
	// schema $ref (typically application/json).
	mtKeys := make([]string, 0, len(content))
	for k := range content {
		mtKeys = append(mtKeys, k)
	}
	sort.Strings(mtKeys)
	for _, mt := range mtKeys {
		media, ok := content[mt].(map[string]any)
		if !ok {
			continue
		}
		if ref := schemaRefName(media["schema"]); ref != "" {
			return ref
		}
	}
	return ""
}

// responseSchemaRefs returns the sorted, de-duplicated set of schema names
// referenced across an operation's responses (any status code, any media type).
func responseSchemaRefs(op map[string]any) []string {
	responses, ok := op["responses"].(map[string]any)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for _, resp := range responses {
		r, ok := resp.(map[string]any)
		if !ok {
			continue
		}
		// OpenAPI 3.x: responses.<code>.content.<media>.schema.$ref
		if content, ok := r["content"].(map[string]any); ok {
			for _, media := range content {
				mm, ok := media.(map[string]any)
				if !ok {
					continue
				}
				if ref := schemaRefName(mm["schema"]); ref != "" {
					seen[ref] = true
				}
			}
		}
		// Swagger 2.0: responses.<code>.schema.$ref
		if ref := schemaRefName(r["schema"]); ref != "" {
			seen[ref] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// schemaRefName extracts the trailing schema name from a `$ref` pointer inside
// a schema object. It recognises both OpenAPI 3.x
// (`#/components/schemas/Name`) and Swagger 2.0 (`#/definitions/Name`) forms,
// and unwraps a one-level `items.$ref` for array schemas. Returns "" when the
// node carries no recognisable schema reference.
func schemaRefName(node any) string {
	schema, ok := node.(map[string]any)
	if !ok {
		return ""
	}
	if ref, ok := schema["$ref"].(string); ok {
		if name := refTail(ref); name != "" {
			return name
		}
	}
	// Array schema: { type: array, items: { $ref: ... } }
	if items, ok := schema["items"].(map[string]any); ok {
		if ref, ok := items["$ref"].(string); ok {
			if name := refTail(ref); name != "" {
				return name
			}
		}
	}
	return ""
}

// refTail returns the final path segment of a JSON-pointer $ref that targets a
// components/schemas or definitions entry, e.g.
// "#/components/schemas/CreateUser" → "CreateUser". Returns "" for refs that
// don't point at a schema/definition.
func refTail(ref string) string {
	const (
		oas3 = "#/components/schemas/"
		sw2  = "#/definitions/"
	)
	switch {
	case strings.HasPrefix(ref, oas3):
		return strings.TrimPrefix(ref, oas3)
	case strings.HasPrefix(ref, sw2):
		return strings.TrimPrefix(ref, sw2)
	}
	return ""
}
