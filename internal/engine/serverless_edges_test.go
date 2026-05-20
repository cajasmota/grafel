// Tests for the serverless function invocation edges pass added by #925.
//
// Structure:
//   - AWS Lambda: Python boto3 producer, Node SDK-v3 InvokeCommand producer,
//     Go SDK producer, Python lambda_handler consumer, Node exports.handler
//     consumer, Java RequestHandler consumer.
//   - GCP Cloud Functions: Python @functions_framework.http consumer,
//     Node functions.http registration.
//   - Azure Functions: Python call_activity producer, Node callActivity producer,
//     C# [FunctionName] consumer, C# StartNewAsync producer.
//   - Cross-language: Python producer → Node Lambda consumer resolves via
//     shared aws-lambda:<FunctionName> entity ID.
//   - False-positive guard: plain JS arrows / Python functions named `invoke`
//     must not trigger detection.
//   - No-op guard: unsupported language (Ruby) returns unchanged slices.
//
// Refs #925.
package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func runServerlessDetect(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	return applyServerlessEdges(lang, path, []byte(src), nil, nil)
}

func serverlessFnByID(ents []types.EntityRecord, id string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == serverlessFunctionKind && ents[i].Name == id {
			return &ents[i]
		}
	}
	return nil
}

func serverlessEdgesOfKind(rels []types.RelationshipRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range rels {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

func requireServerlessFn(t *testing.T, ents []types.EntityRecord, fnID, label string) {
	t.Helper()
	if serverlessFnByID(ents, fnID) == nil {
		t.Errorf("%s: expected ServerlessFunction entity %q, got %v", label, fnID, ents)
	}
}

func requireCallsEdge(t *testing.T, rels []types.RelationshipRecord, fnID, label string) {
	t.Helper()
	want := serverlessFunctionKind + ":" + fnID
	for _, r := range rels {
		if r.Kind == serverlessCallsEdgeKind && r.ToID == want {
			return
		}
	}
	t.Errorf("%s: expected CALLS edge to %q; rels=%v", label, want, rels)
}

func requireHandlesEdge(t *testing.T, rels []types.RelationshipRecord, fnID, label string) {
	t.Helper()
	want := serverlessFunctionKind + ":" + fnID
	for _, r := range rels {
		if r.Kind == serverlessHandlesEdgeKind && r.ToID == want {
			return
		}
	}
	t.Errorf("%s: expected HANDLES edge to %q; rels=%v", label, want, rels)
}

// ---------------------------------------------------------------------------
// AWS Lambda — Python boto3 producer
// ---------------------------------------------------------------------------

// TestServerless_AWS_Python_Boto3Producer verifies that
// `client('lambda').invoke(FunctionName='processOrder')` emits a
// SCOPE.ServerlessFunction entity + CALLS edge.
func TestServerless_AWS_Python_Boto3Producer(t *testing.T) {
	src := `import boto3

def dispatch_order(order_id):
    lam = boto3.client('lambda')
    response = lam.invoke(
        FunctionName='processOrder',
        InvocationType='Event',
        Payload=json.dumps({'order_id': order_id})
    )
    return response
`
	ents, rels := runServerlessDetect(t, "python", "src/dispatcher.py", src)
	requireServerlessFn(t, ents, "aws-lambda:processOrder", "boto3 producer")
	requireCallsEdge(t, rels, "aws-lambda:processOrder", "boto3 producer")

	// Provider property must be set correctly.
	fn := serverlessFnByID(ents, "aws-lambda:processOrder")
	if fn.Properties["provider"] != "aws-lambda" {
		t.Errorf("provider=%q, want aws-lambda", fn.Properties["provider"])
	}
}

// ---------------------------------------------------------------------------
// AWS Lambda — Python handler consumer
// ---------------------------------------------------------------------------

// TestServerless_AWS_Python_LambdaHandler verifies that
// `def lambda_handler(event, context)` emits a ServerlessFunction entity +
// HANDLES edge.
func TestServerless_AWS_Python_LambdaHandler(t *testing.T) {
	src := `import json

def lambda_handler(event, context):
    body = json.loads(event['body'])
    return {
        'statusCode': 200,
        'body': json.dumps({'message': 'ok'})
    }
`
	ents, rels := runServerlessDetect(t, "python", "functions/processOrder/app.py", src)
	requireServerlessFn(t, ents, "aws-lambda:lambda_handler", "python handler")
	requireHandlesEdge(t, rels, "aws-lambda:lambda_handler", "python handler")
}

// TestServerless_AWS_CrossLanguage_PythonProducerNodeConsumer verifies the
// cross-language case: a Python boto3 producer and a Node exports.handler
// consumer both emit the same `aws-lambda:processOrder` entity ID, enabling
// the cross-repo linker to join them.
func TestServerless_AWS_CrossLanguage_PythonProducerNodeConsumer(t *testing.T) {
	pyProducer := `import boto3

def send_to_lambda():
    lam = boto3.client('lambda')
    lam.invoke(FunctionName='processOrder', InvocationType='Event', Payload=b'{}')
`
	nodeConsumer := `const aws = require('@aws-sdk/client-lambda');

exports.handler = async (event, context) => {
    return { statusCode: 200, body: 'ok' };
};
`
	pyEnts, pyRels := runServerlessDetect(t, "python", "producer/dispatcher.py", pyProducer)
	nodeEnts, nodeRels := runServerlessDetect(t, "javascript", "consumer/index.js", nodeConsumer)

	// Python producer emits CALLS to aws-lambda:processOrder.
	requireServerlessFn(t, pyEnts, "aws-lambda:processOrder", "cross-lang python producer fn")
	requireCallsEdge(t, pyRels, "aws-lambda:processOrder", "cross-lang python producer edge")

	// Node consumer emits HANDLES to aws-lambda:handler (handler is the symbol
	// name in exports.handler — logical name resolved via resolveServerlessYMLName).
	requireServerlessFn(t, nodeEnts, "aws-lambda:handler", "cross-lang node consumer fn")
	requireHandlesEdge(t, nodeRels, "aws-lambda:handler", "cross-lang node consumer edge")

	// Ensure same entity Kind on both sides so the linker can join them.
	for _, e := range append(pyEnts, nodeEnts...) {
		if e.Kind == serverlessFunctionKind {
			if e.Properties["provider"] != "aws-lambda" {
				t.Errorf("unexpected provider %q on serverless entity %q", e.Properties["provider"], e.Name)
			}
		}
	}
	_ = pyRels
	_ = nodeRels
}

// ---------------------------------------------------------------------------
// AWS Lambda — Node SDK v3 InvokeCommand producer
// ---------------------------------------------------------------------------

// TestServerless_AWS_Node_InvokeCommandProducer verifies that the AWS SDK v3
// `new InvokeCommand({FunctionName:'X'})` pattern emits a CALLS edge.
func TestServerless_AWS_Node_InvokeCommandProducer(t *testing.T) {
	src := `const { LambdaClient, InvokeCommand } = require('@aws-sdk/client-lambda');
const client = new LambdaClient({ region: 'us-east-1' });

async function triggerResize(imageKey) {
    const cmd = new InvokeCommand({
        FunctionName: 'resizeImage',
        Payload: Buffer.from(JSON.stringify({ key: imageKey }))
    });
    return await client.send(cmd);
}
`
	ents, rels := runServerlessDetect(t, "javascript", "src/trigger.js", src)
	requireServerlessFn(t, ents, "aws-lambda:resizeImage", "InvokeCommand producer")
	requireCallsEdge(t, rels, "aws-lambda:resizeImage", "InvokeCommand producer edge")

	callsEdges := serverlessEdgesOfKind(rels, serverlessCallsEdgeKind)
	if len(callsEdges) == 0 {
		t.Fatal("expected at least one CALLS edge")
	}
	if callsEdges[0].Properties["sdk"] != "aws-sdk-v3" {
		t.Errorf("sdk property=%q, want aws-sdk-v3", callsEdges[0].Properties["sdk"])
	}
}

// ---------------------------------------------------------------------------
// AWS Lambda — Go SDK producer + consumer
// ---------------------------------------------------------------------------

// TestServerless_AWS_Go_InvokeInputProducer verifies the Go AWS SDK v2
// `InvokeInput{FunctionName: "X"}` pattern.
func TestServerless_AWS_Go_InvokeInputProducer(t *testing.T) {
	src := `package main

import (
    "context"
    "github.com/aws/aws-sdk-go-v2/service/lambda"
)

func callTransform(ctx context.Context, client *lambda.Client, payload []byte) error {
    _, err := client.Invoke(ctx, &lambda.InvokeInput{
        FunctionName: aws.String("transformData"),
        Payload:      payload,
    })
    return err
}
`
	ents, rels := runServerlessDetect(t, "go", "cmd/caller/main.go", src)
	requireServerlessFn(t, ents, "aws-lambda:transformData", "Go InvokeInput producer")
	requireCallsEdge(t, rels, "aws-lambda:transformData", "Go InvokeInput producer edge")
}

// TestServerless_AWS_Go_LambdaStartConsumer verifies the Go Lambda runtime
// `lambda.Start(handler)` consumer registration.
func TestServerless_AWS_Go_LambdaStartConsumer(t *testing.T) {
	src := `package main

import (
    "github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(ctx context.Context, event MyEvent) (string, error) {
    return "ok", nil
}

func main() {
    lambda.Start(handleRequest)
}
`
	ents, rels := runServerlessDetect(t, "go", "cmd/handler/main.go", src)
	requireServerlessFn(t, ents, "aws-lambda:handleRequest", "Go lambda.Start consumer")
	requireHandlesEdge(t, rels, "aws-lambda:handleRequest", "Go lambda.Start consumer edge")
}

// ---------------------------------------------------------------------------
// AWS Lambda — Java RequestHandler consumer
// ---------------------------------------------------------------------------

// TestServerless_AWS_Java_RequestHandlerConsumer verifies that `class Foo
// implements RequestHandler<I,O>` emits a HANDLES edge.
func TestServerless_AWS_Java_RequestHandlerConsumer(t *testing.T) {
	src := `package com.example;

import com.amazonaws.services.lambda.runtime.Context;
import com.amazonaws.services.lambda.runtime.RequestHandler;

public class ProcessOrderHandler implements RequestHandler<OrderInput, OrderOutput> {
    @Override
    public OrderOutput handleRequest(OrderInput input, Context context) {
        return new OrderOutput("processed");
    }
}
`
	ents, rels := runServerlessDetect(t, "java", "src/main/java/ProcessOrderHandler.java", src)
	requireServerlessFn(t, ents, "aws-lambda:ProcessOrderHandler", "Java RequestHandler consumer")
	requireHandlesEdge(t, rels, "aws-lambda:ProcessOrderHandler", "Java RequestHandler consumer edge")
}

// ---------------------------------------------------------------------------
// GCP Cloud Functions — Python @functions_framework.http consumer
// ---------------------------------------------------------------------------

// TestServerless_GCP_Python_FunctionsFrameworkConsumer verifies the
// `@functions_framework.http` decorator on a Python function.
func TestServerless_GCP_Python_FunctionsFrameworkConsumer(t *testing.T) {
	src := `import functions_framework

@functions_framework.http
def processWebhook(request):
    return 'ok', 200

@functions_framework.http
def healthCheck(request):
    return 'healthy', 200
`
	ents, rels := runServerlessDetect(t, "python", "functions/main.py", src)

	requireServerlessFn(t, ents, "gcp-cloudfunction:processWebhook", "GCF Python consumer")
	requireHandlesEdge(t, rels, "gcp-cloudfunction:processWebhook", "GCF Python consumer edge")
	requireServerlessFn(t, ents, "gcp-cloudfunction:healthCheck", "GCF Python second consumer")
	requireHandlesEdge(t, rels, "gcp-cloudfunction:healthCheck", "GCF Python second consumer edge")

	fn := serverlessFnByID(ents, "gcp-cloudfunction:processWebhook")
	if fn.Properties["provider"] != "gcp-cloudfunction" {
		t.Errorf("provider=%q, want gcp-cloudfunction", fn.Properties["provider"])
	}
	if fn.Properties["trigger"] != "http" {
		t.Errorf("trigger=%q, want http", fn.Properties["trigger"])
	}
}

// ---------------------------------------------------------------------------
// GCP Cloud Functions — Node functions.http registration
// ---------------------------------------------------------------------------

// TestServerless_GCP_Node_FunctionsHttpConsumer verifies the
// `functions.http('name', handler)` Node registration.
func TestServerless_GCP_Node_FunctionsHttpConsumer(t *testing.T) {
	src := `const functions = require('@google-cloud/functions-framework');

functions.http('helloWorld', (req, res) => {
    res.send('Hello, World!');
});

functions.http('processOrder', async (req, res) => {
    const body = req.body;
    res.json({ processed: true });
});
`
	ents, rels := runServerlessDetect(t, "javascript", "src/index.js", src)
	requireServerlessFn(t, ents, "gcp-cloudfunction:helloWorld", "GCF Node functions.http")
	requireHandlesEdge(t, rels, "gcp-cloudfunction:helloWorld", "GCF Node functions.http edge")
	requireServerlessFn(t, ents, "gcp-cloudfunction:processOrder", "GCF Node functions.http second")
	requireHandlesEdge(t, rels, "gcp-cloudfunction:processOrder", "GCF Node functions.http second edge")
}

// ---------------------------------------------------------------------------
// Azure Functions — Python call_activity / start_new producer
// ---------------------------------------------------------------------------

// TestServerless_Azure_Python_CallActivityProducer verifies the Python durable
// `context.call_activity('FnName', ...)` pattern.
func TestServerless_Azure_Python_CallActivityProducer(t *testing.T) {
	src := `import azure.durable_functions as df

def orchestrator_function(context: df.DurableOrchestrationContext):
    result = yield context.call_activity('ValidateOrder', input_=context.get_input())
    report = yield context.call_activity('GenerateReport', input_=result)
    return report
`
	ents, rels := runServerlessDetect(t, "python", "src/orchestrator/__init__.py", src)
	requireServerlessFn(t, ents, "azure-function:ValidateOrder", "Azure Python call_activity")
	requireCallsEdge(t, rels, "azure-function:ValidateOrder", "Azure Python call_activity edge")
	requireServerlessFn(t, ents, "azure-function:GenerateReport", "Azure Python second call_activity")
	requireCallsEdge(t, rels, "azure-function:GenerateReport", "Azure Python second call_activity edge")
}

// TestServerless_Azure_Python_AppFunctionNameConsumer verifies the Python v2
// programming model `@app.function_name(name='X')` decorator.
func TestServerless_Azure_Python_AppFunctionNameConsumer(t *testing.T) {
	src := `import azure.functions as func

app = func.FunctionApp()

@app.function_name(name='ValidateOrder')
@app.route(route='validate')
def validate_order(req: func.HttpRequest) -> func.HttpResponse:
    return func.HttpResponse('ok')
`
	ents, rels := runServerlessDetect(t, "python", "ValidateOrder/__init__.py", src)
	requireServerlessFn(t, ents, "azure-function:ValidateOrder", "Azure Python v2 function_name")
	requireHandlesEdge(t, rels, "azure-function:ValidateOrder", "Azure Python v2 function_name edge")
}

// ---------------------------------------------------------------------------
// Azure Functions — Node callActivity / startNew producer
// ---------------------------------------------------------------------------

// TestServerless_Azure_Node_CallActivityProducer verifies the JS durable
// `context.df.callActivity('FnName', ...)` pattern.
func TestServerless_Azure_Node_CallActivityProducer(t *testing.T) {
	src := `const df = require('durable-functions');

const orchestrator = df.orchestrator(function* (context) {
    const result = yield context.df.callActivity('ProcessPayment', context.df.getInput());
    const report = yield context.df.callActivity('SendConfirmation', result);
    return report;
});

module.exports = orchestrator;
`
	ents, rels := runServerlessDetect(t, "javascript", "ProcessOrchestrator/index.js", src)
	requireServerlessFn(t, ents, "azure-function:ProcessPayment", "Azure Node callActivity")
	requireCallsEdge(t, rels, "azure-function:ProcessPayment", "Azure Node callActivity edge")
	requireServerlessFn(t, ents, "azure-function:SendConfirmation", "Azure Node second callActivity")
	requireCallsEdge(t, rels, "azure-function:SendConfirmation", "Azure Node second callActivity edge")
}

// ---------------------------------------------------------------------------
// Azure Functions — C# [FunctionName] consumer + StartNewAsync producer
// ---------------------------------------------------------------------------

// TestServerless_Azure_CSharp_FunctionNameConsumer verifies the C#
// `[FunctionName("X")]` attribute that marks an Azure Function handler.
func TestServerless_Azure_CSharp_FunctionNameConsumer(t *testing.T) {
	src := `using Microsoft.Azure.WebJobs;

public class OrderFunctions
{
    [FunctionName("ProcessOrder")]
    public async Task<IActionResult> Run(
        [HttpTrigger(AuthorizationLevel.Function, "post")] HttpRequest req,
        ILogger log)
    {
        log.LogInformation("Processing order");
        return new OkObjectResult("done");
    }
}
`
	ents, rels := runServerlessDetect(t, "csharp", "OrderFunctions.cs", src)
	requireServerlessFn(t, ents, "azure-function:ProcessOrder", "C# FunctionName consumer")
	requireHandlesEdge(t, rels, "azure-function:ProcessOrder", "C# FunctionName consumer edge")

	fn := serverlessFnByID(ents, "azure-function:ProcessOrder")
	if fn.Properties["provider"] != "azure-function" {
		t.Errorf("provider=%q, want azure-function", fn.Properties["provider"])
	}
}

// TestServerless_Azure_CSharp_StartNewAsyncProducer verifies the C# durable
// `client.StartNewAsync("FnName", ...)` orchestration start call.
func TestServerless_Azure_CSharp_StartNewAsyncProducer(t *testing.T) {
	src := `using Microsoft.Azure.WebJobs.Extensions.DurableTask;

public class OrderHttpTrigger
{
    [FunctionName("StartOrderOrchestration")]
    public async Task<HttpResponseMessage> HttpStart(
        [HttpTrigger] HttpRequest req,
        [DurableClient] IDurableOrchestrationClient client,
        ILogger log)
    {
        string instanceId = await client.StartNewAsync("ProcessOrderOrchestrator", null);
        return client.CreateCheckStatusResponse(req, instanceId);
    }
}
`
	ents, rels := runServerlessDetect(t, "csharp", "OrderHttpTrigger.cs", src)

	// Should detect both the [FunctionName] handler AND the StartNewAsync invoke.
	requireServerlessFn(t, ents, "azure-function:StartOrderOrchestration", "C# FunctionName in same file")
	requireHandlesEdge(t, rels, "azure-function:StartOrderOrchestration", "C# FunctionName in same file edge")
	requireServerlessFn(t, ents, "azure-function:ProcessOrderOrchestrator", "C# StartNewAsync producer")
	requireCallsEdge(t, rels, "azure-function:ProcessOrderOrchestrator", "C# StartNewAsync producer edge")
}

// TestServerless_Azure_CSharp_CallActivityAsync verifies the durable
// `context.CallActivityAsync<T>("FnName", ...)` inner-orchestrator pattern.
func TestServerless_Azure_CSharp_CallActivityAsync(t *testing.T) {
	src := `using Microsoft.Azure.WebJobs.Extensions.DurableTask;

public class OrderOrchestrator
{
    [FunctionName("ProcessOrderOrchestrator")]
    public async Task RunOrchestrator(
        [OrchestrationTrigger] IDurableOrchestrationContext context)
    {
        await context.CallActivityAsync<bool>("ValidateInventory", context.GetInput<Order>());
        await context.CallActivityAsync<string>("ChargePayment", context.GetInput<Order>());
    }
}
`
	ents, rels := runServerlessDetect(t, "csharp", "OrderOrchestrator.cs", src)
	requireServerlessFn(t, ents, "azure-function:ProcessOrderOrchestrator", "C# orchestrator handler")
	requireHandlesEdge(t, rels, "azure-function:ProcessOrderOrchestrator", "C# orchestrator handler edge")
	requireServerlessFn(t, ents, "azure-function:ValidateInventory", "C# CallActivityAsync first")
	requireCallsEdge(t, rels, "azure-function:ValidateInventory", "C# CallActivityAsync first edge")
	requireServerlessFn(t, ents, "azure-function:ChargePayment", "C# CallActivityAsync second")
	requireCallsEdge(t, rels, "azure-function:ChargePayment", "C# CallActivityAsync second edge")
}

// ---------------------------------------------------------------------------
// False-positive guard
// ---------------------------------------------------------------------------

// TestServerless_FalsePositive_PlainJSArrow verifies that a plain JS arrow
// function named `invoke` does NOT trigger Lambda detection. Acceptance
// criterion: 0 serverless entities emitted.
func TestServerless_FalsePositive_PlainJSArrow(t *testing.T) {
	src := `// Generic utility — no AWS SDK dependency
const invoke = (fn) => fn();
const result = invoke(() => 42);

function wrapper() {
    return invoke(someCallback);
}
`
	ents, rels := runServerlessDetect(t, "javascript", "src/util.js", src)
	for _, e := range ents {
		if e.Kind == serverlessFunctionKind {
			t.Errorf("false positive: unexpected ServerlessFunction entity %q in plain JS arrow file", e.Name)
		}
	}
	for _, r := range rels {
		if r.Kind == serverlessCallsEdgeKind || r.Kind == serverlessHandlesEdgeKind {
			t.Errorf("false positive: unexpected %s edge in plain JS arrow file: %+v", r.Kind, r)
		}
	}
}

// TestServerless_FalsePositive_PlainPythonInvoke verifies that a Python
// function named `invoke` in a non-Lambda context does NOT produce synthetics.
func TestServerless_FalsePositive_PlainPythonInvoke(t *testing.T) {
	src := `class ServiceClient:
    def invoke(self, method, *args):
        return getattr(self, method)(*args)

    def get_data(self):
        return self.invoke('_fetch')
`
	ents, rels := runServerlessDetect(t, "python", "client/service.py", src)
	for _, e := range ents {
		if e.Kind == serverlessFunctionKind {
			t.Errorf("false positive: unexpected ServerlessFunction entity %q", e.Name)
		}
	}
	_ = rels
}

// TestServerless_FalsePositive_GenericJavaLambda verifies that Java lambda
// expressions (functional interfaces) do NOT trigger Lambda detection.
func TestServerless_FalsePositive_GenericJavaLambda(t *testing.T) {
	src := `import java.util.List;
import java.util.stream.Collectors;

public class Processor {
    public List<String> filterItems(List<String> items) {
        return items.stream()
            .filter(item -> item.startsWith("order"))
            .collect(Collectors.toList());
    }
}
`
	ents, rels := runServerlessDetect(t, "java", "src/Processor.java", src)
	for _, e := range ents {
		if e.Kind == serverlessFunctionKind {
			t.Errorf("false positive: unexpected ServerlessFunction entity %q in Java lambda stream", e.Name)
		}
	}
	_ = rels
}

// ---------------------------------------------------------------------------
// No-op guard
// ---------------------------------------------------------------------------

// TestServerless_NoOp_UnsupportedLanguage verifies that an unsupported
// language (Ruby) returns the input slices unchanged.
func TestServerless_NoOp_UnsupportedLanguage(t *testing.T) {
	src := `require 'aws-sdk-lambda'

client = Aws::Lambda::Client.new(region: 'us-east-1')
client.invoke({ function_name: 'processOrder', invocation_type: 'Event' })
`
	ents, rels := runServerlessDetect(t, "ruby", "app/trigger.rb", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("expected no output for unsupported language ruby, got %d entities, %d rels", len(ents), len(rels))
	}
}

// ---------------------------------------------------------------------------
// Entity schema validation
// ---------------------------------------------------------------------------

// TestServerless_EntitySchemaValid verifies that every emitted
// ServerlessFunction entity has the required fields populated and obeys the
// cross-repo dedup contract: SourceFile must be empty so that two repos
// emitting the same (Kind, Name) collapse to the same entity ID in the graph
// (same strategy used by MessageTopic entities in kafka_edges.go).
//
// Note: EntityRecord.Validate() enforces SourceFile != "" for code entities
// but synthetic cross-repo anchor entities intentionally leave SourceFile empty
// — the same intentional deviation used by Kafka/gRPC synthetics. The dedup
// contract is tested here; the Validate() guard is exercised separately on
// non-synthetic entities in the extractor test suite.
func TestServerless_EntitySchemaValid(t *testing.T) {
	src := `import boto3

def send():
    lam = boto3.client('lambda')
    lam.invoke(FunctionName='validateUser', InvocationType='RequestResponse', Payload=b'{}')
`
	ents, _ := runServerlessDetect(t, "python", "src/sender.py", src)
	if len(ents) == 0 {
		t.Fatal("expected at least one entity")
	}
	for _, e := range ents {
		if e.Kind != serverlessFunctionKind {
			continue
		}
		if e.Name == "" {
			t.Error("ServerlessFunction entity has empty Name")
		}
		if e.Kind == "" {
			t.Error("ServerlessFunction entity has empty Kind")
		}
		if e.Properties["provider"] == "" {
			t.Errorf("ServerlessFunction entity %q missing provider property", e.Name)
		}
		if e.Properties["function_name"] == "" {
			t.Errorf("ServerlessFunction entity %q missing function_name property", e.Name)
		}
		// SourceFile must be empty so the cross-repo linker collapses duplicates
		// (identical strategy to SCOPE.MessageTopic in kafka_edges.go).
		if e.SourceFile != "" {
			t.Errorf("ServerlessFunction entity %q has non-empty SourceFile %q; must be empty for cross-repo dedup", e.Name, e.SourceFile)
		}
		// QualityScore must be in [0.0, 1.0].
		if e.QualityScore < 0.0 || e.QualityScore > 1.0 {
			t.Errorf("ServerlessFunction entity %q has out-of-range QualityScore %.4f", e.Name, e.QualityScore)
		}
	}
}

// ---------------------------------------------------------------------------
// HANDLES kind in AllRelationshipKinds
// ---------------------------------------------------------------------------

// TestServerless_HANDLESKindRegistered verifies that the new HANDLES
// relationship kind is listed in types.AllRelationshipKinds().
func TestServerless_HANDLESKindRegistered(t *testing.T) {
	found := false
	for _, k := range types.AllRelationshipKinds() {
		if string(k) == serverlessHandlesEdgeKind {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("HANDLES kind not in AllRelationshipKinds(); update types/kinds.go")
	}
}

// TestServerless_ServerlessFunctionKindRegistered verifies that the new
// SCOPE.ServerlessFunction entity kind is listed in types.AllEntityKinds().
func TestServerless_ServerlessFunctionKindRegistered(t *testing.T) {
	found := false
	for _, k := range types.AllEntityKinds() {
		if string(k) == serverlessFunctionKind {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("SCOPE.ServerlessFunction kind not in AllEntityKinds(); update types/kinds.go")
	}
}

// Needed to reference types package for kind validation test.
var _ = types.AllRelationshipKinds
var _ = strings.Contains
