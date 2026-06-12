# archigraph

> A local code-knowledge-graph daemon that gives AI agents structural navigation — call graphs, cross-repo dependency traces, HTTP surface maps, and process flows — across one or many repositories, exposed via 65 MCP tools.

[![Build](https://github.com/cajasmota/archigraph/actions/workflows/test.yml/badge.svg)](https://github.com/cajasmota/archigraph/actions/workflows/test.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Status: Preview v0.x](https://img.shields.io/badge/status-preview_v0.x-orange.svg)](CHANGELOG.md)

---

## Why archigraph

AI coding agents are good at reading files. They are not good at navigating relationships between files — especially across multiple repositories. Ask an agent "what calls `PaymentService.charge`?" and it will grep, guess, and sometimes hallucinate. Ask it to trace a request from the mobile app through the API gateway to the database — across three repos — and it will read dozens of files it doesn't need.

archigraph pre-builds the relationship map. It indexes your codebase into an in-memory graph (entities, call edges, import edges, HTTP routes, message-bus topics) and keeps it fresh via file watchers. When an agent asks a structural question, it gets a precise answer in one round-trip instead of twenty file reads.

The graph lives entirely on your machine. No cloud indexing, no account, no data sent anywhere. One binary, one daemon process, one port.

---

## What you get

- **Call graph navigation** — find every caller of a function, walk dependency chains, trace paths between any two entities. Works across repos in a single query.
- **HTTP surface mapping** — enumerate every route definition and every call-site that references it; surface orphan callers (client calls with no matching server handler) without manually auditing both sides.
- **Process flow tracing** — pre-computed BFS from entry points (route handlers, `main`, framework hooks) stored as traceable chains; ad-hoc follow from any entity on demand.
- **Message-bus topology** — topic/broker/service groupings for event-driven systems; publisher and subscriber orphan detection.
- **Cross-repo dependency graph** — index a folder of repos as one group; edges span repo boundaries with confidence scores; diff graph state between any two indexed refs.
- **Documentation and analysis skills** — a 14-skill family (tech docs, business docs, security audit, consultant panel, patterns) all driven off the graph, invokable from Claude Code as slash commands.
- **Real-time dashboard** — 19 surfaces (Graph, Flows, Event-flows, Topology, Paths, Links, GraphQL, IaC, Docs, Security, Taint, DI, Error-flow, Quality, Settings, Pending, Operations, Compare, Missing) embedded in the daemon, no separate server, at `http://127.0.0.1:47274`.

---

## How it works

archigraph runs as a background daemon. The daemon manages a tree-sitter-based indexer (50+ languages), an in-memory graph loaded from `.archigraph/graph.fb` per repo, an MCP server on stdio, live file watchers, and the embedded dashboard — all as one process.

```
your repos
    |
    v  archigraph index (tree-sitter + resolver)
 .archigraph/graph.fb   <-- per-repo binary graph snapshot
    |
    v  daemon (in-memory, mtime-driven reload)
 MCP server (stdio) -- AI agent (Claude Code, Cursor, Windsurf, ...)
 Dashboard (HTTP)   -- browser at http://127.0.0.1:47274
```

When an agent calls `archigraph_find(query="payment processing")`, the daemon runs BM25 against entity labels and qualified names, then expands outward via BFS — returning a ranked, token-budgeted subgraph in one call. No files are read at query time.

After `archigraph install <group>`, the daemon registers itself as an MCP server in your Claude Code config automatically. No manual JSON editing.

---

## Quickstart

> The binary must be built from source during the current preview phase — the installer script and GitHub Releases binaries are not yet published. See [docs/install.md](docs/install.md) for the full install matrix.

```sh
# 1. Build (requires Go 1.25.5+, CGO, Node 20+)
git clone https://github.com/cajasmota/archigraph.git
cd archigraph
make build

# 2. Point it at your code (interactive)
./archigraph wizard

# 3. Start the daemon + register MCP + install skills
./archigraph install <group>

# 4. Confirm everything is wired
./archigraph status <group>

# 5. Open the dashboard
./archigraph dashboard
```

The dashboard is at `http://127.0.0.1:47274`. Your AI agent picks up the MCP server automatically after the next session restart.

To verify from inside Claude Code:
```
archigraph_whoami()
archigraph_stats()
archigraph_clusters()
```

For per-agent setup instructions see [docs/agent-hosts.md](docs/agent-hosts.md).

---

## When you'd reach for it

**Onboarding to an unfamiliar codebase** — run `archigraph wizard`, index, then ask your agent to orient you with `archigraph_clusters` and `archigraph_traces`. You get a module map and top-level flows in minutes instead of hours.

**Doing a code review and want to know the blast radius** — `archigraph_expand` from the changed entity shows every caller and downstream dependency. The `/archigraph-aware-review` skill surfaces this automatically during review.

**Generating documentation** — `/archigraph-tech-docs` produces per-module READMEs, API reference, cross-cutting concerns, and a group synthesis. `/archigraph-business-docs` produces PM-facing capability descriptions and user journeys from the same graph.

**Auditing security** — `/archigraph-security-audit` runs deterministic static checks (auth coverage, reachability, orphan endpoints, PII exposure paths) then an LLM confirmation pass.

**Working across a monorepo or multi-repo group** — archigraph cross-links repos in a group and resolves edges across repo boundaries, so `archigraph_trace(source="mobile-app::UICheckout", target="payments-api::ChargeHandler")` works even though those entities live in different repositories.

---

## What's inside

| Resource | Contents |
|----------|----------|
| [docs/README.md](docs/README.md) | Documentation index |
| [docs/quickstart.md](docs/quickstart.md) | Install + first index + first query |
| [docs/concepts.md](docs/concepts.md) | Knowledge graph, entities, edges, residual edges, repair |
| [docs/mcp-tools.md](docs/mcp-tools.md) | MCP tool catalogue and pointer to full schema |
| [docs/install.md](docs/install.md) | Full install matrix (script, binary, source, dev mode) |
| [docs/agent-hosts.md](docs/agent-hosts.md) | Per-agent setup (Claude Code, Cursor, Windsurf, Continue, Aider, Cline) |
| [skills/README.md](skills/README.md) | Skill family — chains, dependencies, install |
| [internal/mcp/SCHEMA.md](internal/mcp/SCHEMA.md) | Full MCP tool schema (canonical) |
| [docs/adrs/](docs/adrs/) | Architectural decision records (ADR-0001 through ADR-0022) |
| [CHANGELOG.md](CHANGELOG.md) | Version history and breaking changes |
| [CLAUDE.md](CLAUDE.md) | When to use MCP vs grep (agent pairing guide) |

---

## Languages

Core extractors for 50+ languages including Go, Python, TypeScript/JavaScript, Java, C#, C++, Rust, Ruby, PHP, Swift, Kotlin, Scala, Dart, Elixir, and more. Infrastructure: Terraform/HCL, Solidity, Verilog/SystemVerilog. Frontend: Vue SFC, Svelte, Astro. Cross-cutting: SQL, GraphQL, Protocol Buffers, Dockerfile.

Each extractor emits language-specific edges (HTTP endpoints, ORM queries, dynamic dispatch, framework hooks). Full list in [AGENTS.md](AGENTS.md).

---

## Status

Preview (v0.x). Approaching v1.0. APIs, MCP tool names, and graph schema may change between minor versions. macOS is the primary supported platform; Linux is tested; Windows works via MinGW build.

See [CHANGELOG.md](CHANGELOG.md) for breaking changes.
Track the v1.0 milestone: https://github.com/cajasmota/archigraph/milestone/1

### v1.0 ship-gate

- [ ] Bug-rate below 10% on the full validation corpus
- [ ] Daemon determinism (#481) resolved
- [ ] HTTP overhaul — unified HTTP client/server pairing
- [ ] Per-language quality pass (residual orphan elimination)

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). If you're an AI agent contributing to archigraph, see [AGENTS.md](AGENTS.md) for conventions.

---

## License

MIT — see [LICENSE](LICENSE).
