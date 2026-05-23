// quality_bundle.go — unified archigraph_quality tool (#1755).
//
// Folds archigraph_test_coverage, archigraph_find_dead_code,
// archigraph_impact_radius, and archigraph_quality_cycles into a single
// action-dispatched tool following the #1281 / #1639 pattern.
//
// Backward-compat trampolines keep the four legacy tools alive (deprecated).
package mcp

import (
	"context"
	"fmt"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// qualityInjectAction returns a shallow-copied map[string]any of the request's
// arguments with "action" set to the given value. Used by legacy trampolines so
// the original request is not mutated.
func qualityInjectAction(req mcpapi.CallToolRequest, action string) map[string]any {
	src := req.GetArguments()
	dst := make(map[string]any, len(src)+1)
	for k, v := range src {
		dst[k] = v
	}
	dst["action"] = action
	return dst
}

// handleQuality is the unified dispatcher for archigraph_quality.
//
// action=test_coverage — forwards to handleTestCoverage.
//   action-specific args (read from map, not declared in schema):
//     severity          string   high|medium|low|""  filter by severity
//     top_directories   bool     false                include per-dir breakdown
//
// action=dead_code — forwards to handleFindDeadCode.
//   action-specific args:
//     kind_filter       string   ""                   entity kind filter
//
// action=impact_radius — forwards to handleImpactRadius.
//   action-specific args (required):
//     entity_id         string   (required)
//     hops              int      2                    traversal depth [1,6]
//
// action=cycles — forwards to handleQualityCycles.
//   (no additional action-specific args beyond the common set)
//
// Common args (declared in schema): group, cwd, repo_filter, limit.
func (s *Server) handleQuality(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	action := argString(req, "action", "")
	switch action {
	case "test_coverage":
		return s.handleTestCoverage(ctx, req)
	case "dead_code":
		return s.handleFindDeadCode(ctx, req)
	case "impact_radius":
		return s.handleImpactRadius(ctx, req)
	case "cycles":
		return s.handleQualityCycles(ctx, req)
	case "":
		return mcpapi.NewToolResultError(
			"archigraph_quality: action is required. " +
				"Specify action=test_coverage|dead_code|impact_radius|cycles.",
		), nil
	default:
		return mcpapi.NewToolResultError(fmt.Sprintf(
			"archigraph_quality: unknown action %q. "+
				"Valid values: test_coverage, dead_code, impact_radius, cycles.",
			action,
		)), nil
	}
}
