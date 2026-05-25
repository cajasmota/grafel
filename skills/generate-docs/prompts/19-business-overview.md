# Pass 19 — Business overview / landing page (BUSINESS tier)

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


The last business pass. Produce the single landing page a PM opens first: what
the product does, who uses it, and an index into the capabilities, journeys,
domain glossary, and rules written by Passes 15–18. It runs last because it
links to everything those passes produced.

> **READ FIRST:** `snippets/business-voice.md`. Binding.

Synthesised across the whole group.

## Inputs

- `~/.archigraph/groups/<group>/domain.md`.
- The business pages written by Passes 15–18 (glossary, capabilities/*,
  journeys/*, rules/index.md) — you index all of them.

## Output

```
~/.archigraph/docs/<group>/business/overview.md
```

Use `output-templates/business-overview.md`. This is the root of the Business
view the webui surfaces (#1634): the chooser's "Business" tab lands here.

## Procedure

### Step 1 — Write the pitch

Two or three plain-language paragraphs: what the product is, the problem it
solves, who it serves. Lift the framing from `domain.md`, translated to business
voice. No component names.

### Step 2 — Build the indexes

- **Who uses it** — table of user types and their goals.
- **What it does** — one bullet per capability page from Pass 16, each a
  business sentence linking to `capabilities/<slug>.md`.
- **Core ideas** — point at `domain-glossary.md`.
- **How a typical job flows** — one bullet per journey from Pass 17 linking to
  `journeys/<slug>.md`.
- **Rules the product enforces** — 2–3 headline rules + link to `rules/index.md`.

Every link must resolve (the page must already exist from Passes 15–18) — apply
`snippets/link-hygiene.md`. Do not link to a capability/journey that was not
written.

### Step 3 — Anchors + provenance

Headings first, derive `anchors:` per `snippets/anchor-contract.md`. Provenance
in the collapsed `<details>` block.

### Step 4 — Verify + save

Run `snippets/verification-checklist.md`, then run the business-tier link check:
confirm every link from `overview.md` resolves to a file that exists under
`business/`.

```
archigraph_save_finding(
  question="What is the business overview of the <group> group?",
  answer="<file: ~/.archigraph/docs/<group>/business/overview.md>",
  type="business_overview",
)
```

Hand back to the orchestrator. The business tier is complete:
`business/overview.md`, `business/capabilities/*`, `business/domain-glossary.md`,
`business/journeys/*`, `business/rules/index.md`.

---

**[pass-19 telemetry]** Print at end of this pass:
```
[pass-19] archigraph MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
