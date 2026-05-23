// auth_coverage.go — archigraph_auth_coverage MCP tool (#1314).
//
// # Purpose
//
// Walk every http_endpoint_definition entity in a group and determine whether
// it is covered by an auth decorator / middleware.  Endpoints without auth are
// surfaced as potential security vulnerabilities.
//
// # Detection strategy
//
// Auth coverage is determined by three complementary signals, evaluated in
// order of decreasing reliability:
//
//  1. Graph edge — an auth_policy entity (emitted by the patterns/
//     auth_endpoint_linker) shares the same source file as the endpoint.
//     This is the strongest signal because the indexer explicitly linked the
//     two via a common-file relationship.
//
//  2. Entity property — the endpoint entity itself carries an "auth_decorator"
//     or "auth_middleware" property set by an extractor (future extractors may
//     populate these directly).
//
//  3. TAGGED_AS edge — a TAGGED_AS relationship from the endpoint to an entity
//     whose subtype or kind is "auth_policy".
//
// # Severity
//
// | Condition                                   | Severity |
// |---------------------------------------------|----------|
// | No auth + sensitive operation (write/pay)   | error    |
// | No auth + accepts user_id param             | error    |
// | No auth + any other endpoint                | warn     |
// | Auth present                                | info     |
//
// Sensitive operation heuristics: endpoint name or path contains
// "payment", "checkout", "password", "delete", "admin", "write", "create",
// "update", "register", "reset".
//
// IDOR heuristic: HTTP verb is GET/POST/PUT/PATCH/DELETE and path contains
// "{user_id}", ":user_id", or the endpoint properties contain "param:user_id".
//
// # Default-allow vs default-deny
//
// If ≥80 % of endpoints in a repo are covered, the repo is classified as
// "default-deny" (auth is the norm).  Otherwise "default-allow" (auth is the
// exception — higher risk posture).
package mcp

