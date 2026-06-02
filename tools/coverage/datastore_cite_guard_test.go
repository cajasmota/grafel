package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// datastoreCiteGuardCapabilities is the set of capability keys whose
// "full"/"partial" claim asserts that the engine extracts real query /
// model topology from source — i.e. a key/collection/table/index/label
// out of a datastore *driver* call site. A cell in this class CANNOT be
// legitimately backed by a detection-only rule YAML (one whose
// `source_patterns` and `relationship_rules` are both empty), because
// such a YAML only proves the driver is *present*; it extracts zero
// topology. The dead `custom_extractors` Python block these YAMLs carry
// is never executed by the Go engine (internal/engine/schema.go), so it
// is not evidence either.
var datastoreCiteGuardCapabilities = map[string]struct{}{
	"query_attribution": {},
	"model_extraction":  {},
}

// datastoreStoreAliases maps a driver record's trailing store token (the
// last dot-segment of `lang.<lang>.driver.<store>`) to the identifier
// substrings a native Go extractor would mention. Used by the "is there a
// real Go extractor?" escape hatch so the guard fires ONLY on the genuine
// false class (no backing of any kind), not on the separate "under-cited"
// class where a real Go extractor exists but the cell cites the wrong file.
var datastoreStoreAliases = map[string][]string{
	"redis":     {"redis"},
	"redix":     {"redis"},
	"mongodb":   {"mongo"},
	"mongo":     {"mongo"},
	"cassandra": {"cassandra"},
	"xandra":    {"cassandra", "xandra"},
	"dynamodb":  {"dynamo"},
	"elastic":   {"elastic"},
	"neo4j":     {"neo4j", "cypher"},
	"mysql":     {"mysql"},
	"myxql":     {"mysql"},
	"postgres":  {"postgres", "pgx"},
	"postgrex":  {"postgres", "pgx"},
	"npgsql":    {"npgsql", "postgres"},
	"sqlite":    {"sqlite"},
}

// yamlRuleExtractsTopology reports whether the cited YAML rule file at the
// repo-relative path performs any live extraction the Go engine honours: a
// non-empty `source_patterns` OR a non-empty `relationship_rules` (both are
// executed by the declarative rule loader). A missing file or a
// detection-only YAML (both lists empty/absent) returns false.
func yamlRuleExtractsTopology(t *testing.T, repoRoot, rel string) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		// A missing cite is never positive evidence; validate.go reports
		// the cite-exists failure separately.
		return false
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parse rule YAML %s: %v", rel, err)
	}
	return nodeHasNonEmptyList(&root, "source_patterns") ||
		nodeHasNonEmptyList(&root, "relationship_rules")
}

// nodeHasNonEmptyList walks a YAML node tree and reports whether any
// mapping carries the named key bound to a non-empty sequence. This is
// resilient to the wrapper nesting the rule files use (e.g. the keys may
// live at the top level or under an `orms_database:` wrapper).
func nodeHasNonEmptyList(n *yaml.Node, key string) bool {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			if nodeHasNonEmptyList(c, key) {
				return true
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == key && v.Kind == yaml.SequenceNode && len(v.Content) > 0 {
				return true
			}
			if nodeHasNonEmptyList(v, key) {
				return true
			}
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if nodeHasNonEmptyList(c, key) {
				return true
			}
		}
	}
	return false
}

// citeIsValidTopologyBacking reports whether a single cite is acceptable
// backing for a datastore topology cell: a Go extractor source file, or a
// rule YAML that actually extracts (non-empty source_patterns /
// relationship_rules). Anything else (a detection-only YAML, a doc, a dead
// module ref) is not.
func citeIsValidTopologyBacking(t *testing.T, repoRoot, cite string) bool {
	t.Helper()
	if strings.HasSuffix(cite, ".go") {
		return true
	}
	if strings.HasSuffix(cite, ".yaml") || strings.HasSuffix(cite, ".yml") {
		return yamlRuleExtractsTopology(t, repoRoot, cite)
	}
	return false
}

