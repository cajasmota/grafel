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
//     headroom for a 48th tool entry without a bump. The +500 keeps
//     us under the 6000 hard cap with comfortable room for Phase 2B.
const TokenCeiling = 5500
