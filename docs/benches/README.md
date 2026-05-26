# Sanitized Benchmark & Strategy Artifacts

This directory contains sanitized bench reports and strategy documentation from archigraph quality and performance work. Each artifact is a reproducible input for ongoing engineering efforts.

## Purpose

These reports preserve the quantitative and structural findings from benchmark runs, without including real client names, proprietary code shapes, or personal identifying information. Numbers, counts, percentages, and timing metrics are retained — they're not identifying. Real framework/language names (Django, React, Go) are kept un-anonymized.

Anonymization rules applied:
- Client names replaced with fixture labels (e.g., `client-fixture-a`)
- Repository names genericized (`<django-monorepo>`, `<react-typescript-repo>`, `<mobile-app-repo>`)
- Personal names and file paths omitted or replaced with roles
- Metrics, line counts, and percentages are OK to keep

## Contents

- **2026-05-26-quality-bench.md**: MCP quality benchmark report. Compares archigraph MCP vs grep+read on 10 representative questions. Identifies two structural bugs (HTTP linker, no field-access edges) and over-extraction issues. Grounds recommendation not to run `/generate-docs` until critical fixes ship.

- **2026-05-26-indexer-fix-strategy.md**: Detailed indexer/extractor/linker fix plan. Seven fixes across two phases: critical (HTTP linker, field edges), pruning (FK shadows, file doubles, try_catch noise, migrations, markdown). Effort estimates, risk assessments, verification procedures, and rollout ordering.

- **2026-05-26-perf-token-strategy.md**: MCP performance and token-overhead strategy. Identifies 5 latency hotspots (relationship scans, O(N²) loops, line I/O, bridge serialization, JSON round-trips) and 6 token-bloat sources (double-emission, full entity arrays, default depth, metadata duplication, ID-interning, prose notes). Quick-win vs structural fixes, verification plan.

## Reference

Each bench-driven issue (e.g., fixing the HTTP linker, adding field-access edges) should cite the artifact path in its description so reviewers can understand the measurement basis. Example:

> As observed in `docs/benches/2026-05-26-quality-bench.md` (cross-repo HTTP linker issue), the resolver produces 0/331 frontend call linkages due to API prefix mismatch.

## Updates

When a benchmark re-run occurs (e.g., after Phase 1 fixes ship), new artifacts are added to this directory with the run date in the filename. Older artifacts are retained for baseline comparison.
