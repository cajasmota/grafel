// grafel_security_findings MCP tool (#2772 Phase 2B substrate).
//
// Returns the SecurityFinding records produced by the taint-flow pass
// (internal/links/taint_flow.go), ranked by confidence. Filters by
// category, minimum confidence, and source/sink entity-id prefix so
// the agent can drill into specific finding shapes.
//
// Response schema:
//
//	{
//	  "group":     "<group-name>",
//	  "count":     <int>,
//	  "findings":  [
//	    {
//	      "fingerprint":      "<hex>",
//	      "category":         "sql_injection" | ...,
//	      "confidence":       0.85,
//	      "source":           { "id", "name", "kind", "qualified_name",
//	                            "repo", "source_file", "primitive",
//	                            "line" },
//	      "sink":             { ... mirror of source },
//	      "path":             ["entity-id-1", ..., "entity-id-N"],
//	      "explanation":      "Tainted data from req.body reaches a raw
//	                           SQL exec on line 42 without a
//	                           parameterised-query sanitizer."
//	    },
//	    ...
//	  ],
//	  "by_category": { "sql_injection": 3, ... }
//	}
//
// Confidence-floor reasoning: the taint pass drops findings below
// 0.5 (#2772 spec: conservative > aggressive). The default
// `min_confidence` here is 0.7 to surface only high-quality findings;
// callers can lower it explicitly when they want every cell the pass
// computed.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/links"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// defaultMinFindingConfidence is the floor applied when the caller does
// not specify min_confidence. Chosen to match the grafel-security-
// audit skill's auto-submit threshold so findings surfaced here are the
// same set the skill would auto-confirm.
const defaultMinFindingConfidence = 0.7

