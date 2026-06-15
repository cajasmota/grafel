# AWS CDK convention

Required reading: `_graph-searchability.md`.

Applies to repos whose primary purpose is AWS infrastructure defined with CDK (TypeScript or Python).

## Public surface

A CDK repo's "public surface" is the set of stacks and constructs it exposes:

1. **Stacks** — every `cdk.Stack` subclass instantiated in the app entry. Each stack is a deploy unit.
2. **L3 constructs** — composite constructs the repo defines and re-uses or exports.
3. **Stack outputs** — every `CfnOutput` and every cross-stack reference (`stack.exportValue`).
4. **Context keys** — values read via `app.node.tryGetContext(...)`.

## Module shape

Typical TypeScript layout:

```
bin/
  app.ts            # cdk.App() + stack instantiations
lib/
  <stack>.ts        # one Stack per file or grouped by domain
  <construct>.ts    # L2/L3 constructs
config/
  <env>.json        # per-environment values
test/
```

A community will usually map to a single stack file or to a tightly-bundled set of constructs.

## Entry points (Pass 3)

- `bin/app.ts` (or whatever `cdk.json` `app` points to).
- `cdk.json` — synth/deploy entry config.
- Per-environment context files referenced from `cdk.json`.

## Dynamic edges (Pass 4)

The whole point of an IaC repo is to encode runtime couplings. Most edges are dynamic:

- **ARN references** — a Lambda's IAM role is granted on a queue's ARN built at deploy time. Both ends should be backticked in headings: `` ## How `OrderProcessorFn` consumes from `OrdersQueue` ``.
- **Cross-stack references** — when stack A exports a value and stack B imports it, encode the bridge.
- **Event sources** — `lambda.addEventSource(new SqsEventSource(queue))` couples a function to a queue invisibly to anyone reading the function's source.
- **EventBridge rules** — `Rule.addTarget(...)` couples a producer event pattern to one or more targets.

Pass 4 writers should produce a `flows.md` per stack listing every `addEventSource`, every `grant*` call, and every cross-stack reference, each as a backticked-heading section.

## Deployment signals (Pass 5)

- `cdk.json` — the source of truth for `synth`/`deploy`.
- CI files that run `cdk deploy` — what triggers it, what env it targets.
- `cdk.context.json` — cached context (lookups). Note that the file is often committed.

## Manifest files

`package.json` (TypeScript CDK) or `pyproject.toml` (Python CDK). `cdk.json` is config, not a manifest.

## Cross-cutting pitfalls

- **Drift** — CDK assumes nothing else mutates the stack. Note any known out-of-band changes.
- **Bootstrap version** — `cdk-bootstrap` writes a stack into the target account. Mismatched bootstrap versions across accounts cause silent failures.
- **Cross-region / cross-account** — explicit `crossRegionReferences` and trust relationships need their own subsection.

## Cross-repo signals

CDK is the prime source of cross-repo dynamic edges. A Lambda defined here that consumes from a queue produced by another team's repo is exactly the kind of edge ADR-0007 is built to capture. When `grafel_cross_links(action=list)` proposes an edge to/from this repo's stack outputs or to a Lambda handler whose code lives in another repo, accept with high confidence — the IaC repo is usually the canonical witness.
