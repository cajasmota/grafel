// Value-asserting tests for the AWS CDK (TypeScript) resource + dependency
// extraction pass (cdk_edges.go) — part of #3512.
//
// The marquee fixture is a Stack with:
//
//	const dataBucket = new s3.Bucket(this, 'DataBucket', { versioned: true });
//	const handler    = new lambda.Function(this, 'Handler', { ... });
//	dataBucket.grantRead(handler);
//
// We assert the EXACT extracted shape (not len>0):
//   - a SCOPE.InfraResource named 'DataBucket' with construct_type=s3.Bucket
//   - a SCOPE.InfraResource named 'Handler'    with construct_type=lambda.Function
//   - a DEPENDS_ON edge Handler → DataBucket (the grantRead grant)
package engine

import (
	"testing"
)

// runCDKDetect is a lightweight in-process driver mirroring runBullMQDetect.
func runCDKDetect(t *testing.T, lang, path, src string) ([]entityResult, []relResult) {
	t.Helper()
	res := applyCDKEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	out := make([]entityResult, 0, len(res.Entities))
	for _, e := range res.Entities {
		out = append(out, entityResult{kind: e.Kind, name: e.Name, props: e.Properties})
	}
	relOut := make([]relResult, 0, len(res.Relationships))
	for _, r := range res.Relationships {
		relOut = append(relOut, relResult{from: r.FromID, to: r.ToID, kind: r.Kind, props: r.Properties.Snapshot()})
	}
	return out, relOut
}

// cdkResourceByName returns the SCOPE.InfraResource entity with the given
// LogicalId name, or nil.
func cdkResourceByName(ents []entityResult, logicalID string) *entityResult {
	for i := range ents {
		if ents[i].kind == cdkResourceKind && ents[i].name == logicalID {
			return &ents[i]
		}
	}
	return nil
}

// TestCDK_MarqueeFixture_BucketLambdaGrant is the core value-asserting test
// from the #3512 task: the two constructs are emitted by LogicalId and the
// grantRead produces a DEPENDS_ON edge from the grantee (Handler) to the
// granted resource (DataBucket).
func TestCDK_MarqueeFixture_BucketLambdaGrant(t *testing.T) {
	src := `import * as cdk from 'aws-cdk-lib';
import * as s3 from 'aws-cdk-lib/aws-s3';
import * as lambda from 'aws-cdk-lib/aws-lambda';
import { Construct } from 'constructs';

export class DataStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    const dataBucket = new s3.Bucket(this, 'DataBucket', { versioned: true });

    const handler = new lambda.Function(this, 'Handler', {
      runtime: lambda.Runtime.NODEJS_20_X,
      handler: 'index.handler',
      code: lambda.Code.fromAsset('lambda'),
    });

    dataBucket.grantRead(handler);
  }
}
`
	ents, rels := runCDKDetect(t, "typescript", "lib/data-stack.ts", src)

	// Resource: s3.Bucket named DataBucket.
	bucket := cdkResourceByName(ents, "DataBucket")
	if bucket == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'DataBucket', got %+v", ents)
	}
	if bucket.props["construct_type"] != "s3.Bucket" {
		t.Errorf("DataBucket construct_type = %q, want s3.Bucket", bucket.props["construct_type"])
	}
	if bucket.props["iac_tool"] != "aws-cdk" {
		t.Errorf("DataBucket iac_tool = %q, want aws-cdk", bucket.props["iac_tool"])
	}
	// #3549 — uniform cross-tool category. s3.Bucket → "storage" (more precise
	// than the old coarse "datastore"); resource_scope aliases resource_category.
	if bucket.props["resource_category"] != "storage" {
		t.Errorf("DataBucket resource_category = %q, want storage", bucket.props["resource_category"])
	}
	if bucket.props["resource_scope"] != "storage" {
		t.Errorf("DataBucket resource_scope = %q, want storage", bucket.props["resource_scope"])
	}

	// Resource: lambda.Function named Handler.
	handler := cdkResourceByName(ents, "Handler")
	if handler == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'Handler', got %+v", ents)
	}
	if handler.props["construct_type"] != "lambda.Function" {
		t.Errorf("Handler construct_type = %q, want lambda.Function", handler.props["construct_type"])
	}
	// lambda.Function → "function" (was the coarse "service").
	if handler.props["resource_category"] != "function" {
		t.Errorf("Handler resource_category = %q, want function", handler.props["resource_category"])
	}
	if handler.props["resource_scope"] != "function" {
		t.Errorf("Handler resource_scope = %q, want function", handler.props["resource_scope"])
	}

	// Dependency edge: Handler --DEPENDS_ON--> DataBucket (the grantRead grant).
	deps := relsByKind(rels, "DEPENDS_ON")
	var found *relResult
	for i := range deps {
		if deps[i].from == "SCOPE.InfraResource:Handler" && deps[i].to == "SCOPE.InfraResource:DataBucket" {
			found = &deps[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected DEPENDS_ON edge Handler→DataBucket, got %+v", deps)
	}
	if found.props["reason"] != "grant" || found.props["grant"] != "grantRead" {
		t.Errorf("grant edge props = %+v, want reason=grant grant=grantRead", found.props)
	}
}

// TestCDK_L1CfnConstruct asserts L1 escape-hatch constructs
// (`new CfnBucket(this,'id',…)`) are extracted as resources.
func TestCDK_L1CfnConstruct(t *testing.T) {
	src := `import * as cdk from 'aws-cdk-lib';
import { CfnBucket } from 'aws-cdk-lib/aws-s3';

export class RawStack extends cdk.Stack {
  constructor(scope: cdk.App, id: string) {
    super(scope, id);
    const raw = new CfnBucket(this, 'RawBucket', { bucketName: 'raw' });
  }
}
`
	ents, _ := runCDKDetect(t, "typescript", "lib/raw-stack.ts", src)
	r := cdkResourceByName(ents, "RawBucket")
	if r == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'RawBucket', got %+v", ents)
	}
	if r.props["construct_type"] != "CfnBucket" {
		t.Errorf("RawBucket construct_type = %q, want CfnBucket", r.props["construct_type"])
	}
}

