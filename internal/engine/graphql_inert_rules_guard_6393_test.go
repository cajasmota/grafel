package engine

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"
)

// This file is the guard for #6393.
//
// The loader accepts <lang>/<subdir>/<file>.yaml for subdir in
// {frameworks, orms, queues} and unmarshals it into a FrameworkRule. YAML
// unmarshalling ignores unknown top-level keys, so a file keyed on anything
// OTHER than the four mapped keys (frameworks, file_conventions,
// source_patterns, relationship_rules) parses *successfully* into the zero
// FrameworkRule: no name, no detection markers, no patterns, no relationships.
// It is loaded, it is counted by the #6341 inventory assertion, and it drives
// nothing.
//
// That is worse than a parse failure. A parse failure is recorded in
// RuleLoadReport.Failures and named by TestRuleInventory_*. A zero-value rule
// is indistinguishable from a healthy one, so a contributor (or an agent) who
// "configures" behaviour by editing such a file gets silence, not an error.
// #6393 reports exactly that happening: a reader spent an hour treating
// `detection.package_json_dependencies` in graphql/frameworks/*.yaml as live
// detection config. It never was.
//
// Under internal/engine/rules/graphql/frameworks/ this was not merely unwired,
// it was unwirable. A rule under rules/<lang>/ is only ever applied to files
// whose detected language is <lang>, but those files described TypeScript
// (apollo_server, pothos, type-graphql, yoga, urql, relay), Go (gqlgen),
// Python (strawberry) and Elixir (absinthe) frameworks. Even a ServerFrameworks
// field on FrameworkRule would never have fired for them, because a .ts file is
// not a .graphql file. The extraction those files describe is implemented in Go
// (internal/extractors/graphql, internal/engine/http_endpoint_synthesis*.go)
// and is green; the YAML was a description of it, shelved in the one directory
// where it reads as configuration.
//
// So the 14 of them were deleted, and this guard keeps the graphql tree at
// zero: any rule file that reappears under rules/graphql/{frameworks,orms,
// queues}/ carrying a key the engine does not map fails here, by name.
//
// Scope, stated plainly rather than implied: this guard covers rules/graphql
// only, and graphql is NOT the whole population. Measured by this same checker
// with the language filter removed, 249 further rule files across 24 other
// language directories parse to the zero FrameworkRule — keyed on `orms:`,
// `database_libraries:`, `template_engines:`, `frameworks_and_dialects:` and a
// dozen other names the schema does not map. #6393 was filed as 11 files and
// regrounded as 14; both counts are right about graphql and wrong about the
// repo.
//
// Those 249 are deliberately NOT ledgered here. A ledger of 249 blessed files
// reads as approval, and this guard is not a place to record that the rest of
// the tree has the same hole. Widening it is its own issue; see the report on
// #6393. What this guard does promise is narrow and true: graphql is at zero
// and stays there.

// inertRuleFiles returns, in walk order, the loadable rule files under
// rootDir that unmarshal into the zero FrameworkRule — files the engine loads
// and cannot act on. It also returns the number of loadable rule files it
// examined, so a caller can tell "nothing inert" from "nothing looked at".
//
// lang restricts the scan to one language directory; "" scans the whole tree.
//
// It is a free function so the checker itself can be exercised against a
// synthetic tree; a checker that always answers "all fine" would otherwise be
// invisible.
func inertRuleFiles(fsys fs.FS, rootDir, lang string) (inert []string, examined int, err error) {
	walkErr := fs.WalkDir(fsys, rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
			return nil
		}
		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		// Mirror the loader: only <lang>/<subdir>/<file>.yaml is loaded, and
		// only for a supported subdir. Anything else is documentation the
		// loader never sees, and is not this guard's business.
		if len(parts) != 3 || !ruleSubdirs[parts[1]] {
			return nil
		}
		if lang != "" && parts[0] != lang {
			return nil
		}
		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			return nil
		}
		var rule FrameworkRule
		if yaml.Unmarshal(data, &rule) != nil {
			// A file that fails to parse is a different defect, already caught
			// by the #6341 inventory assertion. Not inert — absent.
			return nil
		}
		examined++
		if isZeroRule(rule) {
			inert = append(inert, filepath.ToSlash(path))
		}
		return nil
	})
	return inert, examined, walkErr
}

// isZeroRule reports whether a parsed rule carries nothing the engine can act
// on. Every field FrameworkRule maps is checked; a rule with a framework name
// but no patterns is still a rule (it can gate requires_framework matches), so
// "zero" means literally every mapped field is empty.
func isZeroRule(r FrameworkRule) bool {
	return r.Frameworks.Name == "" &&
		len(r.Frameworks.Detection.ImportMarkers) == 0 &&
		len(r.FileConventions) == 0 &&
		len(r.SourcePatterns) == 0 &&
		len(r.RelationshipRules) == 0
}

