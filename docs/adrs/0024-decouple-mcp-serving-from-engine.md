# ADR-0024: Decouple MCP serving from the engine (serve/engine process split)

- **Status**: Accepted (Phase 0 landed; Phase 1 is the split; Phase 1b shards the fbwriter) — this ADR is the design for epic #5729
- **Date**: 2026-07-11
- **Deciders**: Jorge Cajas
- **Related**: ADR-0004 (single MCP process per machine), ADR-0016 (binary graph format), ADR-0017 (single-binary daemon architecture), ADR-0022 (HTTP MCP transport)
- **Refs**: epic #5729; Phase 0: #5726 / PR #5730 (fbwriter fail-soft + scheduler recover), #5717 / PR #5731 (MCP bridge resilience); convergence: #5725 (statusline status), #5727 (indexed SHA)

## Context

ADR-0017 collapsed grafel's three process families (per-invocation `grafel index`,
per-machine `grafel mcp serve`, per-repo watcher units) into **one** long-lived
`grafel daemon` supervised by the OS service layer (launchd `com.grafel.daemon`
with `KeepAlive`, systemd `Restart=on-failure`, Windows SCM). That daemon owns
*everything*: the MCP dispatch socket, the dashboard, the file watcher, the
scheduler, extraction subprocesses, enrichment, and the fbwriter that produces
`graph.fb`.

Bundling MCP dispatch with the engine has one structural failure mode: **any
engine fault takes down the MCP connection.** Concretely:

- A scheduler/fbwriter panic (e.g. the FlatBuffers 2 GiB builder cliff) crashes
  the whole process. `KeepAlive`/`Restart=on-failure` respawns it, but the
  in-flight MCP socket is severed and the attached AI agent sees a hard RPC
  failure.
- A `grafel update` / hot-deploy that only touches engine code still has to
  restart the entire daemon, dropping every live MCP session.
- Long engine work (a full reindex, an enrichment sweep) shares a process — and
  a fate — with latency-sensitive read queries.

The data layer, however, is **already decoupled**. This is the key enabling fact
and the reason the split is cheap:

- **Zero-copy mmap reads.** `internal/daemon/mcp/graph_cache.go:1-10` documents a
  concurrent LRU of lazily-`mmap`'d `graph.fb` handles that "decouples the
  indexer (which writes a fresh `graph.fb` on every successful index pass) from
  the MCP query handlers (which want zero-copy reads)." Readers never parse; they
  read FlatBuffers in place (ADR-0016/0017).
- **mtime-driven reload — the swap already exists.** `graph_cache.go:154-204`:
  `Get` stats the backing file, compares `info.ModTime().UnixNano()` against the
  `mtime` captured at open (`graph_cache.go:63`), and transparently reopens the
  `mmap` when the engine has written a newer `graph.fb`. Old handles are pinned
  until their readers finish, then `Munmap`'d (`graph_cache.go:331`). **A reader
  process needs no notification from the writer — a fresh atomic file is the
  entire handoff.**
- **Extraction is already subprocess-capable.** `internal/daemon/extract/subproc.go`
  runs an isolated extraction entrypoint (`Run`, `subproc.go:89`) fed a batch
  file, so heavy/faulty extraction is already fault-isolated from its coordinator.
- **Mutations already round-trip through files.** `submit_repair` validates
  against the cached graph and appends the resolution to `repair.json`
  (`internal/daemon/mcp/handlers.go:12-15,202-204`); `list_residuals` /
  enrichment touch `enrichment-candidates.json`. The write side is *already*
  a file drop, not an in-process call.

**Phase 0 has already been merged** to make each half survivable in isolation
before the processes are physically split:

