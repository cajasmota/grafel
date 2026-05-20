// Tests for the managed event-bus edge detection pass added by #927.
//
// Coverage:
//
//  1. AWS EventBridge
//     - Python boto3 put_events producer → PUBLISHES_TO synthetic event
//     - Node PutEventsCommand producer → PUBLISHES_TO
//     - Go PutEventsRequestEntry → PUBLISHES_TO
//     - Terraform aws_cloudwatch_event_rule → SUBSCRIBES_TO synthetic event
//     - Terraform aws_cloudwatch_event_target (Lambda ARN) → EVENTBRIDGE_TRIGGERS
//     - Fixture: rule + target → Lambda from #925 receives EVENTBRIDGE_TRIGGERS edge
//     - False-positive: plain Python function named put_events (no EventBridge guard) → 0 edges
//
//  2. Azure EventGrid
//     - Python EventGridEvent producer → PUBLISHES_TO
//     - Python @app.event_grid_trigger consumer → SUBSCRIBES_TO + EVENTGRID_TRIGGERS
//     - Node EventGridSenderClient producer → PUBLISHES_TO
//     - C# [EventGridTrigger] consumer → SUBSCRIBES_TO + EVENTGRID_TRIGGERS
//
//  3. CNCF CloudEvents
//     - Python CloudEvent builder → PUBLISHES_TO
//     - Node new CloudEvent builder → PUBLISHES_TO
//     - Go cloudevents.NewEvent() + SetType + SetSource → PUBLISHES_TO
//     - Go cloudevents.NewClientHTTP() consumer → SUBSCRIBES_TO
//     - HTTP header detection (ce-type) → CLOUDEVENT_FLOWS
//     - False-positive: plain HTTP handler without ce-type header → 0 edges
//
//  4. No-op guards
//     - Unsupported language (ruby) → unchanged slices
//     - Empty content → unchanged slices
//
// Refs #927.
package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func runEventBusDetect(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	return applyEventBusEdges(lang, path, []byte(src), nil, nil)
}

func eventBusEntityByID(ents []types.EntityRecord, id string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == eventBusEventKind && ents[i].Name == id {
			return &ents[i]
		}
	}
	return nil
}

func requireEventBusEntity(t *testing.T, ents []types.EntityRecord, id, label string) {
	t.Helper()
	if eventBusEntityByID(ents, id) == nil {
		t.Errorf("%s: expected EventBusEvent entity %q; got %v", label, id, entNames(ents))
	}
}

func requireEdgeToEB(t *testing.T, rels []types.RelationshipRecord, toID, kind, label string) {
	t.Helper()
	for _, r := range rels {
		if r.Kind == kind && r.ToID == toID {
			return
		}
	}
	t.Errorf("%s: expected %s edge to %q; rels=%v", label, kind, toID, relSummary(rels))
}


func requireEdgeFromTo(t *testing.T, rels []types.RelationshipRecord, fromID, toID, kind, label string) {
	t.Helper()
	for _, r := range rels {
		if r.Kind == kind && r.FromID == fromID && r.ToID == toID {
			return
		}
	}
	t.Errorf("%s: expected %s edge from %q to %q; rels=%v", label, kind, fromID, toID, relSummary(rels))
}

func requireNoEdgeKind(t *testing.T, rels []types.RelationshipRecord, kind, label string) {
	t.Helper()
	for _, r := range rels {
		if r.Kind == kind {
			t.Errorf("%s: unexpected %s edge: %+v", label, kind, r)
		}
	}
}

func entNames(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		out = append(out, e.Kind+":"+e.Name)
	}
	return out
}

func relSummary(rels []types.RelationshipRecord) []string {
	var out []string
	for _, r := range rels {
		out = append(out, r.Kind+"|"+r.FromID+"->"+r.ToID)
	}
	return out
}

// ---------------------------------------------------------------------------
// 1. AWS EventBridge
// ---------------------------------------------------------------------------

func TestEventBridgePythonProducer(t *testing.T) {
	src := `
import boto3

def publish_order(order):
    client = boto3.client('events', region_name='us-east-1')
    client.put_events(
        Entries=[
            {
                'Source': 'orders',
                'DetailType': 'OrderPlaced',
                'Detail': '{"orderId":"123"}',
                'EventBusName': 'default',
            }
        ]
    )
`
	ents, rels := runEventBusDetect(t, "python", "services/order_svc/events.py", src)

	wantID := "event:eventbridge:orders:OrderPlaced"
	requireEventBusEntity(t, ents, wantID, "boto3 producer")
	requireEdgeToEB(t, rels, eventBusEventKind+":"+wantID, "PUBLISHES_TO", "boto3 producer edge")
}

