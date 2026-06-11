// archigraph_auth_posture_diff MCP tool (#4422, epic #4419 P0 — the BLOCKING
// RBAC-drift class).
//
// Per linked HTTP endpoint, it resolves the ORACLE side's auth posture (e.g. a
// Django get_permissions branch decode, the §10 contract) and the V3 side's auth
// posture (e.g. a NestJS guard/@Require* stack) into the shared {kind, literal}
// vocabulary, then diffs them into a conservative verdict:
//
//	equivalent | stricter | looser | slug_mismatch | kind_mismatch
//
// The decode is framework-agnostic: a PLUGGABLE resolver registry
// (internal/authposture) maps each framework's native signal into the common
// vocabulary, so the diff CORE knows nothing about Django or NestJS. Flagship
// resolvers (Django DRF §10 + NestJS) are implemented; every other framework is
// a registered stub with a follow-up ticket (ref #4419).
//
// Signature:
//
//	auth_posture_diff(
//	  group_oracle:  "<oracle group>",   (required — the behavioral oracle)
//	  group_v3:      "<v3-rewrite group>", (required)
//	  endpoint:      "<verb path substring>", (optional — narrow to one endpoint)
//	  format:        "terse" | "full",   (optional — default terse)
//	)
//
// Join: the oracle and v3 endpoints are joined on their normalised HTTP
// <verb> <path> key (the same key the cross-repo HTTP link resolves on). Each
// joined pair yields one diff record. format=terse omits the per-side Detail
// strings; format=full includes the full posture provenance.
//
// Live cross-graph validation (oracle upvate vs v3 upvate-v3) is DEPLOY-GATED:
// it needs a live reindex of BOTH groups so the endpoints carry current
// auth-posture properties. The decode + diff logic is unit-tested in
// internal/authposture; this handler is exercised by a stubbed 2-group store.
package mcp

