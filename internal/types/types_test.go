package types

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- EntityRecord.Validate() ---

func TestEntityRecord_Validate_RejectsEmptyKind(t *testing.T) {
	e := EntityRecord{
		Name:       "MyFunc",
		SourceFile: "main.go",
	}
	err := e.Validate()
	if err == nil {
		t.Fatal("expected error for empty Kind, got nil")
	}
	if !strings.Contains(err.Error(), "kind is required") {
		t.Errorf("expected 'kind is required' in error, got: %s", err.Error())
	}
}

func TestEntityRecord_Validate_RejectsEmptyName(t *testing.T) {
	e := EntityRecord{
		Kind:       "Operation",
		SourceFile: "main.go",
	}
	err := e.Validate()
	if err == nil {
		t.Fatal("expected error for empty Name, got nil")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected 'name is required' in error, got: %s", err.Error())
	}
}

func TestEntityRecord_Validate_RejectsEmptySourceFile(t *testing.T) {
	e := EntityRecord{
		Kind: "Operation",
		Name: "MyFunc",
	}
	err := e.Validate()
	if err == nil {
		t.Fatal("expected error for empty SourceFile, got nil")
	}
	if !strings.Contains(err.Error(), "source_file is required") {
		t.Errorf("expected 'source_file is required' in error, got: %s", err.Error())
	}
}

func TestEntityRecord_Validate_RejectsAllMissing(t *testing.T) {
	e := EntityRecord{}
	err := e.Validate()
	if err == nil {
		t.Fatal("expected error for all-empty entity, got nil")
	}
	// All three violations must appear in a single error.
	msg := err.Error()
	for _, want := range []string{"kind is required", "name is required", "source_file is required"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected %q in error message, got: %s", want, msg)
		}
	}
}

func TestEntityRecord_Validate_RejectsQualityScoreAboveOne(t *testing.T) {
	e := EntityRecord{
		Kind:         "Operation",
		Name:         "MyFunc",
		SourceFile:   "main.go",
		QualityScore: 1.5,
	}
	err := e.Validate()
	if err == nil {
		t.Fatal("expected error for QualityScore > 1.0, got nil")
	}
	if !strings.Contains(err.Error(), "quality_score") {
		t.Errorf("expected 'quality_score' in error, got: %s", err.Error())
	}
}

func TestEntityRecord_Validate_RejectsQualityScoreBelow0(t *testing.T) {
	e := EntityRecord{
		Kind:         "Operation",
		Name:         "MyFunc",
		SourceFile:   "main.go",
		QualityScore: -0.1,
	}
	err := e.Validate()
	if err == nil {
		t.Fatal("expected error for QualityScore < 0.0, got nil")
	}
}

func TestEntityRecord_Validate_AcceptsValidEntity(t *testing.T) {
	e := EntityRecord{
		Kind:         "Operation",
		Name:         "MyFunc",
		SourceFile:   "main.go",
		QualityScore: 0.8,
	}
	if err := e.Validate(); err != nil {
		t.Errorf("expected no error for valid entity, got: %s", err.Error())
	}
}

func TestEntityRecord_Validate_AcceptsZeroQualityScore(t *testing.T) {
	e := EntityRecord{
		Kind:       "Operation",
		Name:       "MyFunc",
		SourceFile: "main.go",
		// QualityScore defaults to 0.0 — valid lower bound (degraded entities)
	}
	if err := e.Validate(); err != nil {
		t.Errorf("expected no error for QualityScore=0.0, got: %s", err.Error())
	}
}

func TestEntityRecord_Validate_AcceptsOneQualityScore(t *testing.T) {
	e := EntityRecord{
		Kind:         "Operation",
		Name:         "MyFunc",
		SourceFile:   "main.go",
		QualityScore: 1.0,
	}
	if err := e.Validate(); err != nil {
		t.Errorf("expected no error for QualityScore=1.0, got: %s", err.Error())
	}
}

// --- EntityRecord.ComputeID() ---

