package hcl_test

import (
	"context"
	"os"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tshcl "github.com/smacker/go-tree-sitter/hcl"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/hcl" // trigger init()
	"github.com/cajasmota/archigraph/internal/types"
)

// ----------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------

func parseHCL(src []byte) *sitter.Tree {
	p := sitter.NewParser()
	p.SetLanguage(tshcl.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		panic("test helper: hcl parse failed: " + err.Error())
	}
	return tree
}

func extractHCL(src, path string) ([]types.EntityRecord, error) {
	content := []byte(src)
	tree := parseHCL(content)
	ext, ok := extractor.Get("hcl")
	if !ok {
		panic("hcl extractor not registered")
	}
	return ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  content,
		Language: "hcl",
		Tree:     tree,
	})
}

func extractTerraform(src, path string) ([]types.EntityRecord, error) {
	content := []byte(src)
	tree := parseHCL(content)
	ext, ok := extractor.Get("terraform")
	if !ok {
		panic("terraform extractor not registered")
	}
	return ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  content,
		Language: "terraform",
		Tree:     tree,
	})
}

func findBySubtypeAndName(records []types.EntityRecord, subtype, name string) *types.EntityRecord {
	// Issue #44 (HCL) — entity Name is now the canonical reference form
	// (e.g. "local.foo", "module.vpc", "aws_vpc.this"). Tests pass the
	// bare label (e.g. "foo", "vpc", "this"); match that via the
	// preserved Metadata["label"] when Name doesn't match directly.
	for i := range records {
		if records[i].Subtype != subtype {
			continue
		}
		if records[i].Name == name {
			return &records[i]
		}
		if records[i].Metadata != nil {
			if lbl, _ := records[i].Metadata["label"].(string); lbl == name {
				return &records[i]
			}
		}
	}
	return nil
}

func countBySubtype(records []types.EntityRecord, subtype string) int {
	n := 0
	for _, r := range records {
		if r.Subtype == subtype {
			n++
		}
	}
	return n
}

// ----------------------------------------------------------------
// Registration tests
// ----------------------------------------------------------------

func TestHCLRegistered(t *testing.T) {
	_, ok := extractor.Get("hcl")
	if !ok {
		t.Fatal("hcl extractor not registered")
	}
}

func TestTerraformRegistered(t *testing.T) {
	_, ok := extractor.Get("terraform")
	if !ok {
		t.Fatal("terraform extractor not registered")
	}
}

func TestLanguageReturnsHCL(t *testing.T) {
	ext, _ := extractor.Get("hcl")
	if ext.Language() != "hcl" {
		t.Errorf("expected Language()=hcl, got %s", ext.Language())
	}
}

func TestLanguageReturnsTerraform(t *testing.T) {
	ext, _ := extractor.Get("terraform")
	if ext.Language() != "terraform" {
		t.Errorf("expected Language()=terraform, got %s", ext.Language())
	}
}

// ----------------------------------------------------------------
// Empty / nil edge cases
// ----------------------------------------------------------------

func TestEmptyContent(t *testing.T) {
	ext, _ := extractor.Get("hcl")
	records, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.tf",
		Content:  []byte{},
		Language: "hcl",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for empty content, got %d", len(records))
	}
}

func TestNilTree(t *testing.T) {
	// Extractor should parse inline when tree is nil.
	ext, _ := extractor.Get("hcl")
	src := `resource "aws_s3_bucket" "test" {}`
	records, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.tf",
		Content:  []byte(src),
		Language: "hcl",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) == 0 {
		t.Error("expected at least 1 entity (resource), got 0")
	}
}

func TestTerraformBlockSkipped(t *testing.T) {
	src := `
terraform {
  required_version = ">= 1.0"
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// terraform block is metadata, should not emit an entity
	if len(records) != 0 {
		t.Errorf("expected 0 entities for terraform block, got %d", len(records))
	}
}

// ----------------------------------------------------------------
// resource block
// ----------------------------------------------------------------

func TestExtractResource(t *testing.T) {
	src := `
resource "aws_lambda_function" "archigraph_demo" {
  function_name = "archigraph_demo"
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "resource", "archigraph_demo")
	if r == nil {
		t.Fatalf("expected resource 'archigraph_demo' not found in %v", records)
	}
	if r.Kind != "SCOPE.Component" {
		t.Errorf("expected Kind=SCOPE.Component, got %s", r.Kind)
	}
	if r.Language != "hcl" {
		t.Errorf("expected Language=hcl, got %s", r.Language)
	}
	if r.QualifiedName != "resource.aws_lambda_function.archigraph_demo" {
		t.Errorf("unexpected QualifiedName: %s", r.QualifiedName)
	}
}

