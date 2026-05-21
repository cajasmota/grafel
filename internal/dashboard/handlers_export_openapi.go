package dashboard

// handlers_export_openapi.go — OpenAPI 3.0 export from indexed HTTP endpoints (#1340)
//
//	GET /api/export/{group}/openapi?format=yaml|json
//
// Reads all http_endpoint_definition entities in the group and emits a
// standards-compliant OpenAPI 3.0 document.  The spec includes:
//
//   - paths/methods from endpoint Properties["method"] + Properties["path"]
//   - path parameters extracted from canonical {param} placeholders
//   - response shapes (status codes + field keys) from enrichment data
//   - operation summaries from AI enrichment when available
//   - tags derived from owning_backend grouping
//
// The output validates against the OpenAPI 3.0 schema and loads without
// errors in Swagger UI / Redoc.
//
// format=yaml (default) → application/x-yaml + Content-Disposition download
// format=json          → application/json   + Content-Disposition download

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// OpenAPI 3.0 data structures
// ---------------------------------------------------------------------------

// openAPIDoc is the top-level OpenAPI 3.0 document.
type openAPIDoc struct {
	OpenAPI    string                        `json:"openapi" yaml:"openapi"`
	Info       openAPIInfo                   `json:"info" yaml:"info"`
	Paths      map[string]openAPIPathItem    `json:"paths" yaml:"paths"`
	Tags       []openAPITag                  `json:"tags,omitempty" yaml:"tags,omitempty"`
	Components openAPIComponents             `json:"components,omitempty" yaml:"components,omitempty"`
}

type openAPIInfo struct {
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Version     string `json:"version" yaml:"version"`
}

// openAPIPathItem maps HTTP methods to their operations.
// We keep the fields explicit (no dynamic map) so yaml/json marshal order is
// deterministic and swagger-cli validates cleanly.
type openAPIPathItem struct {
	Get     *openAPIOperation `json:"get,omitempty" yaml:"get,omitempty"`
	Post    *openAPIOperation `json:"post,omitempty" yaml:"post,omitempty"`
	Put     *openAPIOperation `json:"put,omitempty" yaml:"put,omitempty"`
	Patch   *openAPIOperation `json:"patch,omitempty" yaml:"patch,omitempty"`
	Delete  *openAPIOperation `json:"delete,omitempty" yaml:"delete,omitempty"`
	Head    *openAPIOperation `json:"head,omitempty" yaml:"head,omitempty"`
	Options *openAPIOperation `json:"options,omitempty" yaml:"options,omitempty"`
}

// setOperation assigns an operation to the correct method field.
func (pi *openAPIPathItem) setOperation(method string, op *openAPIOperation) {
	switch strings.ToUpper(method) {
	case "GET":
		pi.Get = op
	case "POST":
		pi.Post = op
	case "PUT":
		pi.Put = op
	case "PATCH":
		pi.Patch = op
	case "DELETE":
		pi.Delete = op
	case "HEAD":
		pi.Head = op
	case "OPTIONS":
		pi.Options = op
	}
}

