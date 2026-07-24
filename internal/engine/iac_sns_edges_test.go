package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// countSNSSubscribers returns the number of distinct SUBSCRIBES_TO edges whose
// ToID is the given SNS topic ID, plus the set of iac_tool values seen.
func countSNSSubscribers(rels []types.RelationshipRecord, topicID string) (int, map[string]bool) {
	to := messageTopicKind + ":" + topicID
	tools := map[string]bool{}
	n := 0
	for _, r := range rels {
		if r.Kind == subscribesToEdgeKind && r.ToID == to {
			n++
			tools[r.Properties.Get("iac_tool")] = true
		}
	}
	return n, tools
}

func TestIaCSNSEdges_CDK(t *testing.T) {
	src := `
import * as sns from "aws-cdk-lib/aws-sns";
import * as sqs from "aws-cdk-lib/aws-sqs";
import { SqsSubscription } from "aws-cdk-lib/aws-sns-subscriptions";
const orderEvents = new sns.Topic(this, "OrderEvents", { topicName: "order-events" });
const analyticsQueue = new sqs.Queue(this, "AQ", { queueName: "order-events-analytics" });
orderEvents.addSubscription(new SqsSubscription(analyticsQueue));
`
	_res := applyIaCSNSEdges(DetectorPassArgs{Lang: "typescript", Path: "infra/cdk/lib/stack.ts", Content: []byte(src)})
	_, rels := _res.Entities, _res.Relationships
	n, tools := countSNSSubscribers(rels, snsTopicID("order-events"))
	if n != 1 {
		t.Fatalf("CDK: want 1 subscriber, got %d (%+v)", n, rels)
	}
	if !tools["cdk"] {
		t.Fatalf("CDK: want iac_tool=cdk, got %v", tools)
	}
}

func TestIaCSNSEdges_Terraform(t *testing.T) {
	src := `
resource "aws_sqs_queue" "order_events_audit" {
  name = "order-events-audit"
}
resource "aws_sns_topic_subscription" "order_events_audit" {
  topic_arn = "arn:aws:sns:us-east-1:000000000000:order-events"
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.order_events_audit.arn
}
`
	_res := applyIaCSNSEdges(DetectorPassArgs{Lang: "terraform", Path: "infra/terraform/order-events.tf", Content: []byte(src)})
	_, rels := _res.Entities, _res.Relationships
	n, tools := countSNSSubscribers(rels, snsTopicID("order-events"))
	if n != 1 {
		t.Fatalf("TF: want 1 subscriber, got %d (%+v)", n, rels)
	}
	if !tools["terraform"] {
		t.Fatalf("TF: want iac_tool=terraform, got %v", tools)
	}
}

func TestIaCSNSEdges_CloudFormation(t *testing.T) {
	src := `AWSTemplateFormatVersion: "2010-09-09"
Parameters:
  OrderEventsTopicArn:
    Type: String
    Default: "arn:aws:sns:us-east-1:000000000000:order-events"
Resources:
  OrderEventsFraudQueue:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: order-events-fraud
  OrderEventsFraudSubscription:
    Type: AWS::SNS::Subscription
    Properties:
      TopicArn: !Ref OrderEventsTopicArn
      Protocol: sqs
      Endpoint: !GetAtt OrderEventsFraudQueue.Arn
`
	_res := applyIaCSNSEdges(DetectorPassArgs{Lang: "yaml", Path: "infra/cloudformation/order-events-fanout.yaml", Content: []byte(src)})
	_, rels := _res.Entities, _res.Relationships
	n, tools := countSNSSubscribers(rels, snsTopicID("order-events"))
	if n != 1 {
		t.Fatalf("CFN: want 1 subscriber, got %d (%+v)", n, rels)
	}
	if !tools["cloudformation"] {
		t.Fatalf("CFN: want iac_tool=cloudformation, got %v", tools)
	}
}

