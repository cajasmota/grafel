package graph

import (
	"encoding/json"
	"os"
	"testing"
)

// TestGraphStatsSidecar_ExtractMSRoundTrip verifies that the in-band extract_ms
// phase timing added for #5692 survives a JSON encode → decode round-trip
// alongside the existing algo runtime_ms.
func TestGraphStatsSidecar_ExtractMSRoundTrip(t *testing.T) {
	in := &GraphStatsSidecar{
		Version:            1,
		TotalEntities:      42,
		TotalRelationships: 7,
		RuntimeMS:          123,
		ExtractMS:          456,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out GraphStatsSidecar
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ExtractMS != in.ExtractMS {
		t.Errorf("extract_ms round-trip: got %d, want %d", out.ExtractMS, in.ExtractMS)
	}
	if out.RuntimeMS != in.RuntimeMS {
		t.Errorf("runtime_ms must be unchanged (back-compat): got %d, want %d", out.RuntimeMS, in.RuntimeMS)
	}
	// extract_ms must serialise under its documented snake_case key; link_ms
	// must NOT appear in graph-stats.json (it lives in link-stats.json).
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["extract_ms"]; !ok {
		t.Errorf("expected extract_ms key in JSON, got: %s", b)
	}
	if _, ok := raw["link_ms"]; ok {
		t.Errorf("link_ms must NOT be in graph-stats.json (owned by link-stats.json), got: %s", b)
	}
}

// TestGraphStatsSidecar_BackCompatOldSidecar verifies that a sidecar written
// before #5692 (no extract_ms key) still loads, with the field defaulting to
// the zero value (= unknown).
func TestGraphStatsSidecar_BackCompatOldSidecar(t *testing.T) {
	old := `{"version":1,"total_entities":10,"total_relationships":3,"runtime_ms":50}`
	var side GraphStatsSidecar
	if err := json.Unmarshal([]byte(old), &side); err != nil {
		t.Fatalf("legacy sidecar must still load: %v", err)
	}
	if side.TotalEntities != 10 || side.RuntimeMS != 50 {
		t.Fatalf("legacy fields lost: %+v", side)
	}
	if side.ExtractMS != 0 {
		t.Errorf("missing extract_ms must default to 0 (unknown), got %d", side.ExtractMS)
	}
}

// TestLinkStatsSidecar_RoundTrip verifies the dedicated link-stats.json sidecar
// round-trips link_ms through WriteLinkStats → LoadLinkStats, and that a
// missing file surfaces as os.IsNotExist so callers can treat it as "unknown".
func TestLinkStatsSidecar_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Absent file → IsNotExist (callers treat as link timing unknown).
	if _, err := LoadLinkStats(dir); !os.IsNotExist(err) {
		t.Fatalf("absent link-stats.json must be os.IsNotExist, got: %v", err)
	}

	if err := WriteLinkStats(dir, &LinkStatsSidecar{Version: 1, LinkMS: 321}); err != nil {
		t.Fatalf("write link-stats: %v", err)
	}
	got, err := LoadLinkStats(dir)
	if err != nil {
		t.Fatalf("load link-stats: %v", err)
	}
	if got.LinkMS != 321 {
		t.Errorf("link_ms round-trip: got %d, want 321", got.LinkMS)
	}
}

// TestLinkStatsWrite_DoesNotClobberGraphStats is the regression guard for the
// lost-update defect that split link timing into its own file (#5692). The link
// pass (WriteLinkStats) and the reindex writer (WriteSidecar) must own separate
// files, so a link write can never revert graph-stats.json's count/extract
// fields even when it runs concurrently with a reindex.
func TestLinkStatsWrite_DoesNotClobberGraphStats(t *testing.T) {
	dir := t.TempDir()
	outPath := dir + "/graph.json" // WriteSidecar derives the dir from this

	// Reindex writes fresh counts + extract_ms into graph-stats.json.
	orig := &GraphStatsSidecar{
		Version:            1,
		TotalEntities:      500,
		TotalRelationships: 900,
		ExtractMS:          1234,
		RuntimeMS:          77,
	}
	if err := WriteSidecar(outPath, orig, false); err != nil {
		t.Fatalf("write graph-stats: %v", err)
	}

	// Link pass records its duration — into link-stats.json only.
	if err := WriteLinkStats(dir, &LinkStatsSidecar{Version: 1, LinkMS: 999}); err != nil {
		t.Fatalf("write link-stats: %v", err)
	}

	// graph-stats.json counts + extract_ms must be untouched by the link write.
	after, err := LoadSidecar(dir)
	if err != nil {
		t.Fatalf("reload graph-stats: %v", err)
	}
	if after.TotalEntities != 500 || after.TotalRelationships != 900 {
		t.Errorf("link write clobbered counts: %+v", after)
	}
	if after.ExtractMS != 1234 || after.RuntimeMS != 77 {
		t.Errorf("link write clobbered timing fields: %+v", after)
	}

	// link_ms is readable from its own file, and graph-stats.json never grew a
	// link_ms key.
	ls, err := LoadLinkStats(dir)
	if err != nil {
		t.Fatalf("load link-stats: %v", err)
	}
	if ls.LinkMS != 999 {
		t.Errorf("link_ms: got %d, want 999", ls.LinkMS)
	}
	raw, _ := os.ReadFile(SidecarPath(dir))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if _, ok := m["link_ms"]; ok {
		t.Errorf("graph-stats.json must not carry link_ms: %s", raw)
	}
}
