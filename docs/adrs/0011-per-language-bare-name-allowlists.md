# ADR-0011: Per-language bare-name allowlists with collision-prone exclusions

- **Status**: Accepted
- **Date**: 2026-05-10
- **Deciders**: Jorge Cajas

## Context

A "bare name" call is a callsite where the extractor sees only an
unqualified identifier — `foo(...)` rather than `mod.foo(...)` or
`receiver.foo(...)`. Resolving bare names back to graph entities is the
single largest source of resolver false-positives across our corpus.

Two extremes both produce bad graphs:

- **Resolve every bare name optimistically.** `format(...)` then matches
  every entity named `format` in scope, including methods of unrelated
  classes. Edges multiply; downstream graph queries return noise.
- **Refuse all bare names.** Misses real edges in dynamically-typed
  languages (Python, Ruby) where bare-name calls to module-level helpers
  are idiomatic.

## Decision

Each language extractor ships a **bare-name allowlist** — a finite set
of names that the resolver is allowed to match optimistically — plus a
**collision-prone exclusion list** of names that are *never* matched
even if they would otherwise pass the allowlist gate.

- Allowlist entries are derived from the language's own corpus
  fixtures and regression tests; a name only joins the allowlist when
  matching it bare cannot plausibly be wrong (e.g. names of
  user-defined functions in the same module).
- The exclusion list traps short, generic identifiers
  (`format`, `get`, `set`, `run`, `make`, `init`, `new`, `value`, …)
  whose bare-name match is never trustworthy. Excluded names always
  produce a disposition, never an edge.
- All other bare names — names neither allowlisted nor excluded —
  default to producing a disposition (categorised as
  `bare_name_no_scope`), not an edge.

## Rationale

- Whitelisting is safer than blacklisting for graph quality: edges that
  exist are real, even at the cost of missing some.
- The exclusion list catches the obvious traps that an allowlist
  alone would still let through if a corpus fixture happened to define
  one of those generic names.
- Per-language ownership scopes the allowlist to the resolver pass that
  understands the language's scoping rules.

## Consequences

- Every new language port begins with an empty allowlist; coverage
  grows as fixtures land and demonstrate that a name is safe.
- Disposition categories (`bare_name_excluded`, `bare_name_no_scope`,
  `bare_name_allowlisted_no_match`) make resolver behaviour auditable
  in tests.
- Runtime tuning is not supported in v1; allowlist edits require a
  binary rebuild.
