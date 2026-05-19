// Server-Sent Events entity + edge synthesis (#727).
//
// Emits:
//   - Stream entities — identity `sse:<canonical-path>`. The cross-repo
//     linker matches by Name so a Django `StreamingHttpResponse` mounted on
//     `/api/events` and a browser `new EventSource("/api/events")` collapse
//     to the same identity without any new linker code.
//   - STREAMS_TO edges — server handler → Stream (server-side emitter).
//   - STREAMS_FROM edges — client function → Stream (browser EventSource).
//
// Server patterns covered:
//   - Node/Express: `res.writeHead(200, { 'Content-Type': 'text/event-stream' })`
//     emitted from inside an Express route handler. We don't try to bind
//     the call back to the registering route; the surrounding HTTP-endpoint
//     synthesis pass already records the path. The stream channel is keyed
//     by the enclosing function name (covers `setupSSE(req, res) { ... }`).
//   - Django: `StreamingHttpResponse(generator, content_type="text/event-stream")`.
//   - Spring: `SseEmitter` field/return type — flagged by literal mention.
//   - FastAPI: `StreamingResponse(..., media_type="text/event-stream")`.
//   - Quarkus SSE: `@Produces("text/event-stream")` on a JAX-RS resource
//     method (`@Path` provides the path).
//
// Client patterns covered:
//   - Browser/Node: `new EventSource("url")`, template literal, identifier.
//   - Python: requests `iter_lines()` consumers with `Accept: text/event-stream`
//     header (flagged but not given a strict identity unless the URL is a
//     literal at the call site).
//
// Beyond-minimum:
//   - When the SSE response is sent from inside a function that also has a
//     detectable HTTP endpoint (via the http_endpoint synthesis pass that
//     ran earlier), we leave the path on the entity; the cross-stack match
//     happens by the synthetic-path identity.
//
// Refs #727.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

const streamKind = "Stream"
const streamIDPrefix = "sse:"

// applySSESynthesis runs per-file. Append-only.
func applySSESynthesis(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}
	src := string(content)

	seen := map[string]bool{}
	emitStream := func(rawPath string, framework string) string {
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, rawPath)
		if canonical == "" {
			canonical = rawPath
		}
		id := streamIDPrefix + canonical
		if seen[id] {
			return id
		}
		seen[id] = true
		entities = append(entities, types.EntityRecord{
			ID:         id,
			Name:       id,
			Kind:       streamKind,
			SourceFile: path,
			Language:   lang,
			Properties: map[string]string{
				"path":         canonical,
				"framework":    framework,
				"pattern_type": "sse_synthesis",
			},
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
		return id
	}

	emitEdge := func(kind, fromID, toID string, props map[string]string) {
		if fromID == "" || toID == "" {
			return
		}
		if props == nil {
			props = map[string]string{}
		}
		props["pattern_type"] = "sse_synthesis"
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       toID,
			Kind:       kind,
			Properties: props,
		})
	}

	switch lang {
	case "javascript", "typescript":
		synthSSENodeServer(src, path, emitStream, emitEdge)
		synthSSEEventSourceClient(src, path, emitStream, emitEdge)
	case "python":
		synthSSEDjangoStreaming(src, path, emitStream, emitEdge)
		synthSSEFastAPIStreaming(src, path, emitStream, emitEdge)
	case "java", "kotlin":
		synthSSESpringEmitter(src, path, emitStream, emitEdge)
		synthSSEQuarkusProduces(src, path, emitStream, emitEdge)
	}

	return entities, relationships
}

// ---------------------------------------------------------------------------
// Node / Express server: res.writeHead(200, {'Content-Type': 'text/event-stream'})
// ---------------------------------------------------------------------------

// nodeSSEWriteHeadRe matches the canonical writeHead idiom inside Express
// route handlers. The whole call site must mention `text/event-stream` for
// us to fire — common false positives (other writeHead calls with json/html
// content-type) are ignored.
var nodeSSEWriteHeadRe = regexp.MustCompile(
	`\bres\s*\.\s*writeHead\s*\(\s*\d+\s*,\s*\{[^}]*text/event-stream`,
)

// nodeSSESetHeaderRe matches the streaming variant that sets the content
// type via res.setHeader('Content-Type', 'text/event-stream') instead of
// writeHead. Equivalent to the above for our purposes.
var nodeSSESetHeaderRe = regexp.MustCompile(
	`res\s*\.\s*setHeader\s*\(\s*['"]Content-Type['"]\s*,\s*['"]text/event-stream['"]\s*\)`,
)

