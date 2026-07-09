---
name: grafel-feedback
description: >
  Generate a privacy-preserving anonymized quality report for sharing with grafel
  maintainers, then layer an agent-driven "Findings & Interpretation" section on top that
  ranks the most likely extractor/resolver gaps, flags report self-inconsistencies, surfaces
  indexing phase timings, and gives maintainers a prioritized shortlist. Covers extractor
  coverage, orphan rate, resolution disposition, and framework recognition. All identifiers
  are hashed; file paths are scrubbed. Fully offline — no network calls, no telemetry, no
  auto-issue-creation. The interpretation is synthesized purely from the already-anonymized
  report — never from source.
when-to-use: >
  User asks to "file feedback", "generate a feedback report", "report an grafel
  quality issue", "share extraction quality data", or invokes /grafel-feedback
  explicitly. Also useful when a user notices that specific entity kinds are missing,
  orphan rates are unexpectedly high, or framework annotations are not being detected.
---

# grafel-feedback

Generate an anonymized quality report that you can share with grafel maintainers
to help improve extractor coverage, resolver accuracy, and framework support — without
revealing any source code, file paths, or identifier names.

## Privacy promise

The report contains:
- **Entity name hashes**: per-report ephemeral salt (from `crypto/rand`), 4-hex output
  (e.g. `ent_a3f7`, `op_92c1`). Salt is never persisted and never logged.
- **Path templates**: `<go>/<seg-1>/<seg-2>.go` — depth preserved, all segments replaced.
- **Count ranges**: exact entity counts are bucketed (1-5, 6-20, 21-100, 100+).
- **Structural labels only**: kind names (`function`, `class`), language names, and
  framework annotation names (`@GetMapping`, `@Inject`) are not hashed — they are
  public framework vocabulary and essential for maintainers to diagnose issues.

The report does **not** contain:
- Source code (zero lines of code).
- Real file paths (depth + extension only).
- Real identifier names (hashed to 4 hex).
- Any network requests (fully offline).
- Any automatic issue creation (you decide whether to share).

## How to run

```
grafel feedback [--group <name>] [--out <path>] [--yes]
```

**Flags:**
- `--group <name>` — which group to analyse (default: inferred from your current directory).
- `--out <path>` — where to write the report (default: `~/.grafel/feedback/<group>-<timestamp>.md`).
- `--yes` — skip the confirmation prompt (useful for CI or scripting).

**Example:**
```
grafel feedback --group my-service
```

The CLI will show you what will (and will not) be collected, then ask for confirmation
before generating the report.

## Phase: synthesize the interpretation (do this after `grafel feedback` completes)

The deterministic collector emits statistics but **no interpretation** — historically every
genuinely useful finding in a feedback report was prose a human wrote by staring at the tables.
This phase makes *you*, the agent, write that prose automatically. After the `.md` report is
generated, **open it, read every section, and append a new `## Findings & Interpretation`
section to the end of the same file.**

### Privacy constraint (read first — non-negotiable)

The synthesis operates **only** on the already-anonymized report the collector produced: kind
names, counts, percentages, orphan rates, disposition vectors, framework vocabulary, and phase
timings. You must **never**:

- re-read the source tree, re-run indexing, or open any file other than the report `.md`;
- introduce a real identifier, real file path, repo name, or any string not already present in
  the anonymized report;
- infer or reconstruct a real name from a hash.

Everything you write must be derivable from the report's public framework vocabulary and its
numbers. If a hypothesis would require looking at source to confirm, phrase it as a hypothesis
for maintainers — do not go look. State this constraint explicitly at the top of the section you
write (one sentence), so a reader knows the interpretation is source-blind.

### What to write in `## Findings & Interpretation`

Produce these four subsections, in order:

#### 1. Ranked extractor / resolver gaps

Read the **Orphan Rate** table (section 2), the **Resolution Disposition** vector (section 3), and
the kind distribution (section 1). Rank the kinds with the highest orphan rate **and** a
meaningful N (ignore kinds with N < 10 — small samples are noise). For each of the top gaps, write:
a one-line **observation** (kind, orphan %, N-bucket), a **hypothesis** for the root cause, and a
**suggested fix** aimed at the extractor or resolver. Apply these concrete inference rules:

