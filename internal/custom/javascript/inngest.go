package javascript

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extreg.Register("custom_js_inngest", &inngestExtractor{})
}

// inngestExtractor recognises Inngest durable-function definitions in
// JavaScript / TypeScript source. Inngest's `inngest.createFunction(...)`
// (or `<client>.createFunction(...)`) registers an event-triggered async
// workflow function — conceptually the consumer side of an event, the
// Inngest analogue of a BullMQ Worker / serverless function. Each call site
// becomes one SCOPE.Function entity named after the function's id/name, with
// the trigger event captured as a property.
//
// Scope (epic #5479, ticket #5480): the consumer SCOPE.Function ENTITY. The
// EMITS / TRIGGERS edges that wire the event name to producers/topics are later
// tickets (#5482/#5483/#5484); here the event name is recorded as an attribute.
//
// Ticket #5481 (epic #5479, Phase 1 item 2) adds, on top of the function
// entities, one SCOPE.MessageTopic entity per DISTINCT Inngest event name —
// the Inngest event analogue of a BullMQ/Kafka topic. Event names are harvested
// from createFunction `{ event: "..." }` triggers, `<client>.send({ name })`
// producer payloads, and typed `new EventSchemas().fromRecord<{ "name": ... }>()`
// schema definitions. The topic is deduped by event name; the EMITS/TRIGGERS
// edges that wire the topic to its producers/consumers remain #5482/#5483.
type inngestExtractor struct{}

func (e *inngestExtractor) Language() string { return "custom_js_inngest" }

// q matches a single-, double-, or back-quoted string literal, capturing the
// inner value. Kept as one shared fragment so every key regex agrees on what a
// JS/TS string literal looks like.
const inngestStr = "['\"`]([^'\"`]+)['\"`]"

var (
	// Gate: only run when the file actually imports / requires inngest, so a
	// stray `.createFunction(` from another library is not misattributed.
	reInngestImport = regexp.MustCompile("(?:from\\s+['\"`]inngest['\"`]|require\\(\\s*['\"`]inngest['\"`]\\s*\\))")

	// `<recv>.createFunction(` — capture the receiver (usually `inngest`).
	reInngestCreateFunction = regexp.MustCompile(`([A-Za-z_$][A-Za-z0-9_$.]*)\.createFunction\s*\(`)

	// Config object `id` / `name` keys. The first config argument to
	// createFunction; `id` is preferred, `name` is the fallback.
	reInngestID   = regexp.MustCompile(`\bid\s*:\s*` + inngestStr)
	reInngestName = regexp.MustCompile(`\bname\s*:\s*` + inngestStr)

	// Trigger event name. Modern form is a `{ event: "..." }` trigger object;
	// the older positional form passes the same shape as the 2nd argument.
	reInngestEvent = regexp.MustCompile(`\bevent\s*:\s*` + inngestStr)
	// Cron-triggered functions use `{ cron: "..." }` instead of an event.
	reInngestCron = regexp.MustCompile(`\bcron\s*:\s*` + inngestStr)

	// `<recv>.send(` and `<recv>.sendEvent(` — the producer side. The event
	// name lives in a `{ name: "..." }` payload (single event) or an array of
	// such payloads.
	reInngestSend = regexp.MustCompile(`([A-Za-z_$][A-Za-z0-9_$.]*)\.(?:send|sendEvent)\s*\(`)
	// Event payload name key inside a send() call or a typed-schema entry.
	reInngestNameKey = regexp.MustCompile(`\bname\s*:\s*` + inngestStr)

	// Typed event-schema definition. The conventional form is
	// `new EventSchemas().fromRecord<{ "user/created": {...}; ... }>()`, where
	// the event names are the keys of the typed record. We pull the type-arg
	// region after `fromRecord` / `fromUnion` and harvest its quoted keys.
	reInngestEventSchemas = regexp.MustCompile(`\bnew\s+EventSchemas\s*\(`)
	reInngestSchemaKey    = regexp.MustCompile(inngestStr + `\s*:`)
)

