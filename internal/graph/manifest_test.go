package graph

import (
	"os"
	"path/filepath"
	"testing"
)

// TestManifest_WriteReadRoundTrip: a valid manifest survives an atomic
// write + validated read unchanged, and the totals/stream helpers agree.
func TestManifest_WriteReadRoundTrip(t *testing.T) {
	genDir := filepath.Join(t.TempDir(), "graph.7")
	m := &Manifest{
		FormatVersion: ManifestFormatVersion,
		Segments: []SegmentMeta{
			{File: "seg-0000.fb", Kind: SegmentEntities, EntityCount: 2, RelCount: 1, MinKey: "a", MaxKey: "b"},
			{File: "seg-0001.fb", Kind: SegmentEntities, EntityCount: 2, RelCount: 0, MinKey: "m", MaxKey: "n"},
			{File: "seg-0002.fb", Kind: SegmentRelationships, RelCount: 3},
		},
	}
	if err := WriteManifest(genDir, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	// Atomic write leaves no .tmp behind.
	if _, err := os.Stat(filepath.Join(genDir, ManifestFileName+".tmp")); err == nil {
		t.Fatalf("leftover manifest .tmp")
	}
	got, err := ReadManifest(genDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got.FormatVersion != ManifestFormatVersion || len(got.Segments) != 3 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.TotalEntityCount() != 4 {
		t.Errorf("TotalEntityCount = %d, want 4", got.TotalEntityCount())
	}
	if got.TotalRelationshipCount() != 4 {
		t.Errorf("TotalRelationshipCount = %d, want 4", got.TotalRelationshipCount())
	}
	if n := len(got.EntitySegments()); n != 2 {
		t.Errorf("EntitySegments = %d, want 2 (seg with EntityCount>0)", n)
	}
	if n := len(got.RelationshipSegments()); n != 2 {
		t.Errorf("RelationshipSegments = %d, want 2 (seg with RelCount>0)", n)
	}
}

// TestManifest_RejectsMalformed covers the hostile/malformed inputs the read
// path must reject before any segment file is opened (test requirement vi).
func TestManifest_RejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"not-json":            `{ this is not json `,
		"zero-format-version": `{"format_version":0,"segments":[{"file":"seg-0000.fb","kind":"entity"}]}`,
		"future-version":      `{"format_version":999,"segments":[{"file":"seg-0000.fb","kind":"entity"}]}`,
		"no-segments":         `{"format_version":1,"segments":[]}`,
		"path-traversal":      `{"format_version":1,"segments":[{"file":"../../etc/passwd","kind":"entity"}]}`,
		"nested-path":         `{"format_version":1,"segments":[{"file":"sub/seg-0000.fb","kind":"entity"}]}`,
		"absolute-path":       `{"format_version":1,"segments":[{"file":"/tmp/seg-0000.fb","kind":"entity"}]}`,
		"wrong-suffix":        `{"format_version":1,"segments":[{"file":"seg-0000.json","kind":"entity"}]}`,
		"empty-file":          `{"format_version":1,"segments":[{"file":"","kind":"entity"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			genDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(genDir, ManifestFileName), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if m, err := ReadManifest(genDir); err == nil {
				t.Fatalf("ReadManifest accepted malformed %q: %+v", name, m)
			}
		})
	}
}

// TestManifest_ReadMissing: an absent manifest is an error (never a nil-but-ok).
func TestManifest_ReadMissing(t *testing.T) {
	if _, err := ReadManifest(t.TempDir()); err == nil {
		t.Fatal("ReadManifest of a dir with no manifest.json should error")
	}
}

// TestManifest_WriteRejectsInvalid: WriteManifest validates before persisting,
// so a malformed manifest never reaches disk.
func TestManifest_WriteRejectsInvalid(t *testing.T) {
	genDir := filepath.Join(t.TempDir(), "graph.1")
	bad := &Manifest{FormatVersion: ManifestFormatVersion, Segments: []SegmentMeta{{File: "../evil.fb"}}}
	if err := WriteManifest(genDir, bad); err == nil {
		t.Fatal("WriteManifest persisted an invalid segment file name")
	}
	if _, err := os.Stat(filepath.Join(genDir, ManifestFileName)); err == nil {
		t.Fatal("invalid manifest was written to disk")
	}
}