- **fbwriter fail-soft + scheduler recover (#5726 / PR #5730).** The fbwriter
  now `recover()`s the oversized-graph marshal panic at its boundary and returns
  an error instead of aborting the process (`internal/graph/fbwriter/streaming.go:190-193,303-308`;
  `writer.go:41-42`), preserving the last-good `graph.fb`. The scheduler adds
  defense-in-depth `recover()` around an index pass
  (`internal/daemon/sched/scheduler.go:1156-1166`; regression tests
  `internal/daemon/sched/failsoft_5726_test.go`, `internal/graph/fbwriter/failsoft_5726_test.go`).
- **MCP bridge resilience (#5717 / PR #5731).** The daemon-hosted dispatch path
  now recovers a panicking tool handler instead of letting Go's runtime kill the
  process (`internal/daemon/mcp_rpc.go:312-344`, `callToolRecovered`), and the
  CLI bridge's reconnect budget was widened from ~450 ms to ~30-60 s of
  exponential-backoff retries so a daemon restart mid-call is ridden out rather
  than surfaced (`internal/cli/mcp_bridge.go:223-250,295-317`).

Phase 0 makes the single daemon *survive* faults. It does **not** deliver
independent deploy or true blast-radius isolation: the MCP socket still lives in
the same process as the engine, so an engine restart still drops it. That is what
this ADR resolves.

## Decision

Split the single `grafel daemon` into **two cooperating processes** that share
one on-disk seam:

### The two roles

**serve** — the MCP/read plane. Owns the MCP dispatch socket (the UDS /
Windows named pipe the CLI bridge dials, `internal/daemon/mcp_rpc.go`,
`layout.SocketPath`), the dashboard, and the zero-copy `graph_cache` of
`mmap`'d `graph.fb` handles. It holds **no heavy work**: no watcher, no
scheduler, no extraction, no fbwriter. The MCP bridge (`internal/cli/mcp_bridge.go`)
connects **here and only here**. Because serve does nothing but mmap-read and
dispatch, it is nearly impossible to crash and cheap to keep alive across engine
churn.

**engine** — the write plane. Owns the scheduler
(`internal/daemon/sched/scheduler.go`), the file watcher
(`internal/daemon/watch/watcher.go`), extraction (in-process + the
`extract/subproc.go` subprocesses), enrichment, and the fbwriter
(`internal/graph/fbwriter/`). It produces `graph.fb` **atomically** (write temp
+ `rename`). All of the engine's blast radius — the 2 GiB cliff, extractor bugs,
OOM during a full reindex — is confined here. Crashes and deploys are isolated:
restarting engine does not touch serve's MCP socket.

### The seam: the atomic `graph.fb`

The contract between the two processes is **the atomic `graph.fb` FlatBuffer,
nothing more**. engine writes a fresh `graph.fb` per successful pass and renames
it into place; serve's `graph_cache` detects the mtime bump on the next query and
swaps the mmap (`graph_cache.go:154-204`). No IPC, no shared memory coordination,
no notification protocol is required on the *read* path — the filesystem rename
*is* the publish, and mmap-on-mtime *is* the subscribe. This machinery already
exists and ships today; the split reuses it verbatim.

### Supervision model (the hard cross-platform requirement)

**The OS service layer supervises BOTH processes as ONE unit.** `grafel install`
provisions a single logical service; `grafel update` / `start` / `stop` /
`restart` each remain a single operation over that unit. The user never learns
that there are two processes.

Two supervision shapes were considered:

- **(A) OS supervises both** — launchd/systemd/SCM each own a serve unit and an
  engine unit directly.
- **(B) serve supervises engine** — the OS supervises serve; serve spawns and
  restarts engine as a child.

**Recommendation: (B), serve supervises engine, with the OS service layer as the
outer supervisor of serve.** Rationale:

1. **serve is the stable anchor.** It is the process least likely to die and the
   one that must never die (it holds the MCP socket). Making it the parent means
   the thing users depend on is the thing the OS keeps alive; engine is a
   restartable child whose death is a local event, not a service event.
2. **One unit, honestly.** The OS service definition points at a single program
   (`grafel serve`); the existing single-unit install/update/uninstall flow in
   `internal/install/*` and the single unit renderer
   (`internal/install/watchers/watchers.go` — `LaunchdPlist`, `SystemdUnit`,
   `SchtasksXML`) need to change only their `ProgramArgs`/`ExecStart` target,
   not grow a second unit per platform. This keeps `grafel install`/`update` a
   single provisioning op on every OS with **no** per-platform "two-unit"
   special-casing (which SCM in particular makes painful).
3. **Restart policy lives in one place.** serve owns engine's restart/backoff
   loop uniformly across platforms, rather than reproducing launchd `KeepAlive`
   vs systemd `Restart=on-failure` vs SCM `RestartOnFailure` semantics twice.
   The engine child gets a crash-loop backoff and a health gate; serve keeps
   answering reads from last-good `graph.fb` throughout.
4. **Update-path aware.** `grafel update` swaps the binary and restarts serve;
   serve then relaunches engine from the new binary. An engine-only hot-deploy
   (the dev-loop case) can restart just the engine child without disturbing the
   MCP socket — the exact property this ADR exists to deliver.

Trade-off accepted: if serve itself is killed, the OS respawns serve, which
respawns engine — a double hop, but serve start-up is trivial (bind socket, open
mmap lazily) so this is sub-second. The alternative (A) avoids the double hop but
pays for it with two units per platform and split-brain restart semantics; not
worth it.

### Control plane (serve → engine, for the few mutating/triggering tools)

Most tools are pure reads and need no control plane — they resolve entirely from
serve's mmap. Only a handful **trigger or mutate** engine state:
`submit_repair`, `docgen_apply`, enrichment enqueue, and reindex/reindex-trigger.

These already lean on files: `submit_repair` appends to `repair.json`
(`handlers.go:202-204`), enrichment reads/writes `enrichment-candidates.json`.
The engine already watches the repo state dir. So the control plane is a **thin
serve → engine request channel**, with a deliberately conservative default:

- **Default (Phase 1): request-file queue.** serve writes a request record
  (repair resolution, docgen-apply payload, enqueue/reindex request) into a
  well-known `requests/` drop-dir under the repo state dir; engine's watcher/
  scheduler picks it up, acts, and writes a small ack/result file serve can
  report on. This reuses the atomic-file-drop pattern already proven by
  `repair.json`, needs no new transport, and is trivially crash-safe (a
  request is either fully written-and-renamed or absent).
- **Optional (Phase 1 hardening): a small control UDS.** For low-latency,
  request/response triggers (e.g. a synchronous "reindex now and tell me when
  the new graph.fb is live"), serve dials a **second** unix socket that engine
  listens on — mirroring the existing daemon RPC UDS pattern
  (`layout.SocketPath`, Windows named pipe). This is an add-on, not a
  replacement; the file queue remains the durable fallback.

The control plane is **narrow by design**: it carries triggers and mutation
proposals only. It never carries graph data — that always flows the other way,
through the `graph.fb` seam.

### Status / liveness plane (engine → serve)

serve must be able to answer "is the graph fresh / is indexing in flight /
how big is it" **without** talking to the engine on the hot path, and must degrade
gracefully when engine is down. So the engine writes a small **status/heartbeat
file** (atomic write + rename) that serve reads. This converges the split with
#5725 (statusline status) and #5727 (indexed SHA) — those features want exactly
this data, and after the split there is a single canonical producer for it.

Fields (superset of today's `proto.StatusReply` /
`proto.IndexedRepoState`, `internal/daemon/proto/proto.go`):

- **Liveness**: `engine_pid`, `engine_started_at`, `heartbeat_at` (monotonic
  tick; staleness ⇒ serve reports engine degraded/dead), `binary_version`.
- **Freshness (per repo/ref)**: `indexed_ref`, `indexed_commit` (the SHA — #5727),
  `last_index_at`, `last_algo_at`, `entity_count`, `edge_count`
  (from `SchedulerEntityCount`, `internal/daemon/server.go:104-108`),
  `graph_fb_path`, `graph_fb_mtime`.
- **Progress / busy**: `indexing_in_progress` / `index_in_flight[]`, `queue_len`,
  `pending_algo[]`, `pending_links[]`, `busy` (any admission-blocked or in-flight
  work), `last_err` per repo (`IndexedRepoState.LastErr`).

serve reads this file on `Status` RPC and for the dashboard; if the file is
stale or missing, serve reports **degraded** but keeps serving reads from the
last-good `graph.fb`. The heartbeat file is the liveness contract; the `graph.fb`
seam is the data contract. Neither requires a live socket to the engine.

## Phasing

- **Phase 0 — survivability (DONE).** fbwriter fail-soft + scheduler recover
  (#5726/PR #5730); MCP bridge resilience: handler-panic recovery + widened
  reconnect budget (#5717/PR #5731). The single daemon now survives faults but
  still couples the MCP socket to the engine.
- **Phase 1 — the split.** Introduce `grafel serve` (MCP + dashboard + graph_cache
  mmap reads) and `grafel engine` (scheduler + watcher + extraction + enrichment
  + fbwriter). serve supervises engine; OS service layer supervises serve as one
  unit. Add the status/heartbeat file and the request-file control queue. Update
  `internal/install/*` + `watchers.go` unit renderers to target `grafel serve`.
- **Phase 1b — shard the fbwriter (#5726 part 2).** Remove the FlatBuffers 2 GiB
  builder cliff by sharding `graph.fb` (per-repo/per-namespace segments) so the
  engine never marshals a single >2 GiB buffer. serve's graph_cache already keys
  on absolute `graph.fb` path (`graph_cache.go:10`), so multi-file shards are a
  natural extension of the existing per-repo handle model.
- **#5717 follow-ups (fold into Phase 1):** (a) *fail-faster on a genuinely dead
  daemon* — distinguish "engine restarting" (retry) from "serve dead" (fail fast)
  now that they are separate processes, so the bridge's widened budget doesn't
  mask a truly dead serve; (b) *ctx-cancel through the bridge* — thread the MCP
  client's cancellation through `mcp_bridge.go` → serve so an abandoned request
  stops consuming serve resources.

## Alternatives considered

**(a) Single process + recover only (= Phase 0).** Keep one daemon, rely on
panic recovery + fail-soft + OS `KeepAlive`. *Rejected as the end state:* it makes
faults survivable but leaves the MCP socket coupled to the engine, so an engine
restart or engine-only deploy still drops live MCP sessions. Necessary
groundwork, insufficient for independent deploy. **Kept as Phase 0.**

**(b) Subprocess-isolate only the engine within one daemon.** One daemon process
hosts MCP+dashboard and forks the scheduler/extraction/fbwriter as a managed
child, but everything still lives under one OS service identity and one binary
lifecycle with the child re-parented on every restart. *Rejected:* it delivers
crash isolation but not clean *deploy* isolation or a clean process boundary for
the MCP socket, and it muddies the supervision story (the daemon is both an MCP
server and a bespoke process supervisor with no OS-level identity for the child).
The chosen model (B supervision) is essentially this made honest: serve *is* the
MCP server AND the engine's supervisor, but engine is a first-class process, the
seam is an explicit atomic file, and the control/status planes are explicit.

**(c) Full serve/engine split (CHOSEN).** Two processes, atomic `graph.fb` seam,
serve supervises engine, OS supervises serve as one unit. *Trade-off:* a second
process to reason about and two new thin planes (control file-queue, status
heartbeat file) to maintain. *Why it wins:* it is the only option that isolates
engine crashes AND engine deploys from the MCP connection, and it costs little
because the read seam (mmap-on-mtime) and the write seam (file drops) already
exist. The extra surface is small and explicit.

## Consequences

**Positive**
- Engine crashes and engine-only deploys no longer drop the MCP connection —
  the core goal of #5729.
- Read latency is insulated from heavy engine work (separate process, separate
  scheduler/GC/memory budget).
- The dev hot-deploy loop can restart just the engine child, leaving MCP sessions
  live.
- Single canonical producer for status/freshness data (#5725/#5727 converge here).
- Reuses proven machinery: zero-copy mmap reads, mtime reload, atomic file drops,
  the UDS RPC pattern.

**Negative / cost**
- A second process to supervise, monitor, and version-match (serve and engine
  must run the same binary version; `grafel update` must swap both atomically —
  serve relaunches engine from the new binary).
- Two new thin planes (control request-queue, status heartbeat) to build and
  test.
- serve↔engine version skew and the double-hop respawn (OS→serve→engine) are new
  failure surfaces to cover with tests.

## Migration & risk

- **Backward compatibility.** Existing installs run a single `com.grafel.daemon`
  unit. `grafel update` on an old install rewrites the unit to target
  `grafel serve` (which then spawns engine) — a one-time, idempotent unit
  rewrite handled in `internal/install/update.go` + `watchers.go`. The socket
  path, state dir layout, and `graph.fb` location are unchanged, so old MCP
  bridges and dashboards keep working against serve with no client change.
- **Rollout.** Ship behind an install-time capability flag; default off for one
  release, then flip. `grafel doctor` gains serve/engine liveness checks
  (`internal/install/doctor.go`).
- **Failure modes.** engine down ⇒ serve reports **degraded** (stale heartbeat)
  and keeps answering reads from last-good `graph.fb`; no MCP disruption. serve
  down ⇒ OS respawns serve ⇒ serve respawns engine; the bridge's widened
  reconnect budget (#5717) rides out the gap. graph.fb write failure ⇒ fail-soft
  preserves last-good (#5726), serve keeps serving the previous graph.
- **Testing strategy.** Extend `internal/daemon/daemon_test.go` to a two-process
  harness: kill engine, assert serve stays up and reads succeed from last-good;
  kill serve, assert OS/parent respawns and engine is re-parented; version-skew
  guard; heartbeat-staleness ⇒ degraded; request-queue crash-safety (partial
  write never acted on). Add the `#5717` fail-faster and ctx-cancel cases.

## Implementation-plan skeleton (workstreams for epic #5729)

1. **Process boundary & binary entrypoints.** Add `grafel serve` and
   `grafel engine` subcommands; carve the current `runDaemon`
   (`cmd/grafel/daemon.go:401`) into a serve half (socket + dashboard +
   graph_cache) and an engine half (scheduler + watcher + extraction + fbwriter).
   Keep `grafel daemon` as a compatibility shim that launches serve.
2. **Supervision (serve→engine) + OS unit retarget.** Engine child spawn +
   crash-loop backoff + health gate in serve. Retarget `LaunchdPlist` /
   `SystemdUnit` / `SchtasksXML` (`internal/install/watchers/watchers.go`) and
   the install/update/uninstall flow (`internal/install/{install,update,uninstall}.go`)
   to the single `grafel serve` unit. Verify one-unit UX on macOS/Linux/Windows.
3. **Status / heartbeat plane.** Define the heartbeat file schema (superset of
   `proto.StatusReply`/`IndexedRepoState`); engine writes atomically on each
   tick/pass; serve reads it for `Status` RPC + dashboard + degraded detection.
   Wire #5725 statusline and #5727 indexed-SHA onto it.
4. **Control plane.** `requests/` drop-dir + ack protocol for `submit_repair`,
   `docgen_apply`, enrichment enqueue, reindex trigger; move the existing
   `repair.json`/`enrichment-candidates.json` writers behind it. Optional control
   UDS for synchronous triggers.
5. **#5717 follow-ups.** Fail-faster-on-dead-serve (distinguish restart vs dead
   in `mcp_bridge.go`); ctx-cancel propagation bridge→serve.
6. **Phase 1b — fbwriter sharding (#5726 part 2).** Shard `graph.fb` to remove
   the 2 GiB marshal cliff; extend graph_cache to fan across shard files.
7. **Test harness & doctor.** Two-process daemon_test scenarios; `grafel doctor`
   serve/engine liveness + version-skew checks.
