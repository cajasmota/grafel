// Tests for the workflow orchestration edges pass — #934.
//
// Coverage:
//   - Temporal Python: @workflow.defn class, @activity.defn, start_workflow trigger,
//     execute_activity cross-file chain
//   - Temporal Go: RegisterWorkflow / RegisterActivity, ExecuteWorkflow trigger,
//     ExecuteActivity within-workflow edge
//   - Cadence Java: @WorkflowInterface / @ActivityInterface, newWorkflowStub trigger
//   - Temporal TypeScript: proxyActivities, client.start
//   - AWS Step Functions ASL: state machine entity, Task → Lambda STEPFUNCTION_STEP_INVOKES
//   - AWS Step Functions Terraform: aws_sfn_state_machine resource
//   - SFN invocation (Python / Go / Node): STARTS_WORKFLOW edge
//   - False-positive guards: plain `workflow.run()` without Temporal import does not trigger;
//     plain Go `w.run()` without Temporal guard does not trigger
//   - No-op: unsupported language (Ruby) returns unchanged slices
//
// Refs #934.
package engine

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func runWorkflowEdges(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	res := applyWorkflowEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

func runSFNStartEdges(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	res := applySFNStartExecutionEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

func workflowEntityByID(ents []types.EntityRecord, kind, id string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Name == id {
			return &ents[i]
		}
	}
	return nil
}

func wfEdgesOfKind(rels []types.RelationshipRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range rels {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

func requireEntityKind(t *testing.T, ents []types.EntityRecord, kind, id, label string) {
	t.Helper()
	if workflowEntityByID(ents, kind, id) == nil {
		var names []string
		for _, e := range ents {
			names = append(names, e.Kind+":"+e.Name)
		}
		t.Errorf("%s: expected %s entity %q; got %v", label, kind, id, names)
	}
}

func requireWorkflowEdgeTo(t *testing.T, rels []types.RelationshipRecord, edgeKind, toID, label string) {
	t.Helper()
	for _, r := range rels {
		if r.Kind == edgeKind && r.ToID == toID {
			return
		}
	}
	var targets []string
	for _, r := range rels {
		if r.Kind == edgeKind {
			targets = append(targets, r.ToID)
		}
	}
	t.Errorf("%s: expected %s edge to %q; got %v", label, edgeKind, toID, targets)
}

func requireEdge(t *testing.T, rels []types.RelationshipRecord, edgeKind, fromID, toID, label string) {
	t.Helper()
	for _, r := range rels {
		if r.Kind == edgeKind && r.FromID == fromID && r.ToID == toID {
			return
		}
	}
	t.Errorf("%s: expected %s edge %s → %s", label, edgeKind, fromID, toID)
}

// ---------------------------------------------------------------------------
// Temporal Python
// ---------------------------------------------------------------------------

const pyTemporalWorkflowSrc = `
import asyncio
from temporalio import workflow, activity
from temporalio.client import Client

@activity.defn
async def charge_card(amount: float) -> str:
    return "charged"

@activity.defn(name="send_receipt")
async def send_receipt_email(email: str) -> None:
    pass

@workflow.defn
class OrderWorkflow:
    @workflow.run
    async def run(self, order_id: str) -> str:
        result = await workflow.execute_activity(charge_card, schedule_to_close_timeout=timedelta(seconds=10))
        await workflow.execute_activity(send_receipt_email, schedule_to_close_timeout=timedelta(seconds=5))
        return result

async def main():
    client = await Client.connect("temporal:7233")
    handle = await client.start_workflow(OrderWorkflow.run, "order-123", id="order-123", task_queue="orders")
`

func TestTemporalPythonWorkflowDefinition(t *testing.T) {
	ents, rels := runWorkflowEdges(t, "python", "workflows/order.py", pyTemporalWorkflowSrc)

	requireEntityKind(t, ents, workflowKind, temporalWorkflowID("OrderWorkflow"), "workflow entity")
	requireEntityKind(t, ents, activityKind, temporalActivityID("charge_card"), "charge_card activity")
	requireEntityKind(t, ents, activityKind, temporalActivityID("send_receipt"), "send_receipt activity (custom name)")

	// STARTS_WORKFLOW: main → OrderWorkflow
	requireWorkflowEdgeTo(t, rels, startsWorkflowEdgeKind,
		workflowKind+":"+temporalWorkflowID("OrderWorkflow"), "starts_workflow from main")

	// EXECUTES_ACTIVITY: OrderWorkflow → charge_card
	requireEdge(t, rels, executesActivityEdgeKind,
		workflowKind+":"+temporalWorkflowID("OrderWorkflow"),
		activityKind+":"+temporalActivityID("charge_card"),
		"workflow executes charge_card")
}

func TestTemporalPythonCrossFileActivityCall(t *testing.T) {
	// Simulates a cross-file scenario: workflow body only (no @activity.defn in this file).
	// The execute_activity call should still emit the activity synthetic + EXECUTES_ACTIVITY edge.
	src := `
from temporalio import workflow
from activities import process_payment

@workflow.defn
class PaymentWorkflow:
    @workflow.run
    async def run(self):
        await workflow.execute_activity(process_payment, schedule_to_close_timeout=timedelta(seconds=30))
`
	ents, rels := runWorkflowEdges(t, "python", "workflows/payment.py", src)

	requireEntityKind(t, ents, workflowKind, temporalWorkflowID("PaymentWorkflow"), "PaymentWorkflow entity")
	requireEntityKind(t, ents, activityKind, temporalActivityID("process_payment"), "process_payment synthetic")
	requireEdge(t, rels, executesActivityEdgeKind,
		workflowKind+":"+temporalWorkflowID("PaymentWorkflow"),
		activityKind+":"+temporalActivityID("process_payment"),
		"cross-file workflow→activity edge")
}

// ---------------------------------------------------------------------------
// Temporal Go
// ---------------------------------------------------------------------------

const goTemporalSrc = `
package main

import (
	"context"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func OrderWorkflow(ctx workflow.Context, orderID string) (string, error) {
	var result string
	err := workflow.ExecuteActivity(ctx, ChargeCard, orderID).Get(ctx, &result)
	return result, err
}

func ChargeCard(ctx context.Context, orderID string) (string, error) {
	return "charged", nil
}

func main() {
	c, _ := client.Dial(client.Options{})
	defer c.Close()

	w := worker.New(c, "orders", worker.Options{})
	w.RegisterWorkflow(OrderWorkflow)
	w.RegisterActivity(ChargeCard)

	_ = c.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{TaskQueue: "orders"}, OrderWorkflow, "order-1")
}
`

func TestTemporalGoWorkflowRegistration(t *testing.T) {
	ents, _ := runWorkflowEdges(t, "go", "worker/main.go", goTemporalSrc)

	requireEntityKind(t, ents, workflowKind, temporalWorkflowID("OrderWorkflow"), "OrderWorkflow entity")
	requireEntityKind(t, ents, activityKind, temporalActivityID("ChargeCard"), "ChargeCard activity")
}

func TestTemporalGoExecuteWorkflow(t *testing.T) {
	ents, rels := runWorkflowEdges(t, "go", "worker/main.go", goTemporalSrc)

	// STARTS_WORKFLOW from main → OrderWorkflow
	requireWorkflowEdgeTo(t, rels, startsWorkflowEdgeKind,
		workflowKind+":"+temporalWorkflowID("OrderWorkflow"), "ExecuteWorkflow triggers STARTS_WORKFLOW")
	_ = ents
}

func TestTemporalGoExecuteActivity(t *testing.T) {
	ents, rels := runWorkflowEdges(t, "go", "worker/main.go", goTemporalSrc)

	// EXECUTES_ACTIVITY from OrderWorkflow → ChargeCard
	requireEdge(t, rels, executesActivityEdgeKind,
		workflowKind+":"+temporalWorkflowID("OrderWorkflow"),
		activityKind+":"+temporalActivityID("ChargeCard"),
		"OrderWorkflow executes ChargeCard")
	_ = ents
}

// ---------------------------------------------------------------------------
// Cadence Java
// ---------------------------------------------------------------------------

const javaTemporalSrc = `
package com.example;

import io.temporal.workflow.WorkflowInterface;
import io.temporal.workflow.WorkflowMethod;
import io.temporal.activity.ActivityInterface;
import io.temporal.activity.ActivityMethod;
import io.temporal.client.WorkflowClient;

@WorkflowInterface
public interface OrderWorkflow {
    @WorkflowMethod
    String processOrder(String orderId);
}

@ActivityInterface
public interface PaymentActivities {
    @ActivityMethod
    String chargeCard(String orderId);
}

public class StarterMain {
    public static void main(String[] args) {
        WorkflowClient client = WorkflowClient.newInstance(service);
        OrderWorkflow wf = client.newWorkflowStub(OrderWorkflow.class, options);
        WorkflowClient.start(wf::processOrder, orderId);
    }
}
`

func TestJavaTemporalWorkflowInterface(t *testing.T) {
	ents, rels := runWorkflowEdges(t, "java", "src/StarterMain.java", javaTemporalSrc)

	requireEntityKind(t, ents, workflowKind, temporalWorkflowID("OrderWorkflow"), "OrderWorkflow entity")
	requireEntityKind(t, ents, activityKind, temporalActivityID("PaymentActivities"), "PaymentActivities entity")

	// STARTS_WORKFLOW via newWorkflowStub(OrderWorkflow.class)
	requireWorkflowEdgeTo(t, rels, startsWorkflowEdgeKind,
		workflowKind+":"+temporalWorkflowID("OrderWorkflow"), "java STARTS_WORKFLOW via newWorkflowStub")
	_ = rels
}

const javaCadenceSrc = `
package com.example;

import com.uber.cadence.workflow.WorkflowInterface;
import com.uber.cadence.workflow.WorkflowMethod;
import com.uber.cadence.activity.ActivityInterface;

@WorkflowInterface
public interface OrderCadenceWF {
    @WorkflowMethod
    void execute(String orderId);
}

@ActivityInterface
public interface ShipmentActivities {
    void shipOrder(String orderId);
}
`

func TestCadenceJavaInterface(t *testing.T) {
	ents, _ := runWorkflowEdges(t, "java", "src/OrderCadenceWF.java", javaCadenceSrc)

	requireEntityKind(t, ents, workflowKind, temporalWorkflowID("OrderCadenceWF"), "Cadence workflow entity")
	requireEntityKind(t, ents, activityKind, temporalActivityID("ShipmentActivities"), "Cadence activity entity")

	// Verify the engine property is "cadence".
	for _, e := range ents {
		if e.Kind == workflowKind && e.Name == temporalWorkflowID("OrderCadenceWF") {
			if e.Properties["workflow_engine"] != "cadence" {
				t.Errorf("expected workflow_engine=cadence, got %q", e.Properties["workflow_engine"])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Temporal TypeScript
// ---------------------------------------------------------------------------

const tsWorkflowSrc = `
import { proxyActivities } from '@temporalio/workflow';
import type * as activities from './activities';

const { chargeCard, sendReceipt } = proxyActivities<typeof activities>({
  startToCloseTimeout: '1 minute',
});

export async function orderWorkflow(orderId: string): Promise<string> {
  const result = await chargeCard(orderId);
  await sendReceipt(orderId);
  return result;
}
`

const tsClientSrc = `
import { Client } from '@temporalio/client';
import { orderWorkflow } from './workflows';

async function run() {
  const client = new Client();
  const handle = await client.start(orderWorkflow, {
    taskQueue: 'orders',
    workflowId: 'order-1',
    args: ['order-1'],
  });
}
`

func TestTemporalTypeScriptWorkflow(t *testing.T) {
	ents, rels := runWorkflowEdges(t, "typescript", "src/workflows/order.ts", tsWorkflowSrc)

	requireEntityKind(t, ents, workflowKind, temporalWorkflowID("orderWorkflow"), "TS workflow entity")
	requireEntityKind(t, ents, activityKind, temporalActivityID("chargeCard"), "chargeCard activity")
	requireEntityKind(t, ents, activityKind, temporalActivityID("sendReceipt"), "sendReceipt activity")

	// EXECUTES_ACTIVITY edges from orderWorkflow.
	requireEdge(t, rels, executesActivityEdgeKind,
		workflowKind+":"+temporalWorkflowID("orderWorkflow"),
		activityKind+":"+temporalActivityID("chargeCard"),
		"TS workflow→chargeCard")
}

func TestTemporalTypeScriptClientStart(t *testing.T) {
	ents, rels := runWorkflowEdges(t, "typescript", "src/client.ts", tsClientSrc)

	requireWorkflowEdgeTo(t, rels, startsWorkflowEdgeKind,
		workflowKind+":"+temporalWorkflowID("orderWorkflow"), "TS client.start STARTS_WORKFLOW")
	_ = ents
}

// ---------------------------------------------------------------------------
// AWS Step Functions — ASL JSON
// ---------------------------------------------------------------------------

const aslJSON = `{
  "Comment": "Order processing state machine",
  "StartAt": "ChargeCustomer",
  "States": {
    "ChargeCustomer": {
      "Type": "Task",
      "Resource": "arn:aws:lambda:us-east-1:123456789012:function:charge-card",
      "Next": "SendReceipt"
    },
    "SendReceipt": {
      "Type": "Task",
      "Resource": "arn:aws:lambda:us-east-1:123456789012:function:send-receipt",
      "End": true
    },
    "WaitState": {
      "Type": "Wait",
      "Seconds": 10,
      "End": true
    }
  }
}`

func TestASLStateMachineEntity(t *testing.T) {
	ents, _ := applyASLWorkflowEdges("infra/order-flow.asl.json", []byte(aslJSON), nil, nil)

	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("order-flow"), "StateMachine entity from ASL")
}

func TestASLLambdaTaskEdges(t *testing.T) {
	_, rels := applyASLWorkflowEdges("infra/order-flow.asl.json", []byte(aslJSON), nil, nil)

	smFrom := stateMachineKind + ":" + sfnStateMachineID("order-flow")
	chargeTarget := serverlessFunctionKind + ":" + lambdaFunctionID("charge-card")
	receiptTarget := serverlessFunctionKind + ":" + lambdaFunctionID("send-receipt")

	requireEdge(t, rels, stepFunctionStepInvokesEdgeKind, smFrom, chargeTarget, "ASL → charge-card Lambda")
	requireEdge(t, rels, stepFunctionStepInvokesEdgeKind, smFrom, receiptTarget, "ASL → send-receipt Lambda")

	// Wait state should NOT produce an edge.
	if len(wfEdgesOfKind(rels, stepFunctionStepInvokesEdgeKind)) != 2 {
		t.Errorf("expected exactly 2 STEPFUNCTION_STEP_INVOKES edges; got %d", len(wfEdgesOfKind(rels, stepFunctionStepInvokesEdgeKind)))
	}
}

// applyASLWorkflowEdges is also reachable via the main applyWorkflowEdges path for .asl.json files.
func TestASLRoutedThroughApplyWorkflowEdges(t *testing.T) {
	_res := applyWorkflowEdges(DetectorPassArgs{Lang: "json", Path: "infra/order-flow.asl.json", Content: []byte(aslJSON)})
	ents, rels := _res.Entities, _res.Relationships

	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("order-flow"), "routed ASL → StateMachine")
	if len(wfEdgesOfKind(rels, stepFunctionStepInvokesEdgeKind)) == 0 {
		t.Error("expected STEPFUNCTION_STEP_INVOKES edges from routed ASL path")
	}
}

// ---------------------------------------------------------------------------
// ASL — arn:aws:states:::lambda:invoke (Parameters block)
// ---------------------------------------------------------------------------

const aslParamsJSON = `{
  "States": {
    "InvokeViaSDKIntegration": {
      "Type": "Task",
      "Resource": "arn:aws:states:::lambda:invoke",
      "Parameters": {
        "FunctionName": "process-order",
        "Payload.$": "$"
      },
      "End": true
    }
  }
}`

func TestASLStatesLambdaInvokeResource(t *testing.T) {
	_, rels := applyASLWorkflowEdges("infra/sm.asl.json", []byte(aslParamsJSON), nil, nil)

	target := serverlessFunctionKind + ":" + lambdaFunctionID("process-order")
	requireWorkflowEdgeTo(t, rels, stepFunctionStepInvokesEdgeKind, target, "states:::lambda:invoke Parameters FunctionName")
}

// ---------------------------------------------------------------------------
// Terraform SFN
// ---------------------------------------------------------------------------

const tfSFNSrc = `
resource "aws_sfn_state_machine" "order_flow" {
  name     = "order-flow-machine"
  role_arn = aws_iam_role.sfn_role.arn

  definition = <<EOF
{
  "States": {
    "ChargeCustomer": {
      "Type": "Task",
      "Resource": "arn:aws:lambda:us-east-1:123456789012:function:charge-card",
      "End": true
    }
  }
}
EOF
}
`

func TestTerraformSFNResource(t *testing.T) {
	ents, rels := applyTerraformSFNEdges("infra/main.tf", []byte(tfSFNSrc), nil, nil)

	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("order-flow-machine"), "Terraform SFN entity")

	smFrom := stateMachineKind + ":" + sfnStateMachineID("order-flow-machine")
	target := serverlessFunctionKind + ":" + lambdaFunctionID("charge-card")
	requireEdge(t, rels, stepFunctionStepInvokesEdgeKind, smFrom, target, "Terraform SFN → Lambda edge")
}

// ---------------------------------------------------------------------------
// Step Functions start_execution invocation edges
// ---------------------------------------------------------------------------

const pySFNInvokeSrc = `
import boto3

sfn = boto3.client('stepfunctions')

def trigger_order_flow(order_id: str):
    sfn.start_execution(
        stateMachineArn='arn:aws:states:us-east-1:123456789012:stateMachine:order-flow-machine',
        input=json.dumps({"orderId": order_id})
    )
`

func TestPythonSFNStartExecution(t *testing.T) {
	ents, rels := runSFNStartEdges(t, "python", "handlers/trigger.py", pySFNInvokeSrc)

	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("order-flow-machine"), "SFN entity from Python invocation")
	requireWorkflowEdgeTo(t, rels, startsWorkflowEdgeKind,
		stateMachineKind+":"+sfnStateMachineID("order-flow-machine"), "Python STARTS_WORKFLOW → SFN")
}

const goSFNInvokeSrc = `
package main

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
)

func startOrderFlow(ctx context.Context, client *sfn.Client) {
	_, _ = client.StartExecution(ctx, &sfn.StartExecutionInput{
		StateMachineArn: aws.String("arn:aws:states:us-east-1:123456789012:stateMachine:order-flow-machine"),
	})
}
`

func TestGoSFNStartExecution(t *testing.T) {
	ents, rels := runSFNStartEdges(t, "go", "infra/trigger.go", goSFNInvokeSrc)

	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("order-flow-machine"), "SFN entity from Go invocation")
	requireWorkflowEdgeTo(t, rels, startsWorkflowEdgeKind,
		stateMachineKind+":"+sfnStateMachineID("order-flow-machine"), "Go STARTS_WORKFLOW → SFN")
}

const nodeSFNInvokeSrc = `
import { SFNClient, StartExecutionCommand } from "@aws-sdk/client-sfn";

const client = new SFNClient({});

async function triggerOrderFlow(orderId: string) {
  await client.send(new StartExecutionCommand({
    stateMachineArn: 'arn:aws:states:us-east-1:123456789012:stateMachine:order-flow-machine',
    input: JSON.stringify({ orderId }),
  }));
}
`

func TestNodeSFNStartExecution(t *testing.T) {
	ents, rels := runSFNStartEdges(t, "typescript", "src/trigger.ts", nodeSFNInvokeSrc)

	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("order-flow-machine"), "SFN entity from Node invocation")
	requireWorkflowEdgeTo(t, rels, startsWorkflowEdgeKind,
		stateMachineKind+":"+sfnStateMachineID("order-flow-machine"), "Node STARTS_WORKFLOW → SFN")
}

// ---------------------------------------------------------------------------
// False-positive guards
// ---------------------------------------------------------------------------

func TestFalsePositivePlainPythonWorkflowRun(t *testing.T) {
	// A plain function called `start_workflow` without any Temporal imports
	// should not produce any entities or edges.
	src := `
def start_workflow(name):
    workflow.run(name)

def do_thing():
    start_workflow("my-workflow")
`
	ents, rels := runWorkflowEdges(t, "python", "plain/handler.py", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("false positive: expected 0 entities/edges for plain Python, got %d/%d", len(ents), len(rels))
	}
}

func TestFalsePositivePlainGoWorkflowRun(t *testing.T) {
	// Plain Go code with w.RegisterWorkflow-style naming but no Temporal import
	// should NOT trigger.
	src := `
package myapp

type w struct{}

func (w) RegisterWorkflow(fn func()) {
	fn()
}

func main() {
	var worker w
	worker.RegisterWorkflow(func() { println("hello") })
}
`
	ents, rels := runWorkflowEdges(t, "go", "plain/main.go", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("false positive: expected 0 entities/edges for plain Go, got %d/%d", len(ents), len(rels))
	}
}

func TestFalsePositivePlainJavaWorkflow(t *testing.T) {
	// A Java class named OrderWorkflow without any Temporal/Cadence annotations
	// should not trigger.
	src := `
public class OrderWorkflow {
    public void run() {
        System.out.println("running");
    }
}
`
	ents, rels := runWorkflowEdges(t, "java", "src/OrderWorkflow.java", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("false positive: expected 0 Java entities/edges, got %d/%d", len(ents), len(rels))
	}
}

func TestFalsePositiveSFNNoStatesKey(t *testing.T) {
	// JSON file without a States key should not produce a StateMachine.
	src := `{"name": "not-a-state-machine", "values": [1, 2, 3]}`
	ents, rels := applyASLWorkflowEdges("infra/config.json", []byte(src), nil, nil)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("false positive: plain JSON produced %d entities / %d rels", len(ents), len(rels))
	}
}

