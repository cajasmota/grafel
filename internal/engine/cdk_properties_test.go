// Value-asserting tests for CDK curated scalar property stamping (epic #4194,
// iac_resource_property_extraction). These assert the EXACT stamped key=value
// pairs on the InfraResource entity, the negatives (enum/ref/call-valued props
// are NOT stamped as scalars), and a regression guard that the existing
// props_ref DEPENDS_ON edge mining still fires unchanged.
package engine

import (
	"testing"
)

// TestCDK_ScalarProps_TS asserts curated literal scalar props on a TS
// lambda.Function are stamped with exact values, while enum/call-valued props
// (runtime: lambda.Runtime.X, timeout: Duration.seconds(30)) are NOT stamped as
// scalars (they remain reference-bearing and out of scope here).
func TestCDK_ScalarProps_TS(t *testing.T) {
	src := `import * as cdk from 'aws-cdk-lib';
import * as lambda from 'aws-cdk-lib/aws-lambda';

export class FnStack extends cdk.Stack {
  constructor(scope: cdk.App, id: string) {
    super(scope, id);
    const handler = new lambda.Function(this, 'Handler', {
      memorySize: 512,
      timeout: Duration.seconds(30),
      runtime: lambda.Runtime.NODEJS_20_X,
      description: 'a handler',
    });
  }
}
`
	ents, _ := runCDKDetect(t, "typescript", "lib/fn-stack.ts", src)
	r := cdkResourceByName(ents, "Handler")
	if r == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'Handler', got %+v", ents)
	}
	// memorySize=512 — a clean numeric literal, IS stamped.
	if got := r.props["memorySize"]; got != "512" {
		t.Errorf("Handler memorySize = %q, want 512", got)
	}
	// timeout=Duration.seconds(30) — a call expression, NOT a clean scalar.
	if got, ok := r.props["timeout"]; ok {
		t.Errorf("Handler timeout stamped as scalar = %q, want NOT stamped (call expr)", got)
	}
	// runtime=lambda.Runtime.NODEJS_20_X — an enum member ref, NOT stamped.
	if got, ok := r.props["runtime"]; ok {
		t.Errorf("Handler runtime stamped as scalar = %q, want NOT stamped (enum ref)", got)
	}
	// description — not in the curated allow-list, never stamped.
	if _, ok := r.props["description"]; ok {
		t.Errorf("Handler description stamped, want NOT stamped (not curated)")
	}
}

// TestCDK_ScalarProps_TS_StringAndBool asserts quoted-string and bool curated
// scalars are stamped with exact values.
func TestCDK_ScalarProps_TS_StringAndBool(t *testing.T) {
	src := `import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';

export class DbStack extends cdk.Stack {
  constructor(scope: cdk.App, id: string) {
    super(scope, id);
    const db = new rds.DatabaseInstance(this, 'Db', {
      engine: 'postgres',
      engineVersion: '15.3',
      allocatedStorage: 100,
      port: 5432,
    });
  }
}
`
	ents, _ := runCDKDetect(t, "typescript", "lib/db-stack.ts", src)
	r := cdkResourceByName(ents, "Db")
	if r == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'Db', got %+v", ents)
	}
	checks := map[string]string{
		"engine":           "postgres",
		"engineVersion":    "15.3",
		"allocatedStorage": "100",
		"port":             "5432",
	}
	for k, want := range checks {
		if got := r.props[k]; got != want {
			t.Errorf("Db %s = %q, want %q", k, got, want)
		}
	}
}

// TestCDK_ScalarProps_Python asserts Python snake_case curated kwargs are
// stamped with exact values, while enum/call-valued kwargs are NOT.
func TestCDK_ScalarProps_Python(t *testing.T) {
	src := `from aws_cdk import aws_lambda as _lambda, Duration
from constructs import Construct

class FnStack(Stack):
    def __init__(self, scope: Construct, id: str) -> None:
        super().__init__(scope, id)
        handler = _lambda.Function(self, "Handler",
            memory_size=512,
            engine="aurora",
            runtime=_lambda.Runtime.PYTHON_3_12,
        )
`
	ents, _ := runCDKDetect(t, "python", "stacks/fn_stack.py", src)
	r := cdkResourceByName(ents, "Handler")
	if r == nil {
		t.Fatalf("expected SCOPE.InfraResource named 'Handler', got %+v", ents)
	}
	if got := r.props["memory_size"]; got != "512" {
		t.Errorf("Handler memory_size = %q, want 512", got)
	}
	if got := r.props["engine"]; got != "aurora" {
		t.Errorf("Handler engine = %q, want aurora", got)
	}
	if got, ok := r.props["runtime"]; ok {
		t.Errorf("Handler runtime stamped = %q, want NOT stamped (enum ref)", got)
	}
}

// TestCDK_ScalarProps_NoRegressionPropsRefEdge guards that stamping scalar
// props does NOT regress the existing props_ref DEPENDS_ON edge mining: a
// construct variable passed into another construct's props still produces the
// enclosing→passed DEPENDS_ON edge.
func TestCDK_ScalarProps_NoRegressionPropsRefEdge(t *testing.T) {
	src := `import * as cdk from 'aws-cdk-lib';
import * as s3 from 'aws-cdk-lib/aws-s3';
import * as lambda from 'aws-cdk-lib/aws-lambda';

export class AppStack extends cdk.Stack {
  constructor(scope: cdk.App, id: string) {
    super(scope, id);
    const dataBucket = new s3.Bucket(this, 'DataBucket', { versioned: true });
    const handler = new lambda.Function(this, 'Handler', {
      memorySize: 256,
      bucket: dataBucket,
    });
  }
}
`
	ents, rels := runCDKDetect(t, "typescript", "lib/app-stack.ts", src)

	// Scalar stamping still happened.
	h := cdkResourceByName(ents, "Handler")
	if h == nil || h.props["memorySize"] != "256" {
		t.Fatalf("expected Handler memorySize=256, got %+v", h)
	}

	// props_ref edge Handler→DataBucket still fires.
	deps := relsByKind(rels, "DEPENDS_ON")
	var found *relResult
	for i := range deps {
		if deps[i].from == "SCOPE.InfraResource:Handler" &&
			deps[i].to == "SCOPE.InfraResource:DataBucket" &&
			deps[i].props["reason"] == "props_ref" {
			found = &deps[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected props_ref DEPENDS_ON Handler→DataBucket, got %+v", deps)
	}
	if found.props["props_ref"] != "dataBucket" {
		t.Errorf("props_ref detail = %q, want dataBucket", found.props["props_ref"])
	}
}
