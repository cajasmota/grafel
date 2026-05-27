# Coverage tooling — agent guide

The `coverage` command maintains the archigraph capabilities registry at `docs/coverage/registry.json` and regenerates the per-language / per-category markdown views. It is the source of truth for "what does archigraph extract today?"

## Hard rules

- **Standalone dev tool.** No imports from `internal/*` are allowed — pure file I/O + YAML/JSON. This keeps the tool buildable independent of the indexer and lets it run from any worktree without a daemon.
- **Determinism.** Every `gen` invocation must produce byte-identical output for the same input. The pre-commit gen workflow + CI (`.github/workflows/coverage-docs.yml`) compare regenerated docs against the committed copy.
- **Schema is data-driven.** The capability taxonomy lives in `capability-dictionary.yaml` (post-#2752). Do not hardcode capability keys in Go; load them from the dictionary.

## Files

- `main.go` — CLI dispatcher; subcommands: `list`, `get`, `add`, `update`, `gaps`, `stats`, `validate`, `gen`, `discover`, `map-status`
- `schema.go` — registry + record shape
- `store.go` — load / save / canonical ordering of `registry.json`
- `validate.go` — schema invariants (referential integrity, status enum, dictionary key conformance)
- `capability_map.go` + `capability-map.yaml` — capability → file/function mapping for traceability
- `validate_map.go` — verifies `capability-map.yaml` references real files
- `generate.go` + `templates/` — markdown rendering of `docs/coverage/{summary.md,by-language/,by-category/,detail/}`
- `discover.go` / `map_status.go` — bootstrap helpers
- `buckets.go` / `languages.go` / `views.go` — projection helpers used by templates

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

## Coverage matrix update rule

The root `AGENTS.md` "Coverage matrix update" section is **the** rule for capability-changing PRs across the repo. Tooling changes inside this directory generally do NOT require a matrix update unless they alter the schema (in which case the schema PR + record migration must ship together).