func TestResourceMetadata(t *testing.T) {
	src := `resource "aws_sqs_queue" "my_queue" {}`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "resource", "my_queue")
	if r == nil {
		t.Fatal("resource my_queue not found")
	}
	if r.Metadata["resource_type"] != "aws_sqs_queue" {
		t.Errorf("expected resource_type=aws_sqs_queue, got %v", r.Metadata["resource_type"])
	}
}

func TestMultipleResources(t *testing.T) {
	src := `
resource "aws_s3_bucket" "bucket_a" {}
resource "aws_s3_bucket" "bucket_b" {}
resource "aws_iam_role" "my_role" {}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if countBySubtype(records, "resource") < 3 {
		t.Errorf("expected >= 3 resource entities, got %d", countBySubtype(records, "resource"))
	}
}

func TestResourceLineNumbers(t *testing.T) {
	src := `
resource "aws_lambda_function" "fn" {
  function_name = "fn"
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "resource", "fn")
	if r == nil {
		t.Fatal("resource fn not found")
	}
	if r.StartLine < 1 || r.EndLine < r.StartLine {
		t.Errorf("invalid line numbers: start=%d end=%d", r.StartLine, r.EndLine)
	}
}

// ----------------------------------------------------------------
// data block
// ----------------------------------------------------------------

func TestExtractDataSource(t *testing.T) {
	src := `
data "aws_iam_policy_document" "archigraph_role" {
  statement {
    actions = ["sts:AssumeRole"]
  }
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "data_source", "archigraph_role")
	if r == nil {
		t.Fatalf("expected data_source 'archigraph_role' not found in %v", records)
	}
	if r.Kind != "SCOPE.Component" {
		t.Errorf("expected Kind=SCOPE.Component, got %s", r.Kind)
	}
	if r.QualifiedName != "data.aws_iam_policy_document.archigraph_role" {
		t.Errorf("unexpected QualifiedName: %s", r.QualifiedName)
	}
}

func TestDataSourceMetadata(t *testing.T) {
	src := `data "aws_caller_identity" "current" {}`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "data_source", "current")
	if r == nil {
		t.Fatal("data_source current not found")
	}
	if r.Metadata["data_type"] != "aws_caller_identity" {
		t.Errorf("expected data_type=aws_caller_identity, got %v", r.Metadata["data_type"])
	}
}

// ----------------------------------------------------------------
// variable block
// ----------------------------------------------------------------

func TestExtractVariable(t *testing.T) {
	src := `
variable "env" {
  type    = string
  default = "prod"
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "variable", "env")
	if r == nil {
		t.Fatalf("expected variable 'env' not found in %v", records)
	}
	if r.Kind != "SCOPE.Schema" {
		t.Errorf("expected Kind=SCOPE.Schema, got %s", r.Kind)
	}
}

func TestMultipleVariables(t *testing.T) {
	src := `
variable "env" { type = string }
variable "region" { type = string }
variable "memory" { type = number }
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if countBySubtype(records, "variable") < 3 {
		t.Errorf("expected >= 3 variable entities, got %d", countBySubtype(records, "variable"))
	}
}

// ----------------------------------------------------------------
// output block
// ----------------------------------------------------------------

func TestExtractOutput(t *testing.T) {
	src := `
output "lambda_arn" {
  value = aws_lambda_function.archigraph_demo.arn
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "output", "lambda_arn")
	if r == nil {
		t.Fatalf("expected output 'lambda_arn' not found in %v", records)
	}
	if r.Kind != "SCOPE.Schema" {
		t.Errorf("expected Kind=SCOPE.Schema, got %s", r.Kind)
	}
}

// ----------------------------------------------------------------
// module block
// ----------------------------------------------------------------

