package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/archigraph/internal/agents"
	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/process"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/dashboard"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/jobs"
	"github.com/cajasmota/archigraph/internal/mcp"
	"github.com/cajasmota/archigraph/internal/progress"
	"github.com/cajasmota/archigraph/internal/quality"
	"github.com/cajasmota/archigraph/internal/quality/audit"
	"github.com/cajasmota/archigraph/internal/registry"
	"github.com/cajasmota/archigraph/internal/resolve"
)

// daemonProgressBroker is the process-wide indexer progress bus. The Rebuild
// path publishes granular per-repo progress.Event records into it (via the
// indexer's WithPublisher option) and the dashboard's /api/index-progress SSE
// endpoints subscribe to it, so the WebUI Index step renders live per-repo /
// per-module rows with file counters instead of a generic bar (#1531). It is
// created once in runDaemon before the RPC + dashboard servers start.
var daemonProgressBroker = progress.NewBroker()

// defaultDashboardPort is the default TCP port for the embedded dashboard.
const defaultDashboardPort = 47274

// defaultRSSBudgetMB returns the production default for the admission-control
// budget (in MB). It auto-tunes based on available system memory so that
// the daemon's idle RSS (heap inflation after graph load) does not cause the
// scheduler to wedge when the user's repos are large.
//
// Formula: min(2048, systemMemoryMB / 8).  On a 16 GB machine this gives
// 2 GB; on an 8 GB machine 1 GB; on a 4 GB machine 512 MB.  The env var
// ARCHIGRAPH_MAX_RSS_BUDGET_MB and the --max-rss-budget flag both override
// the result, so operators can tune down on constrained hardware.
//
// NOTE: this budget is for the ADDITIONAL predicted RSS of concurrently
// running index jobs only — the daemon's idle RSS is never subtracted from
// it (delta-based accounting).  See internal/daemon/sched for the admission
// logic.
func defaultRSSBudgetMB() int64 {
	sysMB := systemTotalMemoryMB()
	if sysMB <= 0 {
		return 500 // safe fallback when sysinfo is unavailable
	}
	budget := sysMB / 8
	const cap = 2048
	if budget > cap {
		budget = cap
	}
	return budget
}

// systemTotalMemoryMB returns total host physical memory in MB via the
// process package's platform-specific sysinfo implementation.
func systemTotalMemoryMB() int64 {
	return process.TotalMemoryMB()
}

// computeRebuildConcurrency applies the auto-tune formula to an explicit
// memory size (in MB). This is the pure, testable core of defaultRebuildConcurrency.
// Formula: min(8, sysMB/4096), floored at 2.
//
//   - sysMB ≤ 0 → 2 (sysinfo unavailable)
//   - < 8 GB    → 2 (floor)
//   -  8 GB     → 2
//   - 16 GB     → 4
//   - 32 GB     → 8
//   - ≥ 32 GB   → 8 (ceiling; above this, file-I/O contention outweighs the gain)
func computeRebuildConcurrency(sysMB int64) int {
	if sysMB <= 0 {
		return 2
	}
	n := int(sysMB / 4096)
	if n < 2 {
		n = 2
	}
	if n > 8 {
		n = 8
	}
	return n
}

// defaultRebuildConcurrency auto-tunes the parallel rebuild cap based on
// available system memory (#2127). Delegates to computeRebuildConcurrency
// with the live system total so the logic is independently testable.
//
// The env var ARCHIGRAPH_REBUILD_CONCURRENCY and the --max-concurrent-groups
// flag both override the result.
func defaultRebuildConcurrency() int {
	return computeRebuildConcurrency(systemTotalMemoryMB())
}

// resolveEnvRebuildConcurrency reads ARCHIGRAPH_REBUILD_CONCURRENCY (then
// ARCHIGRAPH_MAX_CONCURRENT_GROUPS as a legacy fallback) and returns the
// effective concurrency value, falling back to the auto-tuned default when
// the env var is absent or invalid. This mirrors the parsing logic in
// runDaemon and is exposed for unit testing.
func resolveEnvRebuildConcurrency() int {
	if v := os.Getenv("ARCHIGRAPH_REBUILD_CONCURRENCY"); v != "" {
		if parsed, perr := strconv.Atoi(v); perr == nil && parsed >= 1 {
			return parsed
		}
	}
	if v := os.Getenv("ARCHIGRAPH_MAX_CONCURRENT_GROUPS"); v != "" {
		if parsed, perr := strconv.Atoi(v); perr == nil && parsed >= 1 {
			return parsed
		}
	}
	return defaultRebuildConcurrency()
}

