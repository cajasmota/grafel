// grafel_response_shape_diff MCP tool (#4424, epic #4419 capability E — the
// LAST of the four cross-graph parity diff tools).
//
// Per joined oracle↔v3 endpoint pair, it diffs the RESPONSE contract
// BRANCH-AWARE: it aligns the two sides' response branches by HTTP STATUS, and
// for each status present on both sides diffs the response FIELD SET (fields
// only-in-oracle / only-in-v3 / type-mismatched / optionality-mismatched). A
// status present on one side and absent on the other is reported as
// status_set_drift (the #4424 "oracle has a 409 the v3 collapsed/dropped" case).
//
// COMPOSITION (this tool builds nothing new — it composes existing machinery):
//   - JOIN: the shared cross-group endpoint-join normalizer (endpoint_join.go,
//     #4550) — oracle and v3 endpoints bucket on the same normalised
//     method+path key the cross-repo HTTP link resolver uses.
//   - PER-SIDE RESPONSE CONTRACT: computeEffectiveContract (#4601/#4711) — the
//     framework-PLUGGABLE effective-contract resolver. Its per-handler
//     ResponseBranches come from the #4423 effects-branches facet read off the
//     REAL overridden http_endpoint_definition body (NOT a synthesized mixin
//     contract — the #756 F1 false-"full object" trap). DRF / NestJS / Spring /
//     FastAPI / Express all flow through it uniformly.
//   - FIELD SET per branch: each branch's response Shape descriptor
//     ("Response{id,email}", "UserSerializer", a DTO leaf) is resolved to a field
//     set — braced shapes are parsed directly; a bare serializer/DTO name is
//     expanded via dtoFieldsByProperty (#4635, the SCOPE.Schema field membership).
//   - ALIGNMENT: literalparity.CanonicalKey (#4664) folds snake_case↔camelCase so
//     a casing-only difference never false-positives.
//   - DIFF CORE: internal/responseshapediff (unit-tested independent of MCP) does
//     the branch-aware status/field diff + the verdict.
//
// Verdict per endpoint: equivalent | drift | unresolved. unresolved is honest —
// when a side's response shape cannot be resolved to a field set on ANY branch we
// refuse to call the pair equivalent (mirrors literal_parity's unresolved
// handling #4665 and avoids a false full-object equivalence).
//
// Signature:
//
//	response_shape_diff(
//	  group_oracle: "<oracle group>",   (required — the behavioral oracle)
//	  group_v3:     "<v3-rewrite group>", (required)
//	  endpoint:     "<verb path substring>", (optional — narrow to one endpoint)
//	  format:       "terse" | "full",   (optional — default terse)
//	)
//
// Live cross-graph validation (oracle acme vs v3 acme-v3) is DEPLOY-GATED:
// it needs a live reindex of BOTH groups so the endpoints carry current branch /
// DTO field-membership properties. The diff + composition logic is unit-tested in
// internal/responseshapediff and exercised here by a stubbed 2-group store.
package mcp