import (
	"context"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// Auth-pattern tables — per-framework annotation/middleware names considered
// proof of authentication/authorisation protection.
// ---------------------------------------------------------------------------

// authAnnotationNames is the set of decorator / annotation names that, when
// attached to an endpoint, constitute auth coverage.  Lookup is
// case-insensitive.
var authAnnotationNames = map[string]bool{
	// Django
	"login_required":        true,
	"permission_required":   true,
	"user_passes_test":      true,
	"staff_member_required": true,
	"superuser_required":    true,
	// DRF
	"permission_classes":     true,
	"isauthenticated":        true,
	"isadminuser":            true,
	"isauthorizedorreadonly": true,
	// Flask / Flask-Login / Flask-JWT-Extended
	"jwt_required":       true,
	"fresh_jwt_required": true,
	"roles_required":     true,
	"roles_accepted":     true,
	// FastAPI
	"get_current_user":        true,
	"get_current_active_user": true,
	"oauth2_scheme":           true,
	"verify_token":            true,
	"get_current_superuser":   true,
	"authenticate":            true,
	"require_auth":            true,
	// Express / Node
	"requireauth":    true,
	"authmiddleware": true,
	"verifyjwt":      true,
	"verifytoken":    true,
	"jwtauthguard":   true,
	"authguard":      true,
	// NestJS
	"useguards": true,
	// Spring
	"preauthorize": true,
	"secured":      true,
	"rolesallowed": true,
	// ASP.NET
	"authorize": true,
	// Rails
	"before_action":      true, // commonly used with authenticate_user!
	"authenticate_user!": true,
	// Generic
	"auth":          true,
	"authenticated": true,
	"authorized":    true,
}

// authPropertyKeys are entity property keys whose presence indicates auth.
var authPropertyKeys = []string{
	"auth_decorator",
	"auth_middleware",
	"auth_guard",
	"annotation_name", // set by decorator_extractor
}

// authPropertyValues — for annotation_name property, these values signal auth.
var authPropertyValues = authAnnotationNames

// sensitiveTerms — endpoint name or path tokens that raise severity to error
// when auth is absent.
var sensitiveTerms = []string{
	"payment", "checkout", "billing", "invoice",
	"password", "passwd", "credential",
	"admin", "superuser", "staff",
	"delete", "remove", "destroy",
	"register", "signup", "create",
	"update", "write", "modify", "edit",
	"reset", "token", "secret", "key",
}

// idorParams — path or param tokens that indicate user-scoped data, triggering
// IDOR risk when auth is absent.
var idorParams = []string{
	"{user_id}", ":user_id", "user_id",
	"{account_id}", ":account_id", "account_id",
	"{owner_id}", ":owner_id", "owner_id",
	"{member_id}", ":member_id",
}

// ---------------------------------------------------------------------------
// handleAuthCoverage — the MCP tool handler
// ---------------------------------------------------------------------------

// handleAuthCoverage implements the archigraph_auth_coverage tool.
func (s *Server) handleAuthCoverage(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	limit := argInt(req, "limit", 200)
	onlyMissing := argBool(req, "only_missing", false)

	type EndpointRecord struct {
		EntityID       string `json:"entity_id"`
		Name           string `json:"name"`
		Repo           string `json:"repo"`
		SourceFile     string `json:"source_file,omitempty"`
		StartLine      int    `json:"start_line,omitempty"`
		Method         string `json:"method,omitempty"`
		Path           string `json:"path,omitempty"`
		HasAuth        bool   `json:"has_auth"`
		AuthEvidence   string `json:"auth_evidence,omitempty"`
		Severity       string `json:"severity"`
		SensitiveOp    bool   `json:"sensitive_op,omitempty"`
		IDORRisk       bool   `json:"idor_risk,omitempty"`
		SensitiveTerms string `json:"sensitive_terms,omitempty"`
	}

	type RepoSummary struct {
		Repo          string  `json:"repo"`
		Total         int     `json:"total"`
		Covered       int     `json:"covered"`
		Uncovered     int     `json:"uncovered"`
		CoverageRate  float64 `json:"coverage_rate"`
		DefaultPolicy string  `json:"default_policy"` // "default-deny" or "default-allow"
		ErrorCount    int     `json:"error_count"`
		WarnCount     int     `json:"warn_count"`
	}

	var endpoints []EndpointRecord
	var summaries []RepoSummary

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}

		// Build a file -> []auth_policy_entity index for this repo.
		// auth_policy entities are emitted by the patterns/auth_endpoint_linker
		// as SCOPE.Config entities with subtype "auth_policy".
		authPoliciesByFile := buildAuthPoliciesByFile(r.Doc)

		// Build set of entity IDs reachable via TAGGED_AS edges to auth_policy.
		taggedAuthIDs := buildTaggedAuthIDs(r.Doc)

		repoTotal := 0
		repoCovered := 0
		repoErrors := 0
		repoWarns := 0

		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isDefinitionKind(e.Kind) {
				continue
			}
			// Exclude client-synthesis call-side entries.
			if e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
				continue
			}

			repoTotal++

			hasAuth, evidence := determineAuthCoverage(e, authPoliciesByFile, taggedAuthIDs)

			method := e.Properties["verb"]
			path := e.Properties["path"]
			sensitiveOp, sensitiveMatch := isSensitiveOperation(e.Name, path)
			idorRisk := hasIDORRisk(path, e.Properties)

			var severity string
			switch {
			case hasAuth:
				severity = "info"
			case sensitiveOp || idorRisk:
				severity = "error"
			default:
				severity = "warn"
			}

			if hasAuth {
				repoCovered++
			} else {
				switch severity {
				case "error":
					repoErrors++
				case "warn":
					repoWarns++
				}
			}

			if onlyMissing && hasAuth {
				continue
			}

			endpoints = append(endpoints, EndpointRecord{
				EntityID:       prefixedID(r.Repo, e.ID),
				Name:           e.Name,
				Repo:           r.Repo,
				SourceFile:     e.SourceFile,
				StartLine:      e.StartLine,
				Method:         method,
				Path:           path,
				HasAuth:        hasAuth,
				AuthEvidence:   evidence,
				Severity:       severity,
				SensitiveOp:    sensitiveOp,
				IDORRisk:       idorRisk,
				SensitiveTerms: sensitiveMatch,
			})
		}

		if repoTotal == 0 {
			continue
		}

		coverageRate := float64(repoCovered) / float64(repoTotal)
		defaultPolicy := "default-allow"
		if coverageRate >= 0.80 {
			defaultPolicy = "default-deny"
		}

		summaries = append(summaries, RepoSummary{
			Repo:          r.Repo,
			Total:         repoTotal,
			Covered:       repoCovered,
			Uncovered:     repoTotal - repoCovered,
			CoverageRate:  coverageRate,
			DefaultPolicy: defaultPolicy,
			ErrorCount:    repoErrors,
			WarnCount:     repoWarns,
		})
	}

	// Sort endpoints: errors first, then warns, then info; within same severity
	// sort by repo then source file then line.
	sort.SliceStable(endpoints, func(i, j int) bool {
		si := severityRank(endpoints[i].Severity)
		sj := severityRank(endpoints[j].Severity)
		if si != sj {
			return si < sj
		}
		if endpoints[i].Repo != endpoints[j].Repo {
			return endpoints[i].Repo < endpoints[j].Repo
		}
		if endpoints[i].SourceFile != endpoints[j].SourceFile {
			return endpoints[i].SourceFile < endpoints[j].SourceFile
		}
		return endpoints[i].StartLine < endpoints[j].StartLine
	})

	total := len(endpoints)
	if limit > 0 && len(endpoints) > limit {
		endpoints = endpoints[:limit]
	}

	totalEndpoints := 0
	totalCovered := 0
	for _, s := range summaries {
		totalEndpoints += s.Total
		totalCovered += s.Covered
	}

	overallRate := 0.0
	if totalEndpoints > 0 {
		overallRate = float64(totalCovered) / float64(totalEndpoints)
	}

	return jsonResult(map[string]any{
		"endpoints":        endpoints,
		"count":            len(endpoints),
		"total":            total,
		"truncated":        total > len(endpoints),
		"repo_summaries":   summaries,
		"overall_coverage": overallRate,
		"note": "has_auth=false with severity=error indicates an unprotected endpoint " +
			"performing a sensitive operation or accepting user-scoped parameters (IDOR risk). " +
			"has_auth=false with severity=warn indicates a potentially public endpoint — " +
			"verify intentional exposure.",
	}), nil
}

