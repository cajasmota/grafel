package main

// selftest.go — `grafel selftest` (#5224)
//
// An in-process acceptance ladder that boots a FULLY ISOLATED daemon against
// an EMBEDDED tiny fixture repo and asserts each layer of the stack, printing a
// per-layer PASS/FAIL summary and exiting non-zero on any failure. It is the
// foundation of the release acceptance gate (epic #5223) and doubles as a user
// health-check.
//
// Isolation contract (never touches the user's ~/.grafel):
//   - A fresh temp dir becomes BOTH the daemon root (GRAFEL_DAEMON_ROOT) and the
//     registry/store home (GRAFEL_HOME), so the registry, the graph store, the
//     socket, the pid file and the logs all live under it.
//   - The dashboard binds 127.0.0.1:0 (OS-assigned free port at bind time);
//     the resolved port is learned via the daemon's OnDashboardListen hook
//     (#5224 — avoids a pick-then-rebind race that flaked on Windows CI).
//   - The Layer-1 self-defense conflict check is disabled for the run
//     (GRAFEL_DISABLE_SELFDEFENSE) so the isolated daemon boots even when a
//     canonical user daemon is already running.
//   - On teardown the temp root is removed and we assert no leftover socket.
//
// The fixture is embedded via //go:embed and materialised + git-init'd into the
// temp root at runtime, so the binary is self-contained and the test is
// deterministic and cross-platform (pure Go, no bash-isms).
//
// Layers (1–6 implemented):
//  1. daemon ready  — boot the isolated in-proc daemon; poll socket + dashboard.
//  2. MCP           — tools/list non-empty incl. grafel_stats; one real call returns data.
//  3. cold index    — index the fixture; assert known entities + a known CALLS edge.
//  4. incremental   — append a function → reindex → assert it APPEARS (and the pass
//                     was fast, not a full cold rebuild) → delete it → assert GONE.
//  5. persistence   — stop+restart the isolated daemon → assert the graph is still
//                     queryable from the store with NO reindex.
//  6. teardown      — clean shutdown; remove the isolated root; assert no leftovers.

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	dclient "github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/transport"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// selftestFixture embeds the tiny multi-language fixture repo. The files live
// under selftestdata/fixture/ and are materialised to a temp dir at runtime.
//
//go:embed selftestdata/fixture/*
var selftestFixture embed.FS

const (
	selftestGroupName = "grafel-selftest"
	selftestRepoSlug  = "fixture"
	// Name of the function appended/removed during the incremental layer.
	selftestReindexFn = "AddedByReindex"
)

// layerResult records the outcome of one acceptance layer.
type layerResult struct {
	name   string
	pass   bool
	detail string
	took   time.Duration
}

// selftestEnv holds the resolved isolation knobs + paths for one run.
type selftestEnv struct {
	root    string // temp daemon root == GRAFEL_HOME == GRAFEL_DAEMON_ROOT
	repoDir string // materialised fixture working tree (a git repo)
	layout  daemon.Layout

	// dashPort is the dashboard port. We bind 127.0.0.1:0 and let the OS
	// pick a free port AT BIND TIME, then learn the resolved port via the
	// daemon's OnDashboardListen hook (stored atomically below). This kills
	// the pick-then-close-then-rebind race that flaked on Windows CI (#5224).
	dashPort atomic.Int64
	// dashErr captures the dashboard goroutine's bind/serve error (if any) so
	// a readiness timeout can report the REAL reason instead of an opaque
	// dashboard=false (#5224, Part B).
	dashErr atomic.Pointer[error]
}

