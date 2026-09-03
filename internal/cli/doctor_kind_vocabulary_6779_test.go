package cli

// #6779 — a graph indexed under an OLDER entity-kind vocabulary must be
// SURFACED, naming the group and saying a reindex is needed. Nothing migrates
// and nothing reindexes on this signal: the user chooses when to pay for it.
//
// Everything here asserts on doctor's EMITTED OUTPUT — the bytes a human sees
// — never on DoctorRepoHealth's fields, following the #6640 precedent for the
// same reason: a test reading the field back survives the mutant that matters
// ("the renderer stops printing it"), because the field is still populated by
// the sidecar decode.
//
// All three states are asserted, and the two NEGATIVE directions carry the
// weight. A check that always reports a mismatch passes every positive test
// ever written; only `current` and `no graph` can catch it.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/types"
)

// vocabFixture6779 describes one seeded repo.
type vocabFixture6779 struct {
	// stamp is the vocabulary version written into graph-stats.json. Ignored
	// when noGraph is set.
	stamp int
	// noGraph seeds a state dir with NO stored graph at all — the third state.
	noGraph bool
}

// seedGroup6779 registers a single-repo group in an isolated GRAFEL_HOME and
// returns doctor's rendered report plus the computed health, both produced by
// the REAL path (registry → ComputeDoctorHealth → computeRepoHealth →
// graph-stats.json decode → PrintDoctorHealth).
func seedGroup6779(t *testing.T, group string, fx vocabFixture6779) (rendered string, health *DoctorGroupHealth) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv(daemon.EnvRoot, t.TempDir())

	repoPath := filepath.Join(home, "repos", "legacy")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if !fx.noGraph {
		doc := &graph.Document{
			Version:     graph.SchemaVersion,
			GeneratedAt: time.Now(),
			Repo:        "legacy",
			Entities: []graph.Entity{{
				ID:   "e1",
				Name: "Widget",
				Kind: string(types.EntityKindComponent),
			}},
			Stats: graph.Stats{Files: 1, Entities: 1},
		}
		if err := graph.WriteAtomic(filepath.Join(stateDir, "graph.json"), doc, false); err != nil {
			t.Fatal(err)
		}
		side := &graph.GraphStatsSidecar{
			Version:               1,
			ComputedAt:            time.Now(),
			TotalEntities:         1,
			KindVocabularyVersion: fx.stamp,
		}
		if err := graph.WriteSidecar(graph.SidecarPath(stateDir), side, false); err != nil {
			t.Fatal(err)
		}
	}

	cfgPath := filepath.Join(home, "groups", group+".json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"name":%q,"repos":[{"slug":"legacy","path":%q}]}`, group, repoPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup(group, cfgPath); err != nil {
		t.Fatal(err)
	}
	groups, err := registry.Groups()
	if err != nil {
		t.Fatalf("registry.Groups: %v", err)
	}
	reports := ComputeDoctorHealth(groups, false)

	// PREMISE GUARD — a fixture that silently produced no group would make
	// every "does not contain" assertion below pass for the wrong reason.
	var got *DoctorGroupHealth
	for _, r := range reports {
		if r.GroupName == group {
			got = r
		}
	}
	if got == nil {
		t.Fatalf("premise: group %q missing from doctor report (%d groups)", group, len(reports))
	}
	if len(got.Repos) != 1 {
		t.Fatalf("premise: want 1 repo in report, got %d", len(got.Repos))
	}

	var buf bytes.Buffer
	PrintDoctorHealth(&buf, reports)
	out := buf.String()
	if !strings.Contains(out, group) {
		t.Fatalf("premise: rendered report never names the group %q:\n%s", group, out)
	}
	return out, got
}

// vocabWarningLines returns the rendered lines that mention the kind
// vocabulary. Matching on "vocabulary" keeps the assertion about the CONTENT
// the user reads, not about an exact sentence.
func vocabWarningLines(rendered string) []string {
	var out []string
	for _, ln := range strings.Split(rendered, "\n") {
		if strings.Contains(strings.ToLower(ln), "vocabulary") {
			out = append(out, ln)
		}
	}
	return out
}

// perRepoSection returns the part of the rendered report ABOVE the Quality
// block — the per-repo stats table where the per-repo warnings are printed.
//
// This split exists because the whole-output match above is too generous to
// pin the renderer: doctor also prints IssuesFound verbatim in an "Issues
// found:" section further down, so deleting the per-repo warning line leaves
// the word "vocabulary" in the output and a whole-output assertion survives
// it. Asserting on the two surfaces SEPARATELY is what makes each of them
// independently required.
func perRepoSection(t *testing.T, rendered string) string {
	t.Helper()
	i := strings.Index(rendered, "\n  Quality:")
	if i < 0 {
		t.Fatalf("premise: rendered report has no Quality section to split on:\n%s", rendered)
	}
	return rendered[:i]
}

