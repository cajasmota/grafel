// Tests for the Inngest producer → event-topic edge pass added by #5482
// (epic #5479, Phase 2).
//
// Covers:
//   - producer side: inngest.send({ name: "user/created" }) → PUBLISHES_TO
//     from the enclosing function to SCOPE.MessageTopic:user/created (the
//     #5481 event topic), with no re-emitted topic entity.
//   - multi-send: an array of payloads yields one edge per distinct event name.
//   - step.sendEvent inside a handler attributes to the step receiver.
//   - enclosing-scope-unresolved fallback: a top-level send attributes to
//     Function:module rather than dropping the edge.
//   - non-Inngest files emit nothing (pre-filter gate).
//   - non-JS/TS languages are skipped.
//
// And for the consumer / trigger edge added by #5483:
//   - an event-triggered createFunction → SUBSCRIBES_TO its event topic.
//   - a cron-triggered createFunction → no SUBSCRIBES_TO edge.
//   - end-to-end workflow chain: function A sends event X and function B is
//     triggered by X, yielding A →PUBLISHES_TO→ X →SUBSCRIBES_TO→ B.
package engine

import (
	"testing"
)

// runInngestDetect is a lightweight in-process driver mirroring runBullMQDetect.
func runInngestDetect(t *testing.T, lang, path, src string) ([]entityResult, []relResult) {
	t.Helper()
	res := applyInngestEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	out := make([]entityResult, 0, len(res.Entities))
	for _, e := range res.Entities {
		out = append(out, entityResult{kind: e.Kind, name: e.Name, props: e.Properties})
	}
	relOut := make([]relResult, 0, len(res.Relationships))
	for _, r := range res.Relationships {
		relOut = append(relOut, relResult{from: r.FromID, to: r.ToID, kind: r.Kind, props: r.Properties})
	}
	return out, relOut
}

func relTo(rels []relResult, kind, to string) *relResult {
	for i := range rels {
		if rels[i].kind == kind && rels[i].to == to {
			return &rels[i]
		}
	}
	return nil
}

func TestInngest_Producer_SendEmitsPublishesTo(t *testing.T) {
	src := `import { inngest } from './client';

export async function createUser(data) {
  await inngest.send({ name: "user/created", data });
}
`
	ents, rels := runInngestDetect(t, "typescript", "producer.ts", src)

	// This pass is edge-only; the topic entity belongs to #5481's extractor.
	if len(ents) != 0 {
		t.Errorf("inngest edge pass should not emit entities, got %+v", ents)
	}

	r := relTo(rels, "PUBLISHES_TO", "SCOPE.MessageTopic:user/created")
	if r == nil {
		t.Fatalf("expected PUBLISHES_TO → SCOPE.MessageTopic:user/created, got %+v", rels)
	}
	if r.from != "Function:createUser" {
		t.Errorf("PUBLISHES_TO FromID = %q, want Function:createUser", r.from)
	}
	if r.props["framework"] != "inngest" || r.props["event"] != "user/created" {
		t.Errorf("edge props = %+v, want framework=inngest event=user/created", r.props)
	}
	if r.props["topic_id"] != "event:user/created" {
		t.Errorf("edge topic_id = %q, want event:user/created", r.props["topic_id"])
	}
}

func TestInngest_Producer_MultiSend(t *testing.T) {
	src := `import { inngest } from 'inngest';

export async function fanOut() {
  await inngest.send([
    { name: "user/created", data: {} },
    { name: "email/queued", data: {} },
  ]);
}
`
	_, rels := runInngestDetect(t, "typescript", "fanout.ts", src)

	if relTo(rels, "PUBLISHES_TO", "SCOPE.MessageTopic:user/created") == nil {
		t.Errorf("expected PUBLISHES_TO → user/created, got %+v", rels)
	}
	if relTo(rels, "PUBLISHES_TO", "SCOPE.MessageTopic:email/queued") == nil {
		t.Errorf("expected PUBLISHES_TO → email/queued, got %+v", rels)
	}
	if got := len(relsByKind(rels, "PUBLISHES_TO")); got != 2 {
		t.Errorf("expected 2 PUBLISHES_TO edges from multi-send, got %d (%+v)", got, rels)
	}
}

func TestInngest_StepSendEvent(t *testing.T) {
	src := `import { inngest } from './client';

export const fn = inngest.createFunction(
  { id: "process-order" },
  { event: "order/placed" },
  async ({ event, step }) => {
    await step.sendEvent("notify", { name: "order/notified", data: event.data });
  },
);
`
	_, rels := runInngestDetect(t, "typescript", "step.ts", src)

	if relTo(rels, "PUBLISHES_TO", "SCOPE.MessageTopic:order/notified") == nil {
		t.Fatalf("expected PUBLISHES_TO → order/notified from step.sendEvent, got %+v", rels)
	}
}

// TestInngest_EnclosingScopeFallback asserts a top-level `inngest.send` with no
// enclosing function attributes to Function:module rather than dropping the
// edge.
func TestInngest_EnclosingScopeFallback(t *testing.T) {
	src := `import { inngest } from './client';
inngest.send({ name: "app/bootstrapped" });
`
	_, rels := runInngestDetect(t, "javascript", "bootstrap.js", src)

	r := relTo(rels, "PUBLISHES_TO", "SCOPE.MessageTopic:app/bootstrapped")
	if r == nil {
		t.Fatalf("expected PUBLISHES_TO → app/bootstrapped, got %+v", rels)
	}
	if r.from != "Function:module" {
		t.Errorf("unresolved enclosing scope FromID = %q, want Function:module", r.from)
	}
}

