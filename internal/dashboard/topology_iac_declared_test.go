package dashboard

// topology_iac_declared_test.go — #4496. Verifies that the synthetic topology
// channel entities applyIaCTopologyChannels appends for IaC-declared messaging
// resources (SCOPE.Queue broker=sqs for an aws_sqs_queue, SCOPE.MessageTopic
// broker=sns for an aws_sns_topic) render in the messaging Topology surface
// even with NO detected code publisher/subscriber — fixing the live acme-v3
// "No async channels indexed" gap where the only queues were Terraform-declared.
//
// The entity shapes here mirror exactly what internal/engine/
// applyIaCTopologyChannels emits for the real acme-backend-v3
// infra/terraform/modules/sqs-queue/main.tf (aws_sqs_queue.main + .dlq).

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func TestCollectTopology_IaCDeclaredQueuesSurface(t *testing.T) {
	doc := &graph.Document{
		Repo: "acme-backend-v3",
		Entities: []graph.Entity{
			// As emitted by applyIaCTopologyChannels for aws_sqs_queue.dlq + .main.
			graph.Entity{ID: "iac-dlq", Name: "sqs:dlq", Kind: "SCOPE.Queue"}.WithProperties(map[string]string{"broker": "sqs", "iac_declared": "true", "pattern_type": "iac_declared", "iac_tool": "terraform"}),
			graph.Entity{ID: "iac-main", Name: "sqs:main", Kind: "SCOPE.Queue"}.WithProperties(map[string]string{"broker": "sqs", "iac_declared": "true", "pattern_type": "iac_declared", "iac_tool": "terraform"}),
			// An aws_sns_topic declaration → MessageTopic.
			graph.Entity{ID: "iac-topic", Name: "sns:order-events", Kind: "SCOPE.MessageTopic"}.WithProperties(map[string]string{"broker": "sns", "iac_declared": "true", "pattern_type": "iac_declared"}),
		},
	}
	grp := &DashGroup{
		Name:  "g",
		Repos: map[string]*DashRepo{"acme-backend-v3": {Slug: "acme-backend-v3", Doc: doc}},
	}

	topics, queues, _, _ := collectTopology(grp)

	// BEFORE this change: queues + topics were both empty → "No async channels".
	// AFTER: the two SQS queues and the SNS topic appear as declared channels.
	gotQueues := map[string]bool{}
	for _, q := range queues {
		gotQueues[q["label"].(string)] = true
		if q["broker"] != "sqs" {
			t.Errorf("queue %v broker = %v, want sqs", q["label"], q["broker"])
		}
	}
	for _, want := range []string{"sqs:dlq", "sqs:main"} {
		if !gotQueues[want] {
			t.Errorf("IaC-declared queue %q missing from topology queues: %+v", want, queues)
		}
	}

	foundTopic := false
	for _, tp := range topics {
		if tp["label"] == "sns:order-events" {
			foundTopic = true
			if tp["broker"] != "sns" {
				t.Errorf("topic broker = %v, want sns", tp["broker"])
			}
		}
	}
	if !foundTopic {
		t.Errorf("IaC-declared SNS topic missing from topology topics: %+v", topics)
	}
}
