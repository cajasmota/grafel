package dashboard

// handlers_orphan_callers.go — Paths v2: orphan-caller detector (#1091).
//
//	GET /api/paths/{group}/orphan-callers
//
// Returns frontend FETCHES call sites where the URL does not resolve to any
// backend http_endpoint entity in the indexed group. Each row is a repair
// candidate that can be fed into the existing apply/reject flow (#1010).
//
// Detection logic:
//  1. Walk every FETCHES relationship across all repos in the group.
//  2. Check whether the edge's ToID resolves to an http_endpoint entity in
//     any repo's graph. If not — or if the target entity itself is a
//     consumer-side synthetic without a matched producer — the caller is
//     orphaned.
//  3. Classify the reason:
//     - "dynamic_baseurl"  — target entity has runtime_dynamic=true or
//       the relationship carries a dynamic env-var baseURL signal.
//     - "template_literal" — the url_pattern contains a template-literal
//       placeholder (${…} or {…}) that prevented static resolution.
//     - "no_handler_found" — none of the above; the path simply has no
//       matching producer endpoint anywhere in the group.
//  4. Group by repo + file + line; deduplicate by (caller_id, url_pattern).

import (
	"net/http"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// OrphanCallerRow is one unresolved call site returned by the endpoint.
type OrphanCallerRow struct {
	ID                  string `json:"id"`
	CallerFile          string `json:"caller_file"`
	CallerLine          int    `json:"caller_line"`
	URLPattern          string `json:"url_pattern"`
	Method              string `json:"method"`
	Reason              string `json:"reason"`
	SuggestedRepairKind string `json:"suggested_repair_kind"`
	Repo                string `json:"repo"`
}

// orphanCallerReason classifies why a FETCHES edge could not be resolved.
type orphanCallerReason string

const (
	reasonDynamicBaseURL  orphanCallerReason = "dynamic_baseurl"
	reasonTemplateLiteral orphanCallerReason = "template_literal"
	reasonNoHandlerFound  orphanCallerReason = "no_handler_found"
)

// suggestedRepair maps reason → repair kind hint returned to callers.
var suggestedRepair = map[orphanCallerReason]string{
	reasonDynamicBaseURL:  "annotate_baseurl",
	reasonTemplateLiteral: "resolve_template_url",
	reasonNoHandlerFound:  "add_missing_handler",
}

// handleOrphanCallers — GET /api/paths/{group}/orphan-callers
//
// Returns the list of FETCHES edges that have no matching http_endpoint
// producer in the group. The response is a JSON object:
//
//	{ "orphan_callers": [...], "total": N }
func (s *Server) handleOrphanCallers(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	rows := collectOrphanCallers(grp)

	writeJSON(w, http.StatusOK, map[string]any{
		"callers": rows,
		"total":   len(rows),
	})
}

// collectOrphanCallers runs the orphan-detection pass against a loaded group.
// It is extracted so unit tests can call it without HTTP scaffolding.
func collectOrphanCallers(grp *DashGroup) []OrphanCallerRow {
	// Build a set of all http_endpoint entity IDs across the group —
	// keyed by the bare local ID. We also index by prefixed ID for
	// cross-repo lookups.
	type endpointEntry struct {
		repo       string
		isProducer bool // pattern_type != "http_endpoint_client_synthesis"
	}
	endpointByID := make(map[string]endpointEntry, 256) // bare ID → entry
	// Also build a map of bare entity ID → properties for fast reason lookup.
	entityProps := make(map[string]map[string]string, 256)

	for _, repo := range sortedRepos(grp) {
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			bare := e.ID
			prefixed := dashPrefixedID(repo.Slug, bare)

			// #1217 backward compat: accept all three http endpoint kind strings.
			bareKind := dashStripScopePrefix(e.Kind)
			isHTTPEndpoint := types.IsHTTPEndpointKind(bareKind) ||
				strings.EqualFold(bareKind, httpEndpointKind)
			if !isHTTPEndpoint {
				continue
			}
			// #1217: http_endpoint_definition is always producer-side;
			// http_endpoint_call is always consumer-side.
			// For legacy http_endpoint, fall back to pattern_type check.
			isProducer := e.Kind == "http_endpoint_definition" ||
				(e.Kind != "http_endpoint_call" &&
					e.PropGet("pattern_type") != "http_endpoint_client_synthesis")

			endpointByID[bare] = endpointEntry{repo: repo.Slug, isProducer: isProducer}
			endpointByID[prefixed] = endpointEntry{repo: repo.Slug, isProducer: isProducer}

			if e.PropLen() > 0 {
				entityProps[bare] = e.PropsSnapshot()
				entityProps[prefixed] = e.PropsSnapshot()
			}
		}
	}

	// dedupKey prevents emitting duplicate rows for (callerID, urlPattern).
	type dedupKey struct {
		callerID   string
		urlPattern string
	}
	seen := make(map[dedupKey]bool, 64)

	var rows []OrphanCallerRow

	for _, repo := range sortedRepos(grp) {
		// Build a fast lookup of entity ID → entity for this repo so we
		// can resolve the caller (FromID) to its source_file / start_line.
		entityByID := make(map[string]int, len(repo.Doc.Entities))
		for i := range repo.Doc.Entities {
			entityByID[repo.Doc.Entities[i].ID] = i
		}

		for _, rel := range repo.Doc.Relationships {
			if rel.Kind != "FETCHES" {
				continue
			}

			toID := rel.ToID
			fromID := rel.FromID

			// Determine whether the target resolves to a producer-side
			// http_endpoint anywhere in the group.
			targetEntry, targetExists := endpointByID[toID]
			if !targetExists {
				// Try prefixed form (e.g. when cross-repo phantom edges
				// embed the slug).
				targetEntry, targetExists = endpointByID[dashPrefixedID(repo.Slug, toID)]
			}

			// The caller is orphaned when:
			// (a) no entity with toID exists at all, OR
			// (b) the entity exists but is itself a consumer-side synthetic
			//     with no producer match (i.e. both sides are "client synthesis").
			isOrphan := !targetExists || !targetEntry.isProducer

			if !isOrphan {
				continue
			}

			// Resolve the caller entity (fromID) so we can emit file + line.
			callerIdx, callerKnown := entityByID[fromID]
			var callerFile string
			var callerLine int
			if callerKnown {
				ce := &repo.Doc.Entities[callerIdx]
				callerFile = ce.SourceFile
				callerLine = ce.StartLine
			}

			// Extract url_pattern and method from the relationship properties
			// (set by makeRuntimeEmit in http_endpoint_synthesis.go) or from
			// the target entity's properties if the relationship is bare.
			urlPattern := ""
			method := "GET"
			if rel.PropLen() > 0 {
				if v := rel.PropGet("path"); v != "" {
					urlPattern = v
				}
				if v := rel.PropGet("verb"); v != "" {
					method = strings.ToUpper(v)
				}
			}
			// Fall back to target entity properties when the rel has none.
			if urlPattern == "" {
				if props := entityProps[toID]; props != nil {
					urlPattern = props["path"]
					if v := props["verb"]; v != "" {
						method = strings.ToUpper(v)
					}
				}
			}

			// Classify the reason.
			reason := classifyOrphanReason(toID, urlPattern, entityProps)

			callerID := dashPrefixedID(repo.Slug, fromID)

			dk := dedupKey{callerID: callerID, urlPattern: urlPattern}
			if seen[dk] {
				continue
			}
			seen[dk] = true

			repairKind := suggestedRepair[reason]

			rows = append(rows, OrphanCallerRow{
				ID:                  callerID,
				CallerFile:          callerFile,
				CallerLine:          callerLine,
				URLPattern:          urlPattern,
				Method:              method,
				Reason:              string(reason),
				SuggestedRepairKind: repairKind,
				Repo:                repo.Slug,
			})
		}
	}

	// Stable deterministic sort: repo → file → line → url_pattern.
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		if a.CallerFile != b.CallerFile {
			return a.CallerFile < b.CallerFile
		}
		if a.CallerLine != b.CallerLine {
			return a.CallerLine < b.CallerLine
		}
		return a.URLPattern < b.URLPattern
	})

	if rows == nil {
		rows = []OrphanCallerRow{}
	}
	return rows
}

