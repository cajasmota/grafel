// Tests for the AWS SQS producer/consumer detection pass added by #726 wave 2.
//
// Each language has at minimum:
//   - Static queue URL/name on the producer side (emits SCOPE.Queue + PUBLISHES_TO).
//   - Static queue URL/name on the consumer side (emits SCOPE.Queue + SUBSCRIBES_TO).
//   - create_queue / createQueue emits entity without a direction edge.
//   - Beyond-minimum: Lambda trigger consumer, SNS→SQS fanout.
package engine

import (
	"strings"
	"testing"
)

// runSQSDetect is a lightweight in-process driver for the SQS pass.
func runSQSDetect(t *testing.T, lang, path, src string) ([]entityResult, []relResult) {
	t.Helper()
	ents, rels := applySQSEdges(lang, path, []byte(src), nil, nil)
	out := make([]entityResult, 0, len(ents))
	for _, e := range ents {
		out = append(out, entityResult{kind: e.Kind, name: e.Name, props: e.Properties})
	}
	relOut := make([]relResult, 0, len(rels))
	for _, r := range rels {
		relOut = append(relOut, relResult{from: r.FromID, to: r.ToID, kind: r.Kind, props: r.Properties})
	}
	return out, relOut
}

// ---------------------------------------------------------------------------
// Python — boto3
// ---------------------------------------------------------------------------

// TestSQS_Python_SendMessage covers sqs.send_message(QueueUrl=..., MessageBody=...).
func TestSQS_Python_SendMessage(t *testing.T) {
	src := `import boto3
sqs = boto3.client('sqs')

def enqueue_order(order):
    sqs.send_message(
        QueueUrl='https://sqs.us-east-1.amazonaws.com/123/orders-queue',
        MessageBody=order,
    )
`
	ents, rels := runSQSDetect(t, "python", "send.py", src)
	qID := sqsQueueID("orders-queue")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders-queue, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if !strings.Contains(pubs[0].to, qID) {
		t.Fatalf("PUBLISHES_TO ToID = %q, want to contain %q", pubs[0].to, qID)
	}
}

