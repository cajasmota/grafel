package javascript_test

// Tests for issue #5480 (epic #5479, Inngest Phase 1, item 1): the Inngest
// durable-function extractor (custom_js_inngest). Proves that each
// `inngest.createFunction(...)` call site yields one SCOPE.Function entity
// named after the config `id`/`name`, with the trigger event captured as a
// property. Scope is the ENTITY only — EMITS/TRIGGERS edges are #5482/#5483.
//
// These are the proving fixtures cited by the registry record `msg.inngest`.

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"

	// Blank import to trigger init() registration of the extractor.
	_ "github.com/cajasmota/grafel/internal/custom/javascript"
)

// extractInngest runs the inngest extractor and returns full EntityRecords so
// tests can assert the trigger_event property (the shared entitySummary helper
// only carries Kind/Subtype/Name).
func extractInngest(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_js_inngest")
	if !ok {
		t.Fatal("extractor custom_js_inngest not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "typescript", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

func findFunc(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == string(types.EntityKindFunction) && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// findTopic returns the SCOPE.MessageTopic entity for an event name, or nil.
func findTopic(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == string(types.EntityKindMessageTopic) && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func countTopics(ents []types.EntityRecord) int {
	n := 0
	for i := range ents {
		if ents[i].Kind == string(types.EntityKindMessageTopic) {
			n++
		}
	}
	return n
}

// Modern object-config signature:
//
//	inngest.createFunction({ id, name }, { event }, handler)
func TestInngestCreateFunctionObjectSignature(t *testing.T) {
	src := `
import { Inngest } from "inngest";
const inngest = new Inngest({ id: "my-app" });

export const syncUser = inngest.createFunction(
  { id: "sync-user", name: "Sync User" },
  { event: "user/created" },
  async ({ event, step }) => {
    await step.run("sync", () => doSync(event.data));
  }
);
`
	ents := extractInngest(t, "src/inngest/syncUser.ts", src)
	fn := findFunc(ents, "sync-user")
	if fn == nil {
		t.Fatalf("expected SCOPE.Function entity 'sync-user', got %+v", ents)
	}
	if fn.Subtype != "inngest_function" {
		t.Errorf("expected subtype inngest_function, got %q", fn.Subtype)
	}
	if got := fn.Properties["trigger_event"]; got != "user/created" {
		t.Errorf("expected trigger_event=user/created, got %q", got)
	}
	if got := fn.Properties["framework"]; got != "inngest" {
		t.Errorf("expected framework=inngest, got %q", got)
	}
	if got := fn.Properties["function_id"]; got != "sync-user" {
		t.Errorf("expected function_id=sync-user, got %q", got)
	}
}

// Older positional signature: a bare string id as the first argument is NOT
// used (Inngest never accepted a bare string id), but the historical
// positional trigger form passes the trigger object as the 2nd argument — the
// id still lives in the config object. This asserts the same config-object id
// resolves and the acceptance fixture from #5480 works verbatim.
func TestInngestCreateFunctionAcceptanceFixture(t *testing.T) {
	// The exact acceptance shape from the ticket.
	src := `
import { inngest } from "./client";
inngest.createFunction({ id: "sync-user" }, { event: "user/created" }, async () => {});
`
	ents := extractInngest(t, "functions.ts", src)
	// Exactly one Function entity (the consumer) ...
	funcs := 0
	for i := range ents {
		if ents[i].Kind == string(types.EntityKindFunction) {
			funcs++
		}
	}
	if funcs != 1 {
		t.Fatalf("expected exactly one Function entity, got %d: %+v", funcs, ents)
	}
	fn := findFunc(ents, "sync-user")
	if fn == nil {
		t.Fatalf("expected SCOPE.Function entity 'sync-user'")
	}
	if got := fn.Properties["trigger_event"]; got != "user/created" {
		t.Errorf("expected trigger_event=user/created, got %q", got)
	}
	// ... plus one MessageTopic for the triggered event (#5481).
	if countTopics(ents) != 1 {
		t.Errorf("expected exactly one MessageTopic, got %d: %+v", countTopics(ents), ents)
	}
	if findTopic(ents, "user/created") == nil {
		t.Errorf("expected MessageTopic 'user/created'")
	}
}

// Multiple definitions in one file must not bleed ids/events into one another.
func TestInngestMultipleFunctionsNoBleed(t *testing.T) {
	src := `
import { inngest } from "inngest";
export const a = inngest.createFunction({ id: "fn-a" }, { event: "a/event" }, async () => {});
export const b = inngest.createFunction({ id: "fn-b" }, { event: "b/event" }, async () => {});
`
	ents := extractInngest(t, "multi.ts", src)
	a := findFunc(ents, "fn-a")
	b := findFunc(ents, "fn-b")
	if a == nil || b == nil {
		t.Fatalf("expected both fn-a and fn-b, got %+v", ents)
	}
	if a.Properties["trigger_event"] != "a/event" {
		t.Errorf("fn-a trigger bled: got %q", a.Properties["trigger_event"])
	}
	if b.Properties["trigger_event"] != "b/event" {
		t.Errorf("fn-b trigger bled: got %q", b.Properties["trigger_event"])
	}
}

// Cron-triggered functions carry a cron attribute instead of an event.
func TestInngestCronTrigger(t *testing.T) {
	src := `
import { inngest } from "inngest";
inngest.createFunction({ id: "nightly" }, { cron: "0 0 * * *" }, async () => {});
`
	ents := extractInngest(t, "cron.ts", src)
	fn := findFunc(ents, "nightly")
	if fn == nil {
		t.Fatal("expected SCOPE.Function entity 'nightly'")
	}
	if got := fn.Properties["trigger_cron"]; got != "0 0 * * *" {
		t.Errorf("expected trigger_cron set, got %q", got)
	}
	if got := fn.Properties["trigger_type"]; got != "cron" {
		t.Errorf("expected trigger_type=cron, got %q", got)
	}
}

// No inngest import → no entities, even if a `.createFunction(` happens to
// appear (guards against misattributing another library's API).
func TestInngestNoImportNoMatch(t *testing.T) {
	src := `const x = other.createFunction({ id: "nope" });`
	ents := extractInngest(t, "unrelated.ts", src)
	if len(ents) != 0 {
		t.Errorf("expected no entities without inngest import, got %d", len(ents))
	}
}

func TestInngestNoMatch(t *testing.T) {
	ents := extractInngest(t, "plain.ts", "const x = 1;")
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// #5481: each DISTINCT event name referenced in a createFunction trigger or an
// inngest.send({ name }) producer call yields one SCOPE.MessageTopic entity,
// deduped by event name, framework=inngest.
func TestInngestEventTopics(t *testing.T) {
	src := `
import { inngest } from "./client";

inngest.createFunction({ id: "sync-user" }, { event: "user/created" }, async () => {});

export async function fanOut(id: string) {
  // Same event referenced again as a producer — must dedupe to one topic.
  await inngest.send({ name: "user/created", data: { id } });
  await inngest.send({ name: "user/deleted", data: { id } });
}
`
	ents := extractInngest(t, "events.ts", src)

	if got := countTopics(ents); got != 2 {
		t.Fatalf("expected 2 distinct MessageTopic entities, got %d: %+v", got, ents)
	}
	for _, name := range []string{"user/created", "user/deleted"} {
		tp := findTopic(ents, name)
		if tp == nil {
			t.Fatalf("expected MessageTopic %q, got %+v", name, ents)
		}
		if tp.Subtype != "inngest" {
			t.Errorf("topic %q: expected subtype inngest, got %q", name, tp.Subtype)
		}
		if got := tp.Properties["framework"]; got != "inngest" {
			t.Errorf("topic %q: expected framework=inngest, got %q", name, got)
		}
		if got := tp.Properties["topic_id"]; got != "event:"+name {
			t.Errorf("topic %q: expected topic_id=event:%s, got %q", name, name, got)
		}
	}
}

// #5481: typed event-schema definitions — the quoted keys of a
// new EventSchemas().fromRecord<{ ... }>() type record become MessageTopics.
func TestInngestEventSchemaTopics(t *testing.T) {
	src := `
import { Inngest, EventSchemas } from "inngest";

export const inngest = new Inngest({
  id: "app",
  schemas: new EventSchemas().fromRecord<{
    "user/created": { data: { id: string } };
    "order/placed": { data: { total: number } };
  }>(),
});
`
	ents := extractInngest(t, "schemas.ts", src)
	for _, name := range []string{"user/created", "order/placed"} {
		tp := findTopic(ents, name)
		if tp == nil {
			t.Fatalf("expected MessageTopic %q from schema, got %+v", name, ents)
		}
		if got := tp.Properties["framework"]; got != "inngest" {
			t.Errorf("topic %q: expected framework=inngest, got %q", name, got)
		}
	}
}

// #5481: the send() producer is attribution-gated just like createFunction —
// no inngest import and a non-inngest receiver yields no topics.
func TestInngestSendNoImportNoTopic(t *testing.T) {
	src := `const x = mq.send({ name: "user/created" });`
	ents := extractInngest(t, "unrelated.ts", src)
	if countTopics(ents) != 0 {
		t.Errorf("expected no topics without inngest attribution, got %+v", ents)
	}
}