// nativeGoExtractorExists reports whether a per-language native Go driver
// extractor for the store exists under internal/custom/<lang>/ — the
// "under-cited, not false" escape hatch (audit rule #3625): a topology cell
// whose cite is merely wrong but whose extractor genuinely exists is NOT
// the false class this guard polices (that is tracked by the re-cite ticket
// #3637). The guard only fails when NO per-language extractor exists.
//
// The scan is deliberately confined to the LANGUAGE-SPECIFIC custom dir. The
// shared internal/engine/ tree carries cross-language *synthesis* passes
// (e.g. redis_pubsub_edges.go, mongo agg) that mention every store but do
// NOT constitute per-driver query-topology extraction — including it would
// excuse the very false cells this guard exists to catch.
func nativeGoExtractorExists(t *testing.T, repoRoot, lang, store string) bool {
	t.Helper()
	tokens := datastoreStoreAliases[store]
	if len(tokens) == 0 {
		tokens = []string{store}
	}
	dir := filepath.Join(repoRoot, "internal", "custom", lang)
	return mentionsTokenInGoSources(t, dir, tokens)
}

// mentionsTokenInGoSources scans non-test .go files directly under dir for
// any of the (lower-cased) tokens. Shallow by design: native driver
// extractors live one level under internal/custom/<lang>/.
func mentionsTokenInGoSources(t *testing.T, dir string, tokens []string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		lower := strings.ToLower(string(raw))
		for _, tok := range tokens {
			if strings.Contains(lower, strings.ToLower(tok)) {
				return true
			}
		}
	}
	return false
}