// TestGraphQLRules_NoInertRuleFilesRemain is the assertion over the real
// embedded tree: rules/graphql must contain no loadable rule file that parses
// to a zero-value rule.
func TestGraphQLRules_NoInertRuleFilesRemain(t *testing.T) {
	inert, examined, err := inertRuleFiles(rulesFS, "rules", "graphql")
	if err != nil {
		t.Fatalf("walking the embedded graphql rules: %v", err)
	}

	// Non-vacuity. rules/graphql/frameworks/graphql_schema.yaml is a real rule
	// file and must stay loadable; if the walk stops finding anything, "no
	// inert files" is a statement about an empty set, not about the tree.
	if examined < 1 {
		t.Fatalf("examined %d loadable rule files under rules/graphql, want >= 1: "+
			"the guard is looking at nothing, so its verdict is meaningless", examined)
	}

	if len(inert) > 0 {
		t.Errorf("%d rule file(s) under rules/graphql load into the engine as zero-value "+
			"rules — they look like configuration and drive nothing (#6393):\n  %s\n"+
			"Either give them the mapped schema (frameworks / file_conventions / "+
			"source_patterns / relationship_rules) or move them out of the loaded "+
			"subdirectories. Do not leave them here parsing to nothing.",
			len(inert), strings.Join(inert, "\n  "))
	}
}

// TestInertRuleFiles_DetectsAnInertFile proves the checker fires. Without it,
// an inertRuleFiles that returned nil unconditionally would make the guard
// above pass forever.
func TestInertRuleFiles_DetectsAnInertFile(t *testing.T) {
	fsys := fstest.MapFS{
		// The real shape of the deleted files: a plausible top-level key the
		// engine does not map, full of plausible-looking detection config.
		"rules/graphql/frameworks/apollo_server.yaml": {Data: []byte(
			"server_frameworks:\n  name: Apollo Server\n  detection:\n    package_json_dependencies:\n    - \"@apollo/server\"\n")},
		"rules/graphql/frameworks/urql.yaml": {Data: []byte(
			"client_tools:\n  name: urql\n  detection:\n    package_json_dependencies:\n    - urql\n")},
		// A real rule: mapped key, non-empty content.
		"rules/graphql/frameworks/graphql_schema.yaml": {Data: []byte(
			"frameworks:\n  name: GraphQL Schema\nsource_patterns:\n- pattern: \"type (\\\\w+)\"\n  entity_type: Model\n")},
		// Documentation the loader never reads: two path segments.
		"rules/graphql/_manifest.yaml": {Data: []byte("server_frameworks:\n  name: not loaded\n")},
		// An unsupported subdir: three segments, but never loaded either.
		"rules/graphql/notes/reference.yaml": {Data: []byte("server_frameworks:\n  name: not loaded\n")},
	}

	inert, examined, err := inertRuleFiles(fsys, "rules", "graphql")
	if err != nil {
		t.Fatalf("inertRuleFiles: %v", err)
	}
	if examined != 3 {
		t.Errorf("examined = %d, want 3 (only <lang>/<subdir>/*.yaml in a supported subdir)", examined)
	}
	want := []string{
		"rules/graphql/frameworks/apollo_server.yaml",
		"rules/graphql/frameworks/urql.yaml",
	}
	if strings.Join(inert, ",") != strings.Join(want, ",") {
		t.Errorf("inert = %v, want %v", inert, want)
	}
}

// TestInertRuleFiles_RealRuleIsNotFlagged pins the other direction: a file the
// engine really does act on must never be reported as inert. A checker that
// flagged everything would kill the guard's usefulness just as thoroughly as
// one that flagged nothing.
func TestInertRuleFiles_RealRuleIsNotFlagged(t *testing.T) {
	cases := map[string]string{
		"detection markers only": "frameworks:\n  name: Yoga\n  detection:\n    import_markers:\n    - \"graphql-yoga\"\n",
		"file conventions only":  "file_conventions:\n- glob: \"**/*.graphql\"\n  entity_type: Model\n",
		"source patterns only":   "source_patterns:\n- pattern: \"type (\\\\w+)\"\n  entity_type: Model\n",
		"relationship rules only": "relationship_rules:\n- pattern: \"implements (\\\\w+)\"\n  " +
			"relationship_type: IMPLEMENTS\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{"rules/graphql/frameworks/x.yaml": {Data: []byte(body)}}
			inert, examined, err := inertRuleFiles(fsys, "rules", "graphql")
			if err != nil {
				t.Fatalf("inertRuleFiles: %v", err)
			}
			if examined != 1 {
				t.Fatalf("examined = %d, want 1", examined)
			}
			if len(inert) != 0 {
				t.Errorf("a rule carrying %s was reported inert: %v", name, inert)
			}
		})
	}
}