func TestEntityRecord_ComputeID_IsDeterministic(t *testing.T) {
	e := EntityRecord{
		OrgID:      "org-abc",
		ProjectID:  "proj-123",
		SourceFile: "pkg/server.go",
		Kind:       "Service",
		Name:       "UserService",
	}
	id1 := e.ComputeID()
	id2 := e.ComputeID()
	if id1 != id2 {
		t.Errorf("ComputeID() is not deterministic: got %q then %q", id1, id2)
	}
}

func TestEntityRecord_ComputeID_Is16HexChars(t *testing.T) {
	e := EntityRecord{
		OrgID:      "org-abc",
		ProjectID:  "proj-123",
		SourceFile: "pkg/server.go",
		Kind:       "Service",
		Name:       "UserService",
	}
	id := e.ComputeID()
	if len(id) != 16 {
		t.Errorf("ComputeID() must return 16 chars, got %d: %q", len(id), id)
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("ComputeID() must return lowercase hex, got char %q in %q", c, id)
		}
	}
}

func TestEntityRecord_ComputeID_DiffersWhenQualifiedNameDiffers(t *testing.T) {
	base := EntityRecord{
		OrgID:      "org-abc",
		ProjectID:  "proj-123",
		SourceFile: "pkg/server.go",
		Kind:       "Service",
		Name:       "UserService",
	}
	other := base
	other.Name = "OrderService"

	if base.ComputeID() == other.ComputeID() {
		t.Error("ComputeID() must differ when Name differs")
	}
}

func TestEntityRecord_ComputeID_DiffersWhenSourceFileDiffers(t *testing.T) {
	base := EntityRecord{
		OrgID:      "org-abc",
		ProjectID:  "proj-123",
		SourceFile: "pkg/server.go",
		Kind:       "Service",
		Name:       "UserService",
	}
	other := base
	other.SourceFile = "pkg/client.go"

	if base.ComputeID() == other.ComputeID() {
		t.Error("ComputeID() must differ when SourceFile differs")
	}
}

func TestEntityRecord_ComputeID_DiffersWhenOrgIDDiffers(t *testing.T) {
	base := EntityRecord{
		OrgID:      "org-abc",
		ProjectID:  "proj-123",
		SourceFile: "pkg/server.go",
		Kind:       "Service",
		Name:       "UserService",
	}
	other := base
	other.OrgID = "org-xyz"

	if base.ComputeID() == other.ComputeID() {
		t.Error("ComputeID() must differ when OrgID differs")
	}
}

// --- RelationshipRecord.Validate() ---

func TestRelationshipRecord_Validate_RejectsEmptyFromID(t *testing.T) {
	r := RelationshipRecord{ToID: "b", Kind: "CALLS"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for empty FromID")
	} else if !strings.Contains(err.Error(), "from_id is required") {
		t.Errorf("expected 'from_id is required', got: %s", err.Error())
	}
}

func TestRelationshipRecord_Validate_RejectsEmptyToID(t *testing.T) {
	r := RelationshipRecord{FromID: "a", Kind: "CALLS"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for empty ToID")
	} else if !strings.Contains(err.Error(), "to_id is required") {
		t.Errorf("expected 'to_id is required', got: %s", err.Error())
	}
}

func TestRelationshipRecord_Validate_RejectsEmptyKind(t *testing.T) {
	r := RelationshipRecord{FromID: "a", ToID: "b"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for empty Kind")
	} else if !strings.Contains(err.Error(), "kind is required") {
		t.Errorf("expected 'kind is required', got: %s", err.Error())
	}
}

func TestRelationshipRecord_Validate_AcceptsValid(t *testing.T) {
	r := RelationshipRecord{FromID: "a", ToID: "b", Kind: "CALLS"}
	if err := r.Validate(); err != nil {
		t.Errorf("expected no error, got: %s", err.Error())
	}
}

// --- Relationship.Validate() ---

func TestRelationship_Validate_RejectsEmptySourceID(t *testing.T) {
	r := Relationship{TargetID: "b", Type: "CALLS"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for empty SourceID")
	} else if !strings.Contains(err.Error(), "source_id is required") {
		t.Errorf("expected 'source_id is required', got: %s", err.Error())
	}
}

