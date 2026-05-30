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
		relOut = append(relOut, relResult{from: r.FromID, to: r.ToID, kind: r.Kind, props: r.Properties})
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
	if bucket.props["resource_scope"] != "datastore" {
		t.Errorf("DataBucket resource_scope = %q, want datastore", bucket.props["resource_scope"])
	}

	// Resource: lambda.Function named Handler.
	handler := cdkResourceByName(ents, "Handler")
	if handler == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'Handler', got %+v", ents)
	}
	if handler.props["construct_type"] != "lambda.Function" {
		t.Errorf("Handler construct_type = %q, want lambda.Function", handler.props["construct_type"])
	}
	if handler.props["resource_scope"] != "service" {
		t.Errorf("Handler resource_scope = %q, want service", handler.props["resource_scope"])
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

// TestCDK_NonJSLanguageSkipped asserts non-JS/TS languages are skipped (this PR
// scopes CDK to TypeScript/JavaScript).
func TestCDK_NonJSLanguageSkipped(t *testing.T) {
	src := `from aws_cdk import Stack
class DataStack(Stack):
    def __init__(self, scope, id):
        bucket = s3.Bucket(self, "DataBucket")
`
	ents, rels := runCDKDetect(t, "python", "stack.py", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("non-JS/TS language should be skipped, got %d entities %d rels", len(ents), len(rels))
	}
}