// ---------------------------------------------------------------------------
// No-op guard
// ---------------------------------------------------------------------------

func TestNoOpUnsupportedLanguageRuby(t *testing.T) {
	src := `
require 'temporal'
client = Temporal::Client.new
client.start_workflow(MyWorkflow, 'arg1')
`
	ents, rels := runWorkflowEdges(t, "ruby", "workflows/order.rb", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("expected no-op for Ruby, got %d entities / %d rels", len(ents), len(rels))
	}
}

// ---------------------------------------------------------------------------
// Cross-repo linkage: Step Functions → Lambda entity from #940
// ---------------------------------------------------------------------------

func TestStepFunctionsLambdaEntityIDMatchesServerlessEdgesFormat(t *testing.T) {
	// Verify that the entity ID used for Lambda targets in STEPFUNCTION_STEP_INVOKES
	// edges matches the format used by #940 serverless_edges.go (lambdaFunctionID).
	// Both use: SCOPE.ServerlessFunction:aws-lambda:<FunctionName>
	// This test documents the contract so future changes are caught.

	lambdaID := lambdaFunctionID("charge-card")
	expectedEntityID := "aws-lambda:charge-card"
	if lambdaID != expectedEntityID {
		t.Errorf("lambdaFunctionID format changed: got %q, want %q", lambdaID, expectedEntityID)
	}

	expectedToID := serverlessFunctionKind + ":" + lambdaID
	if expectedToID != "SCOPE.ServerlessFunction:aws-lambda:charge-card" {
		t.Errorf("ToID format changed: got %q", expectedToID)
	}
}

