//go:build darwin || linux

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// effectiveEnvRoot returns the value of GRAFEL_DAEMON_ROOT the child process
// would observe from env, honouring exec's last-occurrence-wins semantics.
// The bool reports whether the child sees it set to a NON-EMPTY value (the
// state that flips the on-disk store layout to <root>/state).
func effectiveEnvRoot(env []string) (val string, setNonEmpty bool) {
	prefix := EnvRoot + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			val = strings.TrimPrefix(kv, prefix)
		}
	}
	return val, val != ""
}

// withEnvRoot applies (or clears) GRAFEL_DAEMON_ROOT for the duration of fn,
// then restores it — so we can compute what store root the child WOULD resolve
// under its inherited env, using the real repoBaseDir/requestsRoot/StoreRootBase
// helpers rather than re-deriving the path shape in the test.
func withEnvRoot(t *testing.T, childVal string, setNonEmpty bool, fn func()) {
	t.Helper()
	prev, had := os.LookupEnv(EnvRoot)
	if setNonEmpty {
		_ = os.Setenv(EnvRoot, childVal)
	} else {
		_ = os.Unsetenv(EnvRoot)
	}
	defer func() {
		if had {
			_ = os.Setenv(EnvRoot, prev)
		} else {
			_ = os.Unsetenv(EnvRoot)
		}
	}()
	fn()
}

// TestEngineChildResolvesSameStoreRootAsServe is the production-divergence
// regression (epic #5729, ADR-0024 PR6 blocker). It builds the REAL production
// engine-child command (defaultEngineChildCommand) and asserts the engine child
// resolves the IDENTICAL store root as serve for every root-determining env
// combination:
//
//   - production: serve has NO GRAFEL_DAEMON_ROOT → the child must ALSO see it
//     unset, so both resolve StoreDir() (~/.grafel|$GRAFEL_HOME/store) — NOT the
//     isolated <root>/state layout. Before the fix the supervisor force-set
//     GRAFEL_DAEMON_ROOT=<layout.Root> on the child, flipping it to /state while
//     serve stayed on /store: reindex/rebuild requests were silently dropped and
//     engine-written graph.fb landed where serve never read it.
//   - isolated: serve HAS GRAFEL_DAEMON_ROOT=<tmp> → the child inherits the SAME
//     value → both resolve <tmp>/state.
func TestEngineChildResolvesSameStoreRootAsServe(t *testing.T) {
	// Pin GRAFEL_HOME so StoreDir() is deterministic and isolated from the real
	// ~/.grafel regardless of scenario.
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)

	const repo = "/some/abs/repo/path"

	t.Run("production_no_daemon_root", func(t *testing.T) {
		// Serve has NO GRAFEL_DAEMON_ROOT (launchd/systemd production).
		prev, had := os.LookupEnv(EnvRoot)
		_ = os.Unsetenv(EnvRoot)
		defer func() {
			if had {
				_ = os.Setenv(EnvRoot, prev)
			}
		}()

		// Serve's OWN resolution (production layout → StoreDir()).
		serveRequests := requestsRoot()
		serveRepoBase := repoBaseDir(repo)
		serveStoreBase := StoreRootBase()
		if want := filepath.Join(home, "store"); serveRequests != want {
			t.Fatalf("precondition: serve requestsRoot()=%q, want %q", serveRequests, want)
		}

		// Serve's layout.Root in production is ~/.grafel (a NON-empty path). The
		// old code force-set GRAFEL_DAEMON_ROOT=layout.Root on the child.
		serveLayoutRoot := filepath.Join(home, ".grafel-layout-root")
		cmd := defaultEngineChildCommand("/self/grafel", serveLayoutRoot)

		childVal, childSetNonEmpty := effectiveEnvRoot(cmd.Env)
		if childSetNonEmpty {
			t.Fatalf("production divergence: engine child sees GRAFEL_DAEMON_ROOT=%q "+
				"(serve has it UNSET). This flips the child to <root>/state while serve "+
				"uses StoreDir()/store — a total serve↔engine disconnect.", childVal)
		}

		// The child, under its inherited env, must resolve the SAME store root.
		withEnvRoot(t, childVal, childSetNonEmpty, func() {
			if got := requestsRoot(); got != serveRequests {
				t.Errorf("engine requestsRoot()=%q, want serve's %q", got, serveRequests)
			}
			if got := repoBaseDir(repo); got != serveRepoBase {
				t.Errorf("engine repoBaseDir()=%q, want serve's %q", got, serveRepoBase)
			}
			if got := StoreRootBase(); got != serveStoreBase {
				t.Errorf("engine StoreRootBase()=%q, want serve's %q", got, serveStoreBase)
			}
		})
	})

	t.Run("isolated_with_daemon_root", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv(EnvRoot, root)

		serveRequests := requestsRoot()
		serveRepoBase := repoBaseDir(repo)
		serveStoreBase := StoreRootBase()
		if want := filepath.Join(root, "state"); serveRequests != want {
			t.Fatalf("precondition: isolated serve requestsRoot()=%q, want %q", serveRequests, want)
		}

		cmd := defaultEngineChildCommand("/self/grafel", root)
		childVal, childSetNonEmpty := effectiveEnvRoot(cmd.Env)
		if !childSetNonEmpty || childVal != root {
			t.Fatalf("isolated mode: engine child must inherit GRAFEL_DAEMON_ROOT=%q, got %q (set=%v)",
				root, childVal, childSetNonEmpty)
		}
		withEnvRoot(t, childVal, childSetNonEmpty, func() {
			if got := requestsRoot(); got != serveRequests {
				t.Errorf("isolated engine requestsRoot()=%q, want serve's %q", got, serveRequests)
			}
			if got := repoBaseDir(repo); got != serveRepoBase {
				t.Errorf("isolated engine repoBaseDir()=%q, want serve's %q", got, serveRepoBase)
			}
			if got := StoreRootBase(); got != serveStoreBase {
				t.Errorf("isolated engine StoreRootBase()=%q, want serve's %q", got, serveStoreBase)
			}
		})
	})
}
