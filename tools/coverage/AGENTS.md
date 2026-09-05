# Coverage tooling — agent guide

The `coverage` command maintains the grafel capabilities registry at `docs/coverage/registry.json` and regenerates the per-language / per-category markdown views. It is the source of truth for "what does grafel extract today?"

## Hard rules

- **Standalone dev tool.** No imports from `internal/*` are allowed — pure file I/O + YAML/JSON. This keeps the tool buildable independent of the indexer and lets it run from any worktree without a daemon.
- **Determinism.** Every `gen` invocation must produce byte-identical output for the same input. The pre-commit gen workflow + CI (`.github/workflows/coverage-docs.yml`) compare regenerated docs against the committed copy.
- **Schema is data-driven.** The capability taxonomy lives in `capability-dictionary.yaml` (post-#2752). Do not hardcode capability keys in Go; load them from the dictionary.

## Files

- `main.go` — CLI dispatcher; subcommands: `list`, `get`, `add`, `update`, `gaps`, `stats`, `validate`, `gen`, `check`, `discover`, `map-status`, `parity`
- `check.go` — **`gateSteps()` is the coverage-docs gate** (#6866). `.github/workflows/coverage-docs.yml` runs `go run ./tools/coverage check` and nothing else, so the workflow cannot drift from what you can run locally. See "Reproducing the CI gate" below.
- `schema.go` — registry + record shape
- `parity.go` — READ-ONLY coverage parity probe (#3876): flags flagship→sibling asymmetry (a capability credited on one framework but missing on same-language siblings in the same `(language, category, subcategory)` group). Uniform-scaffold (all-missing) cells are suppressed by design. `--strict` is a CI gate.
- `store.go` — load / save / canonical ordering of `registry.json`
- `validate.go` — schema invariants (referential integrity, status enum, dictionary key conformance)
- `capability_map.go` + `capability-map.yaml` — capability → file/function mapping for traceability
- `validate_map.go` — verifies `capability-map.yaml` references real files
- `cite_symbol.go` — validates the `file.go:N` line citations embedded in each cell's `notes` prose **by symbol** (#6673); see "Line citations in `notes`" below
- `generate.go` + `templates/` — markdown rendering of `docs/coverage/{summary.md,by-language/,by-category/,detail/}`
- `discover.go` / `map_status.go` — bootstrap helpers
- `buckets.go` / `languages.go` / `views.go` — projection helpers used by templates

## Reproducing the CI gate (#6866)

```
go run ./tools/coverage check
```

That is the whole `coverage-docs` job. Before #6866 the job listed five steps itself; three were
subcommands (`validate`, `backfill --check`, `fmt --check`) and two — `gen`, then a comparison
against working-tree state — existed nowhere but the YAML. So all three subcommands could exit 0 on
a tree CI was red on, which is exactly what happened twice on PR #6864.

**Add a step by adding it to `gateSteps()`, never to the workflow.** One list, no second copy to
forget. Every step carries a `Hint` — the operator-facing `::error::` annotation — because a gate
that cannot say *which* of its five checks failed is worse than the five steps it replaced.

Two behaviours worth knowing:

- **What fails the gate is what `gen` itself changes**, measured as a before/after content
  comparison of `docs/coverage/`, not a diff against `HEAD`. That keeps the #6354 property (a page
  `gen` newly emits fails the gate even though `git diff` alone cannot see an untracked file) and
  drops a false failure the `git diff docs/coverage/` formulation had: `registry.json` lives under
  `docs/coverage/`, so an uncommitted registry edit — the normal state mid-change — failed the gate
  locally for a reason CI can never reproduce.
- **Uncommitted changes under `docs/coverage/` are reported, not fatal.** `check` prints them
  before running anything, read-only (`git status --porcelain`; it never touches your index).

`check` runs `gen`, so it rewrites generated pages in place — the same thing the workflow's `gen`
step always did. They carry the `DO NOT EDIT` marker; nothing hand-authored is at risk.

## Line citations in `notes` (#6673)

A cell's `cites` list is validated by path. The `file.go:N` references inside the cell's `notes`
prose used to be hand-typed text that nothing read — an audit measured **21 of 53 wrong (40%)**,
and every failure was drift onto a *different, plausible* line: zero dead files, zero out-of-range
lines. A "does the cited line exist?" gate would therefore have caught **0 of 21** and gone green
on a registry that was 40% wrong. It was rejected on that measurement. `cite_symbol.go` validates
the **symbol**, not the number.

**Write every line citation in the symbol-anchored form**, with a repo-relative path:

```
`synthesizeUtoipaAxumRoutes` (internal/engine/http_endpoint_utoipa_axum.go:445)
(`cdkAddEventSourceRe`, internal/engine/cdk_edges.go:137-142)
```

The backticked symbol must sit immediately before the citation (an optional `,` and/or `(` may
separate them, nothing else). Bare basenames are rejected: `extractor.go` alone matches 50 files
in this tree.

**The convention is two rules, not one.** Recording only the first is what produced a wrong
cleanup argument in #6671:

1. A **single-line** citation sits on the **exact declaration line**. Citing the last line of the
   symbol's doc comment is rot, not house style — that position carries no meaning, and doc-comment
   blocks vary in length so it is not even consistently "the first comment line".
2. A **range** opens on the **first doc-comment line** and closes on the declaration or in its
   body. **Both ends are checked, and both matter.** The declaration must fall within the range;
   the range must open exactly on the declaration's first doc-comment line (the declaration line
   itself when there is no doc comment); and it must close no later than the last line of the
   declaration. The two bounds catch different defects and neither stands in for the other:

   - The **opening** bound rejects a range that starts anywhere but the doc comment — including
     one starting on the declaration line and skipping the doc comment, which is the shape of
     three of the five corrections it forced (`slsFunction`, `parseProviderBlock` and
     `cdkPyAddEventSourceRe` each cited their own declaration line; the other two opened at doc+1). It rejects `terraform_deep.go:1-900` *for opening at line
     1*, not for its width.
   - The **closing** bound is the only width limit. With the opening bound alone, width was
     unlimited: `cdk_edges.go:137-900` opened correctly and was accepted, claiming a 764-line span
     in a 534-line file, as was `terraform_deep.go:220-9999`.

   A citation whose closing line exceeds the file's length is reported separately, since that is
   wrong regardless of which symbol it names.

**If there is no symbol to anchor to — a statement block, a map-literal key, a regex body, a
comment — do not write a line number at all.** Keep the file path and the prose. A number with
nothing to anchor to is unverifiable prose by construction, and that is where 100% of the measured
drift lived. The checker enforces this: an unanchored `file.go:N` anywhere in `notes` is an error.

**Out of population**, both by stated reason rather than oversight:

- **Bare continuation refs** (`,472-479`, `(:158-160)`, `(:587)`) that add a second location for a
  file named earlier in the sentence. They carry no file token, so deciding which file they belong
  to needs natural language, not a regex.
- **Line refs into non-Go files** — the registry carries five (`aws_cdk.yaml:54-58`, four
  `lang.rust.framework.*.md:21`). Same defect class, but the check validates a citation by
  resolving a *symbol declaration*, and a YAML key or a Markdown table row has none. They are
  unanchorable for the same reason as the statement-block numbers this rule strips.

Both are left as prose; the anchoring rule still stops a *new* unanchored `file.go:N` from
entering the registry.

**The check recurses.** It hangs off `validateCapabilityCell`, which the flat, grouped and
`framework_specific` tiers all route through. A flat walk of `capabilities` sees 38 of the 53
citations and misses 15 in silence — the utoipa ones live at
`capabilities.Routing.route_extraction.notes`.

## Extending the schema

Because the taxonomy is data-driven:

1. Open `capability-dictionary.yaml` and add the new capability key under the right group, with description + status enum if non-default.
2. Run `go run ./tools/coverage validate` — it will fail on any record that doesn't yet have a value for the new key (defaulting to `missing` is fine, but it must be explicit if the dictionary marks it required).
3. Update existing records in `docs/coverage/registry.json` via `go run ./tools/coverage update ...` rather than editing JSON by hand when possible — the tool guarantees canonical placement.
4. Regenerate: `go run ./tools/coverage gen`.
5. Commit the dictionary + registry + regenerated docs together. Splitting them across PRs breaks the CI gate.

## Templates

- Templates live in `templates/` and use Go's `text/template`.
- Keep them deterministic: sort every map / slice before iterating. The `gen_test.go` snapshot test will catch nondeterministic ordering.

## Errors vs warnings — the validate gate policy

`go run ./tools/coverage validate` distinguishes two severities, and the CI
gate (`.github/workflows/coverage-docs.yml`) treats them differently:

- **Errors fail the build** (`main.go` returns non-zero when `totalErrors > 0`).
  These are hard consistency violations: a capability-map citation pointing at a
  registry cell that doesn't exist (or whose shape/group doesn't match), a cited
  source file or function missing on disk, an invalid status enum, a stale
  dictionary key, a duplicate capability key. **Errors must stay at 0.**
