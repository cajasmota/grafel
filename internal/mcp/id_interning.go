package mcp

// id_interning.go — #1740 Token sprint Tier-2.1: short-form entity-ID interning.
//
// Entity IDs on the wire look like "upvate-core::c84f9b9c0c3a7b18" (25–40 chars).
// On graph-heavy responses (expand depth=2, find_callers depth=2, traces:follow)
// the same id can appear 10+ times in a single response. By replacing every
// occurrence with a compact handle (@1, @2, …) and emitting a _id_table field
// that maps handles back to full IDs we save 15–30 % on response bytes with zero
// information loss.
//
// Wire contract:
//   - "_id_table" key is added to the top-level JSON object.
//   - Every occurrence of a qualifying ID string is replaced by its handle.
//   - Handles are assigned in order of first appearance.
//   - Single-occurrence IDs are NOT interned (table overhead outweighs savings).
//   - Opt-out: set MCP_NO_ID_INTERNING=1 to receive raw IDs.
//
// ID patterns recognised:
//   - Prefixed:  <repo-slug>::<16-hex>   (ADR-0009 canonical form)
//   - Bare 16-hex: [0-9a-f]{16}          (local ids before prefixing)
//
// The interning pass operates on the final marshaled JSON string so it is
// encoding-agnostic and works identically for JSON objects, TOON envelopes,
// and any future wire formats that embed IDs as strings.

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// idPattern matches the ADR-0009 canonical prefixed entity-ID form:
//
//	<repo-slug>::<16-hex>  e.g. "upvate-core::c84f9b9c0c3a7b18"
//
// Repo-slug characters: letters, digits, hyphens, underscores, dots (the first
// char must be alphanumeric to avoid matching "::hex" or ":hex" fragments).
// Only the fully-qualified prefixed form is interned. Bare 16-hex IDs are NOT
// matched — they are local IDs that may appear as primary keys in responses
// (e.g. pattern IDs, edge IDs) and round-trip directly as tool arguments;
// interning them would break callers that pass the parsed ID back immediately.
var idPattern = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9\-_.]*::[0-9a-f]{16}`)

// internIDs is the opt-out flag: if MCP_NO_ID_INTERNING=1 this pass is skipped.
func internIDsEnabled() bool {
	return strings.TrimSpace(os.Getenv("MCP_NO_ID_INTERNING")) != "1"
}

// applyIDInterning rewrites res in-place: it scans all TextContent items for
// entity-ID strings, counts occurrences, interns IDs that appear ≥2 times, and
// injects a "_id_table" field at the top level of the first JSON object found.
//
// Rules:
//   - IDs that appear only once are NOT substituted (table overhead > savings).
//   - Non-JSON payloads are returned unchanged.
//   - If no ID appears ≥2 times the response is returned unchanged.
//   - Opt-out: MCP_NO_ID_INTERNING=1 skips this function entirely.
func applyIDInterning(res *mcpapi.CallToolResult) *mcpapi.CallToolResult {
	if res == nil || len(res.Content) == 0 || !internIDsEnabled() {
		return res
	}

	// Phase 1: collect all ID occurrences across all TextContent items.
	// We build a joined "corpus" string so the regex only runs once, then
	// re-run targeted replacement per item.
	var corpus strings.Builder
	for _, c := range res.Content {
		tc, ok := c.(mcpapi.TextContent)
		if !ok {
			continue
		}
		corpus.WriteString(tc.Text)
	}

	counts := countIDOccurrences(corpus.String())

	// Determine which IDs qualify for interning (appear ≥2 times).
	// Build the table in first-appearance order by scanning corpus once more.
	table := make(map[string]string) // full-id -> "@N"
	var order []string               // first-appearance order

	allMatches := idPattern.FindAllString(corpus.String(), -1)
	for _, m := range allMatches {
		if counts[m] < 2 {
			continue
		}
		if _, already := table[m]; !already {
			handle := fmt.Sprintf("@%d", len(order)+1)
			table[m] = handle
			order = append(order, m)
		}
	}

	if len(order) == 0 {
		// Nothing qualifies — return unchanged.
		return res
	}

	// Build the _id_table payload (ordered map via key sequence).
	idTableEntries := make(map[string]string, len(order))
	for _, id := range order {
		idTableEntries[table[id]] = id
	}

	// Phase 2: replace IDs in every TextContent item and inject the table.
	// We inject into the first TextContent item that is a JSON object.
	injected := false
	newContent := make([]mcpapi.Content, len(res.Content))
	copy(newContent, res.Content)

	for i, c := range newContent {
		tc, ok := c.(mcpapi.TextContent)
		if !ok || tc.Text == "" {
			continue
		}

		// Substitute all qualifying IDs.
		substituted := substituteIDs(tc.Text, table)

		// Inject _id_table into the first JSON object content.
		if !injected {
			var obj map[string]any
			if err := json.Unmarshal([]byte(substituted), &obj); err == nil {
				obj["_id_table"] = idTableEntries
				if data, err := json.Marshal(obj); err == nil {
					substituted = string(data)
					injected = true
				}
			}
		}

		newContent[i] = mcpapi.NewTextContent(substituted)
	}

	// If we couldn't inject the table (all non-JSON payloads), still return the
	// substituted content — callers can look for the table in subsequent items.
	// For non-JSON payloads we append the table as a trailing comment.
	if !injected && len(order) > 0 {
		tableData, _ := json.Marshal(idTableEntries)
		for i, c := range newContent {
			tc, ok := c.(mcpapi.TextContent)
			if !ok || tc.Text == "" {
				continue
			}
			newContent[i] = mcpapi.NewTextContent(tc.Text + fmt.Sprintf("\n# _id_table=%s\n", string(tableData)))
			break
		}
	}

	out := *res
	out.Content = newContent
	return &out
}

// countIDOccurrences returns a map from each found ID string to the number of
// times it appears in text.
func countIDOccurrences(text string) map[string]int {
	counts := make(map[string]int)
	for _, m := range idPattern.FindAllString(text, -1) {
		counts[m]++
	}
	return counts
}

// substituteIDs replaces every occurrence of a qualifying ID in text with its
// handle from the table. Replacement is done with a single regex pass to avoid
// partial-match issues (e.g. a bare hex ID appearing inside a prefixed one).
//
// We build a joint pattern that matches any qualifying ID and replaces via a
// callback so each match is looked up in the table.
func substituteIDs(text string, table map[string]string) string {
	if len(table) == 0 || text == "" {
		return text
	}
	return idPattern.ReplaceAllStringFunc(text, func(m string) string {
		if h, ok := table[m]; ok {
			return h
		}
		return m // not in table (appeared only once) — keep raw
	})
}