// ---------------------------------------------------------------------------
// Auth detection helpers
// ---------------------------------------------------------------------------

// buildAuthPoliciesByFile builds a map from source file path → set of auth
// evidence strings for auth_policy entities found in that file.
func buildAuthPoliciesByFile(doc *graph.Document) map[string][]string {
	result := make(map[string][]string)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if !isAuthPolicyEntity(e) {
			continue
		}
		evidence := authEntityEvidence(e)
		result[e.SourceFile] = append(result[e.SourceFile], evidence)
	}
	return result
}

// isAuthPolicyEntity reports whether an entity was emitted by the auth pattern
// extractor (subtype == "auth_policy" or kind is SCOPE.Config with auth props).
func isAuthPolicyEntity(e *graph.Entity) bool {
	if e.Subtype == "auth_policy" {
		return true
	}
	// Entities created by decorator_extractor for auth decorators have
	// decorator_name set to a known auth annotation.
	if dn := e.Properties["decorator_name"]; dn != "" {
		if authAnnotationNames[strings.ToLower(dn)] {
			return true
		}
	}
	// annotation_name property set by auth_endpoint_linker
	if an := e.Properties["annotation_name"]; an != "" {
		return true
	}
	// middleware_name property set by auth_endpoint_linker
	if mn := e.Properties["middleware_name"]; mn != "" {
		return true
	}
	return false
}