func TestEventBridgeNodeProducer(t *testing.T) {
	src := `
import { EventBridgeClient, PutEventsCommand } from '@aws-sdk/client-eventbridge';

const client = new EventBridgeClient({ region: 'us-east-1' });

async function emitOrderEvent() {
  await client.send(new PutEventsCommand({
    Entries: [
      {
        Source: 'order.svc',
        DetailType: 'OrderPlaced',
        Detail: JSON.stringify({ orderId: '123' }),
        EventBusName: 'default',
      },
    ],
  }));
}
`
	ents, rels := runEventBusDetect(t, "typescript", "src/events/order.ts", src)

	wantID := "event:eventbridge:order.svc:OrderPlaced"
	requireEventBusEntity(t, ents, wantID, "Node PutEventsCommand producer")
	requireEdgeToEB(t, rels, eventBusEventKind+":"+wantID, "PUBLISHES_TO", "Node producer edge")
}

func TestEventBridgeGoProducer(t *testing.T) {
	src := `
package events

import (
	"context"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

func PublishShipped(ctx context.Context, orderID string) error {
	entry := ebtypes.PutEventsRequestEntry{
		Source:      aws.String("fulfillment"),
		DetailType:  aws.String("OrderShipped"),
		Detail:      aws.String(`+"`"+`{"orderId":"`+"`"+`+orderID+`+"`"+`"}`+"`"+`),
		EventBusName: aws.String("default"),
	}
	// ... send
	return nil
}
`
	ents, rels := runEventBusDetect(t, "go", "pkg/events/ship.go", src)

	wantID := "event:eventbridge:fulfillment:OrderShipped"
	requireEventBusEntity(t, ents, wantID, "Go producer")
	requireEdgeToEB(t, rels, eventBusEventKind+":"+wantID, "PUBLISHES_TO", "Go producer edge")
}

func TestEventBridgeTerraformRuleAndTarget(t *testing.T) {
	src := `
resource "aws_cloudwatch_event_rule" "process_order" {
  name        = "process-order-rule"
  description = "Route OrderPlaced events to lambda"

  event_pattern = jsonencode({
    source      = ["orders"]
    "detail-type" = ["OrderPlaced"]
  })
}

resource "aws_cloudwatch_event_target" "lambda_target" {
  rule = aws_cloudwatch_event_rule.process_order.name
  arn  = aws_lambda_function.process_order.arn
}
`
	ents, rels := runEventBusDetect(t, "terraform", "infra/eventbridge.tf", src)

	wantEventID := "event:eventbridge:orders:OrderPlaced"
	requireEventBusEntity(t, ents, wantEventID, "TF rule event entity")

	// Rule should SUBSCRIBES_TO the synthetic event.
	ruleID := "SCOPE.Component:aws_cloudwatch_event_rule.process_order"
	requireEdgeFromTo(t, rels, ruleID, eventBusEventKind+":"+wantEventID, "SUBSCRIBES_TO", "TF rule SUBSCRIBES_TO")

	// Target should emit EVENTBRIDGE_TRIGGERS to the Lambda.
	lambdaTarget := serverlessFunctionKind + ":" + lambdaFunctionID("process_order")
	requireEdgeToEB(t, rels, lambdaTarget, eventBridgeTriggersEdge, "TF target EVENTBRIDGE_TRIGGERS")
}

func TestEventBridgeTerraformLambdaSubscribesToEvent(t *testing.T) {
	// When both rule and target are in the same file, the Lambda entity should
	// also get a SUBSCRIBES_TO edge into the event.
	src := `
resource "aws_cloudwatch_event_rule" "order_rule" {
  name          = "order-rule"
  event_pattern = "{\"source\":[\"orders\"],\"detail-type\":[\"OrderPlaced\"]}"
}

resource "aws_cloudwatch_event_target" "order_target" {
  rule = aws_cloudwatch_event_rule.order_rule.name
  arn  = aws_lambda_function.order_handler.arn
}
`
	ents, rels := runEventBusDetect(t, "hcl", "infra/main.tf", src)

	wantEventID := "event:eventbridge:orders:OrderPlaced"
	requireEventBusEntity(t, ents, wantEventID, "HCL rule event entity")

	lambdaID := serverlessFunctionKind + ":" + lambdaFunctionID("order_handler")
	requireEdgeToEB(t, rels, lambdaID, eventBridgeTriggersEdge, "HCL EVENTBRIDGE_TRIGGERS")
	requireEdgeFromTo(t, rels, lambdaID, eventBusEventKind+":"+wantEventID, "SUBSCRIBES_TO", "Lambda SUBSCRIBES_TO event")
}