// ---------------------------------------------------------------------------
// Dedup guard: same entity emitted once across multiple call sites
// ---------------------------------------------------------------------------

func TestTemporalPythonDedup(t *testing.T) {
	src := `
from temporalio import workflow, activity

@activity.defn
async def charge_card(amount: float):
    pass

@workflow.defn
class OrderWorkflow:
    @workflow.run
    async def run(self):
        await workflow.execute_activity(charge_card, schedule_to_close_timeout=timedelta(seconds=10))
        await workflow.execute_activity(charge_card, schedule_to_close_timeout=timedelta(seconds=10))
`
	ents, rels := runWorkflowEdges(t, "python", "workflows/order.py", src)

	// Should have exactly one Activity entity for charge_card.
	count := 0
	for _, e := range ents {
		if e.Kind == activityKind && e.Name == temporalActivityID("charge_card") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 charge_card activity entity, got %d", count)
	}

	// Should have exactly one EXECUTES_ACTIVITY edge.
	execEdges := wfEdgesOfKind(rels, executesActivityEdgeKind)
	if len(execEdges) != 1 {
		t.Errorf("expected 1 EXECUTES_ACTIVITY edge (dedup), got %d", len(execEdges))
	}
}

// ---------------------------------------------------------------------------
// Entity kind properties
// ---------------------------------------------------------------------------