// rebuildConcurrency is the package-level concurrency cap used by
// daemonRebuildFunc. It is set once by runDaemon before the RPC server
// starts accepting connections. Concurrent calls to daemonRebuildFunc
// each get their own semaphore, so different group rebuilds do not share
// the cap — the cap applies to repos within a single group rebuild.
var rebuildConcurrency = defaultRebuildConcurrency()

// rebuildIndexFunc is the per-repo index entrypoint used by daemonRebuildFunc.
// It defaults to the package-level Index function but can be replaced in tests
// to validate parallelism semantics without running a real extractor pass.
// Must be set before the daemon accepts connections (write-once, then read-only).
var rebuildIndexFunc = func(repoPath, outPath, repoTag string, skipPasses []string, pretty, jsonStats bool, opts ...IndexOption) error {
	return Index(repoPath, outPath, repoTag, skipPasses, pretty, jsonStats, opts...)
}

// rebuildLinksFunc is the cross-repo link hook used by daemonRebuildFunc.
// Defaults to runLinksHook but can be swapped in tests.
var rebuildLinksFunc = func(group string) error {
	return runLinksHook(group)
}

// runDaemon is the long-running mode of the archigraph binary. It is
// wired into the CLI as a hidden `archigraph daemon` subcommand —
// users normally reach it via `archigraph start`, which forks this
// process and detaches.
//
// All extractor + registry + linker work happens here. The CLI's other
// subcommands are thin RPC clients (see internal/daemon/client).
func runDaemon(argv []string) error {
	// Parse daemon-only flags. The root cobra command has flag parsing
	// disabled for "daemon" so we own the argv. Unknown flags exit
	// with a clear error rather than being silently ignored.
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	var maxRSSBudget int64
	envBudget := defaultRSSBudgetMB()
	if v := os.Getenv("ARCHIGRAPH_MAX_RSS_BUDGET_MB"); v != "" {
		if parsed, perr := strconv.ParseInt(v, 10, 64); perr == nil && parsed >= 0 {
			envBudget = parsed
		}
	}
	fs.Int64Var(&maxRSSBudget, "max-rss-budget", envBudget,
		"max predicted RSS (MB) for concurrent index jobs; 0 disables admission control")

	var maxConcurrentGroups int
	// Priority: ARCHIGRAPH_REBUILD_CONCURRENCY > ARCHIGRAPH_MAX_CONCURRENT_GROUPS > auto-tune.
	envConcGroups := resolveEnvRebuildConcurrency()
	fs.IntVar(&maxConcurrentGroups, "max-concurrent-groups", envConcGroups,
		"max repos indexed in parallel during rebuild (auto-tuned from memory; floor=2 cap=8)")

	if err := fs.Parse(argv); err != nil {
		return err
	}

	layout, err := daemon.DefaultLayout()
	if err != nil {
		return fmt.Errorf("resolve daemon layout: %w", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		return fmt.Errorf("ensure layout: %w", err)
	}

	// #1626: one-time sweep to relocate any pre-existing in-repo
	// `.archigraph/` graph artifacts into the external store, so groups
	// that were indexed before this change don't need a full re-index and
	// their working trees end up clean. Best-effort + idempotent.
	for _, repoPath := range daemonReposToWatch() {
		if migrated, mErr := daemon.MigrateInRepoState(repoPath); mErr != nil {
			fmt.Fprintf(os.Stderr, "archigraph: migrate %s: %v\n", repoPath, mErr)
		} else if migrated {
			fmt.Fprintf(os.Stderr, "archigraph: migrated in-repo .archigraph for %s → store\n", repoPath)
		}
	}

	// Log to both stderr (so `archigraph start` foreground mode shows
	// progress) and the rotating log file. Phase B will replace the
	// raw file with a size-rotated writer; for Phase A a single append
	// file is fine.
	logFile, err := os.OpenFile(layout.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open log %s: %w", layout.LogPath, err)
	}
	defer logFile.Close()
	logger := log.New(io.MultiWriter(os.Stderr, logFile), "archigraph-daemon: ",
		log.LstdFlags|log.Lmicroseconds)
	// ADR-0016 flip-day (#808): log the active graph format mode so users
	// can confirm the daemon is running in the expected configuration.
	logger.Printf("graph format: fb-default (json-fallback enabled) — graph.fb written on every index; --skip-json opt-in drops graph.json")

	// Resolve dashboard port: env var > default. A future
	// ~/.config/archigraph/daemon.toml can add more overrides.
	dashPort := defaultDashboardPort
	if v := os.Getenv("ARCHIGRAPH_DASHBOARD_PORT"); v != "" {
		if p, perr := strconv.Atoi(v); perr == nil && p > 0 && p <= 65535 {
			dashPort = p
		}
	}

	cfg := daemon.Config{
		Layout:       layout,
		Logger:       logger,
		Index:        daemonIndexFunc,
		Rebuild:      daemonRebuildFunc,
		QualityAudit: daemonQualityAuditFunc,

		// Phase B — wire the watcher + scheduler. The fast reactive
		// reindex skips Pass 4 (graph algorithms) so a freshly-saved
		// file becomes queryable as soon as the basic graph lands;
		// the algorithm pass is run separately on a 30s debounce.
		ReposToWatch:   daemonReposToWatch,
		GroupsForRepo:  daemonGroupsForRepo,
		SchedulerIndex: daemonSchedulerIndex,
		SchedulerLinks: daemonSchedulerLinks,
		SchedulerAlgo:  daemonSchedulerAlgo,

		MaxRSSBudgetMB:      maxRSSBudget,
		RSSHistoryPath:      filepath.Join(filepath.Dir(layout.PIDPath), "repo-rss-history.json"),
		MaxConcurrentGroups: maxConcurrentGroups,

		// Pattern confidence time-decay: runs every 6 hours.
		// PatternGroupDirs returns the patterns storage directory for each
		// registered group so the decay scheduler can find patterns.json.
		PatternGroupDirs: daemonPatternGroupDirs,

		// Phase D — MCP RPC surface (ADR-0017 #832).
		// Inject the tool catalog and dispatcher so the bridge can call
		// Daemon.MCPToolList / Daemon.MCPToolCall over the socket.
		MCPListTools: daemonMCPListTools,
		MCPCallTool:  daemonMCPCallTool,

		// Dashboard HTTP server (#929/#931): fold the SPA + REST API
		// into the daemon process so a single launchd unit serves both.
		// Capture startedAt so /api/info can report daemon uptime (#991).
		DashboardServe: makeDaemonDashboardServe(time.Now()),
		DashboardPort:  dashPort,
		DashboardBind:  "127.0.0.1",
	}

	// Apply the concurrency cap before the RPC server opens so
	// daemonRebuildFunc picks it up immediately. Written once; no race.
	if maxConcurrentGroups >= 1 {
		rebuildConcurrency = maxConcurrentGroups
	}

	ctx := context.Background()
	return daemon.Run(ctx, cfg)
}

// daemonReposToWatch returns every repo from every registered group
// (deduped by absolute path). Called once at daemon startup.
func daemonReposToWatch() []string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, r := range cfg.Repos {
			abs, err := filepath.Abs(r.Path)
			if err != nil {
				abs = r.Path
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out
}

// daemonGroupsForRepo returns the names of the groups whose config
// lists repoPath (compared by absolute path).
func daemonGroupsForRepo(repoPath string) []string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	var out []string
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, r := range cfg.Repos {
			rp, err := filepath.Abs(r.Path)
			if err != nil {
				rp = r.Path
			}
			if rp == abs {
				out = append(out, g.Name)
				break
			}
		}
	}
	return out
}