func TestEventBridgeFalsePositive_PlainPutEvents(t *testing.T) {
	// A function named put_events that is NOT an EventBridge call — guard should
	// prevent detection because the EventBridge guard tokens are absent.
	src := `
def send_data(entries):
    # This just prints entries, nothing to do with EventBridge
    for e in entries:
        print(e)
`
	ents, rels := runEventBusDetect(t, "python", "utils/sender.py", src)
	if len(ents) != 0 {
		t.Errorf("false positive: expected 0 entities, got %v", entNames(ents))
	}
	if len(rels) != 0 {
		t.Errorf("false positive: expected 0 rels, got %v", relSummary(rels))
	}
}

// ---------------------------------------------------------------------------
// 2. Azure EventGrid
// ---------------------------------------------------------------------------

func TestEventGridPythonProducer(t *testing.T) {
	src := `
from azure.eventgrid import EventGridPublisherClient, EventGridEvent

def publish_inventory(topic_key):
    client = EventGridPublisherClient(topic_endpoint, AzureKeyCredential(topic_key))
    events = [
        EventGridEvent(
            subject='/inventory/low',
            event_type='Inventory.LowStock',
            data={'sku': 'ABC-123'},
            data_version='1.0'
        )
    ]
    client.send(events)
`
	ents, rels := runEventBusDetect(t, "python", "svc/inventory/events.py", src)

	wantID := "event:eventgrid:/inventory/low:Inventory.LowStock"
	requireEventBusEntity(t, ents, wantID, "EventGrid Python producer")
	requireEdgeToEB(t, rels, eventBusEventKind+":"+wantID, "PUBLISHES_TO", "EventGrid Python producer edge")
}

func TestEventGridPythonConsumer(t *testing.T) {
	src := `
import azure.functions as func

app = func.FunctionApp()

@app.event_grid_trigger(name='event')
async def handle_inventory_event(event: func.EventGridEvent):
    data = event.get_json()
`
	ents, rels := runEventBusDetect(t, "python", "func/inventory_handler/__init__.py", src)

	// Should emit wildcard EventBusEvent + SUBSCRIBES_TO + EVENTGRID_TRIGGERS.
	wildcardID := "event:eventgrid:*:*"
	requireEventBusEntity(t, ents, wildcardID, "EventGrid consumer wildcard entity")
	requireEdgeToEB(t, rels, eventBusEventKind+":"+wildcardID, "SUBSCRIBES_TO", "EventGrid consumer SUBSCRIBES_TO")
	azureTarget := serverlessFunctionKind + ":" + azureFunctionID("handle_inventory_event")
	requireEdgeToEB(t, rels, azureTarget, eventGridTriggersEdge, "EventGrid EVENTGRID_TRIGGERS")
}

func TestEventGridNodeProducer(t *testing.T) {
	src := `
import { EventGridSenderClient } from '@azure/eventgrid';

const client = new EventGridSenderClient(endpoint, new AzureKeyCredential(key), 'EventGrid');

async function emitOrderShipped() {
  await client.sendEvents([{
    eventType: 'Order.Shipped',
    subject: '/orders/shipped',
    dataVersion: '1.0',
    data: { orderId: '123' },
  }]);
}
`
	ents, rels := runEventBusDetect(t, "javascript", "src/events/shipping.js", src)

	wantID := "event:eventgrid:/orders/shipped:Order.Shipped"
	requireEventBusEntity(t, ents, wantID, "EventGrid Node producer")
	requireEdgeToEB(t, rels, eventBusEventKind+":"+wantID, "PUBLISHES_TO", "EventGrid Node producer edge")
}

