package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// defaultRegistryPath is the canonical on-disk location of the registry.
const defaultRegistryPath = "docs/coverage/registry.json"

// loadRegistry reads and decodes the registry from path.
func loadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var reg Registry
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reg); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if reg.SchemaVersion == 0 {
		reg.SchemaVersion = SchemaVersion
	}
	return &reg, nil
}

// saveRegistry sorts records and persists the registry atomically using
// a temp file + rename. Output is deterministic: records sorted by ID,
// capability maps marshalled in sorted-key order, 2-space indent,
// trailing newline.
func saveRegistry(path string, reg *Registry) error {
	reg.SchemaVersion = SchemaVersion
	sortRegistry(reg)
	buf, err := marshalRegistry(reg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".coverage.json.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// sortRegistry sorts records by ID and also sorts each record's cites
// slice deterministically. Capability map ordering is handled at
// marshal time. Both flat and grouped capability shapes have their
// cite slices normalised.
func sortRegistry(reg *Registry) {
	sort.Slice(reg.Records, func(i, j int) bool {
		return reg.Records[i].ID < reg.Records[j].ID
	})
	for i := range reg.Records {
		for k, cap := range reg.Records[i].Capabilities {
			sort.Strings(cap.Cites)
			reg.Records[i].Capabilities[k] = cap
		}
		for g, inner := range reg.Records[i].Groups {
			for k, cap := range inner {
				sort.Strings(cap.Cites)
				inner[k] = cap
			}
			reg.Records[i].Groups[g] = inner
		}
		for g, inner := range reg.Records[i].FrameworkSpecific {
			for k, cap := range inner {
				sort.Strings(cap.Cites)
				inner[k] = cap
			}
			reg.Records[i].FrameworkSpecific[g] = inner
		}
	}
}

// marshalRegistry produces deterministic JSON bytes: records sorted,
// capability map keys sorted, 2-space indent, trailing newline.
func marshalRegistry(reg *Registry) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("{\n")
	fmt.Fprintf(&buf, "  \"$schema_version\": %d,\n", reg.SchemaVersion)
	buf.WriteString("  \"records\": [")
	for i, rec := range reg.Records {
		if i == 0 {
			buf.WriteString("\n")
		}
		if err := writeRecord(&buf, rec, "    "); err != nil {
			return nil, err
		}
		if i < len(reg.Records)-1 {
			buf.WriteString(",\n")
		} else {
			buf.WriteString("\n  ")
		}
	}
	buf.WriteString("]\n")
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

// writeRecord writes a single record with sorted capability keys.
func writeRecord(buf *bytes.Buffer, rec Record, indent string) error {
	buf.WriteString(indent + "{\n")
	inner := indent + "  "
	if err := writeJSONField(buf, inner, "id", rec.ID, false); err != nil {
		return err
	}
	if err := writeJSONField(buf, inner, "category", rec.Category, false); err != nil {
		return err
	}
	// Subcategory is optional (omitempty in the struct tag); preserve
	// the same on-disk behaviour by only emitting when set. Placement
	// between category and language keeps the field grouping intuitive
	// and matches the schema doc comment.
	if rec.Subcategory != "" {
		if err := writeJSONField(buf, inner, "subcategory", rec.Subcategory, false); err != nil {
			return err
		}
	}
	if err := writeJSONField(buf, inner, "language", rec.Language, false); err != nil {
		return err
	}
	if err := writeJSONField(buf, inner, "label", rec.Label, false); err != nil {
		return err
	}
	buf.WriteString(inner + "\"capabilities\": {")
	if rec.IsGrouped() {
		if err := writeGroupedCapabilities(buf, inner+"  ", rec); err != nil {
			return err
		}
	} else {
		keys := sortedCapKeys(rec.Capabilities)
		if len(keys) > 0 {
			buf.WriteString("\n")
		}
		for i, k := range keys {
			cap := rec.Capabilities[k]
			if err := writeCapability(buf, inner+"  ", k, cap); err != nil {
				return err
			}
			if i < len(keys)-1 {
				buf.WriteString(",\n")
			} else {
				buf.WriteString("\n" + inner)
			}
		}
	}
	buf.WriteString("}")
	if len(rec.FrameworkSpecific) > 0 {
		buf.WriteString(",\n")
		buf.WriteString(inner + "\"framework_specific\": {")
		if err := writeFrameworkSpecific(buf, inner+"  ", rec.FrameworkSpecific); err != nil {
			return err
		}
		buf.WriteString("}\n")
	} else {
		buf.WriteString("\n")
	}
	buf.WriteString(indent + "}")
	return nil
}

// writeFrameworkSpecific serialises rec.FrameworkSpecific in the same
// nested shape as grouped capabilities, with group names sorted
// alphabetically (no canonical taxonomy applies — group names are
// free-form) and capability keys sorted alphabetically within each
// group. Mirrors writeGroupedCapabilities for output stability.
func writeFrameworkSpecific(buf *bytes.Buffer, indent string, fs map[string]map[string]Capability) error {
	if len(fs) == 0 {
		return nil
	}
	groups := make([]string, 0, len(fs))
	for g := range fs {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	buf.WriteString("\n")
	for gi, gname := range groups {
		encG, err := json.Marshal(gname)
		if err != nil {
			return err
		}
		buf.WriteString(indent)
		buf.Write(encG)
		buf.WriteString(": {")
		inner := indent + "  "
		caps := fs[gname]
		keys := sortedCapKeys(caps)
		if len(keys) > 0 {
			buf.WriteString("\n")
		}
		for i, k := range keys {
			if err := writeCapability(buf, inner, k, caps[k]); err != nil {
				return err
			}
			if i < len(keys)-1 {
				buf.WriteString(",\n")
			} else {
				buf.WriteString("\n" + indent)
			}
		}
		buf.WriteString("}")
		if gi < len(groups)-1 {
			buf.WriteString(",\n")
		} else {
			buf.WriteString("\n")
			buf.WriteString(indent[:len(indent)-2])
		}
	}
	return nil
}

// writeGroupedCapabilities serialises the nested capability shape as
// "capabilities": { "Group": { "key": {cell}, ... }, ... }. Group names
// are emitted in their canonical taxonomy order (subcategoryGroups),
// then any extras (including the synthetic "Uncategorized" group) in
// alphabetical order. Within each group, capability keys sort
// alphabetically so output is byte-for-byte stable.
func writeGroupedCapabilities(buf *bytes.Buffer, indent string, rec Record) error {
	groups := orderedGroupNames(rec.Subcategory, rec.Groups)
	if len(groups) == 0 {
		return nil
	}
	buf.WriteString("\n")
	for gi, gname := range groups {
		encG, err := json.Marshal(gname)
		if err != nil {
			return err
		}
		buf.WriteString(indent)
		buf.Write(encG)
		buf.WriteString(": {")
		inner := indent + "  "
		caps := rec.Groups[gname]
		keys := sortedCapKeys(caps)
		if len(keys) > 0 {
			buf.WriteString("\n")
		}
		for i, k := range keys {
			if err := writeCapability(buf, inner, k, caps[k]); err != nil {
				return err
			}
			if i < len(keys)-1 {
				buf.WriteString(",\n")
			} else {
				buf.WriteString("\n" + indent)
			}
		}
		buf.WriteString("}")
		if gi < len(groups)-1 {
			buf.WriteString(",\n")
		} else {
			buf.WriteString("\n")
			// Trim indent back to the "capabilities": { level.
			buf.WriteString(indent[:len(indent)-2])
		}
	}
	return nil
}

// orderedGroupNames returns the group names present in groups, sorted
// by canonical order from subcategoryGroups first then alphabetically
// for any extras (e.g. the synthetic "Uncategorized" bucket).
func orderedGroupNames(sub string, groups map[string]map[string]Capability) []string {
	known := knownGroupNames(sub)
	present := map[string]bool{}
	for g := range groups {
		present[g] = true
	}
	out := make([]string, 0, len(present))
	seen := map[string]bool{}
	for _, g := range known {
		if present[g] {
			out = append(out, g)
			seen[g] = true
		}
	}
	extras := make([]string, 0)
	for g := range present {
		if !seen[g] {
			extras = append(extras, g)
		}
	}
	sort.Strings(extras)
	return append(out, extras...)
}

// writeJSONField writes a "key": value pair using encoding/json for
// proper string escaping. last controls trailing comma+newline.
func writeJSONField(buf *bytes.Buffer, indent, key, value string, last bool) error {
	encKey, err := json.Marshal(key)
	if err != nil {
		return err
	}
	encVal, err := json.Marshal(value)
	if err != nil {
		return err
	}
	buf.WriteString(indent)
	buf.Write(encKey)
	buf.WriteString(": ")
	buf.Write(encVal)
	if last {
		buf.WriteString("\n")
	} else {
		buf.WriteString(",\n")
	}
	return nil
}

// writeCapability serialises a capability cell with sorted, omit-empty
// semantics matching the struct json tags.
func writeCapability(buf *bytes.Buffer, indent, key string, cap Capability) error {
	encKey, err := json.Marshal(key)
	if err != nil {
		return err
	}
	buf.WriteString(indent)
	buf.Write(encKey)
	buf.WriteString(": {\n")
	inner := indent + "  "

	type field struct {
		name string
		val  any
		emit bool
	}
	fields := []field{
		{"status", cap.Status, true},
		{"cites", cap.Cites, len(cap.Cites) > 0},
		{"issue", cap.Issue, cap.Issue != ""},
		{"verified_at", cap.VerifiedAt, cap.VerifiedAt != ""},
		{"verified_sha", cap.VerifiedSHA, cap.VerifiedSHA != ""},
	}
	first := true
	for _, f := range fields {
		if !f.emit {
			continue
		}
		if !first {
			buf.WriteString(",\n")
		}
		first = false
		encName, err := json.Marshal(f.name)
		if err != nil {
			return err
		}
		encVal, err := json.Marshal(f.val)
		if err != nil {
			return err
		}
		buf.WriteString(inner)
		buf.Write(encName)
		buf.WriteString(": ")
		buf.Write(encVal)
	}
	buf.WriteString("\n" + indent + "}")
	return nil
}

// sortedCapKeys returns capability map keys in lexical order.
func sortedCapKeys(m map[string]Capability) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
