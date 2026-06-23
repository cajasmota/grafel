# Extractor recipe — adding detection for a new construct (C2, epic #5359 — milestone 0.1.4)

_Recipe maintained as of 2026-06-23. Companion to
[`docs/new-language-feature-triage.md`](./new-language-feature-triage.md) (C1, the
decision step that produces the work this recipe builds) and
[`tools/coverage/AGENTS.md`](../tools/coverage/AGENTS.md)._

This is the repeatable, end-to-end path to make grafel **model** a new language
construct — once [C1 triage](./new-language-feature-triage.md) has classified it
as **(b) needs-new-extraction** or **(c) changes-existing-extraction**. Follow it
as a checklist. It is grounded in grafel's real extractor architecture, citing
the C# extractor as the worked example.

## grafel's extractor architecture (the 60-second model)

grafel uses **pure manual tree-sitter Node traversal** — no native queries
(`Query`/`QueryCursor` are used **zero** times; confirmed in the B2 assessment,
ADR-0023 §1). Every per-language extractor:

- Lives under `internal/extractors/<lang>/` and registers itself in an `init()`
  via `extractor.Register("<lang>", &Extractor{})`
  (e.g. `internal/extractors/csharp/csharp.go:50`).
- Implements `extractor.Extractor`: `Language() string` and
  `Extract(ctx, file) ([]types.EntityRecord, error)`
  (`csharp.go:58`, `:61`).
- Walks the CST **depth-first** with a `switch node.Type()` dispatch and emits
  `types.EntityRecord` / `types.RelationshipRecord` values
  (`csharp.go:108` `walk(...)`, the `switch node.Type()` at `:122`/`:136`).
- Emits entities with a `Kind` string drawn from the validated set in
  `internal/types/kinds.go` (e.g. `Kind: "SCOPE.Component"` at
  `csharp.go:293`, `Kind: "CONTAINS"` at `:190`).

So "adding extraction for a new construct" = **find its node kind in the CST →
add a `case` to the right `switch node.Type()` that builds an
`EntityRecord`/`RelationshipRecord` → register any new Kind → wire the coverage
registry → fixture-test it.**

## The recipe (checklist)

### 1. Confirm the triage bucket and target

From the C1 [feature-impact report](./new-language-feature-triage.md):

- **(b)** → you'll add a new `case` (and likely a new `build…` helper).
- **(c)** → you'll add/rename a node-type string in an existing `case`.

Note the **grammar-bump prerequisite**: if the bundled grammar (smacker snapshot,
2024-08-27 — see [`grammars.lock`](../grammars.lock)) can't yet produce the node
kind, the [B1 catch-up bump](./grammar-freshness-audit.md) must land first or the
`case` will never fire. Verify with a quick parse of a fixture (step 6) — an
`ERROR` node where you expect your construct means the grammar is the blocker.

### 2. Locate the node kind in the grammar

Identify the exact tree-sitter **node type string** (and any **field names**) the
construct produces. Two reliable ways:

- Read the upstream grammar's `grammar.js` / `node-types.json` for
  `tree-sitter/tree-sitter-<lang>` (the repo tracked in
  [`grammars.lock`](../grammars.lock)).
- Empirically: parse a minimal fixture and print node types. grafel reads
  `node.Type()` everywhere (1628 call sites per ADR-0023); the same string is
  what your `case` must match. For a quick dump, write a throwaway test under the
  extractor package that walks the tree and logs `node.Type()` + `node.String()`.

Record the node type (e.g. `record_declaration`) and any child fields you'll need
via `node.ChildByFieldName("name")` (the pattern at `csharp.go:125`).

### 3. Add the traversal + emit in the right extractor

Edit `internal/extractors/<lang>/…`:

- **(b):** add a `case "<node_kind>":` to the `switch node.Type()` (model:
  `csharp.go:136`), and a `build…` helper that returns a
  `types.EntityRecord` (model: `buildComponent` at `csharp.go:285`, which sets
  `Kind`, `Subtype`, `Name`, position, and returns `(rec, ok)`). Append it to the
  `out` slice. Emit any relationships as `types.RelationshipRecord{ToID:…,
  Kind:"…"}` on the entity (model: the `CONTAINS` edges at `csharp.go:178`/`:190`).
- **(c):** add the new/renamed node-type string to the existing `case`'s match
  list and adapt the field lookups if a field was renamed (e.g. the
  multi-string `case "class_declaration", "interface_declaration", …` at
  `csharp.go:137`). Make the change **additive** — keep matching the old form so
  you don't regress older code.
- Keep the traversal honest: only emit for resolved/literal constructs; return
  `(…, false)` and recurse into children when you can't build a clean record
  (model: the `if !ok { … walk children … }` fallback at `csharp.go:148`).
- Tag language on the way out if the extractor doesn't already
  (`extractor.TagEntitiesLanguage` / `TagRelationshipsLanguage`,
  `csharp.go:90`).

### 4. Register any new entity/edge Kind

If your construct needs a **Kind that doesn't already exist** in
`internal/types/kinds.go`:

1. Add the constant to the right block (`EntityKind…` near `kinds.go:13`, or
   `RelationshipKind…` near `:471`).
