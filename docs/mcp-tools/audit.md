# MCP tools — Audit

[← Back to the MCP tools index](../mcp-tools.md)

Security, secrets, licenses, test coverage, dead code, and cross-group parity diffs (oracle vs v3).

---

## `archigraph_auth_coverage`

Flag HTTP endpoints missing auth.

Key parameters: `repo_filter[]`, `only_missing` (bool), `format` (`terse`/`full`), `verbose` (bool), `limit` (default 50), `token_budget`.

Output: per-endpoint auth status with severity (error = sensitive/IDOR, warn = unauthenticated public, info = covered).

---

## `archigraph_secrets`

Scan for hardcoded secrets; masked findings by severity.

Key parameters: `severity`, `limit` (default 200).

Output: masked credential findings by severity. Test fixtures and opt-out comments are suppressed.

---

## `archigraph_license_audit`

Audit dependency licenses; flag GPL/AGPL conflicts.

Key parameters: `group`/`cwd`.

Output: dependency records flagged by license kind (GPL/AGPL conflict detection).

---

## `archigraph_test_coverage`

Production entities with no TESTS edge, ranked by severity.

Key parameters: `entity_id` (optional — scoped query), `repo_filter[]`, `severity`, `limit` (default 100), `top_directories` (bool).

Output: production entities lacking TESTS edges, ranked by severity.

---

## `archigraph_dead_code`

Reachability dead-code: entities unreached by entry-points.

Key parameters: `repo_filter[]`, `kind_filter`, `from`, `limit` (default 200).

Output: entities not reachable from the entry-point set.

---

## `archigraph_find_dead_code`

Dead/unwired code: isolated, marked-unused, or test-only-referenced symbols.

Key parameters: `repo_filter[]`, `kind_filter`, `limit` (default 100), `min_confidence`.

Output: entities with zero project edges; may be genuine dead code or an extractor gap.

---

## `archigraph_security_findings`

Taint-flow security findings: source → sink paths ranked by confidence.

Key parameters: `category`, `min_confidence`, `limit`, `source_repo`.

Output: ranked taint-flow findings with source → sink paths.

---

## `archigraph_contract_test_effectiveness`

Tautological-spec detector: assertions that can never fail (oracle-blind).

Key parameters: `group`/`cwd`.

Output: contract tests whose assertions cannot fail.

---

## `archigraph_literal_parity`

Cross-group ConstantSet/enum value-set parity diff (oracle vs v3).

Key parameters: `group_oracle` (required), `group_v3` (required), `set` (required).

Output: per-set value parity diff between the two groups.

---

## `archigraph_auth_posture_diff`

Cross-group auth-posture parity diff per linked endpoint (oracle vs v3).

Key parameters: `group_oracle` (required), `group_v3` (required).

Output: per-endpoint auth-posture parity diff.

---

## `archigraph_stub_detector`

Cross-group stub detector: v3 pure where oracle computes (effects).

Key parameters: `group_v3` (required), `group_oracle` (required).

Output: endpoints/functions that are pure in v3 but compute in the oracle.

---

## `archigraph_response_shape_diff`

Cross-group branch-aware response-shape parity diff per endpoint (oracle vs v3).

Key parameters: `group_oracle` (required), `group_v3` (required).

Output: per-endpoint response-shape parity diff.