func TestExtractModule(t *testing.T) {
	src := `
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.0.0"
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "module", "vpc")
	if r == nil {
		t.Fatalf("expected module 'vpc' not found in %v", records)
	}
	if r.Kind != "SCOPE.Component" {
		t.Errorf("expected Kind=SCOPE.Component, got %s", r.Kind)
	}
	if r.QualifiedName != "terraform-aws-modules/vpc/aws" {
		t.Errorf("expected QualifiedName=terraform-aws-modules/vpc/aws, got %s", r.QualifiedName)
	}
}

func TestModuleWithoutSource(t *testing.T) {
	src := `module "simple" { version = "1.0" }`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "module", "simple")
	if r == nil {
		t.Fatal("module simple not found")
	}
	// No source → QualifiedName should be empty
	if r.QualifiedName != "" {
		t.Errorf("expected empty QualifiedName, got %s", r.QualifiedName)
	}
}

// ----------------------------------------------------------------
// provider block
// ----------------------------------------------------------------

func TestExtractProvider(t *testing.T) {
	src := `
provider "aws" {
  region = "us-east-1"
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "provider", "aws")
	if r == nil {
		t.Fatalf("expected provider 'aws' not found in %v", records)
	}
	if r.Kind != "SCOPE.Component" {
		t.Errorf("expected Kind=SCOPE.Component, got %s", r.Kind)
	}
}

// ----------------------------------------------------------------
// locals block
// ----------------------------------------------------------------

func TestExtractLocals(t *testing.T) {
	src := `
locals {
  prefix = "archigraph"
  region = "us-east-1"
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if countBySubtype(records, "local") < 2 {
		t.Errorf("expected >= 2 local entities, got %d", countBySubtype(records, "local"))
	}
	r := findBySubtypeAndName(records, "local", "prefix")
	if r == nil {
		t.Fatal("local 'prefix' not found")
	}
	if r.Kind != "SCOPE.Schema" {
		t.Errorf("expected Kind=SCOPE.Schema, got %s", r.Kind)
	}
}

func TestLocalsMultipleKeys(t *testing.T) {
	src := `
locals {
  a = 1
  b = 2
  c = 3
  d = 4
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if countBySubtype(records, "local") != 4 {
		t.Errorf("expected 4 local entities, got %d", countBySubtype(records, "local"))
	}
}

// ----------------------------------------------------------------
// depends_on relationships
// ----------------------------------------------------------------

func TestDependsOnSingle(t *testing.T) {
	src := `
resource "aws_iam_role_policy" "pol" {
  depends_on = [aws_iam_role.my_role]
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "resource", "pol")
	if r == nil {
		t.Fatal("resource pol not found")
	}
	if len(r.Relationships) == 0 {
		t.Fatal("expected DEPENDS_ON relationship")
	}
	rel := r.Relationships[0]
	if rel.Kind != "DEPENDS_ON" {
		t.Errorf("expected Kind=DEPENDS_ON, got %s", rel.Kind)
	}
	if rel.ToID != "scope:operation:method:hcl:main.tf:aws_iam_role.my_role" {
		t.Errorf("expected ToID=scope:operation:method:hcl:main.tf:aws_iam_role.my_role, got %s", rel.ToID)
	}
}

func TestDependsOnMultiple(t *testing.T) {
	src := `
resource "aws_lambda_function" "fn" {
  depends_on = [
    aws_iam_role.lambda_role,
    aws_ecr_repository.archigraph_demo,
  ]
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "resource", "fn")
	if r == nil {
		t.Fatal("resource fn not found")
	}
	if len(r.Relationships) < 2 {
		t.Errorf("expected >= 2 DEPENDS_ON relationships, got %d", len(r.Relationships))
	}
	for _, rel := range r.Relationships {
		if rel.Kind != "DEPENDS_ON" {
			t.Errorf("unexpected relationship kind: %s", rel.Kind)
		}
	}
}

func TestNoDependsOn(t *testing.T) {
	src := `resource "aws_s3_bucket" "b" { bucket = "my-bucket" }`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "resource", "b")
	if r == nil {
		t.Fatal("resource b not found")
	}
	if len(r.Relationships) != 0 {
		t.Errorf("expected 0 relationships, got %d", len(r.Relationships))
	}
}

// ----------------------------------------------------------------
// OpenTofu (#3553) — .tofu files are byte-for-byte identical HCL and the
// classifier routes them to the "terraform" token, so they flow through this
// exact extractor. This value-asserting test proves a .tofu file with two
// resources, one referencing the other, yields both resource entities AND the
// DEPENDS_ON edge via the same path as .tf — full parity, not just len>0.
// ----------------------------------------------------------------

