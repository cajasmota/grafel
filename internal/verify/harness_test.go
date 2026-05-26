// Package verify exercises the VERIFY-2 measurement harness against the
// in-repo synthetic corpus at testdata/fixtures/sources/. The test asserts a
// minimum entity / relationship floor and a regression-net bug-rate
// ceiling — NOT the v1.0 ship-gate threshold (which is gated on the
// public OSS corpus exercised by scripts/verify2/run.sh).
//
// Refs issue #58.
package verify

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
)

// jsonStats mirrors the cmd/archigraph JSONStats shape. Re-declared here
// (instead of imported) because cmd/archigraph is package main, which is
// not importable.
type jsonStats struct {
	Repo                 string         `json:"repo"`
	Files                int            `json:"files"`
	Entities             int            `json:"entities"`
	Relationships        int            `json:"relationships"`
	Pass1Rels            int            `json:"pass1_rels"`
	Pass2Rels            int            `json:"pass2_rels"`
	Pass3Rels            int            `json:"pass3_rels"`
	DispositionCounts    map[string]int `json:"disposition_counts"`
	BugRate              float64        `json:"bug_rate"`
	ResolutionRate       float64        `json:"resolution_rate"`
	ExternalSynthesized  int            `json:"external_synthesized"`
	ExternalUniqueCount  int            `json:"external_unique_count"`
	ExternalRelsResolved int            `json:"external_rels_resolved"`
}

// repoRoot walks up from this test file to the module root (the directory
// containing go.mod). Tests in internal/verify run from that subdir, so
// we can't hard-code "..".
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	// internal/verify/harness_test.go -> module root is two levels up.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// TestHarness_FixturesCorpus builds archigraph, runs `index --json-stats`
// against testdata/fixtures/sources/, and asserts the regression net: at least
// some entities / relationships were extracted and the bug-rate is well
// below the catastrophic-failure threshold. The actual ship-gate
// (bug-rate <= 1%) is enforced by scripts/verify2/run.sh against the
// public OSS corpus, not this synthetic fixture set.
func TestHarness_FixturesCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	root := repoRoot(t)
	binName := "archigraph"
	if runtime.GOOS == "windows" {
		binName = "archigraph.exe"
	}
	bin := filepath.Join(t.TempDir(), binName)

	build := exec.Command("go", "build", "-o", bin, "./cmd/archigraph")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	corpus := filepath.Join(root, "testdata", "fixtures", "sources")

	// Per ADR-0017, indexing happens inside the daemon. Point the daemon
	// at an isolated tempdir (so this test never touches ~/.archigraph)
	// and start it in the background; the test calls Index via RPC and
	// stops the daemon on cleanup.
	//
	// macOS limits AF_UNIX sun_path to ~103 bytes — t.TempDir() can
	// easily exceed that, so we mint a short dir under os.TempDir() instead.
	// On Windows, os.TempDir() returns the correct system temp directory.
	daemonRoot, err := os.MkdirTemp(os.TempDir(), "archi-d-")
	if err != nil {
		t.Fatalf("mktemp daemon root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(daemonRoot) })

	// Derive the layout to get the correct socket path (named pipe on Windows,
	// Unix domain socket on other platforms).
	t.Setenv(daemon.EnvRoot, daemonRoot)
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("daemon layout: %v", err)
	}

	dcmd := exec.Command(bin, "daemon")
	dcmd.Env = append(os.Environ(), daemon.EnvRoot+"="+daemonRoot)
	var daemonOut bytes.Buffer
	dcmd.Stdout = &daemonOut
	dcmd.Stderr = &daemonOut
	if err := dcmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() {
		if c, err := client.DialPath(layout.SocketPath); err == nil {
			_ = c.Stop()
			_ = c.Close()
		}
		_ = dcmd.Wait()
		if t.Failed() {
			t.Logf("daemon output:\n%s", daemonOut.String())
		}
	})

	// Wait for the daemon to become connectable.
	// On Unix we can stat the socket file; on Windows named pipes are not
	// filesystem objects, so we always poll via DialPath.
	deadline := time.Now().Add(10 * time.Second)
	var dc *client.Client
	for time.Now().Before(deadline) {
		if runtime.GOOS != "windows" {
			if _, err := os.Stat(layout.SocketPath); err != nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		c, err := client.DialPath(layout.SocketPath)
		if err == nil {
			dc = c
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if dc == nil {
		t.Fatalf("daemon never came up; socket=%s; output=%s", layout.SocketPath, daemonOut.String())
	}
	defer dc.Close()

	reply, err := dc.Index(proto.IndexArgs{RepoPath: corpus, JSONStats: true})
	if err != nil {
		t.Fatalf("daemon index rpc failed: %v\noutput: %s", err, daemonOut.String())
	}
	raw := []byte(reply.StatsJSON)
	start := bytes.IndexByte(raw, '{')
	if start < 0 {
		t.Fatalf("no JSON in stats reply. raw=%q daemon=%s", raw, daemonOut.String())
	}
	var stats jsonStats
	if err := json.Unmarshal(raw[start:], &stats); err != nil {
		t.Fatalf("parse json-stats: %v\npayload=%s", err, raw[start:])
	}

	const minEntities = 50
	const minRelationships = 20
	// Synthetic fixtures are isolated single-file samples per language with
	// very few cross-file targets, so the resolver classifies most stubs as
	// bug-extractor / bug-resolver — that is expected for this corpus and
	// is NOT the v1.0 ship-gate measurement (which runs over the public OSS
	// corpus via scripts/verify2/run.sh). The 0.75 ceiling here only catches
	// catastrophic regressions where extraction or resolution collapses.
	const maxBugRate = 0.75 // regression net, NOT the v1.0 ship gate

	if stats.Entities < minEntities {
		t.Errorf("entities=%d, want >= %d", stats.Entities, minEntities)
	}
	if stats.Relationships < minRelationships {
		t.Errorf("relationships=%d, want >= %d", stats.Relationships, minRelationships)
	}
	if stats.BugRate >= maxBugRate {
		t.Errorf("bug_rate=%.4f, want < %.2f (regression net)", stats.BugRate, maxBugRate)
	}
	if len(stats.DispositionCounts) == 0 {
		t.Errorf("disposition_counts empty; resolver classification did not run")
	}

	t.Logf("harness summary: files=%d entities=%d rels=%d bug_rate=%.4f resolution_rate=%.4f dispositions=%s",
		stats.Files, stats.Entities, stats.Relationships, stats.BugRate, stats.ResolutionRate,
		dispositionLine(stats.DispositionCounts))
}

func dispositionLine(m map[string]int) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+itoa(v))
	}
	return strings.Join(parts, ",")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
