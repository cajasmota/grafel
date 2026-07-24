package javascript

// tauri.go — custom extractor for the Tauri JS/TS frontend IPC surface.
//
// This is the cross-language *caller* half of the Tauri IPC contract whose
// in-binary half (the Rust `#[tauri::command]` handler + `generate_handler!`
// registration + `app.emit`/`app.listen` event topology) is emitted by
// internal/custom/rust/tauri.go. Tauri's frontend is JS/TS (a webview), and it
// reaches the Rust backend exclusively through `invoke("cmd", …)` and the
// `@tauri-apps/api` event bus. This extractor detects those frontend sites and
// emits edges keyed on the SAME stable tokens the Rust side carries, so the
// global by-name resolver (internal/resolve) joins frontend → backend across
// the JS↔Rust language boundary with no extra linker — exactly how the
// abibridge C↔asm linker binds by symbol name.
//
// Detected (#5023 / cross-language #5105):
//
//   - invoke("cmd", …) / core.invoke("cmd") / window.__TAURI__.invoke("cmd")
//     → a SCOPE.Operation(ipc_invoke) caller entity with a CALLS edge to
//     ToID "tauri:command:<cmd>" — the Name the Rust #[tauri::command] entity
//     carries. The resolver binds the edge to the real Rust command. This is
//     the frontend `invoke("cmd")` → Rust `#[tauri::command] fn cmd` link.
//   - emit("evt", …) / getCurrentWindow().emit("evt", …) / emitTo(t,"evt",…)
//     → PUBLISHES_TO the shared SCOPE.Datastore(ipc_event) channel node keyed
//     "tauri:event:<evt>" (the same node the Rust emit/listen side uses).
//   - listen("evt", …) / once("evt", …) / getCurrentWindow().listen("evt")
//     → SUBSCRIBES_TO that same channel node.
//
// Honesty:
//
//	partial — heuristic regex match on source text. We resolve literal command
//	and event names only; dynamic / template / variable names yield no edge. We
//	require a Tauri import signal (`@tauri-apps/api` or `window.__TAURI__`) so a
//	bare `invoke(...)` in a non-Tauri codebase is a no-op. The channel/command
//	tokens are emitted as ToID stubs the resolver binds by name; if the Rust
//	side is in a different repo / not indexed, the edge stays an unresolved stub
//	(same contract as every cross-repo link).
//
// Issue #5023 — Tauri IPC commands + emit/listen events (frontend caller half).
// Issue #5105 — TS-side invoke("cmd") → Rust #[tauri::command] cross-language link.

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extreg.Register("custom_js_tauri", &tauriFrontendExtractor{})
}

type tauriFrontendExtractor struct{}

func (e *tauriFrontendExtractor) Language() string { return "custom_js_tauri" }

var (
	// Tauri frontend import / global signal. Either an `@tauri-apps/api…`
	// import or the injected `window.__TAURI__` global marks this as a Tauri
	// frontend file, so a bare invoke/emit/listen in a non-Tauri codebase is
	// not misattributed.
	reTauriFEImport = regexp.MustCompile(
		`@tauri-apps/api|window\.__TAURI__|globalThis\.__TAURI__`,
	)

	// invoke("cmd", …) / core.invoke("cmd") / tauri.invoke("cmd") /
	// window.__TAURI__.invoke("cmd"). The command name is the first string
	// literal argument (single, double, or template-with-no-interpolation).
	reTauriFEInvoke = regexp.MustCompile(
		`(?:\.\s*)?\binvoke\s*\(\s*["'` + "`" + `]([A-Za-z_][\w-]*)["'` + "`" + `]`,
	)

	// emitTo("target", "evt", …) — event name is the SECOND string literal.
	reTauriFEEmitTo = regexp.MustCompile(
		`\bemitTo\s*\(\s*[^,]+,\s*["'` + "`" + `]([A-Za-z_][\w-]*)["'` + "`" + `]`,
	)

	// emit("evt", …) / getCurrentWindow().emit("evt", …). First string literal
	// is the event name. (emitTo is handled above and excluded here.)
	reTauriFEEmit = regexp.MustCompile(
		`(?:\.\s*)?\bemit\s*\(\s*["'` + "`" + `]([A-Za-z_][\w-]*)["'` + "`" + `]`,
	)

	// listen("evt", …) / once("evt", …) / getCurrentWindow().listen("evt").
	// First string literal is the event name.
	reTauriFEListen = regexp.MustCompile(
		`(?:\.\s*)?\b(?:listen|once)\s*\(\s*["'` + "`" + `]([A-Za-z_][\w-]*)["'` + "`" + `]`,
	)
)