// authEntityEvidence returns a human-readable evidence string for an auth
// policy entity (e.g. "@login_required", "verifyToken").
func authEntityEvidence(e *graph.Entity) string {
	if an := e.Properties["annotation_name"]; an != "" {
		return an
	}
	if mn := e.Properties["middleware_name"]; mn != "" {
		return mn
	}
	if dn := e.Properties["decorator_name"]; dn != "" {
		return "@" + dn
	}
	return e.Name
}

// buildTaggedAuthIDs returns the set of entity IDs that have a TAGGED_AS
// relationship pointing to an auth_policy entity.
func buildTaggedAuthIDs(doc *graph.Document) map[string]bool {
	// Collect auth-policy entity IDs first.
	authIDs := map[string]bool{}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if isAuthPolicyEntity(e) {
			authIDs[e.ID] = true
		}
	}

	tagged := map[string]bool{}
	for i := range doc.Relationships {
		rel := &doc.Relationships[i]
		if rel.Kind == "TAGGED_AS" && authIDs[rel.ToID] {
			tagged[rel.FromID] = true
		}
	}
	return tagged
}

// determineAuthCoverage checks three signals (in priority order) and returns
// (hasAuth, evidenceDescription).
func determineAuthCoverage(
	e *graph.Entity,
	authByFile map[string][]string,
	taggedAuthIDs map[string]bool,
) (bool, string) {
	// Signal 1: entity property directly on the endpoint.
	for _, k := range authPropertyKeys {
		if v := e.Properties[k]; v != "" {
			vl := strings.ToLower(v)
			if k == "annotation_name" || k == "auth_decorator" || k == "auth_middleware" || k == "auth_guard" {
				if authPropertyValues[vl] || k != "annotation_name" {
					return true, k + "=" + v
				}
			}
		}
	}

	// Signal 2: TAGGED_AS edge to auth_policy entity.
	if taggedAuthIDs[e.ID] {
		return true, "TAGGED_AS auth_policy"
	}

	// Signal 3: an auth_policy entity shares the same source file.
	if evidences, ok := authByFile[e.SourceFile]; ok && len(evidences) > 0 {
		return true, "file-level: " + strings.Join(deduplicateStrings(evidences), ", ")
	}

	return false, ""
}

// isSensitiveOperation reports whether an endpoint name or path suggests a
// sensitive operation (payment, password change, admin, destructive write).
// Returns (isSensitive, matchedTerms).
func isSensitiveOperation(name, path string) (bool, string) {
	haystack := strings.ToLower(name + " " + path)
	var matches []string
	seen := map[string]bool{}
	for _, term := range sensitiveTerms {
		if strings.Contains(haystack, term) && !seen[term] {
			seen[term] = true
			matches = append(matches, term)
		}
	}
	if len(matches) > 0 {
		return true, strings.Join(matches, ", ")
	}
	return false, ""
}

// hasIDORRisk returns true when the path or properties suggest a user-scoped
// parameter that could be exploited for insecure direct object reference.
func hasIDORRisk(path string, props map[string]string) bool {
	pathLower := strings.ToLower(path)
	for _, param := range idorParams {
		if strings.Contains(pathLower, strings.ToLower(param)) {
			return true
		}
	}
	// Check properties for param markers
	for k, v := range props {
		if strings.Contains(strings.ToLower(k), "param") &&
			strings.Contains(strings.ToLower(v), "user_id") {
			return true
		}
	}
	return false
}

// severityRank maps severity strings to an integer for sort ordering
// (lower = higher priority / shown first).
func severityRank(s string) int {
	switch s {
	case "error":
		return 0
	case "warn":
		return 1
	default:
		return 2
	}
}

// deduplicateStrings returns a deduplicated copy of ss preserving order.
func deduplicateStrings(ss []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
