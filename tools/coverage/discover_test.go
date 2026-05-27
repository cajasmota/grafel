package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRoot returns the path to the mini-repo fixture that exercises
// every discovery source.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "discover-fixture")
}

// fixtureRegistry returns the path to the small registry checked in
// alongside the fixture repo.
func fixtureRegistry(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "discover-fixture-registry.json")
}

func TestYAMLWalker(t *testing.T) {
	cands := map[string]*Candidate{}
	yamlWalker(fixtureRoot(t), cands)
	if _, ok := cands["lang.python.framework.flask"]; !ok {
		t.Fatalf("expected flask candidate, got: %v", keys(cands))
	}
	if _, ok := cands["lang.python.framework.quart"]; !ok {
		t.Fatalf("expected quart candidate")
	}
	if _, ok := cands["lang.python.orm.sqlalchemy"]; !ok {
		t.Fatalf("expected sqlalchemy orm candidate")
	}
	if _, ok := cands["lang.python"]; !ok {
		t.Fatalf("expected language-level python candidate")
	}
}

func TestSynthesizerGrep(t *testing.T) {
	cands := map[string]*Candidate{}
	yamlWalker(fixtureRoot(t), cands)
	synthesizerGrep(fixtureRoot(t), cands)
	flask, ok := cands["lang.python.framework.flask"]
	if !ok {
		t.Fatal("flask missing")
	}
	if !hasEvidence(flask, "synthesizer", "synthesizeFlask") {
		t.Fatalf("flask missing synthesizer evidence: %+v", flask.Evidence)
	}
	if _, ok := cands["synth.unknownfw"]; !ok {
		t.Fatalf("expected unresolved synthesizer to land as synth.unknownfw, keys=%v", keys(cands))
	}
}

func TestExtractorDirLister(t *testing.T) {
	cands := map[string]*Candidate{}
	extractorDirLister(fixtureRoot(t), cands)
	if c, ok := cands["lang.python"]; !ok || !hasEvidence(c, "extractor_dir", "") {
		t.Fatalf("expected extractor_dir evidence on lang.python: %+v", c)
	}
}

func TestFixtureLister(t *testing.T) {
	cands := map[string]*Candidate{}
	yamlWalker(fixtureRoot(t), cands)
	fixtureLister(fixtureRoot(t), cands)
	c := cands["lang.python.framework.flask"]
	if !hasEvidence(c, "test_fixture", "") {
		t.Fatalf("expected test_fixture evidence on flask: %+v", c.Evidence)
	}
}

func TestEnginePatternMatcher(t *testing.T) {
	cands := map[string]*Candidate{}
	yamlWalker(fixtureRoot(t), cands)
	enginePatternMatcher(fixtureRoot(t), cands)
	c := cands["lang.python.framework.flask"]
	found := false
	for _, e := range c.Evidence {
		if e.Kind == "engine_file" && strings.HasSuffix(e.Path, "flask_routes.go") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected engine_file flask_routes.go evidence: %+v", c.Evidence)
	}
}

