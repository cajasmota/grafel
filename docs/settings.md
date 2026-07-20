# Settings file reference

grafel persists user preferences in `~/.grafel/settings.json`.
The file is created automatically on first write (via the Settings surface
or the `PUT /api/settings` endpoint). All fields are optional; missing keys
fall back to the defaults below.

## Schema

```json
{
  "theme": "light",
  "default_group": "",
  "auto_check_updates": true,
  "update_channel": "stable",
  "refresh_schedule": "",
  "telemetry_enabled": false,
  "daemon_rss_budget_mb": 512,
  "watcher_debounce_secs": 2,
  "indexer_parallelism": 4,
  "log_level": "info"
}
```

## Field reference

| Field | Type | Default | Allowed values | Notes |
|-------|------|---------|----------------|-------|
| `theme` | string | `"light"` | `"light"` \| `"dark"` \| `"auto"` | `"auto"` follows the OS dark-mode preference |
| `default_group` | string | `""` | Any registered group slug | Group shown on first dashboard load; empty = most recently used |
| `auto_check_updates` | bool | `true` | `true` \| `false` | Polls GitHub releases on daemon start |
| `update_channel` | string | `"stable"` | `"stable"` \| `"dev"` | `"dev"` includes release candidates |
| `refresh_schedule` | string | `""` | cron expression or `""` | Empty = manual-only refresh; e.g. `"0 3 * * *"` for 03:00 daily |
| `telemetry_enabled` | bool | `false` | `true` \| `false` | Opt-in anonymous usage metrics |
| `daemon_rss_budget_mb` | int | `512` | 100–32768 | Admission budget, in MB, for predicted concurrent index-job RSS. **Requires daemon restart.** |
| `daemon_go_memory_limit_mb` | int | automatic | 100–32768 | Optional Go runtime soft memory limit. When omitted, Grafel uses the upstream fraction-of-RAM formula and clamps it to 2048–2560 MB. **Requires daemon restart.** |
| `watcher_debounce_secs` | int | `2` | 1–60 | How long to wait after a file change before triggering a re-index |
| `indexer_parallelism` | int | `4` | 1–32 | Number of parallel goroutines used during indexing. **Requires daemon restart.** |
| `log_level` | string | `"info"` | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | Daemon log verbosity |

## CPU / concurrency / reindex environment variables