2. Add it to the enumerator so the validator accepts it — `AllEntityKinds()`
   (`kinds.go:339`) / `AllRelationshipKinds()`, which back `IsValidEntityKind`
   (`kinds.go:456`) and `IsValidRelationshipKind`.
3. **Run the producer-kind guard** — this is the standing new-extractor check:

   ```sh
   go test ./internal/types/
   ```

   `internal/types/producer_kinds_test.go` scans the whole producer codebase for
   hard-coded `Kind: "…"` literals on `Entity`/`Relationship`/`…Record`
   composite literals and asserts every distinct value is covered by the
   validator. A new Kind string that isn't registered **fails this test at
   compile/test time** — it is the guardrail that catches typos and stale kinds.

   Prefer reusing an existing Kind (e.g. `SCOPE.Component`, `SCOPE.Operation`,
   `SCOPE.Schema`) over minting a new one unless the construct is genuinely a new
   category — most new constructs map onto an existing Kind with a new `Subtype`.

### 5. Update `registry.json` + coverage docs — in the SAME PR (the hard gate)

**Standing rule (non-negotiable):** every new/changed capability updates
`docs/coverage/registry.json` **and** the regenerated coverage docs **in the same
PR**, guarded by `coverage fmt --check`. Splitting them across PRs breaks the CI
gate (`.github/workflows/coverage-docs.yml`). See
[`tools/coverage/AGENTS.md`](../tools/coverage/AGENTS.md).

1. **Add/update the capability cell.** Prefer the tool over hand-editing JSON so
   placement stays canonical:

   ```sh
   go run ./tools/coverage update <record-id> ...   # set status + cites + notes
   ```

   The cell's `cites` must point at the real files you touched
   (`internal/extractors/<lang>/…`, the fixture/test, and `internal/types/kinds.go`
   if you added a Kind). Set `status` honestly (`full` only when modeled +
   tested + value-asserting; otherwise `partial`/`missing`).
   - If you're adding a brand-new **capability key**, first add it to
     `capability-dictionary.yaml` (the taxonomy is data-driven — don't hardcode
     keys in Go), then `go run ./tools/coverage validate`.

2. **Regenerate the markdown views** (deterministic; CI diffs them):

   ```sh
   go run ./tools/coverage gen
   ```

3. **Validate + assert canonical format.** `registry.json` must stay
   **2-space-indented, sorted** canonical form — make **surgical** edits, never
   re-serialize the whole file:

   ```sh
   go run ./tools/coverage validate
   go run ./tools/coverage fmt --check   # exits non-zero if not canonical
   ```

   `coverage fmt --check` (`tools/coverage/fmt.go`) writes nothing and exits
   non-zero when the on-disk file isn't the canonical 2-space sorted form it
   would produce. Run `go run ./tools/coverage fmt` (no `--check`) to fix.

4. **Commit the dictionary (if changed) + `registry.json` + the regenerated
   `docs/coverage/**` together** with the extractor change.

### 6. Add a fixture and a value-asserting test

- Drop a minimal source file using the new construct under the extractor's
  `testdata/` (model: `internal/extractors/csharp/testdata/`).
- Add a test that runs `Extract` over it and **asserts the specific
  entity/edge** is emitted with the right `Kind`/`Subtype`/`Name` and any edge
  target (model: `internal/extractors/csharp/csharp_test.go` and the per-issue
  tests like `issue4854_field_membership_test.go`). Assert *values*, not just
  counts — a count test passes on the wrong node.

  ```sh
  go test ./internal/extractors/<lang>/...
  ```

- **Fixture-then-live validation:** a green fixture test is necessary but not
  sufficient. The standing rule is to confirm the construct also surfaces on a
  **real indexed project** (fixtures can pass while live extraction fails — a
  recurring failure mode). Index a repo that uses the construct and verify the
  entity/edge appears in the graph before calling the capability `full`.

### 7. Close the loop

- Tick the corresponding **(b)/(c) action item** in the C1 feature-impact report.
- If the construct came from the catch-up window, note it against
  [C3 backfill (#5417)](https://github.com/cajasmota/grafel/issues/5417).
- Update [`grammars.lock`](../grammars.lock) `last_verified` for the language if
  this confirmed it current for version N.

## Pre-merge checklist (copy into the PR)

- [ ] Node kind located in the grammar (and a fixture proves it parses, not
      `ERROR` — grammar-bump prerequisite cleared).
- [ ] `case` added/adapted in `internal/extractors/<lang>/…`; entity/edge emitted.
- [ ] New Kind (if any) registered in `internal/types/kinds.go` +
      `All…Kinds()`; **`go test ./internal/types/` green** (producer-kind guard).
- [ ] `registry.json` cell added/updated via `go run ./tools/coverage update`
      with real `cites`; `capability-dictionary.yaml` updated if a new key.
- [ ] `go run ./tools/coverage gen` re-run; `docs/coverage/**` committed
      **in this PR**.
- [ ] `go run ./tools/coverage validate` clean; `go run ./tools/coverage fmt --check`
      passes (2-space canonical, surgical edits only).
- [ ] Value-asserting fixture test added; `go test ./internal/extractors/<lang>/...`
      green.
- [ ] Live validation: construct surfaces on a real indexed project (not just the
      fixture).