import (
	"context"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/responseshapediff"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// responseShapeRecord is one joined endpoint's response-shape diff record.
type responseShapeRecord struct {
	Endpoint string                    `json:"endpoint"`
	Verdict  responseshapediff.Verdict `json:"verdict"`
	Result   responseshapediff.Result  `json:"result"`
}

// handleResponseShapeDiff implements grafel_response_shape_diff.
func (s *Server) handleResponseShapeDiff(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
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

	oracleEndpoints := collectResponseContracts(lgOracle)
	v3Endpoints := collectResponseContracts(lgV3)

	var records []responseShapeRecord
	for key, oc := range oracleEndpoints {
		vc, ok := v3Endpoints[key]
		if !ok {
			continue // unlinked oracle endpoint — nothing to diff against
		}
		if endpointFilter != "" && !strings.Contains(strings.ToLower(oc.display), endpointFilter) {
			continue
		}
		res := responseshapediff.Diff(oc.contract, vc.contract)
		rec := responseShapeRecord{
			Endpoint: oc.display,
			Verdict:  res.Verdict,
			Result:   res,
		}
		if format != "full" {
			// terse: keep the verdict + drift summary, drop the per-record note unless
			// it explains an unresolved (the actionable case).
			if res.Verdict != responseshapediff.VerdictUnresolved {
				rec.Result.Note = ""
			}
		}
		records = append(records, rec)
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Verdict != records[j].Verdict {
			return responseVerdictRank(records[i].Verdict) < responseVerdictRank(records[j].Verdict)
		}
		return records[i].Endpoint < records[j].Endpoint
	})

	summary := summariseResponseVerdicts(records)
	if records == nil {
		records = []responseShapeRecord{}
	}
	return jsonResult(map[string]any{
		"group_oracle":     oracleGroup,
		"group_v3":         v3Group,
		"joined":           len(records),
		"oracle_endpoints": len(oracleEndpoints),
		"v3_endpoints":     len(v3Endpoints),
		"summary":          summary,
		"records":          records,
		"note":             "live cross-graph validation requires a deploy-window reindex of both groups (#4424)",
	}), nil
}

// responseEndpoint is one harvested endpoint: a human display label and its
// composed per-branch response contract for the diff core.
type responseEndpoint struct {
	display  string
	contract responseshapediff.Contract
}

// collectResponseContracts builds, per group, a map keyed by the SHARED
// cross-group endpoint-join key (normalised verb+path, #4550) → the endpoint's
// composed response Contract (per-branch field sets).
//
// It drives the per-side response shape off computeEffectiveContract (#4601):
// every distinct ViewSet/controller leaf the group's endpoint definitions /
// router-expanded routes attribute is resolved ONCE, and each resolved handler's
// ResponseBranches (from the #4423 effects-branches facet over the REAL endpoint
// body) are projected into responseshapediff.Branch field sets. Reading the
// per-branch facet — not a synthesized mixin contract — is the #756 F1 guard
// against the false "full object" trap.
//
// On a join-key collision (a ViewSet expanded per-action) the contract carrying
// the more-resolved response set wins, so the diff sees the richest shape.
func collectResponseContracts(lg *LoadedGroup) map[endpointJoinKey]responseEndpoint {
	out := map[endpointJoinKey]responseEndpoint{}

	// repoForHandler lets us expand a bare serializer/DTO shape to its field set.
	for _, target := range collectContractTargets(lg) {
		ecRes := computeEffectiveContract(lg, target.target)
		for gi := range ecRes.Groups {
			g := &ecRes.Groups[gi]
			r := repoByName(lg, g.Repo)
			for hi := range g.Handlers {
				h := &g.Handlers[hi]
				verb, path := h.Verb, h.Path
				if path == "" {
					path = target.path
				}
				if verb == "" {
					verb = target.verb
				}
				if path == "" {
					continue
				}
				key := newEndpointJoinKey(verb, path)
				contract := composeResponseContract(r, h)
				disp := strings.TrimSpace(strings.ToUpper(verb) + " " + path)
				if disp == "" {
					disp = h.Handler
				}
				cand := responseEndpoint{display: disp, contract: contract}
				if existing, ok := out[key]; ok && contractResolution(existing.contract) >= contractResolution(cand.contract) {
					continue
				}
				out[key] = cand
			}
		}
	}
	return out
}

// contractTarget is a ViewSet/controller to resolve plus the verb/path of the
// endpoint that pointed at it (used as a fallback when the resolved handler
// carries no verb/path of its own).
type contractTarget struct {
	target string
	verb   string
	path   string
}