// runSelftest is the `grafel selftest` entrypoint. It returns a process exit
// code (0 = all implemented layers PASS).
func runSelftest(argv []string) int {
	keepRoot := false
	for _, a := range argv {
		if a == "--keep-root" || a == "-keep-root" {
			keepRoot = true
		}
	}

	fmt.Fprintln(os.Stdout, "grafel selftest — release acceptance ladder (#5224)")

	env, err := setupSelftestEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "selftest: setup failed: %v\n", err)
		return 1
	}
	if keepRoot {
		fmt.Fprintf(os.Stdout, "selftest: keeping isolated root %s\n", env.root)
	}

	var results []layerResult
	add := func(r layerResult) { results = append(results, r) }

	// Boot the isolated daemon (Layer 1 measures readiness; the daemon must be
	// up for the later layers, so a failure here aborts the ladder).
	ctx, cancel := context.WithCancel(context.Background())
	daemonDone := make(chan error, 1)
	startDaemon := func() {
		cfg := selftestDaemonConfig(env)
		go func() { daemonDone <- daemon.Run(ctx, cfg) }()
	}
	startDaemon()

	// ── Layer 1: daemon ready ─────────────────────────────────────────────────
	r1 := timeLayer("daemon ready", func() (string, error) {
		return selftestWaitReady(env, selftestReadyTimeout())
	})
	add(r1)

	if r1.pass {
		// ── Layer 2: MCP ──────────────────────────────────────────────────────
		add(timeLayer("MCP", func() (string, error) {
			return selftestCheckMCP(env)
		}))

		// ── Layer 3: cold index ───────────────────────────────────────────────
		add(timeLayer("cold index", func() (string, error) {
			return selftestColdIndex(env)
		}))

		// ── Layer 4: incremental reindex ──────────────────────────────────────
		add(timeLayer("incremental reindex", func() (string, error) {
			return selftestIncremental(env)
		}))
	}

	// ── Layer 5: persistence (stop + restart, no reindex) ─────────────────────
	if r1.pass {
		add(timeLayer("persistence", func() (string, error) {
			// Stop the running daemon.
			if err := selftestStopDaemon(env, cancel, daemonDone); err != nil {
				return "", fmt.Errorf("stop daemon: %w", err)
			}
			// Restart a fresh daemon against the SAME isolated root. Reset the
			// resolved-dashboard-port latch so selftestWaitReady waits for the
			// NEW daemon's OnDashboardListen rather than probing the stale port
			// from the first boot (#5224).
			env.dashPort.Store(0)
			env.dashErr.Store(nil)
			ctx, cancel = context.WithCancel(context.Background())
			daemonDone = make(chan error, 1)
			startDaemon()
			if _, err := selftestWaitReady(env, selftestReadyTimeout()); err != nil {
				return "", fmt.Errorf("daemon did not re-become ready: %w", err)
			}
			return selftestPersistence(env)
		}))
	}

	// ── Layer 6: teardown ─────────────────────────────────────────────────────
	add(timeLayer("teardown", func() (string, error) {
		return selftestTeardown(env, cancel, daemonDone, keepRoot)
	}))

	return printSelftestSummary(results)
}