// TestCDK_AddEventSource asserts `fn.addEventSource(new SqsEventSource(queue))`
// produces a DEPENDS_ON edge from the function to the queue.
func TestCDK_AddEventSource(t *testing.T) {
	src := `import * as cdk from 'aws-cdk-lib';
import * as sqs from 'aws-cdk-lib/aws-sqs';
import * as lambda from 'aws-cdk-lib/aws-lambda';
import { SqsEventSource } from 'aws-cdk-lib/aws-lambda-event-sources';

export class WorkerStack extends cdk.Stack {
  constructor(scope: cdk.App, id: string) {
    super(scope, id);
    const jobs = new sqs.Queue(this, 'Jobs', {});
    const worker = new lambda.Function(this, 'Worker', { runtime: 'x', handler: 'h', code: 'c' });
    worker.addEventSource(new SqsEventSource(jobs));
  }
}
`
	ents, rels := runCDKDetect(t, "typescript", "lib/worker-stack.ts", src)

	if cdkResourceByName(ents, "Jobs") == nil {
		t.Fatalf("expected SCOPE.InfraResource 'Jobs', got %+v", ents)
	}
	q := cdkResourceByName(ents, "Jobs")
	if q.props["resource_scope"] != "queue" {
		t.Errorf("Jobs resource_scope = %q, want queue", q.props["resource_scope"])
	}
	deps := relsByKind(rels, "DEPENDS_ON")
	var ok bool
	for _, d := range deps {
		if d.from == "SCOPE.InfraResource:Worker" && d.to == "SCOPE.InfraResource:Jobs" &&
			d.props["reason"] == "event_source" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("expected DEPENDS_ON Worker→Jobs (event_source), got %+v", deps)
	}
}

