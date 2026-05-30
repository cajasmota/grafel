package hcl_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ----------------------------------------------------------------
// Issue #3527 — deeper Terraform extraction: value-asserting tests.
//
// The headline scenario: a config with for_each, a dynamic block, and two
// modules where one passes module.net.vpc_id into the other. We assert the
// concrete iteration recognition, the dynamic-block child entity, and the
// module→module data-flow edge — not len()>0.
// ----------------------------------------------------------------

func sref(name string) string {
	return extractor.BuildOperationStructuralRef("hcl", "main.tf", name)
}

func relExists(records []types.EntityRecord, kind, from, to string, propKey, propVal string) bool {
	for _, r := range records {
		for _, rel := range r.Relationships {
			if rel.Kind != kind || rel.FromID != from || rel.ToID != to {
				continue
			}
			if propKey == "" {
				return true
			}
			if rel.Properties != nil && rel.Properties[propKey] == propVal {
				return true
			}
		}
	}
	return false
}

// TestDeep_HeadlineScenario exercises iteration, dynamic blocks, and
// module→module data flow in a single config.
func TestDeep_HeadlineScenario(t *testing.T) {
	src := `
module "net" {
  source = "./net"
}

module "app" {
  source = "./app"
  vpc_id = module.net.vpc_id
}

resource "aws_instance" "web" {
  for_each = var.instances
  ami      = each.value.ami

  dynamic "ebs_block_device" {
    for_each = var.volumes
    content {
      volume_size = ebs_block_device.value.size
      kms_key     = aws_kms_key.disk.arn
    }
  }
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// (1) Iteration recognition: the resource records for_each mode and emits
	// a USES edge to var.instances (the iteration source).
	web := findBySubtypeAndName(records, "resource", "web")
	if web == nil {
		t.Fatal("resource web not found")
	}
	if web.Metadata["iteration"] != "for_each" {
		t.Errorf("expected iteration=for_each, got %v", web.Metadata["iteration"])
	}
	if web.Metadata["iteration_source"] != "var.instances" {
		t.Errorf("expected iteration_source=var.instances, got %v", web.Metadata["iteration_source"])
	}
	if !relExists(records, "USES", sref("aws_instance.web"), sref("var.instances"), "dataflow", "iteration") {
		t.Error("expected USES iteration-source edge aws_instance.web → var.instances")
	}

	// each.value.ami must NOT produce a bogus CALLS edge to each.value.
	for _, rel := range collectRels(records, "CALLS") {
		if rel.ToID == sref("each.value") {
			t.Errorf("unexpected CALLS edge to each.value pseudo-ref: %+v", rel)
		}
	}

	// (2) Dynamic block entity: the nested dynamic "ebs_block_device" block is
	// emitted as a child SCOPE.Component / dynamic_block entity with its own
	// for_each source, and a CONTAINS edge from the parent resource.
	dyn := findBySubtypeAndName(records, "dynamic_block", "ebs_block_device")
	if dyn == nil {
		t.Fatal("dynamic_block ebs_block_device not found")
	}
	if dyn.Name != "aws_instance.web.dynamic.ebs_block_device" {
		t.Errorf("unexpected dynamic block name: %s", dyn.Name)
	}
	if dyn.Metadata["iteration_source"] != "var.volumes" {
		t.Errorf("expected dynamic block iteration_source=var.volumes, got %v", dyn.Metadata["iteration_source"])
	}
	if !relExists(records, "CONTAINS",
		sref("aws_instance.web"),
		sref("aws_instance.web.dynamic.ebs_block_device"), "nested", "dynamic") {
		t.Error("expected CONTAINS edge resource → dynamic block")
	}
	// The dynamic block's content references aws_kms_key.disk → CALLS edge.
	if !relExists(records, "CALLS",
		sref("aws_instance.web.dynamic.ebs_block_device"),
		sref("aws_kms_key.disk"), "", "") {
		t.Error("expected CALLS edge from dynamic block to aws_kms_key.disk")
	}

	// (3) Module → module data-flow: module.app consumes module.net.vpc_id via
	// its vpc_id input → a USES data-flow edge tagged with the input arg name.
	if !relExists(records, "USES", sref("module.app"), sref("module.net"), "input_arg", "vpc_id") {
		t.Errorf("expected module→module data-flow USES edge module.app → module.net (input_arg=vpc_id); got %+v",
			collectRels(records, "USES"))
	}
}

// TestDeep_CountIteration asserts count meta-arg recognition with a non-entity
// (literal) source produces the mode but no USES edge.
func TestDeep_CountIteration(t *testing.T) {
	src := `
resource "aws_instance" "n" {
  count = 3
  ami   = "ami-123"
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := findBySubtypeAndName(records, "resource", "n")
	if r == nil {
		t.Fatal("resource n not found")
	}
	if r.Metadata["iteration"] != "count" {
		t.Errorf("expected iteration=count, got %v", r.Metadata["iteration"])
	}
	// Literal count has no entity source → no iteration_source, no USES edge.
	if _, ok := r.Metadata["iteration_source"]; ok {
		t.Errorf("did not expect iteration_source for literal count, got %v", r.Metadata["iteration_source"])
	}
	for _, rel := range collectRels(records, "USES") {
		if rel.Properties["dataflow"] == "iteration" {
			t.Errorf("unexpected iteration USES edge for literal count: %+v", rel)
		}
	}
}

// TestDeep_RemoteStateCrossStack asserts that consuming
// data.terraform_remote_state.X.outputs.Y emits a cross-stack DEPENDS_ON edge
// (and NOT a generic data-source CALLS edge).
func TestDeep_RemoteStateCrossStack(t *testing.T) {
	src := `
resource "aws_subnet" "s" {
  vpc_id = data.terraform_remote_state.network.outputs.vpc_id
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !relExists(records, "DEPENDS_ON",
		sref("aws_subnet.s"),
		sref("data.terraform_remote_state.network"), "cross_stack", "true") {
		t.Errorf("expected cross-stack DEPENDS_ON edge to remote state; got DEPENDS_ON=%+v",
			collectRels(records, "DEPENDS_ON"))
	}
	// Must NOT appear as a generic data CALLS edge.
	for _, rel := range collectRels(records, "CALLS") {
		if rel.ToID == sref("data.terraform_remote_state.network") {
			t.Errorf("remote_state must not be a generic data CALLS edge: %+v", rel)
		}
	}
}

// TestDeep_TerraformBlockProviders asserts the terraform{} block is captured as
// a terraform_settings entity with required_providers (version) + backend, and
// emits an IMPORTS edge per required provider.
func TestDeep_TerraformBlockProviders(t *testing.T) {
	src := `
terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    random = {
      source = "hashicorp/random"
    }
  }
  backend "s3" {
    bucket = "my-state"
  }
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := findBySubtypeAndName(records, "terraform_settings", "")
	// label is empty; fall back to name lookup.
	if s == nil {
		for i := range records {
			if records[i].Subtype == "terraform_settings" {
				s = &records[i]
				break
			}
		}
	}
	if s == nil {
		t.Fatal("terraform_settings entity not found")
	}
	if s.Metadata["backend"] != "s3" {
		t.Errorf("expected backend=s3, got %v", s.Metadata["backend"])
	}
	if s.Metadata["required_version"] != ">= 1.5" {
		t.Errorf("expected required_version=\">= 1.5\", got %v", s.Metadata["required_version"])
	}
	// IMPORTS edge for aws provider carrying its version.
	if !relExists(records, "IMPORTS",
		sref("terraform.settings"), "aws", "import_kind", "required_provider") {
		t.Errorf("expected IMPORTS required_provider edge for aws; got %+v", collectRels(records, "IMPORTS"))
	}
	awsVer := ""
	for _, rel := range collectRels(records, "IMPORTS") {
		if rel.ToID == "aws" && rel.Properties["import_kind"] == "required_provider" {
			awsVer = rel.Properties["version"]
		}
	}
	if awsVer != "~> 5.0" {
		t.Errorf("expected aws provider version \"~> 5.0\", got %q", awsVer)
	}
}

// TestDeep_TerraformBlockVersionOnlyNoEntity asserts a terraform{} block with
// only required_version (no providers/backend) stays metadata-only.
func TestDeep_TerraformBlockVersionOnlyNoEntity(t *testing.T) {
	src := `
terraform {
  required_version = ">= 1.0"
}
`
	records, err := extractHCL(src, "main.tf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range records {
		if r.Subtype == "terraform_settings" {
			t.Errorf("did not expect terraform_settings entity for version-only block, got %+v", r)
		}
	}
}
