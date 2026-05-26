# archigraph

> **Status: Preview (v0.x).** Active development. APIs, MCP tool names, and graph schema may change between minor versions. macOS is the primary supported platform; Linux is being tested; Windows works via MinGW build (#937). See [CHANGELOG.md](CHANGELOG.md) for breaking changes. See [CLAUDE.md](CLAUDE.md) for "When to use archigraph MCP vs grep" — these are paired tools, not a grep replacement.

Multi-repo code knowledge graphs for AI agents.

## Status

Approaching v1.0.0. The full v1 post-mortem and migration runbook lives
at [`docs/migration/v1.md`](docs/migration/v1.md). Architectural
decisions are in [`docs/adrs/`](docs/adrs/). Track progress and roadmap
via the [issue tracker](https://github.com/cajasmota/archigraph/issues)
and [milestones](https://github.com/cajasmota/archigraph/milestones).

## What's new (v0.9 → v1.0-rc)

- **Paths v2, Topology v2, Flows v2** — rich list + detail panel surfaces
  for HTTP endpoints, message-bus topics, and process flows.
- **12 dashboard surfaces** — see [Surfaces map](#surfaces-map) below.
- **Explore / Operate nav** — grouped dropdown menus replace the flat nav bar.
- **Cmd+K command palette** — fuzzy search all surfaces and actions from
  anywhere in the dashboard.
- **Real-time indexing progress** — SSE stream at `/api/index-progress`
  drives an in-dashboard progress modal during `archigraph rebuild`.
- **MCP Activity surface (Jarvis)** — live view of every MCP tool call,
  with real-time graph node highlighting for the returned entities.
- **AGENTS.md auto-injection** — after each rebuild, archigraph writes an
  Architecture Map block directly into `AGENTS.md` in every indexed repo.
- **`http_endpoint_definition` + `http_endpoint_call`** — the legacy
  `http_endpoint` kind is split into two distinct entity kinds for
  precise orphan-caller detection (#1217).
- **13 new MCP tools** — Topology v2, Flows v2, Quality, and graph
  traversal surfaces exposed to agents. See
  [`internal/mcp/SCHEMA.md`](internal/mcp/SCHEMA.md).
- **Multi-branch + worktree support** — one graph snapshot per `(repo, ref)`;
  branch switches are detected automatically via `.git/HEAD` watch and optional
  git hooks; HOT/WARM/COLD tier management keeps RAM bounded; `--ref` flag on
  all read commands; `?ref=` on dashboard APIs; graph diff view between any two
  indexed refs. See [Multi-branch guide](docs/user-guide/multi-branch.md).

## Run it locally (testing build)

> **There is no hosted release yet.** The `curl … | bash` installer and the
> GitHub Releases binaries referenced under [Install](#install) are **not
> published during the testing phase** — build and run from source as shown
> here. This is the path to share with testers right now.

### Prerequisites

- **Go 1.25.5+** with **CGO enabled** — tree-sitter needs a C compiler
  (Xcode Command Line Tools on macOS: `xcode-select --install`; `build-essential`
  on Debian/Ubuntu).
- **Node.js 20+** and npm — used to build the dashboard.
- **git**.

### 1. Clone and build

```sh
git clone https://github.com/cajasmota/archigraph.git
cd archigraph
make build                 # builds the embedded dashboard + the ./archigraph binary
./archigraph --version
```

Optional — put it on your `PATH` so you can call `archigraph` from anywhere:

```sh
go install -ldflags="-X main.commit=$(git rev-parse --short HEAD)" ./cmd/archigraph
# installs to ~/go/bin/archigraph — make sure ~/go/bin is on your PATH
```

### 2. Start the daemon

```sh
./archigraph install       # registers + starts the daemon as a background service
# (or run it in the foreground:  ./archigraph start)
./archigraph status        # confirm it's up — it serves http://127.0.0.1:47274
```

### 3. Index your code

```sh
./archigraph wizard        # interactive: point it at a repo or a monorepo folder
```

A monorepo is auto-split into its modules; point it at a folder containing
several git repos and they become one multi-repo group. (You can also click
**Add group** from the dashboard.)

### 4. Open the dashboard

The full dashboard is **embedded in the daemon** — `make build` bundles it into
the binary and `archigraph install` serves it. Just open:

```
http://127.0.0.1:47274
```

No separate dev server is needed for testing. Deep links and browser reloads
work on every screen (Graph, Topology, Flows, Paths, Docs, Operations, Pending,
Settings) — the daemon SPA-falls-back to the app for client-side routes.

> **Developing the dashboard?** The UI source lives in `webui-v2`. For hot-reload
> dev iteration, run a dev server that proxies `/api/*` to the daemon:
>
> ```sh
> cd webui-v2
> npm install
> npm run dev              # → http://localhost:47280 (dev only)
> ```
>
> For testing the shipped build, use the embedded UI at http://127.0.0.1:47274.

### Stop / clean up

```sh
./archigraph uninstall     # stops and removes the daemon service
```

## Install

> **Not yet published** — see [Run it locally](#run-it-locally-testing-build)
> above until the first release ships.

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/cajasmota/archigraph/main/install.sh | bash
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/cajasmota/archigraph/main/install.ps1 | iex
```

### Manual download

Pre-built binaries for every release are published at
https://github.com/cajasmota/archigraph/releases — pick the matching
`<os>_<arch>` archive (`linux_x86_64`, `linux_arm64`, `macos_x86_64`,
`macos_arm64`, or `windows_x86_64`).

### Build from source

Requires Go 1.25.5+. CGO is required (tree-sitter dependency). See
[Run it locally](#run-it-locally-testing-build) for the full testing
walkthrough including the dashboard.

```sh
git clone https://github.com/cajasmota/archigraph.git
cd archigraph
make build
./archigraph --version
```

## Quick start

```sh
# 1. Create a group and index your repos
archigraph wizard
archigraph install <group>

# 2. Open the dashboard
archigraph dashboard

# 3. Generate docs (in Claude Code)
/generate-docs
```

The `/generate-docs` skill produces per-repo documentation (overview, module guides, API reference, patterns) and a cross-repo synthesis. It runs a 12-pass pipeline that typically takes 25–60 minutes for small repos, 1–2 hours for medium repos, and 2–4 hours for large repos. Estimates and pass details are in the [generate-docs skill docs](skills/generate-docs/SKILL.md).

Pass 13 (LLM enrichment) should run with Haiku to avoid high costs. See
[`docs/agent-hosts.md`](docs/agent-hosts.md) for per-host setup instructions
(Claude Code, Cursor, Windsurf, Continue, Aider, Cline).

### LLM-mode docgen and the 5-tier ladder

Docgen uses a deterministic 5-tier validation ladder (Tier 0 → section stub,
Tier 1 → full page, Tier 2 → coherent slice, Tier 3 → full repo, Tier 4 →
full group). LLM prose-fill is an optional overlay via `--llm-mode=emit/apply`
currently wired for Tier 0 and Tier 1. Pass 20 of the skill provides the
Claude Code orchestrator half of the emit → orchestrate → apply loop.

See [`docs/docgen-llm-mode.md`](docs/docgen-llm-mode.md) for the full workflow,
command recipes, section cache details, and troubleshooting reference.

## Usage

> **Using archigraph MCP with an AI agent?** See [`CLAUDE.md`](CLAUDE.md#when-to-use-archigraph-mcp-vs-grep) for the MCP + grep pairing philosophy: when to use MCP for structural questions vs grep for raw enumeration, and how to combine them.

archigraph is a CLI plus a unified daemon process that manages indexing,
the MCP server, embedded dashboard, and file watchers. The common path:

```sh
# 1. Set up a group (interactive). Creates the group config and a
#    cross-repo links file scaffold.
archigraph wizard

# 2. One-shot setup: daemon, MCP, indexer, and dashboard.
archigraph install <group>

# 3. Confirm everything is wired.
archigraph status <group>

# 4. Open the dashboard in your browser (auto-starts daemon if needed).
archigraph dashboard
```

The daemon process manages everything — MCP server, indexing, live file
watchers, and the embedded dashboard on http://127.0.0.1:47274/. Control it
via:

```sh
archigraph start                # start the daemon
archigraph stop                 # stop the daemon
archigraph restart              # restart the daemon
archigraph dashboard            # open dashboard in browser (auto-starts daemon)
archigraph dashboard serve      # run dashboard server standalone
```

After upgrading archigraph, materialize indexed data:

```sh
archigraph rebuild <group> [slug]    # force AST rebuild, no cache; outputs summary
```

Other useful commands:

```sh
archigraph index <repo>              # one-shot indexer (writes graph.fb, optionally graph.json)
archigraph index <repo> --ref <ref>  # index a specific git ref
archigraph reset <group> [slug]      # wipe .archigraph/ and rebuild
archigraph remove <group> <slug>     # remove a repo from a group
archigraph delete <group>            # delete entire group + state
archigraph monorepo add <group> <p>  # opt a path inside a monorepo into indexing
archigraph doctor                    # smoke-check install + tools (rich health report)
archigraph status <group>            # show group health + stats (rich output)
archigraph status <group> --all-refs # show per-ref tier + stats for every indexed branch
archigraph branches                  # list all indexed refs with HOT/WARM/COLD tier + sizes
archigraph uninstall <group>         # remove hooks/watchers from a group
archigraph install-hooks             # install post-checkout/merge/rewrite git hooks (multi-branch)
archigraph patterns list             # inspect agent-learned patterns (ADR-0018)
archigraph patterns export --repo X  # write the CLAUDE.md marker block
archigraph patterns config           # show / set pattern thresholds
```

`archigraph help advanced` lists the full set.

## Surfaces map

The dashboard at http://127.0.0.1:47274/ has 12 surfaces grouped into two
menus.

```
Dashboard (http://127.0.0.1:47274/)
│
├── Explore
│   ├── Graph        /graph          — Cosmograph node-link canvas + 6-band LoD
│   ├── Flows        /flows          — Process-flow DAG list + per-flow detail panel
│   ├── Topology     /topology       — Message-bus topics + broker/service grouping
│   ├── Pending      /pending        — Tiered enrichment queue (Critical/High/Med/Low)
│   ├── Paths        /paths          — HTTP endpoint definitions grouped by backend
│   └── Docs         /docs           — Indexed markdown document tree
│
└── Operate
    ├── Diagnostics  /diagnostics    — Daemon + per-group health checks
    ├── Quality      /quality        — Orphan audit + recall measurement + history trend
    ├── Patterns     /patterns       — Agent-learned patterns list/edit/delete/export
    ├── System       /system         — Daemon control panel (restart, stop, logs)
    ├── Update       /update         — Version check + apply + refresh-rules-lite
    └── Settings     /settings       — Theme, auto-update, telemetry, MCP config, log level

Special
    └── MCP Activity /mcp-activity   — Live Jarvis view of MCP tool calls + graph highlighting
```

### Keyboard shortcuts

| Shortcut | Action |
|----------|--------|
| `Cmd+K` (macOS) / `Ctrl+K` (Linux/Win) | Open command palette |
| `Esc` | Close command palette / close detail panel |
| `↑` / `↓` | Navigate palette results |
| `Enter` | Activate selected result |

## How agents use archigraph

archigraph exposes a Model Context Protocol (MCP) server that AI agents
connect to over stdio. The daemon registers one MCP server per machine;
multiple groups can be active simultaneously.

### MCP setup

After `archigraph install <group>`, the daemon writes an MCP server entry
to your Claude Code `~/.claude/claude.json` (or equivalent for other
clients). No manual configuration is needed.

To verify the wiring:

```sh
archigraph status <group>     # shows MCP: connected / disconnected
```

### What agents can do

The MCP server exposes 19 tools prefixed `archigraph_`. Common workflows:

| Goal | Tool |
|------|------|
| Orient to codebase | `archigraph_whoami` → `archigraph_stats` |
| Find a symbol | `archigraph_find` (BM25 + BFS) |
| Inspect an entity | `archigraph_inspect` |
| Walk the call graph | `archigraph_expand` |
| Trace a data path | `archigraph_trace` |
| Understand flows | `archigraph_traces` |
| Find HTTP endpoints | `archigraph_endpoint_definitions` |
| Find orphan callers | `archigraph_endpoint_calls` |
| Save a finding | `archigraph_save_finding` |

Full tool reference: [`internal/mcp/SCHEMA.md`](internal/mcp/SCHEMA.md).

### Real-time MCP activity

The Jarvis MCP Activity surface (`/mcp-activity`) shows every tool call in
real time. The graph canvas subscribes to the SSE stream at
`/api/mcp-activity/stream` and pulses the returned nodes — so you can watch
the agent's graph exploration as it happens.

### AGENTS.md auto-injection

After every `archigraph rebuild`, archigraph writes a fenced Architecture
Map block into `AGENTS.md` (or creates the file if absent) in each indexed
repo. Agents reading `AGENTS.md` at session start automatically receive the
latest structure without any manual step.

## Real-time indexing progress

During `archigraph rebuild`, the indexer emits progress events to an
in-process pub/sub broker. The dashboard subscribes via:

```
GET /api/index-progress           — progress across all groups (SSE)
GET /api/index-progress/{group}   — progress for a specific group (SSE)
```

Each SSE event is a JSON object:

```json
{
  "group": "my-group",
  "repo": "api-server",
  "phase": "resolve",
  "pct": 62,
  "elapsed_ms": 4210
}
```

The dashboard shows a live `IndexingProgressModal` while any rebuild is
running.

## Settings file

User preferences are persisted to `~/.archigraph/settings.json`. All
fields are optional; missing keys fall back to defaults.

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

| Field | Values | Default | Notes |
|-------|--------|---------|-------|
| `theme` | `light` \| `dark` \| `auto` | `light` | |
| `default_group` | group slug | `""` | Shown on first dashboard load |
| `auto_check_updates` | bool | `true` | |
| `update_channel` | `stable` \| `dev` | `stable` | |
| `refresh_schedule` | cron or `""` | `""` | Empty = manual only |
| `telemetry_enabled` | bool | `false` | |
| `daemon_rss_budget_mb` | 100–2000 | `512` | Requires daemon restart |
| `watcher_debounce_secs` | 1–60 | `2` | |
| `indexer_parallelism` | 1–32 | `4` | Requires daemon restart |
| `log_level` | `debug` \| `info` \| `warn` \| `error` | `info` | |

The Settings surface (`/settings`) in the dashboard edits this file
through `GET/PUT /api/settings`. A `POST /api/settings/reset` restores
factory defaults.

## Contributing

If you're an AI agent contributing to archigraph, see [AGENTS.md](AGENTS.md) for conventions.
End-user-facing agent docs are delivered via the MCP `instructions` handshake — there is no per-user setup.

## Languages

archigraph supports ~50 languages with custom extractors and resolver slices:

**Core (30+):** Go · Python · TypeScript/JavaScript · Java · C# · C++ · Rust · Ruby · PHP · Swift · Kotlin · Scala · Groovy · Lua · Dart · Elixir · Clojure · Erlang · Crystal · Nim · F# · Haskell · OCaml · Elm · Lisp family · Standard ML · ReasonML · ReScript · Pony · Idris

**Frontend + Templates:** Vue SFC · Svelte SFC · Astro · Razor

**Infrastructure & Hardware:** Terraform/HCL · Solidity · Verilog/SystemVerilog · VHDL

**Cross-cutting:** CSS · HTML · SQL · GraphQL · Protocol Buffers · Shell · Dockerfile · YAML · Markdown · Just · Fish

Each extractor emits language-specific edges (HTTP endpoints, ORM queries, dynamic dispatch, framework hooks). See [AGENTS.md](AGENTS.md) for architecture details.

## Corpus & coverage

archigraph is validated against a curated corpus of small-to-medium
sample applications, one per supported language family. Framework
internals are deliberately excluded as primary fixtures — see
[ADR-0014](docs/adrs/0014-corpus-expansion-strategy.md). New language
support lands together with the sample apps that exercise it.

## Roadmap

### v1.0 ship-gate

- [ ] Bug-rate below 10% on the full validation corpus
- [ ] Daemon determinism (#481) resolved — reliable measurement across runs
- [ ] HTTP overhaul — unified HTTP client/server pairing
- [ ] Per-language quality pass (residual orphan elimination)

Track the v1.0 milestone: https://github.com/cajasmota/archigraph/milestone/1

### v1.1 plan

- **Paths v2 epic** — cross-repo endpoint stitching, prefix-aware dedup
  (#1082, not yet dispatched)
- **Embedding strategy** — bundled MiniLM (`hugot` + `simplego`) + BYO
  endpoint for semantic search
- **BYO extractor pipeline** — custom-extractor registration without a
  daemon restart

## License

MIT — see [LICENSE](LICENSE).