func synthSSENodeServer(
	src, path string,
	emitStream func(rawPath, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "text/event-stream") {
		return
	}
	funcs := indexJSEnclosingFunctions(src)

	hits := append(
		nodeSSEWriteHeadRe.FindAllStringIndex(src, -1),
		nodeSSESetHeaderRe.FindAllStringIndex(src, -1)...,
	)
	for _, m := range hits {
		caller := enclosingJSFuncAt(funcs, m[0])
		channelPath := "/" + caller
		if caller == "" {
			channelPath = "/" + sanitiseID(path)
		}
		id := emitStream(channelPath, "node_sse")
		emitEdge(
			string(types.RelationshipKindStreamsTo),
			"Function:" + caller,
			id,
			map[string]string{"framework": "node_sse", "path": channelPath},
		)
	}
}

// ---------------------------------------------------------------------------
// Browser / Node client: new EventSource(url)
// ---------------------------------------------------------------------------

var eventSourceClientRe = regexp.MustCompile(
	"new\\s+EventSource\\s*\\(\\s*(?:['\"]([^'\"\\r\\n$]+)['\"]|`([^`\\r\\n]+)`|([A-Za-z_$][\\w$]*))",
)

func synthSSEEventSourceClient(
	src, path string,
	emitStream func(rawPath, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "new EventSource") {
		return
	}
	syms := buildJSConstantSymbolTable(src)
	funcs := indexJSEnclosingFunctions(src)
	for _, m := range eventSourceClientRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 8 {
			continue
		}
		var rawURL string
		var isTemplate bool
		switch {
		case m[2] >= 0:
			rawURL = src[m[2]:m[3]]
		case m[4] >= 0:
			rawURL = src[m[4]:m[5]]
			isTemplate = true
		case m[6] >= 0:
			ident := src[m[6]:m[7]]
			if v, ok := syms[ident]; ok {
				rawURL = v
			} else {
				continue
			}
		}
		if rawURL == "" {
			continue
		}
		var channelPath string
		if isTemplate && strings.Contains(rawURL, "${") {
			resolved, ok := canonicalizeTemplateLiteral(rawURL, syms)
			if !ok {
				continue
			}
			channelPath = stripURLHost(resolved)
		} else {
			channelPath = stripURLHost(rawURL)
		}
		if channelPath == "" {
			continue
		}
		id := emitStream(channelPath, "event_source")
		caller := enclosingJSFuncAt(funcs, m[0])
		fromID := "Function:" + caller
		if caller == "" {
			fromID = "Function:" + sanitiseID(path)
		}
		emitEdge(
			string(types.RelationshipKindStreamsFrom),
			fromID,
			id,
			map[string]string{"framework": "event_source", "path": channelPath},
		)
	}
}

// ---------------------------------------------------------------------------
// Django: StreamingHttpResponse(..., content_type="text/event-stream")
// ---------------------------------------------------------------------------

// djangoStreamingRe is line-scoped: the content_type kwarg may appear after
// an inner function call (e.g. `StreamingHttpResponse(gen(), content_type=
// "text/event-stream")`), so we tolerate any chars except a newline rather
// than restricting to `[^)]`. The kwarg is anchored by its name + value
// shape so collisions with other StreamingHttpResponse uses are minimal.
var djangoStreamingRe = regexp.MustCompile(
	`StreamingHttpResponse\s*\([^\n\r]*?content_type\s*=\s*['"]text/event-stream['"]`,
)

var pyDefRe = regexp.MustCompile(`(?m)^[ \t]*(?:async\s+)?def\s+(\w+)`)

func synthSSEDjangoStreaming(
	src, path string,
	emitStream func(rawPath, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "StreamingHttpResponse") || !strings.Contains(src, "text/event-stream") {
		return
	}
	for _, m := range djangoStreamingRe.FindAllStringIndex(src, -1) {
		handler := enclosingPyFuncForOffset(src, m[0])
		channelPath := "/" + handler
		if handler == "" {
			channelPath = "/" + sanitiseID(path)
		}
		id := emitStream(channelPath, "django_sse")
		emitEdge(
			string(types.RelationshipKindStreamsTo),
			"Function:" + handler,
			id,
			map[string]string{"framework": "django_sse", "path": channelPath},
		)
	}
}

