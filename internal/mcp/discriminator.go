package mcp

import (
	"fmt"
	"sort"
	"strings"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// discriminator.go — fuzzy enum-correction + helpful errors for the consolidated
// canonical MCP tools (#5578, milestone 0.1.6; ref #5546).
//
// The 68→22 tool consolidation moved each tool's discriminator from the tool
// NAME (validated by the MCP tool list) into a param VALUE
// (aspect/kind/direction/detail/scope/action/view). That created a new failure
// mode: a caller passing an invalid value (e.g. aspect=shape instead of
// response_shape) used to be caught by the tool-name registry and now silently
// falls through to a dispatcher default or hard-fails. This turns that malformed
// call into a one-round-trip self-correction by returning a clear error naming
// the closest valid value and the full valid list.
//
// The closest-match is a small case-insensitive Levenshtein over the fixed enum
// set — the same self-correction spirit as the find_callers fuzzy resolver
// (#5476), but over a tiny known value list rather than entity names, so a plain
// edit-distance is both cheaper and a better fit than substring matching.

// validateDiscriminator checks a discriminator param's value against its set of
// accepted values. It returns nil (no error) when:
//   - value is empty / missing — the handler applies its default, never an error;
//   - value is one of accepted (canonical value OR an accepted synonym alias).
//
// On an unrecognised value it returns a helpful *mcpapi.CallToolResult error:
//
//	invalid <param> '<value>' — did you mean '<closest>'? valid: a|b|c
//
// The "did you mean" clause is only included when a close-enough match exists
// (small edit distance); otherwise just the valid list is shown. canonical is
// the list advertised to agents (synonyms are accepted silently but not
// suggested), so the suggestion always points at a value the schema documents.
func validateDiscriminator(param, value string, accepted, canonical []string) *mcpapi.CallToolResult {
	if value == "" {
		return nil // missing → handler default
	}
	vl := strings.ToLower(value)
	for _, a := range accepted {
		if strings.ToLower(a) == vl {
			return nil
		}
	}
	valid := strings.Join(canonical, "|")
	if best := closestEnum(vl, canonical); best != "" {
		return mcpapi.NewToolResultError(fmt.Sprintf(
			"invalid %s %q — did you mean %q? valid: %s", param, value, best, valid))
	}
	return mcpapi.NewToolResultError(fmt.Sprintf(
		"invalid %s %q — valid: %s", param, value, valid))
}

// closestEnum returns the canonical value nearest to probe by case-insensitive
// Levenshtein distance, but only when that distance is small enough to be a
// plausible typo (≤ max(2, len/2) of the shorter string). Returns "" when no
// candidate is close enough — better to show only the valid list than to
// suggest an unrelated value. probe is assumed already lower-cased.
func closestEnum(probe string, canonical []string) string {
	// Substring/abbreviation match wins outright — a probe that is a contiguous
	// fragment of exactly one canonical value (e.g. "shape" in "response_shape",
	// or "auth" already canonical) is almost certainly that value, even though
	// the raw edit distance is large. Prefer the shortest containing value when
	// several contain the probe.
	if len(probe) >= 3 {
		sub := ""
		for _, c := range canonical {
			if strings.Contains(strings.ToLower(c), probe) {
				if sub == "" || len(c) < len(sub) {
					sub = c
				}
			}
		}
		if sub != "" {
			return sub
		}
	}

	best := ""
	bestDist := 1 << 30
	for _, c := range canonical {
		d := levenshtein(probe, strings.ToLower(c))
		if d < bestDist {
			bestDist, best = d, c
		}
	}
	if best == "" {
		return ""
	}
	shorter := len(probe)
	if l := len(best); l < shorter {
		shorter = l
	}
	threshold := shorter / 2
	if threshold < 2 {
		threshold = 2
	}
	if bestDist > threshold {
		return ""
	}
	return best
}

// levenshtein is the classic edit distance between a and b (single-row DP).
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// requireArgs returns a helpful error result when any of the named args is
// missing/empty for the chosen discriminator value, else nil. Used by the
// polymorphic tools (esp. grafel_diff) where the params a call needs depend on
// the discriminator value — turning a downstream "missing X" hard-fail into an
// up-front, value-aware message. names is sorted in the message for stable
// output.
func requireArgs(req mcpapi.CallToolRequest, param, value string, names ...string) *mcpapi.CallToolResult {
	var missing []string
	for _, n := range names {
		if argString(req, n, "") == "" {
			missing = append(missing, n)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return mcpapi.NewToolResultError(fmt.Sprintf(
		"%s=%s requires: %s — missing: %s",
		param, value, strings.Join(names, ", "), strings.Join(missing, ", ")))
}