func (e *inngestExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.inngest_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "inngest"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}
	// Attribution gate. Run only when the file plausibly uses Inngest: either it
	// imports the `inngest` package directly, or a createFunction call is made on
	// a receiver literally named `inngest` (the conventional client variable,
	// commonly imported from a local `./client` wrapper). This keeps a stray
	// `.createFunction(` from an unrelated library from being misattributed.
	hasImport := reInngestImport.MatchString(src)

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.SourceFile)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Event-name → MessageTopic, deduped by event name within this file. The
	// first reference site wins as the topic's source location; a later typed
	// schema definition is preferred over a bare reference if it shows up.
	eventTopics := make(map[string]bool)
	addEventTopic := func(name string, line int, fromSchema bool) {
		if name == "" || eventTopics[name] {
			return
		}
		eventTopics[name] = true
		ent := makeEntity(name, string(types.EntityKindMessageTopic), "inngest",
			file.Path, file.Language, line)
		src := "INFERRED_FROM_INNGEST_EVENT_REFERENCE"
		if fromSchema {
			src = "INFERRED_FROM_INNGEST_EVENT_SCHEMA"
		}
		setProps(&ent, "framework", "inngest", "topic", name,
			"topic_id", "event:"+name, "provenance", src)
		addEntity(ent)
	}

	for _, m := range reInngestCreateFunction.FindAllStringSubmatchIndex(src, -1) {
		receiver := src[m[2]:m[3]]
		callStart := m[0]

		// Attribution: accept the call if the file imports inngest, or the
		// receiver is the conventional `inngest` client (or a member access
		// ending in `.inngest`).
		if !hasImport && receiver != "inngest" && !strings.HasSuffix(receiver, ".inngest") {
			continue
		}

		// Slice the bounded argument region: from the opening paren of the
		// createFunction call to the matching close paren, so the id/event of
		// one function definition do not bleed into the next.
		seg := boundedCallSegment(src, m[1]-1) // m[1]-1 is the '(' offset

		// Function name: prefer config `id`, fall back to `name`.
		funcName := ""
		if mm := reInngestID.FindStringSubmatch(seg); mm != nil {
			funcName = mm[1]
		} else if mm := reInngestName.FindStringSubmatch(seg); mm != nil {
			funcName = mm[1]
		}
		if funcName == "" {
			// Anonymous / dynamically-named function: skip rather than emit a
			// nameless entity (honest-partial — no id/name literal to anchor).
			continue
		}

		ent := makeEntity(funcName, string(types.EntityKindFunction), "inngest_function",
			file.Path, file.Language, lineOf(src, callStart))
		setProps(&ent, "framework", "inngest", "function_id", funcName, "receiver", receiver,
			"provenance", "INFERRED_FROM_INNGEST_CREATE_FUNCTION")

		// Trigger event name (attribute only — edges are #5482/#5483).
		if mm := reInngestEvent.FindStringSubmatch(seg); mm != nil {
			setProps(&ent, "trigger_event", mm[1], "trigger_type", "event")
			// #5481: the triggered event name is also a distinct MessageTopic.
			addEventTopic(mm[1], lineOf(src, callStart), false)
		} else if mm := reInngestCron.FindStringSubmatch(seg); mm != nil {
			setProps(&ent, "trigger_cron", mm[1], "trigger_type", "cron")
		}

		addEntity(ent)
	}

	// #5481: producer side — `<client>.send({ name: "..." })` /
	// `.sendEvent(...)`. The same attribution gate applies (import present, or a
	// receiver named/ending in `inngest`). Each distinct event name becomes a
	// MessageTopic. An array of payloads yields one topic per `name:` key in the
	// bounded send() region.
	for _, m := range reInngestSend.FindAllStringSubmatchIndex(src, -1) {
		receiver := src[m[2]:m[3]]
		if !hasImport && receiver != "inngest" && !strings.HasSuffix(receiver, ".inngest") {
			continue
		}
		seg := boundedCallSegment(src, m[1]-1)
		for _, nm := range reInngestNameKey.FindAllStringSubmatch(seg, -1) {
			addEventTopic(nm[1], lineOf(src, m[0]), false)
		}
	}

	// #5481: typed event-schema definitions —
	// `new EventSchemas().fromRecord<{ "user/created": {...}; ... }>()`. The
	// event names are the quoted keys of the typed record. We scan a bounded
	// window after the `new EventSchemas(` call (covering the chained
	// .fromRecord<...>() type argument) and harvest its quoted keys as schema-
	// sourced topics. Gated on the inngest import.
	if hasImport {
		for _, m := range reInngestEventSchemas.FindAllStringSubmatchIndex(src, -1) {
			seg := schemaWindow(src, m[1])
			for _, km := range reInngestSchemaKey.FindAllStringSubmatch(seg, -1) {
				addEventTopic(km[1], lineOf(src, m[0]), true)
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// schemaWindow returns the type-argument region of a chained
// `.fromRecord<{ ... }>()` / `.fromUnion<...>()` that follows `new EventSchemas(`
// at byte offset `start`. It locates the next `fromRecord`/`fromUnion` within a
// bounded window, then returns the balanced `<...>` type-argument substring so
// event-name keys can be harvested. Returns "" if no such typed schema is found.
func schemaWindow(src string, start int) string {
	if start < 0 || start >= len(src) {
		return ""
	}
	const maxScan = 4000
	hi := start + maxScan
	if hi > len(src) {
		hi = len(src)
	}
	region := src[start:hi]
	// Anchor on the typed-schema builder call.
	idx := strings.Index(region, "fromRecord")
	if idx < 0 {
		idx = strings.Index(region, "fromUnion")
	}
	if idx < 0 {
		return ""
	}
	// Find the opening `<` after the builder name, then its matching `>`.
	open := strings.IndexByte(region[idx:], '<')
	if open < 0 {
		return ""
	}
	open += idx
	depth := 0
	for i := open; i < len(region); i++ {
		switch region[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return region[open : i+1]
			}
		}
	}
	return region[open:]
}

// boundedCallSegment returns the source substring from openParen (the byte
// offset of a '(') to its matching ')', inclusive, capped to a sane length so
// a malformed/unterminated call cannot scan the whole file.
func boundedCallSegment(src string, openParen int) string {
	if openParen < 0 || openParen >= len(src) || src[openParen] != '(' {
		return ""
	}
	depth := 0
	const maxScan = 4000
	end := openParen
	for i := openParen; i < len(src) && i < openParen+maxScan; i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = i
				return src[openParen : end+1]
			}
		}
	}
	// Unterminated within the cap: return the bounded window.
	if openParen+maxScan < len(src) {
		return src[openParen : openParen+maxScan]
	}
	return src[openParen:]
}