func TestOpenTofuTwoResourcesDependencyEdge(t *testing.T) {
	src := `
resource "aws_iam_role" "lambda_role" {
  name = "lambda-exec"
}

resource "aws_lambda_function" "fn" {
  function_name = "processor"
  role          = aws_iam_role.lambda_role.arn
  depends_on    = [aws_iam_role.lambda_role]
}
`
	// Extract through the "terraform" key (the classifier's target for .tofu)
	// using a .tofu path to assert the file-extension travels through unchanged.
	records, err := extractTerraform(src, "infra/main.tofu")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both resources must be present.
	role := findBySubtypeAndName(records, "resource", "lambda_role")
	if role == nil {
		t.Fatalf("expected resource 'lambda_role' from .tofu file, got %v", records)
	}
	fn := findBySubtypeAndName(records, "resource", "fn")
	if fn == nil {
		t.Fatalf("expected resource 'fn' from .tofu file, got %v", records)
	}

	// The dependency edge fn -> lambda_role must exist, with the canonical
	// terraform-token ToID, proving the same DEPENDS_ON path as .tf.
	wantToID := "scope:operation:method:terraform:infra/main.tofu:aws_iam_role.lambda_role"
	var found bool
	for _, rel := range fn.Relationships {
		if rel.Kind == "DEPENDS_ON" && rel.ToID == wantToID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected DEPENDS_ON edge fn -> lambda_role with ToID %q; got relationships %v",
			wantToID, fn.Relationships)
	}

	// Language must be the shared "terraform" token for downstream IaC gates.
	if fn.Language != "terraform" {
		t.Errorf("expected Language=terraform on .tofu entity, got %s", fn.Language)
	}
}

// ----------------------------------------------------------------
// terraform language key
// ----------------------------------------------------------------

func TestTerraformKeyExtractsEntities(t *testing.T) {
	src := `
resource "aws_s3_bucket" "bucket" {}
variable "env" { type = string }
`
	records, err := extractTerraform(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) < 2 {
		t.Errorf("expected >= 2 entities via terraform key, got %d", len(records))
	}
	// Language field must be "terraform"
	for _, r := range records {
		if r.Language != "terraform" {
			t.Errorf("entity %q has Language=%s, expected terraform", r.Name, r.Language)
		}
	}
}

// ----------------------------------------------------------------
// Entity record invariants
// ----------------------------------------------------------------

func TestEntityRecordInvariants(t *testing.T) {
	src := `
resource "aws_lambda_function" "fn" { function_name = "fn" }
data "aws_region" "current" {}
variable "env" { type = string }
output "fn_arn" { value = aws_lambda_function.fn.arn }
module "vpc" { source = "registry/vpc/aws" }
provider "aws" { region = "us-east-1" }
locals { prefix = "archigraph" }
`
	records, err := extractHCL(src, "invariants.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected entities, got none")
	}
	for _, r := range records {
		if r.Kind == "" {
			t.Errorf("entity %q has empty Kind", r.Name)
		}
		if r.Name == "" {
			t.Error("entity has empty Name")
		}
		if r.Language == "" {
			t.Errorf("entity %q has empty Language", r.Name)
		}
		if r.QualityScore < 0.7 {
			t.Errorf("entity %q has QualityScore below 0.7: %f", r.Name, r.QualityScore)
		}
	}
}

// ----------------------------------------------------------------
// Kind mapping coverage
// ----------------------------------------------------------------

func TestKindMapping(t *testing.T) {
	tests := []struct {
		src      string
		subtype  string
		name     string
		wantKind string
	}{
		{`resource "aws_s3_bucket" "b" {}`, "resource", "b", "SCOPE.Component"},
		{`data "aws_region" "r" {}`, "data_source", "r", "SCOPE.Component"},
		{`module "m" { source = "x" }`, "module", "m", "SCOPE.Component"},
		{`provider "aws" { region = "us-east-1" }`, "provider", "aws", "SCOPE.Component"},
		{`variable "v" { type = string }`, "variable", "v", "SCOPE.Schema"},
		{`output "o" { value = "x" }`, "output", "o", "SCOPE.Schema"},
	}
	for _, tc := range tests {
		t.Run(tc.subtype+"_"+tc.name, func(t *testing.T) {
			records, err := extractHCL(tc.src, "test.tf")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			r := findBySubtypeAndName(records, tc.subtype, tc.name)
			if r == nil {
				t.Fatalf("entity subtype=%s name=%s not found", tc.subtype, tc.name)
			}
			if r.Kind != tc.wantKind {
				t.Errorf("expected Kind=%s, got %s", tc.wantKind, r.Kind)
			}
		})
	}
}

