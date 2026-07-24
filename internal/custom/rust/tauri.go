package rust

// tauri.go — custom extractor for Tauri (Rust desktop/mobile framework).
//
// Detects and emits entities for:
//
//   - #[tauri::command] functions → SCOPE.Operation/ipc_command
//     (ipc_extraction: detecting IPC handler registrations)
//   - tauri::Builder::default().invoke_handler(generate_handler![...]) →
//     SCOPE.Component/ipc_handler_registration
//   - main.rs / src-tauri/main.rs pattern → SCOPE.Component/main_process
//     (main_renderer_split: detecting the Rust backend entry point)
//   - use tauri::api / napi imports → SCOPE.Pattern/native_module
//     (native_module_imports: native module usage detection)
//   - WindowBuilder / WebviewWindow creation → SCOPE.Component/renderer_window
//
// IPC topology edges (#5023, follow-up from #4965):
//
//   - generate_handler![a, b] → CALLS edge from the SCOPE.Component
//     (ipc_handler_registration) to each registered SCOPE.Operation
//     (tauri:command:<name>). This is the in-binary half of the IPC contract:
//     the registration wires every command that the frontend `invoke("name")`
//     can reach. The frontend (TS) `invoke(...)` side is cross-language /
//     cross-repo and is deferred to a follow-up.
//   - app.emit("evt", ..) / window.emit("evt", ..) / emit_all("evt", ..) →
//     PUBLISHES_TO a synthetic SCOPE.Datastore(ipc_event) channel keyed by the
//     literal event name (tauri:event:<name>).
//   - app.listen("evt", ..) / window.listen("evt", ..) / listen_global("evt") →
//     SUBSCRIBES_TO the same synthetic ipc_event channel.
//
// Honesty:
//
//	partial — heuristic regex match on source text. We detect the IPC command
//	registration surface, the registration→command CALLS contract, the
//	emit/listen event-channel pub/sub topology, and main/renderer split. We do
//	NOT resolve the TS-side `invoke("cmd")` caller (cross-language) nor
//	dynamic / non-literal event names. Fixtures prove the detection surface.
//
// Issue #3267 — lang.rust.framework.tauri Process/Native cells.
// Issue #5023 — Tauri IPC commands + emit/listen events.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_rust_tauri", &tauriExtractor{})
}

type tauriExtractor struct{}

func (e *tauriExtractor) Language() string { return "custom_rust_tauri" }

// ---------------------------------------------------------------------------
// Regex catalog
// ---------------------------------------------------------------------------

