// auth_coverage.go — grafel_auth_coverage MCP tool (#1314).
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
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
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
	// Tornado
	"tornado.web.authenticated": true,
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

// openPermissionNames are DRF permission classes that grant access to ALL
// callers (authenticated or not) — their presence does NOT constitute auth
// coverage. Lookup is case-insensitive on the leaf class name.
var openPermissionNames = map[string]bool{
	"allowany": true,
}

// isProtectivePermissionList reports whether a comma-joined list of DRF
// permission-class leaf names (as stamped by the python extractor in the
// `permission_classes` / `get_permissions_classes` properties) represents real
// protection.
//
// Rules:
//   - empty list                       → not protective (permission-less; DRF
//     treats an empty permission_classes as open)
//   - every entry is AllowAny          → not protective (explicitly public)
//   - at least one non-AllowAny entry  → protective (e.g. IsAuthenticated, a
//     custom permission check, IsAdminUser)
//
// Returns (protective, evidenceName) where evidenceName is the first
// non-AllowAny permission class found (for the auth_evidence string).
func isProtectivePermissionList(joined string) (bool, string) {
	parts := strings.Split(joined, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if openPermissionNames[strings.ToLower(p)] {
			continue
		}
		return true, p
	}
	return false, ""
}

// drfClassAuth captures the class-level DRF authorisation posture of a single
// ViewSet / APIView, keyed by its source-line range so an endpoint can be
// attributed to its enclosing class.
type drfClassAuth struct {
	startLine int
	endLine   int
	// hasPermAttr is true when the class declares `permission_classes = [...]`
	// (even if the value is AllowAny / empty).
	hasPermAttr bool
	// permClasses is the comma-joined leaf names of the class-level attribute.
	permClasses string
	// hasGetPermissions is true when the class defines get_permissions().
	hasGetPermissions bool
	// getPermClasses is the comma-joined leaf names referenced in
	// get_permissions().
	getPermClasses string
}

