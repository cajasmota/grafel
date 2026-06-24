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
// Scope (epic #5479, ticket #5480): the ENTITY only. The EMITS / TRIGGERS
// edges that wire the event name to producers/topics are later tickets
// (#5482/#5483/#5484); here the event name is recorded as an attribute.
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
		} else if mm := reInngestCron.FindStringSubmatch(seg); mm != nil {
			setProps(&ent, "trigger_cron", mm[1], "trigger_type", "cron")
		}

		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
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
