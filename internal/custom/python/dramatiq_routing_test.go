package python_test

// dramatiq_routing_test.go — proving fixtures for the dramatiq task_routing
// extractor. Dedicated to issue #3193.
//
// Covers queue->actor routing via:
//   - @dramatiq.actor(queue_name="...") decorator routing
//   - actor.send_with_options(queue_name="...") explicit dispatch override
//
// Kept distinct from broker_binding_test.go (#3074, broker/retry) and the
// celery/rq task-routing tests so the task_routing capability is proved on its
// own fixture.

import "testing"

// findRouting returns the first task_routing entity matching name, or nil.
func findRouting(result []extractResult, name string) *extractResult {
	for i := range result {
		if result[i].Subtype == "task_routing" && result[i].Name == name {
			return &result[i]
		}
	}
	return nil
}

// ============================================================================
// Golden fixture — full queue->actor routing surface
// ============================================================================

func TestDramatiq_TaskRouting_GoldenFixture(t *testing.T) {
	src := fixtureSchema(t, "dramatiq_task_routing.py")
	ents := extract(t, "python_dramatiq", src)

	// Decorator routing: @dramatiq.actor(queue_name="emails") def send_email
	emails := findRouting(ents, "send_email")
	if emails == nil {
		t.Fatal("expected task_routing entity for send_email (queue_name=emails)")
	}
	if emails.Props["framework"] != "dramatiq" {
		t.Errorf("framework: got %q", emails.Props["framework"])
	}
	if emails.Props["pattern_type"] != "actor_queue" {
		t.Errorf("pattern_type: got %q", emails.Props["pattern_type"])
	}
	if emails.Props["queue_name"] != "emails" {
		t.Errorf("queue_name: got %q want emails", emails.Props["queue_name"])
	}
	if emails.Props["actor"] != "send_email" {
		t.Errorf("actor: got %q", emails.Props["actor"])
	}
	if emails.Props["edge_kind"] != "ROUTES_TO" {
		t.Errorf("edge_kind: got %q", emails.Props["edge_kind"])
	}
	if emails.Kind != "SCOPE.Pattern" {
		t.Errorf("kind: got %q", emails.Kind)
	}

	// Decorator routing on a second actor (order of decorator args differs).
	reports := findRouting(ents, "generate_report")
	if reports == nil {
		t.Fatal("expected task_routing entity for generate_report (queue_name=reports)")
	}
	if reports.Props["queue_name"] != "reports" || reports.Props["pattern_type"] != "actor_queue" {
		t.Errorf("generate_report routing props wrong: %+v", reports.Props)
	}

	// Explicit dispatch override:
	// generate_report.send_with_options(queue_name="reports_priority")
	override := findRouting(ents, "generate_report.send_with_options")
	if override == nil {
		t.Fatal("expected task_routing entity for send_with_options queue override")
	}
	if override.Props["pattern_type"] != "send_queue_override" {
		t.Errorf("override pattern_type: got %q", override.Props["pattern_type"])
	}
	if override.Props["queue_name"] != "reports_priority" {
		t.Errorf("override queue_name: got %q want reports_priority", override.Props["queue_name"])
	}
	if override.Props["actor_ref"] != "generate_report" {
		t.Errorf("override actor_ref: got %q", override.Props["actor_ref"])
	}

	// Negative: a bare @dramatiq.actor with no queue_name must NOT emit a
	// task_routing marker.
	if r := findRouting(ents, "default_queue_task"); r != nil {
		t.Errorf("unexpected task_routing entity for bare actor: %+v", r.Props)
	}
}

// ============================================================================
// Focused unit cases
// ============================================================================

func TestDramatiq_TaskRouting_DecoratorQueueName(t *testing.T) {
	src := `import dramatiq

@dramatiq.actor(queue_name="billing")
def charge_card(amount):
    pass
`
	ents := extract(t, "python_dramatiq", src)
	r := findRouting(ents, "charge_card")
	if r == nil {
		t.Fatal("expected task_routing for charge_card")
	}
	if r.Props["queue_name"] != "billing" {
		t.Errorf("queue_name: got %q want billing", r.Props["queue_name"])
	}
}

func TestDramatiq_TaskRouting_SendWithOptionsOverride(t *testing.T) {
	src := `email_task.send_with_options(args=("x",), queue_name="urgent")
`
	ents := extract(t, "python_dramatiq", src)
	r := findRouting(ents, "email_task.send_with_options")
	if r == nil {
		t.Fatal("expected task_routing for send_with_options override")
	}
	if r.Props["queue_name"] != "urgent" || r.Props["actor_ref"] != "email_task" {
		t.Errorf("override props wrong: %+v", r.Props)
	}
}

func TestDramatiq_TaskRouting_NoQueueNameNoMarker(t *testing.T) {
	src := `import dramatiq

@dramatiq.actor(max_retries=2)
def plain_task():
    pass

plain_task.send()
plain_task.send_with_options(args=(1,))
`
	ents := extract(t, "python_dramatiq", src)
	for _, e := range ents {
		if e.Subtype == "task_routing" {
			t.Errorf("unexpected task_routing entity when no queue_name present: %+v", e.Props)
		}
	}
}
