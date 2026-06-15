---
name: grafel-feedback
description: >
  Generate a privacy-preserving anonymized quality report for sharing with grafel
  maintainers. Covers extractor coverage, orphan rate, resolution disposition, and
  framework recognition. All identifiers are hashed; file paths are scrubbed. Fully
  offline — no network calls, no telemetry, no auto-issue-creation.
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

## What the report covers (Phase 1)

| Section | Contents |
|---|---|
| 1. Extractor Coverage | Entity counts by language, kind distribution, source-window completeness, annotation coverage, field extraction rate |
| 2. Orphan Rate | Per-kind orphan rate (entities with no semantic outgoing edges) |
| 3. Resolution Disposition | Breakdown of edge resolution outcomes (resolved, external-known, bug-extractor, …) |
| 4. Framework Recognition | Framework detector hit counts per recognized framework |
| 5. Cross-Stack Flows | _(Phase 2)_ |
| 6. Docgen Quality | _(Phase 2)_ |
| 7. Sanity Check Details | Which automated checks passed / failed, and why |

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