// handleSecurityFindings implements grafel_security_findings. Args:
//   - category        (optional): filter to one of the TaintCategory* keys
//   - min_confidence  (optional, default 0.7): lower bound, in [0, 1]
//   - limit           (optional, default 50): cap on returned findings
//   - source_repo     (optional): filter to findings whose source entity
//     is in the named repo
func (s *Server) handleSecurityFindings(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	if lg.LinksFile == "" {
		return jsonResult(map[string]any{
			"group":       lg.Name,
			"count":       0,
			"findings":    []any{},
			"by_category": map[string]int{},
			"note":        "no links sidecar configured for this group; taint pass has not run",
		}), nil
	}
	sidecar := strings.TrimSuffix(lg.LinksFile, ".json") + "-taint.json"
	raw, err := os.ReadFile(sidecar)
	if err != nil {
		if os.IsNotExist(err) {
			return jsonResult(map[string]any{
				"group":       lg.Name,
				"count":       0,
				"findings":    []any{},
				"by_category": map[string]int{},
				"note":        "no taint sidecar found; re-run the indexer to produce one",
			}), nil
		}
		return mcpapi.NewToolResultError(fmt.Sprintf("read taint sidecar: %v", err)), nil
	}
	var doc struct {
		Version  int                     `json:"version"`
		Method   string                  `json:"method"`
		Findings []links.SecurityFinding `json:"findings"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return mcpapi.NewToolResultError(fmt.Sprintf("parse taint sidecar: %v", err)), nil
	}

	categoryFilter := argString(req, "category", "")
	minConf := argFloat(req, "min_confidence", defaultMinFindingConfidence)
	limit := argInt(req, "limit", 50)
	sourceRepo := argString(req, "source_repo", "")

	out := make([]map[string]any, 0, len(doc.Findings))
	byCategory := map[string]int{}
	for _, f := range doc.Findings {
		if f.Confidence < minConf {
			continue
		}
		if categoryFilter != "" && string(f.Category) != categoryFilter {
			continue
		}
		if sourceRepo != "" && f.Repo != sourceRepo {
			continue
		}
		sourceEnt := resolveEntityByPrefixed(lg, f.SourceID)
		sinkEnt := resolveEntityByPrefixed(lg, f.SinkID)
		out = append(out, map[string]any{
			"fingerprint": f.Fingerprint,
			"category":    string(f.Category),
			"confidence":  f.Confidence,
			"source":      buildFindingEndpoint(sourceEnt, f.SourceID, f.SourcePrimitive, f.SourceLine),
			"sink":        buildFindingEndpoint(sinkEnt, f.SinkID, f.SinkPrimitive, f.SinkLine),
			"path":        f.Path,
			"explanation": buildFindingExplanation(f, sourceEnt, sinkEnt),
		})
		byCategory[string(f.Category)]++
	}
	// Doc is already sorted by confidence descending; preserve that
	// order across filtering then apply the limit.
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return jsonResult(map[string]any{
		"group":            lg.Name,
		"count":            len(out),
		"findings":         out,
		"by_category":      byCategory,
		"min_confidence":   minConf,
		"confidence_floor": links.TaintFindingFloor(),
		"taint_method":     doc.Method,
	}), nil
}

// resolveEntityByPrefixed returns the resolved entity for a "<repo>:<id>"
// prefixed key, or nil when no repo / entity matches. Mirrors the
// resolver pattern from effects_tool.go.
func resolveEntityByPrefixed(lg *LoadedGroup, key string) *graph.Entity {
	repo, local := splitPrefixed(key)
	if repo == "" {
		return nil
	}
	r, ok := lg.Repos[repo]
	if !ok || r.Doc == nil {
		return nil
	}
	if r.LabelIndex == nil {
		return nil
	}
	if e := r.LabelIndex.ByID(local); e != nil {
		return e
	}
	return nil
}

func buildFindingEndpoint(e *graph.Entity, id, primitive string, line int) map[string]any {
	out := map[string]any{
		"id":        id,
		"primitive": primitive,
		"line":      line,
	}
	if e != nil {
		out["name"] = e.Name
		out["kind"] = e.Kind
		out["qualified_name"] = e.QualifiedName
		out["source_file"] = e.SourceFile
	}
	return out
}

func buildFindingExplanation(f links.SecurityFinding, src, sink *graph.Entity) string {
	srcLabel := f.SourceID
	if src != nil && src.Name != "" {
		srcLabel = src.Name
	}
	sinkLabel := f.SinkID
	if sink != nil && sink.Name != "" {
		sinkLabel = sink.Name
	}
	hops := len(f.Path) - 1
	if hops < 0 {
		hops = 0
	}
	cat := categoryNarrative(f.Category)
	if hops == 0 {
		return fmt.Sprintf(
			"%s: tainted input from %s (%s, line %d) reaches the sink %s (%s, line %d) inside the same function with no sanitizer in between. Confidence %.2f.",
			cat, srcLabel, f.SourcePrimitive, f.SourceLine, sinkLabel, f.SinkPrimitive, f.SinkLine, f.Confidence,
		)
	}
	return fmt.Sprintf(
		"%s: tainted input from %s (%s, line %d) flows through %d call hop(s) to the sink %s (%s, line %d) without a sanitizer of category %s on the path. Confidence %.2f.",
		cat, srcLabel, f.SourcePrimitive, f.SourceLine, hops, sinkLabel, f.SinkPrimitive, f.SinkLine, f.Category, f.Confidence,
	)
}

// categoryNarrative returns a short prose label for the finding's
// category. Used by the explanation builder so the agent gets a
// human-readable lead rather than a slug.
func categoryNarrative(cat any) string {
	s := fmt.Sprintf("%v", cat)
	switch s {
	case "sql_injection":
		return "SQL injection"
	case "command_injection":
		return "Command / dynamic-code injection"
	case "path_traversal":
		return "Path traversal"
	case "xss":
		return "Cross-site scripting"
	case "redos":
		return "Regular-expression DoS"
	case "deserialization":
		return "Unsafe deserialization"
	case "ssrf":
		return "Server-side request forgery"
	}
	return "Security finding"
}

// orderForStableOutput sorts the by_category map for stable JSON
// output. Not strictly necessary because the encoder sorts map keys,
// but documents intent for future readers.
//
//nolint:unused // referenced when stable map-output is debugged.
func orderForStableOutput(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