func TestWorkflowEntityProperties(t *testing.T) {
	src := `
from temporalio import workflow

@workflow.defn
class ShipmentWorkflow:
    @workflow.run
    async def run(self):
        pass
`
	ents, _ := runWorkflowEdges(t, "python", "workflows/ship.py", src)

	for _, e := range ents {
		if e.Kind == workflowKind && strings.Contains(e.Name, "ShipmentWorkflow") {
			if e.Properties["workflow_engine"] != "temporal" {
				t.Errorf("expected workflow_engine=temporal, got %q", e.Properties["workflow_engine"])
			}
			if e.Properties["pattern_type"] != "workflow_synthesis" {
				t.Errorf("expected pattern_type=workflow_synthesis, got %q", e.Properties["pattern_type"])
			}
			if e.SourceFile != "" {
				t.Errorf("expected empty SourceFile for synthetic, got %q", e.SourceFile)
			}
			return
		}
	}
	t.Error("ShipmentWorkflow entity not found")
}

// ---------------------------------------------------------------------------
// #6012 — CDK StateMachine: no cartesian product of STEPFUNCTION_STEP_INVOKES
// ---------------------------------------------------------------------------

// sfnInvokeTargets returns, per state-machine FromID, the sorted list of
// STEPFUNCTION_STEP_INVOKES target IDs, plus the total edge count (so callers
// can also assert on duplicates, which a per-machine view alone would hide).
func sfnInvokeTargets(rels []types.RelationshipRecord) (map[string][]string, int) {
	out := map[string][]string{}
	total := 0
	for _, r := range rels {
		if r.Kind != stepFunctionStepInvokesEdgeKind {
			continue
		}
		total++
		out[r.FromID] = append(out[r.FromID], r.ToID)
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out, total
}

func sfnMachineFromID(name string) string {
	return stateMachineKind + ":" + sfnStateMachineID(name)
}

func sfnLambdaToID(name string) string {
	return serverlessFunctionKind + ":" + lambdaFunctionID(name)
}

func TestSFNLambdaARNNameExcludesSurroundingQuote(t *testing.T) {
	// An ARN inside a string literal must not carry the closing quote into the
	// captured function name — that points the edge at a Lambda entity ID that
	// no serverless-function entity will ever match.
	src := `
resource "aws_sfn_state_machine" "order" {
  name       = "order-flow"
  definition = <<EOF
{
  "StartAt": "Validate",
  "States": {
    "Validate": {
      "Type": "Task",
      "Resource": "arn:aws:lambda:us-east-1:111122223333:function:validate-order",
      "End": true
    }
  }
}
EOF
}
`
	// NOTE: the Terraform path emits this edge twice (once from the block-level
	// ARN scan, once from the embedded-ASL re-parse, each with its own dedupe
	// map). That duplication pre-dates #6012 and is deliberately not asserted
	// on here; this test is about the *target ID*, not the edge count.
	_, rels := applyTerraformSFNEdges("infra/sfn.tf", []byte(src), nil, nil)
	targets, total := sfnInvokeTargets(rels)
	if total == 0 {
		t.Fatalf("expected a STEPFUNCTION_STEP_INVOKES edge, got none")
	}
	for _, got := range targets[sfnMachineFromID("order-flow")] {
		if got != sfnLambdaToID("validate-order") {
			t.Errorf("target ID: got %q, want %q", got, sfnLambdaToID("validate-order"))
		}
	}
	if len(targets[sfnMachineFromID("order-flow")]) == 0 {
		t.Errorf("no edge from order-flow; got %v", targets)
	}
}

func TestCDKStateMachineNoCartesianInvokeEdges(t *testing.T) {
	// Two state machines and two Lambdas in one file, each machine invoking
	// exactly one of the Lambdas. A whole-file ARN scan run inside the
	// per-machine loop joins every machine to every Lambda (4 edges).
	src := `{
  "source": "
    const orderFlow = new StateMachine(this, 'OrderFlow', {
      definition: new LambdaInvoke(this, 'Validate', {
        lambdaFunctionArn: 'arn:aws:lambda:us-east-1:111122223333:function:validate-order'
      })
    });
    const refundFlow = new StateMachine(this, 'RefundFlow', {
      definition: new LambdaInvoke(this, 'Refund', {
        lambdaFunctionArn: 'arn:aws:lambda:us-east-1:111122223333:function:issue-refund'
      })
    });
  "
}`
	ents, rels := runWorkflowEdges(t, "json", "cdk/app-stack.json", src)

	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("OrderFlow"), "cdk sfn")
	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("RefundFlow"), "cdk sfn")

	targets, total := sfnInvokeTargets(rels)
	if total != 2 {
		t.Errorf("expected exactly 2 STEPFUNCTION_STEP_INVOKES edges (no cartesian product), got %d: %v", total, targets)
	}
	if got, want := targets[sfnMachineFromID("OrderFlow")], []string{sfnLambdaToID("validate-order")}; !reflect.DeepEqual(got, want) {
		t.Errorf("OrderFlow targets: got %v, want %v", got, want)
	}
	if got, want := targets[sfnMachineFromID("RefundFlow")], []string{sfnLambdaToID("issue-refund")}; !reflect.DeepEqual(got, want) {
		t.Errorf("RefundFlow targets: got %v, want %v", got, want)
	}
}

