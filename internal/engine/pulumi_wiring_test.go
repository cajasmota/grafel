// Value-asserting tests for Pulumi-AWS event-source / trigger wiring edges and
// the AWS resource-property uplift — #5501, stack epic #5479.
//
// These assert the EXACT source→target DEPENDS_ON edge (reason=event_source)
// produced by a wiring resource (EventSourceMapping, BucketNotification,
// Integration, …), the reference resolution (`queue.arn` → the queue resource,
// `fn.name` → the lambda resource), and the curated AWS scalar props newly
// captured on the resource nodes.
package engine

import (
	"testing"
)

func pulumiHasEventSourceEdge(rels []relResult, from, to string) *relResult {
	for i := range rels {
		if rels[i].kind == "DEPENDS_ON" &&
			rels[i].from == "SCOPE.InfraResource:"+from &&
			rels[i].to == "SCOPE.InfraResource:"+to &&
			rels[i].props["reason"] == "event_source" {
			return &rels[i]
		}
	}
	return nil
}

// TestPulumiTS_EventSourceMapping_Wiring is the marquee #5501 fixture: a Lambda
// + SQS queue + EventSourceMapping wiring them yields a source→target
// event_source edge queue→fn, and the resource nodes carry their curated props.
func TestPulumiTS_EventSourceMapping_Wiring(t *testing.T) {
	src := `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const queue = new aws.sqs.Queue("jobs", {
    fifoQueue: true,
    visibilityTimeoutSeconds: 60,
});

const fn = new aws.lambda.Function("worker", {
    runtime: "nodejs20.x",
    handler: "index.handler",
    memorySize: 512,
});

const mapping = new aws.lambda.EventSourceMapping("jobsToWorker", {
    eventSourceArn: queue.arn,
    functionName: fn.name,
    batchSize: 10,
});
`
	ents, rels := runPulumiDetect(t, "typescript", "index.ts", src)

	// --- resource property capture ---
	q := pulumiResourceByName(ents, "jobs")
	if q == nil {
		t.Fatalf("expected SCOPE.InfraResource 'jobs', got %+v", ents)
	}
	if got := q.props["fifoQueue"]; got != "true" {
		t.Errorf("jobs fifoQueue = %q, want true", got)
	}
	if got := q.props["visibilityTimeoutSeconds"]; got != "60" {
		t.Errorf("jobs visibilityTimeoutSeconds = %q, want 60", got)
	}

	fn := pulumiResourceByName(ents, "worker")
	if fn == nil {
		t.Fatalf("expected SCOPE.InfraResource 'worker', got %+v", ents)
	}
	if got := fn.props["runtime"]; got != "nodejs20.x" {
		t.Errorf("worker runtime = %q, want nodejs20.x", got)
	}
	if got := fn.props["handler"]; got != "index.handler" {
		t.Errorf("worker handler = %q, want index.handler", got)
	}
	if got := fn.props["memorySize"]; got != "512" {
		t.Errorf("worker memorySize = %q, want 512", got)
	}

	// --- event-source wiring edge: SOURCE (queue) → TARGET (fn) ---
	e := pulumiHasEventSourceEdge(rels, "jobs", "worker")
	if e == nil {
		t.Fatalf("expected event_source DEPENDS_ON jobs→worker, got %+v", rels)
	}
	if e.props["iac_tool"] != "pulumi" {
		t.Errorf("edge iac_tool = %q, want pulumi", e.props["iac_tool"])
	}
	if e.props["event_source"] != "aws.lambda.EventSourceMapping" {
		t.Errorf("edge detail = %q, want aws.lambda.EventSourceMapping", e.props["event_source"])
	}
}

// TestPulumiTS_BucketNotification_Wiring asserts an S3 BucketNotification wires
// the bucket → the lambda (source→target), including a curated bucket-name prop.
func TestPulumiTS_BucketNotification_Wiring(t *testing.T) {
	src := `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const data = new aws.s3.Bucket("data", { bucket: "my-data-bucket" });

const proc = new aws.lambda.Function("proc", {
    runtime: "nodejs20.x",
    handler: "h.main",
});

const notif = new aws.s3.BucketNotification("dataNotif", {
    bucket: data.id,
    lambdaFunctions: [{ lambdaFunctionArn: proc.arn, events: ["s3:ObjectCreated:*"] }],
});
`
	ents, rels := runPulumiDetect(t, "typescript", "index.ts", src)

	b := pulumiResourceByName(ents, "data")
	if b == nil {
		t.Fatalf("expected bucket 'data', got %+v", ents)
	}
	if got := b.props["bucket"]; got != "my-data-bucket" {
		t.Errorf("data bucket = %q, want my-data-bucket", got)
	}

	if pulumiHasEventSourceEdge(rels, "data", "proc") == nil {
		t.Fatalf("expected event_source DEPENDS_ON data→proc, got %+v", rels)
	}
}