// daemonSchedulerIndex is the fast reactive reindex used by the
// scheduler's worker pool. It skips the graph-algorithm pass so the
// basic graph is available to queries within seconds of a file save;
// the algorithm pass runs separately via daemonSchedulerAlgo on a
// longer debounce.
//
// ref is the git branch name captured at Enqueue time (PH1b of #2087).
// It is passed as the repoTag so the graph artifact is written into the
// correct per-ref directory. When ref is empty the indexer falls back to
// the current HEAD ref via gitmeta.Capture inside StateDirForRepo.
func daemonSchedulerIndex(_ context.Context, repoPath string, ref string) error {
	// ADR-0016 flip-day (#808): graph.fb is always written by default now.
	// WithExportFB is a no-op kept for back-compat; removed in next release.
	// Pass ref as the output-path hint so the per-ref store layout is used.
	_ = ref // ref is stored via StateDirForRepo → StateDirForRepoRef at write time;
	// the indexer itself resolves the correct path via gitmeta at index time.
	err := Index(repoPath, "", "", []string{"graph-algo"}, false, false)
	// Drop the cached mmap so the next MCP query reopens against the
	// freshly written graph.fb. Done on both success and failure paths
	// — a stale handle is worse than a cold miss.
	invalidateAfterIndex(repoPath)
	return err
}