// TestCDK_PropsRef asserts a construct variable passed into another construct's
// props (`{ bucket: dataBucket }`) yields a DEPENDS_ON edge.
func TestCDK_PropsRef(t *testing.T) {
	src := `import * as cdk from 'aws-cdk-lib';
import * as s3 from 'aws-cdk-lib/aws-s3';
import * as lambda from 'aws-cdk-lib/aws-lambda';

export class AppStack extends cdk.Stack {
  constructor(scope: cdk.App, id: string) {
    super(scope, id);
    const dataBucket = new s3.Bucket(this, 'DataBucket', {});
    const fn = new lambda.Function(this, 'Api', {
      runtime: 'x',
      handler: 'h',
      code: 'c',
      environment: { BUCKET: dataBucket.bucketName },
      bucket: dataBucket,
    });
  }
}
`
	_, rels := runCDKDetect(t, "typescript", "lib/app-stack.ts", src)
	deps := relsByKind(rels, "DEPENDS_ON")
	var ok bool
	for _, d := range deps {
		if d.from == "SCOPE.InfraResource:Api" && d.to == "SCOPE.InfraResource:DataBucket" &&
			d.props["reason"] == "props_ref" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("expected DEPENDS_ON Api→DataBucket (props_ref), got %+v", deps)
	}
}