// TestPulumiTS_ApiGatewayIntegration_Wiring asserts an apigatewayv2 Integration
// wires the API → the Lambda target.
func TestPulumiTS_ApiGatewayIntegration_Wiring(t *testing.T) {
	src := `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const api = new aws.apigatewayv2.Api("http", { protocol: "HTTP" });

const fn = new aws.lambda.Function("handler", {
    runtime: "nodejs20.x",
    handler: "h.main",
});

const integ = new aws.apigatewayv2.Integration("httpInteg", {
    apiId: api.id,
    integrationUri: fn.arn,
    integrationType: "AWS_PROXY",
});
`
	_, rels := runPulumiDetect(t, "typescript", "index.ts", src)
	if pulumiHasEventSourceEdge(rels, "http", "handler") == nil {
		t.Fatalf("expected event_source DEPENDS_ON http→handler, got %+v", rels)
	}
}

// TestPulumiTS_DynamoBillingMode asserts DynamoDB billingMode is captured as a
// curated scalar prop.
func TestPulumiTS_DynamoBillingMode(t *testing.T) {
	src := `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const table = new aws.dynamodb.Table("orders", {
    billingMode: "PAY_PER_REQUEST",
    streamViewType: "NEW_AND_OLD_IMAGES",
});
`
	ents, _ := runPulumiDetect(t, "typescript", "index.ts", src)
	tbl := pulumiResourceByName(ents, "orders")
	if tbl == nil {
		t.Fatalf("expected table 'orders', got %+v", ents)
	}
	if got := tbl.props["billingMode"]; got != "PAY_PER_REQUEST" {
		t.Errorf("orders billingMode = %q, want PAY_PER_REQUEST", got)
	}
	if got := tbl.props["streamViewType"]; got != "NEW_AND_OLD_IMAGES" {
		t.Errorf("orders streamViewType = %q, want NEW_AND_OLD_IMAGES", got)
	}
}

// TestPulumiPy_EventSourceMapping_Wiring asserts the Python idiom of an
// EventSourceMapping wires source→target too.
func TestPulumiPy_EventSourceMapping_Wiring(t *testing.T) {
	src := `import pulumi
import pulumi_aws as aws

queue = aws.sqs.Queue("jobs", fifo_queue=True)

fn = aws.lambda_.Function("worker",
    runtime="python3.12",
    handler="index.handler",
)

mapping = aws.lambda_.EventSourceMapping("jobsToWorker",
    event_source_arn=queue.arn,
    function_name=fn.name,
)
`
	ents, rels := runPulumiDetect(t, "python", "__main__.py", src)
	if pulumiResourceByName(ents, "jobs") == nil {
		t.Fatalf("expected queue 'jobs', got %+v", ents)
	}
	if pulumiHasEventSourceEdge(rels, "jobs", "worker") == nil {
		t.Fatalf("expected event_source DEPENDS_ON jobs→worker, got %+v", rels)
	}
}

// TestPulumiTS_NonWiringResourceNoEventSourceEdge guards that an ordinary
// resource (not a wiring type) referencing another resource produces the normal
// output_ref edge, NOT an event_source edge.
func TestPulumiTS_NonWiringResourceNoEventSourceEdge(t *testing.T) {
	src := `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

const data = new aws.s3.Bucket("data", {});

const fn = new aws.lambda.Function("fn", {
    runtime: "nodejs20.x",
    environment: { variables: { BUCKET: data.arn } },
});
`
	_, rels := runPulumiDetect(t, "typescript", "index.ts", src)
	for i := range rels {
		if rels[i].props["reason"] == "event_source" {
			t.Fatalf("non-wiring resource produced event_source edge: %+v", rels[i])
		}
	}
}
