// Value-asserting tests for the Pulumi (TypeScript + Python) resource +
// dependency extraction pass (pulumi_edges.go) — #3528, epic #3512.
//
// The marquee fixtures assert the EXACT extracted shape (not len>0):
//   - SCOPE.InfraResource entities named by their logical-name string literal
//     with construct_type set to the Pulumi resource type.
//   - DEPENDS_ON edges where one resource's output (`data.arn`) feeds another's
//     args, and where an explicit dependsOn / depends_on list is declared.
package engine

import (
	"testing"
)

func runPulumiDetect(t *testing.T, lang, path, src string) ([]entityResult, []relResult) {
	t.Helper()
	res := applyPulumiEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	out := make([]entityResult, 0, len(res.Entities))
	for _, e := range res.Entities {
		out = append(out, entityResult{kind: e.Kind, name: e.Name, props: e.Properties})
	}
	relOut := make([]relResult, 0, len(res.Relationships))
	for _, r := range res.Relationships {
		relOut = append(relOut, relResult{from: r.FromID, to: r.ToID, kind: r.Kind, props: r.Properties})
	}
	return out, relOut
}

func pulumiResourceByName(ents []entityResult, name string) *entityResult {
	for i := range ents {
		if ents[i].kind == pulumiResourceKind && ents[i].name == name {
			return &ents[i]
		}
	}
	return nil
}