// daemonSchedulerLinks re-runs the cross-repo link passes for a group.
// Delegates to the same hook the Rebuild RPC uses so behaviour is
// identical to a force rebuild's link step.
func daemonSchedulerLinks(_ context.Context, group string) error {
	return runLinksHook(group)
}

// daemonSchedulerAlgo runs the full index (including Pass 4 algorithms)
// against a repo. The scheduler arranges cancel+reschedule on new
// writes, so this is allowed to be slow.
func daemonSchedulerAlgo(_ context.Context, repoPath string) error {
	// ADR-0016 flip-day (#808): graph.fb is always written by default now.
	// #1576: tag with the registered CONFIG slug when this path is known to a
	// group, so a watcher-triggered re-index keeps doc.Repo aligned with the
	// dashboard's node slugs and the cross-repo link endpoints. An empty
	// repoTag would fall back to the dir basename and diverge from a slugified
	// config slug (e.g. upvate_core vs upvate-core), dropping cross-repo edges.
	err := Index(repoPath, "", configSlugForPath(repoPath), nil, false, false)
	invalidateAfterIndex(repoPath)
	return err
}

// configSlugForPath returns the registered config slug for repoPath by
// scanning all group configs, or "" when the path is not registered (in which
// case Index falls back to the directory basename). Paths are compared after
// filepath.Clean so trailing-slash / relative differences do not defeat the
// match.
func configSlugForPath(repoPath string) string {
	want := filepath.Clean(repoPath)
	groups, err := registry.Groups()
	if err != nil {
		return ""
	}
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, r := range cfg.Repos {
			if filepath.Clean(r.Path) == want {
				return r.Slug
			}
		}
	}
	return ""
}

// daemonIndexFunc is the IndexFunc handed to daemon.Run. It bridges the
// RPC argument struct onto the existing in-process Index() entrypoint
// defined in this same package.
func daemonIndexFunc(args proto.IndexArgs) (string, string, error) {
	opts := []IndexOption{
		WithRepairCandidates(args.Repair),
		WithRepairApply(args.RepairApply),
		WithExportFB(args.ExportFB),
		WithPrintSkipped(args.PrintSkipped),
		WithAdditionalSkipDirs(args.AdditionalSkipDirs),
		WithExportJSON(args.ExportJSON),
	}
	// Capture stats into a local buffer when the caller asked for them.
	// setCapturedStats is a tiny package-level swap (Phase A serializes
	// indexes, so the single-writer assumption holds — see comment in
	// index.go). Phase B's job queue will thread the writer explicitly.
	var statsBuf bytes.Buffer
	if args.JSONStats {
		restore := setCapturedStats(&statsBuf)
		defer restore()
	}
	err := Index(args.RepoPath, args.OutPath, args.RepoTag, args.SkipPasses,
		args.Pretty, args.JSONStats, opts...)
	if err != nil {
		return "", "", err
	}
	graphPath := args.OutPath
	if graphPath == "" {
		graphPath = daemon.GraphPathForRepo(args.RepoPath)
	}
	return graphPath, statsBuf.String(), nil
}

