# grafel — Contributor Agent Guide

If you're an AI agent helping develop grafel itself, follow these conventions.
End-user-facing guidance for agents calling grafel via MCP is delivered
through the MCP `instructions` handshake (wired into `internal/mcp/server.go`),
not from this file.

## Repo conventions
- Branches: feature branches only, never push to main
- Worktrees: `grafel-worktrees/<branch-name>` per concurrent stream
- GitHub identity: all `gh` / GitHub operations MUST use the repo owner's **personal** account (`cajasmota`) — never any other or secondary (work) account that may be authenticated on the machine. Verify the active account (`gh auth status`) before any write; if a non-personal account is active, switch back first. (Spawned worker agents are local-only and must not run `gh` at all — the coordinator owns all GitHub writes.)
- ADRs in `docs/adrs/`, numbered sequentially
- Quality fixtures in `internal/quality/golden/`; they hold a **recall ratchet**, not 100% recall. As measured on `72c7848ca` (2026-08-08) by `scripts/quality/run.sh --runs 1`, 6 of 20 fixtures reach 100% must-have recall, 12 fall short, and 2 (`groovy-grails-mini`, `swift-swiftui-mini`) carry no `expected.json` and are not graded at all. Per-fixture figures live in `internal/quality/golden/baseline.json`; re-derive them with `scripts/quality/run.sh --runs 1 --update-baseline`. The convention is `scripts/quality/run.sh --ratchet`: recall may not drop below the recorded baseline, and a rise fails too until you record it. Nothing invokes it for you — `.github/workflows/quality.yml` is `workflow_dispatch` only — so run it yourself on any change that touches extraction. The one part that *is* automatic is `go test ./internal/quality -run TestBaseline`, which enforces the baseline's structure without indexing anything. (Refs #6231)

## Coverage matrix update — mandatory for capability-changing PRs

When your PR adds, modifies, or fixes a capability that's tracked in the coverage matrix, the PR MUST also update `docs/coverage/registry.json` to reflect the change. The matrix is the source of truth for grafel's capabilities; PRs that ship code without updating the matrix create drift that erodes the matrix's value.

### When this applies
- A new framework / ORM / tool / protocol gets extraction support → add a new record OR update an existing one
- An existing capability's status changes (e.g. partial → full) → update the cell
- A new capability is implemented that doesn't fit any existing taxonomy slot → propose extending the schema in a separate small PR first, then the implementation PR uses the new key
- A bug fix that materially changes what a capability does → update the cell's notes + verified_at

### When this does NOT apply
- Pure refactors that don't change behavior
- Bug fixes that don't change what we extract (e.g. fixing a panic, not a capability)
- Docs / test-only changes
- Tooling changes that don't touch extractors

### How to update
1. Identify the affected record(s): `go run ./tools/coverage list --json | jq ...`
2. For each capability changed:
   - `go run ./tools/coverage update <record-id> --capability <key> --status <s> --cites <paths,...>` (the tool auto-places into the canonical group)
   - OR edit `docs/coverage/registry.json` directly + run `go run ./tools/coverage validate` to confirm
3. If the capability is implemented in identifiable functions, update `tools/coverage/capability-map.yaml` with file + functions + issues_implemented
4. Set `verified_at` to today's date
5. `go run ./tools/coverage gen` to regenerate `docs/coverage/*.md`
6. `git add docs/coverage/registry.json docs/coverage/capability-map.yaml docs/coverage/` and commit alongside your code changes

