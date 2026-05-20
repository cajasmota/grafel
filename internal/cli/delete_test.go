package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon/proto"
)

// TestDelete_JSONOutputShape verifies the --json flag produces the expected
// shape and forwards the correct args to the daemon.
func TestDelete_JSONOutputShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	makeTestRegistryGroup(t, home, "bigcorp", "svc-a", "svc-b", "svc-c")

	svc := &mockLifecycleService{
		deleteReply: proto.DeleteGroupReply{
			RemovedRepos: []string{"svc-a", "svc-b", "svc-c"},
			FreedBytes:   23456789,
		},
	}
	sock := stubLifecycleDaemon(t, svc)

	var buf bytes.Buffer
	cmd := newTestCmd(&buf)

	err := runDeleteImpl(cmd, "bigcorp", true, false, true, sock)
	if err != nil {
		t.Fatalf("runDeleteImpl: %v", err)
	}

	var result struct {
		Success      bool     `json:"success"`
		Deleted      string   `json:"deleted"`
		RemovedRepos []string `json:"removed_repos"`
		FreedBytes   int64    `json:"freed_bytes"`
		DurationMS   int64    `json:"duration_ms"`
	}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal JSON: %v\nraw: %s", err, buf.String())
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if result.Deleted != "bigcorp" {
		t.Errorf("deleted = %q, want %q", result.Deleted, "bigcorp")
	}
	if len(result.RemovedRepos) != 3 {
		t.Errorf("removed_repos len = %d, want 3", len(result.RemovedRepos))
	}
	if result.FreedBytes != 23456789 {
		t.Errorf("freed_bytes = %d, want 23456789", result.FreedBytes)
	}

	// Verify the daemon received the correct group name.
	if svc.deleteCalledWith.Group != "bigcorp" {
		t.Errorf("daemon.group = %q, want %q", svc.deleteCalledWith.Group, "bigcorp")
	}
}

// TestDelete_UnknownGroupReturnsClearError verifies that an unknown group is
// caught before the daemon is contacted.
func TestDelete_UnknownGroupReturnsClearError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	svc := &mockLifecycleService{}
	sock := stubLifecycleDaemon(t, svc)

	var buf bytes.Buffer
	cmd := newTestCmd(&buf)

	err := runDeleteImpl(cmd, "no-such-group", true, false, false, sock)
	if err == nil || !strings.Contains(err.Error(), "unknown group") {
		t.Errorf("expected 'unknown group' error, got %v", err)
	}
}

// TestDelete_KeepCachesPropagatedToDaemon verifies --keep-caches is forwarded.
func TestDelete_KeepCachesPropagatedToDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	makeTestRegistryGroup(t, home, "preserve", "r1", "r2")

	svc := &mockLifecycleService{}
	sock := stubLifecycleDaemon(t, svc)

	var buf bytes.Buffer
	cmd := newTestCmd(&buf)

	err := runDeleteImpl(cmd, "preserve", true, true, false, sock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !svc.deleteCalledWith.KeepCaches {
		t.Error("expected KeepCaches=true to be forwarded to daemon")
	}
}

// TestDelete_JSONRemovedReposNotNull verifies the removed_repos field is never
// null in JSON output (even when the daemon returns an empty slice).
func TestDelete_JSONRemovedReposNotNull(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	makeTestRegistryGroup(t, home, "empty-group", "only")

	svc := &mockLifecycleService{
		// Daemon returns empty removed_repos slice.
		deleteReply: proto.DeleteGroupReply{RemovedRepos: nil},
	}
	sock := stubLifecycleDaemon(t, svc)

	var buf bytes.Buffer
	cmd := newTestCmd(&buf)

	err := runDeleteImpl(cmd, "empty-group", true, false, true, sock)
	if err != nil {
		t.Fatalf("runDeleteImpl: %v", err)
	}

	raw := buf.String()
	// The JSON must not contain "null" for removed_repos.
	if strings.Contains(raw, `"removed_repos": null`) {
		t.Errorf("removed_repos should never be null in JSON output; got: %s", raw)
	}
	// It should be an empty array.
	if !strings.Contains(raw, `"removed_repos": []`) {
		t.Errorf("expected empty array for removed_repos; got: %s", raw)
	}
}

// TestDelete_HumanOutputFormat verifies non-JSON output is readable.
func TestDelete_HumanOutputFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	makeTestRegistryGroup(t, home, "myteam", "api", "frontend")

	svc := &mockLifecycleService{
		deleteReply: proto.DeleteGroupReply{
			RemovedRepos: []string{"api", "frontend"},
			FreedBytes:   2 * 1024 * 1024,
		},
	}
	sock := stubLifecycleDaemon(t, svc)

	var buf bytes.Buffer
	cmd := newTestCmd(&buf)

	err := runDeleteImpl(cmd, "myteam", true, false, false, sock)
	if err != nil {
		t.Fatalf("runDeleteImpl: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `deleted group "myteam"`) {
		t.Errorf("output missing group name: %q", out)
	}
	if !strings.Contains(out, "api") || !strings.Contains(out, "frontend") {
		t.Errorf("output missing repo names: %q", out)
	}
	if !strings.Contains(out, "MiB") {
		t.Errorf("output missing freed bytes: %q", out)
	}
}