// daemonRebuildFunc force-indexes every repo in a group. We deliberately
// re-implement the iteration here rather than calling into internal/cli
// to avoid pulling cobra back into the daemon's call graph.
//
// Parallelism (#1276): repos are indexed concurrently up to rebuildConcurrency
// workers. One failing repo does not stop the others — all are attempted and
// any errors are collected and returned together. Per-repo wall time is logged
// to stderr for diagnostics. The cross-repo link pass runs only once all
// per-repo indexes complete.
func daemonRebuildFunc(args proto.RebuildArgs) ([]string, string, error) {
	rebuildStart := time.Now()
	fmt.Fprintf(os.Stderr, "archigraph: rebuild start group=%s slug=%q wipe=%v incremental=%v\n",
		args.Group, args.Slug, args.Wipe, args.Incremental)
	defer func() {
		fmt.Fprintf(os.Stderr, "archigraph: rebuild exit group=%s took=%s\n",
			args.Group, time.Since(rebuildStart).Truncate(time.Millisecond))
	}()

	groups, err := registry.Groups()
	if err != nil {
		return nil, "", err
	}
	var ref *registry.GroupRef
	for i := range groups {
		if groups[i].Name == args.Group {
			ref = &groups[i]
			break
		}
	}
	if ref == nil {
		return nil, "", fmt.Errorf("unknown group: %s", args.Group)
	}
	cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
	if err != nil {
		return nil, "", err
	}
	// Issue #1206 — apply group-level extra_stdlib_filter before indexing so
	// the synthesiser suppresses user-configured framework stdlib names.
	for lang, names := range cfg.ExtraStdlibFilter {
		resolve.RegisterExtraStdlibFilter(lang, names)
	}

	// Collect repos to index, respecting the optional single-slug filter.
	type repoWork struct {
		r registry.Repo
	}
	var work []repoWork
	for _, r := range cfg.Repos {
		if args.Slug != "" && r.Slug != args.Slug {
			continue
		}
		work = append(work, repoWork{r: r})
	}

	// Serial fast path: single worker or single repo skips goroutine overhead.
	conc := rebuildConcurrency
	if conc < 1 {
		conc = 1
	}

	// Results collected from workers.
	type repoResult struct {
		path string
		slug string
		err  error
		took time.Duration
	}
	results := make([]repoResult, len(work))

	if conc == 1 || len(work) <= 1 {
		// --- Serial path ---
		for i, w := range work {
			t0 := time.Now()
			// Panic recovery (#2097): convert an extractor panic into an error so
			// the remaining repos in the batch can still run.
			var indexErr error
			func(rw repoWork, idx int) {
				defer func() {
					if r := recover(); r != nil {
						indexErr = fmt.Errorf("index panic: %v", r)
						fmt.Fprintf(os.Stderr,
							"archigraph: rebuild %s panic recovered: %v\n",
							rw.r.Slug, r)
					}
				}()
				if args.Wipe {
					_ = os.RemoveAll(daemon.StateDirForRepo(rw.r.Path))
				}
				var opts []IndexOption
				if args.Incremental && !args.Wipe {
					opts = append(opts, WithIncremental(daemon.StateDirForRepo(rw.r.Path)))
				}
				// Publish granular per-repo progress into the shared broker so the
				// WebUI Index step renders live rows + file counters (#1531).
				opts = append(opts,
					WithPublisher(daemonProgressBroker),
					WithProgressSlugs(args.Group, rw.r.Slug))
				// #1576: tag the graph with the CONFIG slug (not the on-disk
				// directory basename) so doc.Repo matches the slug the dashboard
				// keys nodes by and the slug the cross-repo link pass emits as the
				// link endpoint prefix. When the wizard slugifies a repo name
				// (e.g. upvate_core → upvate-core) an empty repoTag would fall back
				// to the dir basename and diverge, dropping every cross-repo edge.
				indexErr = rebuildIndexFunc(rw.r.Path, "", rw.r.Slug, nil, false, false, opts...)
			}(w, i)
			results[i] = repoResult{
				path: w.r.Path,
				slug: w.r.Slug,
				err:  indexErr,
				took: time.Since(t0),
			}
		}
	} else {
		// --- Parallel path: semaphore-bounded worker pool ---
		//
		// Panic recovery (#2097): a panic inside rebuildIndexFunc would
		// propagate out of the goroutine and crash the daemon. We recover,
		// convert the panic to an error stored in results[idx], and release
		// the semaphore slot via defer so subsequent goroutines are not
		// starved. The deferred <-sem is registered AFTER the blocking
		// sem<- succeeds, so a panic before acquiring the slot cannot leave
		// a phantom holder either.
		sem := make(chan struct{}, conc)
		var wg sync.WaitGroup
		for i, w := range work {
			wg.Add(1)
			go func(idx int, rw repoWork) {
				defer wg.Done()

				// Acquire semaphore slot. This MUST come before the
				// panic-recovery defer so the defer's <-sem only fires
				// once the slot is actually held.
				sem <- struct{}{}
				defer func() { <-sem }()

				t0 := time.Now()

				// Recover from panics in the index function so a single
				// failing repo does not crash the daemon process (#2097).
				var indexErr error
				func() {
					defer func() {
						if r := recover(); r != nil {
							indexErr = fmt.Errorf("index panic: %v", r)
							fmt.Fprintf(os.Stderr,
								"archigraph: rebuild %s panic recovered: %v\n",
								rw.r.Slug, r)
						}
					}()

					if args.Wipe {
						_ = os.RemoveAll(daemon.StateDirForRepo(rw.r.Path))
					}
					var opts []IndexOption
					if args.Incremental && !args.Wipe {
						opts = append(opts, WithIncremental(daemon.StateDirForRepo(rw.r.Path)))
					}
					// Publish granular per-repo progress into the shared broker so
					// the WebUI Index step renders live rows + file counters (#1531).
					opts = append(opts,
						WithPublisher(daemonProgressBroker),
						WithProgressSlugs(args.Group, rw.r.Slug))
					// #1576: tag with the CONFIG slug — see serial path above.
					indexErr = rebuildIndexFunc(rw.r.Path, "", rw.r.Slug, nil, false, false, opts...)
				}()

				results[idx] = repoResult{
					path: rw.r.Path,
					slug: rw.r.Slug,
					err:  indexErr,
					took: time.Since(t0),
				}
			}(i, w)
		}
		wg.Wait()
	}

	// Collect successful paths; log per-repo wall time; gather errors.
	var rebuilt []string
	var errs []string
	for _, res := range results {
		if res.path == "" {
			continue // slot never filled (shouldn't happen)
		}
		fmt.Fprintf(os.Stderr, "archigraph: rebuild %s took %s",
			res.slug, res.took.Truncate(time.Millisecond))
		if res.err != nil {
			fmt.Fprintf(os.Stderr, " [FAILED: %v]\n", res.err)
			errs = append(errs, fmt.Sprintf("index %s: %v", res.slug, res.err))
			continue
		}
		fmt.Fprintln(os.Stderr, "")
		rebuilt = append(rebuilt, res.path)

		// Auto-inject Architecture Map block into AGENTS.md / CLAUDE.md when
		// opted in. Best-effort: a write failure is logged but never fails the
		// rebuild so a read-only repo or missing permissions don't surface as
		// an error to the user (#1216).
		if cfg.Features.AutoInjectAgentsMD {
			mapStats := buildAgentsMapStats(cfg.Name, res.path)
			if err := agents.InjectArchitectureMap(res.path, mapStats); err != nil {
				fmt.Fprintf(os.Stderr,
					"archigraph: auto-inject agents map for %s: %v (non-fatal)\n",
					res.slug, err)
			}
		}
	}

	// Return a combined error if any repos failed. The rebuilt list still
	// contains all repos that succeeded, so the caller can report partial results.
	if len(errs) > 0 {
		return rebuilt, "", fmt.Errorf("%s", strings.Join(errs, "; "))
	}

	// Cross-repo link passes run after every member is indexed.
	warning := ""
	if err := rebuildLinksFunc(args.Group); err != nil {
		// Best-effort — surface as a warning, not a hard failure.
		warning = fmt.Sprintf("link passes failed: %v", err)
	}

	// Persist a quality-metrics snapshot to health-history.jsonl (#1329).
	// Best-effort: failure is logged but never blocks the caller.
	go func() {
		if layout, lerr := daemon.DefaultLayout(); lerr == nil {
			if herr := appendRebuildHistory(layout.Root, args.Group, cfg, rebuilt); herr != nil {
				fmt.Fprintf(os.Stderr, "archigraph: record quality history for %s: %v (non-fatal)\n",
					args.Group, herr)
			}
		}
	}()

	return rebuilt, warning, nil
}

