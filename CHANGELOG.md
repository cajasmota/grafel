# Changelog

All notable changes are documented here. Entries follow
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions.
PR numbers link to https://github.com/cajasmota/grafel/pull/<N>.

---

## [Unreleased]

### Added

- **The wizard (CLI + web) now lets you choose which AI tools get the grafel
  MCP server (#5344):** instead of auto-registering the grafel MCP entry in
  every detected AI tool (Claude Code, Cursor, Windsurf, Codex, Kiro,
  Antigravity), the setup wizard adds a "Configure MCP for which tools?" step
  that lists the detected tools and lets you pick. The default is a smart
  pre-selection — a tool is checked when its MCP config was modified recently
  (within ~30 days) **or** already contains a grafel entry — and the wizard
  **remembers your last choice** (persisted to `~/.grafel/mcp-tools.json`),
  defaulting to it on subsequent runs. The step is skipped automatically when
  ≤1 tool is detected. For scripting, `grafel wizard --mcp-tools=claude,cursor`
  registers exactly those tools and `--no-mcp` registers none; without either
  flag, non-interactive runs keep today's behavior (register every detected
  tool) so existing scripts are unaffected
  ([#5344](https://github.com/cajasmota/grafel/issues/5344)).

### Fixed

- **Graph-view rebuild toast now reports the graph's real entity/relationship
  totals instead of "0 entities, 0 relationships" (#5326):** after a background
  rebuild on the graph view, the completion toast read "rebuilt N repo(s): 0
  entities, 0 relationships" even for a fully populated graph (e.g. 3,888
  entities). `RebuildReply.TotalEntities`/`TotalRels` were left at 0 on the
  progress path because the session totals were never accumulated. The daemon
  now sums the per-repo `graph-stats.json` sidecars (the same cheap, mmap-free
  source the CLI rebuild summary uses) into the reply on rebuild completion, so
  the toast shows the real totals. When a rebuild legitimately has nothing to
  report, the toast now reads "up to date" instead of "0 entities"
  ([#5326](https://github.com/cajasmota/grafel/issues/5326)).
- **Dashboard index wizard now shows one row per repo instead of a single group
  row (Refs #5340, #5326):** the "Index a new group" dialog's Index step could
  collapse to a single row labelled with the GROUP name (e.g. "ivivo · Indexed")
  rather than one row per repo (backend + frontend). This is the web analog of
  the CLI fixes (#5343/#5348). Four changes, mirroring the Go wizard: (1) the
  group-scoped progress event (`repo_slug === group`, the cross-repo links/flows
  pass) is excluded from the per-repo rows and its phase is surfaced in the
  OVERALL header label instead of spawning a spurious group row; (2) the Index
  step seeds one pending row per expected repo from the registered repo list, so
  every repo shows up front and survives dropped early SSE events; (3) the
  progress SSE subscription is opened as soon as the from-scan POST fires —
  before the index starts — so fast indexes don't miss early per-repo events;
  (4) on successful completion every non-terminal row is finalized to Done so
  the final frame shows all repos Done. The single-repo case is unchanged
  ([#5340](https://github.com/cajasmota/grafel/issues/5340)).
- **Wizard indexing view now marks all per-repo rows Done on successful
  completion (#5340):** previously a repo could freeze on its last intermediate
  phase (e.g. "Building communities…") if its final SSE events (centrality →
  writing → done) arrived after the Rebuild RPC returned done and the forwarder
  stopped. The overall bar reached "Done · 100%" but the stuck row disagreed. On
  a successful `IndexOutcome` the view now finalizes every non-terminal row to
  Done (preserving its files/entities counts); failures leave rows as-is.
- **Daemon startup no longer deadlocks when `canonicalizePath`'s `os.ReadDir`
  blocks on a slow filesystem (#5330):** at startup the daemon canonicalizes
  each known repo path by walking its ancestors with a per-segment `os.ReadDir`,
  previously with no timeout. If a single ancestor's FS call hung (a
  `~/Documents` iCloud/Spotlight/TCC stall, a slow/unresponsive mount, or an
  observed launchd-context permission stall) the entire startup blocked forever
  and the daemon never began serving — the hang was only visible via a SIGQUIT
  goroutine dump. Each `os.ReadDir` is now bounded by a timeout (default 3s,
  overridable via `GRAFEL_CANONICALIZE_TIMEOUT_MS`); on timeout `canonicalizePath`
  degrades to preserving the input casing for that and remaining segments — the
  same fallback it already takes on a read error — and logs a WARN with the
  offending path. A new `startup: state-migration begin` log is emitted before
  the migration runs so a wedge here is diagnosable without a goroutine dump
  ([#5330](https://github.com/cajasmota/grafel/issues/5330)).

- **`grafel wizard` TUI renders correctly on legacy Windows CMD/conhost
  (#5340, #856):** the wizard's Bubble Tea TUI used Unicode glyphs (`›`, `✓`,
  `↑/↓`, `·`, `…`, `⚠`, `[✓]`, the braille spinner) with no console code-page
  setup, so outside Windows Terminal it could render as mojibake. The TUI now
  (1) switches the Windows console to the UTF-8 code page (65001) at startup and
  restores the previous code page on exit, so a modern CMD/conhost with a
  TrueType font shows the Unicode glyphs; and (2) falls back to an ASCII-safe
  glyph set (`>`, `v`, `^/v`, `-`, `...`, `!`, `[x]`, `|/-\` spinner) on legacy
  consoles. The glyph set is chosen by a single, unit-tested selector: ASCII on
  Windows unless `WT_SESSION` (Windows Terminal) or `GRAFEL_TUI_UNICODE=1` is
  set; `GRAFEL_ASCII=1` forces ASCII on any OS; non-Windows defaults to Unicode
  ([#5340](https://github.com/cajasmota/grafel/issues/5340),
  [#856](https://github.com/cajasmota/grafel/issues/856)).
- **`grafel wizard` indexing view reliably shows one row per repo (#5340):** the
  TUI indexing screen could collapse to a single row labeled with the GROUP name
  (e.g. "ivivo") reaching Done instead of one row per repo (backend, frontend).
  Two causes are fixed: (1) the wizard now establishes the broker SSE
  subscription **before** triggering the Rebuild RPC, so the early per-repo
  extraction events aren't missed when the index runs fast (previously the
  rebuild fired first and the per-repo events had already replayed by the time we
  subscribed); and (2) the group-scoped event (the cross-repo links/flows pass,
  whose `repo_slug` equals the group) is no longer folded into a spurious
  per-repo row — its phase surfaces in the overall "Indexing &lt;group&gt; — …"
  label instead ([#5340](https://github.com/cajasmota/grafel/issues/5340)).
- **Watcher install is robust to the flaky launchctl err-5 and never aborts the
  wizard (#5338):** `launchctl bootstrap` intermittently fails the first
  bootstrap of a freshly written plist with exit code 5 ("Bootstrap failed: 5:
  Input/output error"). The macOS watcher loader now retries the
  bootout→bootstrap pair (bounded, with a small backoff) **specifically on err
  5**, which clears the transient failure. And a watcher that still fails to
  activate is now a **non-fatal warning** instead of an abort: the group config
  is already persisted, so the wizard completes and the group indexes
  ("warning: watcher for X not activated: …; the group is still registered and
  will index") ([#5338](https://github.com/cajasmota/grafel/issues/5338)).
- **Deleting a group now cleans up its watcher launchd/systemd/schtasks units
  (#5338):** group delete (CLI `grafel delete`, dashboard `DELETE
  /api/v2/groups/{group}`) previously left `com.grafel.watcher.<group>.*` jobs
  loaded and plists on disk, so recreating the group fought stale state. Delete
  now Unloads (bootout) and removes the watcher unit + plist for every repo in
  the group — idempotent, tolerating "not loaded"
  ([#5338](https://github.com/cajasmota/grafel/issues/5338)).

### Changed

- **`grafel wizard` TUI polish — per-screen context, light-blue accent, and a
  captured Done summary (#5340):** each step now carries a concise explanatory
  subtitle so it's clear what's being configured (what indexing a single repo /
  group / monorepo means; "Choose which repositories to include in this group";
  the Name screen names the actual repos being grouped; the optional-docs screen
  explains shared markdown docs). The accent (header badge, step-rail pills,
  cursor `›`, selected/active highlights) moved from pink/magenta to a tasteful
  **light blue** (adaptive 256-color `117`/`75`), keeping green for done/✓. And
  the install/index output that previously scattered over the alt-screen on
  completion ("saved …", "installed N hooks/watchers/MCP", watcher warnings) is
  now **captured and rendered inside the Done screen** — a clean summary line
  plus any watcher warning as a styled non-fatal note — so nothing prints
  misaligned after the TUI tears down (the non-TTY/plain path still prints the
  normal messages) ([#5340](https://github.com/cajasmota/grafel/issues/5340)).
- **`grafel wizard` now uses a cohesive Bubble Tea TUI (#5340):** the
  interactive wizard is a single full-screen `tea.Model` state machine with
  consistent chrome on every step — a styled grafel header + a step rail
  (`Action › Select › Name › Index`) highlighting the current stage, a spacious
  body sized to the terminal (the four actions are always fully visible; long
  repo lists scroll inside a tall viewport with a position indicator), and a
  contextual footer key-hint bar on every screen (including the group-name and
  optional-docs inputs → "optional · enter to skip"). The indexing step is now a
  **per-repo view**: an overall progress bar plus **one row per repo** (name ·
  phase label · files done/total · entities · spinner while active), folding the
  broker SSE phase stream by `repo_slug` with a monotonic phase — which also
  **fixes the dropped-repo display bug** where the old single-line
  carriage-return renderer showed only one repo and dropped the rest. All
  decision logic and side effects are preserved (`ClassifyPath`, group-name
  default, `applyGroupConfig`, daemon auto-index); `ctrl-c`/`esc` cancel cleanly
  with nothing registered. Non-interactive flags
  (`--repos`/`--parent`/`--exclude`/`--no-index`) and non-TTY/CI/`$TERM=dumb`
  contexts never launch the TUI and keep the line-based flow
  ([#5340](https://github.com/cajasmota/grafel/issues/5340)).
- **`grafel wizard` auto-indexes the new group with live progress (#5338):**
  after registering a group (or adding repos to an existing one), the wizard now
  triggers an index by default and renders live CLI phase progress — the same
  broker event stream the dashboard shows — so it ends register → "Indexing…" →
  "Done". A `--no-index` flag skips it for scripting, and a down daemon is a
  warning (the group is registered and indexes later), not a failure
  ([#5338](https://github.com/cajasmota/grafel/issues/5338)).
- **Wizard group-name default is the container folder, not a child repo
  (#5338):** from `ivivo/` holding `backend/` + `frontend/`, the suggested group
  name is now `ivivo` (the common-parent container folder) instead of an
  arbitrary selected child repo's slug. A single selected repo still defaults to
  its own basename ([#5338](https://github.com/cajasmota/grafel/issues/5338)).
- **Wizard TUI gains navigation hints, more height, and `[ ]`/`[✓]` checkboxes
  (#5338):** each interactive step now shows a footer hint ("↑/↓ move · space
  select · enter confirm · / filter · esc back") so users discover that **space**
  toggles a multiselect item; the select/multiselect lists use more terminal
  height (and scroll when long) instead of a few cramped rows; and multiselect
  options now render explicit `[ ]`/`[✓]` brackets (the stock huh `ThemeCharm`
  glyphs `•`/`✓` were ambiguous — the fix sets the correct
  `SelectedPrefix`/`UnselectedPrefix` theme fields)
  ([#5338](https://github.com/cajasmota/grafel/issues/5338)).

### Changed

- **Index wizard is now action-first with smart cwd detection (#5336):** both the
  CLI (`grafel wizard`) and the dashboard scan wizard now open on an explicit
  action choice — **Index a single repository**, **Index a group of related
  repositories**, **Index a monorepo**, **Add a repository to an existing
  group** — with the cursor pre-placed on a smart default derived from the
  current directory. This fixes container folders: a parent directory holding
  multiple repos (e.g. `ivivo/` with `backend/` + `frontend/`) now resolves to
  exactly those child repos instead of scanning the cwd's PARENT for unrelated
  siblings. CLI and dashboard share a single source of truth — the new
  `detect.ClassifyPath` classifier — so they agree on what a folder is (its own
  git status, immediate child git repos, monorepo packages, sibling repos) and
  which action to suggest. The non-interactive `--repos`/`--parent`/`--exclude`
  flags are unchanged for scripting
  ([#5336](https://github.com/cajasmota/grafel/issues/5336)).
- **Granular graph-assembly progress phases in the CLI and wizard (#5334):** the
  graph-assembly tail used to collapse under one coarse "Materializing" /
  "Running algorithms" label, so the long post-extraction passes looked stuck.
  The real passes are now surfaced as live phases — **Building communities**
  (Louvain), **Computing centrality** (PageRank/Betweenness), **Computing
  flows** (process/event-flow walkers), **Writing graph**, and the group-level
  **Detecting cross-repo links** — with identical human labels in BOTH the
  `grafel index`/`rebuild`/`wizard` terminal output (live, TTY-aware) and the
  dashboard scan wizard. The coarse phases are retained as fallbacks
  ([#5334](https://github.com/cajasmota/grafel/issues/5334)).
- **Index wizard main progress bar now reflects real progress (#5332):** the
  wizard's top progress bar was driven solely by the coarse job poller, which
  barely moves during indexing — so it looked frozen near 0% even while every
  repo was at "Materializing"/"Indexed". It now derives a real aggregate from
  the per-repo feed (each repo contributes a phase-weighted fraction that
  advances as it crosses phase boundaries, including through the long
  sub-progress-less "Materializing" phase), and the header shows the current
  overall phase ("Scanning…", "Extracting AST…", …, "Materializing graph…",
  "Done") instead of a static "Indexing…". The active bar also gets a tasteful
  shimmer (respecting `prefers-reduced-motion`) so it never reads as stuck
  ([#5332](https://github.com/cajasmota/grafel/issues/5332)).
- **Windows / locale resilience (#5317):** swept the codebase for control flow
  that branched on a *localized* OS command output / error string — the bug
  class behind the Spanish-Windows `schtasks` install failure — and migrated the
  genuine occurrences to locale-invariant signals (process exit codes, on-disk
  unit-file checks, and typed `syscall.Errno` values). Migrated: the watcher
  `Unload()` for schtasks / systemctl / launchctl (`internal/install/watchers/`,
  which had the same English-text match as the original bug), the daemon
  launchd `Load()`/`Unload()` (`internal/daemon/service/launchd_darwin.go`), and
  the selftest Windows "file in use" detector (`cmd/grafel/selftest.go`, now
  errno-based). The daemon `schtasks` `Unload()` keeps its English match only as
  a documented best-effort race fallback behind the exit-code `IsLoaded()` check
  (Refs [#5317](https://github.com/cajasmota/grafel/issues/5317),
  [#856](https://github.com/cajasmota/grafel/issues/856)).
- Dashboard Paths → "Downstream flow" modal now defaults to and only shows the
  **Tree** view; the **Flowchart** view is hidden behind a `SHOW_FLOWCHART` flag
  pending a layout fix (it currently renders disconnected EXIT/ENTRY/RETURN
  fragments). The flowchart renderer is retained, just gated
  ([#5324](https://github.com/cajasmota/grafel/issues/5324)).

### Added

- **CI lint gate `lint-localematch` (#5317):** a standard-library Go AST analyzer
  (`cmd/lint-localematch`) that fails CI when new code matches command
  output/error text for control flow — `strings.Contains/HasPrefix/HasSuffix/
  Index/EqualFold` or `regexp.MatchString` whose subject is data-flow-derived
  from an `exec.Command(...).Output()/CombinedOutput()/Run()` result or an
  `err.Error()` in an exec-using file. Scoped to files that shell out (low false
  positives); justified race fallbacks opt out with `// nolint:localematch`.
  Wired into `make lint`, `pre-merge.yml`, and `test.yml`
  (Refs [#5317](https://github.com/cajasmota/grafel/issues/5317)).

### Fixed

- **Dashboard wizard showed only ONE repo of a multi-repo group** (Refs
  [#5326](https://github.com/cajasmota/grafel/issues/5326)). The feed-terminal
  fallback added for the freeze fix treated the per-repo SSE feed as terminal as
  soon as *every row seen so far* was `done`/`error`, without knowing how many
  repos to expect. Under the broker's drop policy the first repo could reach
  `done` before the second repo emitted a single event, so the feed looked
  terminal and the wizard tore the SSE subscription down — only one repo row
  ever rendered (which one was a race, hence inconsistent backend-vs-frontend),
  even though the backend correctly indexed all repos. The wizard now threads the
  EXPECTED repo count (the same repo list it registers to start indexing) into
  the feed: feed-terminal fires only once **all** expected repos have reported a
  terminal phase, and the job poller remains the primary terminal signal. The
  EventSource stays subscribed for the whole index, so every repo's rows render.
- **Dashboard wizard indexing froze mid-extraction and never showed completion;
  rebuild appeared to idle-wait ~5 min** ([#5326](https://github.com/cajasmota/grafel/issues/5326)).
  Three independent defects on the rebuild/progress path:
  - **Progress (UI freeze):** the indexer emits its terminal `done`/`error`
    progress event exactly once, but the broker's fan-out is best-effort
    (drop-on-full) — under load that single event could be dropped, leaving the
    wizard SSE stream sending only heartbeats and the UI frozen on the last
    mid-extraction frame. The broker now **retains the last terminal event per
    group** and the `/api/index-progress/{group}` SSE handler replays it on
    connect and re-asserts it on each heartbeat, so the terminal state is
    **always** rendered (then `close`), even if the live event was dropped or
    the client connected late.
  - **Goroutine leak (the "~5 min wait"):** the Rebuild RPC's dead-man heartbeat
    ran `for range ticker.C` with no exit path — `time.Ticker.Stop()` does not
    close the channel, so the goroutine blocked forever, leaking one goroutine
    per Rebuild RPC. The ticker goroutine is now torn down via a stop channel
    when the result lands. (The result itself was already delivered promptly via
    a buffered channel; the "5m0s" log line was the dead-man heartbeat, not a
    timeout the result waited on.)
  - **Diagnosability:** when the stall detector fires it now logs a bounded
    (≤1 MiB), once-per-RPC full goroutine dump (`runtime.Stack(_, true)`) so the
    next stall is root-causable from the daemon log alone. The warning interval
    is overridable via `GRAFEL_STALL_WARN_INTERVAL`.
  - **Wizard UX (frontend follow-up):** the index wizard's terminal state was a
    *separate* source of truth from the now-fixed SSE feed — it came only from
    the job poller (`/api/v2/jobs/{id}`), so a rebuild could finish (`rebuild:
    done` in the daemon log) while the poller hadn't flipped, leaving the button
    stuck on "Indexing…". The wizard now reaches **terminal ("Done")** when
    *either* the job poller flips *or* the per-repo SSE feed shows every repo
    `done`/`error` (the icon, label, progress bar and close-guard all follow this
    effective status). The progress feed now renders **one row per repo** keyed
    by repo slug — a repo-level event and its redundant module-scoped duplicate
    collapse into a single row showing the latest status (no more stale "… module"
    rows frozen mid-extraction). The wizard modal is also slightly larger
    (`max-w-lg`, capped height with internal scroll, taller feed area) so the feed
    no longer scrolls cramped.
- `grafel_find_callers` / `find_callees` / `neighbors` now resolve an entity by
  name or qualified name (not only the opaque entity_id), returning
  disambiguation candidates when ambiguous instead of a hard `entity not found`
  — fixes a ~35% error rate on name-based calls
  ([#5314](https://github.com/cajasmota/grafel/issues/5314)).
- **Windows (end-to-end, from user feedback):** the MCP bridge now dials the
  daemon via `transport.Dial`, selecting the named pipe on Windows (was a
  hardcoded AF_UNIX `net.Dial("unix", ...)`), so `/mcp` can connect; the offline
  stub also always carries a valid `inputSchema` so a connection failure surfaces
  clearly instead of as a cryptic Zod rejection (Refs [#856](https://github.com/cajasmota/grafel/issues/856)).
- **Windows (end-to-end, from user feedback):** `grafel install` no longer aborts
  on a clean install — `schtasks` Unload now checks existence via the exit-code
  based `IsLoaded()` instead of matching English-only error text (worked around a
  localized "cannot find the file" on non-English Windows) (Refs [#856](https://github.com/cajasmota/grafel/issues/856)).
- **Windows (end-to-end, from user feedback):** the task user SID is now resolved
  via the native `os/user` API instead of shelling out to `whoami /user`, which a
  PATH-shadowing MSYS/Git Bash `whoami` could break; an empty SID degrades to a
  conditional (omitted) `<UserId>` instead of invalid task XML (Refs [#856](https://github.com/cajasmota/grafel/issues/856)).
- **Windows (end-to-end, from user feedback):** the watcher PID registry now
  creates its state directory before taking the lock, fixing a non-fatal
  "system cannot find the path specified" on a fresh `%AppData%\grafel`
  (Refs [#856](https://github.com/cajasmota/grafel/issues/856)).
- **Windows (end-to-end, from user feedback):** cross-repo link passes now fall
  back to copying `graph.json`/`graph.fb` into the staging dir when `os.Symlink`
  fails for lack of `SeCreateSymbolicLinkPrivilege`, so cross-repo edges are no
  longer silently 0 without Developer Mode/admin (Refs [#856](https://github.com/cajasmota/grafel/issues/856)).

### Added

- **Windows CMD installer + manual install path.** A new `install.bat` provides
  a non-PowerShell, non-admin one-line install for `cmd.exe`
  (`curl -fL …/install.bat -o "%TEMP%\grafel-install.bat" && "%TEMP%\grafel-install.bat"`);
  it mirrors `install.ps1` exactly (same `%USERPROFILE%\.grafel\bin` prefix,
  release asset names, `checksums.txt` SHA256 verification, and user-PATH append)
  using only Windows 10 1803+ built-ins. New `docs/install-windows-manual.md`
  documents a step-by-step locked-down/air-gapped install, and the install docs
  (README, `docs/install.md`, `docs/quickstart.md`) now present all three Windows
  methods — PowerShell, CMD, and manual
  (Fixes [#5319](https://github.com/cajasmota/grafel/issues/5319), Refs
  [#5318](https://github.com/cajasmota/grafel/issues/5318),
  [#856](https://github.com/cajasmota/grafel/issues/856)).

---

## [0.1.1] — 2026-06-20

### Fixed

- **Reindex is now resource-safe by default** ([#PR](https://github.com/cajasmota/grafel/pull/5310)) —
  background reindexing no longer saturates the host on a fresh `curl|bash`
  install. Previously the good behaviour was env-var-gated, so a clean install
  ran the indexer in-process at full host cores with no per-job CPU bound,
  spiking to 300–998% CPU for 10–20 min per `git push` (reported by a
  dogfooding user on a 36k-entity NestJS repo). Now, out of the box and with no
  env vars set:
  - **Subprocess indexer is default-on**, so each reindex runs in a short-lived
    child process bounded to `GRAFEL_EXTRACT_GOMAXPROCS` (default 2) cores —
    the host stays usable during heavy reindexing.
  - **The daemon's own in-process Go parallelism is capped at ~half the host
    cores** (`GRAFEL_DAEMON_GOMAXPROCS` default), bounding GC / algorithm
    passes / the in-process index fallback.
  - **Incremental reindex is default-on** (already shipped in #5231) so a
    single-file push patches the graph (~25× faster) instead of a full
    reindex.
  - **A Go soft memory limit is applied by default** (already shipped in
    #5237: ~40% of RAM, floor 2 GB, ceiling 2.5 GB) so swap can't saturate.

  All four remain overridable: the env vars are now **opt-OUT / tuning**
  overrides rather than enablers, so the existing production plist
  (`GRAFEL_SUBPROCESS_INDEXER=1`, `GRAFEL_INCREMENTAL_REINDEX=1`,
  `GRAFEL_DAEMON_MEMLIMIT_MB=1536`) is fully back-compatible.

### Added

- **`grafel_stats` exposes live reindex state** ([#PR](https://github.com/cajasmota/grafel/pull/5310)) —
  the tool now returns `is_indexing` (and, while indexing, `indexing_in_flight`
  + `indexing_started_at`) so a coordinator can query reindex state via MCP
  instead of polling `ps aux` for hot grafel processes.

---

## [0.1.0] — 2026-05-23 (Preview Release)

grafel's first pre-release. Active development; APIs, MCP tool names,
and graph schema may change between minor versions.

### Added

- **5-tier docgen ladder** — Tier 0 single-section (#1809), Tier 1
  single-page with contracts (#1812), Tier 2 5-page slice with cross-page
  contracts (#1814), Tier 3 single-repo with repo-level contracts (#1817),
  Tier 4 full multi-repo group (#1820).
- **LLM-mode emit-and-orchestrate workflow** — schema (#1816),
  `--llm-mode=emit` for Tier 0+1 (#1819), `--llm-mode=apply` (#1821),
  section cache (#1823), `generate-docs` skill Pass 20 orchestrator (#1822),
  user-facing docs (#1824).
- **MCP handshake cwd-gate** with `grafel_status` sentinel — tools list
  goes from 2,319 tokens → ~80 bytes for sessions outside any indexed group
  (#1769, #1783).
- **`grafel_stats --breakdown=unresolved_imports`** for import taxonomy
  (#1839).
- **Windows CGO experimental build workflow** (#1791) — produces
  `grafel.exe` artifact via MinGW on GitHub Actions.
- **Python `config_module` entity** for `settings.py`/`urls.py`/etc. (#1778).
- **Go extractor** — method-value references (#1789, #1792), intra-package
  bare-function calls (#1806, #1810), struct field references via receiver
  type chain (#1840, #1843).
- **Resolver** — platform-variant merging for `_unix.go` + `_windows.go`
  pairs (#1815).
- **`grafel_whoami` `wire_version` field** — returns `"0.1.0"`; bump per
  minor release (#1845).

### Changed

- **MCP param renames with compat aliases** — `grafel_find(query=)` was
  `question`; `grafel_get_source(entity_id=)` was `node_id` (#1790).
- **`grafel_stats.fidelity` scope narrowed to IMPORTS-only** matching
  `health-history.bug_rate` — value jumped from 0.656 to 0.943 on grafel
  (prior dilution was hiding ~5,000 import rescues) (#1842, #1844).
- **MCP positioning** embedded in handshake/CLAUDE.md/skills/docs: "pair MCP
  + grep" is the canonical position (#1838).
- **Default response limits shrunk**; token_budget enforcement extended;
  per-tool field elision (#1738, #1739, #1749, #1751).
- **`grafel_find` returns TOON format** with `id` as first column (#1737,
  #1743, #1761).
- **Entity-ID interning** in MCP responses (#1740, #1750).
- **Subgraph fold** — `get_subgraph` + `summarize_subgraph` →
  `grafel_subgraph(format=raw|markdown)` (#1754, #1764, #1768).
- Posix-only test files now carry `//go:build !windows` tags for Windows
  compatibility (#1787, #1804).

### Fixed

- **`grafel_get_source` 5 s fsevents wedge on macOS** — now 48 ms via
  `O_NONBLOCK` + `lstat` + semaphore (#1773, #1780).
- **`gitignore.go` Windows build** via `_unix`/`_windows` split (#1787).
- **`filepathBase` cross-platform path handling** (#1782).
- **Tier 4 LLM emit propagation** through Tier 2/3/4 opts + integration test
  (#1825, #1828, #1832).
- **Section guidance `graph_context`** now populated with
  `qualified_name`/`repo`/`source_window` (#1827, #1831).
- **MSYS2 path resolution** on Windows GHA runners (#1805, #1808).
- **grafel daemon test deadline** 2 s → 4 s + `test.yml` `-timeout 5m`
  (#1797, #1800).
- **`mat: zero length` panic guard** in `RunAlgorithmsWithOptions` (#1795,
  #1801).
- **`TestAxumE2E` skip-on-CI guard** (#1798, #1799).
- **166-file gofmt baseline** restored (#1786).
- **Vendored go-tree-sitter** with `//go:build cgo` patch (#1796, #1802).

### Known Limitations (roadmap for v0.2.0+)

- Resolver platform-variant merging works in unit tests but does not fully
  reflect in `find_callers` end-to-end (#1818 — separate bug class).
- Token-ratio recovery: iter6 had token ratio 6.83× vs 0.7× target — quality
  is strong, token economy needs work (#1807).
- Docgen `source_window` cwd-relative path bug — affects `BuildBundle` when
  called from a cwd outside the indexed repo root (#1834).
- Tier 4 emit one-off bundle/page count mismatch: one page in N produces no
  bundle (#1835).
- Go struct field extraction v2: cross-file struct types, interfaces,
  generics, embedded type promotion (#1840 — current v1 handles same-file
  struct fields only).
- `grafel_stats.fidelity` is IMPORTS-only; CALLS and REFERENCES
  improvements are not yet surfaced as metrics.
- JS/TS/Python sibling sweeps for the receiver-chain pattern that #1843 fixed
  in Go are queued for v0.2.0.

---

## [Unreleased] — v1.0-rc (2026-05-21, overnight session)

### Dashboard — new surfaces and nav

- **Cmd+K command palette** — fuzzy search all surfaces and actions from
  anywhere in the dashboard. (#1234, #1237)
- **Nav redesign** — 9 surfaces reorganised into Explore / Operate dropdown
  menus. (#1210, #1213)
- **MCP Activity surface (Jarvis)** — live log of every MCP tool call at
  `/mcp-activity`. (#1226, #1230)
- **Graph canvas Jarvis integration** — graph nodes pulse in real time when
  returned by an MCP tool. (#1225, #1232)
- **Quality surface** — orphan audit + recall measurement + health-score
  history trend line. (#1198, #1205, #1214, #1223)
- **Patterns surface** — list, edit, delete, and export agent-learned
  patterns. (#1189, #1197)
- **Settings surface** — theme, auto-update, telemetry, MCP config, log
  level, all persisted to `~/.grafel/settings.json`. (#1206, #1211)
- **System surface** — daemon control panel with restart, stop, and live
  log tail. (#1195, #1203)
- **Update surface** — version check, apply, and refresh-rules-lite. (#1199,
  #1208)
- **Diagnostics surface** — daemon + per-group health checks. (#1187, #1193)
- **Maintenance ops** — rebuild, reset, and cleanup actions per group or
  per repo in the dashboard. (#1200, #1204)
- **Graph thumbnail** — group cards on the landing page show a preview of the
  indexed graph. (#983, #1194)
- **Pending surface tiers** — tiered enrichment queue buckets
  (Critical / High / Medium / Low). (#1133, #1185)

### Paths v2

- `/api/paths/{group}` returns endpoint definitions grouped by
  `owning_backend`. (#1218, #1227)
- Orphan-caller detection at `/api/paths/{group}/orphan-callers`. (#1225)
- Duplicate path elimination (105 dupes removed, same endpoint with and
  without prefix). (#1124, #1163)
- XPath / XML namespace strings filtered from the Paths list. (#1125, #1160)
- DRF `ANY`-verb paths deduplicated via `http_endpoint_synthesis` entries.
  (#1126, #1158)

### Topology v2

- Rich per-topic detail panel v2 at
  `/api/topology/{group}/topic/{topicId}`. (#1141, #1178)
- `broker_canonical` + `owning_service` + `broker_groups` metadata. (#1139,
  #1175)
- Orphan publisher detector at `/api/topology/{group}/orphan-publishers`.
  (#1136, #1155)
- Orphan subscriber detector at `/api/topology/{group}/orphan-subscribers`.
  (#1137, #1159)
- Broker + service grouping headers in the list view. (#1142, #1176)
- Four-tab structure: All / Orphan Publishers / Orphan Subscribers /
  Scheduled Jobs. (#1140, #1168)
- `message_topic` YAML frontmatter wired into detail endpoint. (#1143, #1182)
- `Task`/`ScheduledJob` entity kinds bucketed into the Topology queue view.
  (#1116, #1122)

### Flows v2

- Per-flow React Flow DAG detail panel. (#1150, #1177)
- `process_flow` frontmatter wired into the flow detail panel. (#1152, #1181)
- Four-tab structure for Flows v2. (#1149, #1170)
- Entry-kind grouping headers in the flow list. (#1151, #1171)
- Entry-kind grouping metadata on `/api/flows/{group}` list endpoint. (#1148,
  #1167)
- Step-kind annotation and side-effect classification. (#1147, #1166)
- Truncated flow detector at `/api/flows/{group}/truncated`. (#1146, #1161)
- Dead-end flow classifier at `/api/flows/{group}/dead-ends`. (#1145, #1156)

### Real-time indexing progress (SSE)

- In-memory pub/sub broker for indexer progress events. (#1183, #1184)
- Internal `progress` package instruments the full indexer pipeline. (#1188)
- SSE endpoint `/api/index-progress` (all groups) and
  `/api/index-progress/{group}`. (#1186, #1190)
- `rebuild` CLI subscribes to broker for real-time terminal progress. (#1196,
  #1201)
- Dashboard `useIndexProgress` hook + `IndexingProgressModal`. (#1191, #1207)

### MCP — new tools and Jarvis broker

- MCP event broker + SSE endpoint `/api/mcp-activity/stream` (Phase 1).
  (#1215, #1222)
- 3 new HTTP endpoint tools: `grafel_endpoint_definitions`,
  `grafel_endpoint_calls`, `grafel_endpoint_stats`. (#1220, #1229)
- 13 additional tools for Topology v2, Flows v2, Quality, and graph
  traversal. (#1202, #1209)

### Entity model

- **`http_endpoint_definition` + `http_endpoint_call`** — `http_endpoint`
  split into two distinct entity kinds at the extractor layer. Legacy
  `http_endpoint` remains readable via compatibility helper. (#1217, #1233)
- Confidence score (0–100) added to every enrichment `Candidate`. (#1131,
  #1179)
- Enrichment model: 1 `EnrichmentTask` per entity with N pending actions.
  (#1134, #1165)
- Rebuild summary includes per-kind breakdown + color-coded percentage. (#1132,
  #1174)
- `describe_entity` emitter switched to research-driven positive selection;
  noise kinds excluded. (#1130, #1154, #1162, #1173)

### AGENTS.md auto-injection

- After every `grafel rebuild`, an Architecture Map block is written into
  `AGENTS.md` in each indexed repo. (#1216, #1221)

### Graph rendering

- 6-band zoom LoD (expanded from 3) for smoother level-of-detail
  progression. (#1108, #1192)
- Four rendering pathologies fixed: LoD threshold, Process pile-up, sizing,
  and hash labels. (#1121, #1127)
- Galaxy tune + 3-way color mode + Jarvis hook. (#1153, #1172)

### Extractors

- Stdlib placeholder elimination extended to PHP, Elixir, Clojure, and
  Erlang. (#1085, #1224)

### Docs / skills

- `generate-docs` skill: Topology v2 + Flows v2 frontmatter schemas and Pass
  14 validation. (#1212)

### Bug fixes

- Resolve leftover conflict marker from earlier rebase (build). (#1231)
- Merge conflict markers in `daemon.go` resolved. (#1228)
- `inferEntryKind` helper rename to resolve collision. (#1169)
- `actionEntry` field name consistency fix. (standalone commit)
- Unblock `npm run build` — fix tsc errors in test files. (#1180)

---

## Earlier sessions (2026-05-19 – 2026-05-20)

Covered by the session checkpoints in `MEMORY.md`. Key highlights:

- Daemon install-and-forget architecture (ADR-0017).
- `-81%` RSS via profile-driven fix (#637).
- Patterns chain: agent-learned patterns via ADR-0018.
- Cosmograph migration + tuning.
- 25+ new language extractors.
- Custom-extractor pipeline wiring (#1086).
- Lifecycle CLI (#1090).
- Near-zero Python orphans.
- Cross-repo functional testing.
- Paths v2 shipped (#1099, #1098, #1100, #1104).
- Unified enrichment schema (#1105).
- Graph hard-stop (#1101).
- Repo-first layout (#1106, not yet landed at session end).

---

_Older history is tracked in the [GitHub releases](https://github.com/cajasmota/grafel/releases)._
