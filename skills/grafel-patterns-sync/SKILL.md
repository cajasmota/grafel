# grafel-patterns-sync

Bidirectional sync between the per-group pattern store and a repo's version-controlled `CLAUDE.md` / `AGENTS.md`. Marker-wrapped, idempotent, and privacy-aware.

This skill is the export-side surface described by [ADR-0018](../../docs/adrs/0018-agent-learned-patterns.md). Privacy boundary: any anti-pattern with `private=true` MUST NOT be written to a version-controlled file, regardless of which direction the sync is running.

## When to use this skill

- "Update CLAUDE.md with our latest patterns."
- "Pull patterns from this repo's CLAUDE.md into the store."
- "Reconcile CLAUDE.md and the pattern store."

Do NOT invoke this skill if no patterns have been recorded yet (run `/generate-docs` or `/grafel-patterns-discover` first), or if the user has manually edited the marker-wrapped block — surface the edits as candidates first via the import path.

## Marker block format

The sync block lives inside this pair of HTML-comment markers (matching the gfleet `upsertAgentRulesBlock` convention established in AI-Memory):

```
<!-- grafel:patterns:start v=1 -->
... pattern index ...
<!-- grafel:patterns:end -->
```

Content outside the markers is user territory and is preserved byte-for-byte.

The block is a SHORT INDEX, not the full pattern markdown. Each pattern entry is a one-liner:

```
- **<trigger>** — confidence <c>, <n> observations. [Full recipe](docs/patterns/<category>/<id>.md)
```

The full recipe lives at the linked path (written by `/generate-docs` Phase 6). A human reading CLAUDE.md can click through; an agent reading the block has every trigger summarised in one place.

## Inputs

- The target `CLAUDE.md` / `AGENTS.md` file path (per-repo, version-controlled).
- The group name (defaults to the registered group when only one exists).
- The current pattern store, loaded via `grafel_patterns(action=query, include_candidates=false)` or via the CLI's direct file read.

## Procedure

### Step 1 — Resolve the file

If the file does not exist, the skill creates it containing only the marker block. If it exists with no markers, the block is appended after a blank-line separator. If it exists with markers, the block contents are replaced; everything outside the markers is preserved.

### Step 2 — Export from store → file

1. Load all approved patterns (`is_candidate=false`).
2. For each pattern, exclude any anti-pattern with `private=true` — never write these to disk.
3. Group by category (alphabetical).
4. Within each category, sort by ID for deterministic output.
5. Emit the marker-wrapped block via `grafel patterns export --repo <path>` (or directly via the internal sync API).
6. Confirm idempotency: a second `export` immediately after the first must produce a byte-identical file.

### Step 3 — Import from file → store

1. Parse the marker block via `grafel patterns import --repo <path>`.
2. Compare each trigger summary against the store.
3. Patterns present in CLAUDE.md but NOT in the store are surfaced to the user. The user decides whether to import them (creating new patterns via `grafel_patterns(action=record)`) or treat them as drift to be removed from CLAUDE.md.
4. Patterns present in the store but NOT in CLAUDE.md will be added on the next export run; surface them too so the user knows what is about to be written.

### Step 4 — Bidirectional reconciliation

When both directions produce non-empty diffs, prompt the user one diff at a time. For each:

- **Keep store, overwrite CLAUDE.md** — run export.
- **Keep CLAUDE.md, import to store** — call `grafel_patterns(action=record, as_candidate=false, ...)` and re-export.
- **Discard CLAUDE.md entry** — note the trigger, ask the user to confirm, then run export (which drops the entry).

## Privacy guarantee

The export path filters every anti-pattern with `private=true` before serialisation. Tests in `internal/agentpatterns/sync_test.go` enforce that a pattern with a private anti-pattern is exported, but the private entry is not. If a user moves a private anti-pattern out by editing the store directly, the next export will write it — this is by design; the privacy flag is the contract.

## Constraints

- Never write to files outside the marker block. User content is sacred.
- Never strip the markers themselves on a write. Tools downstream rely on them.
- Never export `is_candidate=true` patterns unless the user explicitly passes the `--include-candidates` override (not the default surface).

## Related

- `/grafel-patterns-discover` — populate the store before exporting.
- `/generate-docs` — emits the full pattern markdown that the block links to.
- `grafel patterns export` / `grafel patterns import` — direct CLI surface.