These knobs are read from the daemon's environment (not `settings.json`) and
let you dial CPU usage without recompiling. They take effect on the next daemon
start — set them in whatever launches `grafel start` (your shell profile,
a launchd/systemd unit, etc.). Issues [#5134](https://github.com/cajasmota/grafel/issues/5134)
and [#5135](https://github.com/cajasmota/grafel/issues/5135).

grafel distinguishes two reindex paths and caps them independently:

- **Background reindex** — the watcher/scheduler re-indexing a repo after a
  file save, merge, or branch switch. On a high-churn repo this can fire
  continuously, so it is throttled by default.
- **Explicit rebuild** — a user-triggered `grafel rebuild` / `grafel index`
  (or a dashboard rebuild). You are waiting on it, so it runs at host speed.

The CPU caps below only bite when the **subprocess extractor** is enabled
(`GRAFEL_SUBPROC_EXTRACT=1`), which forks one `grafel extract` child
per file batch. When it is off (the default), the daemon extracts in-process and
`GRAFEL_DAEMON_GOMAXPROCS` / the generic `GOMAXPROCS` are the relevant knobs.

| Env var | Default | Path | Effect / when to change |
|---------|---------|------|--------------------------|
| `GRAFEL_EXTRACT_GOMAXPROCS` | `2` | background | `GOMAXPROCS` set on each **background** extract subprocess. Total extract draw ≈ `concurrency × this`. Lower to `1` to throttle a daemon that's burning CPU on a high-churn repo; raise if background reindexes feel slow and the host is idle. |
| `GRAFEL_REBUILD_GOMAXPROCS` | host cores (`NumCPU`) | explicit | `GOMAXPROCS` set on each **explicit-rebuild** extract subprocess. Defaults to the full host so a user-triggered rebuild runs fast (it is no longer throttled by the background cap — [#5135](https://github.com/cajasmota/grafel/issues/5135)). Lower it if explicit rebuilds on a shared host need to leave headroom for CI. |
| `GRAFEL_EXTRACT_CONCURRENCY` | auto (`NumCPU/2`, capped at 4 background; `NumCPU` explicit) | both | Hard ceiling on the number of concurrent extract subprocesses, honored on **both** paths. Set to `1` as an emergency throttle on a contended machine — it caps explicit rebuilds too. |
| `GRAFEL_DAEMON_GOMAXPROCS` | ~half host cores (`NumCPU/2`, floor 1) | in-process | Caps the **daemon's own** Go-runtime parallelism (in-process extraction, reindex, GC, algorithm passes) without the generic `GOMAXPROCS`. **Resource-safe default (v0.1.1):** unset → half the host cores so the daemon can't saturate every core on a fresh install. Use this when in-process indexing is the CPU source. **Tradeoff:** query handling shares the same process, so lowering this also lowers the ceiling on concurrent query throughput. Raise it (up to host cores, which disables the cap) for faster reindex on an idle box. |
| `GOMAXPROCS` | host cores | in-process | Standard Go knob. Caps the entire daemon process (queries **and** in-process indexing). Prefer `GRAFEL_DAEMON_GOMAXPROCS` (same effect, grafel-scoped name) or the per-subprocess caps above, which don't touch query latency. |
| `GRAFEL_INCREMENTAL_REINDEX` | `1` (on) | background | Diff-aware incremental reindex: single-file edits re-extract only the changed files (~25× faster) instead of a full repo reindex on every settle, with a safe fall-through to full reindex on any precondition failure. **Default-on since #5231.** Set to `0`/`false` to force the legacy full-reindex-every-time behaviour. |
| `GRAFEL_SUBPROCESS_INDEXER` | `1` (on) | background | Runs each reindex job as a short-lived child process (`grafel index-internal`) bounded to `GRAFEL_EXTRACT_GOMAXPROCS` (default 2) cores, instead of indexing in-process at the daemon's `GOMAXPROCS`. **Resource-safe default (v0.1.1):** keeps the daemon heap flat AND bounds per-reindex CPU so background work can't saturate the host. Set to `0`/`false` to force the legacy in-process path. |
| `GRAFEL_REBUILD_CONCURRENCY` | auto (memory-tuned, floor 2, cap 16) | explicit | Number of **repos** indexed in parallel during a group rebuild (distinct from subprocess fan-out *within* a repo). Auto-tuned from system RAM; override to bound memory on constrained hosts. (Legacy alias: `GRAFEL_MAX_CONCURRENT_GROUPS`.) |

### Runtime reload without restart (#5137)

The four CPU/concurrency caps —
`GRAFEL_EXTRACT_GOMAXPROCS`, `GRAFEL_REBUILD_GOMAXPROCS`,
`GRAFEL_EXTRACT_CONCURRENCY`, and `GRAFEL_DAEMON_GOMAXPROCS` — can be
changed **without restarting the daemon** via a JSON config file at
`~/.grafel/cpu.json` (under `$GRAFEL_DAEMON_ROOT` when set):

```json
{
  "extract_gomaxprocs": 1,
  "rebuild_gomaxprocs": 8,
  "extract_concurrency": 2,
  "daemon_gomaxprocs": 4
}
```

Precedence per knob is **env var > `cpu.json` > built-in default** (an explicit
`--flag`/config field still wins over all three). The env vars themselves are
still read once at process start and cannot change in a running daemon —
`cpu.json` is the live-mutable surface.

- The three **per-subprocess** caps (`extract_gomaxprocs`,
  `rebuild_gomaxprocs`, `extract_concurrency`) are re-read cheaply (mtime-cached)
  at the start of **every reindex**: edit `cpu.json` and the new value applies on
  the next reindex, no restart.
- The **in-process** `daemon_gomaxprocs` is applied live on `SIGHUP`
  (`kill -HUP <daemon-pid>`): the daemon re-reads `cpu.json` and calls
  `runtime.GOMAXPROCS` immediately. Clearing the cap restores the host default.

The remaining knobs in this doc (`daemon_rss_budget_mb`, `daemon_go_memory_limit_mb`, `indexer_parallelism`,
`GRAFEL_REBUILD_CONCURRENCY`, the `GRAFEL_MAX_*` budgets) are still
**read at daemon start** and require `grafel restart` to apply.

## API endpoints

### Memory precedence

Grafel uses two independent memory controls. `daemon_rss_budget_mb` limits the
predicted incremental RSS admitted for concurrently running index jobs; it is
not a process RSS cap. `daemon_go_memory_limit_mb` sets Go's soft runtime memory
limit, which increases GC pressure near the configured value but may be
exceeded transiently.

The Go soft-limit precedence is `GOMEMLIMIT` >
`GRAFEL_DAEMON_MEMLIMIT_MB` > `daemon_go_memory_limit_mb` > the built-in
fraction-of-RAM default. Omitting the JSON key therefore preserves upstream
behavior exactly.

For example, a workstation can opt into an 8 GB admission budget and an 8 GB
Go soft limit while leaving all other settings unchanged:

```json
{
  "daemon_rss_budget_mb": 8192,
  "daemon_go_memory_limit_mb": 8192
}
```

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/settings` | Return current settings + defaults |
| `PUT` | `/api/settings` | Partial or full update; returns which fields require a restart |
| `POST` | `/api/settings/reset` | Restore factory defaults |

### `GET /api/settings` response

```json
{
  "settings": { ... },
  "defaults": { ... }
}
```

### `PUT /api/settings` response

The server merges the request body onto the current settings (partial PUT is
safe). Fields that require a daemon restart are listed in `restart_required`:

```json
{
  "settings": { ... },
  "defaults": { ... },
  "restart_required": ["daemon_rss_budget_mb"]
}
```

## Dashboard

The Settings surface at `/settings` in the dashboard provides a GUI for all
fields above and displays a "restart required" banner when applicable.

### Go profiling (`GRAFEL_DEBUG_PPROF`) — #5822

| Env var | Default | Effect |
|---------|---------|--------|
| `GRAFEL_DEBUG_PPROF` | unset (off) | Mounts the standard `net/http/pprof` handlers (`/debug/pprof/{heap,goroutine,profile,cmdline,symbol,trace,...}`) on the dashboard's HTTP mux. Set to `1`/`true`/`yes`/`on` to enable. |

OFF by default: profiling exposes heap/goroutine memory contents, so it must
be explicitly opted into. Even when enabled, the mount is further restricted
to a loopback dashboard bind (`127.0.0.1`/`::1`/`localhost`, the default) —
a non-loopback `dashboard.json` `bind` never exposes it, regardless of the
env var. Takes effect on the next daemon start; capture a heap profile with:

```sh
GRAFEL_DEBUG_PPROF=1 grafel start
curl -o heap.pprof http://127.0.0.1:47274/debug/pprof/heap
go tool pprof heap.pprof
```

Independently of the live endpoint, the Layer-2 CPU watchdog (issue #857)
already writes a goroutine stack dump to a temp file
(`grafel-hotloop-*.pprof.txt`) whenever it self-terminates a hot-looping
daemon; it now ALSO writes a paired heap profile
(`grafel-hotloop-*.heap.pprof`, same directory) at that moment, so a hot-loop
trip captures the heap without needing `GRAFEL_DEBUG_PPROF` to have been set
in advance.
