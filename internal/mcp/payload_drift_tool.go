// archigraph_payload_drift MCP tool (#2770 Phase 2A substrate).
//
// Returns schema-drift findings produced by the generic drift
// detector at internal/links/payload_drift.go. Schema:
//
//   {
//     "group":   "<group>",
//     "total":   <int>,
//     "findings": [
//       {
//         "endpoint_id":         "<repo>::<id>",
//         "endpoint_name":       "http:POST:/api/users",
//         "direction":           "request" | "response",
//         "producer_repo":       "<slug>",
//         "consumer_repo":       "<slug>",
//         "producer_function":   "create_user",
//         "consumer_function":   "submitNewUserForm",
//         "missing_in_producer": ["foo", "bar"],
//         "missing_in_consumer": ["baz"],
//         "severity":            "high" | "medium" | "low",
//         "confidence":          0.0..1.0,
//         "explanation":         "..."
//       }
//     ]
//   }
//
// Findings are sorted by severity (desc) then endpoint name (asc) then
// direction (asc) — same order the pass emits them in.
//
// Optional arguments:
//   - severity: "high" | "medium" | "low" — return only findings at or
//     above this severity threshold. Default: "low" (i.e. everything).
//   - endpoint: substring match against endpoint_name to narrow the
//     result set (case-sensitive — endpoint names are already
//     canonicalised).
//   - repo: substring match against producer_repo / consumer_repo to
//     narrow to one side of the wire.
//   - limit: max number of findings to return. Default: 50. Honours
//     the #1639 token-ceiling pattern; clients can paginate by
//     re-calling with a tighter severity/endpoint filter.
package mcp

import (
	"context"
	"strings"

	"github.com/cajasmota/archigraph/internal/links"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// handlePayloadDrift implements archigraph_payload_drift.
func (s *Server) handlePayloadDrift(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	group, _, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	// archigraphHome resolves to ~/.archigraph when empty (production
	// path) — same convention every other tool that reads sidecar
	// JSONs uses.
	paths, err := links.PathsFor("", group)
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	findings, err := links.LoadDriftFindings(paths)
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}

	severityFloor := argString(req, "severity", "low")
	endpointFilter := argString(req, "endpoint", "")
	repoFilter := argString(req, "repo", "")
	limit := argInt(req, "limit", 50)
	if limit <= 0 {
		limit = 50
	}

	floor := driftSeverityRank(severityFloor)
	out := make([]map[string]any, 0, len(findings))
	for _, f := range findings {
		if driftSeverityRank(string(f.Severity)) < floor {
			continue
		}
		if endpointFilter != "" && !strings.Contains(f.EndpointName, endpointFilter) {
			continue
		}
		if repoFilter != "" &&
			!strings.Contains(f.ProducerRepo, repoFilter) &&
			!strings.Contains(f.ConsumerRepo, repoFilter) {
			continue
		}
		out = append(out, driftFindingToMap(f))
		if len(out) >= limit {
			break
		}
	}

	return jsonResult(map[string]any{
		"group":     group,
		"total":     len(findings),
		"returned":  len(out),
		"sidecar":   links.DriftSidecarPath(paths),
		"findings":  out,
	}), nil
}

// driftSeverityRank mirrors the rank used inside the pass to order
// findings (high=3, medium=2, low=1). Unknown values fall back to 0
// so the filter accepts everything when the caller passes garbage.
func driftSeverityRank(s string) int {
	switch s {
	case string(links.DriftSeverityHigh):
		return 3
	case string(links.DriftSeverityMedium):
		return 2
	case string(links.DriftSeverityLow):
		return 1
	}
	return 0
}

// driftFindingToMap projects the SchemaDrift struct into the public
// JSON shape. Centralised so future schema additions land in one
// place rather than scattered through ad-hoc map literals.
func driftFindingToMap(f links.SchemaDrift) map[string]any {
	out := map[string]any{
		"endpoint_id":   f.EndpointID,
		"endpoint_name": f.EndpointName,
		"direction":     f.Direction,
		"severity":      string(f.Severity),
		"confidence":    f.Confidence,
		"explanation":   f.Explanation,
	}
	if f.ProducerRepo != "" {
		out["producer_repo"] = f.ProducerRepo
	}
	if f.ConsumerRepo != "" {
		out["consumer_repo"] = f.ConsumerRepo
	}
	if f.ProducerFunction != "" {
		out["producer_function"] = f.ProducerFunction
	}
	if f.ConsumerFunction != "" {
		out["consumer_function"] = f.ConsumerFunction
	}
	if f.ProducerFile != "" {
		out["producer_file"] = f.ProducerFile
	}
	if f.ConsumerFile != "" {
		out["consumer_file"] = f.ConsumerFile
	}
	if len(f.ProducerFields) > 0 {
		out["producer_fields"] = f.ProducerFields
	}
	if len(f.ConsumerFields) > 0 {
		out["consumer_fields"] = f.ConsumerFields
	}
	if len(f.MissingInProducer) > 0 {
		out["missing_in_producer"] = f.MissingInProducer
	}
	if len(f.MissingInConsumer) > 0 {
		out["missing_in_consumer"] = f.MissingInConsumer
	}
	if f.ProducerConfidence > 0 {
		out["producer_confidence"] = f.ProducerConfidence
	}
	if f.ConsumerConfidence > 0 {
		out["consumer_confidence"] = f.ConsumerConfidence
	}
	return out
}
