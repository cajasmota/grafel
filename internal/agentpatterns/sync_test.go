package agentpatterns

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func samplePatterns() []Pattern {
	return []Pattern{
		{
			ID:          "aaaa1111bbbb2222",
			Kind:        "AgentPattern",
			Category:    CategoryCode,
			Trigger:     Trigger{NaturalLanguage: "adding a chi HTTP handler"},
			Confidence:  0.72,
			Observations: 6,
			IsCandidate: false,
			AntiPatterns: []AntiPattern{
				{DoNot: "register a route in `init()`", Reason: "breaks teardown ordering"},
				{DoNot: "stash a DB handle in the request", Reason: "leaks across goroutines", Private: true},
			},
		},
		{
			ID:          "ccc3333dddd44444",
			Kind:        "AgentPattern",
			Category:    CategoryProcess,
			Trigger:     Trigger{NaturalLanguage: "shipping a feature branch"},
			Confidence:  0.85,
			Observations: 12,
			IsCandidate: false,
		},
		{
			ID:          "candidate1234567",
			Kind:        "AgentPattern",
			Category:    CategoryCode,
			Trigger:     Trigger{NaturalLanguage: "speculative match"},
			Confidence:  0.4,
			IsCandidate: true,
		},
	}
}

func TestRenderBlock_skipsCandidatesAndPrivate(t *testing.T) {
	block := RenderBlock(samplePatterns(), ExportOptions{})
	if !strings.Contains(block, "adding a chi HTTP handler") {
		t.Fatalf("approved pattern missing from block: %s", block)
	}
	if strings.Contains(block, "speculative match") {
		t.Fatalf("candidate leaked into block: %s", block)
	}
	if strings.Contains(block, "stash a DB handle") {
		t.Fatalf("private anti-pattern leaked into block: %s", block)
	}
	if !strings.Contains(block, "register a route in `init()`") {
		t.Fatalf("public anti-pattern missing from block")
	}
	if !strings.Contains(block, BlockStartMarker) || !strings.Contains(block, BlockEndMarker) {
		t.Fatalf("markers missing")
	}
}

func TestUpsertFile_createsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := UpsertFile(path, samplePatterns(), ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), BlockStartMarker) {
		t.Fatalf("expected start marker in created file")
	}
}

func TestUpsertFile_preservesUserContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const userContent = "# Project rules\n\nUser-authored intro paragraph.\n\n## Other section\n\nMore user content.\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertFile(path, samplePatterns(), ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "Project rules") || !strings.Contains(s, "User-authored intro") || !strings.Contains(s, "Other section") {
		t.Fatalf("user content lost on append:\n%s", s)
	}
	if !strings.Contains(s, BlockStartMarker) {
		t.Fatalf("block not appended")
	}
}

func TestUpsertFile_replacesExistingBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const initial = "# Project\n\nintro.\n\n" +
		BlockStartMarker + "\n\nOLD CONTENT\n\n" + BlockEndMarker + "\n\n## Tail\nuser tail.\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertFile(path, samplePatterns(), ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	s := string(out)
	if strings.Contains(s, "OLD CONTENT") {
		t.Fatalf("old block content survived replacement")
	}
	if !strings.Contains(s, "adding a chi HTTP handler") {
		t.Fatalf("new content missing")
	}
	if !strings.Contains(s, "user tail.") || !strings.Contains(s, "intro.") {
		t.Fatalf("user content outside markers was destroyed:\n%s", s)
	}
}

func TestRoundtrip_idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	patterns := samplePatterns()
	if err := UpsertFile(path, patterns, ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	// Second upsert with identical input must be byte-identical.
	if err := UpsertFile(path, patterns, ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatalf("upsert is not idempotent:\nFIRST:\n%s\n\nSECOND:\n%s", first, second)
	}
}

func TestParseBlock_extractsTriggers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := UpsertFile(path, samplePatterns(), ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	refs, err := ParseBlock(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs (skipping candidate), got %d: %+v", len(refs), refs)
	}
	got := map[string]bool{}
	for _, r := range refs {
		got[r.TriggerLine] = true
	}
	if !got["adding a chi HTTP handler"] || !got["shipping a feature branch"] {
		t.Fatalf("missing trigger: %+v", refs)
	}
}

func TestDiff_reportsBothDirections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	// Write a block with only one trigger.
	subset := samplePatterns()[:1]
	if err := UpsertFile(path, subset, ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	// Diff against fuller store: in-store-only should fire.
	report, err := Diff(path, samplePatterns(), ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.InStoreOnly) != 1 {
		t.Fatalf("expected 1 in-store-only, got %d: %+v", len(report.InStoreOnly), report)
	}
	if len(report.InBlockOnly) != 0 {
		t.Fatalf("expected 0 in-block-only, got %d", len(report.InBlockOnly))
	}
}