var (
	// #[tauri::command]  (preceding an fn declaration)
	reTauriCommand = regexp.MustCompile(
		`#\[tauri::command\][\s\S]*?(?:async\s+)?(?:pub\s+)?fn\s+(\w+)\s*\(`,
	)

	// generate_handler![cmd1, cmd2, cmd3]  — IPC handler registration
	reTauriGenerateHandler = regexp.MustCompile(
		`generate_handler!\s*\[([^\]]+)\]`,
	)

	// tauri::Builder::default() — app entry point (main process)
	reTauriBuilder = regexp.MustCompile(
		`tauri::Builder\s*::\s*default\s*\(\s*\)`,
	)

	// fn main() in a Tauri context (check file path heuristic + tauri import)
	reTauriMainFn = regexp.MustCompile(
		`(?m)^(?:pub\s+)?fn\s+main\s*\(\s*\)`,
	)

	// use tauri or extern crate tauri → marks this as a Tauri file
	reTauriImport = regexp.MustCompile(
		`\buse\s+tauri\b|extern\s+crate\s+tauri\b`,
	)

	// WindowBuilder / WebviewWindowBuilder — renderer window creation
	reTauriWindowBuilder = regexp.MustCompile(
		`(?:WindowBuilder|WebviewWindowBuilder|tauri::WindowBuilder)\s*::\s*new\s*\(`,
	)

	// tauri::api:: usage → native module imports (e.g. tauri::api::path, tauri::api::shell)
	reTauriApiUsage = regexp.MustCompile(
		`tauri::api::(\w+)`,
	)

	// tauri_plugin_ crate imports → native plugin imports
	reTauriPlugin = regexp.MustCompile(
		`tauri_plugin_(\w+)`,
	)

	// tauri::path::BaseDirectory or tauri::Manager trait usage
	reTauriManager = regexp.MustCompile(
		`tauri::Manager|AppHandle|tauri::AppHandle`,
	)

	// app.emit("evt", ..) / window.emit_all("evt", ..) / handle.emit_to(.., "evt", ..)
	// Event-publish sites. We capture the FIRST string-literal argument as the
	// event name. emit_to takes a target label first, so its event name is the
	// second literal — handled by a dedicated pattern below.
	reTauriEmit = regexp.MustCompile(
		`\.\s*(?:emit|emit_all)\s*\(\s*"([^"]+)"`,
	)
	// emit_to("target", "evt", ..) — event name is the SECOND string literal.
	reTauriEmitTo = regexp.MustCompile(
		`\.\s*emit_to\s*\(\s*[^,]+,\s*"([^"]+)"`,
	)

	// app.listen("evt", ..) / window.listen_global("evt", ..) / once("evt", ..)
	// Event-subscribe sites. First string-literal argument is the event name.
	reTauriListen = regexp.MustCompile(
		`\.\s*(?:listen|listen_global|once)\s*\(\s*"([^"]+)"`,
	)

	// A bare Rust identifier (validates a generate_handler! command entry).
	reIdent = regexp.MustCompile(`^[A-Za-z_]\w*$`)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *tauriExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/rust")
	_, span := tracer.Start(ctx, "indexer.tauri_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "tauri"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)

	// Quick guard: skip files that have no tauri signals at all
	if !reTauriImport.MatchString(src) &&
		!reTauriCommand.MatchString(src) &&
		!reTauriBuilder.MatchString(src) {
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
	// 1. ipc_extraction — #[tauri::command] fn declarations
	// -----------------------------------------------------------------------
	for _, m := range reTauriCommand.FindAllStringSubmatchIndex(src, -1) {
		cmdName := src[m[2]:m[3]]
		ent := makeEntity("tauri:command:"+cmdName, "SCOPE.Operation", "ipc_command",
			file.Path, file.Language, lineOf(src, m[0]))
		// QualifiedName = the stable token so the resolver's byQualifiedName tier
		// binds both the in-binary generate_handler! CALLS edge AND the
		// cross-language frontend invoke("cmd") CALLS edge (ToID carries this
		// exact token) to this command entity.
		ent.QualifiedName = "tauri:command:" + cmdName
		setProps(&ent,
			"framework", "tauri",
			"command_name", cmdName,
			"provenance", "INFERRED_FROM_TAURI_COMMAND_ATTR",
		)
		add(ent)
	}

	// -----------------------------------------------------------------------
	// 2. ipc_extraction — generate_handler![cmd1, cmd2]
	// -----------------------------------------------------------------------
	for _, m := range reTauriGenerateHandler.FindAllStringSubmatchIndex(src, -1) {
		handlerList := src[m[2]:m[3]]
		ent := makeEntity("tauri:generate_handler", "SCOPE.Component", "ipc_handler_registration",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "tauri",
			"handler_list", handlerList,
			"provenance", "INFERRED_FROM_TAURI_GENERATE_HANDLER",
		)
		// CALLS edge: the registration wires every listed command. ToID is the
		// stable tauri:command:<name> token, the same Name the #[tauri::command]
		// entity carries, so the linker joins registration → command in-file and
		// a future TS-side invoke("name") can target the same command surface.
		for _, raw := range strings.Split(handlerList, ",") {
			cmd := strings.TrimSpace(raw)
			// generate_handler! entries may be path-qualified (commands::greet)
			// or generic-suffixed; take the final ident segment.
			if i := strings.LastIndex(cmd, "::"); i >= 0 {
				cmd = cmd[i+2:]
			}
			cmd = strings.TrimSpace(cmd)
			if cmd == "" || !reIdent.MatchString(cmd) {
				continue
			}
			ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
				FromID: "tauri:generate_handler",
				ToID:   "tauri:command:" + cmd,
				Kind:   string(types.RelationshipKindCalls),
				Properties: types.Props{
					{K: "command_name", V: cmd},
					{K: "framework", V: "tauri"},
					{K: "ipc", V: "command"},
					{K: "via", V: "INFERRED_FROM_TAURI_GENERATE_HANDLER"},
				},
			})
		}
		add(ent)
	}

	// -----------------------------------------------------------------------
	// 3. main_renderer_split — tauri::Builder entry point (Rust backend)
	// -----------------------------------------------------------------------
	for _, m := range reTauriBuilder.FindAllStringIndex(src, -1) {
		ent := makeEntity("tauri:Builder::default", "SCOPE.Component", "main_process",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "tauri",
			"provenance", "INFERRED_FROM_TAURI_BUILDER",
		)
		add(ent)
	}

	// main_renderer_split — fn main() in a Tauri file
	for _, m := range reTauriMainFn.FindAllStringIndex(src, -1) {
		ent := makeEntity("tauri:main", "SCOPE.Function", "main_entry_point",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "tauri",
			"provenance", "INFERRED_FROM_TAURI_MAIN_FN",
		)
		add(ent)
	}

	// WindowBuilder / WebviewWindowBuilder → renderer window (renderer side)
	for _, m := range reTauriWindowBuilder.FindAllStringIndex(src, -1) {
		ent := makeEntity("tauri:WindowBuilder", "SCOPE.Component", "renderer_window",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "tauri",
			"provenance", "INFERRED_FROM_TAURI_WINDOW_BUILDER",
		)
		add(ent)
	}

	// -----------------------------------------------------------------------
	// 3b. ipc_extraction — emit/listen event channel pub/sub topology
	// -----------------------------------------------------------------------
	// Each distinct literal event name becomes one synthetic SCOPE.Datastore
	// (ipc_event) channel node, keyed tauri:event:<name>. emit sites attach a
	// PUBLISHES_TO edge to it; listen sites attach a SUBSCRIBES_TO edge. The
	// shared channel node lets the global linker join producer ↔ consumer even
	// across files, mirroring the redis pub/sub channel modelling.
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
			"provenance", "INFERRED_FROM_TAURI_EVENT",
		)
		add(ch)
	}

	emitEventEdge := func(re *regexp.Regexp, kind, role, provenance string) {
		for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
			evt := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			ensureChannel(evt, line)
			owner := makeEntity("tauri:"+role+":"+evt, "SCOPE.Operation", "ipc_event_"+role,
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

	emitEventEdge(reTauriEmit, string(types.RelationshipKindPublishesTo),
		"publish", "INFERRED_FROM_TAURI_EMIT")
	emitEventEdge(reTauriEmitTo, string(types.RelationshipKindPublishesTo),
		"publish", "INFERRED_FROM_TAURI_EMIT_TO")
	emitEventEdge(reTauriListen, string(types.RelationshipKindSubscribesTo),
		"subscribe", "INFERRED_FROM_TAURI_LISTEN")

	// -----------------------------------------------------------------------
	// 4. native_module_imports — tauri::api::* usage
	// -----------------------------------------------------------------------
	seenAPI := make(map[string]bool)
	for _, m := range reTauriApiUsage.FindAllStringSubmatchIndex(src, -1) {
		apiModule := src[m[2]:m[3]]
		if seenAPI[apiModule] {
			continue
		}
		seenAPI[apiModule] = true
		ent := makeEntity("tauri:api:"+apiModule, "SCOPE.Pattern", "native_module",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "tauri",
			"module_name", "tauri::api::"+apiModule,
			"provenance", "INFERRED_FROM_TAURI_API_USAGE",
		)
		add(ent)
	}

	// native_module_imports — tauri_plugin_* crates
	seenPlugin := make(map[string]bool)
	for _, m := range reTauriPlugin.FindAllStringSubmatchIndex(src, -1) {
		pluginName := src[m[2]:m[3]]
		if seenPlugin[pluginName] {
			continue
		}
		seenPlugin[pluginName] = true
		ent := makeEntity("tauri:plugin:"+pluginName, "SCOPE.Pattern", "native_plugin",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "tauri",
			"plugin_name", "tauri_plugin_"+pluginName,
			"provenance", "INFERRED_FROM_TAURI_PLUGIN",
		)
		add(ent)
	}

	// tauri::Manager / AppHandle → native bridge usage
	for _, m := range reTauriManager.FindAllStringIndex(src, -1) {
		ent := makeEntity("tauri:AppHandle", "SCOPE.Pattern", "native_module",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "tauri",
			"provenance", "INFERRED_FROM_TAURI_MANAGER",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