func TestCDKStateMachineWithoutLambdaEmitsNoInvokeEdges(t *testing.T) {
	// A machine that invokes no Lambda must still yield an entity, and must
	// not inherit its sibling's target.
	src := `{
  "source": "
    new StateMachine(this, 'PassOnlyFlow', {
      definition: new Pass(this, 'NoOp')
    });
    new StateMachine(this, 'OrderFlow', {
      definition: new LambdaInvoke(this, 'Validate', {
        lambdaFunctionArn: 'arn:aws:lambda:us-east-1:111122223333:function:validate-order'
      })
    });
  "
}`
	ents, rels := runWorkflowEdges(t, "json", "cdk/app-stack.json", src)

	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("PassOnlyFlow"), "cdk sfn")

	targets, total := sfnInvokeTargets(rels)
	if total != 1 {
		t.Errorf("expected exactly 1 STEPFUNCTION_STEP_INVOKES edge, got %d: %v", total, targets)
	}
	if got := targets[sfnMachineFromID("PassOnlyFlow")]; len(got) != 0 {
		t.Errorf("PassOnlyFlow must invoke nothing, got %v", got)
	}
}

func TestCDKStateMachinesSharingTargetKeepBothEdges(t *testing.T) {
	// Two machines genuinely invoking the same Lambda must produce one edge
	// each — dedupe is per-machine, not per-target-across-machines.
	src := `{
  "source": "
    new StateMachine(this, 'OrderFlow', {
      definition: new LambdaInvoke(this, 'Audit', {
        lambdaFunctionArn: 'arn:aws:lambda:us-east-1:111122223333:function:audit-log'
      })
    });
    new StateMachine(this, 'RefundFlow', {
      definition: new LambdaInvoke(this, 'Audit', {
        lambdaFunctionArn: 'arn:aws:lambda:us-east-1:111122223333:function:audit-log'
      })
    });
  "
}`
	_, rels := runWorkflowEdges(t, "json", "cdk/app-stack.json", src)

	targets, total := sfnInvokeTargets(rels)
	if total != 2 {
		t.Errorf("expected 2 STEPFUNCTION_STEP_INVOKES edges (one per machine), got %d: %v", total, targets)
	}
	for _, sm := range []string{"OrderFlow", "RefundFlow"} {
		if got, want := targets[sfnMachineFromID(sm)], []string{sfnLambdaToID("audit-log")}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s targets: got %v, want %v", sm, got, want)
		}
	}
}