func TestDeterminism(t *testing.T) {
	res1, err := Discover(fixtureRoot(t), fixtureRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	res2, err := Discover(fixtureRoot(t), fixtureRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	var b1, b2 bytes.Buffer
	if err := writeDiscoverJSON(&b1, res1); err != nil {
		t.Fatal(err)
	}
	if err := writeDiscoverJSON(&b2, res2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1.Bytes(), b2.Bytes()) {
		t.Fatalf("non-deterministic output:\nrun1:\n%s\nrun2:\n%s", b1.String(), b2.String())
	}
}

func TestMergeProposalAndOrphans(t *testing.T) {
	res, err := Discover(fixtureRoot(t), fixtureRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	// flask is in both registry and discovered → AlreadyInRegistry.
	var flask *Candidate
	for i := range res.Proposal {
		if res.Proposal[i].CandidateID == "lang.python.framework.flask" {
			flask = &res.Proposal[i]
			break
		}
	}
	if flask == nil {
		t.Fatal("flask not in proposal")
	}
	if !flask.AlreadyInRegistry {
		t.Fatalf("flask should be already_in_registry")
	}
	// Orphan: deprecated-fwk has status=partial in registry but no code evidence.
	foundOrphan := false
	for _, o := range res.OrphansInRegistry {
		if o.ID == "lang.python.framework.deprecated-fwk" {
			foundOrphan = true
			break
		}
	}
	if !foundOrphan {
		t.Fatalf("expected deprecated-fwk orphan (status=partial, no evidence), got: %+v", res.OrphansInRegistry)
	}
}

func TestStatusUpgradeCandidates(t *testing.T) {
	res, err := Discover(fixtureRoot(t), fixtureRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	// quart is in registry with status=missing, and we have code evidence for it
	// (YAML rule exists) → status_upgrade_candidate.
	foundQuart := false
	for _, s := range res.StatusUpgradeCandidates {
		if s.ID == "lang.python.framework.quart" {
			foundQuart = true
			if s.CurrentStatus != "missing" {
				t.Fatalf("expected current_status=missing, got %s", s.CurrentStatus)
			}
			if len(s.EvidenceFound) == 0 {
				t.Fatalf("expected evidence_found to be non-empty")
			}
			if s.SuggestedStatus != "partial" {
				t.Fatalf("expected suggested_status=partial (conservative), got %s", s.SuggestedStatus)
			}
			break
		}
	}
	if !foundQuart {
		t.Fatalf("expected quart in status_upgrade_candidates, got: %+v", res.StatusUpgradeCandidates)
	}
}

func TestAspirationalsNotReportedAsOrphans(t *testing.T) {
	res, err := Discover(fixtureRoot(t), fixtureRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	// aspirational-fwk is in registry with status=missing, and we have NO code evidence.
	// It should NOT be reported as an orphan (it's intentional backlog).
	for _, o := range res.OrphansInRegistry {
		if o.ID == "lang.python.framework.aspirational-fwk" {
			t.Fatalf("aspirational-fwk should NOT be reported as orphan, but found: %+v", o)
		}
	}
	// Also check it's not a status_upgrade_candidate since we have no evidence.
	for _, s := range res.StatusUpgradeCandidates {
		if s.ID == "lang.python.framework.aspirational-fwk" {
			t.Fatalf("aspirational-fwk should NOT be a status_upgrade_candidate (no evidence), but found: %+v", s)
		}
	}
}

func TestCiteDriftDetection(t *testing.T) {
	res, err := Discover(fixtureRoot(t), fixtureRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	// The registry cites "internal/engine/old_flask_path.go" which
	// does not exist under the fixture root → must show up as drift.
	var drift *CiteDriftItem
	for i := range res.CiteDrift {
		if res.CiteDrift[i].ID == "lang.python.framework.flask" {
			drift = &res.CiteDrift[i]
			break
		}
	}
	if drift == nil {
		t.Fatalf("expected cite drift for flask, got: %+v", res.CiteDrift)
	}
	if len(drift.StaleCites) == 0 {
		t.Fatalf("expected stale cites listed: %+v", drift)
	}
	foundStale := false
	for _, s := range drift.StaleCites {
		if s == "internal/engine/old_flask_path.go" {
			foundStale = true
			break
		}
	}
	if !foundStale {
		t.Fatalf("expected old_flask_path.go in stale: %+v", drift.StaleCites)
	}
}

func TestCmdDiscoverJSONOutput(t *testing.T) {
	var out bytes.Buffer
	if err := cmdDiscover([]string{"--repo-root", fixtureRoot(t), "--registry", fixtureRegistry(t), "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	// Smoke check: JSON output mentions a known candidate.
	if !strings.Contains(out.String(), "lang.python.framework.flask") {
		t.Fatalf("expected flask in output: %s", out.String())
	}
}

func TestBuildWalker(t *testing.T) {
	cands := map[string]*Candidate{}
	buildWalker(fixtureRoot(t), cands)
	// fixture python build_tools.yaml mentions pip, poetry, uv.
	for _, want := range []string{"build.pip", "build.poetry", "build.uv"} {
		c, ok := cands[want]
		if !ok {
			t.Fatalf("expected %s candidate, got: %v", want, keys(cands))
		}
		if !hasEvidence(c, "yaml_rule", "python/build_tools.yaml") {
			t.Fatalf("expected yaml_rule evidence for %s: %+v", want, c.Evidence)
		}
	}
	// Dockerfile extractor dir → build.dockerfile evidence.
	if c, ok := cands["build.dockerfile"]; !ok || !hasEvidence(c, "extractor_dir", "extractors/dockerfile") {
		t.Fatalf("expected build.dockerfile extractor_dir evidence: %+v", c)
	}
	// Cross/manifest extractor source mentions go.mod, package.json, cargo.toml.
	for _, want := range []string{"build.go-modules", "build.npm", "build.cargo"} {
		c, ok := cands[want]
		if !ok {
			t.Fatalf("expected %s candidate from manifest source, got: %v", want, keys(cands))
		}
		if !hasEvidence(c, "extractor_source", "manifest/extractor.go") {
			t.Fatalf("expected extractor_source evidence for %s: %+v", want, c.Evidence)
		}
	}
}

func TestCIWalker(t *testing.T) {
	cands := map[string]*Candidate{}
	ciWalker(fixtureRoot(t), cands)
	if c, ok := cands["ci.github-actions"]; !ok || !hasEvidence(c, "yaml_rule", "github_actions.yaml") {
		t.Fatalf("expected ci.github-actions yaml_rule evidence: %+v", c)
	}
	if c, ok := cands["ci.circleci"]; !ok || !hasEvidence(c, "yaml_rule", "circleci.yaml") {
		t.Fatalf("expected ci.circleci yaml_rule evidence: %+v", c)
	}
}

func TestObservabilityWalker(t *testing.T) {
	cands := map[string]*Candidate{}
	observabilityWalker(fixtureRoot(t), cands)
	for _, want := range []string{
		"infra.observability.opentelemetry",
		"infra.observability.sentry",
		"infra.observability.prometheus",
		"infra.observability.datadog",
		"infra.observability.newrelic",
		"infra.observability.honeycomb",
	} {
		c, ok := cands[want]
		if !ok {
			t.Fatalf("expected %s candidate, got: %v", want, keys(cands))
		}
		if len(c.Evidence) == 0 {
			t.Fatalf("%s has no evidence", want)
		}
	}
}

func TestBrokerWalker(t *testing.T) {
	cands := map[string]*Candidate{}
	brokerWalker(fixtureRoot(t), cands)
	if c, ok := cands["msg.broker.kafka"]; !ok || !hasEvidence(c, "engine_file", "kafka_edges.go") {
		t.Fatalf("expected msg.broker.kafka engine_file evidence: %+v", c)
	}
	if c, ok := cands["msg.broker.nats"]; !ok || !hasEvidence(c, "engine_file", "nats_edges.go") {
		t.Fatalf("expected msg.broker.nats engine_file evidence: %+v", c)
	}
}

func TestContainerWalker(t *testing.T) {
	cands := map[string]*Candidate{}
	containerWalker(fixtureRoot(t), cands)
	for _, want := range []string{
		"infra.container.dockerfile",
		"infra.container.docker-compose",
		"infra.container.kubernetes",
	} {
		c, ok := cands[want]
		if !ok {
			t.Fatalf("expected %s candidate, got: %v", want, keys(cands))
		}
		if len(c.Evidence) == 0 {
			t.Fatalf("%s has no evidence", want)
		}
	}
}

func TestIacWalker(t *testing.T) {
	cands := map[string]*Candidate{}
	iacWalker(fixtureRoot(t), cands)
	if c, ok := cands["infra.iac.terraform"]; !ok || !hasEvidence(c, "yaml_rule", "hcl/_manifest.yaml") {
		t.Fatalf("expected infra.iac.terraform yaml_rule evidence: %+v", c)
	}
	if c, ok := cands["infra.iac.cloudformation"]; !ok {
		t.Fatalf("expected infra.iac.cloudformation candidate, got: %v", keys(cands))
	} else if len(c.Evidence) == 0 {
		t.Fatalf("infra.iac.cloudformation has no evidence")
	}
}

func TestDatabaseWalker(t *testing.T) {
	cands := map[string]*Candidate{}
	databaseWalker(fixtureRoot(t), cands)
	// fixture has python/orms/postgresql_py.yaml and redis_py.yaml.
	if c, ok := cands["lang.python.driver.postgres"]; !ok {
		t.Fatalf("expected lang.python.driver.postgres candidate, got: %v", keys(cands))
	} else if !hasEvidence(c, "yaml_rule", "postgresql_py.yaml") {
		t.Fatalf("expected postgresql_py.yaml evidence: %+v", c.Evidence)
	}
	if c, ok := cands["lang.python.driver.redis"]; !ok {
		t.Fatalf("expected lang.python.driver.redis candidate")
	} else if !hasEvidence(c, "yaml_rule", "redis_py.yaml") {
		t.Fatalf("expected redis_py.yaml evidence: %+v", c.Evidence)
	}
	// Also a top-level db.* candidate.
	if _, ok := cands["db.postgres"]; !ok {
		t.Fatalf("expected db.postgres candidate, got: %v", keys(cands))
	}
	if _, ok := cands["db.redis"]; !ok {
		t.Fatalf("expected db.redis candidate")
	}
}

func TestConfigWalker(t *testing.T) {
	cands := map[string]*Candidate{}
	configWalker(fixtureRoot(t), cands)
	for _, want := range []string{
		"config.toml", "config.ini", "config.properties", "config.dotenv",
		"config.makefile", "config.tsconfig", "config.yaml",
	} {
		c, ok := cands[want]
		if !ok {
			t.Fatalf("expected %s candidate, got: %v", want, keys(cands))
		}
		if !hasEvidence(c, "extractor_source", "config/discover.go") {
			t.Fatalf("expected extractor_source evidence for %s: %+v", want, c.Evidence)
		}
	}
}

func TestNewWalkersDeterminism(t *testing.T) {
	// Run all new walkers twice and assert the candidate map is
	// byte-identical when sorted by ID.
	run := func() string {
		cands := map[string]*Candidate{}
		buildWalker(fixtureRoot(t), cands)
		ciWalker(fixtureRoot(t), cands)
		observabilityWalker(fixtureRoot(t), cands)
		brokerWalker(fixtureRoot(t), cands)
		containerWalker(fixtureRoot(t), cands)
		iacWalker(fixtureRoot(t), cands)
		databaseWalker(fixtureRoot(t), cands)
		configWalker(fixtureRoot(t), cands)
		ids := make([]string, 0, len(cands))
		for id := range cands {
			ids = append(ids, id)
		}
		insertionSortStrings(ids)
		var b bytes.Buffer
		for _, id := range ids {
			c := cands[id]
			b.WriteString(id)
			b.WriteByte('|')
			for _, e := range c.Evidence {
				b.WriteString(e.Kind)
				b.WriteByte(':')
				b.WriteString(e.Path)
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		return b.String()
	}
	a := run()
	c := run()
	if a != c {
		t.Fatalf("non-deterministic walker output:\n%s\n---\n%s", a, c)
	}
}

// insertionSortStrings is a tiny helper that avoids importing sort in
// the test file; used by TestNewWalkersDeterminism only.
func insertionSortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// hasEvidence reports whether the candidate has an evidence entry of
// the requested kind whose symbol or path contains needle.
func hasEvidence(c *Candidate, kind, needle string) bool {
	if c == nil {
		return false
	}
	for _, e := range c.Evidence {
		if e.Kind != kind {
			continue
		}
		if needle == "" {
			return true
		}
		if e.Symbol == needle || strings.Contains(e.Path, needle) {
			return true
		}
	}
	return false
}

func keys(m map[string]*Candidate) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
