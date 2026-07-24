package hcl_test

import (
	"testing"

	hcl "github.com/cajasmota/grafel/internal/extractors/hcl"
	"github.com/cajasmota/grafel/internal/types"
)

// findModuleInstance returns the module entity with the given label, or nil.
func findModuleInstance(records []types.EntityRecord, label string) *types.EntityRecord {
	return findBySubtypeAndName(records, "module", label)
}

// instantiatesEdge returns the first INSTANTIATES relationship on a record.
func instantiatesEdge(rec *types.EntityRecord) *types.RelationshipRecord {
	if rec == nil {
		return nil
	}
	for i := range rec.Relationships {
		if rec.Relationships[i].Kind == "INSTANTIATES" {
			return &rec.Relationships[i]
		}
	}
	return nil
}

// TestModuleInstantiationResolvesDefinitionDir is the headline #4657 fixture:
// an env stack's `module "worker" { source = "../../modules/worker-service" }`
// must resolve to the module DEFINITION directory and emit an INSTANTIATES edge
// tagged with that directory, plus stamp the env onto the instance.
func TestModuleInstantiationResolvesDefinitionDir(t *testing.T) {
	src := `
module "worker" {
  source       = "../../modules/worker-service"
  queue_url    = module.dispatch_queue.queue_url
  desired_count = 3
}

module "dispatch_queue" {
  source = "../../modules/sqs-queue"
}
`
	records, err := extractTerraform(src, "envs/prod/main.tf")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	worker := findModuleInstance(records, "worker")
	if worker == nil {
		t.Fatal("module.worker instance not extracted")
	}

	// definition_dir / env / module_source must be stamped on Properties (the
	// surface the dashboard reads).
	if got := worker.Properties["definition_dir"]; got != "modules/worker-service" {
		t.Errorf("definition_dir = %q, want modules/worker-service", got)
	}
	if got := worker.Properties["env"]; got != "prod" {
		t.Errorf("env = %q, want prod", got)
	}
	if got := worker.Properties["module_source"]; got != "../../modules/worker-service" {
		t.Errorf("module_source = %q, want ../../modules/worker-service", got)
	}

	edge := instantiatesEdge(worker)
	if edge == nil {
		t.Fatal("no INSTANTIATES edge on module.worker")
	}
	if want := hcl.DefinitionDirMarkerPrefix + "modules/worker-service"; edge.ToID != want {
		t.Errorf("INSTANTIATES ToID = %q, want %q", edge.ToID, want)
	}
	if got := edge.Properties.Get("definition_dir"); got != "modules/worker-service" {
		t.Errorf("edge definition_dir prop = %q, want modules/worker-service", got)
	}

	// The second instance resolves to the sqs-queue definition.
	dq := findModuleInstance(records, "dispatch_queue")
	if dq == nil {
		t.Fatal("module.dispatch_queue instance not extracted")
	}
	if got := dq.Properties["definition_dir"]; got != "modules/sqs-queue" {
		t.Errorf("dispatch_queue definition_dir = %q, want modules/sqs-queue", got)
	}
}

// TestModuleInstantiationRemoteSourceNoEdge verifies registry/git sources do
// NOT get a definition_dir or an INSTANTIATES edge (no in-repo directory), but
// still record the raw source + env.
func TestModuleInstantiationRemoteSourceNoEdge(t *testing.T) {
	src := `
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.0.0"
}
`
	records, err := extractTerraform(src, "envs/staging/main.tf")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	vpc := findModuleInstance(records, "vpc")
	if vpc == nil {
		t.Fatal("module.vpc not extracted")
	}
	if got := vpc.Properties["definition_dir"]; got != "" {
		t.Errorf("definition_dir = %q, want empty for registry source", got)
	}
	if got := vpc.Properties["module_source"]; got != "terraform-aws-modules/vpc/aws" {
		t.Errorf("module_source = %q", got)
	}
	if got := vpc.Properties["env"]; got != "staging" {
		t.Errorf("env = %q, want staging", got)
	}
	if e := instantiatesEdge(vpc); e != nil {
		t.Errorf("unexpected INSTANTIATES edge for registry source: %+v", e)
	}
}
