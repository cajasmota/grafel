# Pass 10 — Pattern convergence + promotion (Phase 4 of ADR-0018)

---

## Staging path

Read `run_id` and `staging_path` from `~/.grafel/groups/<group>/plan.json` (written by Pass 2). All doc files produced by this pass MUST be written into `<staging_path>/<relative-path>` — NOT directly to `~/.grafel/docs/<group>/`. Wherever this prompt says `~/.grafel/docs/<group>/`, substitute `<staging_path>/`. The daemon promotes staging to canonical at the end of Pass 20.

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use grafel MCP tools:
`grafel_inspect`, `grafel_find`, `grafel_subgraph`, `grafel_orient (view=overview)`,
`grafel_orient (view=clusters)`, `grafel_orient (view=me)`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `grafel_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `grafel_orient (view=me)` before doing anything else in this pass. If it errors:
ABORT with: "grafel MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."


---

You are the `/generate-docs` coordinator running after Passes 4–7 produced the prose docs. Your job here is to aggregate per-subagent pattern observations and promote those that converged into approved patterns.

This pass produces no markdown. Its outputs are mutations on the pattern store via the `grafel_patterns` MCP tool.

## Preconditions

- Pass 1 (inventory) and Pass 4 (cluster) both finished. Each writer subagent had the opportunity to call `grafel_patterns(action=record, as_candidate=true, proposer_subagent=<id>)` whenever it observed ≥ `per_subagent_threshold` (default 2) instances of a structural recurrence within its cluster.
- The pattern store now contains a mix of approved patterns (from earlier runs) and candidates (`is_candidate=true`) from the current run.

## Procedure

### Step 1 — Enumerate candidates

```
grafel_patterns(action=query, include_candidates=true, text=<empty or wildcard>, category=<optional filter>)
```

Filter to `is_candidate=true`. The result has each candidate's trigger, exemplars, and `proposer_subagents` list.

### Step 2 — Cluster by similarity

For each pair of candidates `(a, b)`:

1. Compute trigger-text similarity (BM25 cosine on the union of `trigger.natural_language` + `trigger.keywords`).
2. Require similarity ≥ `cluster_similarity_threshold` (default 0.8).
3. Require at least one overlapping exemplar entity.

When both conditions hold, treat them as members of the same cluster. The β implementation in `internal/mcp/patterns.go` (`tryMergeCandidate`) does this merge at `record` time; this pass re-runs the same logic against the full candidate list to catch cross-subagent convergence that record-time merging may have missed.

### Step 3 — Promote convergent clusters

For each cluster:

- Count distinct proposer subagents.
- If the count is ≥ `convergence_threshold` (default 3), call:

  ```
  grafel_patterns(action=promote, candidate_id=<merged_id>, approval_note=<optional>)
  ```

  Promotion does NOT auto-approve. It sets the candidate aside for user confirmation and emits the promoted record in the response payload.

### Step 4 — Surface for user approval

List every promoted candidate to the user:

- Trigger one-liner.
- Exemplar count + sample paths.
- Proposer subagent list (audit trail).
- Suggested category.

The user replies with approve / reject / refine for each. For approval, the candidate's `is_candidate` flag is already set to false by the promote action; nothing else to do. For refine, call `grafel_patterns(action=refine, pattern_id=..., changes={...})`. For reject, call `grafel_patterns(action=reject, pattern_id=..., reason=...)`.

### Step 5 — Carry forward

Non-convergent candidates stay in the store with `is_candidate=true`. They may converge in a future `/generate-docs` or `/grafel-patterns-discover` run. The `grafel patterns gc` subcommand prunes them when they exceed `candidate_decay_days` (default 90).

## Constraints

- DO NOT promote a candidate that has < `convergence_threshold` proposers, even if its similarity score is high.
- DO NOT silently merge candidates across categories; promote within a category only.
- DO NOT delete candidates here — the gc subcommand owns lifecycle pruning.

---

**[pass-10 telemetry]** Print at end of this pass:
```
[pass-10] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
