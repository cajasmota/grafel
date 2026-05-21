package dashboard

// handlers_security.go — Security & quality HTTP surface (#1330).
//
// Three routes:
//
//	GET /api/security/auth-coverage/{group}   — auth coverage for HTTP endpoints
//	GET /api/security/secrets/{group}         — hardcoded secrets + SM integrations
//	GET /api/security/cycles/{group}          — import-cycle findings
//
// All routes follow the same pattern as handlers_nplus1.go: load each repo's
// graph document, run the graph-package detector, aggregate, return JSON.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ─────────────────────────────────────────────────────────────────────────────
// Auth-coverage wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// AuthEndpointFinding is a single HTTP endpoint finding.
type AuthEndpointFinding struct {
	EntityID     string `json:"entity_id"`
	Name         string `json:"name"`
	Repo         string `json:"repo"`
	SourceFile   string `json:"source_file,omitempty"`
	StartLine    int    `json:"start_line,omitempty"`
	Method       string `json:"method,omitempty"`
	Path         string `json:"path,omitempty"`
	HasAuth      bool   `json:"has_auth"`
	AuthEvidence string `json:"auth_evidence,omitempty"`
	Severity     string `json:"severity"` // "error" | "warn" | "info"
	SensitiveOp  bool   `json:"sensitive_op,omitempty"`
	IDORRisk     bool   `json:"idor_risk,omitempty"`
}

// GroupAuthCoverageReport is the wire shape for GET /api/security/auth-coverage/{group}.
type GroupAuthCoverageReport struct {
	Group           string                `json:"group"`
	TotalEndpoints  int                   `json:"total_endpoints"`
	CoveredCount    int                   `json:"covered_count"`
	UncoveredCount  int                   `json:"uncovered_count"`
	CoveragePct     float64               `json:"coverage_pct"`
	ErrorCount      int                   `json:"error_count"`
	WarnCount       int                   `json:"warn_count"`
	InfoCount       int                   `json:"info_count"`
	Findings        []AuthEndpointFinding `json:"findings"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Secrets wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// SecuritySecretFinding is a single secret-related finding returned by
// GET /api/security/secrets/{group}.
type SecuritySecretFinding struct {
	EntityID   string `json:"entity_id"`
	Name       string `json:"name"`
	Repo       string `json:"repo"`
	SourceFile string `json:"source_file,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	Language   string `json:"language,omitempty"`
	// Category: "hardcoded_credential" | "secrets_management"
	Category string `json:"category"`
	// Provider is set for secrets_management findings (e.g. "vault", "aws_secrets_manager")
	Provider    string `json:"provider,omitempty"`
	Severity    string `json:"severity"` // "error" | "warn" | "info"
	Remediation string `json:"remediation,omitempty"`
}

// GroupSecretsReport is the wire shape for GET /api/security/secrets/{group}.
type GroupSecretsReport struct {
	Group          string          `json:"group"`
	TotalFindings  int             `json:"total_findings"`
	ErrorCount     int             `json:"error_count"`
	WarnCount      int             `json:"warn_count"`
	InfoCount      int             `json:"info_count"`
	ByCategory     map[string]int  `json:"by_category"`
	Findings       []SecuritySecretFinding `json:"findings"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Cycles wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// CycleFindingEdge is a directed edge within a cycle.
type CycleFindingEdge struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
}

// CycleFinding wraps graph.ImportCycle with a repo annotation.
type CycleFinding struct {
	Repo                  string             `json:"repo"`
	Members               []string           `json:"members"`
	Edges                 []CycleFindingEdge `json:"edges"`
	WeakestLinkFromID     string             `json:"weakest_link_from_id,omitempty"`
	WeakestLinkToID       string             `json:"weakest_link_to_id,omitempty"`
	SuggestedExtractionID string             `json:"suggested_extraction_id,omitempty"`
	Size                  int                `json:"size"`
	Severity              string             `json:"severity"` // "error" (>5 members) | "warn" (3-5) | "info" (2)
}

// GroupCyclesReport is the wire shape for GET /api/security/cycles/{group}.
type GroupCyclesReport struct {
	Group         string         `json:"group"`
	TotalCycles   int            `json:"total_cycles"`
	ErrorCount    int            `json:"error_count"`
	WarnCount     int            `json:"warn_count"`
	InfoCount     int            `json:"info_count"`
	Findings      []CycleFinding `json:"findings"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Auth-coverage helpers (mirror mcp/auth_coverage.go logic for HTTP surface)
// ─────────────────────────────────────────────────────────────────────────────

var sensitiveTerms = []string{
	"payment", "checkout", "password", "delete", "admin",
	"write", "create", "update", "register", "reset",
}

func isEndpointKind(kind string) bool {
	return kind == "http_endpoint_definition" ||
		kind == "SCOPE.Endpoint" ||
		strings.HasSuffix(kind, ".Endpoint")
}

func hasAuthProperty(e *graph.Entity) (bool, string) {
	for _, k := range []string{"auth_decorator", "auth_middleware", "auth_annotation"} {
		if v := e.Properties[k]; v != "" && v != "false" {
			return true, k + "=" + v
		}
	}
	return false, ""
}

func buildAuthPoliciesByFileHTTP(doc *graph.Document) map[string][]string {
	m := make(map[string][]string)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Properties["subtype"] == "auth_policy" || e.Subtype == "auth_policy" {
			m[e.SourceFile] = append(m[e.SourceFile], e.ID)
		}
	}
	return m
}