func TestCDKStateMachineDedupesRepeatedARNWithinOneMachine(t *testing.T) {
	// The same Lambda referenced twice inside one machine's body yields one
	// edge — matching the CloudFormation sibling's seenEdge behaviour.
	src := `{
  "source": "
    new StateMachine(this, 'OrderFlow', {
      definition: Chain.start(new LambdaInvoke(this, 'A', {
        lambdaFunctionArn: 'arn:aws:lambda:us-east-1:111122223333:function:validate-order'
      })).next(new LambdaInvoke(this, 'B', {
        lambdaFunctionArn: 'arn:aws:lambda:us-east-1:111122223333:function:validate-order'
      }))
    });
  "
}`
	_, rels := runWorkflowEdges(t, "json", "cdk/app-stack.json", src)

	targets, total := sfnInvokeTargets(rels)
	if total != 1 {
		t.Errorf("expected 1 deduped STEPFUNCTION_STEP_INVOKES edge, got %d: %v", total, targets)
	}
}

func TestCDKStateMachineWithoutInlinePropsAdoptsNothing(t *testing.T) {
	// A machine whose props come from a variable has no inline props object of
	// its own. It must adopt neither a *later* construct's braces nor an
	// unrelated intervening construct's braces — both would attribute an edge
	// to the wrong machine, which is the whole point of #6012.
	src := `{
  "source": "
    new StateMachine(this, 'FromVariable', props);
    const fn = new Function(this, 'Worker', {
      environment: { TARGET_ARN: 'arn:aws:lambda:us-east-1:111122223333:function:gamma-worker' }
    });
    new StateMachine(this, 'OrderFlow', {
      definition: new LambdaInvoke(this, 'Validate', {
        lambdaFunctionArn: 'arn:aws:lambda:us-east-1:111122223333:function:validate-order'
      })
    });
  "
}`
	ents, rels := runWorkflowEdges(t, "json", "cdk/app-stack.json", src)

	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("FromVariable"), "cdk sfn")

	targets, total := sfnInvokeTargets(rels)
	if total != 1 {
		t.Errorf("expected exactly 1 STEPFUNCTION_STEP_INVOKES edge, got %d: %v", total, targets)
	}
	if got := targets[sfnMachineFromID("FromVariable")]; len(got) != 0 {
		t.Errorf("FromVariable has no inline props and must invoke nothing, got %v", got)
	}
	if got, want := targets[sfnMachineFromID("OrderFlow")], []string{sfnLambdaToID("validate-order")}; !reflect.DeepEqual(got, want) {
		t.Errorf("OrderFlow targets: got %v, want %v", got, want)
	}
}

