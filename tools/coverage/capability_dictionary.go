// Capability dictionary loader (#2752).
//
// The dictionary is the single source of truth for the coverage tool's
// taxonomy: buckets, registry categories (and their flat capability
// allow-lists), subcategories (and their per-subcategory capabilities
// + ordered group taxonomies). It replaces the previously-hardcoded
// maps in schema.go and buckets.go.
//
// The default dictionary ships embedded in the binary so the tool stays
// standalone. LoadCapabilityDictionary reads any path off disk for
// tests or alternate deployments.
package main

import (
	"embed"
	"fmt"
	"os"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

// defaultDictionaryPath is the on-disk location of the canonical
// dictionary file relative to the repository root.
const defaultDictionaryPath = "tools/coverage/capability-dictionary.yaml"

//go:embed capability-dictionary.yaml
var embeddedDictionary embed.FS

// CapabilityDictionary is the in-memory representation of the YAML
// dictionary. All consumers in this package go through the singleton
// returned by dict(); LoadCapabilityDictionary is exported for tests
// and for callers that want a fresh load from a specific path.
type CapabilityDictionary struct {
	SchemaVersion int                         `yaml:"$schema_version"`
	Buckets       map[string]BucketEntry      `yaml:"buckets"`
	Categories    map[string]CategoryEntry    `yaml:"categories"`
	Subcategories map[string]SubcategoryEntry `yaml:"subcategories"`

	// Derived state, populated by indexDerived after Unmarshal.
	bucketOrder      []string                       // sorted by BucketEntry.Order
	bucketOfCategory map[string]string              // category → bucket
	subcatsByCat     map[string][]string            // category → ordered subcategory slugs
	groupsBySubcat   map[string][]capabilityGroup   // subcategory → ordered groups
	groupIndex       map[string]map[string][]string // subcategory → group name → keys
}

// BucketEntry is one bucket definition (render order + category list).
type BucketEntry struct {
	Order      int      `yaml:"order"`
	Categories []string `yaml:"categories"`
}

// CategoryEntry is one registry category definition (flat capability
// allow-list and, when applicable, the ordered subcategory render
// sequence).
type CategoryEntry struct {
	Capabilities     []string `yaml:"capabilities"`
	SubcategoryOrder []string `yaml:"subcategory_order"`
}

// SubcategoryEntry is one subcategory definition. Capabilities is the
// full allow-list of keys (used by validation); Groups is the optional
// ordered group taxonomy (used by rendering).
type SubcategoryEntry struct {
	Display        string                `yaml:"display"`
	ParentCategory string                `yaml:"parent_category"`
	Capabilities   []string              `yaml:"capabilities"`
	Groups         []SubcategoryGroupDef `yaml:"groups"`
}

// SubcategoryGroupDef is one group bucket inside a subcategory.
type SubcategoryGroupDef struct {
	Name string   `yaml:"name"`
	Keys []string `yaml:"keys"`
}

// LoadCapabilityDictionary reads and parses the dictionary at path. Use
// dict() in normal code paths; this entry point exists for tests and
// alternate deployments.
func LoadCapabilityDictionary(path string) (*CapabilityDictionary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read capability dictionary %s: %w", path, err)
	}
	return parseCapabilityDictionary(data, path)
}

// loadEmbeddedDictionary returns the dictionary baked into the binary.
// Used as the fallback when the on-disk file is unavailable so the tool
// still runs from any working directory.
func loadEmbeddedDictionary() (*CapabilityDictionary, error) {
	data, err := embeddedDictionary.ReadFile("capability-dictionary.yaml")
	if err != nil {
		return nil, fmt.Errorf("read embedded capability dictionary: %w", err)
	}
	return parseCapabilityDictionary(data, "capability-dictionary.yaml")
}

// parseCapabilityDictionary decodes raw YAML bytes and validates that
// the schema version matches what this binary understands. Derived
// indexes are populated before return so callers can rely on every
// helper returning consistent results.
func parseCapabilityDictionary(data []byte, path string) (*CapabilityDictionary, error) {
	d := &CapabilityDictionary{}
	if err := yaml.Unmarshal(data, d); err != nil {
		return nil, fmt.Errorf("parse capability dictionary %s: %w", path, err)
	}
	if d.SchemaVersion != 1 {
		return nil, fmt.Errorf("capability dictionary %s: unsupported $schema_version %d (want 1)", path, d.SchemaVersion)
	}
	d.indexDerived()
	return d, nil
}

