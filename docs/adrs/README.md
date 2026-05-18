# Architecture Decision Records

This directory holds archigraph's ADRs. Each record captures a single
architecturally-significant decision: the context that produced it, the
decision itself, and the consequences we accepted.

| ID | Status | Title |
|---|---|---|
| [0001](0001-go-native-single-binary-distribution.md) | Accepted | Go-native single-binary distribution |
| [0002](0002-clean-room-mcp-server-in-go.md) | Accepted | Clean-room MCP server in Go |
| [0003](0003-scope-entity-taxonomy.md) | Accepted | SCOPE entity taxonomy |
| [0004](0004-single-mcp-process-per-machine.md) | Accepted | Single MCP process per machine |
| [0005](0005-pre-bake-graph-attributes-during-indexing.md) | Accepted | Pre-bake graph attributes during indexing |
| [0006](0006-in-memory-json-persistence-no-graph-database.md) | Accepted | In-memory JSON persistence, no graph database |
| [0007](0007-doc-as-bridge-for-cross-repo-and-dynamic-connections.md) | Accepted | Doc-as-bridge for cross-repo and dynamic connections |
| [0008](0008-caller-cwd-aware-routing-for-multi-group-setups.md) | Accepted | Caller-CWD-aware routing for multi-group setups |
| [0009](0009-cross-repo-id-namespacing.md) | Accepted | Cross-repo ID namespacing |
| [0010](0010-structural-refs-format.md) | Accepted | Structural refs format |
| [0011](0011-per-language-bare-name-allowlists.md) | Accepted | Per-language bare-name allowlists |
| [0012](0012-receiver-type-stdlib-dispatch.md) | Accepted | Receiver-type stdlib dispatch |
| [0013](0013-cross-file-import-aware-resolution.md) | Accepted | Cross-file import-aware resolution |
| [0014](0014-corpus-expansion-strategy.md) | Accepted | Corpus expansion strategy — sample apps, not framework internals |
| [0015](0015-residual-repair-agent-enrichment.md) | Proposed | Residual repair via agent-side enrichment |

Numbers are append-only.

## Spec sidecars

ADRs that introduce on-disk or wire-format contracts publish the schemas as sibling files under [`../specs/`](../specs/):

- [`enrichment-candidates-v2.schema.json`](../specs/enrichment-candidates-v2.schema.json) — ADR-0015
- [`repair-v1.schema.json`](../specs/repair-v1.schema.json) — ADR-0015
- [`mcp-residual-repair-tools.md`](../specs/mcp-residual-repair-tools.md) — ADR-0015
- [`repair-trust-model.md`](../specs/repair-trust-model.md) — ADR-0015