// TestIaCSNSEdges_FanOutCollapse verifies that subscriptions for the SAME topic
// name declared across three IaC tools collapse onto a single topic node with
// three distinct SQS subscribers.
func TestIaCSNSEdges_FanOutCollapse(t *testing.T) {
	cdk := `const t = new sns.Topic(this, "T", { topicName: "order-events" });
const q = new sqs.Queue(this, "Q", { queueName: "order-events-analytics" });
t.addSubscription(new SqsSubscription(q));`
	tf := `resource "aws_sqs_queue" "a" { name = "order-events-audit" }
resource "aws_sns_topic_subscription" "a" {
  topic_arn = "arn:aws:sns:us-east-1:0:order-events"
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.a.arn
}`
	cfn := `Resources:
  FraudQ:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: order-events-fraud
  Sub:
    Type: AWS::SNS::Subscription
    Properties:
      TopicArn: "arn:aws:sns:us-east-1:0:order-events"
      Protocol: sqs
      Endpoint: !GetAtt FraudQ.Arn`

	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	var _res DetectorPassResult
	_res = applyIaCSNSEdges(DetectorPassArgs{Lang: "typescript", Path: "stack.ts", Content: []byte(cdk), Entities: ents, Relationships: rels})
	ents, rels = _res.Entities, _res.Relationships
	_res = applyIaCSNSEdges(DetectorPassArgs{Lang: "terraform", Path: "x.tf", Content: []byte(tf), Entities: ents, Relationships: rels})
	ents, rels = _res.Entities, _res.Relationships
	_res = applyIaCSNSEdges(DetectorPassArgs{Lang: "yaml", Path: "x.yaml", Content: []byte(cfn), Entities: ents, Relationships: rels})
	ents, rels = _res.Entities, _res.Relationships

	n, tools := countSNSSubscribers(rels, snsTopicID("order-events"))
	if n != 3 {
		t.Fatalf("fan-out: want 3 subscribers, got %d", n)
	}
	for _, want := range []string{"cdk", "terraform", "cloudformation"} {
		if !tools[want] {
			t.Fatalf("fan-out: missing iac_tool=%s (got %v)", want, tools)
		}
	}
	// All SNS topic entities share ONE canonical ID, so they collapse to a
	// single node at render time (entities are deduped by ID across files /
	// repos, mirroring kafka_wrapper_edges.go which also emits one topic
	// entity per file). Assert every emitted SNS topic carries the same ID.
	topicCount := 0
	for _, e := range ents {
		if e.Kind == messageTopicKind {
			topicCount++
			if e.Name != snsTopicID("order-events") {
				t.Fatalf("fan-out: SNS topic entity has unexpected ID %q", e.Name)
			}
		}
	}
	if topicCount == 0 {
		t.Fatalf("fan-out: no SNS topic entity emitted")
	}
}

// TestIaCSNSEdges_CloudFormation_SameTemplateResourceRefs — #5802. The
// existing TestIaCSNSEdges_CloudFormation test above covers TopicArn
// resolved via a Parameter Default (an ARN literal one hop away). This
// covers the more common shape: TopicArn/Endpoint intrinsics that reference
// AWS::SNS::Topic / AWS::SQS::Queue resource logical ids declared directly
// in the SAME template (`TopicArn: !Ref FanoutTopic`, `Endpoint: !GetAtt
// ConsumerQueueA.Arn`) — a single topic fanning out to two queues. Locks in
// that this resolution already works so it does not regress.
func TestIaCSNSEdges_CloudFormation_SameTemplateResourceRefs(t *testing.T) {
	src := `AWSTemplateFormatVersion: '2010-09-09'
Resources:
  FanoutTopic:
    Type: AWS::SNS::Topic
    Properties:
      TopicName: demo-fanout-topic
  ConsumerQueueA:
    Type: AWS::SQS::Queue
    Properties: { QueueName: demo-consumer-a }
  ConsumerQueueB:
    Type: AWS::SQS::Queue
    Properties: { QueueName: demo-consumer-b }
  SubA:
    Type: AWS::SNS::Subscription
    Properties:
      TopicArn: !Ref FanoutTopic
      Protocol: sqs
      Endpoint: !GetAtt ConsumerQueueA.Arn
      RawMessageDelivery: true
  SubB:
    Type: AWS::SNS::Subscription
    Properties:
      TopicArn: !Ref FanoutTopic
      Protocol: sqs
      Endpoint: !GetAtt ConsumerQueueB.Arn
`
	res := applyIaCSNSEdges(DetectorPassArgs{Lang: "yaml", Path: "fanout.yaml", Content: []byte(src)})
	n, tools := countSNSSubscribers(res.Relationships, snsTopicID("demo-fanout-topic"))
	if n != 2 {
		t.Fatalf("same-template CFN fanout: want 2 subscribers (queue A + B), got %d; rels=%+v", n, res.Relationships)
	}
	if !tools["cloudformation"] {
		t.Fatalf("same-template CFN fanout: want iac_tool=cloudformation, got %v", tools)
	}
	// Each queue must resolve to its own SQS queue name (QueueName), not
	// collapse to a generic/disconnected placeholder.
	wantQueueIDs := map[string]bool{
		sqsQueueID("demo-consumer-a"): false,
		sqsQueueID("demo-consumer-b"): false,
	}
	for _, r := range res.Relationships {
		if r.Kind != subscribesToEdgeKind {
			continue
		}
		if !strings.HasPrefix(r.FromID, queueEntityKind+":") {
			continue
		}
		qid := strings.TrimPrefix(r.FromID, queueEntityKind+":")
		if _, ok := wantQueueIDs[qid]; ok {
			wantQueueIDs[qid] = true
		}
	}
	for qid, found := range wantQueueIDs {
		if !found {
			t.Errorf("same-template CFN fanout: missing SUBSCRIBES_TO from queue %q", qid)
		}
	}
}