func buildTaggedAuthIDsHTTP(doc *graph.Document) map[string]bool {
	tagged := make(map[string]bool)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind == "TAGGED_AS" {
			// Check if the target is an auth_policy entity
			for j := range doc.Entities {
				if doc.Entities[j].ID == r.ToID {
					if doc.Entities[j].Subtype == "auth_policy" ||
						doc.Entities[j].Properties["subtype"] == "auth_policy" {
						tagged[r.FromID] = true
					}
					break
				}
			}
		}
	}
	return tagged
}

func determineEndpointAuth(e *graph.Entity, byFile map[string][]string, taggedIDs map[string]bool) (bool, string) {
	// Signal 1: auth_policy entity in same source file
	if policies, ok := byFile[e.SourceFile]; ok && len(policies) > 0 {
		return true, "auth_policy_in_file"
	}
	// Signal 2: entity property
	if ok, ev := hasAuthProperty(e); ok {
		return true, ev
	}
	// Signal 3: TAGGED_AS edge to auth_policy
	if taggedIDs[e.ID] {
		return true, "tagged_as_auth_policy"
	}
	return false, ""
}

func isSensitiveEndpoint(name, path string) bool {
	combined := strings.ToLower(name + " " + path)
	for _, t := range sensitiveTerms {
		if strings.Contains(combined, t) {
			return true
		}
	}
	return false
}