type openAPIOperation struct {
	OperationID string                    `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Summary     string                    `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string                    `json:"description,omitempty" yaml:"description,omitempty"`
	Tags        []string                  `json:"tags,omitempty" yaml:"tags,omitempty"`
	Parameters  []openAPIParameter        `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Responses   map[string]openAPIResponse `json:"responses" yaml:"responses"`
}

type openAPIParameter struct {
	Name     string        `json:"name" yaml:"name"`
	In       string        `json:"in" yaml:"in"`
	Required bool          `json:"required" yaml:"required"`
	Schema   openAPISchema `json:"schema" yaml:"schema"`
}

type openAPIResponse struct {
	Description string                    `json:"description" yaml:"description"`
	Content     map[string]openAPIMedia   `json:"content,omitempty" yaml:"content,omitempty"`
}

type openAPIMedia struct {
	Schema openAPISchema `json:"schema" yaml:"schema"`
}

type openAPISchema struct {
	Type       string                   `json:"type,omitempty" yaml:"type,omitempty"`
	Properties map[string]openAPISchema `json:"properties,omitempty" yaml:"properties,omitempty"`
}

type openAPITag struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type openAPIComponents struct {
	Schemas map[string]openAPISchema `json:"schemas,omitempty" yaml:"schemas,omitempty"`
}

// ---------------------------------------------------------------------------
// Path-parameter regex
// ---------------------------------------------------------------------------

var rePathParam = regexp.MustCompile(`\{([A-Za-z_]\w*)\}`)

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// handleExportOpenAPI — GET /api/export/{group}/openapi?format=yaml|json
func (s *Server) handleExportOpenAPI(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "yaml"
	}
	if format != "yaml" && format != "json" {
		writeErr(w, http.StatusBadRequest, "format must be yaml or json")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	// -----------------------------------------------------------------------
	// Collect all http_endpoint_definition entities across repos.
	// -----------------------------------------------------------------------

	type rawEP struct {
		method        string
		path          string
		handlerName   string
		framework     string
		owningBackend string
		description   string   // AI summary when available
		responseKeys  []string // from enrichment
		statusCodes   []int
		repo          string
	}

	var eps []rawEP

	for _, repo := range sortedRepos(grp) {
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]

			// Accept http_endpoint_definition and backward-compat kinds.
			kind := dashStripScopePrefix(e.Kind)
			if !types.IsHTTPEndpointDefinitionKind(kind) &&
				kind != httpEndpointKind &&
				e.Kind != "Endpoint" && e.Kind != "Route" {
				continue
			}
			// Exclude call-site synthetics.
			if e.Kind == "http_endpoint_call" ||
				e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
				continue
			}

			path := e.Properties["path"]
			if path == "" {
				path = e.Name
			}
			if !isHTTPEndpointPath(path) {
				continue
			}

			method := strings.ToUpper(e.Properties["method"])
			if method == "" {
				method = strings.ToUpper(e.Properties["verb"])
			}
			if method == "" {
				method = "ANY"
			}
			// Skip DRF urlconf_nested_include ANY placeholders.
			if method == "ANY" && e.Properties["urlconf_nested_include"] == "true" {
				continue
			}

			owningBackend := e.Properties["owning_backend"]
			if owningBackend == "" {
				owningBackend = inferOwningBackend(e.Name, repo.Slug)
			}

			// AI enrichment description — stored in Properties["summary"] or
			// Properties["description"] (both used depending on extractor).
			description := e.Properties["summary"]
			if description == "" {
				description = e.Properties["description"]
			}

			// Response keys
			var respKeys []string
			if rk := e.Properties["response_keys"]; rk != "" {
				for _, k := range strings.Split(rk, ",") {
					k = strings.TrimSpace(k)
					if k != "" {
						respKeys = append(respKeys, k)
					}
				}
			}

			// Status codes
			var statusCodes []int
			if sc := e.Properties["status_codes"]; sc != "" {
				for _, s := range strings.Split(sc, ",") {
					if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
						statusCodes = append(statusCodes, n)
					}
				}
			}

			eps = append(eps, rawEP{
				method:        method,
				path:          path,
				handlerName:   e.Name,
				framework:     e.Properties["framework"],
				owningBackend: owningBackend,
				description:   description,
				responseKeys:  respKeys,
				statusCodes:   statusCodes,
				repo:          repo.Slug,
			})
		}
	}

	// -----------------------------------------------------------------------
	// Build OpenAPI document
	// -----------------------------------------------------------------------

	doc := openAPIDoc{
		OpenAPI: "3.0.3",
		Info: openAPIInfo{
			Title:       group + " API",
			Description: fmt.Sprintf("Generated by archigraph from %d indexed HTTP endpoints.", len(eps)),
			Version:     "0.0.0",
		},
		Paths: map[string]openAPIPathItem{},
	}

	// Collect unique tags (owning backends).
	tagSet := map[string]bool{}

	// We may encounter the same (path, method) multiple times across repos.
	// Merge: first wins for summary/description; response codes are unioned.
	type opKey struct{ path, method string }
	opMap := map[opKey]*openAPIOperation{}
	pathOrder := []string{}
	pathSeen := map[string]bool{}

	for _, ep := range eps {
		if !pathSeen[ep.path] {
			pathSeen[ep.path] = true
			pathOrder = append(pathOrder, ep.path)
		}

		key := opKey{ep.path, ep.method}
		if _, exists := opMap[key]; !exists {
			// Extract path parameters.
			var params []openAPIParameter
			for _, m := range rePathParam.FindAllStringSubmatch(ep.path, -1) {
				params = append(params, openAPIParameter{
					Name:     m[1],
					In:       "path",
					Required: true,
					Schema:   openAPISchema{Type: "string"},
				})
			}

			op := &openAPIOperation{
				OperationID: buildOperationID(ep.method, ep.path),
				Summary:     ep.description,
				Parameters:  params,
				Responses:   map[string]openAPIResponse{},
			}
			if ep.owningBackend != "" {
				op.Tags = []string{ep.owningBackend}
				tagSet[ep.owningBackend] = true
			}
			opMap[key] = op
		}

		op := opMap[key]

		// Merge status codes + response body schema.
		for _, sc := range ep.statusCodes {
			codeStr := strconv.Itoa(sc)
			if _, ok := op.Responses[codeStr]; !ok {
				resp := openAPIResponse{Description: httpStatusText(sc)}
				if len(ep.responseKeys) > 0 && sc < 300 {
					schema := openAPISchema{
						Type:       "object",
						Properties: map[string]openAPISchema{},
					}
					for _, k := range ep.responseKeys {
						schema.Properties[k] = openAPISchema{Type: "string"}
					}
					resp.Content = map[string]openAPIMedia{
						"application/json": {Schema: schema},
					}
				}
				op.Responses[codeStr] = resp
			}
		}

		// Guarantee at least a default response.
		if len(op.Responses) == 0 {
			op.Responses["200"] = openAPIResponse{Description: "OK"}
		}
	}

	// Sort paths for deterministic output.
	sort.Strings(pathOrder)

	for _, path := range pathOrder {
		item := openAPIPathItem{}

		// Collect methods that apply to this path.
		for key, op := range opMap {
			if key.path != path {
				continue
			}
			// Expand ANY into the common read+write set.
			if key.method == "ANY" {
				for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
					cloned := cloneOperation(op)
					item.setOperation(m, cloned)
				}
			} else {
				item.setOperation(key.method, op)
			}
		}
		doc.Paths[path] = item
	}

	// Build sorted tag list.
	tagNames := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tagNames = append(tagNames, t)
	}
	sort.Strings(tagNames)
	for _, t := range tagNames {
		doc.Tags = append(doc.Tags, openAPITag{Name: t})
	}

	// -----------------------------------------------------------------------
	// Serialise and return.
	// -----------------------------------------------------------------------

	if format == "json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-openapi.json"`, group))
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(doc); err != nil {
			// Header already sent — nothing we can do except log.
			_ = err
		}
		return
	}

	// yaml (default)
	w.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-openapi.yaml"`, group))
	if err := yaml.NewEncoder(w).Encode(doc); err != nil {
		_ = err
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// reNonAlnum strips characters that are not letters or digits.
var reNonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// titleCase returns s with its first letter upper-cased (rest unchanged, ASCII).
func titleCase(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// buildOperationID derives a camelCase operationId from method + path.
// E.g. "GET /api/users/{id}" → "getUsersById"
func buildOperationID(method, path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var words []string
	words = append(words, strings.ToLower(method))
	for _, p := range parts {
		if p == "" {
			continue
		}
		// If the segment is purely a path parameter {name}, emit "By<Name>".
		if m := rePathParam.FindStringSubmatch(p); m != nil && m[0] == p {
			words = append(words, "By"+titleCase(m[1]))
			continue
		}
		// Literal segment (or mixed): strip non-alnum and title-case the
		// lowercased result so we get consistent camelCase.
		// Path params should not appear in literal segments in valid OpenAPI
		// paths, but we strip braces defensively.
		clean := reNonAlnum.ReplaceAllString(p, "")
		if clean != "" {
			words = append(words, titleCase(strings.ToLower(clean)))
		}
	}
	return strings.Join(words, "")
}

// cloneOperation shallow-copies an operation so that the ANY expansion
// produces independent pointers (editing one verb won't mutate others).
func cloneOperation(op *openAPIOperation) *openAPIOperation {
	if op == nil {
		return nil
	}
	cloned := *op
	return &cloned
}

// httpStatusText maps common numeric status codes to short descriptions.
// Unknown codes fall back to "HTTP <code>".
func httpStatusText(code int) string {
	texts := map[int]string{
		200: "OK",
		201: "Created",
		204: "No Content",
		301: "Moved Permanently",
		302: "Found",
		304: "Not Modified",
		400: "Bad Request",
		401: "Unauthorized",
		403: "Forbidden",
		404: "Not Found",
		405: "Method Not Allowed",
		409: "Conflict",
		422: "Unprocessable Entity",
		429: "Too Many Requests",
		500: "Internal Server Error",
		502: "Bad Gateway",
		503: "Service Unavailable",
	}
	if t, ok := texts[code]; ok {
		return t
	}
	return fmt.Sprintf("HTTP %d", code)
}