// buildAgentsMapStats loads the per-repo graph artefacts produced by the
// just-completed index and assembles the Stats struct passed to
// agents.InjectArchitectureMap. It is intentionally best-effort — any read
// failure yields a zero-valued field rather than an error.
func buildAgentsMapStats(group, repoPath string) agents.Stats {
	stateDir := daemon.StateDirForRepo(repoPath)

	s := agents.Stats{
		Group:         group,
		DashboardPort: resolveDefaultDashboardPort(),
	}

	// Read graph.fb for per-kind entity breakdown. Falls back gracefully if the
	// file is absent or the FB decoder is unavailable.
	if doc, err := loadGraphFromStateDir(stateDir); err == nil && doc != nil {
		s.Entities = doc.Stats.Entities
		s.Relationships = doc.Stats.Relationships
		for _, e := range doc.Entities {
			switch e.Kind {
			// #1217: count all three http endpoint kind strings.
			case "http_endpoint", "http_endpoint_definition", "http_endpoint_call":
				s.HTTPEndpoints++
			case "queue":
				s.Queues++
			case "topic", "pubsub_topic":
				s.Topics++
			}
			if strings.HasPrefix(e.Kind, "SCOPE.Process") || e.Kind == "process" {
				s.ProcessFlows++
			}
		}
	}

	return s
}