func hasIDORParam(path string, props map[string]string) bool {
	return strings.Contains(path, "{user_id}") ||
		strings.Contains(path, ":user_id") ||
		props["param:user_id"] != ""
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: GET /api/security/auth-coverage/{group}
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleSecurityAuthCoverage(w http.ResponseWriter, r *http.Request) {
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
	filterSeverity := strings.ToLower(q.Get("severity"))
	filterFile := q.Get("file")
	onlyUncovered := q.Get("only_uncovered") == "true"

	result := GroupAuthCoverageReport{Group: groupName}

	for _, rp := range repoPaths {
		stateDir := filepath.Join(rp.Path, ".archigraph")
		doc, loadErr := graph.LoadGraphFromDir(stateDir)
		if loadErr != nil {
			continue
		}

		byFile := buildAuthPoliciesByFileHTTP(doc)
		taggedIDs := buildTaggedAuthIDsHTTP(doc)

		for i := range doc.Entities {
			e := &doc.Entities[i]
			if !isEndpointKind(e.Kind) {
				continue
			}
			if e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
				continue
			}

			result.TotalEndpoints++

			hasAuth, evidence := determineEndpointAuth(e, byFile, taggedIDs)
			path := e.Properties["path"]
			method := e.Properties["verb"]
			sensitiveOp := isSensitiveEndpoint(e.Name, path)
			idorRisk := hasIDORParam(path, e.Properties)

			var severity string
			switch {
			case hasAuth:
				severity = "info"
				result.InfoCount++
				result.CoveredCount++
			case sensitiveOp || idorRisk:
				severity = "error"
				result.ErrorCount++
				result.UncoveredCount++
			default:
				severity = "warn"
				result.WarnCount++
				result.UncoveredCount++
			}

			if filterSeverity != "" && severity != filterSeverity {
				continue
			}
			if filterFile != "" && !strings.Contains(e.SourceFile, filterFile) {
				continue
			}
			if onlyUncovered && hasAuth {
				continue
			}

			result.Findings = append(result.Findings, AuthEndpointFinding{
				EntityID:     rp.Slug + "/" + e.ID,
				Name:         e.Name,
				Repo:         rp.Slug,
				SourceFile:   e.SourceFile,
				StartLine:    e.StartLine,
				Method:       method,
				Path:         path,
				HasAuth:      hasAuth,
				AuthEvidence: evidence,
				Severity:     severity,
				SensitiveOp:  sensitiveOp,
				IDORRisk:     idorRisk,
			})
		}
	}

	if result.TotalEndpoints > 0 {
		result.CoveragePct = 100.0 * float64(result.CoveredCount) / float64(result.TotalEndpoints)
	}

	// Sort by severity (error first), then file+name.
	sevOrder := map[string]int{"error": 0, "warn": 1, "info": 2}
	sort.SliceStable(result.Findings, func(i, j int) bool {
		si := sevOrder[result.Findings[i].Severity]
		sj := sevOrder[result.Findings[j].Severity]
		if si != sj {
			return si < sj
		}
		if result.Findings[i].SourceFile != result.Findings[j].SourceFile {
			return result.Findings[i].SourceFile < result.Findings[j].SourceFile
		}
		return result.Findings[i].Name < result.Findings[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: GET /api/security/secrets/{group}
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleSecuritySecrets(w http.ResponseWriter, r *http.Request) {
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
	filterSeverity := strings.ToLower(q.Get("severity"))
	filterFile := q.Get("file")

	result := GroupSecretsReport{
		Group:      groupName,
		ByCategory: make(map[string]int),
	}

	sevOrder := map[string]int{"error": 0, "warn": 1, "info": 2}

	for _, rp := range repoPaths {
		stateDir := filepath.Join(rp.Path, ".archigraph")
		doc, loadErr := graph.LoadGraphFromDir(stateDir)
		if loadErr != nil {
			continue
		}

		for i := range doc.Entities {
			e := &doc.Entities[i]

			var category, severity, remediation, provider string

			switch {
			case e.Kind == "SCOPE.Pattern" && e.Subtype == "secrets_management":
				// Positive signal: code is using a secrets manager properly.
				category = "secrets_management"
				severity = "info"
				provider = e.Properties["provider"]
				remediation = ""
			case e.Kind == "SCOPE.Pattern" && e.Subtype == "pattern_recommendation" &&
				e.Name == "hardcoded_credential":
				// Negative signal: hard-coded credential.
				category = "hardcoded_credential"
				severity = "error"
				remediation = "use_env_vars"
			default:
				continue
			}

			if filterSeverity != "" && severity != filterSeverity {
				continue
			}
			if filterFile != "" && !strings.Contains(e.SourceFile, filterFile) {
				continue
			}

			switch severity {
			case "error":
				result.ErrorCount++
			case "warn":
				result.WarnCount++
			case "info":
				result.InfoCount++
			}
			result.ByCategory[category]++

			result.Findings = append(result.Findings, SecuritySecretFinding{
				EntityID:    rp.Slug + "/" + e.ID,
				Name:        e.Name,
				Repo:        rp.Slug,
				SourceFile:  e.SourceFile,
				StartLine:   e.StartLine,
				Language:    e.Language,
				Category:    category,
				Provider:    provider,
				Severity:    severity,
				Remediation: remediation,
			})
		}
	}

	result.TotalFindings = len(result.Findings)

	// Sort by severity, then file+name.
	sort.SliceStable(result.Findings, func(i, j int) bool {
		si := sevOrder[result.Findings[i].Severity]
		sj := sevOrder[result.Findings[j].Severity]
		if si != sj {
			return si < sj
		}
		if result.Findings[i].SourceFile != result.Findings[j].SourceFile {
			return result.Findings[i].SourceFile < result.Findings[j].SourceFile
		}
		return result.Findings[i].Name < result.Findings[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler: GET /api/security/cycles/{group}
// ─────────────────────────────────────────────────────────────────────────────

func cycleSeverity(size int) string {
	if size > 5 {
		return "error"
	}
	if size >= 3 {
		return "warn"
	}
	return "info"
}

func (s *Server) handleSecurityCycles(w http.ResponseWriter, r *http.Request) {
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
	filterSeverity := strings.ToLower(q.Get("severity"))
	minSize := 2
	if q.Get("min_size") != "" {
		fmt.Sscanf(q.Get("min_size"), "%d", &minSize)
	}

	result := GroupCyclesReport{Group: groupName}

	sevOrder := map[string]int{"error": 0, "warn": 1, "info": 2}

	for _, rp := range repoPaths {
		stateDir := filepath.Join(rp.Path, ".archigraph")
		doc, loadErr := graph.LoadGraphFromDir(stateDir)
		if loadErr != nil {
			continue
		}

		// FindImportCycles accepts an optional pagerank map for weakest-link
		// annotation. Pagerank is not persisted in Document, so we pass nil
		// here; cycles are still detected and sized correctly.
		cycles := graph.FindImportCycles(doc.Entities, doc.Relationships, nil)

		for _, c := range cycles {
			if c.Size < minSize {
				continue
			}

			sev := cycleSeverity(c.Size)
			if filterSeverity != "" && sev != filterSeverity {
				continue
			}

			edges := make([]CycleFindingEdge, len(c.Edges))
			for i, e := range c.Edges {
				edges[i] = CycleFindingEdge{FromID: e.FromID, ToID: e.ToID}
			}

			switch sev {
			case "error":
				result.ErrorCount++
			case "warn":
				result.WarnCount++
			case "info":
				result.InfoCount++
			}

			result.Findings = append(result.Findings, CycleFinding{
				Repo:                  rp.Slug,
				Members:               c.Members,
				Edges:                 edges,
				WeakestLinkFromID:     c.WeakestLinkFromID,
				WeakestLinkToID:       c.WeakestLinkToID,
				SuggestedExtractionID: c.SuggestedExtractionID,
				Size:                  c.Size,
				Severity:              sev,
			})
		}
	}

	result.TotalCycles = len(result.Findings)

	// Sort: error first, then descending size, then repo.
	sort.SliceStable(result.Findings, func(i, j int) bool {
		si := sevOrder[result.Findings[i].Severity]
		sj := sevOrder[result.Findings[j].Severity]
		if si != sj {
			return si < sj
		}
		if result.Findings[i].Size != result.Findings[j].Size {
			return result.Findings[i].Size > result.Findings[j].Size
		}
		return result.Findings[i].Repo < result.Findings[j].Repo
	})

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}