// indexDerived populates the read-only derived indexes used by helpers.
func (d *CapabilityDictionary) indexDerived() {
	// bucketOrder by ascending Order then name for ties.
	names := make([]string, 0, len(d.Buckets))
	for n := range d.Buckets {
		names = append(names, n)
	}
	sort.SliceStable(names, func(i, j int) bool {
		oi, oj := d.Buckets[names[i]].Order, d.Buckets[names[j]].Order
		if oi != oj {
			return oi < oj
		}
		return names[i] < names[j]
	})
	d.bucketOrder = names

	d.bucketOfCategory = map[string]string{}
	for bname, b := range d.Buckets {
		for _, cat := range b.Categories {
			d.bucketOfCategory[cat] = bname
		}
	}

	d.subcatsByCat = map[string][]string{}
	for cat, ce := range d.Categories {
		if len(ce.SubcategoryOrder) > 0 {
			out := make([]string, len(ce.SubcategoryOrder))
			copy(out, ce.SubcategoryOrder)
			d.subcatsByCat[cat] = out
		}
	}
	// Fill in any subcategories that declare a parent_category but
	// aren't listed in the category's subcategory_order (append in
	// sorted order so determinism holds).
	parentToSubs := map[string][]string{}
	for slug, sc := range d.Subcategories {
		parentToSubs[sc.ParentCategory] = append(parentToSubs[sc.ParentCategory], slug)
	}
	for cat, subs := range parentToSubs {
		sort.Strings(subs)
		known := map[string]bool{}
		for _, s := range d.subcatsByCat[cat] {
			known[s] = true
		}
		for _, s := range subs {
			if !known[s] {
				d.subcatsByCat[cat] = append(d.subcatsByCat[cat], s)
				known[s] = true
			}
		}
	}

	d.groupsBySubcat = map[string][]capabilityGroup{}
	d.groupIndex = map[string]map[string][]string{}
	for slug, sc := range d.Subcategories {
		if len(sc.Groups) == 0 {
			continue
		}
		groups := make([]capabilityGroup, len(sc.Groups))
		inner := map[string][]string{}
		for i, g := range sc.Groups {
			keys := make([]string, len(g.Keys))
			copy(keys, g.Keys)
			groups[i] = capabilityGroup{Name: g.Name, Keys: keys}
			inner[g.Name] = keys
		}
		d.groupsBySubcat[slug] = groups
		d.groupIndex[slug] = inner
	}
}

// BucketOrder returns the render order of bucket names.
func (d *CapabilityDictionary) BucketOrder() []string {
	out := make([]string, len(d.bucketOrder))
	copy(out, d.bucketOrder)
	return out
}

// BucketForCategory returns the bucket that owns cat, falling back to
// BucketOther for unmapped categories (matching the documented "Other:
// everything else" rule so new categories ship as Other until they are
// explicitly classified).
func (d *CapabilityDictionary) BucketForCategory(cat string) string {
	if b, ok := d.bucketOfCategory[cat]; ok {
		return b
	}
	return BucketOther
}

// CategoryCapabilities returns the flat capability allow-list for cat,
// or nil when the category is unknown.
func (d *CapabilityDictionary) CategoryCapabilities(cat string) []string {
	ce, ok := d.Categories[cat]
	if !ok {
		return nil
	}
	out := make([]string, len(ce.Capabilities))
	copy(out, ce.Capabilities)
	return out
}

