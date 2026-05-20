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

## Runtime edge extractors

As of 2026-05-20, the following runtime-distributed systems are fully wired:

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

See `internal/engine/rules/*.yaml` for per-framework rule packs.

## Resolver slices

**Go:** +83% bug-rate reduction on fixture corpus via per-import gate + sentinel folding

**Python:** Cross-file class-hierarchy resolution with EXTENDS edge emission + global registry

**TypeScript/JavaScript:** External JSX/hook CALLS rewritten to ext: + route ref stubs to Dynamic

See #945, #961, #962 for implementation details.

## Cross-platform status

**Phase 1 (macOS, Linux):** Complete
- macOS: native install + daemon lifecycle ✓
- Linux: systemd integration + XDG socket paths ✓

**Phase 2 (Windows):** Planning in progress (see #856 for decision items)

## MCP server

17 tools available (stable per #669). Clients auto-discover via `archigraph mcp serve`.

## Skills

- Skill markdown lives under `skills/<skill-name>/SKILL.md`; per-pass prompts (when applicable) live in `skills/<skill-name>/prompts/`.
- The pattern-discovery + sync skills (ADR-0018) are `/archigraph-patterns-discover` and `/archigraph-patterns-sync`. They sit alongside `/generate-docs`, which holds the primary discovery path.
- Invoke skills via the agent host's `/skill-name` command. The CLI surface for direct pattern inspection is `archigraph patterns <verb>` — see `archigraph help advanced`.

## CLI features

- **Path alias resolution:** TypeScript path aliases via tsconfig.json
- **Graph export:** --export-json flag controls graph.json output (post-#816 default is graph.fb)