// enclosingPyFuncForOffset returns the name of the nearest preceding
// `def`/`async def` to `pos`.
func enclosingPyFuncForOffset(src string, pos int) string {
	name := ""
	for _, m := range pyDefRe.FindAllStringSubmatchIndex(src, -1) {
		if m[0] > pos {
			break
		}
		if m[2] >= 0 {
			name = src[m[2]:m[3]]
		}
	}
	return name
}

// ---------------------------------------------------------------------------
// FastAPI: StreamingResponse(..., media_type="text/event-stream")
// ---------------------------------------------------------------------------

// fastapiStreamingRe is line-scoped for the same reason as djangoStreamingRe:
// the first positional argument is commonly a generator call whose `(` and
// `)` would prematurely terminate a `[^)]*?` scan.
var fastapiStreamingRe = regexp.MustCompile(
	`StreamingResponse\s*\([^\n\r]*?media_type\s*=\s*['"]text/event-stream['"]`,
)

func synthSSEFastAPIStreaming(
	src, path string,
	emitStream func(rawPath, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "StreamingResponse") || !strings.Contains(src, "text/event-stream") {
		return
	}
	for _, m := range fastapiStreamingRe.FindAllStringIndex(src, -1) {
		handler := enclosingPyFuncForOffset(src, m[0])
		channelPath := "/" + handler
		if handler == "" {
			channelPath = "/" + sanitiseID(path)
		}
		id := emitStream(channelPath, "fastapi_sse")
		emitEdge(
			string(types.RelationshipKindStreamsTo),
			"Function:" + handler,
			id,
			map[string]string{"framework": "fastapi_sse", "path": channelPath},
		)
	}
}

// ---------------------------------------------------------------------------
// Spring: SseEmitter
// ---------------------------------------------------------------------------

// springSseEmitterRe captures a method that returns SseEmitter or stores it
// in a field. We treat this as a server-side STREAMS_TO emitter keyed by
// the method name (the path is wired by @GetMapping above the method).
var springSseEmitterRe = regexp.MustCompile(
	`\bSseEmitter\b[^\r\n]*[\r\n]+[\s\S]{0,200}?(?:public|private|protected)\s+(?:SseEmitter|ResponseEntity<\s*SseEmitter\s*>)\s+(\w+)\s*\(`,
)

// springSseEmitterMethodRe is a simpler form — any method whose return type
// is SseEmitter.
var springSseEmitterMethodRe = regexp.MustCompile(
	`(?m)(?:public|private|protected)\s+SseEmitter\s+(\w+)\s*\(`,
)

func synthSSESpringEmitter(
	src, path string,
	emitStream func(rawPath, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "SseEmitter") {
		return
	}
	seenMethod := map[string]bool{}
	collect := func(handler string) {
		if seenMethod[handler] {
			return
		}
		seenMethod[handler] = true
		channelPath := "/" + handler
		id := emitStream(channelPath, "spring_sse")
		emitEdge(
			string(types.RelationshipKindStreamsTo),
			"Class:" + handler,
			id,
			map[string]string{"framework": "spring_sse", "path": channelPath, "handler": handler},
		)
	}
	for _, m := range springSseEmitterRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		collect(m[1])
	}
	for _, m := range springSseEmitterMethodRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		collect(m[1])
	}
}

// ---------------------------------------------------------------------------
// Quarkus / JAX-RS: @Produces("text/event-stream")
// ---------------------------------------------------------------------------

var quarkusSseProducesRe = regexp.MustCompile(
	`@Produces\s*\(\s*(?:MediaType\.SERVER_SENT_EVENTS|"text/event-stream")\s*\)[\s\S]{0,300}?(?:public|private|protected)\s+[\w<>\[\],.\s?]+?\s+(\w+)\s*\(`,
)

func synthSSEQuarkusProduces(
	src, path string,
	emitStream func(rawPath, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "text/event-stream") && !strings.Contains(src, "SERVER_SENT_EVENTS") {
		return
	}
	for _, m := range quarkusSseProducesRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		handler := m[1]
		channelPath := "/" + handler
		id := emitStream(channelPath, "quarkus_sse")
		emitEdge(
			string(types.RelationshipKindStreamsTo),
			"Class:" + handler,
			id,
			map[string]string{"framework": "quarkus_sse", "path": channelPath, "handler": handler},
		)
	}
}
