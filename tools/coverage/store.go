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
// marshal time.
func sortRegistry(reg *Registry) {
	sort.Slice(reg.Records, func(i, j int) bool {
		return reg.Records[i].ID < reg.Records[j].ID
	})
	for i := range reg.Records {
		for k, cap := range reg.Records[i].Capabilities {
			sort.Strings(cap.Cites)
			reg.Records[i].Capabilities[k] = cap
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
	if err := writeJSONField(buf, inner, "language", rec.Language, false); err != nil {
		return err
	}
	if err := writeJSONField(buf, inner, "label", rec.Label, false); err != nil {
		return err
	}
	buf.WriteString(inner + "\"capabilities\": {")
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
	buf.WriteString("}\n")
	buf.WriteString(indent + "}")
	return nil
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
