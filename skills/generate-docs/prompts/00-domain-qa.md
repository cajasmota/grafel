# Pass 0 — Domain Q&A

---

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use archigraph MCP tools:
`archigraph_inspect`, `archigraph_find`, `archigraph_expand`, `archigraph_stats`,
`archigraph_clusters`, `archigraph_whoami`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `archigraph_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `archigraph_whoami` before doing anything else in this pass. If it errors:
ABORT with: "archigraph MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."

## CRITICAL STORAGE DISCIPLINE
===========================
All generated documentation MUST be written under:
  `~/.archigraph/docs/<group>/...`

Determine `<group>` via the `archigraph_whoami` MCP call (the Pre-flight assertion
above). Pass it through every subsequent file write as `${OUTPUT_ROOT}`.

You are STRICTLY FORBIDDEN from writing documentation files into:
- The source repo's working tree (anywhere under `<repo>/docs/`, `<repo>/doc/`, etc.)
- The CWD unless CWD is already inside `~/.archigraph/docs/<group>/`
- Any path that is a git working directory

If you find yourself about to write to a repo path, STOP. The skill assumes
the archigraph-owned store. Writing elsewhere breaks the storage contract
and pollutes the user's source repo.

The daemon dashboard reads from `~/.archigraph/docs/<group>/` -- any output
written elsewhere is invisible to it.

### Pre-flight storage assertion -- SECOND action in this pass

Compute and verify the output root immediately after the `archigraph_whoami` call:

```bash
OUTPUT_ROOT="$HOME/.archigraph/docs/<group>/"   # substitute <group> from whoami
mkdir -p "$OUTPUT_ROOT"
echo "OUTPUT_ROOT=$OUTPUT_ROOT"
```

All file writes in this pass MUST use `${OUTPUT_ROOT}<relative-path>`. Never write to any
other location. If `mkdir -p` fails, ABORT: "Cannot create output directory at $OUTPUT_ROOT."
## CRITICAL OUTPUT DISCIPLINE
==========================
The generate-docs skill produces markdown files in the canonical store
at `~/.archigraph/docs/<group>/`. It does NOT produce:
- VitePress / Docusaurus / Sphinx / mkdocs scaffolding
- `package.json` or any build manifests for static site generators
- Any non-markdown asset that wraps the docs for publishing
- `.gitignore` entries

Publishing is downstream — handled by the archigraph dashboard or
external tooling. If you find yourself about to write a `config.ts`,
`package.json`, `mkdocs.yml`, `.vitepress/config.ts`, or any build
manifest, STOP. The skill's job is content, not infrastructure.




---


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
- <description>; encode in `~/.archigraph/docs/<group>/<repo-slug>/...` per ADR-0007.

## Doc preferences
- Emphasize: ...
- Exclude: ...
```

When finished, hand control back to the orchestrator with the path to `domain.md`.

---

**[pass-00 telemetry]** Print at end of this pass:
```
[pass-00] archigraph MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
