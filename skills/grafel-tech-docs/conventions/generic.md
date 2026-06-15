# Generic fallback convention

Required reading: `_graph-searchability.md`.

This convention is the last resort. Use it only when no other convention fits and the user has not yet run the `extend-convention` skill to derive a stack-specific one.

Producing docs against `generic.md` will be visibly worse than against a stack convention — sections will be vague, dynamic-edge guidance will be hand-wavy, and cross-repo edges will mostly require manual confirmation. After a first pass, the user should run `extend-convention` to upgrade.

## Public surface

Whatever the repo's README, top-level docs, or package manifest declare as exports / endpoints / scripts. If none of those exist, treat the repo as opaque and produce only an `overview.md` summarizing what was indexable.

## Module shape

Follow whatever the directory layout suggests. A Louvain community is the best signal you have; treat each as a candidate module.

## Entry points (Pass 3)

Look for any file named `main`, `index`, `app`, or anything referenced from a top-level executable script (`Makefile`, `scripts/*`).

## Dynamic edges (Pass 4)

Without a convention you cannot anticipate which dynamic edges to look for. Ask the user during Pass 0 question 10. Encode whatever they list as bridge headings, but don't promise more than you've been told.

## Deployment signals (Pass 5)

- `Dockerfile` and any `compose.*.yml`.
- CI files (`.github/workflows/`, `.gitlab-ci.yml`, `Jenkinsfile`).
- Any deploy script under `scripts/` or `deploy/`.

## Manifest files

Whatever the repo has. List them in `reference/dependencies.md` even if you cannot parse them in detail.

## Cross-cutting pitfalls

You cannot reliably guess these. Leave the cross-cutting pages stubbed and recommend that the user run `extend-convention`.

## Cross-repo signals

Treat every `grafel_cross_links(action=list)` result as needing manual confirmation. Do not auto-accept.
