package mcp_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// TestMCPHandshakeBudget verifies that the total MCP handshake size stays
// within the token ceiling defined by the spec (issue #1333).
//
// Budget:  3,500 tokens (ceiling)
// Method:  conservative 4-chars-per-token estimate, matching cmd/mcp-audit.
//
// When this test fails, you have added a tool or description that exceeds the
// budget. Options:
//   - Shorten tool descriptions to fit within maxDescLen (80 chars).
//   - Remove a tool or fold it into an existing action-dispatch bundle.
//   - Request a budget increase by updating defaultCeiling AND the SCHEMA.md
//     handshake section with a justification comment.
func TestMCPHandshakeBudget(t *testing.T) {
	const (
		// tokenCeiling matches cmd/mcp-audit defaultCeiling.
		// Baseline: 2,963 tokens (28 tools, measured 2026-05-21 post refactor/mcp-real-3k).
		// 2026-05-23 (#1384, epic #1380): ceiling bumped to 3,100 to seat the new
		// archigraph_module_analysis tool (module-level SCC/PageRank/betweenness).
		// Current measurement at 29 tools: 3,085 tokens. See cmd/mcp-audit/main.go.
		// 2026-05-23 (#1659): ceiling bumped to 3,200 to seat archigraph_apply_docgen_repairs
		// (docgen→graph repair feedback loop). 30 tools, measured 3,176 tokens.
		tokenCeiling  = 3200
		charsPerToken = 4
		envelopeBytes = 512 // initEnvelopeBytes constant from cmd/mcp-audit
		maxDescLen    = 80
	)

	tmp, err := os.CreateTemp(t.TempDir(), "registry-*.json")
	if err != nil {
		t.Fatalf("create temp registry: %v", err)
	}
	if _, err := tmp.WriteString(`{"groups":{}}`); err != nil {
		t.Fatalf("write temp registry: %v", err)
	}
	tmp.Close()

	srv, err := mcp.NewServer(mcp.Config{RegistryPath: tmp.Name()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	byName := srv.MCP.ListTools()
	if len(byName) == 0 {
		t.Fatal("no tools registered — server may not have initialised correctly")
	}

	totalChars := envelopeBytes
	var descViolations []string

	for name, st := range byName {
		b, err := json.Marshal(st.Tool)
		if err != nil {
			t.Fatalf("marshal tool %s: %v", name, err)
		}
		totalChars += len(b)

		if dl := len(st.Tool.Description); dl > maxDescLen {
			descViolations = append(descViolations,
				fmt.Sprintf("%s: description %d chars (limit %d)", name, dl, maxDescLen))
		}
	}

	handshakeTokens := int(math.Ceil(float64(totalChars) / charsPerToken))

	t.Logf("tools=%d  chars=%d  tokens=%d  ceiling=%d",
		len(byName), totalChars, handshakeTokens, tokenCeiling)

	for _, v := range descViolations {
		t.Errorf("description too long: %s", v)
	}

	if handshakeTokens > tokenCeiling {
		t.Errorf("handshake %d tokens exceeds ceiling %d (delta +%d)",
			handshakeTokens, tokenCeiling, handshakeTokens-tokenCeiling)
	}
}

// validateToolDescription returns an error string when the tool description
// is empty or exceeds maxDescLen.  Called from TestMCPToolDescriptions.
func validateToolDescription(t mcpapi.Tool, maxLen int) string {
	if t.Description == "" {
		return t.Name + ": description is empty"
	}
	if len(t.Description) > maxLen {
		return fmt.Sprintf("%s: description %d chars (limit %d)",
			t.Name, len(t.Description), maxLen)
	}
	return ""
}

// TestMCPToolDescriptions checks that every registered tool has a non-empty
// description that fits within maxDescLen characters.
func TestMCPToolDescriptions(t *testing.T) {
	const maxDescLen = 80

	tmp, err := os.CreateTemp(t.TempDir(), "registry-*.json")
	if err != nil {
		t.Fatalf("create temp registry: %v", err)
	}
	if _, err := tmp.WriteString(`{"groups":{}}`); err != nil {
		t.Fatalf("write temp registry: %v", err)
	}
	tmp.Close()

	srv, err := mcp.NewServer(mcp.Config{RegistryPath: tmp.Name()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	byName := srv.MCP.ListTools()
	for name, st := range byName {
		if msg := validateToolDescription(st.Tool, maxDescLen); msg != "" {
			t.Errorf("%s", msg)
		}
		_ = name
	}
}
