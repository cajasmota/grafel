package main

// mcpBridgeCmd wires the internal cli.newMCPBridgeCmd into the root command.
// The command is hidden from help output; it is invoked automatically by
// Claude Code via the mcpServers entry written by 'archigraph install'.
//
// See internal/cli/mcp_bridge.go for the implementation.
