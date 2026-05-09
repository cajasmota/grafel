package dockerfile_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/dockerfile"
	"github.com/cajasmota/archigraph/internal/types"
)

// collectRels returns all relationships across entities matching the kind.
func collectRels(entities []types.EntityRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == kind {
				out = append(out, r)
			}
		}
	}
	return out
}

// TestDockerfile_Imports_FromInstruction asserts every FROM yields an IMPORTS
// edge file→image (treating the base image as an external dependency).
func TestDockerfile_Imports_FromInstruction(t *testing.T) {
	src := `FROM golang:1.22 AS builder
RUN go build ./...

FROM ubuntu:22.04 AS runtime
EXPOSE 8080
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	imports := collectRels(entities, "IMPORTS")
	wantImages := map[string]bool{"golang:1.22": false, "ubuntu:22.04": false}
	for _, r := range imports {
		if r.FromID != "Dockerfile" {
			t.Errorf("IMPORTS FromID: want Dockerfile, got %q", r.FromID)
		}
		if _, ok := wantImages[r.ToID]; ok {
			wantImages[r.ToID] = true
		}
		if r.Properties["language"] != "dockerfile" {
			t.Errorf("IMPORTS missing language=dockerfile, got %q", r.Properties["language"])
		}
		if r.Properties["import_kind"] != "from" {
			t.Errorf("IMPORTS import_kind: want 'from', got %q", r.Properties["import_kind"])
		}
	}
	for img, seen := range wantImages {
		if !seen {
			t.Errorf("missing IMPORTS for image %q. got %+v", img, imports)
		}
	}
}

// TestDockerfile_Contains_Stages asserts a file-level SCOPE.Component is
// emitted carrying one CONTAINS edge per FROM stage.
func TestDockerfile_Contains_Stages(t *testing.T) {
	src := `FROM golang:1.22 AS builder
RUN go build ./...

FROM ubuntu:22.04 AS runtime
EXPOSE 8080
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	contains := collectRels(entities, "CONTAINS")
	if len(contains) != 2 {
		t.Fatalf("expected 2 CONTAINS edges, got %d: %+v", len(contains), contains)
	}

	wantToIDs := map[string]bool{
		extractor.BuildOperationStructuralRef("dockerfile", "Dockerfile", "golang:1.22"):  false,
		extractor.BuildOperationStructuralRef("dockerfile", "Dockerfile", "ubuntu:22.04"): false,
	}
	for _, r := range contains {
		if _, ok := wantToIDs[r.ToID]; ok {
			wantToIDs[r.ToID] = true
		}
	}
	for to, seen := range wantToIDs {
		if !seen {
			t.Errorf("missing CONTAINS to %q. got %+v", to, contains)
		}
	}
}

// TestDockerfile_Contains_NoStagesNoComponent asserts no file-level component
// is emitted when there are no FROM instructions (degenerate input).
func TestDockerfile_Contains_NoStagesNoComponent(t *testing.T) {
	src := `# only a comment
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == "CONTAINS" {
				t.Errorf("unexpected CONTAINS edge: %+v", r)
			}
		}
	}
}

// TestDockerfile_Uses_CopyFromStage asserts COPY --from=<stage> emits a USES
// edge to the referenced stage structural-ref.
func TestDockerfile_Uses_CopyFromStage(t *testing.T) {
	src := `FROM golang:1.22 AS builder
RUN go build -o /app/bin ./...

FROM ubuntu:22.04 AS runtime
COPY --from=builder /app/bin /usr/local/bin
`
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	uses := collectRels(entities, "USES")
	if len(uses) != 1 {
		t.Fatalf("expected 1 USES edge, got %d: %+v", len(uses), uses)
	}
	wantTo := extractor.BuildOperationStructuralRef("dockerfile", "Dockerfile", "golang:1.22")
	if uses[0].ToID != wantTo {
		t.Errorf("USES ToID: want %q, got %q", wantTo, uses[0].ToID)
	}
	if uses[0].Properties["language"] != "dockerfile" {
		t.Errorf("USES missing language=dockerfile, got %q", uses[0].Properties["language"])
	}
	if uses[0].Properties["from_stage"] != "builder" {
		t.Errorf("USES from_stage: want 'builder', got %q", uses[0].Properties["from_stage"])
	}
}

// TestDockerfile_Imports_SingleStageNoAlias asserts FROM without AS still
// emits an IMPORTS edge.
func TestDockerfile_Imports_SingleStageNoAlias(t *testing.T) {
	src := "FROM alpine:3.18\n"
	tree := parseForTest(t, src)
	entities := extractEntities(t, "Dockerfile", src, tree)

	imports := collectRels(entities, "IMPORTS")
	if len(imports) != 1 {
		t.Fatalf("expected 1 IMPORTS edge, got %d: %+v", len(imports), imports)
	}
	if imports[0].ToID != "alpine:3.18" {
		t.Errorf("IMPORTS ToID: want alpine:3.18, got %q", imports[0].ToID)
	}
}