// collectContractTargets enumerates the distinct ViewSet/controller targets to
// feed computeEffectiveContract: one per owning class of every server-side HTTP
// endpoint definition (and router-expanded route) in the group. Deduped by
// resolved target string so each ViewSet is resolved once.
func collectContractTargets(lg *LoadedGroup) []contractTarget {
	seen := map[string]bool{}
	var out []contractTarget
	for _, r := range lg.Repos {
		if r == nil || r.Doc == nil {
			continue
		}
		r.forEachEntity(func(e *graph.Entity) bool {
			if !isHTTPEndpointDefinition(e) && !isRouterExpandedRoute(e) {
				return true
			}
			owner := endpointOwningClass(e)
			if owner == "" {
				return true
			}
			verb, path := endpointVerbPath(e)
			key := strings.ToLower(owner)
			if seen[key] {
				return true
			}
			seen[key] = true
			out = append(out, contractTarget{target: owner, verb: verb, path: path})
			return true
		})
	}
	return out
}

// endpointOwningClass returns the ViewSet/controller leaf name that owns an
// endpoint entity, from the properties the route/endpoint extractors stamp:
// drf_view_method ("ViewSet.method"), controller / controller_class, or the
// owning class encoded in a qualified handler name. Empty when no attribution.
func endpointOwningClass(e *graph.Entity) string {
	if e.PropLen() == 0 {
		return ""
	}
	if dvm := strings.TrimSpace(e.PropGet("drf_view_method")); dvm != "" {
		if owning := prefixBeforeDot(dvm); owning != "" {
			return leafAfterDot(owning)
		}
		return leafAfterDot(dvm)
	}
	for _, k := range []string{"controller", "controller_class", "owner_class", "view_class", "handler_class"} {
		if v := strings.TrimSpace(e.PropGet(k)); v != "" {
			return leafAfterDot(v)
		}
	}
	if h := strings.TrimSpace(e.PropGet("handler")); h != "" {
		if owning := prefixBeforeDot(h); owning != "" {
			return leafAfterDot(owning)
		}
	}
	return ""
}

// composeResponseContract projects an effectiveContract's per-branch response
// shapes into a responseshapediff.Contract (status→field set). Each branch's
// Shape descriptor is resolved to a field set; a branch whose shape cannot be
// turned into one is recorded Resolved:false so the diff is honest-partial for
// that status rather than reporting every field as missing.
func composeResponseContract(r *LoadedRepo, c *effectiveContract) responseshapediff.Contract {
	out := responseshapediff.Contract{}
	for _, b := range c.ResponseBranches {
		if b.Status == 0 {
			continue
		}
		fields, resolved := resolveBranchFields(r, b.Shape, c.Serializer)
		out.Branches = append(out.Branches, responseshapediff.Branch{
			Status:   b.Status,
			Fields:   fields,
			Resolved: resolved,
		})
		if resolved {
			out.ResolvedAny = true
		}
	}
	return out
}

// resolveBranchFields turns a branch's Shape descriptor into a field set.
//
//   - "Wrapper{a,b}" / "dict{a,b}" / "{a,b}" → the braced keys (parsed directly).
//   - a bare serializer/DTO leaf ("UserSerializer", "UserDto") → expanded via the
//     SCOPE.Schema field membership (dtoFieldsByProperty, #4635).
//   - empty shape, but a static serializer is declared on the contract → expand
//     that serializer (the DRF success-branch case where the body is the
//     serializer with no inline dict).
//
// resolved=false when no field set could be derived (honest-partial).
func resolveBranchFields(r *LoadedRepo, shape, serializer string) ([]responseshapediff.Field, bool) {
	shape = strings.TrimSpace(shape)

	// 1. Braced field set in the shape descriptor.
	if keys := bracedKeys(shape); keys != nil {
		return fieldsFromNames(keys), true
	}

	// 2. A bare type name in the shape → expand as a DTO/serializer.
	if typeName := bareTypeName(shape); typeName != "" {
		if fs := expandDTOFields(r, typeName); len(fs) > 0 {
			return fs, true
		}
	}

	// 3. Fall back to the contract's static serializer (DRF success branch).
	if serializer != "" {
		if fs := expandDTOFields(r, serializer); len(fs) > 0 {
			return fs, true
		}
	}

	return nil, false
}