func TestInngest_NonInngestFileEmitsNothing(t *testing.T) {
	src := `const client = makeClient();
client.send({ name: "foo" });
`
	ents, rels := runInngestDetect(t, "typescript", "unrelated.ts", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("non-Inngest file should emit nothing, got %d entities %d rels", len(ents), len(rels))
	}
}

func TestInngest_NonJSLanguageSkipped(t *testing.T) {
	src := `inngest.send({ name: "user/created" })`
	ents, rels := runInngestDetect(t, "python", "x.py", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("non-JS/TS language should be skipped, got %d entities %d rels", len(ents), len(rels))
	}
}

// TestInngest_Consumer_EventTriggerEmitsSubscribesTo asserts an event-triggered
// createFunction emits a SUBSCRIBES_TO edge Function:<id> → its event topic,
// reusing the same consumer edge kind/direction BullMQ uses (#5483).
func TestInngest_Consumer_EventTriggerEmitsSubscribesTo(t *testing.T) {
	src := `import { inngest } from './client';

export const fn = inngest.createFunction(
  { id: "send-welcome" },
  { event: "user/created" },
  async ({ event, step }) => { /* ... */ },
);
`
	ents, rels := runInngestDetect(t, "typescript", "consumer.ts", src)

	// Edge-only pass; the Function/topic entities belong to #5480/#5481.
	if len(ents) != 0 {
		t.Errorf("inngest edge pass should not emit entities, got %+v", ents)
	}

	r := relTo(rels, "SUBSCRIBES_TO", "SCOPE.MessageTopic:user/created")
	if r == nil {
		t.Fatalf("expected SUBSCRIBES_TO → SCOPE.MessageTopic:user/created, got %+v", rels)
	}
	if r.from != "Function:send-welcome" {
		t.Errorf("SUBSCRIBES_TO FromID = %q, want Function:send-welcome", r.from)
	}
	if r.props["framework"] != "inngest" || r.props["event"] != "user/created" {
		t.Errorf("edge props = %+v, want framework=inngest event=user/created", r.props)
	}
	if r.props["topic_id"] != "event:user/created" {
		t.Errorf("edge topic_id = %q, want event:user/created", r.props["topic_id"])
	}
	if r.props["provenance"] != "INFERRED_FROM_INNGEST_CREATE_FUNCTION" {
		t.Errorf("edge provenance = %q, want INFERRED_FROM_INNGEST_CREATE_FUNCTION", r.props["provenance"])
	}
}

// TestInngest_Consumer_CronTriggerEmitsNoEdge asserts a cron-triggered function
// (no `event:`) is a scheduled job and emits no SUBSCRIBES_TO edge.
func TestInngest_Consumer_CronTriggerEmitsNoEdge(t *testing.T) {
	src := `import { inngest } from './client';

export const nightly = inngest.createFunction(
  { id: "nightly-report" },
  { cron: "0 0 * * *" },
  async () => { /* ... */ },
);
`
	_, rels := runInngestDetect(t, "typescript", "cron.ts", src)

	if got := len(relsByKind(rels, "SUBSCRIBES_TO")); got != 0 {
		t.Errorf("cron-triggered function should emit no SUBSCRIBES_TO edge, got %d (%+v)", got, rels)
	}
}

// TestInngest_WorkflowChain asserts the end-to-end event→function→event chain:
// function A sends event X (PUBLISHES_TO X) and function B is triggered by X
// (SUBSCRIBES_TO X). Together these form A →PUBLISHES_TO→ X →SUBSCRIBES_TO→ B,
// the workflow chain the topology renderer stitches via the shared topic.
func TestInngest_WorkflowChain(t *testing.T) {
	src := `import { inngest } from 'inngest';

// Producer: function A emits event X.
export async function emitX() {
  await inngest.send({ name: "order/placed" });
}

// Consumer: function B is triggered by event X.
export const handleX = inngest.createFunction(
  { id: "handle-order" },
  { event: "order/placed" },
  async ({ event }) => { /* ... */ },
);
`
	_, rels := runInngestDetect(t, "typescript", "workflow.ts", src)

	pub := relTo(rels, "PUBLISHES_TO", "SCOPE.MessageTopic:order/placed")
	if pub == nil {
		t.Fatalf("expected producer PUBLISHES_TO → order/placed, got %+v", rels)
	}
	if pub.from != "Function:emitX" {
		t.Errorf("PUBLISHES_TO FromID = %q, want Function:emitX", pub.from)
	}

	sub := relTo(rels, "SUBSCRIBES_TO", "SCOPE.MessageTopic:order/placed")
	if sub == nil {
		t.Fatalf("expected consumer SUBSCRIBES_TO → order/placed, got %+v", rels)
	}
	if sub.from != "Function:handle-order" {
		t.Errorf("SUBSCRIBES_TO FromID = %q, want Function:handle-order", sub.from)
	}

	// Both edges share the same topic stub — the chain join point.
	if pub.to != sub.to {
		t.Errorf("workflow chain not joined: PUBLISHES_TO→%q vs SUBSCRIBES_TO→%q", pub.to, sub.to)
	}
}