// TestPulumiTS_MarqueeFixture is the #3528 value-asserting TS test: a file with
// `new aws.s3.Bucket("data")` and a lambda whose args reference `data.arn`
// produces both resources plus a DEPENDS_ON edge from the consumer to data.
func TestPulumiTS_MarqueeFixture(t *testing.T) {
	src := `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const data = new aws.s3.Bucket("data", { versioned: true });

const fn = new aws.lambda.Function("fn", {
    runtime: "nodejs20.x",
    handler: "index.handler",
    environment: { variables: { BUCKET_ARN: data.arn } },
});
`
	ents, rels := runPulumiDetect(t, "typescript", "index.ts", src)

	data := pulumiResourceByName(ents, "data")
	if data == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'data', got %+v", ents)
	}
	if data.props["construct_type"] != "aws.s3.Bucket" {
		t.Errorf("data construct_type = %q, want aws.s3.Bucket", data.props["construct_type"])
	}
	if data.props["iac_tool"] != "pulumi" {
		t.Errorf("data iac_tool = %q, want pulumi", data.props["iac_tool"])
	}
	if data.props["resource_category"] != "storage" {
		t.Errorf("data resource_category = %q, want storage", data.props["resource_category"])
	}
	if data.props["resource_scope"] != "storage" {
		t.Errorf("data resource_scope = %q, want storage", data.props["resource_scope"])
	}

	fn := pulumiResourceByName(ents, "fn")
	if fn == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'fn', got %+v", ents)
	}
	if fn.props["construct_type"] != "aws.lambda.Function" {
		t.Errorf("fn construct_type = %q, want aws.lambda.Function", fn.props["construct_type"])
	}

	deps := relsByKind(rels, "DEPENDS_ON")
	var found *relResult
	for i := range deps {
		if deps[i].from == "SCOPE.InfraResource:fn" && deps[i].to == "SCOPE.InfraResource:data" {
			found = &deps[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected DEPENDS_ON edge fn→data (output_ref), got %+v", deps)
	}
	if found.props["reason"] != "output_ref" {
		t.Errorf("edge reason = %q, want output_ref", found.props["reason"])
	}
}

// TestPulumiTS_DependencyAttribution_Cell is the cell anchor for the
// platform/app_topology `dependency_attribution` capability on
// infra.resource.pulumi (issue #4202). It drives the REAL edge-emitter
// (applyPulumiEdges) and asserts the EXACT explicit-`dependsOn` DEPENDS_ON edge
// together with its attribution properties — iac_tool=pulumi and reason=
// depends_on — proving the attribution metadata the cell is credited for.
func TestPulumiTS_DependencyAttribution_Cell(t *testing.T) {
	src := `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const queue = new aws.sqs.Queue("queue", {});

const worker = new aws.lambda.Function("worker", {
    runtime: "nodejs20.x",
    handler: "h",
}, { dependsOn: [queue] });
`
	_, rels := runPulumiDetect(t, "typescript", "index.ts", src)
	deps := relsByKind(rels, "DEPENDS_ON")
	var found *relResult
	for i := range deps {
		if deps[i].from == "SCOPE.InfraResource:worker" && deps[i].to == "SCOPE.InfraResource:queue" &&
			deps[i].props["reason"] == "depends_on" {
			found = &deps[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected DEPENDS_ON worker→queue (depends_on), got %+v", deps)
	}
	if found.kind != "DEPENDS_ON" {
		t.Errorf("edge kind = %q, want DEPENDS_ON", found.kind)
	}
	if found.props["iac_tool"] != "pulumi" {
		t.Errorf("attribution iac_tool = %q, want pulumi", found.props["iac_tool"])
	}
}

// TestPulumiTS_DependsOnAndStackRef asserts an explicit `dependsOn: [queue]`
// list produces a DEPENDS_ON edge, and a StackReference yields a cross-stack
// node + edge.
func TestPulumiTS_DependsOnAndStackRef(t *testing.T) {
	src := `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const queue = new aws.sqs.Queue("queue", {});

const worker = new aws.lambda.Function("worker", {
    runtime: "nodejs20.x",
    handler: "h",
}, { dependsOn: [queue] });

const upstream = new pulumi.StackReference("acme/network/prod");
`
	ents, rels := runPulumiDetect(t, "typescript", "index.ts", src)

	q := pulumiResourceByName(ents, "queue")
	if q == nil || q.props["resource_scope"] != "queue" {
		t.Fatalf("expected queue resource scope=queue, got %+v", q)
	}

	deps := relsByKind(rels, "DEPENDS_ON")
	var dep, cross bool
	for _, d := range deps {
		if d.from == "SCOPE.InfraResource:worker" && d.to == "SCOPE.InfraResource:queue" &&
			d.props["reason"] == "depends_on" {
			dep = true
		}
	}
	if !dep {
		t.Errorf("expected DEPENDS_ON worker→queue (depends_on), got %+v", deps)
	}

	// Cross-stack node emitted.
	if pulumiResourceByName(ents, "pulumi-stack:acme/network/prod") == nil {
		t.Errorf("expected cross-stack node pulumi-stack:acme/network/prod, got %+v", ents)
	}
	_ = cross
}

// TestPulumiTS_ComponentResource asserts a ComponentResource subclass becomes a
// component-scoped resource node.
func TestPulumiTS_ComponentResource(t *testing.T) {
	src := `import * as pulumi from "@pulumi/pulumi";

export class VpcStack extends pulumi.ComponentResource {
    constructor(name: string) { super("pkg:VpcStack", name); }
}
`
	ents, _ := runPulumiDetect(t, "typescript", "vpc.ts", src)
	c := pulumiResourceByName(ents, "VpcStack")
	if c == nil {
		t.Fatalf("expected component resource VpcStack, got %+v", ents)
	}
	if c.props["resource_scope"] != "component" {
		t.Errorf("VpcStack resource_scope = %q, want component", c.props["resource_scope"])
	}
}

// TestPulumiPy_ComponentResource_StackAppTopology is a value-asserting test
// backing the iac_stack_app_topology (#4200) capability credit for Pulumi
// (Python). A `class X(pulumi.ComponentResource)` subclass is the module/
// component composition boundary; applyPulumiEdgesPython Pass 3 emits it as a
// component-scoped topology entity (resource_scope=component, construct_type=
// pulumi.ComponentResource). Credit is partial: the boundary entity is emitted
// but no ComponentResource→child containment edge is produced.
func TestPulumiPy_ComponentResource_StackAppTopology(t *testing.T) {
	src := `import pulumi

class NetworkComponent(pulumi.ComponentResource):
    def __init__(self, name):
        super().__init__("pkg:NetworkComponent", name)
`
	ents, _ := runPulumiDetect(t, "python", "__main__.py", src)
	c := pulumiResourceByName(ents, "NetworkComponent")
	if c == nil {
		t.Fatalf("expected component-composition entity NetworkComponent, got %+v", ents)
	}
	if c.props["resource_scope"] != "component" {
		t.Errorf("NetworkComponent resource_scope = %q, want component (the module/component boundary node)", c.props["resource_scope"])
	}
	if c.props["construct_type"] != "pulumi.ComponentResource" {
		t.Errorf("NetworkComponent construct_type = %q, want pulumi.ComponentResource", c.props["construct_type"])
	}
}

// TestPulumiPy_MarqueeFixture is the Python value-asserting test: a Bucket
// "data" plus a lambda referencing `data.arn` → both resources + DEPENDS_ON.
func TestPulumiPy_MarqueeFixture(t *testing.T) {
	src := `import pulumi
import pulumi_aws as aws

data = aws.s3.Bucket("data", versioned=True)

fn = aws.lambda_.Function("fn",
    runtime="python3.12",
    handler="index.handler",
    environment={"variables": {"BUCKET_ARN": data.arn}},
)
`
	ents, rels := runPulumiDetect(t, "python", "__main__.py", src)

	data := pulumiResourceByName(ents, "data")
	if data == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'data', got %+v", ents)
	}
	if data.props["construct_type"] != "aws.s3.Bucket" {
		t.Errorf("data construct_type = %q, want aws.s3.Bucket", data.props["construct_type"])
	}
	if data.props["resource_category"] != "storage" {
		t.Errorf("data resource_category = %q, want storage", data.props["resource_category"])
	}
	if data.props["resource_scope"] != "storage" {
		t.Errorf("data resource_scope = %q, want storage", data.props["resource_scope"])
	}

	fn := pulumiResourceByName(ents, "fn")
	if fn == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'fn', got %+v", ents)
	}

	deps := relsByKind(rels, "DEPENDS_ON")
	var ok bool
	for _, d := range deps {
		if d.from == "SCOPE.InfraResource:fn" && d.to == "SCOPE.InfraResource:data" &&
			d.props["reason"] == "output_ref" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("expected DEPENDS_ON fn→data (output_ref), got %+v", deps)
	}
}

// TestPulumiPy_DependsOnOption asserts `opts=pulumi.ResourceOptions(
// depends_on=[queue])` produces a DEPENDS_ON edge.
func TestPulumiPy_DependsOnOption(t *testing.T) {
	src := `import pulumi
import pulumi_aws as aws

queue = aws.sqs.Queue("queue")

worker = aws.lambda_.Function("worker",
    runtime="python3.12",
    handler="h",
    opts=pulumi.ResourceOptions(depends_on=[queue]),
)
`
	ents, rels := runPulumiDetect(t, "python", "__main__.py", src)
	if pulumiResourceByName(ents, "queue") == nil {
		t.Fatalf("expected queue resource, got %+v", ents)
	}
	deps := relsByKind(rels, "DEPENDS_ON")
	var ok bool
	for _, d := range deps {
		if d.from == "SCOPE.InfraResource:worker" && d.to == "SCOPE.InfraResource:queue" &&
			d.props["reason"] == "depends_on" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("expected DEPENDS_ON worker→queue (depends_on), got %+v", deps)
	}
}

// TestPulumi_NonPulumiFileEmitsNothing asserts the import pre-filter gate.
func TestPulumi_NonPulumiFileEmitsNothing(t *testing.T) {
	tsSrc := `const data = new aws.s3.Bucket("data", {});`
	ents, rels := runPulumiDetect(t, "typescript", "index.ts", tsSrc)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("non-Pulumi TS file should emit nothing, got %d ents %d rels", len(ents), len(rels))
	}
	pySrc := `data = aws.s3.Bucket("data")`
	ents, rels = runPulumiDetect(t, "python", "main.py", pySrc)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("non-Pulumi Python file should emit nothing, got %d ents %d rels", len(ents), len(rels))
	}
}

