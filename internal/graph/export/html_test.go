package export

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteHTML_ParseableAndSelfContained(t *testing.T) {
	var sb strings.Builder
	if err := WriteHTML(&sb, sampleDoc(), 0); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	out := sb.String()

	// Structural sanity: a single well-formed document shell.
	for _, want := range []string{"<html", "</html>", "<head>", "</head>", "<body>", "</body>"} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	if !strings.HasPrefix(out, "<!DOCTYPE html>") {
		t.Errorf("missing doctype")
	}
	// Self-contained: no external resource loads. (The SVG XML namespace URI
	// http://www.w3.org/2000/svg is a constant identifier, not a fetch, so we
	// only reject actual loading constructs.)
	for _, bad := range []string{"<script src=", "<link ", "url(http", "@import", "src=\"http"} {
		if strings.Contains(out, bad) {
			t.Errorf("HTML is not self-contained: contains %q", bad)
		}
	}
	// Inline SVG embedded (prolog stripped).
	if !strings.Contains(out, "<svg") {
		t.Errorf("missing inline svg")
	}
	if strings.Contains(out, "<?xml") {
		t.Errorf("inline svg should not carry an XML prolog")
	}
	// Embedded graph JSON is present and valid.
	const open = `<script id="graph-data" type="application/json">`
	i := strings.Index(out, open)
	if i < 0 {
		t.Fatalf("missing embedded graph-data script")
	}
	rest := out[i+len(open):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		t.Fatalf("unterminated graph-data script")
	}
	var payload struct {
		Repo  string           `json:"repo"`
		Nodes []map[string]any `json:"nodes"`
		Edges []map[string]any `json:"edges"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(rest[:j])), &payload); err != nil {
		t.Fatalf("embedded JSON invalid: %v", err)
	}
	if len(payload.Nodes) != 2 {
		t.Errorf("want 2 embedded nodes, got %d", len(payload.Nodes))
	}
	if len(payload.Edges) != 1 {
		t.Errorf("want 1 embedded edge, got %d", len(payload.Edges))
	}
}

func TestWriteHTML_Deterministic(t *testing.T) {
	var a, b strings.Builder
	if err := WriteHTML(&a, sampleDoc(), 0); err != nil {
		t.Fatal(err)
	}
	if err := WriteHTML(&b, sampleDoc(), 0); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Errorf("HTML output not deterministic")
	}
}

func TestWriteHTML_TopNCap(t *testing.T) {
	var sb strings.Builder
	if err := WriteHTML(&sb, bigDoc(100), 10); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	out := sb.String()
	const open = `<script id="graph-data" type="application/json">`
	i := strings.Index(out, open)
	rest := out[i+len(open):]
	j := strings.Index(rest, "</script>")
	var payload struct {
		Nodes   []map[string]any `json:"nodes"`
		Dropped int              `json:"dropped"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(rest[:j])), &payload); err != nil {
		t.Fatalf("embedded JSON invalid: %v", err)
	}
	if len(payload.Nodes) != 10 {
		t.Errorf("top-N cap: want 10 embedded nodes, got %d", len(payload.Nodes))
	}
	if payload.Dropped != 90 {
		t.Errorf("want 90 dropped, got %d", payload.Dropped)
	}
}