// TestCDK_DependencyAttribution_Cell is the cell anchor for the
// platform/app_topology `dependency_attribution` capability on
// infra.resource.aws-cdk (issue #4202). It drives the REAL edge-emitter
// (applyCDKEdges) and asserts the EXACT grant DEPENDS_ON edge together with its
// attribution properties — iac_tool=aws-cdk, reason=grant, and the grant method
// recorded under the `grant` key — proving the attribution metadata the cell is
// credited for, not merely that an edge exists.
func TestCDK_DependencyAttribution_Cell(t *testing.T) {
	src := `import * as cdk from 'aws-cdk-lib';
import * as s3 from 'aws-cdk-lib/aws-s3';
import * as lambda from 'aws-cdk-lib/aws-lambda';

export class AttribStack extends cdk.Stack {
  constructor(scope: cdk.App, id: string) {
    super(scope, id);
    const dataBucket = new s3.Bucket(this, 'DataBucket', {});
    const reader = new lambda.Function(this, 'Reader', { runtime: 'x', handler: 'h', code: 'c' });
    dataBucket.grantReadWrite(reader);
  }
}
`
	_, rels := runCDKDetect(t, "typescript", "lib/attrib-stack.ts", src)
	deps := relsByKind(rels, "DEPENDS_ON")
	var found *relResult
	for i := range deps {
		if deps[i].from == "SCOPE.InfraResource:Reader" && deps[i].to == "SCOPE.InfraResource:DataBucket" {
			found = &deps[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected DEPENDS_ON Reader→DataBucket, got %+v", deps)
	}
	if found.kind != "DEPENDS_ON" {
		t.Errorf("edge kind = %q, want DEPENDS_ON", found.kind)
	}
	if found.props["iac_tool"] != "aws-cdk" {
		t.Errorf("attribution iac_tool = %q, want aws-cdk", found.props["iac_tool"])
	}
	if found.props["reason"] != "grant" {
		t.Errorf("attribution reason = %q, want grant", found.props["reason"])
	}
	if found.props["grant"] != "grantReadWrite" {
		t.Errorf("attribution grant = %q, want grantReadWrite", found.props["grant"])
	}
}

// TestCDK_NonCDKFileEmitsNothing asserts the pre-filter gate: a plain TS file
// with a `new X(this,'id',…)` idiom but no CDK import emits nothing.
func TestCDK_NonCDKFileEmitsNothing(t *testing.T) {
	src := `class Widget {
  constructor() {
    const child = new SomeThing(this, 'Foo', {});
  }
}
`
	ents, rels := runCDKDetect(t, "typescript", "widget.ts", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("non-CDK file should emit nothing, got %d entities %d rels", len(ents), len(rels))
	}
}

// TestCDK_JavaBuilderFormSupported asserts the Java CDK binding (added in #3550)
// extracts a builder-form construct. (This test previously asserted Java was
// UNSUPPORTED; the JVM/Go/.NET bindings now implement it, so it asserts the
// extracted shape instead.) A bare `Bucket` construct type is aliased to
// `s3.Bucket` so the shared classifier yields the "storage" category.
func TestCDK_JavaBuilderFormSupported(t *testing.T) {
	src := `import software.amazon.awscdk.Stack;
class DataStack extends Stack {
    DataStack() {
        Bucket bucket = Bucket.Builder.create(this, "DataBucket").build();
    }
}
`
	ents, _ := runCDKDetect(t, "java", "DataStack.java", src)
	b := cdkResourceByName(ents, "DataBucket")
	if b == nil {
		t.Fatalf("expected SCOPE.InfraResource 'DataBucket', got %+v", ents)
	}
	if b.props["construct_type"] != "s3.Bucket" {
		t.Errorf("DataBucket construct_type = %q, want s3.Bucket", b.props["construct_type"])
	}
	if b.props["resource_category"] != "storage" {
		t.Errorf("DataBucket resource_category = %q, want storage", b.props["resource_category"])
	}
}

// TestCDK_UnsupportedLanguageSkipped asserts a language with NO CDK binding
// (e.g. Ruby) is still skipped cleanly.
func TestCDK_UnsupportedLanguageSkipped(t *testing.T) {
	src := `require "aws-cdk-lib"
bucket = Bucket.new(self, "DataBucket")
`
	ents, rels := runCDKDetect(t, "ruby", "data_stack.rb", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("unsupported language should be skipped, got %d entities %d rels", len(ents), len(rels))
	}
}

// TestCDKPython_MarqueeFixture is the #3528 value-asserting test: a CDK-Python
// Stack with a Bucket 'Data', a Function 'Fn', and a grant_read produces both
// resources plus a Fn→Data DEPENDS_ON edge.
func TestCDKPython_MarqueeFixture(t *testing.T) {
	src := `from aws_cdk import Stack
from aws_cdk import aws_s3 as s3
from aws_cdk import aws_lambda as _lambda
from constructs import Construct


class DataStack(Stack):
    def __init__(self, scope: Construct, construct_id: str, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        data = s3.Bucket(self, "Data", versioned=True)

        fn = _lambda.Function(
            self, "Fn",
            runtime=_lambda.Runtime.PYTHON_3_12,
            handler="index.handler",
            code=_lambda.Code.from_asset("lambda"),
        )

        data.grant_read(fn)
`
	ents, rels := runCDKDetect(t, "python", "stacks/data_stack.py", src)

	data := cdkResourceByName(ents, "Data")
	if data == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'Data', got %+v", ents)
	}
	if data.props["construct_type"] != "s3.Bucket" {
		t.Errorf("Data construct_type = %q, want s3.Bucket", data.props["construct_type"])
	}
	if data.props["iac_tool"] != "aws-cdk" {
		t.Errorf("Data iac_tool = %q, want aws-cdk", data.props["iac_tool"])
	}
	if data.props["resource_category"] != "storage" {
		t.Errorf("Data resource_category = %q, want storage", data.props["resource_category"])
	}
	if data.props["resource_scope"] != "storage" {
		t.Errorf("Data resource_scope = %q, want storage", data.props["resource_scope"])
	}

	fn := cdkResourceByName(ents, "Fn")
	if fn == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'Fn', got %+v", ents)
	}
	if fn.props["construct_type"] != "_lambda.Function" {
		t.Errorf("Fn construct_type = %q, want _lambda.Function", fn.props["construct_type"])
	}

	deps := relsByKind(rels, "DEPENDS_ON")
	var found *relResult
	for i := range deps {
		if deps[i].from == "SCOPE.InfraResource:Fn" && deps[i].to == "SCOPE.InfraResource:Data" {
			found = &deps[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected DEPENDS_ON edge Fn→Data (grant_read), got %+v", deps)
	}
	if found.props["reason"] != "grant" || found.props["grant"] != "grant_read" {
		t.Errorf("grant edge props = %+v, want reason=grant grant=grant_read", found.props)
	}
}

// TestCDKPython_AddEventSource asserts `fn.add_event_source(SqsEventSource(q))`
// produces a DEPENDS_ON edge from the function to the queue.
func TestCDKPython_AddEventSource(t *testing.T) {
	src := `from aws_cdk import Stack
from aws_cdk import aws_sqs as sqs
from aws_cdk import aws_lambda as _lambda
from aws_cdk.aws_lambda_event_sources import SqsEventSource


class WorkerStack(Stack):
    def __init__(self, scope, construct_id):
        super().__init__(scope, construct_id)
        jobs = sqs.Queue(self, "Jobs")
        worker = _lambda.Function(self, "Worker", runtime="x", handler="h", code="c")
        worker.add_event_source(SqsEventSource(jobs))
`
	ents, rels := runCDKDetect(t, "python", "stacks/worker.py", src)
	q := cdkResourceByName(ents, "Jobs")
	if q == nil {
		t.Fatalf("expected SCOPE.InfraResource 'Jobs', got %+v", ents)
	}
	if q.props["resource_scope"] != "queue" {
		t.Errorf("Jobs resource_scope = %q, want queue", q.props["resource_scope"])
	}
	deps := relsByKind(rels, "DEPENDS_ON")
	var ok bool
	for _, d := range deps {
		if d.from == "SCOPE.InfraResource:Worker" && d.to == "SCOPE.InfraResource:Jobs" &&
			d.props["reason"] == "event_source" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("expected DEPENDS_ON Worker→Jobs (event_source), got %+v", deps)
	}
}

// TestCDKPython_KwargRef asserts a construct passed as a keyword argument
// (`bucket=data`) yields a props_ref DEPENDS_ON edge.
func TestCDKPython_KwargRef(t *testing.T) {
	src := `from aws_cdk import Stack
from aws_cdk import aws_s3 as s3
from aws_cdk import aws_lambda as _lambda


class AppStack(Stack):
    def __init__(self, scope, construct_id):
        super().__init__(scope, construct_id)
        data = s3.Bucket(self, "Data")
        fn = _lambda.Function(self, "Api", runtime="x", handler="h", code="c", bucket=data)
`
	_, rels := runCDKDetect(t, "python", "stacks/app.py", src)
	deps := relsByKind(rels, "DEPENDS_ON")
	var ok bool
	for _, d := range deps {
		if d.from == "SCOPE.InfraResource:Api" && d.to == "SCOPE.InfraResource:Data" &&
			d.props["reason"] == "props_ref" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("expected DEPENDS_ON Api→Data (props_ref), got %+v", deps)
	}
}

// TestCDKPython_NonCDKFileEmitsNothing asserts the pre-filter gate: a plain
// Python file with a `X(self, "id")` idiom but no CDK import emits nothing.
func TestCDKPython_NonCDKFileEmitsNothing(t *testing.T) {
	src := `class Widget:
    def __init__(self):
        child = SomeThing(self, "Foo", enabled=True)
`
	ents, rels := runCDKDetect(t, "python", "widget.py", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("non-CDK Python file should emit nothing, got %d entities %d rels", len(ents), len(rels))
	}
}

// TestCDK_IAMGrantAttribution_Cell is the value-asserting test backing the
// platform/iac_provisioning `iac_iam_grant_attribution` capability cell
// (#4197). It drives the REAL grant pass (applyCDKEdges Pass 3) on a CDK-TS
// snippet with `bucket.grantRead(fn)` and asserts the EXACT IAM-grant
// attribution edge: a DEPENDS_ON whose From is the grantee (Handler), whose
// To is the granted target resource (DataBucket), carrying reason=grant and
// the precise grant method under the `grant` key (grantRead). This is the
// "which principal/grantee is granted which action on which target" datum the
// cell claims — pinned exactly, never len>0.
func TestCDK_IAMGrantAttribution_Cell(t *testing.T) {
	src := `import * as s3 from 'aws-cdk-lib/aws-s3';
import * as lambda from 'aws-cdk-lib/aws-lambda';

export class GrantStack extends cdk.Stack {
  constructor(scope: Construct, id: string) {
    super(scope, id);
    const dataBucket = new s3.Bucket(this, 'DataBucket', {});
    const handler    = new lambda.Function(this, 'Handler', {});
    dataBucket.grantRead(handler);
  }
}
`
	_, rels := runCDKDetect(t, "typescript", "lib/grant-stack.ts", src)

	deps := relsByKind(rels, "DEPENDS_ON")
	var grant *relResult
	for i := range deps {
		if deps[i].from == "SCOPE.InfraResource:Handler" && deps[i].to == "SCOPE.InfraResource:DataBucket" {
			grant = &deps[i]
			break
		}
	}
	if grant == nil {
		t.Fatalf("expected IAM-grant DEPENDS_ON edge grantee Handler→target DataBucket, got %+v", deps)
	}
	if grant.props["reason"] != "grant" {
		t.Errorf("grant attribution reason = %q, want grant", grant.props["reason"])
	}
	if grant.props["grant"] != "grantRead" {
		t.Errorf("grant attribution method = %q, want grantRead", grant.props["grant"])
	}
	if grant.props["iac_tool"] != "aws-cdk" {
		t.Errorf("grant attribution iac_tool = %q, want aws-cdk", grant.props["iac_tool"])
	}
}

// TestCDK_EventSourceWiring_Cell pins the iac_event_source_wiring capability
// (#4198). It drives the REAL event-source pass (applyCDKEdges Pass 4) on a
// CDK-TS snippet with `worker.addEventSource(new SqsEventSource(jobs))` and
// asserts the EXACT event-source wiring edge: a DEPENDS_ON whose From is the
// triggered function (Worker), whose To is the event source (Jobs), carrying
// reason=event_source and iac_tool=aws-cdk. This is the "which event source
// invokes which function" datum the cell claims — pinned exactly, never len>0.
func TestCDK_EventSourceWiring_Cell(t *testing.T) {
	src := `import * as sqs from 'aws-cdk-lib/aws-sqs';
import * as lambda from 'aws-cdk-lib/aws-lambda';
import { SqsEventSource } from 'aws-cdk-lib/aws-lambda-event-sources';

export class WireStack extends cdk.Stack {
  constructor(scope: Construct, id: string) {
    super(scope, id);
    const jobs   = new sqs.Queue(this, 'Jobs', {});
    const worker = new lambda.Function(this, 'Worker', {});
    worker.addEventSource(new SqsEventSource(jobs));
  }
}
`
	_, rels := runCDKDetect(t, "typescript", "lib/wire-stack.ts", src)

	deps := relsByKind(rels, "DEPENDS_ON")
	var wire *relResult
	for i := range deps {
		if deps[i].from == "SCOPE.InfraResource:Worker" && deps[i].to == "SCOPE.InfraResource:Jobs" {
			wire = &deps[i]
			break
		}
	}
	if wire == nil {
		t.Fatalf("expected event-source DEPENDS_ON edge fn Worker→source Jobs, got %+v", deps)
	}
	if wire.props["reason"] != "event_source" {
		t.Errorf("event-source wiring reason = %q, want event_source", wire.props["reason"])
	}
	if wire.props["iac_tool"] != "aws-cdk" {
		t.Errorf("event-source wiring iac_tool = %q, want aws-cdk", wire.props["iac_tool"])
	}
}

// TestCDKPython_EventSourceWiring_Cell mirrors the cell assertion on CDK-Python
// (applyCDKEdgesPython Pass 4): `worker.add_event_source(SqsEventSource(jobs))`
// emits the DEPENDS_ON Worker→Jobs edge with reason=event_source.
func TestCDKPython_EventSourceWiring_Cell(t *testing.T) {
	src := `from aws_cdk import aws_sqs as sqs, aws_lambda as lambda_
from aws_cdk.aws_lambda_event_sources import SqsEventSource

class WireStack(Stack):
    def __init__(self, scope, id):
        super().__init__(scope, id)
        jobs = sqs.Queue(self, "Jobs")
        worker = lambda_.Function(self, "Worker")
        worker.add_event_source(SqsEventSource(jobs))
`
	_, rels := runCDKDetect(t, "python", "stacks/wire_stack.py", src)

	deps := relsByKind(rels, "DEPENDS_ON")
	var ok bool
	for _, d := range deps {
		if d.from == "SCOPE.InfraResource:Worker" && d.to == "SCOPE.InfraResource:Jobs" &&
			d.props["reason"] == "event_source" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("expected event-source DEPENDS_ON Worker→Jobs (reason=event_source), got %+v", deps)
	}
}
