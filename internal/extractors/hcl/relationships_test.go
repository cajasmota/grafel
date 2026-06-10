package hcl_test

import (
	"os"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ----------------------------------------------------------------
// helpers
// ----------------------------------------------------------------

func collectRels(records []types.EntityRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range records {
		for _, rel := range r.Relationships {
			if rel.Kind == kind {
				out = append(out, rel)
			}
		}
	}
	return out
}

func findFileComponent(records []types.EntityRecord, path string) *types.EntityRecord {
	for i := range records {
		if records[i].Kind == "SCOPE.Component" &&
			records[i].Subtype == "file" &&
			records[i].SourceFile == path {
			return &records[i]
		}
	}
	return nil
}

// ----------------------------------------------------------------
// IMPORTS — module source
// ----------------------------------------------------------------

// TestImports_ModuleSource asserts that a module block's `source = "..."`
// emits an IMPORTS edge from the file to the source value.
func TestImports_ModuleSource(t *testing.T) {
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
	imports := collectRels(records, "IMPORTS")
	found := false
	for _, rel := range imports {
		if rel.ToID == "terraform-aws-modules/vpc/aws" && rel.FromID == "main.tf" {
			found = true
			if rel.Properties["import_kind"] != "module" {
				t.Errorf("expected import_kind=module, got %q", rel.Properties["import_kind"])
			}
		}
	}
	if !found {
		t.Errorf("expected IMPORTS edge file→module source, got %+v", imports)
	}
}

// TestImports_ModuleWithoutSource asserts no IMPORTS edge is emitted for a
// module block missing the source attribute.
func TestImports_ModuleWithoutSource(t *testing.T) {
	src := `module "stub" { version = "1.0" }`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, rel := range collectRels(records, "IMPORTS") {
		if rel.Properties["import_kind"] == "module" {
			t.Errorf("unexpected module IMPORTS edge: %+v", rel)
		}
	}
}

// ----------------------------------------------------------------
// IMPORTS — provider
// ----------------------------------------------------------------

// TestImports_Provider asserts that a `provider "aws" {}` block emits an
// IMPORTS edge from the file to the provider name.
func TestImports_Provider(t *testing.T) {
	src := `provider "aws" { region = "us-east-1" }`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	imports := collectRels(records, "IMPORTS")
	found := false
	for _, rel := range imports {
		if rel.ToID == "aws" && rel.FromID == "main.tf" && rel.Properties["import_kind"] == "provider" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected provider IMPORTS edge, got %+v", imports)
	}
}

// ----------------------------------------------------------------
// CONTAINS — file → top-level blocks
// ----------------------------------------------------------------

// TestContains_FileLevelComponent asserts that a SCOPE.Component / file
// entity is emitted carrying CONTAINS edges to every top-level block.
func TestContains_FileLevelComponent(t *testing.T) {
	src := `
resource "aws_s3_bucket" "b" {}
variable "env" { type = string }
output "o" { value = "x" }
provider "aws" { region = "us-east-1" }
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fc := findFileComponent(records, "main.tf")
	if fc == nil {
		t.Fatal("expected file-level SCOPE.Component / file entity, not found")
	}
	contains := collectRels(records, "CONTAINS")
	if len(contains) < 4 {
		t.Errorf("expected >= 4 CONTAINS edges, got %d: %+v", len(contains), contains)
	}
	// All CONTAINS edges must use BuildOperationStructuralRef("hcl", ...)
	wantPrefix := extractor.BuildOperationStructuralRef("hcl", "main.tf", "")
	for _, rel := range contains {
		if rel.FromID != "main.tf" {
			t.Errorf("CONTAINS FromID expected file path, got %q", rel.FromID)
		}
		if len(rel.ToID) <= len(wantPrefix) || rel.ToID[:len(wantPrefix)] != wantPrefix {
			t.Errorf("CONTAINS ToID does not match structural-ref prefix %q: got %q", wantPrefix, rel.ToID)
		}
	}
}

// TestContains_LocalsKeys asserts each locals key gets its own CONTAINS edge.
func TestContains_LocalsKeys(t *testing.T) {
	src := `
locals {
  prefix = "x"
  region = "y"
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	contains := collectRels(records, "CONTAINS")
	wantPrefix := extractor.BuildOperationStructuralRef("hcl", "main.tf", "local.prefix")
	wantRegion := extractor.BuildOperationStructuralRef("hcl", "main.tf", "local.region")
	seenPrefix, seenRegion := false, false
	for _, rel := range contains {
		if rel.ToID == wantPrefix {
			seenPrefix = true
		}
		if rel.ToID == wantRegion {
			seenRegion = true
		}
	}
	if !seenPrefix || !seenRegion {
		t.Errorf("expected CONTAINS edges for both locals keys, got %+v", contains)
	}
}

// TestContains_NoBlocksNoComponent asserts the file-level component is not
// emitted when the file has no top-level blocks.
func TestContains_NoBlocksNoComponent(t *testing.T) {
	src := `# only a comment
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findFileComponent(records, "main.tf") != nil {
		t.Errorf("did not expect file-level component for empty file, got entities: %+v", records)
	}
}

// ----------------------------------------------------------------
// CALLS — interpolation cross-references
// ----------------------------------------------------------------

// TestCalls_ResourceReferencesResource asserts that a resource referencing
// another resource via its attributes emits a CALLS edge.
func TestCalls_ResourceReferencesResource(t *testing.T) {
	src := `
resource "aws_iam_role" "lambda_role" {}

resource "aws_lambda_function" "fn" {
  role = aws_iam_role.lambda_role.arn
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	calls := collectRels(records, "CALLS")
	found := false
	for _, rel := range calls {
		if rel.ToID == "scope:operation:method:hcl:main.tf:aws_iam_role.lambda_role" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CALLS edge to aws_iam_role.lambda_role, got %+v", calls)
	}
}

// TestCalls_ResourceReferencesVariable asserts CALLS edge to var.<name>.
func TestCalls_ResourceReferencesVariable(t *testing.T) {
	src := `
resource "aws_lambda_function" "fn" {
  function_name = var.fn_name
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	calls := collectRels(records, "CALLS")
	found := false
	for _, rel := range calls {
		if rel.ToID == "scope:operation:method:hcl:main.tf:var.fn_name" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CALLS edge to var.fn_name, got %+v", calls)
	}
}

// TestCalls_ResourceReferencesLocal asserts CALLS edge to local.<name>.
func TestCalls_ResourceReferencesLocal(t *testing.T) {
	src := `
resource "aws_s3_bucket" "b" {
  bucket = local.bucket_name
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	calls := collectRels(records, "CALLS")
	found := false
	for _, rel := range calls {
		if rel.ToID == "scope:operation:method:hcl:main.tf:local.bucket_name" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CALLS edge to local.bucket_name, got %+v", calls)
	}
}

// TestCalls_ResourceReferencesModuleOutput asserts CALLS edge to module.<name>.
func TestCalls_ResourceReferencesModuleOutput(t *testing.T) {
	src := `
resource "aws_lambda_function" "fn" {
  vpc_config {
    subnet_ids = module.vpc.private_subnets
  }
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	calls := collectRels(records, "CALLS")
	found := false
	for _, rel := range calls {
		if rel.ToID == "scope:operation:method:hcl:main.tf:module.vpc" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CALLS edge to module.vpc, got %+v", calls)
	}
}

// TestCalls_NoSelfReference asserts that a resource does not emit a CALLS
// edge to itself (e.g. when its own labels appear inside its body — they
// shouldn't, but we guard against an emitted self-edge anyway).
func TestCalls_NoSelfReference(t *testing.T) {
	src := `
resource "aws_lambda_function" "fn" {
  function_name = "fn"
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, rel := range collectRels(records, "CALLS") {
		if rel.FromID == rel.ToID {
			t.Errorf("unexpected self CALLS edge: %+v", rel)
		}
	}
}

// ----------------------------------------------------------------
// Real-world corpus smoke test
// ----------------------------------------------------------------

// TestCorpus_TerraformAwsVpc_RelationshipCounts asserts that the real-world
// terraform-aws-vpc corpus produces non-trivial counts of all three new edge
// kinds. This is the proxy for issue #162 corpus coverage.
func TestCorpus_TerraformAwsVpc_RelationshipCounts(t *testing.T) {
	const path = "../../../testdata/fixtures/real-world/hcl/terraform_aws_vpc.tf"
	srcBytes, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("corpus not found: %v", err)
	}
	records, err := extractHCL(string(srcBytes), "terraform_aws_vpc.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contains := collectRels(records, "CONTAINS")
	calls := collectRels(records, "CALLS")
	if len(contains) < 5 {
		t.Errorf("expected >= 5 CONTAINS edges in vpc corpus, got %d", len(contains))
	}
	if len(calls) < 5 {
		t.Errorf("expected >= 5 CALLS edges in vpc corpus, got %d", len(calls))
	}
}

// ----------------------------------------------------------------
// Issue #4625 — cross-module output references → semantic USES edges
// ----------------------------------------------------------------

// crossModuleUSES collects USES edges tagged dataflow=cross_module.
func crossModuleUSES(records []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, rel := range collectRels(records, "USES") {
		if rel.Properties["dataflow"] == "cross_module" {
			out = append(out, rel)
		}
	}
	return out
}

// TestCrossModule_ConsumesQueueURL is the headline #4625 fixture: a worker ECS
// service consumes another module's queue_url output. Before the fix the only
// edge was a bare CALLS to module.dispatch_queue (output name + semantics lost,
// surfaced as an unresolved relation). After: a semantic USES edge carrying the
// referenced output and the derived "consumes" verb, bound to the same-file
// module block so it resolves.
func TestCrossModule_ConsumesQueueURL(t *testing.T) {
	src := `
module "dispatch_queue" {
  source = "./modules/sqs"
  name   = "dispatch"
}

resource "aws_ecs_service" "worker" {
  name = "worker"
  environment {
    queue_url = module.dispatch_queue.queue_url
  }
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	uses := crossModuleUSES(records)
	if len(uses) != 1 {
		t.Fatalf("expected exactly 1 cross-module USES edge, got %d: %+v", len(uses), uses)
	}
	rel := uses[0]
	wantTo := extractor.BuildOperationStructuralRef("hcl", "main.tf", "module.dispatch_queue")
	if rel.ToID != wantTo {
		t.Errorf("ToID = %q, want %q", rel.ToID, wantTo)
	}
	if got := rel.Properties["module_output"]; got != "queue_url" {
		t.Errorf("module_output = %q, want queue_url", got)
	}
	if got := rel.Properties["semantic"]; got != "consumes" {
		t.Errorf("semantic = %q, want consumes", got)
	}
}

// TestCrossModule_SemanticVariants asserts the semantic-label derivation for the
// cloud-architecture verbs #4625 renders.
func TestCrossModule_SemanticVariants(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "redrive",
			src: `
module "dlq" { source = "./dlq" }
resource "aws_sqs_queue" "main" {
  redrive_policy = module.dlq.queue_arn
}`,
			want: "redrive",
		},
		{
			name: "logs-to",
			src: `
module "logs" { source = "./logs" }
resource "aws_ecs_task_definition" "task" {
  log_group = module.logs.log_group_name
}`,
			want: "logs-to",
		},
		{
			name: "assumes",
			src: `
module "iam" { source = "./iam" }
resource "aws_ecs_task_definition" "task" {
  execution_role_arn = module.iam.role_arn
}`,
			want: "assumes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			records, err := extractHCL(tc.src, "main.tf")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			uses := crossModuleUSES(records)
			if len(uses) != 1 {
				t.Fatalf("expected 1 cross-module USES edge, got %d: %+v", len(uses), uses)
			}
			if got := uses[0].Properties["semantic"]; got != tc.want {
				t.Errorf("semantic = %q, want %q", got, tc.want)
			}
		})
	}
}
