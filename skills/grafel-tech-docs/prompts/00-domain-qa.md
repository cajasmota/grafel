# Pass 0 — Domain Q&A

---

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use grafel MCP tools:
`grafel_inspect`, `grafel_find`, `grafel_expand`, `grafel_stats`,
`grafel_clusters`, `grafel_whoami`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `grafel_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `grafel_whoami` before doing anything else in this pass. If it errors:
ABORT with: "grafel MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."


---

You are a documentation architect. Before you index anything or query grafel, you need to understand the domain.

Run this pass **only on the first invocation of `generate-docs` for a given group**. Subsequent runs skip Pass 0 unless the user explicitly asks to rebuild domain context.

## What you produce

A short markdown file at `~/.grafel/groups/<group>/domain.md` with the user's answers. The orchestrator passes this file to every later writer subagent as required reading.

## Questions to ask

Ask the user, one batch at a time, in order. Stop and wait for answers before moving on.

### Batch A — Identity

1. What is the user-facing name of this group? (One line.)
2. One-paragraph description of what the group does.
3. Who is the primary audience for the docs you are about to generate? (Internal engineers, on-call SREs, external integrators, all of the above?)

### Batch B — Boundaries

4. List the repos in scope. For each, note:
   - Repo slug as registered with grafel.
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
- <description>; encode in `~/.grafel/docs/<group>/<repo-slug>/...` per ADR-0007.

## Doc preferences
- Emphasize: ...
- Exclude: ...
```

When finished, hand control back to the orchestrator with the path to `domain.md`.

---

**[pass-00 telemetry]** Print at end of this pass:
```
[pass-00] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
