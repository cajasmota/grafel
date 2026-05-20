package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterWriteAgentsMD(t *testing.T) {
	tmpDir := t.TempDir()
	agentsMDPath := filepath.Join(tmpDir, "AGENTS.md")

	tests := []struct {
		name           string
		existingContent string
		group          string
		wantContains   []string
		wantNotContains string
	}{
		{
			name:  "create new file",
			group: "test-group",
			wantContains: []string{
				agentsMDStartMarker,
				"archigraph",
				"test-group",
				"MCP",
				agentsMDEndMarker,
			},
		},
		{
			name:            "append to existing file",
			existingContent: "# My Project\n\nSome docs here.\n",
			group:           "my-group",
			wantContains: []string{
				"# My Project",
				"Some docs here",
				"my-group",
			},
		},
		{
			name:            "replace existing block",
			existingContent: "# Docs\n\n" + agentsMDStartMarker + "\nold block\n" + agentsMDEndMarker + "\n\nMore docs.",
			group:           "new-group",
			wantContains: []string{
				"# Docs",
				"More docs",
				"new-group",
			},
			wantNotContains: "old block",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up from previous test
			os.Remove(agentsMDPath)

			if tt.existingContent != "" {
				if err := os.WriteFile(agentsMDPath, []byte(tt.existingContent), 0o644); err != nil {
					t.Fatalf("setup: write existing file: %v", err)
				}
			}

			stub := renderAgentsMDStub(tt.group)
			if err := upsertAgentsMDFile(agentsMDPath, stub); err != nil {
				t.Fatalf("upsertAgentsMDFile: %v", err)
			}

			content, err := os.ReadFile(agentsMDPath)
			if err != nil {
				t.Fatalf("read result: %v", err)
			}
			result := string(content)

			// Check for expected content
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("missing expected content: %q\nfull output:\n%s", want, result)
				}
			}

			// Check for unwanted content
			if tt.wantNotContains != "" && strings.Contains(result, tt.wantNotContains) {
				t.Errorf("found unwanted content: %q\nfull output:\n%s", tt.wantNotContains, result)
			}
		})
	}
}

func TestRegisterIDempotence(t *testing.T) {
	tmpDir := t.TempDir()
	agentsMDPath := filepath.Join(tmpDir, "AGENTS.md")

	// Write once
	stub1 := renderAgentsMDStub("group-a")
	if err := upsertAgentsMDFile(agentsMDPath, stub1); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first, _ := os.ReadFile(agentsMDPath)

	// Write again with different group
	stub2 := renderAgentsMDStub("group-b")
	if err := upsertAgentsMDFile(agentsMDPath, stub2); err != nil {
		t.Fatalf("second write: %v", err)
	}
	second, _ := os.ReadFile(agentsMDPath)

	// The file should contain group-b, not group-a (replaced)
	if strings.Contains(string(second), "group-a") {
		t.Error("old group name still present after idempotent update")
	}
	if !strings.Contains(string(second), "group-b") {
		t.Error("new group name not found after idempotent update")
	}

	// File size should be roughly the same (not concatenating)
	if len(second) > len(first)*2 {
		t.Errorf("file grew unexpectedly on second write: %d -> %d bytes", len(first), len(second))
	}
}

func TestRegisterStubContent(t *testing.T) {
	stub := renderAgentsMDStub("test-group")

	// Verify markers are present
	if !strings.Contains(stub, agentsMDStartMarker) {
		t.Error("missing start marker")
	}
	if !strings.Contains(stub, agentsMDEndMarker) {
		t.Error("missing end marker")
	}

	// Verify key phrases are present
	keywords := []string{
		"archigraph",
		"test-group",
		"MCP",
		"group",
	}
	for _, kw := range keywords {
		if !strings.Contains(stub, kw) {
			t.Errorf("missing keyword: %q in stub:\n%s", kw, stub)
		}
	}
}
