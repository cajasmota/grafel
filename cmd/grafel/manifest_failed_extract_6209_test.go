package main

// #6209 — a file whose EXTRACTION failed must not be recorded in the manifest
// as though it had been indexed.
//
// commitManifest stamps every file in the raw walk. A file that was read
// successfully is stamped at the bytes the walk read (#6212) — including when
// the extractor that ran on those bytes returned an error and contributed
// nothing to the graph. The next incremental pass hashes the file, matches the
// stamp, classifies it UNCHANGED, and never extracts it again. The graph is
// permanently short that file's entities and nothing anywhere says so; only a
// content edit ever re-presents it.
//
// A file that is never stamped at all behaves the opposite way — it reads as
// new on every pass and eventually succeeds — which is why the stamp, not the
// failure, is the defect.
//
// THE TRADE-OFF THIS TEST PINS. Simply excluding failed files from the stamp
// heals the transient case and converts the deterministic case into a permanent
// tax: a genuinely unparseable file would be re-read and re-parsed on every pass
// forever. So the manifest carries a per-file consecutive-failure counter and
// the retry is BOUNDED (diff.MaxExtractRetries). Both halves are asserted here:
// the file must come back, and it must eventually stop coming back.
//
// Assertions are on extraction counts per pass and on manifest contents. Never
// on timing.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/indexer/diff"
	"github.com/cajasmota/grafel/internal/types"
)

// failedExtractHarness drives repeated Index(..., WithIncremental(stateDir))
// passes over one committed git fixture with a single file whose extraction
// always fails, and reports how many times each file was handed to Pass 1 on
// each pass.
type failedExtractHarness struct {
	repo     string
	stateDir string
	victim   string
	// unsupported, when set, is a file whose extractor reports
	// ErrNoExtractorForLanguage. That is a permanent, CORRECT classification —
	// the repo contains a language grafel does not model — not a failure to
	// retry, and the indexer counts it as skipped rather than failed. Set
	// before the first pass; read by the seam thereafter.
	unsupported string

	mu       sync.Mutex
	attempts map[string]int
	// lastLogs is the stderr of the most recent pass. The budget-exhaustion
	// warning is the only thing that announces a file has gone permanently
	// dark, so it is an assertable output, not decoration.
	lastLogs string
}

