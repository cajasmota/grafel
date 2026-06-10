package mcp

// TokenCeiling is the maximum allowed token count for the MCP tool
// handshake response. Enforced by cmd/mcp-audit and asserted by
// budget_test.go.
//
// History:
//   - 4200 → 5000: PR #2442 orphan-handler re-wires
//     (archigraph_cross_links, archigraph_save_finding,
//     archigraph_list_findings, archigraph_license_audit).
//   - 5000 → 5500: PR for #2770 Phase 2A — adds archigraph_payload_drift
//     for cross-repo schema-drift findings. The tool itself is minimal
//     (only declared args: group, cwd; severity/endpoint/repo/limit are
//     undeclared per the #1639 token-ceiling pattern) but the corpus of
//     existing tools already sits near the ceiling, leaving no
//     headroom for a 48th tool entry without a bump.
//   - 5500 → 6000: PR for #2772 Phase 2B — adds
//     archigraph_security_findings for taint-flow source→sink findings.
//     With #2770's payload-drift tool already in (49 tools post-rebase),
//     the new tool's category / min_confidence / limit / source_repo
//     args push the handshake near the prior ceiling; +500 restores
//     headroom under the 6500 next-bump line.
//   - 6000 → 6500: PR for #2774 / #2775 Phase 3 misc — adds four
//     sidecar-reader tools (archigraph_pure_functions, archigraph_
//     import_cycles, archigraph_def_use, archigraph_template_patterns)
//     for the pure-function / module-cycle / def-use / template-pattern
//     analyses. Each is a thin reader of its corresponding link-pass
//     sidecar with a handful of optional filters; per-tool footprint
//     is small but four entries push us past the 6000 ceiling. After
//     this bump further additions must fold into an existing action-
//     dispatch bundle rather than add a new top-level tool.
//   - 6500 → 7000: PR for #4421 (epic #4419 P0) — adds
//     archigraph_literal_parity, the cross-group ConstantSet / SCOPE.Enum
//     value-set parity differ (oracle vs v3-rewrite). It is a distinct
//     cross-GROUP capability (two required group params + an entity-lookup
//     auto-locate), not a filter on an existing single-group tool, so it
//     cannot fold into any current action-dispatch bundle without muddying
//     that bundle's group contract. Its three required string args
//     (group_oracle, group_v3, set) plus a tight ≤80-char description push
//     the handshake to ~6581; +500 restores headroom. After this bump the
//     fold-into-a-bundle rule still stands for any further SINGLE-group
//     additions.
const TokenCeiling = 7000
