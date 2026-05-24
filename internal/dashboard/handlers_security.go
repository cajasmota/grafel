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
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbreader"
	fb "github.com/cajasmota/archigraph/internal/graph/fbgraph"
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
	Group          string                `json:"group"`
	TotalEndpoints int                   `json:"total_endpoints"`
	CoveredCount   int                   `json:"covered_count"`
	UncoveredCount int                   `json:"uncovered_count"`
	CoveragePct    float64               `json:"coverage_pct"`
	ErrorCount     int                   `json:"error_count"`
	WarnCount      int                   `json:"warn_count"`
	InfoCount      int                   `json:"info_count"`
	Findings       []AuthEndpointFinding `json:"findings"`
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
	Group         string                  `json:"group"`
	TotalFindings int                     `json:"total_findings"`
	ErrorCount    int                     `json:"error_count"`
	WarnCount     int                     `json:"warn_count"`
	InfoCount     int                     `json:"info_count"`
	ByCategory    map[string]int          `json:"by_category"`
	Findings      []SecuritySecretFinding `json:"findings"`
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
	Group       string         `json:"group"`
	TotalCycles int            `json:"total_cycles"`
	ErrorCount  int            `json:"error_count"`
	WarnCount   int            `json:"warn_count"`
	InfoCount   int            `json:"info_count"`
	Findings    []CycleFinding `json:"findings"`
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

