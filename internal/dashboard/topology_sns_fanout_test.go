package dashboard

// topology_sns_fanout_test.go — #1596. Verifies that the IaC-declared
// SNS→multi-SQS fan-out (one SNS topic with SQS subscribers declared across
// CDK / Terraform / CloudFormation) renders in the topology surface as a
// single SNS topic node with 3+ SQS subscribers. Mirrors the entity/edge
// shape emitted by internal/engine/applyIaCSNSEdges (topic = SCOPE.MessageTopic
// broker=sns; each SQS subscriber = SUBSCRIBES_TO edge FromID=SQS-queue,
// ToID=SNS-topic).

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func TestCollectTopology_SNSMultiIaCFanOut(t *testing.T) {
	doc := &graph.Document{
		Repo: "polyglot-platform",
		Entities: []graph.Entity{
			graph.Entity{ID: "topic-oe", Name: "sns:order-events", Kind: "SCOPE.MessageTopic"}.WithProperties(map[string]string{"broker": "sns", "pattern_type": "iac_sns_fanout"}),
			graph.Entity{ID: "q-analytics", Name: "sqs:order-events-analytics", Kind: "SCOPE.Queue"}.WithProperties(map[string]string{"broker": "sqs", "pattern_type": "iac_sns_fanout", "iac_tool": "cdk"}),
			graph.Entity{ID: "q-audit", Name: "sqs:order-events-audit", Kind: "SCOPE.Queue"}.WithProperties(map[string]string{"broker": "sqs", "pattern_type": "iac_sns_fanout", "iac_tool": "terraform"}),
			graph.Entity{ID: "q-fraud", Name: "sqs:order-events-fraud", Kind: "SCOPE.Queue"}.WithProperties(map[string]string{"broker": "sqs", "pattern_type": "iac_sns_fanout", "iac_tool": "cloudformation"}),
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "s1", FromID: "q-analytics", ToID: "topic-oe", Kind: "SUBSCRIBES_TO"}.WithProperties(map[string]string{"iac_tool": "cdk"}),
			graph.Relationship{ID: "s2", FromID: "q-audit", ToID: "topic-oe", Kind: "SUBSCRIBES_TO"}.WithProperties(map[string]string{"iac_tool": "terraform"}),
			graph.Relationship{ID: "s3", FromID: "q-fraud", ToID: "topic-oe", Kind: "SUBSCRIBES_TO"}.WithProperties(map[string]string{"iac_tool": "cloudformation"}),
		},
	}
	grp := &DashGroup{
		Name:  "g",
		Repos: map[string]*DashRepo{"polyglot-platform": {Slug: "polyglot-platform", Doc: doc}},
	}

	topics, _, _, _ := collectTopology(grp)

	var oe map[string]any
	for _, topic := range topics {
		if topic["label"] == "sns:order-events" {
			oe = topic
			break
		}
	}
	if oe == nil {
		t.Fatalf("order-events SNS topic not found in topology topics: %+v", topics)
	}
	if oe["broker"] != "sns" {
		t.Errorf("broker = %v, want sns", oe["broker"])
	}
	consumers, _ := oe["consumers"].([]string)
	if len(consumers) != 3 {
		t.Fatalf("want 3 SQS subscribers on order-events topic, got %d (%v)", len(consumers), consumers)
	}
}
