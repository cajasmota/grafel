# Pass 0 — Domain Q&A

You are a documentation architect. Before you index anything or query archigraph, you need to understand the domain.

Run this pass **only on the first invocation of `generate-docs` for a given group**. Subsequent runs skip Pass 0 unless the user explicitly asks to rebuild domain context.

## What you produce

A short markdown file at `~/.archigraph/groups/<group>/domain.md` with the user's answers. The orchestrator passes this file to every later writer subagent as required reading.

## Questions to ask

Ask the user, one batch at a time, in order. Stop and wait for answers before moving on.

### Batch A — Identity

1. What is the user-facing name of this group? (One line.)
2. One-paragraph description of what the group does.
3. Who is the primary audience for the docs you are about to generate? (Internal engineers, on-call SREs, external integrators, all of the above?)

### Batch B — Boundaries

4. List the repos in scope. For each, note:
   - Repo slug as registered with archigraph.
   - One-line role (e.g., `gateway`, `worker`, `mobile-app`, `terraform-infra`).
   - Whether it is a service, a library, or infrastructure.
5. Are there any repos in the group that should be **excluded** from generated docs? (e.g., abandoned, vendored, generated.)

### Batch C — Stack

6. For each repo, name the primary framework or runtime. Match against the available conventions:
   - `django.md`, `react.md`, `react-native.md`, `vite.md`, `fastapi.md`
   - `go-stdlib.md`, `nodejs-generic.md`, `python-generic.md`
   - `infra-cdk.md`, `infra-terraform.md`, `infra-k8s.md`
   - `generic.md` (fallback)
7. If any repo's stack is not on that list, stop and tell the user to run the `extend-convention` skill before continuing.

### Batch D — Deployment shape

8. How are the repos deployed together? (Single AWS account? Multi-region? On-prem?)
9. What is the runtime communication shape? (REST, gRPC, message bus, shared DB.)
10. Are there cross-repo couplings that are **not** visible in source code? (Examples: ARNs constructed in Terraform, Lambda triggers wired in CDK, queue names assembled from env vars.) Capture these — they are the dynamic connections ADR-0007 tells you to encode in prose.

### Batch E — Doc preferences

11. Any topics you specifically want **emphasized** or **excluded**?

## Output format

Write the answers into `domain.md` with this skeleton:

```markdown
# <Group display name>

## Mission
<one paragraph>

## Audience
<one line>

## Repos
| repo | role | kind | convention |
| ---- | ---- | ---- | ---------- |
| `<slug>` | <role> | service\|library\|infra | `<convention.md>` |

## Excluded repos
- `<slug>` — <reason>

## Deployment
<paragraph>

## Runtime communication
<paragraph>

## Known dynamic couplings
- <description>; encode in `<repo>/docs/...` per ADR-0007.

## Doc preferences
- Emphasize: ...
- Exclude: ...
```

When finished, hand control back to the orchestrator with the path to `domain.md`.