// evaluate returns (decided, hasAuth, evidence) for this class. decided=false
// means the class carries no explicit DRF permission signal at all, so the
// caller should fall back to the repo-level default policy.
func (c drfClassAuth) evaluate() (decided, hasAuth bool, evidence string) {
	// A dynamic get_permissions() override is the strongest class-level signal.
	if c.hasGetPermissions {
		if prot, name := isProtectivePermissionList(c.getPermClasses); prot {
			return true, true, "DRF get_permissions: " + name
		}
		// get_permissions present but we could not extract a protective class
		// (e.g. it returns AllowAny, or the body is too dynamic to parse). If
		// the only thing we saw was AllowAny, treat as open; otherwise treat
		// the mere presence of get_permissions as a (weak) protective signal —
		// hand-written get_permissions almost always gates access.
		if c.getPermClasses != "" {
			return true, false, ""
		}
		return true, true, "DRF get_permissions override"
	}
	if c.hasPermAttr {
		if prot, name := isProtectivePermissionList(c.permClasses); prot {
			return true, true, "DRF permission_classes: " + name
		}
		// Explicit AllowAny / empty permission_classes → genuinely open.
		return true, false, ""
	}
	return false, false, ""
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
// EndpointRecord — one auth-coverage finding (package-scoped for #2828 terse).
// ---------------------------------------------------------------------------

// EndpointRecord is a single endpoint's auth-coverage finding. It is the wire
// shape emitted under "endpoints" in format=full; in format=terse it is
// rendered to a single line via terse().
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

// terse renders one finding as a compact single line, e.g.
//
//	error DELETE /users/{user_id} NO-AUTH idor,sensitive(delete) routes/u.py:42 (repo1)
//	info  GET /dashboard auth(login_required) views/dashboard.py:10 (repo1)
//
// It keeps every fact a reviewer acts on (severity, verb, path, auth state +
// evidence, IDOR/sensitive flags, location, repo) and drops only the JSON
// envelope and the redundant entity_id/name.
func (r EndpointRecord) terse() string {
	var b strings.Builder
	b.WriteString(r.Severity)
	b.WriteByte(' ')
	if r.Method != "" {
		b.WriteString(r.Method)
		b.WriteByte(' ')
	}
	if r.Path != "" {
		b.WriteString(r.Path)
	} else if r.Name != "" {
		b.WriteString(r.Name)
	}
	if r.HasAuth {
		b.WriteString(" auth")
		if r.AuthEvidence != "" {
			b.WriteString("(")
			b.WriteString(r.AuthEvidence)
			b.WriteString(")")
		}
	} else {
		b.WriteString(" NO-AUTH")
		var flags []string
		if r.IDORRisk {
			flags = append(flags, "idor")
		}
		if r.SensitiveOp {
			if r.SensitiveTerms != "" {
				flags = append(flags, "sensitive("+r.SensitiveTerms+")")
			} else {
				flags = append(flags, "sensitive")
			}
		}
		if len(flags) > 0 {
			b.WriteByte(' ')
			b.WriteString(strings.Join(flags, ","))
		}
	}
	if r.SourceFile != "" {
		b.WriteByte(' ')
		b.WriteString(r.SourceFile)
		if r.StartLine > 0 {
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(r.StartLine))
		}
	}
	if r.Repo != "" {
		b.WriteString(" (")
		b.WriteString(r.Repo)
		b.WriteString(")")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// handleAuthCoverage — the MCP tool handler
// ---------------------------------------------------------------------------

// handleAuthCoverage implements the grafel_auth_coverage tool.
func (s *Server) handleAuthCoverage(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	// #2828 token-cost optimisation. Live telemetry flagged auth_coverage as the
	// single biggest token hog (~7.5K tok/call): the old default limit of 200
	// full per-endpoint records plus a ~340-byte static note on every call.
	//
	//   - format="terse" (default): emit one compact line per finding instead of
	//     a verbose object, and drop the static note. Same security-essential
	//     facts (repo, verb, path, severity, has_auth, location), far fewer bytes.
	//   - format="full": the legacy structured `endpoints` array (every field),
	//     for callers that machine-parse individual records.
	//   - limit default lowered 200→50 with an explicit truncation marker.
	//   - token_budget: optional hard byte cap (token_budget*4) on the returned
	//     list, truncating with a marker so the agent can opt into more.
	format := strings.ToLower(argString(req, "format", ""))
	verbose := argBool(req, "verbose", false)
	switch format {
	case "full":
		verbose = true
	case "terse":
		verbose = false
	}
	limit := argInt(req, "limit", 50)
	tokenBudget := argInt(req, "token_budget", 0)
	onlyMissing := argBool(req, "only_missing", false)

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

		// Issue #2816 — DRF class-level authorisation index: per source file,
		// the line-range posture of every ViewSet/APIView carrying
		// permission_classes / get_permissions, plus the repo-wide DRF default
		// permission policy harvested from the settings REST_FRAMEWORK dict.
		drfAuthByFile := buildDRFClassAuthByFile(r.Doc)
		drfDefaultProtected, drfDefaultEvidence := repoDRFDefaultPolicy(r.Doc)

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
			if e.PropGet("pattern_type") == "http_endpoint_client_synthesis" {
				continue
			}

			repoTotal++

			hasAuth, evidence := determineAuthCoverage(
				e, authPoliciesByFile, taggedAuthIDs,
				drfAuthByFile, drfDefaultProtected, drfDefaultEvidence,
			)

			method := e.PropGet("verb")
			path := e.PropGet("path")
			sensitiveOp, sensitiveMatch := isSensitiveOperation(e.Name, path)
			idorRisk := hasIDORRisk(path, e.PropsSnapshot())

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
	// #2828: optional hard token budget. Trim further (after the limit cap) so
	// the rendered list fits ~token_budget*4 bytes, always leaving a marker.
	if tokenBudget > 0 {
		endpoints = capAuthByBudget(endpoints, tokenBudget*4, verbose)
	}
	shown := len(endpoints)

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

	resp := map[string]any{
		"count":            shown,
		"total":            total,
		"truncated":        total > shown,
		"repo_summaries":   summaries,
		"overall_coverage": overallRate,
		"format":           formatLabel(verbose),
	}
	if total > shown {
		resp["truncated_count"] = total - shown
		resp["truncation_note"] = fmt.Sprintf(
			"%d more findings omitted — pass a larger limit or token_budget, or only_missing=true to focus on uncovered endpoints",
			total-shown,
		)
	}
	if verbose {
		// Legacy structured array + the explanatory note (full mode only).
		resp["endpoints"] = endpoints
		resp["note"] = "has_auth=false with severity=error indicates an unprotected endpoint " +
			"performing a sensitive operation or accepting user-scoped parameters (IDOR risk). " +
			"has_auth=false with severity=warn indicates a potentially public endpoint — " +
			"verify intentional exposure."
	} else {
		// Terse: one compact line per finding. severity=error first (already
		// sorted). Format: "<SEV> <verb> <path> [auth|NO-AUTH] file:line (repo)".
		resp["findings"] = renderTerseAuthFindings(endpoints)
	}
	return jsonResult(resp), nil
}

// renderTerseAuthFindings serialises auth findings as one compact line each,
// preserving the security-essential facts (severity, verb, path, auth state,
// IDOR/sensitive flags, location) while dropping the verbose per-record JSON
// envelope (entity_id, repeated keys). #2828.
func renderTerseAuthFindings(recs []EndpointRecord) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.terse())
	}
	return out
}