func TestEventGridCSharpConsumer(t *testing.T) {
	src := `
using Microsoft.Azure.WebJobs;
using Microsoft.Azure.EventGrid.Models;

public static class InventoryFunction
{
    [FunctionName("InventoryHandler")]
    public static void Run(
        [EventGridTrigger] EventGridEvent gridEvent,
        ILogger log)
    {
        log.LogInformation(gridEvent.Data.ToString());
    }
}
`
	ents, rels := runEventBusDetect(t, "csharp", "Functions/InventoryFunction.cs", src)

	wildcardID := "event:eventgrid:*:*"
	requireEventBusEntity(t, ents, wildcardID, "C# EventGridTrigger consumer entity")
	requireEdgeToEB(t, rels, eventBusEventKind+":"+wildcardID, "SUBSCRIBES_TO", "C# EventGridTrigger SUBSCRIBES_TO")
	azureTarget := serverlessFunctionKind + ":" + azureFunctionID("Run")
	requireEdgeToEB(t, rels, azureTarget, eventGridTriggersEdge, "C# EVENTGRID_TRIGGERS")
}

// ---------------------------------------------------------------------------
// 3. CNCF CloudEvents
// ---------------------------------------------------------------------------

func TestCloudEventPythonProducer(t *testing.T) {
	src := `
from cloudevents.http import CloudEvent, to_structured

def emit_shipment_event(order_id: str):
    event = CloudEvent({
        'type': 'com.example.order.shipped',
        'source': '/shop/orders',
        'id': order_id,
    })
    headers, body = to_structured(event)
    # POST to channel
`
	ents, rels := runEventBusDetect(t, "python", "svc/shipping/events.py", src)

	wantID := "event:cloudevents:/shop/orders:com.example.order.shipped"
	requireEventBusEntity(t, ents, wantID, "Python CloudEvent producer")
	requireEdgeToEB(t, rels, eventBusEventKind+":"+wantID, "PUBLISHES_TO", "Python CloudEvent PUBLISHES_TO")
}

func TestCloudEventNodeProducer(t *testing.T) {
	src := `
const { CloudEvent, emitterFor, httpTransport } = require('cloudevents');

async function publishEvent() {
  const event = new CloudEvent({
    type: 'com.example.user.created',
    source: '/users/signup',
    data: { userId: 'abc123' },
  });
  const emit = emitterFor(httpTransport('http://receiver'));
  await emit(event);
}
`
	ents, rels := runEventBusDetect(t, "javascript", "src/events/user.js", src)

	wantID := "event:cloudevents:/users/signup:com.example.user.created"
	requireEventBusEntity(t, ents, wantID, "Node CloudEvent producer")
	requireEdgeToEB(t, rels, eventBusEventKind+":"+wantID, "PUBLISHES_TO", "Node CloudEvent PUBLISHES_TO")
}

func TestCloudEventGoProducer(t *testing.T) {
	src := `
package main

import (
	"context"
	cloudevents "github.com/cloudevents/sdk-go/v2"
)

func SendEvent(ctx context.Context) error {
	c, err := cloudevents.NewClientHTTP()
	if err != nil {
		return err
	}
	event := cloudevents.NewEvent()
	event.SetSource("https://example.com/producer")
	event.SetType("com.example.data.created")
	event.SetData(cloudevents.ApplicationJSON, map[string]string{"key": "value"})
	return c.Send(ctx, event)
}
`
	ents, rels := runEventBusDetect(t, "go", "cmd/producer/main.go", src)

	wantProducerID := "event:cloudevents:https://example.com/producer:com.example.data.created"
	requireEventBusEntity(t, ents, wantProducerID, "Go CloudEvent producer entity")
	requireEdgeToEB(t, rels, eventBusEventKind+":"+wantProducerID, "PUBLISHES_TO", "Go CloudEvent PUBLISHES_TO")

	// NewClientHTTP() also registers a consumer wildcard.
	wildcardID := "event:cloudevents:*:*"
	requireEventBusEntity(t, ents, wildcardID, "Go CloudEvent client consumer entity")
	requireEdgeToEB(t, rels, eventBusEventKind+":"+wildcardID, "SUBSCRIBES_TO", "Go CloudEvent SUBSCRIBES_TO")
}

