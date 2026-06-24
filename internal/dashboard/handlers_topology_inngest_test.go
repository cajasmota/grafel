package dashboard

// handlers_topology_inngest_test.go — #5485 (epic #5479, headline)
//
// The topology screen must reflect the Inngest async-workflow graph: events as
// MessageTopic nodes, functions wired in via PUBLISHES_TO / SUBSCRIBES_TO, and
// the event→function→event chain legible. Inngest entities reuse grafel's pub-
// sub kinds, so these tests assert the Inngest-specific labeling and the step
// detail wiring that #5485 adds on top of the existing renderer.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// inngestWorkflowDoc builds a two-function Inngest workflow chained by an
// intermediate event:
//
//	order/created → send-email → email/sent → archive
//
// send-email is triggered by (SUBSCRIBES_TO) order/created and emits
// (PUBLISHES_TO) email/sent; archive is triggered by email/sent. send-email has
// two durable steps (a run + a waitForEvent).
func inngestWorkflowDoc() *graph.Document {
	return &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{ID: "topic:order.created", Name: "order/created", Kind: "SCOPE.MessageTopic",
				SourceFile: "events.ts", StartLine: 1,
				Properties: map[string]string{"framework": "inngest", "topic_id": "event:order/created"}},
			{ID: "topic:email.sent", Name: "email/sent", Kind: "SCOPE.MessageTopic",
				SourceFile: "events.ts", StartLine: 2,
				Properties: map[string]string{"framework": "inngest", "topic_id": "event:email/sent"}},
			{ID: "fn:send-email", Name: "send-email", Kind: "SCOPE.Function",
				SourceFile: "fns.ts", StartLine: 10,
				Properties: map[string]string{"framework": "inngest", "function_id": "send-email"}},
			{ID: "fn:archive", Name: "archive", Kind: "SCOPE.Function",
				SourceFile: "fns.ts", StartLine: 40,
				Properties: map[string]string{"framework": "inngest", "function_id": "archive"}},
			// #5484 durable steps of send-email.
			{ID: "step:fetch", Name: "fetch-user", Kind: "SCOPE.Operation",
				SourceFile: "fns.ts", StartLine: 12,
				Properties: map[string]string{"framework": "inngest", "step_kind": "run", "step_id": "fetch-user"}},
			{ID: "step:wait", Name: "await-confirm", Kind: "SCOPE.Operation",
				SourceFile: "fns.ts", StartLine: 20,
				Properties: map[string]string{"framework": "inngest", "step_kind": "waitForEvent", "step_id": "await-confirm", "wait_event": "user/confirmed"}},
		},
		Relationships: []graph.Relationship{
			// send-email triggered by order/created, emits email/sent.
			{ID: "e1", FromID: "fn:send-email", ToID: "topic:order.created", Kind: "SUBSCRIBES_TO",
				Properties: map[string]string{"framework": "inngest"}},
			{ID: "e2", FromID: "fn:send-email", ToID: "topic:email.sent", Kind: "PUBLISHES_TO",
				Properties: map[string]string{"framework": "inngest"}},
			// archive triggered by email/sent.
			{ID: "e3", FromID: "fn:archive", ToID: "topic:email.sent", Kind: "SUBSCRIBES_TO",
				Properties: map[string]string{"framework": "inngest"}},
			// CONTAINS: send-email → its two steps.
			{ID: "c1", FromID: "fn:send-email", ToID: "step:fetch", Kind: "CONTAINS"},
			{ID: "c2", FromID: "fn:send-email", ToID: "step:wait", Kind: "CONTAINS"},
		},
	}
}

func inngestGroup() *DashGroup {
	return &DashGroup{
		Name:  "g",
		Repos: map[string]*DashRepo{"svc": {Slug: "svc", Doc: inngestWorkflowDoc()}},
	}
}

