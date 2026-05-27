# Language extractors — agent guide

Per-language extractors that lift source into archigraph entities + edges. One sibling directory per language; everything else is shared scaffolding.

## Conventions

- **Directory name = canonical language slug.** Use the short, project-wide slug (e.g. `jsts` for TypeScript/JavaScript, `csharp` for C#, `golang` for Go). Do **not** introduce a new spelling — check existing siblings before naming a new one.
- **Tree-sitter is the parser.** All extractors go through `github.com/smacker/go-tree-sitter` (see `internal/treesitter/`). Avoid hand-rolled regex parsing.
- **Entity kinds come from `internal/types/kinds.go`.** Never invent a kind string at the extractor site — if you need a new one, add it to `kinds.go` first.
- **Resolver slices** live alongside the extractor (class-hierarchy, import-path aliases, framework edges). Cross-cutting engine passes consume the resolver output; do not synthesise HTTP / ORM edges directly inside the extractor.

## Tests

- Golden fixtures: `testdata/` per language; assert deterministic byte-identical output.
- Cross-language invariants (orphans, hierarchy soundness) run in `internal/quality/`. Bug-rate parity gate applies to every PR.

## Coverage matrix update

If your change adds or modifies extraction for a framework / ORM / protocol tracked in the matrix, you **must** update `docs/coverage/registry.json` in the same PR. See the root `AGENTS.md` "Coverage matrix update" section for the canonical workflow — do not duplicate it here.

Typical extractor PRs that trigger an update:
- New framework support (e.g. add a `lang.<lang>.framework.<name>` record)
- New ORM detection (`lang.<lang>.orm.<name>`)
- Materially improved capability (missing → partial → full) — bump `verified_at` + add cites pointing at the new code paths

## Related

- Cross-cutting passes that consume extractor output: `internal/engine/AGENTS.md`
- Cross-repo + cross-language linkers: `internal/links/AGENTS.md`
- Custom extractor wiring: `internal/extractors/custom_registry.go` (see #1086)