func (e *tauriFrontendExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.tauri_frontend_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "tauri"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	// Only JS/TS frontend files.
	switch file.Language {
	case "javascript", "typescript", "js", "ts", "jsx", "tsx":
	default:
		return nil, nil
	}

	src := string(file.Content)

	// Guard: require a Tauri frontend signal. A bare invoke()/emit() in a
	// non-Tauri codebase must be a no-op.
	if !reTauriFEImport.MatchString(src) {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// -----------------------------------------------------------------------
	// 1. invoke("cmd", …) → CALLS tauri:command:<cmd> (cross-language link).
	// -----------------------------------------------------------------------
	for _, m := range reTauriFEInvoke.FindAllStringSubmatchIndex(src, -1) {
		cmd := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		caller := makeEntity("tauri:invoke:"+cmd, "SCOPE.Operation", "ipc_invoke",
			file.Path, file.Language, line)
		setProps(&caller,
			"framework", "tauri",
			"command_name", cmd,
			"ipc", "command",
			"provenance", "INFERRED_FROM_TAURI_FRONTEND_INVOKE",
		)
		caller.Relationships = append(caller.Relationships, types.RelationshipRecord{
			FromID: caller.Name,
			// ToID is the stable token the Rust #[tauri::command] entity carries
			// as its Name (internal/custom/rust/tauri.go), so the by-name
			// resolver joins frontend invoke → Rust command across JS↔Rust.
			ToID: "tauri:command:" + cmd,
			Kind: string(types.RelationshipKindCalls),
			Properties: types.Props{
				{K: "command_name", V: cmd},
				{K: "cross_lang", V: "js_to_rust"},
				{K: "framework", V: "tauri"},
				{K: "ipc", V: "command"},
				{K: "via", V: "INFERRED_FROM_TAURI_FRONTEND_INVOKE"},
			},
		})
		add(caller)
	}

	// -----------------------------------------------------------------------
	// 2. emit/listen event channel pub/sub topology. Shares ONE channel node
	//    per literal event name with the Rust side (tauri:event:<name>) so the
	//    resolver joins producer ↔ consumer across files AND languages.
	// -----------------------------------------------------------------------
	emittedChannel := make(map[string]bool)
	ensureChannel := func(evt string, line int) {
		if emittedChannel[evt] {
			return
		}
		emittedChannel[evt] = true
		ch := makeEntity("tauri:event:"+evt, "SCOPE.Datastore", "ipc_event",
			file.Path, file.Language, line)
		setProps(&ch,
			"framework", "tauri",
			"event_name", evt,
			"channel", evt,
			"provenance", "INFERRED_FROM_TAURI_FRONTEND_EVENT",
		)
		add(ch)
	}

	emitEventEdge := func(re *regexp.Regexp, kind, role, provenance string) {
		for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
			evt := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			ensureChannel(evt, line)
			owner := makeEntity("tauri:fe_"+role+":"+evt, "SCOPE.Operation", "ipc_event_"+role,
				file.Path, file.Language, line)
			setProps(&owner,
				"framework", "tauri",
				"event_name", evt,
				"ipc_role", role,
				"provenance", provenance,
			)
			owner.Relationships = append(owner.Relationships, types.RelationshipRecord{
				FromID: owner.Name,
				ToID:   "tauri:event:" + evt,
				Kind:   kind,
				Properties: types.Props{
					{K: "channel", V: evt},
					{K: "event_name", V: evt},
					{K: "framework", V: "tauri"},
					{K: "via", V: provenance},
				},
			})
			add(owner)
		}
	}

	// emitTo first (second-literal event name) so its sites are not also
	// captured by the generic emit() pattern with the wrong (target) name.
	emitEventEdge(reTauriFEEmitTo, string(types.RelationshipKindPublishesTo),
		"publish", "INFERRED_FROM_TAURI_FRONTEND_EMIT_TO")
	emitEventEdge(reTauriFEEmit, string(types.RelationshipKindPublishesTo),
		"publish", "INFERRED_FROM_TAURI_FRONTEND_EMIT")
	emitEventEdge(reTauriFEListen, string(types.RelationshipKindSubscribesTo),
		"subscribe", "INFERRED_FROM_TAURI_FRONTEND_LISTEN")

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
