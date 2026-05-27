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