// TestDatastoreCiteValidityGuard is the permanent guard against the
// false-datastore-coverage class found by the #3625 datastore audit and
// remediated in #3635. For every datastore *driver* record
// (`lang.<lang>.driver.<store>`), every full/partial query_attribution /
// model_extraction cell MUST either:
//
//   - cite at least one .go file OR a rule YAML whose
//     source_patterns/relationship_rules is non-empty (valid backing), OR
//   - have a native Go extractor that mentions the datastore (the
//     "under-cited not false" escape hatch — that re-cite is #3637's job).
//
// A driver topology cell stamped full/partial with NEITHER — backed solely
// by a detection-only YAML and with no extractor anywhere — is a false
// claim and fails this test, listing every offender. Before the #3635
// downgrades this test failed on the audited false cells; after them it
// passes. It complements the dispatch-parity guard.
func TestDatastoreCiteValidityGuard(t *testing.T) {
	root := repoRoot(t)
	reg, err := loadRegistry(filepath.Join(root, defaultRegistryPath))
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}

	var offenders []string
	for _, rec := range reg.Records {
		lang, store, ok := parseDriverRecordID(rec.ID)
		if !ok {
			continue
		}
		visit := func(group string, caps map[string]Capability) {
			for capKey, cell := range caps {
				if _, watched := datastoreCiteGuardCapabilities[capKey]; !watched {
					continue
				}
				if cell.Status != StatusFull && cell.Status != StatusPartial {
					continue
				}
				backed := false
				for _, cite := range cell.Cites {
					if citeIsValidTopologyBacking(t, root, cite) {
						backed = true
						break
					}
				}
				if !backed && nativeGoExtractorExists(t, root, lang, store) {
					backed = true // under-cited, not false — see #3637
				}
				if !backed {
					loc := rec.ID + "." + capKey
					if group != "" {
						loc = rec.ID + ".[" + group + "]." + capKey
					}
					offenders = append(offenders, loc+" cites="+strings.Join(cell.Cites, ","))
				}
			}
		}
		if rec.IsGrouped() {
			for g, caps := range rec.Groups {
				visit(g, caps)
			}
		} else {
			visit("", rec.Capabilities)
		}
		for g, caps := range rec.FrameworkSpecific {
			visit(g, caps)
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("found %d datastore driver topology cell(s) stamped full/partial with no valid backing "+
			"(must cite a .go file OR a rule YAML with non-empty source_patterns/relationship_rules, "+
			"or have a native Go extractor for the store); a detection-only YAML / dead module ref is "+
			"not evidence:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// parseDriverRecordID extracts (lang, store) from a driver record id of the
// shape `lang.<lang>.driver.<store>`. Returns ok=false for any other id.
func parseDriverRecordID(id string) (lang, store string, ok bool) {
	parts := strings.Split(id, ".")
	if len(parts) != 4 || parts[0] != "lang" || parts[2] != "driver" {
		return "", "", false
	}
	return parts[1], parts[3], true
}

// TestDatastoreCiteValidityGuard_DetectsDetectionOnlyYAML proves the guard's
// cite-validity helper actually catches the false-coverage class: a cell
// citing a detection-only rule YAML (empty source_patterns/relationship_rules)
// is rejected, while the same cell citing a Go file or an extracting YAML is
// accepted. This keeps the guard honest if its helpers are ever refactored.
func TestDatastoreCiteValidityGuard_DetectsDetectionOnlyYAML(t *testing.T) {
	root := t.TempDir()

	// A detection-only rule YAML: driver present, but zero topology.
	detectionOnly := "internal/engine/rules/fixture/detection_only.yaml"
	writeRuleFixture(t, root, detectionOnly, ""+
		"orms_database:\n"+
		"  name: Fixture Driver\n"+
		"  detection:\n"+
		"    import_patterns:\n"+
		"    - import fixture\n"+
		"source_patterns: []\n"+
		"relationship_rules: []\n")

	// An extracting rule YAML: non-empty source_patterns.
	extracting := "internal/engine/rules/fixture/extracting.yaml"
	writeRuleFixture(t, root, extracting, ""+
		"orms_database:\n"+
		"  name: Fixture Driver\n"+
		"source_patterns:\n"+
		"- pattern: \"FROM (\\\\w+)\"\n"+
		"  emits: table\n"+
		"relationship_rules: []\n")

	goCite := "internal/custom/fixture/extractor.go"
	writeRuleFixture(t, root, goCite, "package fixture\n")

	if citeIsValidTopologyBacking(t, root, detectionOnly) {
		t.Error("detection-only YAML (empty source_patterns/relationship_rules) wrongly accepted as backing")
	}
	if !citeIsValidTopologyBacking(t, root, extracting) {
		t.Error("extracting YAML (non-empty source_patterns) wrongly rejected as backing")
	}
	if !citeIsValidTopologyBacking(t, root, goCite) {
		t.Error(".go cite wrongly rejected as backing")
	}
	if citeIsValidTopologyBacking(t, root, "internal/engine/rules/fixture/missing.yaml") {
		t.Error("missing YAML wrongly accepted as backing")
	}
}

// TestDatastoreCiteValidityGuard_EscapeHatchForUnderCited proves the
// "under-cited not false" escape hatch: when a native Go extractor for the
// store exists, the guard must NOT flag the cell (re-citing it is #3637's
// job), but it MUST flag a store with no extractor at all.
func TestDatastoreCiteValidityGuard_EscapeHatchForUnderCited(t *testing.T) {
	root := t.TempDir()
	writeRuleFixture(t, root, "internal/custom/python/redis.go",
		"package python\n\n// RedisExtractor extracts redis ops.\ntype RedisExtractor struct{}\n")

	if !nativeGoExtractorExists(t, root, "python", "redis") {
		t.Error("under-cited python/redis cell wrongly flagged: a native redis.go extractor exists")
	}
	if nativeGoExtractorExists(t, root, "rust", "cassandra") {
		t.Error("false rust/cassandra cell wrongly excused: no native extractor exists for it")
	}
}

// writeRuleFixture materialises a fixture file under root for the guard
// self-tests.
func writeRuleFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}
