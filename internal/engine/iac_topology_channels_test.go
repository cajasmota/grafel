package engine

// In-pipeline validation for #4496 — IaC-declared SQS/SNS queues surface as
// messaging Topology channels.
//
// The TestIaCTopologyChannels_RealTerraformSQSModule case runs a BYTE-COPY of
// the real core-backend-v3 `infra/terraform/modules/sqs-queue/main.tf`
// (aws_sqs_queue.main + .dlq) through the actual HCL extractor and then through
// applyIaCTopologyChannels, asserting both queues emerge as SCOPE.Queue
// broker=sqs channels. Before this pass, /topology reported "No async channels
// indexed" for repos whose only queues are Terraform-declared.

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tshcl "github.com/smacker/go-tree-sitter/hcl"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/hcl" // register hcl/terraform
	"github.com/cajasmota/archigraph/internal/types"
)

// realCoreBackendSQSModule is a verbatim copy of
// core-backend-v3/infra/terraform/modules/sqs-queue/main.tf.
const realCoreBackendSQSModule = `locals {
  queue_name = "${var.env}-${var.name}"
  dlq_name   = "${var.env}-${var.name}-dlq"

  common_tags = merge(
    {
      env     = var.env
      module  = "sqs-queue"
      managed = "terraform"
    },
    var.tags,
  )
}

# Dead-letter queue — receives messages that exceed max_receive_count
resource "aws_sqs_queue" "dlq" {
  name                      = local.dlq_name
  message_retention_seconds = var.dlq_message_retention_seconds

  tags = merge(local.common_tags, { role = "dlq" })
}

# Main queue with redrive policy pointing at the DLQ
resource "aws_sqs_queue" "main" {
  name                       = local.queue_name
  visibility_timeout_seconds = var.visibility_timeout_seconds
  message_retention_seconds  = var.message_retention_seconds

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = var.max_receive_count
  })

  tags = merge(local.common_tags, { role = "main" })
}
`

// extractHCLEntities runs the real HCL extractor over src and returns the
// extracted entity records (the input applyIaCTopologyChannels sees in-pipeline).
func extractHCLEntities(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	content := []byte(src)
	p := sitter.NewParser()
	p.SetLanguage(tshcl.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("hcl parse failed: %v", err)
	}
	ext, ok := extractor.Get("hcl")
	if !ok {
		t.Fatalf("hcl extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: content, Language: "hcl", Tree: tree,
	})
	if err != nil {
		t.Fatalf("hcl extract failed: %v", err)
	}
	return recs
}

func sqsChannel(ents []types.EntityRecord, id string) *types.EntityRecord {
	want := queueEntityKind
	for i := range ents {
		if ents[i].Kind == want && ents[i].Name == id {
			return &ents[i]
		}
	}
	return nil
}

func TestIaCTopologyChannels_RealTerraformSQSModule(t *testing.T) {
	path := "infra/terraform/modules/sqs-queue/main.tf"
	extracted := extractHCLEntities(t, realCoreBackendSQSModule, path)

	// Sanity: before the pass, NO topology channel (SCOPE.Queue) exists — the
	// extractor only emits SCOPE.Component resources. This is the bug.
	for i := range extracted {
		if extracted[i].Kind == queueEntityKind || extracted[i].Kind == messageTopicKind {
			t.Fatalf("pre-pass: unexpected topology channel %q already present", extracted[i].Name)
		}
	}

	res := applyIaCTopologyChannels(DetectorPassArgs{
		Lang:     "hcl",
		Path:     path,
		Entities: extracted,
	})

	// The two aws_sqs_queue resources (.dlq + .main) must now appear as
	// SCOPE.Queue channels keyed by their declaration labels.
	dlq := sqsChannel(res.Entities, sqsQueueID("dlq"))
	main := sqsChannel(res.Entities, sqsQueueID("main"))
	if dlq == nil {
		t.Fatalf("aws_sqs_queue.dlq did not surface as a SCOPE.Queue channel; entities=%v", channelNames(res.Entities))
	}
	if main == nil {
		t.Fatalf("aws_sqs_queue.main did not surface as a SCOPE.Queue channel; entities=%v", channelNames(res.Entities))
	}
	for _, q := range []*types.EntityRecord{dlq, main} {
		if q.Properties["broker"] != "sqs" {
			t.Errorf("%s broker = %q, want sqs", q.Name, q.Properties["broker"])
		}
		if q.Properties["iac_declared"] != "true" {
			t.Errorf("%s missing iac_declared=true", q.Name)
		}
	}
}

