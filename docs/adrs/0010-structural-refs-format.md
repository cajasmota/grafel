# ADR-0010: Structural-refs format — Format A vs Format B

- **Status**: Accepted
- **Date**: 2026-05-10
- **Deciders**: Jorge Cajas

## Context

The resolver consumes a stream of *structural references* from each
language extractor: small records describing "this site in the AST
mentions this symbol in this way". The resolver later turns each ref
into either a concrete edge (`calls`, `extends`, `implements`,
`reads_attr`, …) or a *disposition* — a recorded decision that the ref
cannot be resolved to a graph entity (with a category explaining why).

Two candidate wire formats were prototyped during the port:

- **Format A — flat record per ref.** Each ref is a single struct with
  every field the resolver might need: `kind`, `caller_id`, `callee_text`,
  `receiver_text`, `import_alias`, `file`, `line`, `col`, plus optional
  language-specific fields. Fields not relevant to a given ref kind are
  zero/empty.
- **Format B — kind-tagged sum type.** Each ref is one of N typed
  variants (`CallRef`, `InheritanceRef`, `AttrAccessRef`, …) with only
  the fields meaningful to that variant.

## Decision

**Adopt Format A** (flat record), with the `kind` field acting as the
discriminator and unused fields left zero-valued.

## Rationale

- Extractors emit refs at high volume during indexing; a flat struct
  serialises and merges into the per-repo intermediate file with no
  per-variant boilerplate.
- The resolver itself is a switch on `kind`; reading optional fields
  inside a branch is no clumsier than unwrapping a sum-typed variant
  in Go (which lacks ergonomic sum types).
- New ref kinds (added when a language gains a new resolver feature)
  are a one-line schema addition, not a new variant + serialiser pair.
- The disposition table is keyed by `(kind, category)`; flat fields make
  the categoriser easy to inspect and unit-test.

The cost — sending zero-valued fields over the wire — is negligible
because the intermediate file is per-repo and never leaves the host.

## Consequences

- All extractors share one `StructuralRef` struct; no per-language
  variants.
- Validation that a ref has the fields its kind requires lives in the
  resolver, not the type system.
- Format-B style sum types remain an option for a future v2 wire
  format if extractor-resolver split ever becomes a network boundary.
