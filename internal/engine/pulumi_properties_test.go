// Value-asserting tests for Pulumi curated scalar property stamping (epic
// #4194, iac_resource_property_extraction). They assert the EXACT stamped
// key=value pairs on the InfraResource entity, the negatives (Output/ref-valued
// args are NOT stamped as scalars), and a regression guard that the existing
// output_ref DEPENDS_ON edge mining still fires unchanged.
package engine

import (
	"testing"
)

// TestPulumi_ScalarProps_TS asserts curated literal scalar args on a TS Pulumi
// resource are stamped with exact values, while Output-valued args
// (role: role.arn) are NOT stamped as scalars.
func TestPulumi_ScalarProps_TS(t *testing.T) {
	src := `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const web = new aws.ec2.Instance("web", {
    instanceType: "t3.micro",
    ami: ami.id,
    count: 3,
});
`
	ents, _ := runPulumiDetect(t, "typescript", "index.ts", src)
	r := pulumiResourceByName(ents, "web")
	if r == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'web', got %+v", ents)
	}
	// instanceType="t3.micro" — quoted-string scalar, IS stamped.
	if got := r.props["instanceType"]; got != "t3.micro" {
		t.Errorf("web instanceType = %q, want t3.micro", got)
	}
	// count=3 — numeric scalar, IS stamped.
	if got := r.props["count"]; got != "3" {
		t.Errorf("web count = %q, want 3", got)
	}
	// ami=ami.id — an Output/member-access reference, NOT stamped (and ami isn't
	// curated anyway, doubly excluded).
	if _, ok := r.props["ami"]; ok {
		t.Errorf("web ami stamped, want NOT stamped (output ref / not curated)")
	}
}

// TestPulumi_ScalarProps_TS_OutputValuedCuratedKeyNotStamped asserts that a
// CURATED key whose value is an Output reference is NOT stamped as a scalar.
func TestPulumi_ScalarProps_TS_OutputValuedCuratedKeyNotStamped(t *testing.T) {
	src := `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const cache = new aws.elasticache.Cluster("cache", {
    nodeType: sizing.nodeType,
    port: 6379,
});
`
	ents, _ := runPulumiDetect(t, "typescript", "index.ts", src)
	r := pulumiResourceByName(ents, "cache")
	if r == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'cache', got %+v", ents)
	}
	// nodeType=sizing.nodeType — member-access ref, curated key but NOT stamped.
	if got, ok := r.props["nodeType"]; ok {
		t.Errorf("cache nodeType stamped = %q, want NOT stamped (output ref)", got)
	}
	// port=6379 — numeric scalar, IS stamped.
	if got := r.props["port"]; got != "6379" {
		t.Errorf("cache port = %q, want 6379", got)
	}
}

// TestPulumi_ScalarProps_Python asserts Python snake_case curated kwargs are
// stamped with exact values, while Output-valued kwargs are NOT.
func TestPulumi_ScalarProps_Python(t *testing.T) {
	src := `import pulumi
import pulumi_aws as aws

web = aws.ec2.Instance("web",
    instance_type="t3.large",
    ami=ami.id,
    desired_capacity=2,
)
`
	ents, _ := runPulumiDetect(t, "python", "__main__.py", src)
	r := pulumiResourceByName(ents, "web")
	if r == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'web', got %+v", ents)
	}
	if got := r.props["instance_type"]; got != "t3.large" {
		t.Errorf("web instance_type = %q, want t3.large", got)
	}
	if got := r.props["desired_capacity"]; got != "2" {
		t.Errorf("web desired_capacity = %q, want 2", got)
	}
	if _, ok := r.props["ami"]; ok {
		t.Errorf("web ami stamped, want NOT stamped (output ref / not curated)")
	}
}

// TestPulumi_ScalarProps_NoRegressionOutputRefEdge guards that scalar stamping
// does NOT regress the existing output_ref DEPENDS_ON edge mining.
func TestPulumi_ScalarProps_NoRegressionOutputRefEdge(t *testing.T) {
	src := `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const data = new aws.s3.Bucket("data", { versioned: true });

const fn = new aws.lambda.Function("fn", {
    runtime: "nodejs20.x",
    memorySize: 1024,
    environment: { variables: { BUCKET: data.arn } },
});
`
	ents, rels := runPulumiDetect(t, "typescript", "index.ts", src)

	// Scalar stamping still happened on fn.
	fn := pulumiResourceByName(ents, "fn")
	if fn == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'fn', got %+v", ents)
	}
	if got := fn.props["runtime"]; got != "nodejs20.x" {
		t.Errorf("fn runtime = %q, want nodejs20.x", got)
	}
	if got := fn.props["memorySize"]; got != "1024" {
		t.Errorf("fn memorySize = %q, want 1024", got)
	}

	// output_ref edge fn→data still fires.
	deps := relsByKind(rels, "DEPENDS_ON")
	var found *relResult
	for i := range deps {
		if deps[i].from == "SCOPE.InfraResource:fn" &&
			deps[i].to == "SCOPE.InfraResource:data" &&
			deps[i].props["reason"] == "output_ref" {
			found = &deps[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected output_ref DEPENDS_ON fn→data, got %+v", deps)
	}
}