// bracedKeys extracts the comma-separated keys inside the FIRST {...} of a shape
// descriptor. Returns nil when there is no brace pair (so the caller can try a
// DTO expansion instead). An empty pair "{}" returns a non-nil empty slice
// (resolved, but no fields).
func bracedKeys(shape string) []string {
	open := strings.Index(shape, "{")
	if open < 0 {
		return nil
	}
	close := strings.LastIndex(shape, "}")
	if close <= open {
		return nil
	}
	inner := strings.TrimSpace(shape[open+1 : close])
	if inner == "" {
		return []string{}
	}
	var keys []string
	for _, tok := range strings.Split(inner, ",") {
		k := strings.TrimSpace(tok)
		// drop a "key: type" annotation, keep the key.
		if i := strings.IndexAny(k, ":="); i >= 0 {
			k = strings.TrimSpace(k[:i])
		}
		k = strings.Trim(k, "'\"")
		if k != "" && k != "..." {
			keys = append(keys, k)
		}
	}
	return keys
}

// bareTypeName returns the shape descriptor as a type name when it is a plain
// identifier with no braces / punctuation (a serializer or DTO leaf the branch
// returns directly). Empty otherwise.
func bareTypeName(shape string) string {
	if shape == "" || strings.ContainsAny(shape, "{}()[]<>,") {
		return ""
	}
	return leafAfterDot(shape)
}

// expandDTOFields resolves a serializer/DTO leaf name to its response field set
// via the shared SCOPE.Schema field-membership helper (#4635). Returns the
// fields with their declared type + optionality so the per-status field diff can
// surface type / optionality mismatches.
func expandDTOFields(r *LoadedRepo, typeName string) []responseshapediff.Field {
	if r == nil || typeName == "" {
		return nil
	}
	cfs := dtoFieldsByProperty(r, leafAfterDot(typeName))
	if len(cfs) == 0 {
		return nil
	}
	out := make([]responseshapediff.Field, 0, len(cfs))
	for _, cf := range cfs {
		out = append(out, responseshapediff.Field{
			Name:     cf.Name,
			Type:     cf.Type,
			Optional: !cf.Required,
		})
	}
	return out
}

// fieldsFromNames builds a field set from bare names (no type/optionality info —
// the braced-shape case carries names only).
func fieldsFromNames(names []string) []responseshapediff.Field {
	out := make([]responseshapediff.Field, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		out = append(out, responseshapediff.Field{Name: n})
	}
	return out
}

// contractResolution scores how resolved a contract is (resolved branches +
// total fields), so the richest contract wins on a join-key collision.
func contractResolution(c responseshapediff.Contract) int {
	score := 0
	for _, b := range c.Branches {
		if b.Resolved {
			score += 1 + len(b.Fields)
		}
	}
	return score
}

// repoByName returns the LoadedRepo with the given repo name, or nil.
func repoByName(lg *LoadedGroup, name string) *LoadedRepo {
	if name == "" {
		return nil
	}
	for _, r := range lg.Repos {
		if r != nil && r.Repo == name {
			return r
		}
	}
	return nil
}

// responseVerdictRank orders records most-actionable first: drift surfaces before
// unresolved before equivalent.
func responseVerdictRank(v responseshapediff.Verdict) int {
	switch v {
	case responseshapediff.VerdictDrift:
		return 0
	case responseshapediff.VerdictUnresolved:
		return 1
	case responseshapediff.VerdictEquivalent:
		return 2
	default:
		return 3
	}
}

// summariseResponseVerdicts counts records by verdict for the response header.
func summariseResponseVerdicts(records []responseShapeRecord) map[string]int {
	m := map[string]int{}
	for _, r := range records {
		m[string(r.Verdict)]++
	}
	return m
}
