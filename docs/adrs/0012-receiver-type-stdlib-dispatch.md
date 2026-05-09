# ADR-0012: Receiver-type tracking for stdlib interface dispatch

- **Status**: Accepted
- **Date**: 2026-05-10
- **Deciders**: Jorge Cajas

## Context

In statically-typed languages (Go, Java, C#, TypeScript) a method call
`x.Foo(...)` is dispatched on the static type of `x`. When `x` is an
interface from the language standard library — e.g. Go's
`io.Reader`, Java's `java.util.List`, TS's `Iterable<T>` — naive
extraction produces a callsite where the resolver knows the method
name (`Foo`) but not which concrete implementation is called.

Two failure modes follow:

- **Lose the edge entirely.** The resolver gives up because the
  receiver type points at an interface with no in-graph definition.
- **Match by method name across the whole graph.** Every type in the
  repo with a method named `Foo` becomes a candidate, polluting
  traversal results.

## Decision

Extractors track the **static receiver type** of every method call they
emit, and the resolver runs a dedicated *stdlib-interface-dispatch* pass
that:

1. Recognises a curated set of stdlib interfaces per language.
2. For a call whose receiver type is one of those interfaces, looks at
   the call-site's enclosing scope to find concrete types that *could*
   reach this call (e.g. local-variable assignments, parameter types
   declared elsewhere in the file).
3. If exactly one candidate concrete type defines the method, emits a
   `calls` edge to that concrete method.
4. Otherwise, records a disposition (`stdlib_interface_unresolved`)
   instead of guessing.

The stdlib-interface set is curated, not heuristic: each entry costs
zero or one false positive in the corpus tests before it lands.

## Rationale

- Most real call-sites in our corpus that hit a stdlib interface have
  a single locally-known concrete type — the resolver just needs the
  receiver-type signal to find it.
- Stopping at the curated set keeps the pass cheap and predictable;
  user-defined interfaces are handled by the regular resolver, which
  already has visibility of all their implementers.
- The disposition path keeps the graph honest when the analysis can't
  decide, which matters for downstream callers like the
  graph-completeness verifier.

## Consequences

- Each statically-typed language ships a `stdlib_interfaces.go` (or
  equivalent) listing the interfaces and methods covered.
- The receiver-type field on `StructuralRef` is non-optional for
  languages that have one; dynamic languages leave it empty.
- Adding a new stdlib interface to the curated set is a tested,
  reviewed change — never a runtime knob.
