# Changelog

All notable changes are documented here. Entries follow
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions.
PR numbers link to https://github.com/cajasmota/grafel/pull/<N>.

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
