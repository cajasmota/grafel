//go:build unix

package secrets

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestScanPathDoesNotBlockOnFifo is the third liveness probe for #6416, and the
// most exposed of them.
//
// ScanPath is a SECOND, independent filepath.WalkDir that also branched only on
// d.IsDir(), then handed the path to scanFile → os.Open. The size guard could
// not help: d.Info().Size() is 0 for a FIFO, so it passes, and skipFile is only
// an extension denylist. A FIFO named `creds.go` therefore blocked the scan
// forever.
//
// It is reachable from internal/mcp/secrets_tools.go (the live daemon's MCP
// tool) and internal/dashboard/handlers_secrets.go (an HTTP handler), so a
// caller who never touches the indexer can wedge a daemon goroutine.
func TestScanPathDoesNotBlockOnFifo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "creds.go"), 0o644); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_, _ = ScanPath(root, 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ScanPath blocked on a fifo named creds.go; the scan must not open a non-regular file")
	}
}

// The key is a syntactically valid AKIA id and deliberately NOT
// AKIAIOSFODNN7EXAMPLE: that one is the AWS documentation key, which secrets.go
// suppresses by name, so a fixture built on it would assert nothing.
//
// TestScanPathStillScansSymlinkedFile keeps the symlink decision honest on this
// path too: rejecting every symlink would close the hang by losing findings.
func TestScanPathStillScansSymlinkedFile(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real_config.go")
	if err := os.WriteFile(real, []byte("package p\n\nconst k = \"AKIA3QW7ZP2LMXR8VTKD\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "linked.go")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	scanRes, err := ScanPath(root, 0)
	findings := scanRes.Findings
	if err != nil {
		t.Fatal(err)
	}
	var sawLink bool
	for _, f := range findings {
		if filepath.Base(f.File) == "linked.go" {
			sawLink = true
		}
	}
	if !sawLink {
		t.Errorf("symlinked source file was not scanned; findings=%+v", findings)
	}
}
