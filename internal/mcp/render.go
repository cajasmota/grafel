package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// nodeWithRepo carries an entity together with the repo it lives in. Edges
// reference nodes by their prefixed id (<repo>::<localId>).
type nodeWithRepo struct {
	Repo   string
	Entity *graph.Entity
	Score  float64
}

// renderResult is the structured input to the compact renderer.
type renderResult struct {
	Header         string
	MatchedTotal   int
	Nodes          []nodeWithRepo
	Edges          []renderEdge
	HiddenImpCalls int
	OneRepo        bool
	OneCommunity   *int
	TruncatedNote  string
}

// renderEdge is a directed edge entry in the compact output.
type renderEdge struct {
	From string // label
	To   string // label
	Kind string
}

// prefixedID produces "<repo>::<localId>" per ADR-0009.
func prefixedID(repo, id string) string { return repo + "::" + id }

// splitPrefixed splits "<repo>::<localId>"; returns ("",id) if no prefix.
func splitPrefixed(s string) (string, string) {
	i := strings.Index(s, "::")
	if i < 0 {
		return "", s
	}
	return s[:i], s[i+2:]
}

// estimateTokens approximates token count as len/4 per the brief.
func estimateTokens(s string) int { return len(s) / 4 }

// renderCompact serializes a renderResult to the compact text format and
// enforces tokenBudget. Implicit "calls" edges between visible nodes are
// suppressed; SCOPE. prefix is stripped on user-facing kinds.
func renderCompact(r renderResult, tokenBudget int) string {
	if len(r.Nodes) == 0 {
		return "# no matches\n"
	}
	// Sort nodes by score desc.
	sort.SliceStable(r.Nodes, func(i, j int) bool { return r.Nodes[i].Score > r.Nodes[j].Score })

	var b strings.Builder
	headerLine := fmt.Sprintf("# nodes (%d matched", r.MatchedTotal)
	if r.OneCommunity != nil {
		headerLine += fmt.Sprintf(", community: %d", *r.OneCommunity)
	}
	headerLine += ")"
	b.WriteString(headerLine + "\n")

	visible := map[string]string{} // prefixedID -> label
	shown := 0

	// #1737 — emit ranked hits as TOON when toonWireEnabled and MCP_FIND_FORMAT
	// is not "markdown". The prose header and footer lines stay in markdown; only
	// the per-hit rows move to the tabular encoding, yielding ~40-50% savings on
	// the hits section.
	if toonWireEnabled() && !findFormatMarkdown() {
		// Enforce token budget: add nodes until we would exceed it.
		keep := r.Nodes
		if tokenBudget > 0 {
			// Estimate header already written + TOON table for growing node slice.
			for i := 1; i <= len(r.Nodes); i++ {
				toon := hitsToTOON(r.Nodes[:i], r.OneRepo)
				if estimateTokens(b.String()+toon) > tokenBudget {
					keep = r.Nodes[:i-1]
					break
				}
			}
		}
		if len(keep) > 0 {
			b.WriteString(hitsToTOON(keep, r.OneRepo))
		}
		shown = len(keep)
		// Populate visible for edge rendering.
		for _, nw := range keep {
			visible[prefixedID(nw.Repo, nw.Entity.ID)] = nw.Entity.Name
		}
	} else {
		for i := range r.Nodes {
			nw := r.Nodes[i]
			label := nw.Entity.Name
			loc := fmt.Sprintf("%s:%d", nw.Entity.SourceFile, nw.Entity.StartLine)
			var line string
			if r.OneRepo {
				line = fmt.Sprintf("%s  %s", label, loc)
			} else {
				line = fmt.Sprintf("[%s] %s  %s", nw.Repo, label, loc)
			}
			// Token-budget enforcement: stop adding nodes if the running budget
			// (current rendered text) exceeds the limit.
			if tokenBudget > 0 && estimateTokens(b.String()+line+"\n") > tokenBudget {
				break
			}
			b.WriteString(line + "\n")
			visible[prefixedID(nw.Repo, nw.Entity.ID)] = label
			shown++
		}
	}
	if shown < len(r.Nodes) {
		b.WriteString(fmt.Sprintf("# truncated: %d nodes hidden by token budget\n", len(r.Nodes)-shown))
	}

	// Edges: drop implicit calls between visible nodes, strip SCOPE prefix.
	hidden := r.HiddenImpCalls
	visibleEdges := []renderEdge{}
	for _, e := range r.Edges {
		k := stripScopePrefix(e.Kind)
		if strings.EqualFold(k, "calls") || strings.EqualFold(k, "CALLS") {
			// implicit call between two visible nodes -> hide
			hidden++
			continue
		}
		visibleEdges = append(visibleEdges, renderEdge{From: e.From, To: e.To, Kind: k})
	}
	if len(visibleEdges) > 0 || hidden > 0 {
		b.WriteString(fmt.Sprintf("\n# edges (suppressed: %d implicit calls; shown: %d)\n", hidden, len(visibleEdges)))
		for _, e := range visibleEdges {
			line := fmt.Sprintf("%s → %s  [%s]\n", e.From, e.To, e.Kind)
			if tokenBudget > 0 && estimateTokens(b.String()+line) > tokenBudget {
				b.WriteString("# edges truncated by token budget\n")
				break
			}
			b.WriteString(line)
		}
	}
	if r.TruncatedNote != "" {
		b.WriteString("# " + r.TruncatedNote + "\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// #1663 — compact serializers
// ---------------------------------------------------------------------------

// compactJSON serializes v to minified JSON (no indentation, no trailing
// whitespace). Field names and shapes are unchanged. Returns "null" on error;
// callers that need to detect marshal failure should use json.Marshal directly.
//
// This is the wire-format helper for MCP tool responses (#1663). The package
// disk-bound writers (repair, candidates, docstate, memory notes) intentionally
// keep MarshalIndent because those files are read by humans on disk.
func compactJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(data)
}

// tabularEncode serializes a slice of homogeneous records as a header-prefixed
// row-major table. Format:
//
//	[!schema {f1,f2,f3}]
//	{v1,v2,v3}
//	{v1,v2,v3}
//
// Strings are escaped: backslash, comma, and brace are quoted. Numbers and
// booleans are emitted bare. Nested objects/arrays are emitted as their
// json.Marshal form (single-cell).
//
// Use only for true list-of-record payloads where the schema is fixed across
// every row. For nested or heterogeneous shapes use compactJSON.
//
// NOTE: not wired to any production tool by default. The schema contract that
// MCP callers depend on is plain JSON; opting a tool into tabularEncode is a
// behavioural change that requires caller-side updates. The helper lives here
// so future opt-in payloads can use it; see docs/verify2/mcp-payload-after.md
// for projected savings.
func tabularEncode(schema []string, rows [][]any) string {
	var b strings.Builder
	b.WriteString("[!schema {")
	for i, f := range schema {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(f)
	}
	b.WriteString("}]\n")
	for _, row := range rows {
		b.WriteByte('{')
		for i, v := range row {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(tabularCell(v))
		}
		b.WriteString("}\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// #1672 — TOON wire conversion helpers
// ---------------------------------------------------------------------------

// toonWireEnabled returns true when the MCP_WIRE_FORMAT env var is unset or
// set to "toon". Set MCP_WIRE_FORMAT=json to opt out of TOON encoding and
// receive minified JSON on the wire instead.
func toonWireEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MCP_WIRE_FORMAT")))
	return v == "" || v == "toon"
}

// findFormatMarkdown returns true when the caller has opted out of TOON
// encoding for archigraph_find ranked-hits via MCP_FIND_FORMAT=markdown.
// By default (env unset or "toon") TOON encoding is active.
func findFormatMarkdown() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MCP_FIND_FORMAT")))
	return v == "markdown"
}

// hitsToTOON serialises the ranked-hit nodes from a renderResult as a TOON
// table. Schema: {name, kind, file, line, score}. When oneRepo is false the
// repo column is prepended: {repo, name, kind, file, line, score}.
//
// Token-budget enforcement is the caller's responsibility; this helper encodes
// all rows and returns the full table plus the number of rows written so the
// caller can append a truncation note.
func hitsToTOON(nodes []nodeWithRepo, oneRepo bool) string {
	if len(nodes) == 0 {
		return ""
	}
	var schema []string
	if oneRepo {
		schema = []string{"name", "kind", "file", "line", "score"}
	} else {
		schema = []string{"repo", "name", "kind", "file", "line", "score"}
	}
	rows := make([][]any, 0, len(nodes))
	for _, nw := range nodes {
		kind := stripScopePrefix(nw.Entity.Kind)
		score := fmt.Sprintf("%.2f", nw.Score)
		var row []any
		if oneRepo {
			row = []any{nw.Entity.Name, kind, nw.Entity.SourceFile, nw.Entity.StartLine, score}
		} else {
			row = []any{nw.Repo, nw.Entity.Name, kind, nw.Entity.SourceFile, nw.Entity.StartLine, score}
		}
		rows = append(rows, row)
	}
	return tabularEncode(schema, rows)
}

// recordsToTOON detects whether arr is a homogeneous list of records (every
// element is a map[string]any with the same key set) and, if so, returns the
// TOON-encoded text. Returns ("", false) when the array is not suitable for
// tabular encoding (empty, heterogeneous, non-object elements, etc.).
//
// Key ordering is sorted for determinism; the schema line mirrors the result.
func recordsToTOON(arr []any) (string, bool) {
	if len(arr) == 0 {
		return "", false
	}
	// First pass: collect the canonical key set from the first element.
	first, ok := arr[0].(map[string]any)
	if !ok {
		return "", false
	}
	keys := make([]string, 0, len(first))
	for k := range first {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "", false
	}

	// Second pass: verify every element has exactly the same key set.
	for _, item := range arr[1:] {
		obj, ok := item.(map[string]any)
		if !ok {
			return "", false
		}
		if len(obj) != len(keys) {
			return "", false
		}
		for _, k := range keys {
			if _, has := obj[k]; !has {
				return "", false
			}
		}
	}

	// Build the rows slice for tabularEncode.
	rows := make([][]any, len(arr))
	for i, item := range arr {
		obj := item.(map[string]any)
		row := make([]any, len(keys))
		for j, k := range keys {
			row[j] = obj[k]
		}
		rows[i] = row
	}
	return tabularEncode(keys, rows), true
}

// tabularCell renders one cell value. Strings need escaping for `,` `}` `\`.
func tabularCell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return tabularEscapeString(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", x)
	default:
		// Fallback: minified JSON for nested types.
		data, err := json.Marshal(x)
		if err != nil {
			return ""
		}
		return tabularEscapeString(string(data))
	}
}

func tabularEscapeString(s string) string {
	if !strings.ContainsAny(s, `,}{\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, r := range s {
		switch r {
		case '\\', ',', '{', '}':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