func TestCDKStateMachineBodyClampedToNextConstruct(t *testing.T) {
	// A brace inside a string literal desynchronises the brace counter, so the
	// "closing" brace found for OrderFlow lies beyond the next construct. The
	// body must still be clamped to where RefundFlow begins, or OrderFlow
	// swallows RefundFlow's target.
	src := `{
  "source": "
    new StateMachine(this, 'OrderFlow', {
      comment: 'this literal opens a brace { and never closes it',
      definition: new LambdaInvoke(this, 'Validate', {
        lambdaFunctionArn: 'arn:aws:lambda:us-east-1:111122223333:function:validate-order'
      })
    });
    new StateMachine(this, 'RefundFlow', {
      definition: new LambdaInvoke(this, 'Refund', {
        lambdaFunctionArn: 'arn:aws:lambda:us-east-1:111122223333:function:issue-refund'
      })
    });
    const trailing = 'and this literal closes it }';
  "
}`
	_, rels := runWorkflowEdges(t, "json", "cdk/app-stack.json", src)

	targets, total := sfnInvokeTargets(rels)
	if got := targets[sfnMachineFromID("OrderFlow")]; len(got) != 0 &&
		!reflect.DeepEqual(got, []string{sfnLambdaToID("validate-order")}) {
		t.Errorf("OrderFlow must not reach past RefundFlow's declaration, got %v", got)
	}
	if total > 2 {
		t.Errorf("expected at most 2 STEPFUNCTION_STEP_INVOKES edges, got %d: %v", total, targets)
	}
}

// ---------------------------------------------------------------------------
// #6012 — CloudFormation state machines are named per logical ID, not per file
// ---------------------------------------------------------------------------

// cfnTwoMachineTemplate is a template declaring TWO
// AWS::StepFunctions::StateMachine resources with TWO distinct Lambda targets.
// One machine and one Lambda cannot distinguish correct behaviour from a
// per-file collapse or a cartesian product; two of each can.
const cfnTwoMachineTemplate = `
AWSTemplateFormatVersion: '2010-09-09'
Resources:
  OrderFlow:
    Type: AWS::StepFunctions::StateMachine
    Properties:
      RoleArn: arn:aws:iam::111122223333:role/sfn
      DefinitionString: |
        {
          "StartAt": "Validate",
          "States": {
            "Validate": {
              "Type": "Task",
              "Resource": "arn:aws:lambda:us-east-1:111122223333:function:validate-order",
              "End": true
            }
          }
        }
  RefundFlow:
    Type: AWS::StepFunctions::StateMachine
    Properties:
      RoleArn: arn:aws:iam::111122223333:role/sfn
      DefinitionString: |
        {
          "StartAt": "Refund",
          "States": {
            "Refund": {
              "Type": "Task",
              "Resource": "arn:aws:lambda:us-east-1:111122223333:function:issue-refund",
              "End": true
            }
          }
        }
`

func TestCloudFormationStateMachinesNamedByLogicalID(t *testing.T) {
	ents, rels := runWorkflowEdges(t, "yaml", "infra/template.yaml", cfnTwoMachineTemplate)

	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("OrderFlow"), "cfn sfn")
	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("RefundFlow"), "cfn sfn")

	// The file-derived name must NOT be used when logical IDs are available:
	// that node conflates the two machines.
	if workflowEntityByID(ents, stateMachineKind, sfnStateMachineID("template")) != nil {
		t.Errorf("file-derived state machine %q emitted alongside logical-ID machines",
			sfnStateMachineID("template"))
	}

	smCount := 0
	for _, e := range ents {
		if e.Kind == stateMachineKind {
			smCount++
		}
	}
	if smCount != 2 {
		t.Errorf("expected 2 state machine entities (one per logical ID), got %d", smCount)
	}

	targets, total := sfnInvokeTargets(rels)
	if total != 2 {
		t.Errorf("expected exactly 2 STEPFUNCTION_STEP_INVOKES edges, got %d: %v", total, targets)
	}
	if got, want := targets[sfnMachineFromID("OrderFlow")], []string{sfnLambdaToID("validate-order")}; !reflect.DeepEqual(got, want) {
		t.Errorf("OrderFlow targets: got %v, want %v", got, want)
	}
	if got, want := targets[sfnMachineFromID("RefundFlow")], []string{sfnLambdaToID("issue-refund")}; !reflect.DeepEqual(got, want) {
		t.Errorf("RefundFlow targets: got %v, want %v", got, want)
	}
}

