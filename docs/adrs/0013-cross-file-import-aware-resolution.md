# ADR-0013: Cross-file import-aware resolution (Python / Java / PHP / Ruby)

- **Status**: Accepted
- **Date**: 2026-05-10
- **Deciders**: Jorge Cajas

## Context

In single-file resolution, a callsite `mod.foo(...)` resolves only when
`mod` is a same-file binding the resolver can understand. In real
codebases the binding usually arrives via an `import`, `require`, or
`use` statement at the top of the file, and the target lives in a
different file (or different package).

Per-file extractors that ignore imports either:

- miss the edge (treating `mod.foo` as unresolved), or
- match `foo` everywhere (using only the unqualified callee name).

Both options degrade graph quality on every language that allows
namespaced calls — Python (`from x import y`), Java (`import a.b.C;`),
PHP (`use A\B\C;`), Ruby (`require_relative "..."` plus `Module::Method`).

## Decision

Each affected language gains a **cross-file import-aware resolution
pass** that:

1. During extraction, records each file's import table — alias →
   fully-qualified target.
2. During resolution, when a callsite has a non-empty receiver/qualifier,
   first looks the qualifier up in the importing file's import table to
   translate alias → canonical module/class.
3. Then resolves the canonical target against the per-repo entity index.
4. Falls through to the bare-name resolver only when no qualifier is
   present (and ADR-0011's bare-name policy applies).

Import-table data is part of the per-file extractor output, not
recomputed at query time.

## Rationale

- Imports are the highest-signal scoping data available without full
  type inference; using them lifts qualified-call recall significantly
  on every dynamic-with-namespaces language.
- Per-file import tables stay local: the resolver does not need to
  build a global symbol table to handle them.
- The same architecture works for Python, Java, PHP, and Ruby with
  language-specific lookup rules pluggable per extractor.

## Consequences

- Each of those four extractors emits an `imports` block per file.
- The resolver runs the import-aware pass before the bare-name pass
  per file.
- Languages without first-class imports (e.g. Go uses package-level
  imports differently and gets its own pass) follow this ADR's spirit
  but with their own rules.
- A future global-symbol-table mode is not precluded; this ADR is the
  v1 baseline.
