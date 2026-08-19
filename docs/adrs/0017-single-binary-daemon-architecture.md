# ADR-0017: Single-binary daemon architecture

- **Status**: Accepted (Phase A landed; Phases B/C/D follow in dedicated PRs)
- **Date**: 2026-05-19
- **Deciders**: Jorge Cajas
- **Related**: ADR-0001 (single-binary distribution), ADR-0004 (single MCP process per machine), ADR-0016 (binary graph format)
- **Supersedes**: the multi-process model where `grafel index`, `grafel mcp serve`, and per-repo watcher units each ran as independent processes

## Context

Until this ADR, grafel spread its work across three independent process families:

1. **Per-invocation `grafel index <repo>`** — a fresh process per index, full of cold-start cost (parser factory build, classifier load, framework rules parse) for every run.
2. **Per-machine `grafel mcp serve`** — one MCP process, but every tool call re-parsed `graph.json` from disk (50 MB / 640 k allocs per call against `client-fixture-b`; see ADR-0016).
3. **Per-repo OS-native watcher units** — launchd/systemd units that exec `grafel index <repo>` on file changes, multiplying cold-start cost by repo count.

The cumulative effects are:

- **No process is steady-state.** Every fs event spawns a new indexer; the MCP server reparses on every call.
- **Algorithm passes run on every index.** PageRank, community detection, and articulation-point detection (ADR-0005's bake-in step) re-run for a one-line file edit, even though their outputs only depend on the post-debounce graph topology.
- **Cross-repo link passes are not coordinated.** Each watcher unit may finish at a different time; `remerge` had to be invoked manually to reconverge.
- **MCP memory grows unboundedly** during a session because the parsed graph is held in a tool-local map per call and re-parsed next call (no shared cache).

The product target is **install-and-forget**: a user runs `grafel install`, walks away, and the tool keeps the graph up to date and answers MCP queries from any AI agent. The current architecture cannot deliver that without a coordinating process. The fixture corpus, the JSON-parse cost, and the MCP-on-stdio model also stand in the way of the multi-million-entity ceiling in `project_grafel_v1_ship_gate_state`.

grafel has zero users today (`project_grafel_v1_ship_gate_state`). We have total freedom to break wire formats, CLI surface, and install semantics.

## Decision

**Run everything inside a single long-running daemon process per machine.** All indexing, cross-repo linking, scheduling, and MCP query serving happens inside `grafel daemon` (the long-running binary mode). The CLI binary, when invoked with any other subcommand, acts as a thin RPC client to that daemon over a Unix-domain socket.

### Core invariants

- **One daemon per machine.** PID file at `~/.grafel/daemon.pid`. The daemon refuses to start if another instance holds the socket.
- **No in-process CLI fallback.** If the daemon isn't running, `grafel index <repo>` returns a structured error: `daemon not running; run 'grafel start' or reinstall via 'grafel install'`. Silent in-process indexing is gone.
- **No backwards compatibility.** Standalone MCP serve, in-process index code paths, and the old "apply group config" install semantic are removed in this ADR's rollout. Users on prior binaries reinstall.
- **Multi-repo and monorepo keep working.** The group registry remains the source of truth for which repos are indexed and how cross-repo links are built. The daemon owns scheduling across the registered set.

### Communication

- **Transport**: Unix-domain socket at `~/.grafel/sockets/daemon.sock` (mode 0600).
- **Protocol**: `net/rpc` with the `jsonrpc` codec from `net/rpc/jsonrpc`. Stdlib only. No protobuf, no gRPC. The wire shape is JSON-RPC 1.0; service methods are named `Daemon.Index`, `Daemon.Status`, etc.
- **Why JSON-RPC over Unix socket** (vs gRPC, vs custom framing): stdlib only, zero new dependencies, trivial to debug with `nc -U` and `jq`, sufficient throughput for a control plane that handles tens of requests per second at most.

### Process layout

```
grafel daemon              (hidden subcommand; the long-running mode)
  └── Server (net/rpc/jsonrpc on UDS)
       ├── Index RPC           (calls cmd/grafel Index() in-process)
       ├── Status RPC          (reports uptime, RSS, in-flight jobs)
       ├── Stop RPC            (initiates graceful shutdown)
       └── [Phase B] fsnotify watcher per registered repo
       └── [Phase D] MCP query handlers backed by lazy-mmap graphs

grafel <anything-else>     (thin client mode)
  └── dials daemon.sock, invokes RPC, prints response
```

### Lifecycle commands

- `grafel start` — start the daemon (forks the binary in `daemon` mode, detached).
- `grafel stop` — RPC `Daemon.Stop`; daemon completes in-flight requests and exits.
- `grafel restart` — stop, then start.
- `grafel status` — RPC `Daemon.Status`; reports pid, uptime, RSS, in-flight jobs, registered groups. Falls back to "daemon not running" without erroring out.
- `grafel logs [--follow] [--tail N]` — reads `~/.grafel/logs/daemon.log`.

Watchers `start`/`stop`/`restart` per-group are gone; the daemon owns the fsnotify watchers in Phase B.

### Algorithm scheduling

Algorithm passes (PageRank, communities, articulation points) currently run inside `Index()` as the "graph-algo" pass. They are pure functions of the post-link graph and add measurable cost on every index. Phase B will:

1. Continue running them inline for one-shot RPC indexes (correctness floor).
2. Skip them in the debounced fs-driven reindex path, and instead schedule a per-group algorithm pass debounced ~30 s after the last fs-driven activity for any member of the group.
3. Write algorithm outputs to `graph-stats.fb` separately (this file already exists for the JSON variant), so the main `graph.fb` is decoupled from algorithm completion.

This ADR records the scheduling intent; Phase B owns the implementation.

### Service install

`grafel install`, in Phase C, will (with no arguments and no flags):

1. Resolve the running binary's absolute path.
2. macOS: write `~/Library/LaunchAgents/com.grafel.daemon.plist` with `RunAtLoad=true`, `KeepAlive=true`, stdout/stderr to `~/.grafel/logs/daemon.log`; `launchctl bootstrap gui/$UID`.
3. Linux: write `~/.config/systemd/user/grafel.service` with `Restart=on-failure`, `WantedBy=default.target`; `systemctl --user enable --now grafel.service`.
4. Register the daemon's MCP endpoint in `~/.claude/settings.json`, `~/.cursor/settings.json`, etc., via the existing `internal/install/mcpreg` helpers — pointing them at the daemon's socket-backed MCP proxy.

The OLD `grafel install <config>` semantic (apply group config) is REMOVED. Group configs are now consumed by the daemon at startup via the registry, and applied automatically; no per-group install step.

This ADR records the install intent; Phase C owns the implementation.

### Lazy mmap MCP integration

Phase D will:

1. Stop preloading any graph at daemon start.
2. mmap `graph.fb` (ADR-0016) the first time an MCP query targets that repo; hold the mmap handle in an LRU keyed by repo, with a cap of 5–10 entries.
3. Serve MCP queries inside the daemon's RPC server (the `grafel mcp serve` standalone is already deleted in Phase A; in Phase D the daemon registers itself as the MCP endpoint at install time).
4. Cross-repo linker also uses mmap reads — no full JSON loads.

## What this ADR deletes

- `grafel mcp serve` (the standalone stdio MCP server) — deleted in Phase A.
- `grafel remerge` (already deprecated) — deleted in Phase A.
- In-process fallback for `grafel index <repo>` and `grafel rebuild` — both become thin RPC clients that error when the daemon is down.
- The old `grafel install <config>` apply-group-config semantic — gone in Phase C; the new `install` is service-registration only.
- Per-repo watcher units under launchd/systemd — gone in Phase B; one daemon watches all repos.

## Memory + benchmark targets

These targets guide Phases B and D; Phase A's surface is the RPC plumbing and lifecycle, where idle RSS is the only meaningful number.

| Scenario                                                              | Target           |
|-----------------------------------------------------------------------|------------------|
| Daemon idle, no repos indexed                                         | ≤80 MB RSS       |
| Daemon indexing `client-fixture-b`                                    | ≤450 MB peak     |
| Daemon serving 50 MCP queries against an already-indexed fixture      | ≤120 MB steady   |
| Daemon with 3 repos indexed, idle                                     | ≤150 MB steady   |
| 3 repos concurrent index via daemon (jobs serialized)                 | ≤450 MB peak     |

If a target is missed, the PR that lands the relevant phase must document why and what is needed to close the gap. Numbers must be backed by pprof captures, not eyeballed.

## Phasing

| Phase | Scope                                                       | PR title                                                            |
|-------|-------------------------------------------------------------|---------------------------------------------------------------------|
| A     | Daemon core, RPC, lifecycle, delete-mcp-serve, thin clients | `[v1.0 daemon] Phase A: daemon core + RPC plumbing (ADR-0017)`      |
| B     | fsnotify watcher, debounced reindex, algorithm scheduling   | `[v1.0 daemon] Phase B: fsnotify + debounce + algorithm scheduling` |
| C     | `grafel install` service registration (mac+linux)       | `[v1.0 daemon] Phase C: zero-config service install`                |
| D     | Lazy mmap, MCP query handlers inside daemon                 | `[v1.0 daemon] Phase D: lazy mmap + daemon-served MCP`              |

## Consequences

**Positive**

- One process to debug. One log. One place to read RSS.
- Algorithm passes stop dominating fs-driven reindex latency.
- MCP graph state lives in mmap'd memory shared across calls — eliminates the per-call 50 MB JSON parse.
- Cross-repo link passes are first-class scheduled work, not a `remerge` manual step.
- Install becomes a one-liner.

**Negative**

- The CLI binary now has two modes (daemon vs client) selected by subcommand; users who run `grafel daemon` directly get a foregrounded daemon, which can be surprising. Mitigated by `grafel start` doing the fork+detach.
- A crashed daemon now blocks all CLI subcommands until restarted. Mitigated by the service file's `KeepAlive`/`Restart=on-failure`. `grafel status` must remain crash-safe — never return an error just because the daemon is down.
- All RPC methods must be backwards-compatible additions only; the client and daemon may be at different versions during an upgrade. Mitigated by a `Daemon.Version` RPC and a refusal-to-talk policy when major versions differ.

**Migration**

Users on any prior binary reinstall via `grafel install`. There is no compat shim; the standalone subcommands listed above return helpful errors pointing at the new install path.

## Amendment (2026-05-20) — Per-repo state isolation under `GRAFEL_DAEMON_ROOT` (issue #745)

When parallel coding agents each run an isolated daemon (different `GRAFEL_DAEMON_ROOT`s) against a shared fixture corpus, the socket is isolated (this amendment originally said "the socket and registry" — that was wrong; see the 2026-08-19 amendment below) but the per-repo state directory — `<repo>/.grafel/` per ADR-0007 — is shared. Two daemons indexing the same fixture race on `graph.json` and corrupt each other's outputs.

This amendment narrows the daemon's reach into the repo working tree:

- **Default behavior (no env var set) is unchanged.** Per-repo state continues to live in `<repo>/.grafel/` per ADR-0007. Single-user installs are not affected.
- **When `GRAFEL_DAEMON_ROOT` is set,** the daemon writes ALL per-repo state under

  ```
  $GRAFEL_DAEMON_ROOT/state/<sha256(abs_repo_path)[:16]>/
  ```

  instead of into the repo working tree. The hash segment is deterministic (same repo → same segment across processes and hosts), filesystem-safe, and collision-resistant.
- **The repo's own `.grafel/` is never created or modified by the daemon under this mode.** Pristine read-only fixture corpora stay pristine, no matter how many parallel agents index them.
- **Group-level metadata that lives co-located by design** (`<repo>/.grafel/group.json`, written by the wizard for repo-to-group discovery via CWD walk) is NOT routed through the helper. That file is a configuration marker, not state, and must be discoverable regardless of which daemon is running.

The helper is `internal/daemon.StateDirForRepo(repoPath)` (and the convenience wrapper `GraphPathForRepo`). All indexer write paths, reader paths (audit, dashboard, MCP, doctor, status, watch, links), and enrichment/repair persistence go through it. The variable name `GRAFEL_DAEMON_ROOT` becomes the switch for socket + per-repo state isolation. (This sentence originally claimed "the full three-dimensional isolation (socket + registry + per-repo state)". It never isolated the registry — corrected in the 2026-08-19 amendment below.)

This is a deliberate, env-gated exception to ADR-0007. ADR-0007's co-located default remains the right model for the single-user installation it was written for; the daemon-root override exists solely for the parallel-agent development workflow.

## Amendment (2026-08-19) — `GRAFEL_DAEMON_ROOT` never isolated the registry (issue #6331)

The 2026-05-20 amendment above states twice that `GRAFEL_DAEMON_ROOT` isolates the registry: "the socket and registry are isolated", and "the single switch for the full three-dimensional isolation (socket + registry + per-repo state)". **Both statements were wrong from the day they were written**, and `AGENTS.md` repeated the second one.

The registry has never consulted `GRAFEL_DAEMON_ROOT`. `registry.HomeDir` reads `GRAFEL_HOME`, and falls back to `~/.grafel` otherwise:

```go
func HomeDir() (string, error) {
    if override := os.Getenv("GRAFEL_HOME"); override != "" {
        return override, nil
    }
    home, err := os.UserHomeDir()
    ...
    return filepath.Join(home, ".grafel"), nil
}
```

So `GRAFEL_DAEMON_ROOT` alone moves the socket, the pidfile, the logs and per-repo state, while `registry.json`, the group overlays, the embeddings cache and the progress sidecars all stay under the **real** home. That is not isolation, it is a private daemon pointed at the live store — and the daemon's startup tail runs `MigrateToRefStore` and `PruneStaleGenerations` against exactly that store (#6134). An agent following the old text got a daemon it believed was sandboxed and let it relocate and prune the developer's real data.

Consequently:

- **`GRAFEL_DAEMON_ROOT` alone is refused** by the CLI's isolation guard (`internal/envguard`, #6331), as is `GRAFEL_HOME` alone. Neither variable on its own is isolation.
- **Full isolation is all three together**: `HOME`, `GRAFEL_HOME` and `GRAFEL_DAEMON_ROOT` — which is what `testsupport.IsolateHome` sets.

  ```sh
  export HOME=$(mktemp -d)
  export GRAFEL_HOME=$HOME/.grafel
  export GRAFEL_DAEMON_ROOT=$HOME/.grafel
  ```
- Everything the 2026-05-20 amendment says about **per-repo state** (`$GRAFEL_DAEMON_ROOT/state/<sha256(abs_repo_path)[:16]>/`, `StateDirForRepo`, pristine fixtures) is unaffected and still correct. Only the registry claim was false.
- The fact is pinned by `TestHomeDirIgnoresDaemonRoot` (`internal/registry/home_ignores_daemon_root_6331_test.go`), so this text and the code cannot drift apart again silently. If that test ever fails because the daemon root DOES move the registry, this amendment and the guard's daemon-root leg must be revisited together.