// KnownCategories returns sorted category slugs.
func (d *CapabilityDictionary) KnownCategories() []string {
	out := make([]string, 0, len(d.Categories))
	for k := range d.Categories {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SubcategoryDisplay returns the human-facing heading for sub, or ""
// when the subcategory has no entry (caller falls back to prettyKey).
func (d *CapabilityDictionary) SubcategoryDisplay(sub string) string {
	if sc, ok := d.Subcategories[sub]; ok {
		return sc.Display
	}
	return ""
}

// SubcategoriesByCategory returns the canonical render order of
// subcategory slugs for cat: the explicit subcategory_order list,
// followed by any extra subcategories whose parent_category matches
// (alphabetical).
func (d *CapabilityDictionary) SubcategoriesByCategory(cat string) []string {
	in, ok := d.subcatsByCat[cat]
	if !ok {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// HasSubcategory reports whether sub is declared for cat.
func (d *CapabilityDictionary) HasSubcategory(cat, sub string) bool {
	for _, s := range d.subcatsByCat[cat] {
		if s == sub {
			return true
		}
	}
	return false
}

// SubcategoryCapabilities returns the allow-list of keys for sub, or
// nil when the subcategory is unknown.
func (d *CapabilityDictionary) SubcategoryCapabilities(sub string) []string {
	sc, ok := d.Subcategories[sub]
	if !ok {
		return nil
	}
	out := make([]string, len(sc.Capabilities))
	copy(out, sc.Capabilities)
	return out
}

// GroupsForSubcategory returns the ordered group taxonomy for sub, or
// nil when none is declared.
func (d *CapabilityDictionary) GroupsForSubcategory(sub string) []capabilityGroup {
	in, ok := d.groupsBySubcat[sub]
	if !ok {
		return nil
	}
	out := make([]capabilityGroup, len(in))
	for i, g := range in {
		keys := make([]string, len(g.Keys))
		copy(keys, g.Keys)
		out[i] = capabilityGroup{Name: g.Name, Keys: keys}
	}
	return out
}

// GroupNames returns the declared group names for sub in canonical
// render order, or nil when no group taxonomy exists.
func (d *CapabilityDictionary) GroupNames(sub string) []string {
	groups := d.groupsBySubcat[sub]
	if len(groups) == 0 {
		return nil
	}
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.Name
	}
	return out
}

// GroupKeys returns the declared key list for (sub, group) or nil when
// the group is not part of sub's taxonomy.
func (d *CapabilityDictionary) GroupKeys(sub, group string) []string {
	g, ok := d.groupIndex[sub]
	if !ok {
		return nil
	}
	keys, ok := g[group]
	if !ok {
		return nil
	}
	out := make([]string, len(keys))
	copy(out, keys)
	return out
}

// GroupForCapability returns the group name in sub's taxonomy that owns
// key, or "" when key is not declared in any group.
func (d *CapabilityDictionary) GroupForCapability(sub, key string) string {
	for _, g := range d.groupsBySubcat[sub] {
		for _, k := range g.Keys {
			if k == key {
				return g.Name
			}
		}
	}
	return ""
}

// HasGroup reports whether group is part of sub's taxonomy.
func (d *CapabilityDictionary) HasGroup(sub, group string) bool {
	if _, ok := d.groupIndex[sub]; !ok {
		return false
	}
	_, ok := d.groupIndex[sub][group]
	return ok
}

// dictionarySingleton caches the loaded dictionary for the lifetime of
// the process. Loaded lazily from disk (when present) or the embedded
// fallback so callers can fire-and-forget.
var (
	dictionarySingleton *CapabilityDictionary
	dictionaryOnce      sync.Once
	dictionaryErr       error
)

// dict returns the process-wide dictionary singleton. On first call it
// tries the on-disk file at defaultDictionaryPath and falls back to the
// embedded copy. Subsequent calls return the cached value. Errors are
// fatal — the tool cannot operate without a taxonomy.
func dict() *CapabilityDictionary {
	dictionaryOnce.Do(func() {
		if data, err := os.ReadFile(defaultDictionaryPath); err == nil {
			d, perr := parseCapabilityDictionary(data, defaultDictionaryPath)
			if perr == nil {
				dictionarySingleton = d
				return
			}
			dictionaryErr = perr
			return
		}
		d, err := loadEmbeddedDictionary()
		if err != nil {
			dictionaryErr = err
			return
		}
		dictionarySingleton = d
	})
	if dictionaryErr != nil {
		panic(fmt.Sprintf("load capability dictionary: %v", dictionaryErr))
	}
	return dictionarySingleton
}