func TestCloudFormationStateMachineRecordsLiteralStateMachineName(t *testing.T) {
	// StateMachineName is recorded as a property so cross-source resolution has
	// something to match on. Node identity must still come from the logical ID:
	// the field is optional and often an unresolved intrinsic.
	src := `
Resources:
  OrderFlow:
    Type: AWS::StepFunctions::StateMachine
    Properties:
      StateMachineName: prod-order-flow
      DefinitionString: '{"States": {}}'
  RefundFlow:
    Type: AWS::StepFunctions::StateMachine
    Properties:
      StateMachineName: !Sub '${AWS::StackName}-refund'
      DefinitionString: '{"States": {}}'
  AuditFlow:
    Type: AWS::StepFunctions::StateMachine
    Properties:
      DefinitionString: '{"States": {}}'
`
	ents, _ := runWorkflowEdges(t, "yaml", "infra/template.yaml", src)

	for _, tc := range []struct{ logicalID, wantName string }{
		{"OrderFlow", "prod-order-flow"},
		{"RefundFlow", ""}, // unresolved !Sub — not recorded
		{"AuditFlow", ""},  // absent
	} {
		e := workflowEntityByID(ents, stateMachineKind, sfnStateMachineID(tc.logicalID))
		if e == nil {
			t.Errorf("%s: entity not found (identity must come from the logical ID)", tc.logicalID)
			continue
		}
		if got := e.Properties["state_machine_name"]; got != tc.wantName {
			t.Errorf("%s: state_machine_name = %q, want %q", tc.logicalID, got, tc.wantName)
		}
		if got := e.Properties["sm_name"]; got != tc.logicalID {
			t.Errorf("%s: sm_name = %q, want the logical ID", tc.logicalID, got)
		}
	}
}

func TestCloudFormationStateMachineWithoutLambdaEmitsNoInvokeEdges(t *testing.T) {
	src := `
Resources:
  PassOnlyFlow:
    Type: AWS::StepFunctions::StateMachine
    Properties:
      DefinitionString: |
        {"StartAt": "NoOp", "States": {"NoOp": {"Type": "Pass", "End": true}}}
  OrderFlow:
    Type: AWS::StepFunctions::StateMachine
    Properties:
      DefinitionString: |
        {"StartAt": "Validate", "States": {"Validate": {"Type": "Task",
         "Resource": "arn:aws:lambda:us-east-1:111122223333:function:validate-order", "End": true}}}
`
	ents, rels := runWorkflowEdges(t, "yaml", "infra/template.yaml", src)

	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("PassOnlyFlow"), "cfn sfn")

	targets, total := sfnInvokeTargets(rels)
	if total != 1 {
		t.Errorf("expected exactly 1 STEPFUNCTION_STEP_INVOKES edge, got %d: %v", total, targets)
	}
	if got := targets[sfnMachineFromID("PassOnlyFlow")]; len(got) != 0 {
		t.Errorf("PassOnlyFlow must invoke nothing, got %v", got)
	}
}

func TestCloudFormationStateMachinesSharingTargetKeepBothEdges(t *testing.T) {
	src := `
Resources:
  OrderFlow:
    Type: AWS::StepFunctions::StateMachine
    Properties:
      DefinitionString: |
        {"States": {"Audit": {"Type": "Task",
         "Resource": "arn:aws:lambda:us-east-1:111122223333:function:audit-log", "End": true}}}
  RefundFlow:
    Type: AWS::StepFunctions::StateMachine
    Properties:
      DefinitionString: |
        {"States": {"Audit": {"Type": "Task",
         "Resource": "arn:aws:lambda:us-east-1:111122223333:function:audit-log", "End": true}}}
`
	_, rels := runWorkflowEdges(t, "yaml", "infra/template.yaml", src)

	targets, total := sfnInvokeTargets(rels)
	if total != 2 {
		t.Errorf("expected 2 STEPFUNCTION_STEP_INVOKES edges (one per machine), got %d: %v", total, targets)
	}
	for _, sm := range []string{"OrderFlow", "RefundFlow"} {
		if got, want := targets[sfnMachineFromID(sm)], []string{sfnLambdaToID("audit-log")}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s targets: got %v, want %v", sm, got, want)
		}
	}
}

func TestCloudFormationStateMachineFallsBackToPathNameWithoutLogicalID(t *testing.T) {
	// A fragment where the resource type marker is present but no enclosing
	// logical-ID key can be identified: keep the old file-derived name rather
	// than dropping the machine entirely.
	src := `
Type: AWS::StepFunctions::StateMachine
Properties:
  DefinitionString: '{"States": {"Validate": {"Type": "Task", "Resource": "arn:aws:lambda:us-east-1:111122223333:function:validate-order", "End": true}}}'
`
	ents, rels := runWorkflowEdges(t, "yaml", "infra/order-flow-template.yaml", src)

	requireEntityKind(t, ents, stateMachineKind, sfnStateMachineID("order-flow-template"), "cfn sfn fallback")

	targets, total := sfnInvokeTargets(rels)
	if total != 1 {
		t.Errorf("expected 1 STEPFUNCTION_STEP_INVOKES edge, got %d: %v", total, targets)
	}
	if got, want := targets[sfnMachineFromID("order-flow-template")], []string{sfnLambdaToID("validate-order")}; !reflect.DeepEqual(got, want) {
		t.Errorf("fallback machine targets: got %v, want %v", got, want)
	}
}