// TestCollectTopology_InngestWorkflowChain asserts the event→function→event
// workflow renders: both events appear as topic nodes labeled with the inngest
// broker_canonical, and the PUBLISHES_TO/SUBSCRIBES_TO edges form the chain via
// each topic's producers/consumers (the function names).
func TestCollectTopology_InngestWorkflowChain(t *testing.T) {
	resp := collectTopologyResponse(inngestGroup(), "", nil)

	if len(resp.Topics) != 2 {
		t.Fatalf("expected 2 Inngest event topics, got %d", len(resp.Topics))
	}

	byLabel := map[string]map[string]any{}
	for _, tp := range resp.Topics {
		byLabel[tp["label"].(string)] = tp
		if got := tp["broker_canonical"]; got != "inngest" {
			t.Errorf("topic %v broker_canonical = %v, want inngest", tp["label"], got)
		}
	}

	// order/created → send-email (consumer/trigger), no producer.
	oc := byLabel["order/created"]
	if oc == nil {
		t.Fatal("missing order/created topic")
	}
	if cs, _ := oc["consumers"].([]string); len(cs) != 1 || cs[0] != "svc::fn:send-email" {
		t.Errorf("order/created consumers = %v, want [svc::fn:send-email]", oc["consumers"])
	}

	// email/sent: produced by send-email, consumed by archive — the chain link.
	es := byLabel["email/sent"]
	if es == nil {
		t.Fatal("missing email/sent topic")
	}
	if ps, _ := es["producers"].([]string); len(ps) != 1 || ps[0] != "svc::fn:send-email" {
		t.Errorf("email/sent producers = %v, want [svc::fn:send-email]", es["producers"])
	}
	if cs, _ := es["consumers"].([]string); len(cs) != 1 || cs[0] != "svc::fn:archive" {
		t.Errorf("email/sent consumers = %v, want [svc::fn:archive]", es["consumers"])
	}

	// A broker_groups band must be present for the inngest canonical.
	foundBand := false
	for _, bg := range resp.BrokerGroups {
		if bg.Broker == "inngest" {
			foundBand = true
		}
	}
	if !foundBand {
		t.Error("expected an 'inngest' broker_groups band")
	}
}

// TestBrokerCanonical_Inngest asserts framework=inngest (with no broker name)
// maps to the inngest canonical instead of "unknown".
func TestBrokerCanonical_Inngest(t *testing.T) {
	if got := brokerCanonical("", "inngest"); got != "inngest" {
		t.Errorf("brokerCanonical(\"\", \"inngest\") = %q, want inngest", got)
	}
}

// TestBuildTopicDetail_InngestSteps asserts the topology detail panel surfaces an
// Inngest function's durable step structure (#5484 inngest_step children) and the
// inngest framework label, on the chain-link event email/sent.
func TestBuildTopicDetail_InngestSteps(t *testing.T) {
	grp := inngestGroup()
	detail, found := buildTopicDetail(grp, "", "svc::topic:email.sent")
	if !found {
		t.Fatal("email/sent topic detail not found")
	}
	if detail.Framework != "inngest" {
		t.Errorf("detail.Framework = %q, want inngest", detail.Framework)
	}

	// send-email is the producer; it must carry its two durable steps.
	var sendEmail *topicEntityRecord
	for i := range detail.Producers {
		if detail.Producers[i].Name == "send-email" {
			sendEmail = &detail.Producers[i]
		}
	}
	if sendEmail == nil {
		t.Fatalf("send-email not found among producers: %+v", detail.Producers)
	}
	if sendEmail.Framework != "inngest" {
		t.Errorf("send-email.Framework = %q, want inngest", sendEmail.Framework)
	}
	if len(sendEmail.InngestSteps) != 2 {
		t.Fatalf("send-email InngestSteps = %d, want 2 (%+v)", len(sendEmail.InngestSteps), sendEmail.InngestSteps)
	}
	// Steps are in source order: run(fetch-user) then waitForEvent(await-confirm).
	if sendEmail.InngestSteps[0].StepKind != "run" || sendEmail.InngestSteps[0].StepID != "fetch-user" {
		t.Errorf("step[0] = %+v, want run/fetch-user", sendEmail.InngestSteps[0])
	}
	if sendEmail.InngestSteps[1].StepKind != "waitForEvent" || sendEmail.InngestSteps[1].WaitEvent != "user/confirmed" {
		t.Errorf("step[1] = %+v, want waitForEvent wait_event=user/confirmed", sendEmail.InngestSteps[1])
	}

	// archive (consumer of email/sent) has no steps — InngestSteps must be empty.
	for _, c := range detail.Consumers {
		if c.Name == "archive" && len(c.InngestSteps) != 0 {
			t.Errorf("archive should have 0 steps, got %d", len(c.InngestSteps))
		}
	}
}