import (
	"context"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/authposture"
	"github.com/cajasmota/archigraph/internal/graph"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// authPostureSignalProps are the entity property keys an auth-posture resolver
// may read. Harvested into the framework-neutral Signal; each resolver picks
// the keys its framework stamps. Kept explicit so the Signal carries exactly the
// auth surface and nothing else.
//
// This is the SIGNAL-PLUMBING allow-list (the keys copied onto the resolver
// Signal), NOT the cross-group JOIN keying — the #4550 boundary was about the
// join algorithm, which is untouched here. The framework-specific blocks below
// were added in #4734/#4742-#4747 so every registered resolver
// (Spring/Rails/Flask/Laravel/ASP.NET/Go/Phoenix) reads its STRUCTURED posture
// signal in the LIVE diff path instead of degrading to source-scan/unknown.
var authPostureSignalProps = []string{
	// Django DRF.
	"has_get_permissions", "has_permission_classes", "permission_classes",
	"get_permissions_classes", "get_permissions_source", "drf_default_permission_classes",
	// NestJS reconciled posture + @Require* literals.
	"require_page", "require_action", "require_superuser", "is_public",
	"auth_required", "auth_method", "auth_guard", "auth_roles", "auth_scopes",
	// Generic reconciled posture shared across framework extractors (Rails/Flask/
	// Laravel/ASP.NET/Go/Phoenix all stamp some subset of these via the #3734
	// flat-contract convention).
	"auth_kind", "auth_permissions", "auth_middleware", "auth_policy",
	"middleware", "allow_anonymous",
	// Spring (#4734) — method/class/global @PreAuthorize/@Secured/@RolesAllowed
	// + SecurityFilterChain. The resolver reads both the bare and spring_*-prefixed
	// class/global forms.
	"auth_expression", "pre_authorize", "preauthorize", "post_authorize",
	"secured", "roles_allowed",
	"class_pre_authorize", "class_secured", "class_roles_allowed",
	"spring_class_pre_authorize", "spring_class_post_authorize",
	"spring_class_secured", "spring_class_roles_allowed", "spring_global_authorization",
	// Rails (#4742) — Pundit / CanCanCan literals.
	"pundit_policy", "pundit_action", "cancancan_ability",
	// Flask (#4743) — Flask-Principal permission / decorator name + page.
	"auth_decorator", "auth_page",
	// Laravel (#4744) — middleware / permission already covered above (auth_middleware,
	// middleware, auth_permissions, auth_page).
	// ASP.NET Core (#4745) — policy + class-level / global [Authorize]/[AllowAnonymous].
	"aspnet_class_authorize", "aspnet_class_roles", "aspnet_class_policy",
	"aspnet_class_allow_anonymous", "aspnet_fallback_policy",
	// Phoenix (#4747) — resolved pipeline list + per-pipeline plug list.
	"auth_pipelines", "auth_plugs", "phoenix_pipelines", "phoenix_plugs",
	"pipe_through", "plugs",
	// Cross-framework action context.
	"effective_action", "action", "framework",
}

// authPostureSourceProps are the endpoint property keys that may carry the raw
// auth-bearing SOURCE body a resolver source-scans when its structured props are
// absent. Tried in order; the first non-empty one populates Signal.Source. The
// Django get_permissions body comes first (the flagship path); the per-framework
// handler/controller/route/router source bodies (#4742-#4747) follow so each
// resolver's source-scan fallback works in the LIVE diff path, not just unit
// tests. Engine stamping of these bodies is tracked per-framework; this harvest
// is forward-compatible — it picks up whichever body the extractor stamps.
var authPostureSourceProps = []string{
	"get_permissions_source", // Django DRF (flagship).
	"auth_source",            // generic reconciled auth-source body.
	"handler_source",         // flat handler body (Flask/FastAPI/Go).
	"controller_source",      // Rails / Laravel / ASP.NET controller body.
	"action_source",          // ASP.NET / DRF action body.
	"view_source",            // Flask view / Django view body.
	"route_source",           // Laravel / Go route-registration body.
	"router_source",          // Phoenix / Go router body.
	"guard_source",           // NestJS guard / middleware body.
	"middleware_source",      // middleware-chain body.
}

// authPostureRecord is one joined endpoint's diff record.
type authPostureRecord struct {
	Endpoint       string              `json:"endpoint"`
	Verdict        authposture.Verdict `json:"verdict"`
	Detail         string              `json:"detail,omitempty"`
	OracleResolved authposture.Posture `json:"oracle_resolved"`
	V3Stack        authposture.Posture `json:"v3_stack"`
}

// handleAuthPostureDiff implements archigraph_auth_posture_diff.
func (s *Server) handleAuthPostureDiff(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	oracleGroup := argString(req, "group_oracle", "")
	v3Group := argString(req, "group_v3", "")
	if oracleGroup == "" || v3Group == "" {
		return mcpapi.NewToolResultError("group_oracle and group_v3 are both required"), nil
	}
	endpointFilter := strings.ToLower(strings.TrimSpace(argString(req, "endpoint", "")))
	format := strings.ToLower(strings.TrimSpace(argString(req, "format", "terse")))
	if format == "" {
		format = "terse"
	}

	lgOracle := s.State.Group(oracleGroup)
	if lgOracle == nil {
		return mcpapi.NewToolResultError("group_oracle " + oracleGroup + " not loaded"), nil
	}
	lgV3 := s.State.Group(v3Group)
	if lgV3 == nil {
		return mcpapi.NewToolResultError("group_v3 " + v3Group + " not loaded"), nil
	}

	reg := authposture.NewRegistry()

	oracleEndpoints := collectAuthEndpoints(lgOracle)
	v3Endpoints := collectAuthEndpoints(lgV3)

	// Join on the normalised <verb> <path> key (the cross-repo HTTP link key).
	var records []authPostureRecord
	for key, oe := range oracleEndpoints {
		ve, ok := v3Endpoints[key]
		if !ok {
			continue // unlinked oracle endpoint — nothing to diff against
		}
		if endpointFilter != "" && !strings.Contains(strings.ToLower(oe.display), endpointFilter) {
			continue
		}
		oraclePosture, _ := reg.Resolve(oe.signal)
		v3Posture, _ := reg.Resolve(ve.signal)
		d := authposture.Diff(v3Posture, oraclePosture)
		rec := authPostureRecord{
			Endpoint:       oe.display,
			Verdict:        d.Verdict,
			OracleResolved: d.OraclePosture,
			V3Stack:        d.V3Posture,
		}
		if format == "full" {
			rec.Detail = d.Detail
		} else {
			// terse: drop the per-side provenance detail strings.
			rec.OracleResolved.Detail = ""
			rec.V3Stack.Detail = ""
		}
		records = append(records, rec)
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Verdict != records[j].Verdict {
			return verdictRank(records[i].Verdict) < verdictRank(records[j].Verdict)
		}
		return records[i].Endpoint < records[j].Endpoint
	})

	summary := summariseVerdicts(records)
	if records == nil {
		records = []authPostureRecord{}
	}
	return jsonResult(map[string]any{
		"group_oracle":     oracleGroup,
		"group_v3":         v3Group,
		"joined":           len(records),
		"oracle_endpoints": len(oracleEndpoints),
		"v3_endpoints":     len(v3Endpoints),
		"summary":          summary,
		"records":          records,
		"note":             "live cross-graph validation requires a deploy-window reindex of both groups (#4422)",
	}), nil
}