// TestDoctorReportsOlderKindVocabulary is the POSITIVE direction: a graph
// stamped with an older vocabulary is named, marked DEGRADED, and told to
// reindex.
func TestDoctorReportsOlderKindVocabulary(t *testing.T) {
	older := types.KindVocabularyVersion - 1
	rendered, health := seedGroup6779(t, "vocab-older", vocabFixture6779{stamp: older})

	// The PER-REPO warning line, asserted on its own section: this is the
	// surface a human reads next to the repo's status line, and it must exist
	// independently of the "Issues found:" summary below. Asserting only on
	// the whole output would let a deleted per-repo warning survive, because
	// the issue text further down also says "vocabulary".
	repoLines := vocabWarningLines(perRepoSection(t, rendered))
	if len(repoLines) == 0 {
		t.Fatalf("doctor's per-repo section never mentions the kind vocabulary for a stale-vocabulary graph:\n%s", rendered)
	}
	joined := strings.Join(repoLines, "\n")
	if !strings.Contains(strings.ToLower(joined), "reindex") {
		t.Errorf("warning does not tell the user to reindex:\n%s", joined)
	}
	if !strings.Contains(joined, fmt.Sprintf("v%d", older)) ||
		!strings.Contains(joined, fmt.Sprintf("v%d", types.KindVocabularyVersion)) {
		t.Errorf("warning names neither the stored (v%d) nor this build's (v%d) vocabulary:\n%s",
			older, types.KindVocabularyVersion, joined)
	}
	if health.Status != "DEGRADED" {
		t.Errorf("group status = %q, want DEGRADED", health.Status)
	}
	var issue string
	for _, i := range health.IssuesFound {
		if strings.Contains(strings.ToLower(i), "vocabulary") {
			issue = i
		}
	}
	if issue == "" {
		t.Errorf("no doctor issue raised for the stale vocabulary: %v", health.IssuesFound)
	} else if !strings.Contains(issue, "legacy") {
		t.Errorf("issue does not name the repo: %q", issue)
	}
}

// TestDoctorSilentOnCurrentKindVocabulary is the UNDER-FIRING control: a graph
// stamped with THIS build's vocabulary must produce no warning at all. Without
// it a check that reports a mismatch unconditionally passes the positive test
// above.
func TestDoctorSilentOnCurrentKindVocabulary(t *testing.T) {
	rendered, health := seedGroup6779(t, "vocab-current", vocabFixture6779{stamp: types.KindVocabularyVersion})

	if lines := vocabWarningLines(rendered); len(lines) != 0 {
		t.Errorf("doctor warns about the vocabulary on a CURRENT graph:\n%s", strings.Join(lines, "\n"))
	}
	for _, i := range health.IssuesFound {
		if strings.Contains(strings.ToLower(i), "vocabulary") {
			t.Errorf("doctor raised a vocabulary issue on a CURRENT graph: %q", i)
		}
	}
}

// TestDoctorSilentWhenNoGraphIndexed is the THREE-STATE control: a repo with
// no stored graph has no vocabulary to be stale. Telling its owner to reindex
// "because the vocabulary is old" would be a confident wrong answer about a
// graph that does not exist — and a check that produced it would be one that
// had collapsed no-graph into older-vocabulary.
func TestDoctorSilentWhenNoGraphIndexed(t *testing.T) {
	rendered, health := seedGroup6779(t, "vocab-nograph", vocabFixture6779{noGraph: true})

	if lines := vocabWarningLines(rendered); len(lines) != 0 {
		t.Errorf("doctor warns about the vocabulary on a repo with NO graph:\n%s", strings.Join(lines, "\n"))
	}
	for _, i := range health.IssuesFound {
		if strings.Contains(strings.ToLower(i), "vocabulary") {
			t.Errorf("doctor raised a vocabulary issue on a repo with NO graph: %q", i)
		}
	}
}

// TestDoctorReportsUnstampedGraphAsOlder covers the shape every graph indexed
// before this mechanism existed actually has on disk: a sidecar with no
// vocabulary field at all. It is on an older vocabulary — v0.3.1 renamed kinds
// under it — and must be reported, not waved through as current.
func TestDoctorReportsUnstampedGraphAsOlder(t *testing.T) {
	rendered, _ := seedGroup6779(t, "vocab-unstamped", vocabFixture6779{stamp: 0})
	if lines := vocabWarningLines(perRepoSection(t, rendered)); len(lines) == 0 {
		t.Fatalf("doctor is silent about a graph whose sidecar predates the vocabulary stamp:\n%s", rendered)
	}
}
