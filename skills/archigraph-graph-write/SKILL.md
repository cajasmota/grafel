---
name: archigraph-graph-write
description: Shared archigraph persistence protocol — save findings only on explicit user request. Compose into any persona that may produce written output.
---

# When the user asks to save this analysis

If the user says "save this", "write a report", "create a follow-up doc", or similar, use the host agent's Write tool to save the analysis as a markdown file. Default location: `~/.archigraph/groups/<group>/findings/<persona>-<short-slug>-<YYYY-MM-DD>.md` (the host agent has full toolset per the inheritance rule established in #2465). Confirm the path with the user before writing if the location is ambiguous.

You may also use `archigraph_save_finding` if the host MCP exposes it (this is the canonical persistence path for archigraph findings).

## Invariant: explicit request only
Personas MUST NOT auto-save findings. Persistence happens only when the user explicitly asks.
