# extend-convention

Add a new stack convention for the `generate-docs` skill. Run when the user has a repo whose framework or runtime is not covered by any existing file under `skills/generate-docs/conventions/`.

## When to use this skill

- The user said in Pass 0 of `generate-docs` that none of the listed conventions match their repo.
- The user explicitly asks: "Add a convention for `<stack>`."
- A `generate-docs` run aborted at Pass 0 with the message: "stack X has no convention; run `extend-convention`."

The CLI surface (per grafel's CLI plan) for invoking this skill is:

```bash
grafel conventions add <stack>
```

The CLI command creates a stub convention file at `~/.claude/skills/generate-docs/conventions/<stack>.md` and then triggers this skill to fill it. If the CLI is unavailable, the user can place a stub manually and invoke this skill directly.

## Inputs

- The target stack name (e.g., `flask`, `nestjs`, `gin`, `nextjs-app-router`).
- A path to the stub file the agent will fill.
- A path to a reference codebase (the user's repo) that uses the stack.
- The skill always reads `_graph-searchability.md` and **two** existing conventions for structural reference. Pick the two most similar:
  - For a new Python framework: read `django.md` and `fastapi.md`.
  - For a new Node framework: read `react.md` and `nodejs-generic.md`.
  - For a new infra stack: read `infra-cdk.md` and `infra-terraform.md`.
  - For a new mobile stack: read `react-native.md` and one web equivalent.
  - Otherwise: read `generic.md` and the closest match.

## Procedure

### Step 1 — Internalize structure

Read the universal convention `_graph-searchability.md` first, then the two reference conventions. Note their section order — it is not negotiable. Every convention follows the same shape:

1. Required reading callout.
2. Public surface (numbered list).
3. Module shape (with directory tree).
4. Entry points (Pass 3).
5. Dynamic edges (Pass 4).
6. Deployment signals (Pass 5).
7. Manifest files (Pass 5 — `dependencies.md`).
8. Cross-cutting pitfalls.
9. Cross-repo signals.

### Step 2 — Inspect the user's codebase

Run, against the user's repo:

```bash
grafel index <repo>          # if not already indexed
```

Then via MCP, with the repo's CWD active so `grafel_whoami` resolves correctly:

- `grafel_graph_stats(repo_filter=["<repo>"])` — top entity kinds and edge kinds tell you what the indexer found.
- `grafel_search(question="entry points")` — confirms what runs the app.
- `grafel_search(question="public API surface")` — confirms what's exposed.
- `grafel_list_clusters(repo_filter=["<repo>"])` — confirms the natural module boundary.

Read at most ~10 source files to confirm structural assumptions. Do not read the whole repo — the question is structural, not behavioral.

### Step 3 — Ask the user targeted questions

You must ask, even if the codebase suggests answers:

1. **Stack identity.** What is the canonical name of this stack? Any close variants (e.g., "Flask vs Quart")?
2. **Public surface.** Which of these does the framework consider "public":
   - HTTP routes / RPC services / CLI entries / package exports / event handlers / scheduled tasks / native modules / IaC outputs?
   - Anything else specific to the stack?
3. **Module shape.** What is the directory layout the framework expects or its community has standardized on? Are there places where `grafel_list_clusters` will reliably mis-cluster?
4. **Entry points.** What file does the framework start at? What declares config? What declares dependencies?
5. **Dynamic edges.** What runtime couplings does the stack create that static analysis misses? List 2-5 patterns. (Examples to prompt the user with: middleware ordering, signal dispatch, plugin discovery, dependency injection containers, ARN references, label selectors.)
6. **Deployment signals.** How does this stack typically deploy? Containers, serverless, static hosting, on-prem? What configuration files are involved?
7. **Manifest files.** Which files declare dependencies? Which lockfile is canonical?
8. **Cross-cutting pitfalls.** What is the top "gotcha" for a new engineer joining a project on this stack? Aim for 2-4 items.
9. **Cross-repo signals.** How does a service on this stack typically connect to other services? Which signals does grafel's `grafel_list_link_candidates` need to trust by default?

### Step 4 — Draft the convention

Open the stub file and fill it in the order listed in Step 1. Use the language of the reference conventions — terse, opinionated, concrete examples. Backtick every code identifier. Use language tags on every code block.

The convention should be ~80-150 lines of markdown. Less than that means you didn't dig deep enough; more than that means you wandered into prose.

### Step 5 — Self-review

Run the verification checklist from `generate-docs/snippets/verification-checklist.md` against the new convention:

- Backtick discipline.
- Code-block tags.
- No empty sections.
- No placeholder text.
- No mention of any prior tooling name (only `grafel`).

Then a content review:

- Section order matches the canonical shape.
- Each section has at least one concrete example, not just abstract guidance.
- Cross-cutting pitfalls list at least two stack-specific items, not generic advice.
- The "Cross-repo signals" section names which auto-accept / require-confirmation policy applies (consistent with how the existing conventions guide Pass 8).

### Step 6 — Show the user

Print the rendered convention back to the user with a short summary:

> I drafted `conventions/<stack>.md` with the following sections: ... Run `generate-docs` again — it will pick up the new convention automatically.

If the user requests edits, apply them and re-run Step 5.

### Step 7 — Install

The convention file lives in this repo at `skills/generate-docs/conventions/<stack>.md`. The `grafel skills install` command (per the grafel CLI surface) syncs `skills/` into:

- `~/.claude/skills/generate-docs/conventions/`
- `~/.codeium/windsurf/skills/generate-docs/conventions/`

If the user has not run `grafel skills install` since the convention was created, remind them to do so before re-running `generate-docs`.

## Notes

- This skill never touches conventions other than the new one. Never edit `_graph-searchability.md` or any built-in convention from this skill — those are owned by the `generate-docs` skill itself.
- If the user's stack is a thin wrapper over an existing stack (e.g., "Quart is async Flask"), prefer extending the existing convention with a note rather than creating a new file. Ask the user before forking.
