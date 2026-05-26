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
	"reflect"

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

// filterObject filters a JSON object: envelope keys + listed fields are kept.
// Values that are arrays of records are recursively filtered.
//
// #2328: envelope values that are typed slices (e.g. `[]item` from a handler
// using a local struct) are now coerced into []any via reflection so the
// per-record filter applies uniformly — pre-#2328 the legacy path relied on
// marshal+unmarshal to flatten everything into []any before filtering.
func filterObject(obj map[string]any, keep map[string]bool) map[string]any {
	out := make(map[string]any, len(obj))
	for k, v := range obj {
		if envelopeFields[k] {
			out[k] = filterEnvelopeValue(v, keep)
			continue
		}
		if keep[k] {
			out[k] = v
		}
	}
	return out
}

// filterEnvelopeValue applies fields= filtering to a value sitting under a
// known envelope key (results, items, callers, callees, …). It handles
// []any, map[string]any, and — for the typed-struct fast path — typed
// slices and structs reached via reflection.
func filterEnvelopeValue(v any, keep map[string]bool) any {
	switch payload := v.(type) {
	case []any:
		return filterArray(payload, keep)
	case []map[string]any:
		arr := make([]any, len(payload))
		for i, m := range payload {
			arr[i] = m
		}
		return filterArray(arr, keep)
	case map[string]any:
		return filterNestedObject(payload, keep)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		arr := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			arr[i] = reflectStructToMap(rv.Index(i))
		}
		return filterArray(arr, keep)
	case reflect.Ptr:
		if rv.IsNil() {
			return v
		}
		return filterEnvelopeValue(rv.Elem().Interface(), keep)
	}
	return v
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

// applyFieldsToValue applies `fields=` filtering to a structured handler value
// (map[string]any, []any, []map[string]any, or a typed struct/struct-slice
// reachable via reflection). Returns a value of the same broad shape, with
// per-record keys not in `fields` stripped. Envelope keys (envelopeFields) are
// always preserved.
//
// #2328: this is the reflection-based variant that lets `fields=` callers ride
// the single-marshal fast path. The pre-#2328 `applyFieldsToResult` required
// the value to first be marshaled and re-parsed into map[string]any/[]any; this
// variant works directly on the in-memory handler value.
//
// Behaviour parity with applyFieldsToResult:
//   - map[string]any input: filterObject (envelope-aware, recurses into known
//     list keys)
//   - []any / []map[string]any: filterArray on each record
//   - typed struct: converted to map[string]any via reflection, then filtered
//   - typed []struct: each element converted, filtered, returned as []any
//   - scalars / unknown shapes: returned unchanged
//
// `keep` is the precomputed lookup table built from the fields slice (callers
// who already have one avoid a second allocation).
func applyFieldsToValue(v any, keep map[string]bool) any {
	if v == nil || keep == nil {
		return v
	}
	switch payload := v.(type) {
	case map[string]any:
		return filterObject(payload, keep)
	case []any:
		return filterArray(payload, keep)
	case []map[string]any:
		arr := make([]any, len(payload))
		for i, m := range payload {
			arr[i] = m
		}
		return filterArray(arr, keep)
	}
	// Reflection path: typed structs / slices of typed structs. We convert
	// to map[string]any/[]any via JSON tag-aware reflection, then route
	// through the existing filter helpers so the filtered shape is
	// identical to what the legacy post-unmarshal path produced.
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		arr := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			arr[i] = reflectStructToMap(rv.Index(i))
		}
		return filterArray(arr, keep)
	case reflect.Struct:
		converted := reflectStructToMap(rv)
		if m, ok := converted.(map[string]any); ok {
			return filterObject(m, keep)
		}
		return converted
	case reflect.Ptr:
		if rv.IsNil() {
			return v
		}
		return applyFieldsToValue(rv.Elem().Interface(), keep)
	}
	return v
}

// reflectStructToMap converts a struct (or any/interface wrapping one) into a
// map keyed by JSON tag, mirroring what json.Marshal+Unmarshal would produce.
// Falls back to returning the original interface for non-struct kinds — the
// caller will treat that as an opaque record.
func reflectStructToMap(rv reflect.Value) any {
	// Unwrap interface / pointer layers.
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Map {
		// Already a map; convert to map[string]any so filterArray's record
		// type assertion succeeds.
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			k, ok := iter.Key().Interface().(string)
			if !ok {
				return rv.Interface()
			}
			out[k] = iter.Value().Interface()
		}
		return out
	}
	if rv.Kind() != reflect.Struct {
		return rv.Interface()
	}
	t := rv.Type()
	out := make(map[string]any, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, omitempty := jsonFieldName(f)
		if name == "-" {
			continue
		}
		fv := rv.Field(i)
		if omitempty && fv.IsZero() {
			continue
		}
		out[name] = fv.Interface()
	}
	return out
}

// jsonFieldName extracts the JSON wire name (and omitempty flag) for a struct
// field, falling back to the Go name when no tag is present.
func jsonFieldName(f reflect.StructField) (string, bool) {
	tag, ok := f.Tag.Lookup("json")
	if !ok || tag == "" {
		return f.Name, false
	}
	name := tag
	omitempty := false
	if i := indexByte(tag, ','); i >= 0 {
		name = tag[:i]
		rest := tag[i+1:]
		for len(rest) > 0 {
			j := indexByte(rest, ',')
			var part string
			if j < 0 {
				part = rest
				rest = ""
			} else {
				part = rest[:j]
				rest = rest[j+1:]
			}
			if part == "omitempty" {
				omitempty = true
			}
		}
	}
	if name == "" {
		name = f.Name
	}
	return name, omitempty
}

// indexByte avoids pulling in strings just for one IndexByte call.
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
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
