package engine_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// Issue #6776, arm B1 — the eight rule-declared entity kinds that arm A
// (c428cde7a) measured at ZERO entities on both corpora.
//
// Arm A counted entity kinds at the serialization leaf and found eight of the
// 25 ledgered kinds producing nothing on either corpus (the grafel repo, and
// testdata/fixtures): Decorator, Fixture, Implementation, Interface,
// Relationship, Template, TestClass, TestConfig — 17 declaration sites in six
// rule files. The obvious reading of a zero is "dead rule, delete it".
//
// A zero has two causes and arm A cannot tell them apart:
//
//	(a) the rule's pattern is DEAD — it cannot match anything, so no corpus
//	    could ever move it off zero; or
//	(b) the pattern is LIVE but UNEXERCISED — it describes a real framework
//	    construct that neither corpus happens to contain.
//
// This file settles that, per site, by driving the SHIPPED rule tree
// (engine.LoadAllRules) over a minimal input carrying the construct each
// pattern names. The measured answer is (b) for all 17 sites: every one fires.
// None of the eight kinds is dead, so arm B1 deletes no rule and the ledger in
// internal/entkinds does not move — see
// TestZeroProducingKinds6776_LedgerUnmoved there.
//
// Why the corpora read zero is then just absence of the construct: neither
// corpus contains a Jinja/`.j2` template directory, a conftest.py, a
// `@pytest.fixture`, a `class Test*`, a SQLAlchemy `relationship(...)`, a
// Kotlin `expect`/`actual` declaration, a GraphQL `interface`, or a GraphQL
// `directive` definition. TestZeroProducingKinds6776_CorporaLackTheConstructs
// observes that directly rather than asserting it in prose.
//
// # Both directions
//
// Recall cannot detect over-firing, so each source_pattern is pinned twice:
// once on an input it MUST match, and once on the nearest input it must NOT.
// The negatives are chosen to be the exact thing a widened pattern would
// swallow — `fun` without `expect`, `def` without the fixture decorator,
// `relationship(...)` without the assignment, `@auth` used rather than
// declared, `type Node` rather than `interface Node`. A firing-only version of
// this file would survive deleting the discriminating half of every pattern.
// ---------------------------------------------------------------------------

func detect6776(t *testing.T, lang, path, content string) *engine.DetectResult {
	t.Helper()
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	res, err := engine.New(rules).Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(content),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Detect(%s): %v", path, err)
	}
	return res
}

func entity6776(res *engine.DetectResult, kind, name string) (types.EntityRecord, bool) {
	for _, e := range res.Entities {
		if e.Kind == kind && e.Name == name {
			return e, true
		}
	}
	return types.EntityRecord{}, false
}