// capAuthByBudget returns the largest prefix of recs whose rendered form fits
// maxBytes. Verbose mode measures the JSON array; terse mode measures the
// joined terse lines. Always returns at least one record when recs is non-empty
// and a single record already exceeds the budget (so the marker is meaningful).
func capAuthByBudget(recs []EndpointRecord, maxBytes int, verbose bool) []EndpointRecord {
	if maxBytes <= 0 || len(recs) == 0 {
		return recs
	}
	size := func(n int) int {
		if verbose {
			b, _ := json.Marshal(recs[:n])
			return len(b)
		}
		total := 0
		for i := 0; i < n; i++ {
			total += len(recs[i].terse()) + 1
		}
		return total
	}
	if size(len(recs)) <= maxBytes {
		return recs
	}
	lo, hi := 1, len(recs)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if size(mid) <= maxBytes {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return recs[:lo]
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
	if dn := e.PropGet("decorator_name"); dn != "" {
		if authAnnotationNames[strings.ToLower(dn)] {
			return true
		}
	}
	// annotation_name property set by auth_endpoint_linker
	if an := e.PropGet("annotation_name"); an != "" {
		return true
	}
	// middleware_name property set by auth_endpoint_linker
	if mn := e.PropGet("middleware_name"); mn != "" {
		return true
	}
	return false
}

// authEntityEvidence returns a human-readable evidence string for an auth
// policy entity (e.g. "@login_required", "verifyToken").
func authEntityEvidence(e *graph.Entity) string {
	if an := e.PropGet("annotation_name"); an != "" {
		return an
	}
	if mn := e.PropGet("middleware_name"); mn != "" {
		return mn
	}
	if dn := e.PropGet("decorator_name"); dn != "" {
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

// determineAuthCoverage checks the available signals (in priority order) and
// returns (hasAuth, evidenceDescription).
//
// DRF class-level signals (issue #2816) take precedence over the coarse
// file-level auth_policy match because they can both PROVE protection
// (permission_classes=[IsAuthenticated], get_permissions()) AND prove that an
// endpoint is genuinely public (permission_classes=[AllowAny]) — the latter is
// essential to avoid mislabelling intentional login/public endpoints.
func determineAuthCoverage(
	e *graph.Entity,
	authByFile map[string][]string,
	taggedAuthIDs map[string]bool,
	drfAuthByFile map[string][]drfClassAuth,
	drfDefaultProtected bool,
	drfDefaultEvidence string,
) (bool, string) {
	// Signal 0: the authoritative reconciled posture. The engine auth resolvers
	// (Nest @UseGuards / @RequirePage / @RequireAction / @Authenticated, Spring
	// Security, Quarkus, gRPC/tRPC interceptors, …) collapse every method-,
	// controller- and global-level signal — including @Public exemptions — into a
	// single `auth_required` verdict plus an `auth_method`/`auth_guard` evidence
	// stamp. When that verdict says the route IS authenticated it MUST win, so an
	// endpoint gated only by an INHERITED controller/global guard (no own
	// decorator, so none of the raw signal-1 keys below are set) is never
	// mislabelled NO-AUTH while the endpoint_posture surface simultaneously shows
	// it authenticated — the contradictory dual badge (#auth-posture-conflict).
	//
	// auth_required=="false" is a DECISIVE public verdict (explicit @Public /
	// AllowAny / permitAll): genuinely unauthenticated by design, so it does NOT
	// count as covered and falls through (no raw signal will rescue it).
	if e.PropGet("auth_required") == "true" {
		if g := e.PropGet("auth_guard"); g != "" {
			return true, "auth_guard=" + g
		}
		if m := e.PropGet("auth_method"); m != "" && m != "unknown" {
			return true, "auth_method=" + m
		}
		return true, "auth_required=true"
	}

	// Signal 1: entity property directly on the endpoint.
	for _, k := range authPropertyKeys {
		if v := e.PropGet(k); v != "" {
			vl := strings.ToLower(v)
			if k == "annotation_name" || k == "auth_decorator" || k == "auth_middleware" || k == "auth_guard" {
				if authPropertyValues[vl] || k != "annotation_name" {
					return true, k + "=" + v
				}
			}
		}
	}

	// Signal 2: DRF class-level authorisation. Attribute the endpoint to its
	// enclosing ViewSet/APIView by source-line range and read that class's
	// permission_classes / get_permissions posture. A decisive class verdict
	// (protected OR explicitly-open) wins over the weaker file/edge signals.
	fileClasses := drfAuthByFile[e.SourceFile]
	if cls, ok := enclosingDRFClass(fileClasses, e.StartLine); ok {
		if decided, hasAuth, evidence := cls.evaluate(); decided {
			if hasAuth {
				return true, evidence
			}
			// Class explicitly opts into AllowAny / empty perms → genuinely
			// open. Do NOT fall through to weaker signals or the global
			// default; an explicit class-level AllowAny overrides the default.
			return false, ""
		}
		// Class present but carried no explicit permission signal → fall
		// through to the repo-level DRF default below.
	} else if len(fileClasses) > 0 {
		// Router-synthesised endpoints (standard CRUD verbs) frequently carry
		// no source line, so they cannot be attributed to an enclosing class by
		// range. Fall back to a file-level aggregate verdict: when every DRF
		// class in the file agrees on a posture we can apply it confidently.
		if decided, hasAuth, evidence := fileDRFVerdict(fileClasses); decided {
			if hasAuth {
				return true, evidence
			}
			return false, ""
		}
	}

	// Signal 3: TAGGED_AS edge to auth_policy entity.
	if taggedAuthIDs[e.ID] {
		return true, "TAGGED_AS auth_policy"
	}

	// Signal 4: an auth_policy entity shares the same source file.
	if evidences, ok := authByFile[e.SourceFile]; ok && len(evidences) > 0 {
		return true, "file-level: " + strings.Join(deduplicateStrings(evidences), ", ")
	}

	// Signal 5: repo-wide DRF default permission policy. An endpoint with no
	// explicit per-method/class permission inherits REST_FRAMEWORK's
	// DEFAULT_PERMISSION_CLASSES; when that default is protective every
	// otherwise-unmarked DRF endpoint is covered. Only applied to Python DRF
	// endpoints (those whose enclosing source file participates in the DRF
	// class index, or where the repo declares a protective default).
	if drfDefaultProtected && isLikelyDRFEndpoint(e) {
		return true, drfDefaultEvidence
	}

	return false, ""
}

// isLikelyDRFEndpoint reports whether an http endpoint entity originates from a
// Python DRF view (so the repo-wide DEFAULT_PERMISSION_CLASSES applies to it).
// We gate on the .py source suffix to avoid leaking a Python global default
// onto endpoints from other languages/frameworks in a polyglot repo.
func isLikelyDRFEndpoint(e *graph.Entity) bool {
	if e.Language == "python" {
		return true
	}
	return strings.HasSuffix(e.SourceFile, ".py")
}

// enclosingDRFClass returns the DRF class whose [startLine, endLine] range
// contains line, preferring the innermost (smallest) range when nested. The
// boolean is false when no class in classes contains line.
func enclosingDRFClass(classes []drfClassAuth, line int) (drfClassAuth, bool) {
	var best drfClassAuth
	found := false
	for _, c := range classes {
		if c.startLine == 0 || line == 0 {
			continue
		}
		// endLine may be 0 for malformed/partial entities — treat as unbounded.
		within := line >= c.startLine && (c.endLine == 0 || line <= c.endLine)
		if !within {
			continue
		}
		if !found || (c.startLine >= best.startLine && rangeSize(c) <= rangeSize(best)) {
			best = c
			found = true
		}
	}
	return best, found
}

// fileDRFVerdict computes an aggregate authorisation verdict for a source file
// from the postures of all DRF classes it declares. Used for endpoints that
// carry no source line (router-synthesised CRUD verbs) and therefore cannot be
// attributed to a single enclosing class by range.
//
// Verdict rules:
//   - at least one class is decisively protected AND none is decisively open →
//     protected (the common single-ViewSet-per-file case, and files where all
//     ViewSets gate access).
//   - at least one class is decisively open AND none is decisively protected →
//     open (e.g. auth_viewset.py: all AllowAny login/register endpoints).
//   - mixed (some open, some protected) → undecided (decided=false): the file
//     hosts both public and protected ViewSets, so a line-less endpoint cannot
//     be safely attributed; the caller falls back to the repo default.
//   - no class carries a decisive posture → undecided.
func fileDRFVerdict(classes []drfClassAuth) (decided, hasAuth bool, evidence string) {
	var anyProtected, anyOpen bool
	var protectedEvidence string
	for _, c := range classes {
		d, ha, ev := c.evaluate()
		if !d {
			continue
		}
		if ha {
			anyProtected = true
			if protectedEvidence == "" {
				protectedEvidence = ev
			}
		} else {
			anyOpen = true
		}
	}
	switch {
	case anyProtected && !anyOpen:
		return true, true, protectedEvidence
	case anyOpen && !anyProtected:
		return true, false, ""
	default:
		// mixed or no decisive posture
		return false, false, ""
	}
}

// rangeSize returns the line span of a class (large when unbounded).
func rangeSize(c drfClassAuth) int {
	if c.endLine == 0 {
		return 1 << 30
	}
	return c.endLine - c.startLine
}

// buildDRFClassAuthByFile indexes every Python class entity that carries a
// DRF class-level authorisation property, grouped by source file. These
// properties are stamped by the python extractor's applyDRFPermissionProperties
// pass (issue #2816).
func buildDRFClassAuthByFile(doc *graph.Document) map[string][]drfClassAuth {
	out := make(map[string][]drfClassAuth)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.PropLen() == 0 {
			continue
		}
		_, hasAttr := e.PropLookup("has_permission_classes")
		_, hasGet := e.PropLookup("has_get_permissions")
		if !hasAttr && !hasGet {
			continue
		}
		out[e.SourceFile] = append(out[e.SourceFile], drfClassAuth{
			startLine:         e.StartLine,
			endLine:           e.EndLine,
			hasPermAttr:       hasAttr,
			permClasses:       e.PropGet("permission_classes"),
			hasGetPermissions: hasGet,
			getPermClasses:    e.PropGet("get_permissions_classes"),
		})
	}
	return out
}

// repoDRFDefaultPolicy inspects the repo's Django settings config_module entity
// for the harvested REST_FRAMEWORK DEFAULT_PERMISSION_CLASSES (stamped by the
// python config_module extractor, issue #2816) and reports whether the global
// default protects endpoints. When the key is absent DRF's built-in default is
// AllowAny → not protected.
func repoDRFDefaultPolicy(doc *graph.Document) (protected bool, evidence string) {
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.PropLen() == 0 {
			continue
		}
		if e.PropGet("drf_default_permission_present") != "true" {
			continue
		}
		if prot, name := isProtectivePermissionList(e.PropGet("drf_default_permission_classes")); prot {
			return true, "DRF default permission: " + name
		}
		// Present but AllowAny/empty default → explicitly open. A later
		// settings module won't override an explicit open default, so keep
		// scanning only for a protective one.
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