// classifyOrphanReason determines why a FETCHES edge has no resolved handler.
// It checks the target entity's known properties (if any) and the URL pattern
// itself for template-literal placeholders.
func classifyOrphanReason(toID, urlPattern string, entityProps map[string]map[string]string) orphanCallerReason {
	// Check entity-level dynamic flags first.
	if props := entityProps[toID]; props != nil {
		if props["runtime_dynamic"] == "true" {
			return reasonDynamicBaseURL
		}
		if props["dynamic_baseurl"] == "true" {
			return reasonDynamicBaseURL
		}
	}

	// Template-literal placeholder in the URL pattern: ${…} (JS) or {…} (generic).
	if urlPattern != "" && containsTemplatePlaceholder(urlPattern) {
		return reasonTemplateLiteral
	}

	return reasonNoHandlerFound
}

// containsTemplatePlaceholder reports whether a URL pattern contains a
// template-literal style variable reference: ${name} (JavaScript) or
// {name} (URL template / Python f-string / Go fmt.Sprintf).
func containsTemplatePlaceholder(s string) bool {
	// JS-style: ${…}
	if strings.Contains(s, "${") {
		return true
	}
	// Generic placeholder: {…}  — but only when it is NOT the very first
	// segment (leading {…} is classified as dynamic_baseurl by the synthesis
	// pass, so we avoid double-classification here).
	idx := strings.Index(s, "{")
	if idx < 0 {
		return false
	}
	// If the placeholder is at position 0 or 1 (after leading "/"), it is
	// the leading-path-placeholder pattern handled above — skip.
	leadingOK := idx > 1
	return leadingOK && strings.Contains(s[idx:], "}")
}