// TestSQS_Python_ReceiveMessage covers sqs.receive_message(QueueUrl=...).
func TestSQS_Python_ReceiveMessage(t *testing.T) {
	src := `import boto3
sqs = boto3.client('sqs')

def poll():
    response = sqs.receive_message(QueueUrl='https://sqs.us-east-1.amazonaws.com/123/orders-queue', MaxNumberOfMessages=10)
    return response.get('Messages', [])
`
	ents, rels := runSQSDetect(t, "python", "poll.py", src)
	qID := sqsQueueID("orders-queue")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders-queue, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestSQS_Python_CreateQueue covers sqs.create_queue(QueueName=...) emitting
// a SCOPE.Queue entity without a direction edge.
func TestSQS_Python_CreateQueue(t *testing.T) {
	src := `import boto3
sqs = boto3.client('sqs')

def setup():
    sqs.create_queue(QueueName='dead-letter-queue', Attributes={'MessageRetentionPeriod': '86400'})
`
	ents, rels := runSQSDetect(t, "python", "setup.py", src)
	qID := sqsQueueID("dead-letter-queue")
	q := queueByName(ents, qID)
	if q == nil {
		t.Fatalf("expected SCOPE.Queue for dead-letter-queue, ents=%v", ents)
	}
	if q.props["declared"] != "true" {
		t.Fatalf("declared = %q, want true", q.props["declared"])
	}
	if len(relsByKind(rels, publishesToEdgeKind))+len(relsByKind(rels, subscribesToEdgeKind)) != 0 {
		t.Fatalf("create_queue should not emit direction edges, rels=%v", rels)
	}
}

// TestSQS_Python_LambdaTrigger covers the Lambda handler pattern where
// event["Records"][0]["eventSource"] == "aws:sqs".
func TestSQS_Python_LambdaTrigger(t *testing.T) {
	src := `def handler(event, context):
    for record in event['Records']:
        if record['eventSource'] == 'aws:sqs':
            body = record['body']
            process(body)
`
	ents, rels := runSQSDetect(t, "python", "lambda_handler.py", src)
	// Should emit a SCOPE.Queue + SUBSCRIBES_TO
	if len(ents) == 0 {
		t.Fatalf("expected at least one SCOPE.Queue entity, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge for Lambda trigger, rels=%v", rels)
	}
	if subs[0].props["lambda_trigger"] != "true" {
		t.Fatalf("lambda_trigger property = %q, want true", subs[0].props["lambda_trigger"])
	}
}

// ---------------------------------------------------------------------------
// Node — aws-sdk v2 + v3
// ---------------------------------------------------------------------------

// TestSQS_Node_SendMessageV2 covers sqs.sendMessage({QueueUrl: "..."}).
func TestSQS_Node_SendMessageV2(t *testing.T) {
	src := `const AWS = require('aws-sdk');
const sqs = new AWS.SQS();

async function enqueue(msg) {
  await sqs.sendMessage({
    QueueUrl: 'https://sqs.us-east-1.amazonaws.com/123/orders-queue',
    MessageBody: msg,
  }).promise();
}
`
	ents, rels := runSQSDetect(t, "javascript", "send.js", src)
	qID := sqsQueueID("orders-queue")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders-queue, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// TestSQS_Node_SendMessageV3 covers new SendMessageCommand({QueueUrl: "..."}).
func TestSQS_Node_SendMessageV3(t *testing.T) {
	src := `import { SQSClient, SendMessageCommand } from '@aws-sdk/client-sqs';
const client = new SQSClient({ region: 'us-east-1' });

export async function send(msg: string) {
  await client.send(new SendMessageCommand({
    QueueUrl: 'https://sqs.us-east-1.amazonaws.com/123/orders-queue',
    MessageBody: msg,
  }));
}
`
	ents, rels := runSQSDetect(t, "typescript", "send.ts", src)
	qID := sqsQueueID("orders-queue")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders-queue, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
	if pubs[0].props["messaging_layer"] != "aws-sdk-v3" {
		t.Fatalf("messaging_layer = %q, want aws-sdk-v3", pubs[0].props["messaging_layer"])
	}
}

// TestSQS_Node_ReceiveMessageV2 covers sqs.receiveMessage({QueueUrl: "..."}).
func TestSQS_Node_ReceiveMessageV2(t *testing.T) {
	src := `const AWS = require('aws-sdk');
const sqs = new AWS.SQS();

async function poll() {
  const data = await sqs.receiveMessage({ QueueUrl: 'https://sqs.us-east-1.amazonaws.com/123/orders-queue', MaxNumberOfMessages: 10 }).promise();
  return data.Messages || [];
}
`
	ents, rels := runSQSDetect(t, "javascript", "poll.js", src)
	qID := sqsQueueID("orders-queue")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders-queue, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// ---------------------------------------------------------------------------
// Go — aws-sdk-go-v2
// ---------------------------------------------------------------------------

// TestSQS_Go_SendMessage covers client.SendMessage with QueueUrl field.
func TestSQS_Go_SendMessage(t *testing.T) {
	src := `package main

import (
    "context"
    "github.com/aws/aws-sdk-go-v2/service/sqs"
    "github.com/aws/aws-sdk-go-v2/aws"
)

func Enqueue(ctx context.Context, client *sqs.Client, msg string) error {
    _, err := client.SendMessage(ctx, &sqs.SendMessageInput{
        QueueUrl:    aws.String("https://sqs.us-east-1.amazonaws.com/123/orders-queue"),
        MessageBody: aws.String(msg),
    })
    return err
}
`
	ents, rels := runSQSDetect(t, "go", "send.go", src)
	qID := sqsQueueID("orders-queue")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders-queue, ents=%v", ents)
	}
	pubs := relsByKind(rels, publishesToEdgeKind)
	if len(pubs) == 0 {
		t.Fatalf("expected PUBLISHES_TO edge, rels=%v", rels)
	}
}

// TestSQS_Go_ReceiveMessage covers client.ReceiveMessage with QueueUrl field.
func TestSQS_Go_ReceiveMessage(t *testing.T) {
	src := `package main

import (
    "context"
    "github.com/aws/aws-sdk-go-v2/service/sqs"
    "github.com/aws/aws-sdk-go-v2/aws"
)

func Poll(ctx context.Context, client *sqs.Client) {
    resp, _ := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
        QueueUrl:            aws.String("https://sqs.us-east-1.amazonaws.com/123/orders-queue"),
        MaxNumberOfMessages: 10,
    })
    _ = resp
}
`
	ents, rels := runSQSDetect(t, "go", "poll.go", src)
	qID := sqsQueueID("orders-queue")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for orders-queue, ents=%v", ents)
	}
	subs := relsByKind(rels, subscribesToEdgeKind)
	if len(subs) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// TestSQS_Go_ReceiveMessage_ConstResolved verifies that a QueueUrl referencing
// a package-level const (rather than an inline literal) is resolved via the Go
// const table. Regression for #1553 — the ShipFast shipping service receives
// from the inventory-reserved queue using a named queue-URL const.
func TestSQS_Go_ReceiveMessage_ConstResolved(t *testing.T) {
	src := `package internal

import (
    "context"
    "github.com/aws/aws-sdk-go-v2/service/sqs"
    "github.com/aws/aws-sdk-go-v2/aws"
)

const inventoryReservedQueueURL = "https://sqs.us-east-1.amazonaws.com/000000000000/inventory-reserved"

func pollOnce(ctx context.Context, c *sqs.Client) {
    out, _ := c.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
        QueueUrl: aws.String(inventoryReservedQueueURL),
    })
    _ = out
}
`
	ents, rels := runSQSDetect(t, "go", "consumer.go", src)
	qID := sqsQueueID("inventory-reserved")
	if queueByName(ents, qID) == nil {
		t.Fatalf("expected SCOPE.Queue for inventory-reserved (const-resolved), ents=%v", ents)
	}
	if len(relsByKind(rels, subscribesToEdgeKind)) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, rels=%v", rels)
	}
}

// ---------------------------------------------------------------------------
// Helpers and guards
// ---------------------------------------------------------------------------

// TestSQS_QueueIDNormalization verifies that sqsQueueID strips the URL prefix
// so cross-repo matching works regardless of AWS account/region in the URL.
func TestSQS_QueueIDNormalization(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://sqs.us-east-1.amazonaws.com/123456789012/my-queue", "sqs:my-queue"},
		{"my-queue", "sqs:my-queue"},
		{"orders-queue.fifo", "sqs:orders-queue.fifo"},
	}
	for _, tc := range cases {
		got := sqsQueueID(tc.in)
		if got != tc.want {
			t.Errorf("sqsQueueID(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// TestSQS_NoOpForUnsupportedLanguage ensures the pass is a strict no-op
// for languages it doesn't claim to support.
func TestSQS_NoOpForUnsupportedLanguage(t *testing.T) {
	ents, rels := runSQSDetect(t, "ruby", "lib/x.rb", `sqs.send_message(queue_url: "x", message_body: "y")`)
	if len(ents) != 0 || len(rels) != 0 {
		t.Fatalf("expected no-op for unsupported language, got ents=%v rels=%v", ents, rels)
	}
}

// TestSQS_LooksLikeSQSQueue exercises the queue-name/URL gate.
func TestSQS_LooksLikeSQSQueue(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"my-queue", true},
		{"orders_queue", true},
		{"https://sqs.us-east-1.amazonaws.com/123/my-queue", true},
		{"hello world", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := looksLikeSQSQueue(tc.in); got != tc.want {
			t.Errorf("looksLikeSQSQueue(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}