- **Warnings never fail the build.** They are advisory nudges and are expected
  to number in the thousands at the group's current breadth. Do not "fix" them
  by deleting registry rows or capability-map content — that would silence
  reality rather than reflect it. The two dominant warning classes are
  structural and intentional:
  1. *"capability has no mapping entry"* (`validate_map.go`, the
     mapping-coverage nudge) — every registry cell with status `full`/`partial`
     ideally has a `capability-map.yaml` symbol/function mapping. The mapping
     section only enumerates grafel's own code-delivering records (~18); the
     hundreds of framework/language records (e.g. `lang.jsts.framework.*`,
     `test.pytest`) are descriptive coverage entries served by *shared*
     extractors, so per-record symbol mappings are deliberately absent. Adding
     mapping is incremental, never required.
  2. *"capability has no tracking issue"* (`validate.go`,
     `validateCapabilityCell`) — `partial`/`missing` cells without an `issue:`
     link get a nudge to file a ticket. These mark known framework-support gaps
     and are advisory, not blocking.

  If a warning is ever genuinely wrong (cell status misrepresents what the code
  does), fix it at the source — flip the registry cell or correct the citation —
  rather than suppressing the warning. (Ref: #2799.)

## Coverage matrix update rule

The root `AGENTS.md` "Coverage matrix update" section is **the** rule for capability-changing PRs across the repo. Tooling changes inside this directory generally do NOT require a matrix update unless they alter the schema (in which case the schema PR + record migration must ship together).