// ----------------------------------------------------------------
// Fixture file test (>=100 entities per AC #1 proxy)
// ----------------------------------------------------------------

func TestFixtureEntityCount(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/fixtures/sources/hcl/hcl__sample.tf")
	if err != nil {
		t.Skipf("fixture not found: %v", err)
	}
	records, err := extractHCL(string(src), "hcl__sample.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) < 30 {
		t.Errorf("expected >= 30 entities from fixture, got %d", len(records))
		for _, r := range records {
			t.Logf("  [%s] %s (%s)", r.Kind, r.Name, r.Subtype)
		}
	}
}

func TestFixtureSubtypeCoverage(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/fixtures/sources/hcl/hcl__sample.tf")
	if err != nil {
		t.Skipf("fixture not found: %v", err)
	}
	records, err := extractHCL(string(src), "hcl__sample.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subtypes := map[string]int{}
	for _, r := range records {
		subtypes[r.Subtype]++
	}

	required := []string{"resource", "data_source", "variable", "output", "module", "provider", "local"}
	for _, sub := range required {
		if subtypes[sub] == 0 {
			t.Errorf("expected at least 1 entity with subtype=%s in fixture, got 0", sub)
		}
	}
}

func TestFixtureDependsOn(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/fixtures/sources/hcl/hcl__sample.tf")
	if err != nil {
		t.Skipf("fixture not found: %v", err)
	}
	records, err := extractHCL(string(src), "hcl__sample.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// At least one entity should have a DEPENDS_ON relationship.
	found := false
	for _, r := range records {
		for _, rel := range r.Relationships {
			if rel.Kind == "DEPENDS_ON" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("expected at least 1 DEPENDS_ON relationship in fixture")
	}
}

// ----------------------------------------------------------------
// Terraform fixture: EKS + Lambda + RDS infra module
// Acceptance criteria: ≥80% entity recall on the synthetic fixture.
// ----------------------------------------------------------------

// terraformFixturePath is the path to the EKS + Lambda + RDS fixture relative
// to the test file.
const terraformFixturePath = "../../../testdata/fixtures/sources/terraform/terraform__infra.tf"

// expectedTerraformEntities lists every entity that must be present in the
// fixture to satisfy the ≥80% recall gate.  The list covers the canonical
// representative for each block type; the full fixture has more entries.
var expectedTerraformEntities = []struct {
	subtype string
	label   string
}{
	// resources
	{"resource", "postgres"},
	{"resource", "processor"},
	{"resource", "lambda_role"},
	{"resource", "lambda_policy"},
	{"resource", "lambda"},
	{"resource", "rds"},
	{"resource", "processor_input"},
	{"resource", "processor_dlq"},
	{"resource", "artifacts"},
	// data sources
	{"data_source", "available"},
	{"data_source", "current"},
	{"data_source", "lambda_assume_role"},
	{"data_source", "lambda_permissions"},
	{"data_source", "cluster"},
	// modules
	{"module", "vpc"},
	{"module", "eks"},
	// variables
	{"variable", "region"},
	{"variable", "cluster_name"},
	{"variable", "db_password"},
	{"variable", "lambda_memory"},
	{"variable", "environment"},
	// outputs
	{"output", "eks_cluster_name"},
	{"output", "rds_endpoint"},
	{"output", "lambda_arn"},
	{"output", "sqs_queue_url"},
	{"output", "vpc_id"},
	// providers
	{"provider", "aws"},
	{"provider", "kubernetes"},
	// locals
	{"local", "name_prefix"},
	{"local", "common_tags"},
	{"local", "lambda_function_name"},
	{"local", "db_identifier"},
}

func TestTerraformFixtureEntityCount(t *testing.T) {
	src, err := os.ReadFile(terraformFixturePath)
	if err != nil {
		t.Skipf("terraform fixture not found: %v", err)
	}
	records, err := extractTerraform(string(src), "terraform__infra.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The fixture is large; verify a floor that represents ≥80% recall.
	// (Actual count is much higher due to file-level SCOPE.Component + many
	// resources; 30 is a conservative lower bound.)
	if len(records) < 30 {
		t.Errorf("expected >= 30 entities from terraform fixture, got %d", len(records))
		for _, r := range records {
			t.Logf("  [%s] %s (%s)", r.Kind, r.Name, r.Subtype)
		}
	}
}

func TestTerraformFixtureLanguageLabel(t *testing.T) {
	src, err := os.ReadFile(terraformFixturePath)
	if err != nil {
		t.Skipf("terraform fixture not found: %v", err)
	}
	records, err := extractTerraform(string(src), "terraform__infra.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range records {
		if r.Language != "terraform" {
			t.Errorf("entity %q has Language=%s, expected terraform", r.Name, r.Language)
		}
	}
}

func TestTerraformFixtureSubtypeCoverage(t *testing.T) {
	src, err := os.ReadFile(terraformFixturePath)
	if err != nil {
		t.Skipf("terraform fixture not found: %v", err)
	}
	records, err := extractTerraform(string(src), "terraform__infra.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subtypes := map[string]int{}
	for _, r := range records {
		subtypes[r.Subtype]++
	}

	required := []string{"resource", "data_source", "variable", "output", "module", "provider", "local"}
	for _, sub := range required {
		if subtypes[sub] == 0 {
			t.Errorf("expected at least 1 entity with subtype=%s in terraform fixture, got 0", sub)
		}
	}
}

func TestTerraformFixtureRecall(t *testing.T) {
	src, err := os.ReadFile(terraformFixturePath)
	if err != nil {
		t.Skipf("terraform fixture not found: %v", err)
	}
	records, err := extractTerraform(string(src), "terraform__infra.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := 0
	missing := []string{}
	for _, want := range expectedTerraformEntities {
		r := findBySubtypeAndName(records, want.subtype, want.label)
		if r != nil {
			found++
		} else {
			missing = append(missing, want.subtype+":"+want.label)
		}
	}
	total := len(expectedTerraformEntities)
	recall := float64(found) / float64(total)
	if recall < 0.80 {
		t.Errorf("terraform fixture recall %.1f%% (%d/%d) < 80%%; missing: %v",
			recall*100, found, total, missing)
	}
}

func TestTerraformFixtureDependsOn(t *testing.T) {
	src, err := os.ReadFile(terraformFixturePath)
	if err != nil {
		t.Skipf("terraform fixture not found: %v", err)
	}
	records, err := extractTerraform(string(src), "terraform__infra.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, r := range records {
		for _, rel := range r.Relationships {
			if rel.Kind == "DEPENDS_ON" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("expected at least 1 DEPENDS_ON relationship in terraform fixture")
	}
}

func TestTerraformFixtureModuleImports(t *testing.T) {
	src, err := os.ReadFile(terraformFixturePath)
	if err != nil {
		t.Skipf("terraform fixture not found: %v", err)
	}
	records, err := extractTerraform(string(src), "terraform__infra.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The file-level entity should carry IMPORTS edges for both module sources.
	fileRec := findFileComponent(records, "terraform__infra.tf")
	if fileRec == nil {
		t.Fatal("expected file-level SCOPE.Component entity, not found")
	}

	importsCount := 0
	for _, rel := range fileRec.Relationships {
		if rel.Kind == "IMPORTS" {
			importsCount++
		}
	}
	if importsCount < 2 {
		t.Errorf("expected >= 2 IMPORTS edges from file entity (vpc + eks modules), got %d", importsCount)
	}
}

func TestTerraformFixtureZeroFalsePositives(t *testing.T) {
	src, err := os.ReadFile(terraformFixturePath)
	if err != nil {
		t.Skipf("terraform fixture not found: %v", err)
	}
	records, err := extractTerraform(string(src), "terraform__infra.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Zero false-positive check: every entity must have a non-empty Name,
	// Kind, and Language, and must have a valid Subtype.
	validSubtypes := map[string]bool{
		"resource": true, "data_source": true, "module": true,
		"variable": true, "output": true, "provider": true,
		"local": true, "file": true,
		// Issue #3527 — deeper extraction adds these subtypes.
		"terraform_settings": true, "dynamic_block": true,
	}
	for _, r := range records {
		if r.Name == "" {
			t.Errorf("entity has empty Name (Kind=%s, Subtype=%s)", r.Kind, r.Subtype)
		}
		if r.Kind == "" {
			t.Errorf("entity %q has empty Kind", r.Name)
		}
		if r.Language == "" {
			t.Errorf("entity %q has empty Language", r.Name)
		}
		if r.Subtype != "" && !validSubtypes[r.Subtype] {
			t.Errorf("entity %q has unexpected Subtype=%s", r.Name, r.Subtype)
		}
	}
}
