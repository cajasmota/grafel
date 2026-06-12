# archigraph docs

Reference documentation for archigraph. The [README.md](../README.md) at the repo root covers the pitch and quickstart; this tree is the manual.

---

## Getting started

| Doc | Contents |
|-----|----------|
| [quickstart.md](quickstart.md) | Install, first index, first query — 5 commands |
| [install.md](install.md) | Full install matrix: script, binary, source, dev mode, troubleshooting |
| [agent-hosts.md](agent-hosts.md) | Per-agent MCP setup (Claude Code, Cursor, Windsurf, Continue, Aider, Cline) |

## Core concepts

| Doc | Contents |
|-----|----------|
| [concepts.md](concepts.md) | Knowledge graph model, entity kinds, edge kinds, residual edges, repair |
| [modes.md](modes.md) | Daemon operational modes (background, workstation, readonly) — memory, background activity, feature activation |
| [graph-format.md](graph-format.md) | On-disk graph format (`.archigraph/graph.fb`), binary schema, JSON export |
| [embedding.md](embedding.md) | Semantic search embedding strategy (BM25 + optional MiniLM / BYO endpoint) |

## Using with AI agents

| Doc | Contents |
|-----|----------|
| [mcp-tools.md](mcp-tools.md) | MCP tool catalogue and pointer to the full canonical schema |
| [skills.md](skills.md) | Skill family overview and pointer to `skills/README.md` |
| [../CLAUDE.md](../CLAUDE.md) | When to use MCP vs grep (pairing guide for agents) |
| [../skills/using-archigraph/SKILL.md](../skills/using-archigraph/SKILL.md) | Day-to-day agent orientation: Pass-based workflows, anti-patterns, examples |

## Advanced

| Doc | Contents |
|-----|----------|
| [user-guide/multi-branch.md](user-guide/multi-branch.md) | Multi-branch + worktree support, HOT/WARM/COLD tier, branch switching |
| [docgen-llm-mode.md](docgen-llm-mode.md) | LLM docgen emit/apply loop, 5-tier ladder, section cache, troubleshooting |
| [settings.md](settings.md) | `~/.archigraph/settings.json` reference (all fields, defaults, API) |
| [sse-endpoints.md](sse-endpoints.md) | Server-sent event endpoints (index progress, MCP activity stream) |
| [troubleshooting.md](troubleshooting.md) | Symptoms, diagnoses, and fixes |

## Reference

| Doc | Contents |
|-----|----------|
| [../internal/mcp/SCHEMA.md](../internal/mcp/SCHEMA.md) | Full MCP tool schema — canonical source of truth for inputs/outputs |
| [adrs/README.md](adrs/README.md) | Architectural decision records index (ADR-0001 through ADR-0022) |
| [RELEASING.md](RELEASING.md) | Release process (maintainer reference) |