// loadGraphFromStateDir is a thin wrapper around graph.LoadGraphFromDir that
// isolates the graph-loading call used by buildAgentsMapStats. Keeping it
// separate makes it easy to stub in tests without touching the full graph
// package.
func loadGraphFromStateDir(stateDir string) (*graph.Document, error) {
	return graph.LoadGraphFromDir(stateDir)
}

// daemonQualityAuditFunc is the QualityAuditFunc handed to daemon.Run.
// It calls audit.AuditPath (in this process — the daemon process) and
// serialises the result into the wire reply.
func daemonQualityAuditFunc(args proto.QualityAuditRequest) (proto.QualityAuditReply, error) {
	rep, err := audit.AuditPath(args.RepoPath, args.Corpus)
	if err != nil {
		return proto.QualityAuditReply{}, err
	}

	// Build the scalar summary by folding per-repo numbers.
	var totalEntities, totalOrphans int
	orphansByKind := make(map[string]int)
	for _, rr := range rep.Repos {
		if rr == nil {
			continue
		}
		totalEntities += rr.Entities
		totalOrphans += rr.Orphans
		for cause, n := range rr.OrphanClassification {
			orphansByKind[string(cause)] += n
		}
	}
	orphanRate := 0.0
	if totalEntities > 0 {
		orphanRate = 100.0 * float64(totalOrphans) / float64(totalEntities)
	}

	// Serialise the report according to the requested format.
	var sb strings.Builder
	if args.JSON {
		enc := json.NewEncoder(&sb)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return proto.QualityAuditReply{}, fmt.Errorf("encode audit report: %w", err)
		}
	} else {
		if err := rep.WriteMarkdown(&sb); err != nil {
			return proto.QualityAuditReply{}, fmt.Errorf("format audit report: %w", err)
		}
	}

	return proto.QualityAuditReply{
		OrphansByKind:     orphansByKind,
		TotalEntities:     totalEntities,
		TotalOrphans:      totalOrphans,
		OrphanRatePercent: orphanRate,
		Markdown:          sb.String(),
	}, nil
}

// daemonRecallFunc is the RecallRunner injected into the dashboard server.
// It runs the full in-process indexer against a named golden fixture and
// returns the quality.JSONReport serialised as JSON bytes.
//
// The fixture must be one of the directories inside internal/quality/golden/;
// the path is resolved via goldenFixturesDir() inside the handler.
func daemonRecallFunc(fixtureName string) ([]byte, error) {
	goldenDir, err := dashboard.GoldenFixturesDir()
	if err != nil {
		return nil, fmt.Errorf("locate fixtures: %w", err)
	}
	fixtureDir := filepath.Join(goldenDir, fixtureName)

	fix, err := quality.LoadFixture(fixtureDir)
	if err != nil {
		return nil, fmt.Errorf("load fixture %q: %w", fixtureName, err)
	}
	srcDir := quality.SourceDir(fixtureDir)
	if st, serr := os.Stat(srcDir); serr != nil || !st.IsDir() {
		return nil, fmt.Errorf("fixture src/ missing or not a directory: %s", srcDir)
	}

	tmp, err := os.MkdirTemp("", "archigraph-recall-*")
	if err != nil {
		return nil, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(tmp)

	graphPath := filepath.Join(tmp, "graph.json")
	if err := Index(srcDir, graphPath, fix.Name, nil, false, false, WithExportJSON(true)); err != nil {
		return nil, fmt.Errorf("index fixture src: %w", err)
	}

	doc, err := loadDocument(graphPath)
	if err != nil {
		return nil, fmt.Errorf("load graph: %w", err)
	}

	rep := quality.Evaluate(fix, doc)
	jr := rep.ToJSON()
	raw, err := json.Marshal(jr)
	if err != nil {
		return nil, fmt.Errorf("encode recall report: %w", err)
	}
	return raw, nil
}

// mustEncodeStatus is a small helper for the `status` command when it
// prints the daemon's reply as JSON. Lives here so cmd/archigraph
// doesn't have to import encoding/json from a half-dozen call sites.
func mustEncodeStatus(w io.Writer, reply proto.StatusReply) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(reply)
}