func anyOfKind6776(res *engine.DetectResult, kind string) bool {
	for _, e := range res.Entities {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

func dump6776(res *engine.DetectResult) string {
	var b strings.Builder
	for i, e := range res.Entities {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(e.Kind + "/" + e.Name)
	}
	if b.Len() == 0 {
		return "(no entities)"
	}
	return b.String()
}

// zeroKindSite is one declaration site from the arm-A zero list.
//
// `glob` is set for file_conventions sites only. The detector stamps the
// matched glob onto the emitted entity as the `file_convention` property, so
// for those sites the assertion names the exact YAML line that produced the
// entity rather than merely observing that SOMETHING of that kind appeared.
// source_patterns carry no per-rule identity in their properties, so those
// sites are separated by construction instead: within a language bucket each
// kind below is declared by exactly one source_pattern shape, and the paths
// are chosen so no file_convention for the same kind also matches.
type zeroKindSite struct {
	label   string // kind + the rule file:line it comes from
	lang    string
	path    string
	content string
	kind    string
	name    string
	glob    string // non-empty ⇒ file_convention site; asserted exactly
	negPath string // input the same rule must NOT produce `kind` on
	negBody string
	negWhy  string
}

func zeroKindSites() []zeroKindSite {
	return []zeroKindSite{
		{
			label: "Template — python/frameworks/flask.yaml:52 (file_convention)",
			lang:  "python", path: "templates/admin/index.html", content: "<h1>hi</h1>\n",
			kind: "Template", name: "index", glob: "templates/**/*.html",
			negPath: "static/admin/index.html", negBody: "<h1>hi</h1>\n",
			negWhy: "a .html file outside templates/ is not a Flask template",
		},
		{
			label: "Template — ansible/frameworks/ansible_core.yaml:37 (file_convention)",
			lang:  "ansible", path: "roles/web/templates/nginx.conf.j2", content: "server { }\n",
			kind: "Template", name: "nginx.conf", glob: "roles/*/templates/*.j2",
			negPath: "roles/web/tasks/main.yml", negBody: "- name: install\n",
			negWhy: "a task file in a role is not a template",
		},
		{
			label: "Relationship — python/frameworks/sqlalchemy.yaml:74 (source_pattern)",
			lang:  "python", path: "app/models.py",
			content: "class User(Base):\n    items = relationship(\"Item\")\n",
			kind:    "Relationship", name: "Item",
			negPath: "app/models.py", negBody: "# see relationship(\"Item\") in the docs\ncheck_relationship(\"Item\")\n",
			negWhy: "the pattern requires an assignment; a bare or commented call is not a mapped relationship",
		},
		{
			label: "TestConfig — python/frameworks/pytest.yaml:48 (file_convention)",
			lang:  "python", path: "conftest.py", content: "import pytest\n",
			kind: "TestConfig", name: "conftest", glob: "conftest.py",
			negPath: "conf.py", negBody: "import pytest\n",
			negWhy: "conf.py is not conftest.py",
		},
		{
			label: "TestClass — python/frameworks/pytest.yaml:60 (source_pattern)",
			lang:  "python", path: "tests/test_orders.py", content: "class TestOrders:\n    pass\n",
			kind: "TestClass", name: "TestOrders",
			negPath: "tests/test_orders.py", negBody: "class Orders:\n    pass\n",
			negWhy: "a class not named Test* is not a pytest test class",
		},
		{
			label: "Fixture — python/frameworks/pytest.yaml:65 (source_pattern)",
			lang:  "python", path: "tests/conftest_helpers.py",
			content: "import pytest\n\n@pytest.fixture\ndef db_session():\n    return 1\n",
			kind:    "Fixture", name: "db_session",
			negPath: "tests/conftest_helpers.py",
			negBody: "import pytest\n\n@pytest.mark.usefixtures(\"db\")\ndef db_session():\n    return 1\n",
			negWhy:  "a def under a non-fixture pytest decorator is not a fixture",
		},
		{
			label: "Interface — kotlin/frameworks/kmp.yaml:40 (file_convention)",
			lang:  "kotlin", path: "shared/commonMain/kotlin/Platform.kt", content: "package a\n",
			kind: "Interface", name: "Platform", glob: "*/commonMain/**/*.kt",
			negPath: "shared/androidMain/kotlin/Platform.kt", negBody: "package a\n",
			negWhy: "androidMain is a platform source set, not the common one",
		},
		{
			label: "Interface — kotlin/frameworks/kmp.yaml:59 (source_pattern, expect fun)",
			lang:  "kotlin", path: "shared/src/Platform.kt", content: "expect fun currentTime(): Long\n",
			kind: "Interface", name: "currentTime",
			negPath: "shared/src/Platform.kt", negBody: "fun currentTime(): Long = 0\n",
			negWhy: "an ordinary fun is not an expect declaration",
		},
		{
			label: "Interface — kotlin/frameworks/kmp.yaml:65 (source_pattern, expect class)",
			lang:  "kotlin", path: "shared/src/Clock.kt", content: "expect class Clock\n",
			kind: "Interface", name: "Clock",
			negPath: "shared/src/Clock.kt", negBody: "class Clock\n",
			negWhy: "an ordinary class is not an expect declaration",
		},
		{
			label: "Interface — graphql/frameworks/graphql_schema.yaml:65 (source_pattern)",
			lang:  "graphql", path: "schema.graphql", content: "interface Node {\n  id: ID!\n}\n",
			kind: "Interface", name: "Node",
			negPath: "schema.graphql", negBody: "type Node {\n  id: ID!\n}\n",
			negWhy: "an object type is not an interface",
		},
		{
			label: "Implementation — kotlin/frameworks/kmp.yaml:43 (file_convention, androidMain)",
			lang:  "kotlin", path: "shared/androidMain/kotlin/Platform.kt", content: "package a\n",
			kind: "Implementation", name: "Platform", glob: "*/androidMain/**/*.kt",
			negPath: "shared/commonMain/kotlin/Platform.kt", negBody: "package a\n",
			negWhy: "commonMain holds the expect side, not an actual implementation",
		},
		{
			label: "Implementation — kotlin/frameworks/kmp.yaml:46 (file_convention, iosMain)",
			lang:  "kotlin", path: "shared/iosMain/kotlin/Platform.kt", content: "package a\n",
			kind: "Implementation", name: "Platform", glob: "*/iosMain/**/*.kt",
			negPath: "shared/commonMain/kotlin/Platform.kt", negBody: "package a\n",
			negWhy: "commonMain holds the expect side, not an actual implementation",
		},
		{
			label: "Implementation — kotlin/frameworks/kmp.yaml:49 (file_convention, desktopMain)",
			lang:  "kotlin", path: "shared/desktopMain/kotlin/Platform.kt", content: "package a\n",
			kind: "Implementation", name: "Platform", glob: "*/desktopMain/**/*.kt",
			negPath: "shared/commonMain/kotlin/Platform.kt", negBody: "package a\n",
			negWhy: "commonMain holds the expect side, not an actual implementation",
		},
		{
			label: "Implementation — kotlin/frameworks/kmp.yaml:52 (file_convention, jsMain)",
			lang:  "kotlin", path: "shared/jsMain/kotlin/Platform.kt", content: "package a\n",
			kind: "Implementation", name: "Platform", glob: "*/jsMain/**/*.kt",
			negPath: "shared/commonMain/kotlin/Platform.kt", negBody: "package a\n",
			negWhy: "commonMain holds the expect side, not an actual implementation",
		},
		{
			label: "Implementation — kotlin/frameworks/kmp.yaml:72 (source_pattern, actual fun)",
			lang:  "kotlin", path: "shared/src/Platform.kt", content: "actual fun currentTime(): Long = 0\n",
			kind: "Implementation", name: "currentTime",
			negPath: "shared/src/Platform.kt", negBody: "expect fun currentTime(): Long\n",
			negWhy: "the expect side must not be counted as an implementation",
		},
		{
			label: "Implementation — kotlin/frameworks/kmp.yaml:78 (source_pattern, actual class)",
			lang:  "kotlin", path: "shared/src/Clock.kt", content: "actual class Clock\n",
			kind: "Implementation", name: "Clock",
			negPath: "shared/src/Clock.kt", negBody: "expect class Clock\n",
			negWhy: "the expect side must not be counted as an implementation",
		},
		{
			label: "Decorator — graphql/frameworks/graphql_schema.yaml:75 (source_pattern)",
			lang:  "graphql", path: "schema.graphql", content: "directive @auth on FIELD_DEFINITION\n",
			kind: "Decorator", name: "auth",
			negPath: "schema.graphql", negBody: "type User {\n  name: String @auth\n}\n",
			negWhy: "applying a directive is not declaring one",
		},
	}
}

// TestZeroProducingKinds6776_EverySiteFires is the whole verdict of arm B1: no
// rule behind the eight zero-producing kinds is dead. Each case drives the
// shipped rule tree over the construct its pattern names and requires the
// entity to appear, by kind AND name — a rule whose pattern were broken, whose
// glob could never match, or whose language bucket were unreachable turns
// exactly its own subtest red.
func TestZeroProducingKinds6776_EverySiteFires(t *testing.T) {
	sites := zeroKindSites()
	if len(sites) != 17 {
		t.Fatalf("arm A measured 17 declaration sites across the eight zero-producing kinds; "+
			"this table has %d — the table and the ledger have diverged", len(sites))
	}
	for _, s := range sites {
		t.Run(s.label, func(t *testing.T) {
			res := detect6776(t, s.lang, s.path, s.content)
			e, ok := entity6776(res, s.kind, s.name)
			if !ok {
				t.Fatalf("rule did not fire: want %s/%s from %s; got: %s",
					s.kind, s.name, s.path, dump6776(res))
			}
			if s.glob != "" {
				if got := e.Properties["pattern_type"]; got != "file_convention" {
					t.Errorf("pattern_type = %q, want file_convention: the entity came from some "+
						"other rule, so this case does not observe the file_convention site", got)
				}
				if got := e.Properties["file_convention"]; got != s.glob {
					t.Errorf("file_convention = %q, want %q: a different glob produced this entity",
						got, s.glob)
				}
			}
		})
	}
}

// TestZeroProducingKinds6776_NoSiteOverFires is the other direction. Firing
// alone would be satisfied by a pattern widened until it matches anything, and
// every serious defect in this milestone came from an over-permissive rule.
// Each negative is the nearest neighbour of its positive — same language, same
// kind, the discriminating token removed — so relaxing a rule turns exactly its
// own subtest red.
func TestZeroProducingKinds6776_NoSiteOverFires(t *testing.T) {
	for _, s := range zeroKindSites() {
		t.Run(s.label, func(t *testing.T) {
			res := detect6776(t, s.lang, s.negPath, s.negBody)
			if anyOfKind6776(res, s.kind) {
				t.Errorf("rule over-fires: %s emitted on %s (%s); got: %s",
					s.kind, s.negPath, s.negWhy, dump6776(res))
			}
		})
	}
}

// TestZeroProducingKinds6776_CorporaLackTheConstructs explains the zero arm A
// measured, instead of leaving the explanation as a comment. For each kind it
// searches testdata/fixtures for the construct its rule matches, restricted to
// the file shapes that rule's language bucket can reach — a TypeScript
// `interface Node` is not a GraphQL one, so an unrestricted grep would assert
// something false. Every search comes back empty, which is why the kinds read
// zero while the rules behind them are demonstrably live.
//
// If a fixture later introduces one of these constructs the kind stops being a
// zero-producer, and this test says so by name rather than the arm-A
// measurement drifting silently stale.
func TestZeroProducingKinds6776_CorporaLackTheConstructs(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "fixtures")
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("fixture corpus not found at %s: %v", root, err)
	}

	probes := []struct {
		kind   string
		exts   []string // file extensions the owning rule bucket reaches
		base   string   // exact base name (used instead of exts when set)
		marker string
	}{
		{kind: "Decorator", exts: []string{".graphql", ".gql", ".graphqls"}, marker: "directive @"},
		{kind: "Interface", exts: []string{".graphql", ".gql", ".graphqls"}, marker: "interface "},
		{kind: "Fixture", exts: []string{".py"}, marker: "@pytest.fixture"},
		{kind: "TestClass", exts: []string{".py"}, marker: "\nclass Test"},
		{kind: "Relationship", exts: []string{".py"}, marker: "= relationship("},
		{kind: "Implementation", exts: []string{".kt"}, marker: "actual fun "},
		{kind: "Interface", exts: []string{".kt"}, marker: "expect fun "},
		{kind: "Template", exts: []string{".j2"}, marker: ""},
		{kind: "TestConfig", base: "conftest.py", marker: ""},
	}

	hits := map[int][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		for i, pr := range probes {
			if pr.base != "" {
				if d.Name() == pr.base {
					hits[i] = append(hits[i], p)
				}
				continue
			}
			ext := strings.ToLower(filepath.Ext(p))
			matched := false
			for _, e := range pr.exts {
				if ext == e {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			if pr.marker == "" {
				hits[i] = append(hits[i], p)
				continue
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			if strings.Contains("\n"+string(b), pr.marker) {
				hits[i] = append(hits[i], p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	// The walk must actually have read the languages it claims to clear;
	// otherwise an empty result would mean "found no .py files" rather than
	// "found no fixtures". Counted separately so a corpus move cannot turn this
	// test into a vacuous pass.
	seenExt := map[string]int{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			seenExt[strings.ToLower(filepath.Ext(p))]++
		}
		return nil
	})
	for _, e := range []string{".py", ".kt", ".graphql"} {
		if seenExt[e] == 0 {
			t.Fatalf("testdata/fixtures holds no %s files; this test would pass vacuously", e)
		}
	}

	for i, pr := range probes {
		if got := hits[i]; len(got) > 0 {
			what := pr.marker
			if what == "" {
				what = pr.base
				if what == "" {
					what = strings.Join(pr.exts, "/")
				}
			}
			t.Errorf("%s: testdata/fixtures now contains %q (e.g. %s), so this kind is no longer "+
				"a zero-producer and the arm-A measurement is stale", pr.kind, what, got[0])
		}
	}
}