- **High-orphan data-layer kinds** (`DataAccess`, `Datastore`, `Schema`, `Model`, `Repository`,
  `Entity`/ORM entities) ⟹ likely **missing `DataAccess → Datastore` READS/WRITES edges**: the
  data-access objects are extracted but never linked to what they read/write. Suggested fix:
  audit the resolver rule that binds query/repository methods to their backing table/collection.
- **High `http_endpoint` orphan rate** ⟹ either **client repos are not in the indexed group** (so
  the endpoints have no inbound callers) **or** the endpoints use **dynamic / prefixed / templated
  URLs** the resolver can't match statically. Suggested fix: confirm the caller repos are in the
  group; if so, look at URL templating (path prefixes, base-URL constants, router-mounted subpaths).
- **Async handler kinds present but 0 callers** (e.g. `message_topic` / event handlers /
  `@KafkaListener` / queue consumers exist in the kind table but their orphan rate is ~100% or the
  disposition shows no inbound edges) ⟹ **topic → handler edges are not modeled**: the producer
  side publishes to a topic string the consumer subscribes to, but the resolver doesn't join them.
  Suggested fix: model the topic-name binding between publisher and subscriber.
- **High orphan on a kind whose framework IS recognized** (framework hit count ≥ 1 in section 4
  but the kind it should annotate is orphaned) ⟹ the **detector fires but the edge-builder for
  that framework rule is missing or partial**. Suggested fix: name the framework and the specific
  annotation, and point at the edge rule that should follow from it.

Rank by **impact = orphan_rate × N-bucket weight** (a 90%-orphan kind with N in 100+ matters more
than a 100%-orphan kind with N of 11). List the top gaps, most impactful first.

#### 2. Auto-flagged report self-inconsistencies (the D-class)

Before trusting any single number, cross-check the sections against each other and flag
**collector bugs** — a metric that contradicts another metric is a bug in the *report*, not a real
gap in the user's codebase. **Do not report a self-inconsistent metric as a genuine finding** —
flag it as a `[D] collector self-inconsistency` so maintainers fix the collector, not the extractor.
Rules:

- If a completeness / extraction / coverage metric reads **0% or "none found"** for a kind, **but
  the kind distribution table shows those entities exist** (N > 0), flag it: the entities were
  extracted, so a 0% coverage reading is a **collector measurement bug**, not a missing extractor.
- If the **resolution disposition vector does not sum to 100% ± 0.1%** (and section 7 didn't
  already catch it), flag a disposition-accounting bug.
- If **framework hits ≥ 1** for a framework but **every kind that framework should produce has
  orphan rate exactly 100% at large N**, flag a possible double-counting or detector/edge-builder
  mismatch rather than assuming the whole framework is unsupported.
- If a percentage is reported against a **denominator of 0** (e.g. "0 of 0 windows complete = 0%"),
  flag it as a divide-by-zero / vacuous metric, not a quality problem.

For each flag, note which two sections disagree and what the corrected reading likely is.

#### 3. Indexing time / phase timings

If the report includes phase-timing fields (`extract_ms`, `link_ms`, resolver/algo runtime, or an
overall indexing-duration line — being added under the timings work), surface them:

- Identify the **dominant phase** (largest share of total time) and state its share.
- Classify the profile: **cold-index dominated** (extraction/parse is the bulk) vs
  **enrichment dominated** (linking / resolution / algorithms are the bulk). Say which, and what it
  implies (e.g. enrichment-dominated + high orphan rate suggests the resolver is doing a lot of
  work but still not closing edges — a resolver-rule efficiency signal).
- If no timing fields are present in the report, write one line: "No phase timings present in this
  report version." and move on. **Do not fabricate timings.**

#### 4. Top 3 things for maintainers

Close with a short prose list titled **"Top 3 things for maintainers"** — the three highest-value
actions, drawn from subsections 1–3, ordered by expected payoff. Each item is one or two sentences:
what to fix and why it matters (how many entities / how much of the graph it would unblock). This is
the part a maintainer reads first, so make it decisive and specific to the numbers in this report.

### After appending

Re-run the verification checklist below over the **whole** file (the substrate plus your appended
section) to confirm no real identifier or path slipped in through the interpretation. Only then is
the report ready to share.

## How to verify the report before sharing