func TestRelationship_Validate_RejectsEmptyTargetID(t *testing.T) {
	r := Relationship{SourceID: "a", Type: "CALLS"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for empty TargetID")
	} else if !strings.Contains(err.Error(), "target_id is required") {
		t.Errorf("expected 'target_id is required', got: %s", err.Error())
	}
}

func TestRelationship_Validate_RejectsEmptyType(t *testing.T) {
	r := Relationship{SourceID: "a", TargetID: "b"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for empty Type")
	} else if !strings.Contains(err.Error(), "type is required") {
		t.Errorf("expected 'type is required', got: %s", err.Error())
	}
}

func TestRelationship_Validate_AcceptsValid(t *testing.T) {
	r := Relationship{SourceID: "a", TargetID: "b", Type: "IMPORTS"}
	if err := r.Validate(); err != nil {
		t.Errorf("expected no error, got: %s", err.Error())
	}
}

// --- FileBatch JSON round-trip ---

func TestFileBatch_JSONRoundTrip(t *testing.T) {
	original := FileBatch{
		JobID:       "job-001",
		OrgID:       "org-abc",
		ProjectID:   "proj-123",
		ProjectSlug: "my-service",
		BatchIndex:  2,
		TotalFiles:  40,
		TotalBytes:  102400,
		Files: []FileEntry{
			{Path: "pkg/server.go", Language: "go", SizeBytes: 4096, S3Key: "jobs/job-001/batch-2/server.go"},
			{Path: "pkg/client.go", Language: "go", SizeBytes: 2048, S3Key: "jobs/job-001/batch-2/client.go"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded FileBatch
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.JobID != original.JobID {
		t.Errorf("JobID mismatch: got %q, want %q", decoded.JobID, original.JobID)
	}
	if decoded.OrgID != original.OrgID {
		t.Errorf("OrgID mismatch: got %q, want %q", decoded.OrgID, original.OrgID)
	}
	if decoded.BatchIndex != original.BatchIndex {
		t.Errorf("BatchIndex mismatch: got %d, want %d", decoded.BatchIndex, original.BatchIndex)
	}
	if decoded.TotalBytes != original.TotalBytes {
		t.Errorf("TotalBytes mismatch: got %d, want %d", decoded.TotalBytes, original.TotalBytes)
	}
	if len(decoded.Files) != len(original.Files) {
		t.Fatalf("Files length mismatch: got %d, want %d", len(decoded.Files), len(original.Files))
	}
	for i, f := range decoded.Files {
		if f.Path != original.Files[i].Path {
			t.Errorf("Files[%d].Path mismatch: got %q, want %q", i, f.Path, original.Files[i].Path)
		}
		if f.S3Key != original.Files[i].S3Key {
			t.Errorf("Files[%d].S3Key mismatch: got %q, want %q", i, f.S3Key, original.Files[i].S3Key)
		}
	}
}

// --- FileEntry with zero SizeBytes is valid ---

func TestFileEntry_ZeroSizeBytesIsValid(t *testing.T) {
	entry := FileEntry{
		Path:      "pkg/empty.go",
		Language:  "go",
		SizeBytes: 0,
		S3Key:     "jobs/job-001/batch-1/empty.go",
	}
	// FileEntry has no Validate() — verify it round-trips correctly with zero size.
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var decoded FileEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if decoded.SizeBytes != 0 {
		t.Errorf("expected SizeBytes=0 after round-trip, got %d", decoded.SizeBytes)
	}
}

// --- EnrichmentStatus constants ---

func TestEnrichmentStatusConstants(t *testing.T) {
	if StatusPending != "pending" {
		t.Errorf("StatusPending = %q, want %q", StatusPending, "pending")
	}
	if StatusEnriched != "enriched" {
		t.Errorf("StatusEnriched = %q, want %q", StatusEnriched, "enriched")
	}
	if StatusDegraded != "degraded" {
		t.Errorf("StatusDegraded = %q, want %q", StatusDegraded, "degraded")
	}
}
