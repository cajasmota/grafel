# archigraph — Contributor Agent Guide

If you're an AI agent helping develop archigraph itself, follow these conventions.
End-user-facing guidance for agents calling archigraph via MCP is delivered
through the MCP `instructions` handshake (see `docs/agent-instructions-draft.md`
and PR wiring it into `internal/mcp/server.go`), not from this file.

## Repo conventions
- Branches: feature branches only, never push to main
- Worktrees: `archigraph-worktrees/<branch-name>` per concurrent stream
- ADRs in `docs/adrs/`, numbered sequentially
- Quality fixtures in `internal/quality/golden/`; must hold 100% must-have recall on every PR

## Coordinator role
- Dispatch implementation work to subagents; do not edit code directly when acting as coordinator
- One PR per scope; small focused changes
- All claims about numbers must come from a real measurement (profile-first)

## Daemon discipline
- If you spawn a daemon for testing, set `ARCHIGRAPH_DAEMON_ROOT=/tmp/arch-<task>` and stop it on exit
- Verify no PIDs survive with `ps aux | grep archigraph`
- Never `git stash` (concurrent worktree race; commit-checkpoint instead)
- See `docs/adrs/0004-single-mcp-process-per-machine.md` for the daemon architecture
- `ARCHIGRAPH_DAEMON_ROOT` isolates THREE things: the daemon socket, the registry, AND per-repo state (issue #745). When the env var is set, per-repo state lives at `$ARCHIGRAPH_DAEMON_ROOT/state/<sha256(abs_repo_path)[:16]>/` instead of `<repo>/.archigraph/`. This means two parallel agents can index the SAME fixture without racing, and the fixture's own `.archigraph/` is never touched. When the env var is unset, ADR-0007 co-located behavior is preserved. Helper: `internal/daemon.StateDirForRepo` / `GraphPathForRepo` — use it for every per-repo state read/write; never hardcode `<repo>/.archigraph/<file>`.

## Where things live
- MCP server: `internal/mcp/`
- Per-language extractors: `internal/extractors/<lang>/`
- Cross-cutting extractors: `internal/engine/`
- Graph format: `internal/graph/fbreader/` + `internal/graph/fbwriter/`
- Per-framework rule packs: `internal/engine/rules/*.yaml`
- Quality / orphan audit: `internal/quality/audit/`

## Tests + gates
- `go test ./...` is the baseline gate
- Bug-rate parity across PRs is checked via golden fixtures + cross-language invariant tests
- Determinism test in `cmd/archigraph/determinism_test.go` must pass byte-identical output

## Language support

As of 2026-05-21, ~50 languages are fully supported with custom extractors:

**Primary (30+):** Go, Python, TypeScript/JavaScript, Java, C#, C++, Rust, Ruby, PHP, Swift, Kotlin, Scala, Groovy, Lua, Dart, Elixir, Clojure, Erlang, Crystal, Nim, F#, Haskell, OCaml, Elm, Lisp family (Common Lisp, Scheme, Racket), Standard ML, ReasonML, ReScript, Pony, Idris

**Frontend + Templates:** Vue SFC, Svelte SFC, Astro, Razor

**Infrastructure & Hardware:** Terraform/HCL, Solidity, Verilog/SystemVerilog, VHDL

**Cross-cutting:** CSS, HTML, SQL, GraphQL, Protocol Buffers, Shell, Dockerfile, YAML, Markdown, Just, Fish

Each language ships with a resolver slice for cross-file class-hierarchy, import-path alias, and framework-specific edge emission (e.g., HTTP endpoints, ORM queries, dynamic dispatch). See `internal/extractors/<lang>/` for per-language implementations and `internal/engine/rules/*.yaml` for framework rule packs.

## Runtime edge extractors

The following runtime-distributed systems are fully wired:

**Async task queues:**
- Celery, Sidekiq, Bull, dramatiq, RQ, Hangfire, Quartz

**Serverless:**
- AWS Lambda, Google Cloud Functions, Azure Functions

**Event buses:**
- AWS EventBridge, Azure EventGrid, CloudEvents

**Pub/Sub + Streams:**
- Apache Kafka, RabbitMQ, AWS SQS, Google Cloud Pub/Sub, NATS
- Redis pub/sub, Redis Streams, Apache Pulsar

**Workflows:**
- Temporal, Cadence, AWS Step Functions

**Real-time protocols:**
- gRPC, WebSockets, Server-Sent Events, GraphQL subscriptions

## Architecture milestones

**Graph visualization (Cosmograph):** 1M+ node capacity via WebGL, replaces react-force-graph. Includes degree-based node sizing, semantic layout (community clustering, hub gravity, module locality), hover-to-focus (dim non-neighbors, highlight hovered neighborhood), zoom controls, and cross-repo edge highlighting (#1023, #1044, #1056, #1064, #1070–#1079, #1081, #1095).

**Custom extractor wiring (#1086):** RunCustomExtractors now called from daemon's extraction pipeline. Enables Celery/Django/Flask/FastAPI/runtime-edge extractors. Previously wired into `archigraph index` only.

**Per-language resolver slices:** Dedicated cross-file resolution for each language—class-hierarchy, import-path aliases, framework-specific edges. Go (+83% bug-rate reduction), Python (EXTENDS edge emission), TypeScript/JavaScript (external JSX/hook rewriting). Per-language files in `internal/extractors/<lang>/` + `internal/engine/dynamic_patterns_<lang>.go` (#1028).

**Stdlib elimination (#1088):** Stops emitting placeholder External entities for Python builtins, reducing graph noise.

**CLI lifecycle ops (#1090):** `archigraph remove <group> <slug>`, `archigraph delete <group>`, improved `archigraph monorepo remove` with --json. Dashboard command opens browser (#948); `archigraph rebuild` rich summary (#995); `archigraph doctor` health report (#1042); `archigraph status` rich output (#1007).

## Cross-platform status

**Phase 1 (macOS, Linux):** Complete
- macOS: native install + daemon lifecycle ✓
- Linux: systemd integration + XDG socket paths (#939) ✓

**Phase 2 (Windows):** In progress
- Blockers: #856 (decision items) + sub-issues. Socket transport, path canonicalization, and daemon registration remain open.

## MCP server

14 tools available (stable per #669), grouped into 6 categories:

**Query (5):** `archigraph_find` (BM25-ranked BFS query), `archigraph_inspect` (lookup by id/qname/label), `archigraph_expand` (neighborhood traversal), `archigraph_trace` (confidence-weighted shortest path), `archigraph_traces` (process-flow queries).

**Analysis (2):** `archigraph_clusters` (Louvain communities), `archigraph_stats` (corpus-level metrics).

**Memory (3):** `archigraph_save_finding` (persist Q&A pairs), `archigraph_list_findings` (retrieve findings), `archigraph_get_source` (source-file snippet lookup).

**Lifecycle (3):** `archigraph_enrichments` (enrichment candidates: list/submit/reject), `archigraph_cross_links` (cross-repo link candidates: list/accept/reject), `archigraph_repairs` (residual-edge repair queue per ADR-0015: list/submit).

**Patterns (1):** `archigraph_patterns` (ADR-0018 agent-learned pattern store: query/record).

**Introspection (1):** `archigraph_whoami` (inferred group + repo + doc-state nudge).

Clients auto-discover via `archigraph mcp serve`.

## Skills

- Skill markdown lives under `skills/<skill-name>/SKILL.md`; per-pass prompts (when applicable) live in `skills/<skill-name>/prompts/`.
- The pattern-discovery + sync skills (ADR-0018) are `/archigraph-patterns-discover` and `/archigraph-patterns-sync`. They sit alongside `/generate-docs`, which holds the primary discovery path.
- Invoke skills via the agent host's `/skill-name` command. The CLI surface for direct pattern inspection is `archigraph patterns <verb>` — see `archigraph help advanced`.

## CLI features

- **Path alias resolution:** TypeScript path aliases via tsconfig.json
- **Graph export:** --export-json flag controls graph.json output (post-#816 default is graph.fb)
