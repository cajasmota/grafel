package mcp

// TokenCeiling is the maximum allowed token count for the MCP tool
// handshake response. Enforced by cmd/mcp-audit and asserted by
// budget_test.go. Bumped from 4200 → 5000 to accommodate orphan-handler
// re-wires in PR #2442 (archigraph_cross_links, archigraph_save_finding,
// archigraph_list_findings, archigraph_license_audit). Previous 4200 ceiling
// overflowed at 4476 tokens; 5000 gives ~500-token headroom for future tools.
const TokenCeiling = 5000
