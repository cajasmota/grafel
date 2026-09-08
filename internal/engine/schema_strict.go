package engine

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// This file makes the EXECUTABLE half of the rule schema strict (#7014).
//
// The rule schema has two halves. The metadata half — `frameworks:`,
// `category`, `github_stars_2025`, the fourteen different spellings of the
// top-level container — is 95% unmodelled: 465 of 488 distinct key paths in the
// rule tree are dropped by yaml.Unmarshal without a word. The executable half —
// the entries of `source_patterns`, `relationship_rules` and `file_conventions`
// — is by contrast exactly modelled: the audit on #7014 found ZERO unmodelled
// keys inside those three list containers across all 520 loader-scoped files.
//
// That asymmetry is what makes this guard free. A typo in an executable
// position (`name_group` misspelled, `source_group` written where
// `target_group` was meant) is today indistinguishable from a key the loader
// simply does not model: yaml.Unmarshal drops both in silence. That is the
// defect class that produced #7014 in the first place —
// `detection_signals.package_json_deps` sat unread in 59 files for the entire
// life of the tree because nothing objects to a key nobody models.
//
// So: unknown keys INSIDE the three list containers are a load error. Unknown
// keys ANYWHERE ELSE keep loading exactly as before.
//
// The boundary is deliberate, not an unfinished job. Full strict
// `KnownFields(true)` unmarshalling turns 515 of 520 rule files red; a
// top-level-key allowlist turns 254 red. Both are migrations with their own
// decision to make (see #7013). Scoped to the three containers the cost today
// is ZERO files. Widening it is a migration, not a tidy-up — see
// Test7014_UnknownKeyOutsideExecutableContainersStillLoads, which exists to
// make that widening fail loudly rather than land by accident.
//
// Key matching is exact and case-sensitive, which is marginally stricter than
// yaml.v3's own field matching. No rule file in the tree relies on the
// difference (measured: 0 red).

// strictContainerFields returns the set of YAML keys a struct models, derived
// from its `yaml:"..."` tags. Deriving it by reflection rather than hand-listing
// the keys means a NEW field added to SourcePattern et al. is accepted the
// moment it is declared — the guard cannot drift out of sync with the struct it
// is guarding.
func strictContainerFields(t reflect.Type) map[string]bool {
	fields := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		name := strings.Split(f.Tag.Get("yaml"), ",")[0]
		switch name {
		case "-":
			continue
		case "":
			name = strings.ToLower(f.Name)
		}
		fields[name] = true
	}
	return fields
}

// rejectUnknownContainerKeys fails if the mapping node carries any key the
// struct type does not model. container names the list container for the error
// message ("source_patterns", …).
//
// A non-mapping node is passed through untouched: that is a plain type error
// and yaml.v3 reports it better than this function could.
func rejectUnknownContainerKeys(value *yaml.Node, t reflect.Type, container string) error {
	if value.Kind != yaml.MappingNode {
		return nil
	}
	allowed := strictContainerFields(t)
	var unknown []string
	line := 0
	for i := 0; i+1 < len(value.Content); i += 2 {
		key := value.Content[i].Value
		if allowed[key] {
			continue
		}
		unknown = append(unknown, key)
		if line == 0 {
			line = value.Content[i].Line
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	known := make([]string, 0, len(allowed))
	for k := range allowed {
		known = append(known, k)
	}
	sort.Strings(known)
	return fmt.Errorf(
		"line %d: unknown key %s in %s[]: the engine does not model it, so it would be silently ignored (known keys: %s)",
		line, strings.Join(quoteAll(unknown), ", "), container, strings.Join(known, ", "))
}

func quoteAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

// The three aliases below exist only to break the recursion: decoding into a
// type that does NOT have an UnmarshalYAML method is what lets each
// UnmarshalYAML delegate the real work back to yaml.v3.

type fileConventionFields FileConvention

// UnmarshalYAML rejects unknown keys inside a `file_conventions:` entry (#7014).
func (c *FileConvention) UnmarshalYAML(value *yaml.Node) error {
	if err := rejectUnknownContainerKeys(value, reflect.TypeOf(FileConvention{}), "file_conventions"); err != nil {
		return err
	}
	var raw fileConventionFields
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*c = FileConvention(raw)
	return nil
}

type sourcePatternFields SourcePattern

// UnmarshalYAML rejects unknown keys inside a `source_patterns:` entry (#7014).
func (p *SourcePattern) UnmarshalYAML(value *yaml.Node) error {
	if err := rejectUnknownContainerKeys(value, reflect.TypeOf(SourcePattern{}), "source_patterns"); err != nil {
		return err
	}
	var raw sourcePatternFields
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*p = SourcePattern(raw)
	return nil
}

type relationshipRuleFields RelationshipRule

// UnmarshalYAML rejects unknown keys inside a `relationship_rules:` entry (#7014).
func (r *RelationshipRule) UnmarshalYAML(value *yaml.Node) error {
	if err := rejectUnknownContainerKeys(value, reflect.TypeOf(RelationshipRule{}), "relationship_rules"); err != nil {
		return err
	}
	var raw relationshipRuleFields
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*r = RelationshipRule(raw)
	return nil
}
