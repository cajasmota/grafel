package main

// Capability-mapping file: mechanical capability ↔ code traceability.
//
// The mapping file (tools/coverage/capability-map.yaml) cross-references
// each capability declared in docs/coverage/registry.json with the
// implementing symbols (source files + function names) and the issues
// that landed them. The validate subcommand reads this file (when
// present) and verifies the cited files and functions actually exist
// on disk, catching drift before it ships.
//
// See issue #2741 (Phase 1) for the spec. Phases 2-5 (code annotations,
// PR-body tagging, smoke tests, scheduled drift detection) are out of
// scope here and intentionally not implemented.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// defaultCapabilityMapPath is the canonical on-disk location of the
// mapping file, resolved relative to a repository root.
const defaultCapabilityMapPath = "tools/coverage/capability-map.yaml"

// CapabilityMap is the root document of capability-map.yaml.
//
// Records is keyed by registry record ID (e.g. lang.jsts.framework.react).
// Each record's Capabilities map is keyed either by capability slug (flat
// records) or by group name (grouped records introduced by #2737); the
// loader inspects the YAML shape and routes accordingly.
type CapabilityMap struct {
	Records map[string]MapRecord `yaml:"records"`
}

// MapRecord is the per-registry-record mapping entry.
type MapRecord struct {
	// Capabilities holds flat-shape entries (capability slug → entry)
	// for records without nested groups.
	Capabilities map[string]MapEntry
	// Groups holds nested-shape entries (group name → capability slug →
	// entry) for records that mirror #2737's grouped capability shape.
	Groups map[string]map[string]MapEntry
}

// MapEntry is the mapping payload for one capability cell: the symbols
// (files + functions) and tests that implement it, plus provenance.
type MapEntry struct {
	Status            string      `yaml:"status,omitempty"`
	Symbols           []MapSymbol `yaml:"symbols,omitempty"`
	Tests             []MapTest   `yaml:"tests,omitempty"`
	IssuesImplemented []string    `yaml:"issues_implemented,omitempty"`
	VerifiedAt        string      `yaml:"verified_at,omitempty"`
}

// MapSymbol is one source-file citation plus the functions within it
// that implement the capability.
type MapSymbol struct {
	File      string   `yaml:"file"`
	Functions []string `yaml:"functions,omitempty"`
}

// MapTest is one test-file citation backing a capability.
type MapTest struct {
	File string `yaml:"file"`
}

// IsGrouped reports whether the mapping record uses the grouped shape.
func (m MapRecord) IsGrouped() bool { return len(m.Groups) > 0 }

