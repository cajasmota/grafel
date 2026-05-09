# ADR-0014: Corpus expansion strategy — sample apps, not framework internals

- **Status**: Accepted
- **Date**: 2026-05-10
- **Deciders**: Jorge Cajas

## Context

Adding language support to archigraph requires fixtures: real source
trees that exercise extractor and resolver behaviour at realistic
scale. Two candidate sources were considered:

- **Framework internals** — index the source tree of a framework
  itself (e.g. Django, Spring, Rails). High volume, high complexity,
  but architecturally unrepresentative: framework code is
  metaprogramming-heavy and dominated by patterns user applications do
  not use.
- **Sample applications built on those frameworks** — small-to-medium
  apps written *with* the framework (e.g. `django-realworld`, the
  Spring `petclinic`, a Rails CRUD demo). Lower volume, but the call
  patterns match what real user code looks like.

## Decision

Corpus expansion uses **sample applications**, not framework
internals.

- A new language is "covered" when archigraph indexes 2–3 sample apps
  and the resolver disposition table is empty of unexplained
  categories.
- Framework internals can be added later as stress fixtures, but they
  do not gate language coverage.
- Each sample app is pinned to a specific commit; the corpus repo
  tracks fixtures by submodule or vendored snapshot.

## Rationale

- Resolver behaviour on framework code is not a proxy for resolver
  behaviour on user code. Tuning extractors against framework
  internals tends to bias the resolver toward metaprogramming patterns
  that hurt user-app graphs.
- Sample apps are small enough to run in CI on every PR; framework
  internals are not.
- A small, real corpus catches regressions earlier than a large,
  unrepresentative one.

## Consequences

- The corpus is curated by hand; PRs that add languages must also add
  the sample apps that exercise them.
- Framework-internal coverage gaps are tracked as separate, optional
  stress-test issues.
- The benchmark harness (post-1.0) runs against the same sample
  corpus, keeping graph-quality and token-economy measurements aligned.