// buildAuthPoliciesByFileReader is the fbreader variant of buildAuthPoliciesByFileHTTP
// used by the S8 zero-copy path when a DashRepo.Reader is available.
func buildAuthPoliciesByFileReader(r *fbreader.Reader) map[string][]string {
	m := make(map[string][]string)
	r.IterateEntities(func(e *fb.Entity) bool {
		// Check Properties["subtype"] via fbgraph PropertyEntry.
		var pe fb.PropertyEntry
		for i := 0; i < e.PropertiesLength(); i++ {
			if e.Properties(&pe, i) && string(pe.Key()) == "subtype" && string(pe.Value()) == "auth_policy" {
				sf := string(e.SourceFile())
				m[sf] = append(m[sf], string(e.Id()))
				return true
			}
		}
		return true
	})
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

// buildTaggedAuthIDsReader is the fbreader variant of buildTaggedAuthIDsHTTP
// used by the S8 zero-copy path. It first builds a set of auth-policy entity
// IDs from the entity vector, then walks the relationship vector for TAGGED_AS
// edges pointing at those IDs.
func buildTaggedAuthIDsReader(r *fbreader.Reader) map[string]bool {
	// Phase 1: collect IDs of auth_policy entities.
	authPolicyIDs := make(map[string]bool)
	r.IterateEntities(func(e *fb.Entity) bool {
		var pe fb.PropertyEntry
		for i := 0; i < e.PropertiesLength(); i++ {
			if e.Properties(&pe, i) && string(pe.Key()) == "subtype" && string(pe.Value()) == "auth_policy" {
				authPolicyIDs[string(e.Id())] = true
				return true
			}
		}
		return true
	})
	// Phase 2: walk TAGGED_AS relationships.
	tagged := make(map[string]bool)
	r.IterateRelationships(func(rel *fb.Relationship) bool {
		if string(rel.Kind()) == "TAGGED_AS" {
			if authPolicyIDs[string(rel.ToId())] {
				tagged[string(rel.FromId())] = true
			}
		}
		return true
	})
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

	// S8 (#2159): prefer the pre-loaded cached group so we don't call
	// LoadGraphFromDir per-request. Fall back to a direct load when the
	// cache is cold (first request after daemon start).
	cachedGrp, _ := s.graphs.GetGroupCached(groupName)

	for _, rp := range repoPaths {
		// Try the cache first.
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

		// Build auth-policy maps using the zero-copy reader when available,
		// otherwise fall back to the materialised Document.
		var byFile map[string][]string
		var taggedIDs map[string]bool
		if rdr != nil {
			byFile = buildAuthPoliciesByFileReader(rdr)
			taggedIDs = buildTaggedAuthIDsReader(rdr)
		} else {
			byFile = buildAuthPoliciesByFileHTTP(doc)
			taggedIDs = buildTaggedAuthIDsHTTP(doc)
		}

		// Iterate entities — use the mmap reader when available.
		iterEntities := func(visit func(id, name, kind, subtype, sourceFile string, startLine int, props map[string]string)) {
			if rdr != nil {
				rdr.IterateEntities(func(e *fb.Entity) bool {
					props := make(map[string]string, e.PropertiesLength())
					var pe fb.PropertyEntry
					for i := 0; i < e.PropertiesLength(); i++ {
						if e.Properties(&pe, i) {
							props[string(pe.Key())] = string(pe.Value())
						}
					}
					visit(string(e.Id()), string(e.Name()), string(e.Kind()), string(e.Subtype()), string(e.SourceFile()), int(e.SourceLine()), props)
					return true
				})
				return
			}
			for i := range doc.Entities {
				ent := &doc.Entities[i]
				visit(ent.ID, ent.Name, ent.Kind, ent.Subtype, ent.SourceFile, ent.StartLine, ent.Properties)
			}
		}

		iterEntities(func(id, name, kind, subtype, sourceFile string, startLine int, props map[string]string) {
			if !isEndpointKind(kind) {
				return
			}
			if props["pattern_type"] == "http_endpoint_client_synthesis" {
				return
			}

			result.TotalEndpoints++

			// Build a lightweight graph.Entity for determineEndpointAuth.
			e := &graph.Entity{
				ID:         id,
				Name:       name,
				Kind:       kind,
				Subtype:    subtype,
				SourceFile: sourceFile,
				StartLine:  startLine,
				Properties: props,
			}

			hasAuth, evidence := determineEndpointAuth(e, byFile, taggedIDs)
			epath := props["path"]
			method := props["verb"]
			sensitiveOp := isSensitiveEndpoint(name, epath)
			idorRisk := hasIDORParam(epath, props)

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
				return
			}
			if filterFile != "" && !strings.Contains(sourceFile, filterFile) {
				return
			}
			if onlyUncovered && hasAuth {
				return
			}

			result.Findings = append(result.Findings, AuthEndpointFinding{
				EntityID:     rp.Slug + "/" + id,
				Name:         name,
				Repo:         rp.Slug,
				SourceFile:   sourceFile,
				StartLine:    startLine,
				Method:       method,
				Path:         epath,
				HasAuth:      hasAuth,
				AuthEvidence: evidence,
				Severity:     severity,
				SensitiveOp:  sensitiveOp,
				IDORRisk:     idorRisk,
			})
		})
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

	// S8 (#2159): use the cached group to avoid per-request LoadGraphFromDir.
	cachedGrpSecrets, _ := s.graphs.GetGroupCached(groupName)

	for _, rp := range repoPaths {
		var doc *graph.Document
		var rdr *fbreader.Reader
		if cachedGrpSecrets != nil {
			if dr, ok := cachedGrpSecrets.Repos[rp.Slug]; ok && dr != nil {
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

		iterSecretEntities := func(visit func(id, name, kind, subtype, sourceFile, language string, startLine int, props map[string]string)) {
			if rdr != nil {
				rdr.IterateEntities(func(e *fb.Entity) bool {
					props := make(map[string]string, e.PropertiesLength())
					var pe fb.PropertyEntry
					for i := 0; i < e.PropertiesLength(); i++ {
						if e.Properties(&pe, i) {
							props[string(pe.Key())] = string(pe.Value())
						}
					}
					visit(string(e.Id()), string(e.Name()), string(e.Kind()), string(e.Subtype()), string(e.SourceFile()), props["language"], int(e.SourceLine()), props)
					return true
				})
				return
			}
			for i := range doc.Entities {
				ent := &doc.Entities[i]
				visit(ent.ID, ent.Name, ent.Kind, ent.Subtype, ent.SourceFile, ent.Language, ent.StartLine, ent.Properties)
			}
		}

		iterSecretEntities(func(id, name, kind, subtype, sourceFile, language string, startLine int, props map[string]string) {
			var category, severity, remediation, provider string

			switch {
			case kind == "SCOPE.Pattern" && subtype == "secrets_management":
				category = "secrets_management"
				severity = "info"
				provider = props["provider"]
				remediation = ""
			case kind == "SCOPE.Pattern" && subtype == "pattern_recommendation" &&
				name == "hardcoded_credential":
				category = "hardcoded_credential"
				severity = "error"
				remediation = "use_env_vars"
			default:
				return
			}

			if filterSeverity != "" && severity != filterSeverity {
				return
			}
			if filterFile != "" && !strings.Contains(sourceFile, filterFile) {
				return
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
				EntityID:    rp.Slug + "/" + id,
				Name:        name,
				Repo:        rp.Slug,
				SourceFile:  sourceFile,
				StartLine:   startLine,
				Language:    language,
				Category:    category,
				Provider:    provider,
				Severity:    severity,
				Remediation: remediation,
			})
		})
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

	// S8 (#2159): use the cached group to avoid per-request LoadGraphFromDir.
	cachedGrpCycles, _ := s.graphs.GetGroupCached(groupName)

	for _, rp := range repoPaths {
		var doc *graph.Document
		if cachedGrpCycles != nil {
			if dr, ok := cachedGrpCycles.Repos[rp.Slug]; ok && dr != nil {
				doc = dr.Doc
			}
		}
		if doc == nil {
			stateDir := daemon.StateDirForRepo(rp.Path)
			var loadErr error
			doc, loadErr = graph.LoadGraphFromDir(stateDir)
			if loadErr != nil {
				continue
			}
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
