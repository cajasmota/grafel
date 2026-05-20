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
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/dashboard"
	"github.com/cajasmota/archigraph/internal/quality/audit"
	"github.com/cajasmota/archigraph/internal/registry"
)

// defaultDashboardPort is the default TCP port for the embedded dashboard.
const defaultDashboardPort = 47274

// defaultRSSBudgetMB is the production default for the concurrency
// cap. Chosen to match the post-#639 single-reindex peak (343MB) plus
// headroom for one small concurrent reindex: targets the 500MB cap
// from the real-fixture benchmark.
const defaultRSSBudgetMB = 500

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
	envBudget := int64(defaultRSSBudgetMB)
	if v := os.Getenv("ARCHIGRAPH_MAX_RSS_BUDGET_MB"); v != "" {
		if parsed, perr := strconv.ParseInt(v, 10, 64); perr == nil && parsed >= 0 {
			envBudget = parsed
		}
	}
	fs.Int64Var(&maxRSSBudget, "max-rss-budget", envBudget,
		"max predicted RSS (MB) for concurrent index jobs; 0 disables admission control")
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

		MaxRSSBudgetMB: maxRSSBudget,
		RSSHistoryPath: filepath.Join(filepath.Dir(layout.PIDPath), "repo-rss-history.json"),

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
func daemonSchedulerIndex(_ context.Context, repoPath string) error {
	// ADR-0016 flip-day (#808): graph.fb is always written by default now.
	// WithExportFB is a no-op kept for back-compat; removed in next release.
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
	err := Index(repoPath, "", "", nil, false, false)
	invalidateAfterIndex(repoPath)
	return err
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
func daemonRebuildFunc(args proto.RebuildArgs) ([]string, string, error) {
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
	var rebuilt []string
	for _, r := range cfg.Repos {
		if args.Slug != "" && r.Slug != args.Slug {
			continue
		}
		if args.Wipe {
			_ = os.RemoveAll(daemon.StateDirForRepo(r.Path))
		}
		if err := Index(r.Path, "", "", nil, false, false); err != nil {
			return rebuilt, "", fmt.Errorf("index %s: %w", r.Slug, err)
		}
		rebuilt = append(rebuilt, r.Slug)
	}
	// Cross-repo link passes run after every member is indexed.
	warning := ""
	if err := runLinksHook(args.Group); err != nil {
		// Best-effort — surface as a warning, not a hard failure.
		warning = fmt.Sprintf("link passes failed: %v", err)
	}
	return rebuilt, warning, nil
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
		srv.UseListener(l)
		if logger != nil {
			logger.Printf("dashboard ready http://%s/", addr)
		}
		return srv.Serve(ctx)
	}
}