// LoadCapabilityMap reads tools/coverage/capability-map.yaml relative to
// repoRoot and returns the decoded map. Missing files return (nil, nil)
// so callers can treat the mapping as optional. Decode errors surface
// as wrapped errors with the path.
func LoadCapabilityMap(repoRoot string) (*CapabilityMap, error) {
	path := filepath.Join(repoRoot, defaultCapabilityMapPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parseCapabilityMap(data, path)
}

// parseCapabilityMap is the pure-data half of LoadCapabilityMap, split
// out so tests can drive it without a real filesystem.
//
// The YAML shape is ambiguous on its face: a capability key can either
// hold a single MapEntry (flat) or a sub-map of MapEntries (grouped).
// We resolve this by decoding into a generic intermediate and then
// inspecting each capability value's shape. The presence of one of the
// MapEntry leaf keys (status, symbols, tests, issues_implemented,
// verified_at) marks it as a leaf; otherwise it is a group.
func parseCapabilityMap(data []byte, path string) (*CapabilityMap, error) {
	var raw struct {
		Records map[string]struct {
			Capabilities map[string]any `yaml:"capabilities"`
		} `yaml:"records"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := &CapabilityMap{Records: map[string]MapRecord{}}
	for recID, recBody := range raw.Records {
		rec := MapRecord{}
		for capKey, value := range recBody.Capabilities {
			node, err := remarshalYAML(value)
			if err != nil {
				return nil, fmt.Errorf("%s: records[%s].%s: %w", path, recID, capKey, err)
			}
			if isLeafEntry(node) {
				var entry MapEntry
				if err := node.Decode(&entry); err != nil {
					return nil, fmt.Errorf("%s: records[%s].%s: %w", path, recID, capKey, err)
				}
				if rec.Capabilities == nil {
					rec.Capabilities = map[string]MapEntry{}
				}
				rec.Capabilities[capKey] = entry
				continue
			}
			// Grouped: value is a map of capability slug → MapEntry.
			var group map[string]MapEntry
			if err := node.Decode(&group); err != nil {
				return nil, fmt.Errorf("%s: records[%s].%s: %w", path, recID, capKey, err)
			}
			if rec.Groups == nil {
				rec.Groups = map[string]map[string]MapEntry{}
			}
			rec.Groups[capKey] = group
		}
		if len(rec.Capabilities) > 0 && len(rec.Groups) > 0 {
			return nil, fmt.Errorf("%s: records[%s] mixes flat and grouped capability shapes", path, recID)
		}
		out.Records[recID] = rec
	}
	return out, nil
}

// remarshalYAML round-trips a decoded value back through the YAML
// encoder so we can inspect it as a yaml.Node and selectively decode
// it as either a leaf MapEntry or a nested group.
func remarshalYAML(v any) (*yaml.Node, error) {
	buf, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var n yaml.Node
	if err := yaml.Unmarshal(buf, &n); err != nil {
		return nil, err
	}
	// yaml.Unmarshal returns a document node; peel one layer.
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		return n.Content[0], nil
	}
	return &n, nil
}

// leafKeys is the set of YAML keys that identify a MapEntry leaf as
// opposed to a nested group. Any of these keys at the top level of a
// capability value classifies the value as a leaf entry.
var leafKeys = map[string]bool{
	"status":             true,
	"symbols":            true,
	"tests":              true,
	"issues_implemented": true,
	"verified_at":        true,
}

// isLeafEntry returns true when node is a mapping that contains at
// least one leaf-only key. An empty mapping (no keys) is treated as a
// leaf to keep the validation surface tight rather than implicitly
// promoting empties to groups.
func isLeafEntry(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	if len(node.Content) == 0 {
		return true
	}
	for i := 0; i < len(node.Content); i += 2 {
		if leafKeys[node.Content[i].Value] {
			return true
		}
	}
	return false
}

// SortedRecordIDs returns the mapping's record IDs in stable sorted
// order. Use in place of `range cm.Records` whenever output must be
// deterministic (tool stdout, fixture comparisons, summary lines).
func (cm *CapabilityMap) SortedRecordIDs() []string {
	ids := make([]string, 0, len(cm.Records))
	for id := range cm.Records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// SortedFlatKeys returns the flat capability slugs for rec in sorted
// order. Returns an empty slice for grouped records.
func (m MapRecord) SortedFlatKeys() []string {
	keys := make([]string, 0, len(m.Capabilities))
	for k := range m.Capabilities {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SortedGroupNames returns the group names for rec in sorted order.
// Returns an empty slice for flat records.
func (m MapRecord) SortedGroupNames() []string {
	names := make([]string, 0, len(m.Groups))
	for g := range m.Groups {
		names = append(names, g)
	}
	sort.Strings(names)
	return names
}

// SortedKeysInGroup returns the capability slugs for the named group in
// sorted order. Returns an empty slice if the group does not exist.
func (m MapRecord) SortedKeysInGroup(group string) []string {
	g := m.Groups[group]
	keys := make([]string, 0, len(g))
	for k := range g {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Lookup returns the MapEntry for a (group, capability) coordinate. For
// flat records pass group="". Returns the zero value and ok=false when
// the entry does not exist.
func (m MapRecord) Lookup(group, capability string) (MapEntry, bool) {
	if group == "" {
		entry, ok := m.Capabilities[capability]
		return entry, ok
	}
	g, ok := m.Groups[group]
	if !ok {
		return MapEntry{}, false
	}
	entry, ok := g[capability]
	return entry, ok
}
