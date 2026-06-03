package hcl

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractResource runs the real HCL extractor and returns the resource entity
// whose Name matches selfRef.
func extractResource(t *testing.T, src, selfRef string) types.EntityRecord {
	t.Helper()
	e := &HCLExtractor{lang: "terraform"}
	recs, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "main.tf",
		Language: "terraform",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	for _, r := range recs {
		if r.Name == selfRef && r.Subtype == "resource" {
			return r
		}
	}
	t.Fatalf("resource %q not found in %d records", selfRef, len(recs))
	return types.EntityRecord{}
}

// TestStampScalarProps_AWSInstance asserts the EXACT curated scalar values are
// stamped on an aws_instance: string instance_type and numeric count.
func TestStampScalarProps_AWSInstance(t *testing.T) {
	src := `resource "aws_instance" "web" {
  instance_type = "t3.micro"
  count         = 3
  monitoring    = true
}`
	rec := extractResource(t, src, "aws_instance.web")
	if got := rec.Properties["instance_type"]; got != "t3.micro" {
		t.Errorf("instance_type = %q, want %q", got, "t3.micro")
	}
	if got := rec.Properties["count"]; got != "3" {
		t.Errorf("count = %q, want %q", got, "3")
	}
	// "monitoring" is a bool literal but NOT in the curated allow-list — must
	// not be stamped (bounded scope).
	if _, ok := rec.Properties["monitoring"]; ok {
		t.Errorf("monitoring should NOT be stamped (not in allow-list); got %q", rec.Properties["monitoring"])
	}
}

// TestStampScalarProps_LambdaScalars asserts string + numeric curated props on
// a lambda function resource.
func TestStampScalarProps_LambdaScalars(t *testing.T) {
	src := `resource "aws_lambda_function" "api" {
  runtime     = "python3.12"
  memory_size = 512
  timeout     = 30
}`
	rec := extractResource(t, src, "aws_lambda_function.api")
	for k, want := range map[string]string{
		"runtime":     "python3.12",
		"memory_size": "512",
		"timeout":     "30",
	} {
		if got := rec.Properties[k]; got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// TestStampScalarProps_ReferenceNotStamped is the negative/boundary case: an
// attribute whose value is a traversal REFERENCE (a curated key, but a
// reference value) must NOT be stamped as a scalar — it stays an edge.
func TestStampScalarProps_ReferenceNotStamped(t *testing.T) {
	src := `resource "aws_db_instance" "db" {
  engine            = "postgres"
  instance_class    = aws_instance.web.type
  allocated_storage = var.storage_gb
}`
	rec := extractResource(t, src, "aws_db_instance.db")
	// Plain string scalar IS stamped.
	if got := rec.Properties["engine"]; got != "postgres" {
		t.Errorf("engine = %q, want %q", got, "postgres")
	}
	// Reference value (aws_instance.web.type) must NOT be stamped.
	if v, ok := rec.Properties["instance_class"]; ok {
		t.Errorf("instance_class is a reference and must not be stamped; got %q", v)
	}
	// var.* reference must NOT be stamped.
	if v, ok := rec.Properties["allocated_storage"]; ok {
		t.Errorf("allocated_storage is a var reference and must not be stamped; got %q", v)
	}
}

// TestStampScalarProps_InterpolatedStringNotStamped: a curated key whose string
// value contains ${...} interpolation is reference-bearing → not a scalar.
func TestStampScalarProps_InterpolatedStringNotStamped(t *testing.T) {
	src := `resource "aws_instance" "web" {
  instance_type = "${var.size}.micro"
}`
	rec := extractResource(t, src, "aws_instance.web")
	if v, ok := rec.Properties["instance_type"]; ok {
		t.Errorf("interpolated instance_type must not be stamped; got %q", v)
	}
}

// TestStampScalarProps_CollectionNotStamped: a curated key whose value is a
// collection (object/list) is not a scalar → not stamped.
func TestStampScalarProps_CollectionNotStamped(t *testing.T) {
	// "size" is a curated key; here it is a list, not a scalar.
	src := `resource "aws_autoscaling_group" "asg" {
  min_size = 1
  size     = [1, 2, 3]
}`
	rec := extractResource(t, src, "aws_autoscaling_group.asg")
	if got := rec.Properties["min_size"]; got != "1" {
		t.Errorf("min_size = %q, want %q", got, "1")
	}
	if v, ok := rec.Properties["size"]; ok {
		t.Errorf("list-valued size must not be stamped; got %q", v)
	}
}

// TestStampScalarProps_NoRegressionEdges confirms scalar stamping does NOT
// suppress the existing reference-edge mining (DEPENDS_ON still emitted).
func TestStampScalarProps_NoRegressionEdges(t *testing.T) {
	src := `resource "aws_instance" "web" {
  instance_type = "t3.micro"
  depends_on    = [aws_iam_role.lambda_role]
}`
	rec := extractResource(t, src, "aws_instance.web")
	if got := rec.Properties["instance_type"]; got != "t3.micro" {
		t.Errorf("instance_type = %q, want t3.micro", got)
	}
	found := false
	for _, rel := range rec.Relationships {
		if rel.Kind == "DEPENDS_ON" {
			found = true
		}
	}
	if !found {
		t.Errorf("DEPENDS_ON edge regressed: expected a depends_on edge alongside scalar stamping")
	}
}
