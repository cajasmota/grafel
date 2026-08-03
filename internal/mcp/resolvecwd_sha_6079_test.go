// resolvecwd_sha_6079_test.go — issue #6079, at the consumer the issue names.
//
// The gitmeta-level regression lives in internal/gitmeta/cache_sha_6079_test.go.
// This one drives the actual serving surface: ResolveCWD is what grafel_whoami
// reports indexed_sha from, so a stale gitmeta memo becomes a confidently wrong
// commit id in an MCP response.
package mcp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
)

// TestResolveCWD_SHAIsFreshAfterSameBranchCommit: commit on the branch you are
// already on, then ask again. The reported SHA must be the new commit.
func TestResolveCWD_SHAIsFreshAfterSameBranchCommit(t *testing.T) {
	t.Setenv(daemon.EnvRoot, t.TempDir())
	// #6101: never read the developer's real state directory.
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repoDir := gitRepoForDiscovery(t)
	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	t.Cleanup(st.Close)

	first := ResolveCWD(st, repoDir)
	if first.SHA == "" {
		t.Fatalf("fixture degenerate: no SHA resolved (source=%q group=%q)", first.Source, first.Group)
	}
	if first.Ref != "main" {
		t.Fatalf("fixture degenerate: Ref=%q, want main", first.Ref)
	}

	// A plain same-branch commit: refs/heads/main moves, HEAD does not.
	//
	// The non-vacuity guard MUST straddle the commit. An earlier version of this
	// test read the HEAD bytes AFTER committing and compared them to a re-read
	// after ResolveCWD, which only proved HEAD stayed put across the lookup —
	// inert, since nothing in that window could have moved it. What has to be
	// asserted is that the COMMIT left the HEAD pointer untouched, because that is
	// the whole premise of #6079.
	checkHead := assertHeadPointerUnmoved(t, repoDir)
	wantSHA := commitOnBranch(t, repoDir, "second")
	checkHead()

	if wantSHA == "" || wantSHA == first.SHA {
		t.Fatalf("fixture degenerate: SHA did not advance (%q -> %q)", first.SHA, wantSHA)
	}

	got := ResolveCWD(st, repoDir)
	if got.Ref != "main" {
		t.Fatalf("vacuous: Ref moved to %q — the pre-existing key would have caught that", got.Ref)
	}
	if got.SHA != wantSHA {
		t.Errorf("grafel_whoami would report a stale commit after a same-branch commit:\n got  %q\n want %q",
			got.SHA, wantSHA)
	}
}
