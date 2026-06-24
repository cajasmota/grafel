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