// daemonNotRunningErr is the canonical user-facing error returned by
// any client subcommand when the daemon socket is unreachable.
var daemonNotRunningErr = errors.New(
	"daemon not running; run 'archigraph start' or reinstall via 'archigraph install'",
)

// daemonPatternGroupDirs returns a map of group-name → patterns storage
// directory for every registered group. This is injected into daemon.Config
// so the pattern decay scheduler can find each group's patterns.json.
//
// Directory convention mirrors internal/mcp/patterns.go defaultPatternsDir:
// ~/.archigraph/groups/<group>-patterns/. Groups whose patterns are stored in
// a custom MemoryDir (MCP registry config) will be found there by the MCP
// server; the daemon uses the default path which covers production deployments.
func daemonPatternGroupDirs() map[string]string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	out := make(map[string]string, len(groups))
	for _, g := range groups {
		dir := filepath.Join(home, ".archigraph", "groups", g.Name+"-patterns")
		out[g.Name] = dir
	}
	return out
}

// makeDaemonDashboardServe returns the DashboardServe hook injected into
// daemon.Config. It captures daemonStartedAt so the /api/info endpoint can
// report uptime without a separate RPC call (#991).
//
// This function lives in cmd/archigraph (not internal/daemon) to avoid the
// import cycle: internal/dashboard already imports internal/daemon.
func makeDaemonDashboardServe(daemonStartedAt time.Time) func(ctx context.Context, bind string, port int, logger *log.Logger) error {
	return func(ctx context.Context, bind string, port int, logger *log.Logger) error {
		addr := net.JoinHostPort(bind, strconv.Itoa(port))
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("dashboard listen %s: %w", addr, err)
		}

		// Build dashboard config: fixed port (the daemon already owns the listener).
		cfg := dashboard.Config{
			PortRange: dashboard.PortRange{Min: port, Max: port},
			Bind:      bind,
		}
		srv, err := dashboard.NewServer(cfg, dashboard.NewLiveStore())
		if err != nil {
			_ = l.Close()
			return fmt.Errorf("dashboard new server: %w", err)
		}
		// Tell the dashboard server when the daemon started so /api/info
		// can compute and report uptime (#991).
		srv.SetDaemonStartedAt(daemonStartedAt)

		// Wire the shared indexer progress broker (#1531) so the
		// /api/index-progress SSE endpoints can fan granular per-repo /
		// per-module progress.Event records to the WebUI Index step. The
		// Rebuild path publishes into this same broker (see daemonRebuildFunc).
		srv.SetProgressBroker(daemonProgressBroker)

		// Wire MCP activity broker (epic #1157, Phase 1: Jarvis).
		// The same broker is injected into the shared MCP server so tool
		// calls emit events that flow through the dashboard SSE endpoint.
		activityBroker := mcp.NewMCPActivityBroker()
		logPath := mcp.DefaultActivityLogPath()
		if logPath != "" {
			actLog := mcp.NewActivityLog(logPath)
			activityBroker.SetLog(actLog)
			srv.SetMCPActivityLog(logPath)
		}
		srv.SetMCPActivityBroker(activityBroker)
		// Wire the broker into the shared MCP server (lazily initialised).
		// We call mcpServerInstance here to ensure it exists; on failure we
		// proceed without activity emission rather than crashing the daemon.
		if mcpSrv, initErr := mcpServerInstance(); initErr == nil {
			mcpSrv.SetActivityBroker(activityBroker)
		}

		// Wire the recall runner so POST /api/quality/recall can run the
		// in-process indexer against golden fixtures (#1198).
		srv.SetRecallRunner(daemonRecallFunc)

		// Wire the enrichment job queue (#1244). Jobs persist to
		// ~/.archigraph/jobs.jsonl so history survives daemon restarts.
		var jobHistoryPath string
		if daemonLayout, layoutErr := daemon.DefaultLayout(); layoutErr == nil {
			jobHistoryPath = filepath.Join(daemonLayout.Root, "jobs.jsonl")
		}
		jobQueue := jobs.NewQueue(jobHistoryPath, jobs.DefaultWorkers)
		jobQueue.Start()
		srv.SetJobQueue(jobQueue)
		// Stop the job queue when the daemon context is cancelled.
		go func() {
			<-ctx.Done()
			jobQueue.Stop()
		}()

		srv.UseListener(l)
		if logger != nil {
			logger.Printf("dashboard ready http://%s/", addr)
		}
		return srv.Serve(ctx)
	}
}