func channelNames(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind == queueEntityKind || ents[i].Kind == messageTopicKind {
			out = append(out, ents[i].Kind+":"+ents[i].Name)
		}
	}
	return out
}

// TestIaCTopologyChannels_CrossCloud asserts the pass is cloud/tool-agnostic by
// feeding synthetic IaC resource entities shaped like each tool's output for
// AWS SNS, GCP Pub/Sub, and Azure Service Bus, then checking each yields a
// channel on the right broker.
func TestIaCTopologyChannels_CrossCloud(t *testing.T) {
	ents := []types.EntityRecord{
		// Terraform aws_sns_topic (category in Metadata, name from label).
		{Name: "aws_sns_topic.events", Kind: "SCOPE.Component", Subtype: "resource", Language: "hcl",
			Metadata: map[string]interface{}{"resource_type": "aws_sns_topic", "resource_category": types.IaCCategoryTopic}},
		// CDK GCP not applicable; use Terraform google_pubsub_topic.
		{Name: "google_pubsub_topic.jobs", Kind: "SCOPE.Component", Subtype: "resource", Language: "hcl",
			Metadata: map[string]interface{}{"resource_type": "google_pubsub_topic", "resource_category": types.IaCCategoryTopic}},
		// Pulumi-style Azure Service Bus queue (props carry construct_type + category).
		{Name: "OrdersQueue", Kind: "SCOPE.InfraResource", Language: "typescript",
			Properties: map[string]string{"construct_type": "azure.servicebus.Queue", "resource_category": types.IaCCategoryQueue, "iac_tool": "pulumi", "name": "orders"}},
		// CloudFormation Kinesis stream (semantic Kind + props).
		{Name: "EventStream", Kind: "SCOPE.Queue", Language: "yaml",
			Properties: map[string]string{"resource_type": "AWS::Kinesis::Stream", "resource_category": types.IaCCategoryStream, "iac_tool": "cloudformation"}},
	}

	res := applyIaCTopologyChannels(DetectorPassArgs{Lang: "hcl", Entities: ents})

	wantBrokers := map[string]string{
		"sns:events":          "sns",
		"pubsub:jobs":         "pubsub",
		"servicebus:orders":   "servicebus",
		"kafka:EventStream":   "kafka",
	}
	got := map[string]string{}
	for i := range res.Entities {
		e := &res.Entities[i]
		if e.Properties["iac_declared"] == "true" {
			got[e.Name] = e.Properties["broker"]
		}
	}
	for id, broker := range wantBrokers {
		if got[id] != broker {
			t.Errorf("channel %q broker = %q, want %q (got map=%v)", id, got[id], broker, got)
		}
	}
}

// TestIaCTopologyChannels_DedupesCodeSide verifies the pass does not double-emit
// a queue the code-side SQS pass already minted (same sqs:<name> ID).
func TestIaCTopologyChannels_DedupesCodeSide(t *testing.T) {
	ents := []types.EntityRecord{
		// Pre-existing code-side queue.
		{Name: sqsQueueID("orders"), Kind: queueEntityKind, Properties: map[string]string{"broker": "sqs"}},
		// IaC declaration of the SAME queue.
		{Name: "aws_sqs_queue.orders", Kind: "SCOPE.Component", Subtype: "resource",
			Metadata: map[string]interface{}{"resource_type": "aws_sqs_queue", "resource_category": types.IaCCategoryQueue, "label": "orders"},
			Properties: map[string]string{"name": "orders"}},
	}
	res := applyIaCTopologyChannels(DetectorPassArgs{Lang: "hcl", Entities: ents})
	n := 0
	for i := range res.Entities {
		if res.Entities[i].Kind == queueEntityKind && res.Entities[i].Name == sqsQueueID("orders") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("queue sqs:orders emitted %d times, want 1 (dedup against code-side failed)", n)
	}
}
