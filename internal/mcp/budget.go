package mcp

// TokenCeiling is the maximum allowed token count for the MCP tool
// handshake response. Enforced by cmd/mcp-audit and asserted by
// budget_test.go. Bumped 2026-05-27 from 3500 → 4200 after docgen
// tools landed in #2207. Bumps require coordinating both sites.
const TokenCeiling = 4200
