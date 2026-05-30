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
// Honesty:
//
//	partial — heuristic regex match on source text. We detect the IPC command
//	registration surface and main/renderer split at the file level but cannot
//	perform cross-file call-graph analysis to fully verify the IPC contract.
//	Fixtures prove the detection surface.
//
// Issue #3267 — lang.rust.framework.tauri Process/Native cells.

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
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
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *tauriExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
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