// authEndpoint is one harvested endpoint: its normalised join key, a human
// display label, and the framework-neutral Signal for resolution.
type authEndpoint struct {
	display string
	signal  authposture.Signal
}

// collectAuthEndpoints scans a group for HTTP endpoint-definition entities and
// builds an authEndpoint per endpoint, keyed by the SHARED cross-group join key
// (normalised <verb> <path>, /api[/vN] prefix stripped, path-params → {*} — the
// same key stub_detector joins on, see endpoint_join.go). When two endpoints
// fold to the same key (e.g. ViewSet per-action expansion), the one carrying the
// richest auth signal wins so the diff sees the most specific posture.
//
// The join key deliberately does NOT include the DRF #action suffix: the oracle
// (DRF) stamps an action while the v3 (NestJS) does not, so a #action key never
// matched a v3 endpoint and the diff joined ZERO endpoints live (#4550). Folding
// per-action ViewSet rows by richest-signal-wins keeps the most specific posture.
func collectAuthEndpoints(lg *LoadedGroup) map[endpointJoinKey]authEndpoint {
	out := map[endpointJoinKey]authEndpoint{}
	for _, r := range lg.Repos {
		if r == nil || r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isHTTPEndpointDefinition(e) {
				continue
			}
			verb, path := endpointVerbPath(e)
			if path == "" {
				continue
			}
			key := newEndpointJoinKey(verb, path)
			ae := authEndpoint{
				display: strings.TrimSpace(strings.ToUpper(verb) + " " + path),
				signal:  buildAuthSignal(e),
			}
			if ae.display == "" {
				ae.display = e.Name
			}
			if existing, ok := out[key]; ok && signalRichness(existing.signal) >= signalRichness(ae.signal) {
				continue
			}
			out[key] = ae
		}
	}
	return out
}

// isHTTPEndpointDefinition reports whether e is an HTTP endpoint definition
// (handles the legacy "http_endpoint" kind and the Sub-A split definition kind).
func isHTTPEndpointDefinition(e *graph.Entity) bool {
	switch e.Kind {
	case "http_endpoint_definition", "http_endpoint", "SCOPE.http_endpoint_definition", "SCOPE.http_endpoint":
		return true
	}
	return false
}

// endpointVerbPath extracts the verb and path from an endpoint entity's props.
func endpointVerbPath(e *graph.Entity) (verb, path string) {
	if e.Properties == nil {
		return "", ""
	}
	return e.Properties["verb"], e.Properties["path"]
}

// buildAuthSignal harvests the framework-neutral Signal from an endpoint entity:
// its auth-posture properties, the DRF action context, and the get_permissions
// source body when stamped.
func buildAuthSignal(e *graph.Entity) authposture.Signal {
	sig := authposture.Signal{Props: map[string]string{}}
	if e.Properties != nil {
		for _, k := range authPostureSignalProps {
			if v, ok := e.Properties[k]; ok {
				sig.Props[k] = v
			}
		}
		sig.Framework = e.Properties["framework"]
		// Source fallback: try the framework-neutral source-body props in priority
		// order so a resolver's source-scan path works in the LIVE diff, not only
		// the Django get_permissions case (#4742-#4747).
		for _, k := range authPostureSourceProps {
			if v := strings.TrimSpace(e.Properties[k]); v != "" {
				sig.Source = v
				break
			}
		}
		if a := strings.TrimSpace(e.Properties["effective_action"]); a != "" {
			sig.Action = a
		} else {
			sig.Action = strings.TrimSpace(e.Properties["action"])
		}
	}
	return sig
}

// signalRichness scores how much auth evidence a signal carries, so the richest
// endpoint wins on a join-key collision.
func signalRichness(sig authposture.Signal) int {
	score := len(sig.Props)
	if sig.Source != "" {
		score += 5
	}
	if sig.Action != "" {
		score++
	}
	return score
}

// verdictRank orders verdicts most-actionable first: looser (RBAC regression)
// surfaces before slug_mismatch/kind_mismatch, then stricter, then equivalent.
func verdictRank(v authposture.Verdict) int {
	switch v {
	case authposture.VerdictLooser:
		return 0
	case authposture.VerdictKindMismatch:
		return 1
	case authposture.VerdictSlugMismatch:
		return 2
	case authposture.VerdictStricter:
		return 3
	case authposture.VerdictEquivalent:
		return 4
	default:
		return 5
	}
}

// summariseVerdicts counts records by verdict for the response header.
func summariseVerdicts(records []authPostureRecord) map[string]int {
	m := map[string]int{}
	for _, r := range records {
		m[string(r.Verdict)]++
	}
	return m
}