func newFailedExtractHarness(t *testing.T, name, victim string) *failedExtractHarness {
	t.Helper()
	h := &failedExtractHarness{
		repo:     fallbackManifestRepo(t, name, "main"),
		victim:   victim,
		attempts: map[string]int{},
	}
	h.stateDir = filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(h.stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAFEL_DAEMON_ROOT", h.stateDir)

	prev := extractPass1
	extractPass1 = func(ctx context.Context, f extractors.FileInput) ([]types.EntityRecord, error) {
		rel := filepath.ToSlash(f.Path)
		h.mu.Lock()
		h.attempts[rel]++
		h.mu.Unlock()
		if rel == h.victim {
			// Deterministic: the same file fails identically on every pass,
			// which is precisely the case an unbounded retry would tax forever.
			return nil, fmt.Errorf("synthetic deterministic extraction failure (#6209)")
		}
		if h.unsupported != "" && rel == h.unsupported {
			return nil, extractors.ErrNoExtractorForLanguage
		}
		return prev(ctx, f)
	}
	t.Cleanup(func() { extractPass1 = prev })
	return h
}

// pass runs one index and returns the per-file Pass-1 extraction counts for
// THAT pass alone.
func (h *failedExtractHarness) pass(t *testing.T) map[string]int {
	t.Helper()
	h.mu.Lock()
	before := map[string]int{}
	for k, v := range h.attempts {
		before[k] = v
	}
	h.mu.Unlock()

	var idxErr error
	logs := captureStderr(t, func() {
		idxErr = Index(h.repo, filepath.Join(h.stateDir, "graph.fb"), "test-repo",
			[]string{"graph-algo"}, false, false, WithIncremental(h.stateDir))
	})
	if idxErr != nil {
		t.Fatalf("Index: %v\n%s", idxErr, logs)
	}
	h.lastLogs = logs

	h.mu.Lock()
	defer h.mu.Unlock()
	delta := map[string]int{}
	for k, v := range h.attempts {
		if d := v - before[k]; d > 0 {
			delta[k] = d
		}
	}
	return delta
}

func (h *failedExtractHarness) manifest() *diff.Manifest {
	return diff.LoadManifest(h.stateDir)
}

// TestIncremental_FailedExtractionIsRetried is the defect.
//
// Pass 1 indexes everything and the victim's extractor fails. Pass 2 must hand
// that file to the extractor again: the graph does not contain its entities, so
// the manifest may not claim it is indexed.
func TestIncremental_FailedExtractionIsRetried(t *testing.T) {
	if testing.Short() {
		t.Skip("runs real index passes; skipped under -short")
	}
	const victim = "svc/thing.go"
	const unsupported = "svc/gadget.go"
	h := newFailedExtractHarness(t, "failstamp-retry", victim)
	h.unsupported = unsupported

	pass1 := h.pass(t)
	if got := pass1[victim]; got != 1 {
		t.Fatalf("fixture is inert: pass 1 handed %s to the extractor %d time(s), want exactly 1", victim, got)
	}
	if got := pass1[unsupported]; got != 1 {
		t.Fatalf("fixture is inert: pass 1 handed %s to the extractor %d time(s), want exactly 1 — "+
			"the ErrNoExtractorForLanguage case is not being exercised at all", unsupported, got)
	}

	m := h.manifest()
	if _, ok := m.Files[victim]; !ok {
		t.Fatalf("fixture is inert: %s has no manifest entry at all after pass 1, so there is "+
			"nothing for pass 2 to hash-match away", victim)
	}
	if got := m.Files[victim].Failures; got != 1 {
		t.Fatalf("manifest records %d consecutive extraction failure(s) for %s after pass 1, want 1 — "+
			"the entry is indistinguishable from a successfully indexed file, which is exactly the "+
			"silent gap #6209 is about", got, victim)
	}
	for _, rel := range fallbackFixtureFiles {
		if rel == victim {
			continue
		}
		if got := m.Files[rel].Failures; got != 0 {
			if rel == unsupported {
				t.Fatalf("manifest records %d failure(s) for %s, whose extractor reported "+
					"ErrNoExtractorForLanguage. That is a permanent and CORRECT answer — the language "+
					"is not modelled — so retrying it buys nothing and costs a re-read plus a re-parse "+
					"on every unsupported file in the repo, %d times each (#6209)",
					got, rel, diff.MaxExtractRetries)
			}
			t.Fatalf("manifest records %d failure(s) for %s, which extracted cleanly — the failure "+
				"mark must name the file that failed, not the walk (#6209)", got, rel)
		}
	}

	pass2 := h.pass(t)
	if pass2[victim] != 1 {
		t.Fatalf("pass 2 handed %s to the extractor %d time(s), want 1. The file's extraction FAILED "+
			"on pass 1, so its entities are missing from the graph; stamping it as indexed anyway "+
			"means it is hash-matched away on every future pass and never retried until its bytes "+
			"change (#6209).\nfull pass-2 extraction set: %v", victim, pass2[victim], pass2)
	}
	for rel, n := range pass2 {
		if rel != victim {
			t.Fatalf("pass 2 also re-extracted %s (%d time(s)); only the failed file should have "+
				"re-presented, otherwise the retry is just a full reindex wearing a hat: %v", rel, n, pass2)
		}
	}
}

// TestIncremental_FailedExtractionRetryIsBounded is the other half.
//
// The naive fix — leave a failed file unstamped so it re-presents — passes the
// test above and fails this one: a deterministically unparseable file would be
// re-read and re-parsed on every pass for the life of the repo. The retry
// budget is what makes "never silently skipped" affordable.
func TestIncremental_FailedExtractionRetryIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("runs real index passes; skipped under -short")
	}
	if diff.MaxExtractRetries != 2 {
		t.Fatalf("this test enumerates passes by hand for MaxExtractRetries == 2, got %d",
			diff.MaxExtractRetries)
	}
	const victim = "svc/thing.go"
	h := newFailedExtractHarness(t, "failstamp-bounded", victim)

	// Pass 1 is the initial attempt; passes 2 and 3 are the two retries.
	for n := 1; n <= 1+diff.MaxExtractRetries; n++ {
		if got := h.pass(t)[victim]; got != 1 {
			t.Fatalf("pass %d handed %s to the extractor %d time(s), want 1", n, victim, got)
		}
		if got := h.manifest().Files[victim].Failures; got != n {
			t.Fatalf("after pass %d the manifest records %d consecutive failure(s) for %s, want %d — "+
				"without a durable count the retry cannot be bounded (#6209)", n, got, victim, n)
		}
		// The warning belongs to the pass that spends the LAST attempt, and to
		// no other. Earlier is a false alarm about a file still being retried.
		warned := strings.Contains(h.lastLogs, "no longer retrying")
		if want := n == 1+diff.MaxExtractRetries; warned != want {
			t.Fatalf("pass %d logged the give-up warning = %v, want %v. The manifest count is durable "+
				"but nothing outside the diff package reads it, so this line is the ONLY signal that "+
				"%s just went permanently dark (#6209).\nlogs:\n%s", n, warned, want, victim, h.lastLogs)
		}
		if warned && !strings.Contains(h.lastLogs, victim) {
			t.Fatalf("the give-up warning does not name the file it is about:\n%s", h.lastLogs)
		}
	}

	// The budget is spent. The file stays stamped and stays quiet until its
	// bytes change.
	pass4 := h.pass(t)
	if n := pass4[victim]; n != 0 {
		t.Fatalf("pass %d re-extracted %s again (%d time(s)) after %d consecutive deterministic "+
			"failures. An unbounded retry converts a silent gap into a permanent per-pass parse "+
			"tax on every genuinely unparseable file in the repo (#6209).",
			2+diff.MaxExtractRetries, victim, n, 1+diff.MaxExtractRetries)
	}
	if len(pass4) != 0 {
		t.Fatalf("pass %d extracted %v; a pass with no content change must extract nothing",
			2+diff.MaxExtractRetries, pass4)
	}
	if got := h.manifest().Files[victim].Failures; got != 1+diff.MaxExtractRetries {
		t.Fatalf("the failure count for %s moved to %d on a pass that never re-extracted it, want it "+
			"pinned at %d — the count records consecutive ATTEMPTS, and a pass that skipped the file "+
			"has not made one (#6209)", victim, got, 1+diff.MaxExtractRetries)
	}
}