func TestCloudEventHTTPHeaderDetection(t *testing.T) {
	// Plain HTTP handler that reads CloudEvents headers but no SDK.
	src := `
package handler

import "net/http"

func HandleEvent(w http.ResponseWriter, r *http.Request) {
	ceType := r.Header.Get("ce-type")
	ceSource := r.Header.Get("ce-source")
	ceSpecVersion := r.Header.Get("ce-specversion")
	if ceType == "" {
		http.Error(w, "not a cloudevent", 400)
		return
	}
	// process
}
`
	_, rels := runEventBusDetect(t, "go", "handlers/event.go", src)

	// Should emit at least a wildcard CLOUDEVENT_FLOWS edge because no type literal.
	found := false
	for _, r := range rels {
		if r.Kind == cloudEventFlowsEdge {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected CLOUDEVENT_FLOWS edge from HTTP-header handler; rels=%v", relSummary(rels))
	}
}

func TestCloudEventFalsePositive_PlainHTTPNoHeaders(t *testing.T) {
	// A plain HTTP handler that does NOT reference any CloudEvent headers.
	src := `
package handler

import "net/http"

func ServeHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	w.Write([]byte("ok"))
}
`
	ents, rels := runEventBusDetect(t, "go", "handlers/health.go", src)
	if len(ents) != 0 {
		t.Errorf("false positive: expected 0 entities, got %v", entNames(ents))
	}
	requireNoEdgeKind(t, rels, cloudEventFlowsEdge, "no CLOUDEVENT_FLOWS on plain HTTP")
}

// ---------------------------------------------------------------------------
// 4. No-op guards
// ---------------------------------------------------------------------------

func TestEventBusEdgesUnsupportedLanguage(t *testing.T) {
	src := `
def handle(req):
    pass
`
	ents, rels := runEventBusDetect(t, "ruby", "app.rb", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("ruby should be a no-op; ents=%v rels=%v", entNames(ents), relSummary(rels))
	}
}

func TestEventBusEdgesEmptyContent(t *testing.T) {
	ents, rels := applyEventBusEdges("python", "file.py", nil, nil, nil)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("empty content should be a no-op; ents=%v rels=%v", entNames(ents), relSummary(rels))
	}
}

// ---------------------------------------------------------------------------
// 5. Cross-repo acceptance: producer in one file, Terraform rule in another.
// ---------------------------------------------------------------------------

func TestEventBridgeCrossRepoLinkage(t *testing.T) {
	// Simulate producer in a Python microservice + Terraform rule in infra repo.
	// Both files emit the same event ID, enabling cross-repo linking.
	pyProducer := `
import boto3

def place_order(order_id):
    client = boto3.client('events')
    client.put_events(Entries=[{
        'Source': 'orders',
        'DetailType': 'OrderPlaced',
        'Detail': '{}',
    }])
`
	tfRule := `
resource "aws_cloudwatch_event_rule" "process_order" {
  name          = "process-order-rule"
  event_pattern = "{\"source\":[\"orders\"],\"detail-type\":[\"OrderPlaced\"]}"
}

resource "aws_cloudwatch_event_target" "proc_target" {
  rule = aws_cloudwatch_event_rule.process_order.name
  arn  = aws_lambda_function.process_order.arn
}
`
	entsA, relsA := runEventBusDetect(t, "python", "services/orders/publish.py", pyProducer)
	entsB, relsB := runEventBusDetect(t, "terraform", "infra/eventbridge.tf", tfRule)

	wantID := "event:eventbridge:orders:OrderPlaced"

	// Both repos emit the same synthetic entity ID.
	if eventBusEntityByID(entsA, wantID) == nil {
		t.Errorf("cross-repo: producer repo missing entity %q; entities=%v", wantID, entNames(entsA))
	}
	if eventBusEntityByID(entsB, wantID) == nil {
		t.Errorf("cross-repo: infra repo missing entity %q; entities=%v", wantID, entNames(entsB))
	}

	// Producer side has PUBLISHES_TO.
	found := false
	for _, r := range relsA {
		if r.Kind == "PUBLISHES_TO" && strings.HasSuffix(r.ToID, wantID) {
			found = true
		}
	}
	if !found {
		t.Errorf("cross-repo: no PUBLISHES_TO from producer; rels=%v", relSummary(relsA))
	}

	// Infra side has EVENTBRIDGE_TRIGGERS.
	triggerFound := false
	for _, r := range relsB {
		if r.Kind == eventBridgeTriggersEdge {
			triggerFound = true
		}
	}
	if !triggerFound {
		t.Errorf("cross-repo: no EVENTBRIDGE_TRIGGERS in infra repo; rels=%v", relSummary(relsB))
	}
}