1. Open the generated `.md` file in any text editor or markdown viewer.
2. Check that **no real source code** appears anywhere. The report should contain only
   markdown tables, percentage statistics, and hash-like identifiers (e.g. `ent_a3f7`).
3. Check that **no real file paths** appear. All paths should look like `<go>/<seg-1>/<seg-2>.go`.
4. Check that **no real class or function names** appear. All entity references should
   be in the form `<kind-prefix>_<4-hex>` (e.g. `op_92c1`, `ent_b61d`).
5. Framework annotation names like `@GetMapping` or `@Inject` **are** allowed — they are
   public framework vocabulary and help maintainers understand which framework rules fired.

If you spot any real identifier or path that was not scrubbed, **do not share the report**
and file an issue at https://github.com/cajasmota/grafel/issues describing the leak.

## How to file the GitHub issue

Once you have verified the report:

1. Go to https://github.com/cajasmota/grafel/issues/new?template=feedback-report.yml
2. Check the anonymization-verification box confirming you have reviewed the report.
3. Paste the full contents of the `.md` file into the **Feedback report** field.
4. Fill in the **grafel version** (shown in the report header).
5. Select the **Impact** category that best describes your issue.
6. Submit the issue.

Maintainers will triage the report using the confidence score, the orphan-rate table,
and the resolution disposition vector to identify the most likely extractor or resolver gap.
The agent-synthesized **`## Findings & Interpretation`** section (and its "Top 3 things for
maintainers" list) is written to make this triage fast — it pre-ranks the likely gaps and flags
any collector self-inconsistencies — but it is a hypothesis aid layered on the deterministic
substrate, not ground truth. When choosing the issue's **Impact** category, let the ranked
findings guide you (extractor gap vs resolver bug vs framework support vs performance).

## What the report covers

The report is produced in **two layers**:

- **The deterministic substrate** (sections 1–7 below) is written offline by the `grafel feedback`
  Go collector. It is pure statistics — counts, percentages, and pass/fail checks — with **no
  interpretation**. This layer is unchanged and fully reproducible.
- **The interpretation layer** (section 8, `## Findings & Interpretation`) is appended by *you*,
  the running agent, in a second phase (see "Phase: synthesize the interpretation" below). It reads
  only the anonymized substrate and turns statistics into ranked hypotheses, self-consistency flags,
  timing call-outs, and a maintainer shortlist. Every genuinely actionable finding lives here.

| Section | Contents |
|---|---|
| 1. Extractor Coverage | Entity counts by language, kind distribution, source-window completeness, annotation coverage, field extraction rate |
| 2. Orphan Rate | Per-kind orphan rate (entities with no semantic outgoing edges) |
| 3. Resolution Disposition | Breakdown of edge resolution outcomes (resolved, external-known, bug-extractor, …) |
| 4. Framework Recognition | Framework detector hit counts per recognized framework |
| 5. Cross-Stack Flows | _(Phase 2)_ |
| 6. Docgen Quality | _(Phase 2)_ |
| 7. Sanity Check Details | Which automated checks passed / failed, and why |
| 8. Findings & Interpretation | **Agent-synthesized** — ranked extractor/resolver gap hypotheses with suggested fixes, auto-flagged report self-inconsistencies, indexing phase-timing call-outs, and a "Top 3 for maintainers" list. Derived solely from sections 1–7. |

## Confidence score

The report header includes a **Confidence** percentage, computed as the fraction of
automated sanity checks that passed:

- Entity count > 0 for each indexed language
- Orphan rate < 100% for all kinds with N >= 10
- Resolution vector sums to 100% ± 0.1%
- Framework hits >= 1 if known-framework files were detected
- Total entities >= 50 (else the report is suppressed entirely)

A low confidence score (e.g. 40%) may indicate a partial index or an unusual
environment. The report may still contain useful signals — confidence is a triage
aid for maintainers, not a quality gate.

## Minimum codebase size

The report requires at least **50 indexed entities**. Below that threshold, metrics
are statistically unreliable and small-sample combinations could fingerprint the
codebase. The CLI will emit a suppression notice instead of a full report.

## Phase 2 (not yet available)

Phase 2 will add:
- Failure pattern section (AST node-type patterns for top-5 failures)
- Expected vs actual edge tables
- Synthetic fixture tarball generation
- Docgen quality section
- Cross-stack flow section