### Enforcement
- The CI workflow at `.github/workflows/coverage-docs.yml` rejects PRs that change `docs/coverage.json` (or related files) without regenerating docs
- Reviewers should flag capability-changing PRs that don't touch the registry
- A future enhancement (#2741 Phase 3) will scan PR body for `implements-capability:` tags and auto-update `verified_at`

### Examples
- PR adds Rails ORM extraction → updates `lang.ruby.orm.activerecord` record's relevant capability from missing → full
- PR fixes Django DRF endpoint attribution → updates `lang.python.framework.django-drf` `handler_attribution` cell's verified_at + cites
- PR ships a new framework synthesizer → may need to add a new record `lang.X.framework.Y` (use `go run ./tools/coverage add`)
- PR extends the JS extractor with React Context detection → updates `lang.jsts.framework.react` `Structure.context_extraction` cell (which exists thanks to #2751)

### Coverage tool CLI quick-reference
`go run ./tools/coverage <subcommand>` — supported subcommands: `list`, `get`, `add`, `update`, `gaps`, `stats`, `validate`, `gen`, `discover`, `map-status`. See `tools/coverage/AGENTS.md` for tooling-specific conventions and `docs/coverage/summary.md` for the rendered matrix.

## Coordinator role
- Dispatch implementation work to subagents; do not edit code directly when acting as coordinator
- One PR per scope; small focused changes
- Claims about numbers come from a real measurement — see "Evidence"

## Daemon discipline
- If you spawn a daemon for testing, isolate ALL THREE of `HOME`, `GRAFEL_HOME` and `GRAFEL_DAEMON_ROOT`, and stop it on exit. `GRAFEL_DAEMON_ROOT` alone is NOT isolation, and the CLI now refuses it (#6331):

  ```sh
  export HOME=$(mktemp -d)
  export GRAFEL_HOME=$HOME/.grafel
  export GRAFEL_DAEMON_ROOT=$HOME/.grafel
  ```
- Verify no PIDs survive with `ps aux | grep grafel`
- Never `git stash` (concurrent worktree race; commit-checkpoint instead)
- See `docs/adrs/0004-single-mcp-process-per-machine.md` for the daemon architecture
- `GRAFEL_DAEMON_ROOT` isolates the daemon socket (plus pidfile and logs) AND per-repo state (issue #745). It does **NOT** isolate the registry — this bullet used to claim it did, and so did ADR-0017; both were wrong. `registry.HomeDir` reads `GRAFEL_HOME` only, so `GRAFEL_DAEMON_ROOT` alone gives you a private daemon pointed at the LIVE store, whose startup tail then relocates and prunes it (#6134). See the 2026-08-19 amendment in `docs/adrs/0017-single-binary-daemon-architecture.md`. When the env var is set, per-repo state lives at `$GRAFEL_DAEMON_ROOT/state/<sha256(abs_repo_path)[:16]>/` instead of `<repo>/.grafel/`. This means two parallel agents can index the SAME fixture without racing, and the fixture's own `.grafel/` is never touched. When the env var is unset, ADR-0007 co-located behavior is preserved. Helper: `internal/daemon.StateDirForRepo` / `GraphPathForRepo` — use it for every per-repo state read/write; never hardcode `<repo>/.grafel/<file>`.

## Where things live
- MCP server: `internal/mcp/`
- Per-language extractors: `internal/extractors/<lang>/`
- Cross-cutting extractors: `internal/engine/`
- Graph format: `internal/graph/fbreader/` + `internal/graph/fbwriter/`
- Per-framework rule packs: `internal/engine/rules/*.yaml`
- Quality / orphan audit: `internal/quality/audit/`
- Capability coverage matrix: `docs/coverage/registry.json` + `docs/coverage/summary.md` (generated); tooling in `tools/coverage/`

## Tests + gates
- `go test ./...` is the baseline gate
- Bug-rate parity across PRs is checked via golden fixtures + cross-language invariant tests
- Determinism test in `cmd/grafel/determinism_test.go` must pass byte-identical output
- Evidence rules for behaviour that depends on repo shape: see "Evidence" below

## Evidence

Two rules, one subject: what you may claim, and what the claim rests on. (Refs #6347)

**Real-tree measurement.** Any change whose behaviour depends on the shape or distribution of a repository must be measured against at least one real tree before it ships, and the count recorded in the PR body. Fixtures pin the mechanism; only a real tree establishes the distribution — a fixture encodes what its author already expected, which is the wrong instrument for finding what they did not. Report a count, not an impression. Compliance is usually one command: run the binary over a checkout, or `grep -rlI` a candidate set and pipe it through the code under test.
- If the change alters what a user is **shown** — a report, a ranking, a coverage table — measure the *rendered output*, not the function that feeds it. In #6338 the classifier underneath was correct and well tested; the report built from it came out 60 rows deep on a real 31,820-file tree (`.json 1938`, `.ds_store 11`) with the one line the reporter needed buried in it, and #6342 shipped green over that report because nothing looked at it.
- Where a real tree genuinely cannot be used (confidential or licence-incompatible corpora), say so in the PR and state the substitute, so the gap is visible rather than absent.

**No unverified claims.** Every factual claim in a comment, commit message, PR body or issue — a count, a code path, a "this doesn't exist" — must be verified or explicitly marked unverified. Verified means one of: you ran it and read the output; you read the code path and confirmed it executes; you cite a recorded measurement. Reading config and inferring its effect is not verification — #6329 was filed on a mechanism whose A/B measured a **0-edge delta**, because that config had never been loaded.
- Marking something unverified is always acceptable and costs nothing. #6345's PR body is the model: "Both local repos are **~0.1% generated by line** against the requester's **47.6%**. The mechanism is verified [...] but the **ranking benefit is not, and cannot be locally.**" The failure mode being prevented is confident assertion, not uncertainty.
- Numbers rot fastest, and durable comments rot silently. A design report claimed ~105 marker hits on a corpus; the strict regex found 4, the other 101 being JPA `@GeneratedValue` caught by a case-insensitive probe. #6345 carried "14 marker hits on this repo" in a package doc while its own PR body said 12 and the measured figure was 13. Write how a number was measured next to the number, or don't write it.
- "X does not exist" needs a search you can show. The per-language external base-type allowlists were asserted absent in a research note; they are at `internal/resolve/refs.go:4342` (PHP), `:4693` (Python), `:5316` (Java).

## Derive, don't list

Sibling to "Evidence": that section governs whether a claim was measured, this one governs whether the *set* the claim ranges over is the whole set. The failure is a claim of totality that holds only for the subset the author happened to look at. It survives green suites, because the enumeration and the test confirming it come from the same mental model — eight instances on 2026-08-19 alone, several written by people actively hunting for the pattern. (Refs #6361)

**State what you searched FOR and WHERE, separately from what you concluded EXISTS.** Put the pattern, the tree and the tool next to the finding, so a reader can see the subset the conclusion rests on. A #6365 review comment reported "exactly four path-token FromIDs outside the tree"; measured, twelve — eight missed, six of them through one hop, and five of those six sat in `internal/custom/`, the very tree whose omission that same comment had just flagged as a defect.

**Derive the set from the source of truth. Where a hand list is unavoidable, make it load-bearing.** Load-bearing means a test fails in *both* directions: a listed item that no longer matches, and an unlisted item that appears. `knownInvisibleOffenders` in `internal/extractors/file_anchored_rels_guard_test.go` is the in-repo example — it cannot rot silently. Unpinned enumerations rot fast: #6349's AST proof walked `FuncDecl` bodies only and the counterexample was a package-level `var` initializer; #6357's invariant "over every emitted structural edge" filtered on a `scope:` prefix; #6345's "one chokepoint" was Pass 1 of 3.

**Name the unit of analysis for any aggregate figure.** Under-searching is not the only way in. #6375's withdrawn "only 4 kinds corpus-wide have zero participation" counted kind names across a merged corpus, while the check it described fires per kind and repo; at that unit it is 10 kinds across 14 firings, and the omitted set contained `Route` — the kind that same PR promoted to its headline finding.

**Mark MEASURED vs INFERRED per claim, not per document, and when totality is not available name the subset rather than dropping the qualifier.** #6365 ran the chain code -> allow-list -> prose about the allow-list -> corrected prose, and each link carried the failure forward: the allow-list blessed six real defects on a criterion that did not hold; the prose describing it called `swift` a structural ref, concealing a live defect it had certified as fine; the correction then repeated the identical mis-attribution one item to the left, on `markdown`. A per-item "INFERRED — checked for the prefix form only" stops all three.

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

**Custom extractor wiring (#1086):** RunCustomExtractors now called from daemon's extraction pipeline. Enables Celery/Django/Flask/FastAPI/runtime-edge extractors. Previously wired into `grafel index` only.

**Per-language resolver slices:** Dedicated cross-file resolution for each language—class-hierarchy, import-path aliases, framework-specific edges. Go (+83% bug-rate reduction), Python (EXTENDS edge emission), TypeScript/JavaScript (external JSX/hook rewriting). Per-language files in `internal/extractors/<lang>/` + `internal/engine/dynamic_patterns_<lang>.go` (#1028).

**Stdlib elimination (#1088):** Stops emitting placeholder External entities for Python builtins, reducing graph noise.

**CLI lifecycle ops (#1090):** `grafel remove <group> <slug>`, `grafel delete <group>`, improved `grafel monorepo remove` with --json. Dashboard command opens browser (#948); `grafel rebuild` rich summary (#995); `grafel doctor` health report (#1042); `grafel status` rich output (#1007).

## Cross-platform status

**Phase 1 (macOS, Linux):** Complete
- macOS: native install + daemon lifecycle ✓
- Linux: systemd integration + XDG socket paths (#939) ✓

**Phase 2 (Windows):** In progress
- Blockers: #856 (decision items) + sub-issues. Socket transport, path canonicalization, and daemon registration remain open.

## MCP server

14 tools available (stable per #669), grouped into 6 categories:

**Query (5):** `grafel_find` (BM25-ranked BFS query), `grafel_inspect` (lookup by id/qname/label), `grafel_expand` (neighborhood traversal), `grafel_trace` (confidence-weighted shortest path), `grafel_traces` (process-flow queries).

**Analysis (2):** `grafel_clusters` (Louvain communities), `grafel_stats` (corpus-level metrics).

**Memory (3):** `grafel_save_finding` (persist Q&A pairs), `grafel_list_findings` (retrieve findings), `grafel_get_source` (source-file snippet lookup).

**Lifecycle (3):** `grafel_enrichments` (enrichment candidates: list/submit/reject), `grafel_cross_links` (cross-repo link candidates: list/accept/reject), `grafel_repairs` (residual-edge repair queue per ADR-0015: list/submit).

**Patterns (1):** `grafel_patterns` (ADR-0018 agent-learned pattern store: query/record).

**Introspection (1):** `grafel_whoami` (inferred group + repo + doc-state nudge).

Clients auto-discover via `grafel mcp serve`.

## Skills

- Skill markdown lives under `skills/<skill-name>/SKILL.md`; per-pass prompts (when applicable) live in `skills/<skill-name>/prompts/`.
- The pattern-discovery + sync skills (ADR-0018) are `/grafel-patterns-discover` and `/grafel-patterns-sync`. They sit alongside `/generate-docs`, which holds the primary discovery path.
- Invoke skills via the agent host's `/skill-name` command. The CLI surface for direct pattern inspection is `grafel patterns <verb>` — see `grafel help advanced`.

## CLI features

- **Path alias resolution:** TypeScript path aliases via tsconfig.json
- **Graph export:** --export-json flag controls graph.json output (post-#816 default is graph.fb)

<!-- grafel:mcp-usage:start v=1 -->

## grafel MCP

This repo is part of grafel group **grafel**. grafel is an architecture knowledge graph available via MCP. When you (an AI coding agent) need to understand how this codebase fits together, prefer the grafel MCP tools over `grep` + reading files.

### When to use grafel instead of grep

| Question shape | Prefer |
|---|---|
| "Where is `X` defined?" | `grafel_find` |
| "What does `X` look like + its neighbors?" | `grafel_inspect` |
| "Who calls `X`?" | `grafel_expand` / `grafel_find_callers` |
| "End-to-end flow when user does X?" | `grafel_traces` |
| "How does the frontend talk to the backend?" | `grafel_cross_links` |
| "Show me the source of `X`" | `grafel_get_source` |

### When grep IS still better

- Substring search across all files for non-entity strings (comments, TODOs).
- Anything where you need raw file contents in bulk.

### Anti-patterns

- Don't read an entire file to find one function — `grafel_inspect` returns it directly.
- Don't glob for a class name across the repo — `grafel_find` indexes it.
- Don't traverse imports manually — `grafel_expand` does it via the IMPORTS edge.

The full agent guide is delivered automatically in the MCP `instructions` handshake when you connect.

_Do not edit between the markers — this block is auto-updated by `grafel install`._

<!-- grafel:mcp-usage:end -->