// TestPulumi_GoSupported asserts the Pulumi Go binding (added in #3550) now
// extracts a resource. (This test previously asserted Go was UNSUPPORTED.)
func TestPulumi_GoSupported(t *testing.T) {
	src := `package main
import "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
func main() {
    s3.NewBucket(ctx, "data", &s3.BucketArgs{})
}
`
	ents, _ := runPulumiDetect(t, "go", "main.go", src)
	b := pulumiResourceByName(ents, "data")
	if b == nil {
		t.Fatalf("expected SCOPE.InfraResource 'data', got %+v", ents)
	}
	if b.props["construct_type"] != "s3.Bucket" {
		t.Errorf("data construct_type = %q, want s3.Bucket", b.props["construct_type"])
	}
	if b.props["resource_category"] != "storage" {
		t.Errorf("data resource_category = %q, want storage", b.props["resource_category"])
	}
}

// TestPulumi_UnsupportedLanguageSkipped asserts a language with NO Pulumi
// binding (e.g. Java) is still skipped cleanly.
func TestPulumi_UnsupportedLanguageSkipped(t *testing.T) {
	src := `import com.pulumi.Pulumi;
import com.pulumi.aws.s3.Bucket;
class App {
    static void main(String[] args) {
        var bucket = new Bucket("data");
    }
}
`
	ents, rels := runPulumiDetect(t, "java", "App.java", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("unsupported language should be skipped, got %d ents %d rels", len(ents), len(rels))
	}
}
