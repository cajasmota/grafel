package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/registry"
)

// mockLifecycleService is a minimal net/rpc handler that accepts RemoveRepo
// and DeleteGroup calls from tests without starting the real daemon.
type mockLifecycleService struct {
	removeCalledWith proto.RemoveRepoArgs
	removeReply      proto.RemoveRepoReply
	removeErr        error
	deleteCalledWith proto.DeleteGroupArgs
	deleteReply      proto.DeleteGroupReply
	deleteErr        error
}

func (m *mockLifecycleService) RemoveRepo(args *proto.RemoveRepoArgs, reply *proto.RemoveRepoReply) error {
	m.removeCalledWith = *args
	*reply = m.removeReply
	return m.removeErr
}

func (m *mockLifecycleService) DeleteGroup(args *proto.DeleteGroupArgs, reply *proto.DeleteGroupReply) error {
	m.deleteCalledWith = *args
	*reply = m.deleteReply
	return m.deleteErr
}

// stubLifecycleDaemon starts a minimal JSON-RPC server over a Unix-domain
// socket and returns the socket path. The server responds only to
// RemoveRepo and DeleteGroup.
//
// macOS limits Unix socket paths to 104 characters, so we use os.MkdirTemp
// with a short base-dir rather than t.TempDir (which produces long paths
// under /var/folders/…).
func stubLifecycleDaemon(t *testing.T, svc *mockLifecycleService) string {
	t.Helper()
	// Use /tmp directly to keep the path short (≤104 chars on macOS).
	dir, err := os.MkdirTemp("", "ag-stub-")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srv := rpc.NewServer()
	if err := srv.RegisterName(proto.ServiceName, svc); err != nil {
		t.Fatalf("register: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.ServeCodec(jsonrpc.NewServerCodec(conn))
		}
	}()
	return sock
}

// makeTestRegistryGroup writes a minimal group config and registry entry under
// the given ARCHIGRAPH_HOME so test commands can look up the group.
func makeTestRegistryGroup(t *testing.T, home, group string, slugs ...string) {
	t.Helper()
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, group+".fleet.json")
	cfg := registry.GroupConfig{Name: group}
	cfg.Features.GitHooks = true
	for _, s := range slugs {
		cfg.Repos = append(cfg.Repos, registry.Repo{Slug: s, Path: t.TempDir()})
	}
	if err := registry.SaveGroupConfig(cfgPath, &cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup(group, cfgPath); err != nil {
		t.Fatal(err)
	}
}

// newTestCmd returns a bare cobra.Command that captures output.
func newTestCmd(buf *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	return cmd
}

// TestRemove_JSONOutputShape verifies the --json flag produces the expected
// JSON shape and forwards the correct args to the daemon.
func TestRemove_JSONOutputShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	makeTestRegistryGroup(t, home, "acme", "core", "extras")

	svc := &mockLifecycleService{
		removeReply: proto.RemoveRepoReply{FreedBytes: 5924888},
	}
	sock := stubLifecycleDaemon(t, svc)

	var buf bytes.Buffer
	cmd := newTestCmd(&buf)

	err := runRemoveImpl(cmd, "acme", "core", false, true, true, sock)
	if err != nil {
		t.Fatalf("runRemoveImpl: %v", err)
	}

	var result struct {
		Success bool `json:"success"`
		Removed struct {
			Group string `json:"group"`
			Slug  string `json:"slug"`
		} `json:"removed"`
		FreedBytes int64 `json:"freed_bytes"`
		DurationMS int64 `json:"duration_ms"`
	}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal JSON: %v\nraw: %s", err, buf.String())
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if result.Removed.Group != "acme" {
		t.Errorf("group = %q, want %q", result.Removed.Group, "acme")
	}
	if result.Removed.Slug != "core" {
		t.Errorf("slug = %q, want %q", result.Removed.Slug, "core")
	}
	if result.FreedBytes != 5924888 {
		t.Errorf("freed_bytes = %d, want 5924888", result.FreedBytes)
	}
	if svc.removeCalledWith.Group != "acme" || svc.removeCalledWith.Slug != "core" {
		t.Errorf("daemon saw group=%q slug=%q, want acme/core",
			svc.removeCalledWith.Group, svc.removeCalledWith.Slug)
	}
}

// TestRemove_LastRepoBlockedWhenForced verifies that removing the last repo
// with --force is rejected with a clear error (to avoid orphaned empty groups).
func TestRemove_LastRepoBlockedWhenForced(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	makeTestRegistryGroup(t, home, "solo", "only-repo")

	svc := &mockLifecycleService{}
	sock := stubLifecycleDaemon(t, svc)

	var buf bytes.Buffer
	cmd := newTestCmd(&buf)

	err := runRemoveImpl(cmd, "solo", "only-repo", false, true, false, sock)
	if err == nil {
		t.Fatal("expected error when removing last repo with --force")
	}
	if !strings.Contains(err.Error(), "only one repo") {
		t.Errorf("error = %q, want mention of 'only one repo'", err.Error())
	}
}

// TestRemove_UnknownGroupReturnsClearError verifies that an unknown group is
// caught before the daemon is contacted.
func TestRemove_UnknownGroupReturnsClearError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	svc := &mockLifecycleService{}
	sock := stubLifecycleDaemon(t, svc)

	var buf bytes.Buffer
	cmd := newTestCmd(&buf)

	err := runRemoveImpl(cmd, "ghost", "any-repo", false, true, false, sock)
	if err == nil || !strings.Contains(err.Error(), "unknown group") {
		t.Errorf("expected 'unknown group' error, got %v", err)
	}
}

// TestRemove_KeepCachePropagatedToDaemon verifies --keep-cache is forwarded.
func TestRemove_KeepCachePropagatedToDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	makeTestRegistryGroup(t, home, "duo", "alpha", "beta")

	svc := &mockLifecycleService{}
	sock := stubLifecycleDaemon(t, svc)

	var buf bytes.Buffer
	cmd := newTestCmd(&buf)

	// keepCache=true, force=true (skip prompt), jsonOut=false
	err := runRemoveImpl(cmd, "duo", "alpha", true, true, false, sock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !svc.removeCalledWith.KeepCache {
		t.Error("expected KeepCache=true to be forwarded to daemon")
	}
}

// TestRemove_HumanOutputContainsFreedBytes verifies the non-JSON output
// includes the freed-bytes value.
func TestRemove_HumanOutputContainsFreedBytes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	makeTestRegistryGroup(t, home, "org", "repo-a", "repo-b")

	svc := &mockLifecycleService{
		removeReply: proto.RemoveRepoReply{FreedBytes: 1 * 1024 * 1024},
	}
	sock := stubLifecycleDaemon(t, svc)

	var buf bytes.Buffer
	cmd := newTestCmd(&buf)

	err := runRemoveImpl(cmd, "org", "repo-a", false, true, false, sock)
	if err != nil {
		t.Fatalf("runRemoveImpl: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "removed org/repo-a") {
		t.Errorf("output missing 'removed org/repo-a': %q", out)
	}
	if !strings.Contains(out, "MiB") {
		t.Errorf("output missing MiB suffix: %q", out)
	}
}
