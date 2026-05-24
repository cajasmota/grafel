// fields_filter.go — GraphQL-style `fields=` selection support (#1741).
//
// Callers know what they need; let them ask for it. Every list/object-returning
// MCP tool that opts in accepts an optional `fields` []string arg. When present,
// fields not listed are stripped from result objects. The envelope keys
// (elapsed_ms, count, items, truncation_note, etc.) are always preserved.
//
// When `fields` is absent the response shape is unchanged from the default.
//
// Per-tool field whitelists are documented in SCHEMA.md.
package mcp

import (
	"encoding/json"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// envelopeFields are top-level keys that are NEVER stripped regardless of the
// caller's `fields=` selection. They carry metadata (latency, totals,
// truncation, no-edge signals) the agent needs in order to interpret a result.
var envelopeFields = map[string]bool{
	"items":           true,
	"count":           true,
	"total":           true,
	"truncated":       true,
	"truncation_note": true,
	"elapsed_ms":      true,
	"result":          true,
	"note":            true,
	"_id_table":       true,
	"matches":         true,
	"results":         true,
	"callers":         true,
	"callees":         true,
	"neighbors":       true,
	"affected":        true,
	"nodes":           true,
	"edges":           true,
	"entity_id":       true, // root identity for single-entity tools (callers/callees)
	"entity_name":     true,
	"repo":            true,
	"depth":           true,
	"direction":       true,
}

// fieldsArg extracts and normalises the `fields` request argument. Returns nil
// when not present or empty — callers should treat nil as "no filter".
func fieldsArg(req mcpapi.CallToolRequest) []string {
	raw := argStringSlice(req, "fields")
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyFieldsToResult walks a CallToolResult's JSON text payload and strips
// per-record fields not present in `fields`. Envelope keys (envelopeFields)
// are preserved. Records nested under known list keys (items/results/callers/
// callees/neighbors/affected/matches/nodes) are filtered.
//
// Best-effort: any parse failure returns the input unchanged.
func applyFieldsToResult(res *mcpapi.CallToolResult, fields []string) *mcpapi.CallToolResult {
	if res == nil || len(fields) == 0 || len(res.Content) == 0 {
		return res
	}
	keep := make(map[string]bool, len(fields))
	for _, f := range fields {
		keep[f] = true
	}
	for i, c := range res.Content {
		tc, ok := c.(mcpapi.TextContent)
		if !ok || tc.Text == "" {
			continue
		}
		// Try object then array.
		var obj map[string]any
		if err := json.Unmarshal([]byte(tc.Text), &obj); err == nil {
			filtered := filterObject(obj, keep)
			if data, err := json.Marshal(filtered); err == nil {
				res.Content[i] = mcpapi.NewTextContent(string(data))
			}
			continue
		}
		var arr []any
		if err := json.Unmarshal([]byte(tc.Text), &arr); err == nil {
			filtered := filterArray(arr, keep)
			if data, err := json.Marshal(filtered); err == nil {
				res.Content[i] = mcpapi.NewTextContent(string(data))
			}
		}
	}
	return res
}

// filterObject filters a JSON object: envelope keys + listed fields are kept.
// Values that are arrays of records are recursively filtered.
func filterObject(obj map[string]any, keep map[string]bool) map[string]any {
	out := make(map[string]any, len(obj))
	for k, v := range obj {
		if envelopeFields[k] {
			// Recurse into known list-shaped envelope values.
			if arr, ok := v.([]any); ok {
				out[k] = filterArray(arr, keep)
			} else if sub, ok := v.(map[string]any); ok {
				// e.g. matches: { ... } — leave nested objects alone unless they
				// contain a list of records.
				out[k] = filterNestedObject(sub, keep)
			} else {
				out[k] = v
			}
			continue
		}
		if keep[k] {
			out[k] = v
		}
	}
	return out
}

// filterNestedObject recurses one level deeper, filtering arrays it finds.
func filterNestedObject(obj map[string]any, keep map[string]bool) map[string]any {
	out := make(map[string]any, len(obj))
	for k, v := range obj {
		if arr, ok := v.([]any); ok {
			out[k] = filterArray(arr, keep)
			continue
		}
		out[k] = v
	}
	return out
}

// filterArray filters each record (object) in an array.
func filterArray(arr []any, keep map[string]bool) []any {
	out := make([]any, 0, len(arr))
	for _, item := range arr {
		rec, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		filtered := make(map[string]any, len(rec))
		for k, v := range rec {
			if keep[k] {
				filtered[k] = v
			}
		}
		// Preserve at least the first identifying field if the caller's
		// selection misses everything (avoid silent empty rows).
		if len(filtered) == 0 {
			for _, idKey := range []string{"id", "entity_id", "name"} {
				if v, ok := rec[idKey]; ok {
					filtered[idKey] = v
					break
				}
			}
		}
		out = append(out, filtered)
	}
	return out
}
