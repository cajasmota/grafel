# `<repo-slug>` deployment reference

> How this repo gets to production.

## Deploy target

> One paragraph: AWS account, GCP project, K8s cluster, static-host provider, etc.

## Build

- Build command: `<command>`.
- Output artifact: `<path or image>`.
- Build-time env vars: see [`config.md`](./config.md).

## Deploy mechanism

> One paragraph: CI pipeline, GitOps controller, manual `cdk deploy`, etc. Name the workflow file in backticks.

## Pipeline stages

1. `<stage>` — <trigger> — <command>.

## Health checks

| Endpoint | Purpose | Expected response |
| -------- | ------- | ----------------- |

## Rollback

> One paragraph: how to roll back, who can do it, where to find the previous version.

## Production access

- On-call rotation: <where to find>.
- Logs: <where>.
- Metrics: <where>.

## Known deployment pitfalls

> Pull from the convention's `cross_cutting_pitfalls` and from prior incidents (if `grafel_index_status` surfaced any).