// setupSelftestEnv creates the isolated root, materialises + git-inits the
// fixture, registers the selftest group, picks a free dashboard port, and sets
// the isolation env vars. All env vars are set before any package reads them.
func setupSelftestEnv() (*selftestEnv, error) {
	root, err := os.MkdirTemp("", "grafel-selftest-*")
	if err != nil {
		return nil, fmt.Errorf("mktemp root: %w", err)
	}

	// Isolation: point registry/store home AND daemon root at the temp dir, and
	// disable the Layer-1 self-defense conflict check for the run.
	mustSetenv := func(k, v string) error {
		if err := os.Setenv(k, v); err != nil {
			return fmt.Errorf("setenv %s: %w", k, err)
		}
		return nil
	}
	grafelHome := filepath.Join(root, ".grafel")
	if err := os.MkdirAll(grafelHome, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir grafel home: %w", err)
	}
	envs := map[string]string{
		"GRAFEL_HOME":                grafelHome,
		daemon.EnvRoot:               grafelHome, // GRAFEL_DAEMON_ROOT
		daemon.EnvDisableSelfDefense: "1",
		// Keep the in-process daemon lean + deterministic for CI.
		"GRAFEL_REBUILD_CONCURRENCY": "1",
		// Point the OS home at the temp root too. This is required because a
		// few code paths (notably the MCP server's defaultRegistryPath and the
		// XDG-less ConfigDir fallback) resolve via os.UserHomeDir() rather than
		// GRAFEL_HOME — so without redirecting HOME the registry the selftest
		// writes under GRAFEL_HOME would be invisible to the MCP server, which
		// would report "no groups registered". Setting HOME makes both
		// resolvers agree on <root> and keeps the run fully isolated.
		"HOME": root, // Unix
		// Ensure XDG config (fleet files) also lands under the isolated root so
		// ConfigPathFor writes where the daemon/registry expect it.
		"XDG_CONFIG_HOME": filepath.Join(root, ".config"),
	}
	if runtimeIsWindows() {
		envs["USERPROFILE"] = root // os.UserHomeDir() on Windows
	}
	for k, v := range envs {
		if err := mustSetenv(k, v); err != nil {
			return nil, err
		}
	}

	// Materialise the embedded fixture into <root>/repo and git-init it.
	repoDir := filepath.Join(root, "repo")
	if err := materializeFixture(repoDir); err != nil {
		return nil, fmt.Errorf("materialize fixture: %w", err)
	}
	if err := gitInitFixture(repoDir); err != nil {
		return nil, fmt.Errorf("git-init fixture: %w", err)
	}

	// Register the selftest group pointing at the fixture so MCP CWD routing
	// resolves queries to it.
	if err := registerSelftestGroup(repoDir); err != nil {
		return nil, fmt.Errorf("register group: %w", err)
	}

	// NOTE: the dashboard port is NOT pre-picked here. The old approach bound
	// 127.0.0.1:0, read the port, CLOSED the socket, then handed the number to
	// the daemon to RE-BIND — which races on Windows (TIME_WAIT / ephemeral
	// exclusion) and made the daemon's net.Listen fail silently, so the
	// readiness probe saw dashboard=false forever. Instead we pass
	// DashboardPort=0 to the daemon (it binds 127.0.0.1:0 ONCE, at the moment
	// it serves) and learn the resolved port via OnDashboardListen (#5224).

	layout, err := daemon.DefaultLayout()
	if err != nil {
		return nil, fmt.Errorf("resolve layout: %w", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		return nil, fmt.Errorf("ensure layout: %w", err)
	}

	return &selftestEnv{root: root, repoDir: repoDir, layout: layout}, nil
}

// materializeFixture writes every embedded fixture file into destDir.
func materializeFixture(destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	const prefix = "selftestdata/fixture"
	return fs.WalkDir(selftestFixture, prefix, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(prefix, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := selftestFixture.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// gitInitFixture turns the materialised fixture into a committed git repo so
// the indexer's gitmeta capture resolves a ref (the store path is keyed by ref).
func gitInitFixture(repoDir string) error {
	run := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		// Deterministic identity + no GPG/hooks/global config interference.
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=grafel-selftest", "GIT_AUTHOR_EMAIL=selftest@grafel.dev",
			"GIT_COMMITTER_NAME=grafel-selftest", "GIT_COMMITTER_EMAIL=selftest@grafel.dev",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := run("init", "-q", "-b", "main"); err != nil {
		// Older git lacks `-b`; fall back to a plain init + branch rename.
		if err2 := run("init", "-q"); err2 != nil {
			return err
		}
		_ = run("checkout", "-q", "-b", "main")
	}
	if err := run("add", "-A"); err != nil {
		return err
	}
	return run("commit", "-q", "-m", "grafel selftest fixture")
}

// registerSelftestGroup writes a fleet config for the fixture and registers it.
func registerSelftestGroup(repoDir string) error {
	cfgPath, err := registry.ConfigPathFor(selftestGroupName)
	if err != nil {
		return err
	}
	cfg := &registry.GroupConfig{
		Name: selftestGroupName,
		Repos: []registry.Repo{
			{Slug: selftestRepoSlug, Path: repoDir, Stack: registry.StackList{"go", "python"}},
		},
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		return err
	}
	return registry.AddGroup(selftestGroupName, cfgPath)
}

// selftestDaemonConfig builds the full daemon.Config with the REAL MCP + Index
// wiring (the same function values the production daemon uses), minus the
// background sweepers/watchers we don't need for a short-lived acceptance run.
func selftestDaemonConfig(env *selftestEnv) daemon.Config {
	logger := buildDaemonSlogLogger(os.Stdout)
	return daemon.Config{
		Layout:         env.layout,
		Logger:         logger,
		Index:          daemonIndexFunc,
		Rebuild:        makeDaemonRebuildFunc(1),
		MCPListTools:   daemonMCPListTools,
		MCPCallTool:    daemonMCPCallTool,
		DashboardServe: makeDaemonDashboardServe(time.Now()),
		// 0 = let the OS pick a free port AT BIND TIME (no pick-then-rebind
		// race). The resolved port arrives via OnDashboardListen (#5224).
		DashboardPort: 0,
		DashboardBind: "127.0.0.1",
		OnDashboardListen: func(addr string) {
			// addr is "127.0.0.1:<port>"; extract and store the port.
			if _, portStr, err := net.SplitHostPort(addr); err == nil {
				if p, perr := strconv.Atoi(portStr); perr == nil {
					env.dashPort.Store(int64(p))
				}
			}
		},
		OnDashboardError: func(err error) {
			if err != nil {
				env.dashErr.Store(&err)
			}
		},
	}
}

// selftestReadyTimeout resolves the daemon-ready probe budget. Cold CGO
// startup on Windows CI legitimately exceeds the old hard-coded 30s, so the
// default is generous and overridable via GRAFEL_SELFTEST_READY_TIMEOUT_SEC.
func selftestReadyTimeout() time.Duration {
	if v := os.Getenv("GRAFEL_SELFTEST_READY_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 60 * time.Second
}

// selftestWaitReady polls the daemon socket (RPC Ping) and the dashboard HTTP
// listener until both answer or the timeout elapses.
func selftestWaitReady(env *selftestEnv, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	socketReady := false
	dashReady := false
	dashURL := ""
	httpClient := &http.Client{Timeout: 1 * time.Second}

	for time.Now().Before(deadline) {
		if !socketReady {
			if c, err := dclient.DialPath(env.layout.SocketPath); err == nil {
				if _, perr := c.Ping(); perr == nil {
					socketReady = true
				}
				_ = c.Close()
			}
		}
		if !dashReady {
			// The dashboard port is OS-assigned at bind time and reported via
			// OnDashboardListen (#5224). Until that fires, port is 0 and we
			// can't probe yet — just keep polling.
			if p := env.dashPort.Load(); p > 0 {
				dashURL = fmt.Sprintf("http://127.0.0.1:%d/", p)
				if resp, err := httpClient.Get(dashURL); err == nil {
					_ = resp.Body.Close()
					dashReady = true
				}
			}
		}
		if socketReady && dashReady {
			return fmt.Sprintf("socket=%s dashboard=%s", env.layout.SocketPath, dashURL), nil
		}
		// (#5264) The Windows-only dashboard-readiness skip is removed: the
		// Windows acceptance run confirmed the isolated selftest daemon now
		// binds the dashboard (`daemon ready ... dashboard=http://127.0.0.1:…/`),
		// so Layer-1 requires a real dashboard URL on every OS — no skip branch.
		time.Sleep(100 * time.Millisecond)
	}
	// Surface the dashboard goroutine's real bind/serve error (if any) so a
	// future failure (e.g. on Windows CI) shows the reason, not just
	// dashboard=false (#5224, Part B).
	if !dashReady {
		if ep := env.dashErr.Load(); ep != nil && *ep != nil {
			return "", fmt.Errorf("not ready within %s (socket=%v dashboard=%v: %v)", timeout, socketReady, dashReady, *ep)
		}
	}
	return "", fmt.Errorf("not ready within %s (socket=%v dashboard=%v)", timeout, socketReady, dashReady)
}

// selftestCheckMCP asserts tools/list is non-empty (incl. grafel_stats) and a
// real grafel_stats call returns a JSON-parseable payload.
func selftestCheckMCP(env *selftestEnv) (string, error) {
	c, err := dclient.DialPath(env.layout.SocketPath)
	if err != nil {
		return "", fmt.Errorf("dial: %w", err)
	}
	defer c.Close()

	// tools/list — must be non-empty. NOTE: the cwd-gate (#1769) intentionally
	// returns just the sentinel tool when a group resolves but has 0 repos
	// loaded into the graph (which is the case here, BEFORE the cold-index
	// layer runs). So at this point either the full catalog (incl. grafel_stats)
	// or the sentinel-only list is acceptable — the contract is "non-empty".
	var listReply daemon.MCPToolListReply
	if err := callRPC(env, "Daemon.MCPToolList", &daemon.MCPToolListArgs{CWD: env.repoDir}, &listReply); err != nil {
		return "", fmt.Errorf("MCPToolList: %w", err)
	}
	if len(listReply.Tools) == 0 {
		return "", fmt.Errorf("tools/list returned empty catalog")
	}

	// One real, NON-gated tool call must round-trip through the dispatcher and
	// return content. MCPToolCall is not cwd-gated, so grafel_stats dispatches
	// regardless of whether the catalog above was sentinel-trimmed. We only
	// assert it RESPONDS with a content block (entities may be 0 until Layer 3).
	var callReply daemon.MCPToolCallReply
	args := &daemon.MCPToolCallArgs{Name: "grafel_stats", Arguments: map[string]any{}, CWD: env.repoDir}
	if err := callRPC(env, "Daemon.MCPToolCall", args, &callReply); err != nil {
		return "", fmt.Errorf("MCPToolCall grafel_stats: %w", err)
	}
	if callReply.IsError {
		return "", fmt.Errorf("grafel_stats returned an error result: %v", callReply.Content)
	}
	if len(callReply.Content) == 0 {
		return "", fmt.Errorf("grafel_stats returned no content")
	}
	return fmt.Sprintf("tools=%d (cwd-gated); grafel_stats dispatched + returned content", len(listReply.Tools)), nil
}

// selftestColdIndex runs a cold index of the fixture and asserts the known
// entities + a known CALLS edge exist.
func selftestColdIndex(env *selftestEnv) (string, error) {
	if err := selftestIndexRPC(env, false); err != nil {
		return "", fmt.Errorf("index RPC: %w", err)
	}
	doc, err := selftestLoadGraph(env)
	if err != nil {
		return "", err
	}

	// Known entities from the embedded fixture.
	if !hasEntityNamed(doc, "Greet") {
		return "", fmt.Errorf("expected entity Greet not found (entities=%d)", len(doc.Entities))
	}
	if !hasEntityNamed(doc, "RunGreeting") {
		return "", fmt.Errorf("expected entity RunGreeting not found (entities=%d)", len(doc.Entities))
	}
	// Known CALLS edge RunGreeting -> Greet.
	if !hasCallEdge(doc, "RunGreeting", "Greet") {
		return "", fmt.Errorf("expected CALLS edge RunGreeting->Greet not found (rels=%d)", len(doc.Relationships))
	}

	// Cross-check against the live MCP grafel_stats path. The store load above
	// is the authoritative assertion; this corroborates the query surface sees
	// the freshly-indexed graph. The MCP server keeps its own mtime-guarded
	// graph cache (1s granularity), so we poll briefly to let it pick up the
	// just-written graph.fb before reporting — without failing the layer on a
	// cache lag (the on-disk graph is already proven correct).
	mcpNote := selftestStatsEntitiesWithRetry(env, 3*time.Second)

	return fmt.Sprintf("entities=%d rels=%d; Greet+RunGreeting present; CALLS edge present; %s",
		len(doc.Entities), len(doc.Relationships), mcpNote), nil
}

// selftestIncremental appends a new function, reindexes, asserts the new entity
// APPEARS and the pass was fast (not a full cold rebuild), then deletes it and
// asserts it is GONE.
func selftestIncremental(env *selftestEnv) (string, error) {
	mainGo := filepath.Join(env.repoDir, "main.go")
	orig, err := os.ReadFile(mainGo)
	if err != nil {
		return "", fmt.Errorf("read main.go: %w", err)
	}

	// Append a new top-level function.
	appended := string(orig) + fmt.Sprintf("\n// %s is appended by the selftest incremental layer.\nfunc %s() int { return 42 }\n",
		selftestReindexFn, selftestReindexFn)
	if err := os.WriteFile(mainGo, []byte(appended), 0o644); err != nil {
		return "", fmt.Errorf("append fn: %w", err)
	}

	// Reindex and time it (incremental should be fast — well under the cold
	// rebuild budget on a fixture this size).
	t0 := time.Now()
	if err := selftestIndexRPC(env, false); err != nil {
		_ = os.WriteFile(mainGo, orig, 0o644)
		return "", fmt.Errorf("reindex after append: %w", err)
	}
	reindexTook := time.Since(t0)

	doc, err := selftestLoadGraph(env)
	if err != nil {
		return "", err
	}
	if !hasEntityNamed(doc, selftestReindexFn) {
		_ = os.WriteFile(mainGo, orig, 0o644)
		return "", fmt.Errorf("appended fn %s did NOT appear after reindex", selftestReindexFn)
	}
	// Fast-pass guard: on a 2-file fixture a reindex must complete well within
	// a generous bound; a multi-second pass would indicate a pathological full
	// rebuild. This is a soft signal (logged in detail), not a hard cold-vs-
	// incremental classifier — the graph store is shared, so we assert by wall
	// time that the pass did not balloon.
	const fastBudget = 20 * time.Second
	if reindexTook > fastBudget {
		_ = os.WriteFile(mainGo, orig, 0o644)
		return "", fmt.Errorf("reindex took %s (> %s fast budget) — not behaving incrementally", reindexTook, fastBudget)
	}

	// Now delete the function (restore original) and reindex → assert GONE.
	if err := os.WriteFile(mainGo, orig, 0o644); err != nil {
		return "", fmt.Errorf("restore main.go: %w", err)
	}
	if err := selftestIndexRPC(env, false); err != nil {
		return "", fmt.Errorf("reindex after delete: %w", err)
	}
	doc2, err := selftestLoadGraph(env)
	if err != nil {
		return "", err
	}
	if hasEntityNamed(doc2, selftestReindexFn) {
		return "", fmt.Errorf("deleted fn %s still present after reindex", selftestReindexFn)
	}

	return fmt.Sprintf("appended %s appeared (reindex %s) then deletion removed it",
		selftestReindexFn, reindexTook.Truncate(time.Millisecond)), nil
}

// selftestPersistence asserts the graph is queryable after a daemon restart
// with NO reindex (the store was loaded from disk).
func selftestPersistence(env *selftestEnv) (string, error) {
	// The restarted daemon must serve grafel_stats with the previously-indexed
	// entities WITHOUT us issuing any index call.
	c, err := dclient.DialPath(env.layout.SocketPath)
	if err != nil {
		return "", fmt.Errorf("dial after restart: %w", err)
	}
	defer c.Close()

	// Authoritative proof: the graph artifact persisted on disk and is loadable
	// by the restarted process WITHOUT any reindex call having been issued.
	doc, err := selftestLoadGraph(env)
	if err != nil {
		return "", fmt.Errorf("load graph from store after restart: %w", err)
	}
	if !hasEntityNamed(doc, "Greet") {
		return "", fmt.Errorf("Greet missing after restart — persistence broken")
	}

	// Corroborate via the live MCP path: the restarted daemon must serve
	// grafel_stats from the on-disk store via lazy reload. Best-effort — the
	// store load above is authoritative.
	mcpNote := selftestStatsEntitiesWithRetry(env, 3*time.Second)

	return fmt.Sprintf("graph reloaded from store after restart (%d entities, no reindex); %s",
		len(doc.Entities), mcpNote), nil
}

// selftestTeardown stops the daemon, removes the isolated root, and asserts no
// leftover socket file remains.
func selftestTeardown(env *selftestEnv, cancel context.CancelFunc, done chan error, keepRoot bool) (string, error) {
	if err := selftestStopDaemon(env, cancel, done); err != nil {
		return "", fmt.Errorf("stop: %w", err)
	}
	// Assert no leftover socket (Unix only; named pipes are not files).
	if env.layout.SocketPath != "" && !strings.HasPrefix(env.layout.SocketPath, `\\.\pipe\`) {
		if _, err := os.Stat(env.layout.SocketPath); err == nil {
			return "", fmt.Errorf("socket file still present after shutdown: %s", env.layout.SocketPath)
		}
	}
	if !keepRoot {
		if err := removeAllWithRetry(env.root); err != nil {
			return "", fmt.Errorf("remove isolated root: %w", err)
		}
	}
	return "clean shutdown; isolated root removed; no leftover socket", nil
}

// removeAllWithRetry is os.RemoveAll with a short bounded backoff. On Windows a
// file cannot be unlinked while any process holds an open handle, and handles
// are released slightly asynchronously after the owning goroutine exits — so a
// single RemoveAll can race a not-quite-closed handle (e.g. the MCP activity
// log; see #5264) and fail with "being used by another process". Retrying for a
// short window tolerates that brief lag. On Unix the first attempt almost always
// succeeds (open files can be unlinked), so the retry loop costs nothing.
func removeAllWithRetry(path string) error {
	const attempts = 10
	var err error
	for i := 0; i < attempts; i++ {
		if err = os.RemoveAll(path); err == nil {
			return nil
		}
		// Confirm it's really gone (RemoveAll can report an error yet still have
		// removed the tree on some platforms); bail out cleanly if so.
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return err
}

// ── helpers ──────────────────────────────────────────────────────────────────

// selftestStopDaemon asks the daemon to stop (best-effort RPC), cancels the
// context, and waits for daemon.Run to return.
func selftestStopDaemon(env *selftestEnv, cancel context.CancelFunc, done chan error) error {
	if c, err := dclient.DialPath(env.layout.SocketPath); err == nil {
		_ = c.Stop()
		_ = c.Close()
	}
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		return fmt.Errorf("daemon did not exit within 15s")
	}
	return nil
}

// selftestIndexRPC issues a synchronous Index RPC for the fixture repo.
func selftestIndexRPC(env *selftestEnv, async bool) error {
	c, err := dclient.DialPath(env.layout.SocketPath)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.Index(proto.IndexArgs{
		RepoPath: env.repoDir,
		RepoTag:  selftestRepoSlug,
		Async:    async,
	})
	return err
}

// selftestLoadGraph loads the per-repo graph Document from the isolated store.
func selftestLoadGraph(env *selftestEnv) (*graph.Document, error) {
	dir := daemon.StateDirForRepo(env.repoDir)
	if dir == "" {
		return nil, fmt.Errorf("could not resolve state dir for %s", env.repoDir)
	}
	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		return nil, fmt.Errorf("load graph from %s: %w", dir, err)
	}
	if doc == nil {
		return nil, fmt.Errorf("nil graph document from %s", dir)
	}
	return doc, nil
}

// callRPC dials the socket, makes a single net/rpc call, and closes.
func callRPC(env *selftestEnv, method string, args, reply any) error {
	conn, err := transport.DialTimeout(env.layout.SocketPath, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	cl := rpc.NewClientWithCodec(jsonrpc.NewClientCodec(conn))
	defer cl.Close()
	return cl.Call(method, args, reply)
}

// hasEntityNamed reports whether any entity has the given Name.
func hasEntityNamed(doc *graph.Document, name string) bool {
	for i := range doc.Entities {
		if doc.Entities[i].Name == name {
			return true
		}
	}
	return false
}

// hasCallEdge reports whether a CALLS edge from an entity named fromName to one
// named toName exists.
func hasCallEdge(doc *graph.Document, fromName, toName string) bool {
	idByName := map[string][]string{}
	nameByID := map[string]string{}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		idByName[e.Name] = append(idByName[e.Name], e.ID)
		nameByID[e.ID] = e.Name
	}
	fromIDs := map[string]bool{}
	for _, id := range idByName[fromName] {
		fromIDs[id] = true
	}
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if !strings.EqualFold(r.Kind, "CALLS") {
			continue
		}
		if fromIDs[r.FromID] && nameByID[r.ToID] == toName {
			return true
		}
	}
	return false
}

// statsEntities extracts the "entities" total from a grafel_stats MCP reply.
// Returns (entities, rawJSON, error).
func statsEntities(reply daemon.MCPToolCallReply) (int, string, error) {
	if len(reply.Content) == 0 {
		return 0, "", fmt.Errorf("grafel_stats: empty content")
	}
	text, _ := reply.Content[0]["text"].(string)
	if text == "" {
		return 0, "", fmt.Errorf("grafel_stats: empty text block")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return 0, text, fmt.Errorf("grafel_stats: content not JSON: %w", err)
	}
	// The handler nests entities under "totals" (totals["entities"]). Accept a
	// top-level "entities" too for robustness across handler revisions.
	if t, ok := payload["totals"].(map[string]any); ok {
		if n, ok := numFrom(t["entities"]); ok {
			return n, text, nil
		}
	}
	if n, ok := numFrom(payload["entities"]); ok {
		return n, text, nil
	}
	// No entities key — treat as 0 (valid for a pre-index stats call).
	return 0, text, nil
}

func numFrom(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

// runtimeIsWindows reports whether the host OS is Windows (for HOME/USERPROFILE).
func runtimeIsWindows() bool { return runtime.GOOS == "windows" }

// selftestStatsEntitiesWithRetry polls grafel_stats via MCP until it reports
// entities>0 (or the timeout elapses), returning a human note. Best-effort: the
// on-disk store load is the authoritative persistence/index assertion, so this
// never fails a layer — it only annotates whether the live query surface had
// caught up with the freshly-written graph (its mtime-guarded cache has ~1s
// granularity).
func selftestStatsEntitiesWithRetry(env *selftestEnv, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	last := "MCP entities=0 (store-authoritative)"
	for time.Now().Before(deadline) {
		var callReply daemon.MCPToolCallReply
		args := &daemon.MCPToolCallArgs{Name: "grafel_stats", Arguments: map[string]any{}, CWD: env.repoDir}
		if err := callRPC(env, "Daemon.MCPToolCall", args, &callReply); err == nil && !callReply.IsError {
			if n, raw, serr := statsEntities(callReply); serr == nil {
				if n > 0 {
					return fmt.Sprintf("MCP grafel_stats served %d entities", n)
				}
				last = "MCP entities=0 (store-authoritative)"
			} else {
				last = fmt.Sprintf("MCP non-JSON (store-authoritative): %.40q", raw)
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return last
}

// timeLayer runs fn, records wall time, and builds a layerResult.
func timeLayer(name string, fn func() (string, error)) layerResult {
	t0 := time.Now()
	detail, err := fn()
	took := time.Since(t0)
	if err != nil {
		return layerResult{name: name, pass: false, detail: err.Error(), took: took}
	}
	return layerResult{name: name, pass: true, detail: detail, took: took}
}

// printSelftestSummary prints the per-layer PASS/FAIL table and returns the
// process exit code (0 = all PASS).
func printSelftestSummary(results []layerResult) int {
	fmt.Fprintln(os.Stdout, "\n── grafel selftest summary ───────────────────────────────")
	allPass := true
	for _, r := range results {
		status := "PASS"
		if !r.pass {
			status = "FAIL"
			allPass = false
		}
		fmt.Fprintf(os.Stdout, "  [%s] %-22s %8s  %s\n",
			status, r.name, r.took.Truncate(time.Millisecond), r.detail)
	}
	fmt.Fprintln(os.Stdout, "──────────────────────────────────────────────────────────")
	if allPass {
		fmt.Fprintln(os.Stdout, "grafel selftest: ALL LAYERS PASS")
		return 0
	}
	fmt.Fprintln(os.Stdout, "grafel selftest: FAILURES DETECTED")
	return 1
}
