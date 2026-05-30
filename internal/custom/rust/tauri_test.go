package rust_test

// tauri_test.go — tests for custom_rust_tauri extractor.
// Proves ipc_extraction, main_renderer_split, and native_module_imports.

import (
	"testing"
)

func TestTauri_IPCCommand(t *testing.T) {
	src := `
use tauri::Manager;

#[tauri::command]
async fn greet(name: String) -> String {
    format!("Hello, {}!", name)
}

#[tauri::command]
fn get_version() -> &'static str {
    "1.0.0"
}
`
	ents := extract(t, "custom_rust_tauri", fi("commands.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "tauri:command:greet") {
		t.Error("expected tauri:command:greet ipc_command")
	}
	if !containsEntity(ents, "SCOPE.Operation", "tauri:command:get_version") {
		t.Error("expected tauri:command:get_version ipc_command")
	}
}

func TestTauri_GenerateHandler(t *testing.T) {
	src := `
use tauri::Manager;

fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![greet, get_version, read_file])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
`
	ents := extract(t, "custom_rust_tauri", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "tauri:generate_handler") {
		t.Error("expected tauri:generate_handler ipc_handler_registration")
	}
}

func TestTauri_BuilderMainProcess(t *testing.T) {
	src := `
use tauri::Manager;

fn main() {
    tauri::Builder::default()
        .run(tauri::generate_context!())
        .unwrap();
}
`
	ents := extract(t, "custom_rust_tauri", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "tauri:Builder::default") {
		t.Error("expected tauri:Builder::default main_process component")
	}
	if !containsEntitySubtype(ents, "SCOPE.Function", "main_entry_point") {
		t.Error("expected main_entry_point function")
	}
}

func TestTauri_NativeModuleImports(t *testing.T) {
	src := `
use tauri::Manager;

fn setup(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let path = tauri::api::path::app_data_dir(app.config()).unwrap();
    let dir = tauri::api::dir::read_dir(&path, true).unwrap();
    Ok(())
}
`
	ents := extract(t, "custom_rust_tauri", fi("setup.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Pattern", "tauri:api:path") {
		t.Error("expected tauri:api:path native_module")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "tauri:api:dir") {
		t.Error("expected tauri:api:dir native_module")
	}
}

func TestTauri_NativePlugin(t *testing.T) {
	src := `
use tauri::Manager;
use tauri_plugin_sql::{Migration, MigrationKind};
use tauri_plugin_fs::FsExt;

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_sql::Builder::new().build())
        .run(tauri::generate_context!())
        .unwrap();
}
`
	ents := extract(t, "custom_rust_tauri", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Pattern", "tauri:plugin:sql") {
		t.Error("expected tauri:plugin:sql native_plugin")
	}
}

func TestTauri_WindowBuilder(t *testing.T) {
	src := `
use tauri::Manager;

fn create_window(app: &tauri::AppHandle) {
    tauri::WindowBuilder::new(app, "main", tauri::WindowUrl::App("index.html".into()))
        .title("My App")
        .build()
        .unwrap();
}
`
	ents := extract(t, "custom_rust_tauri", fi("window.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "tauri:WindowBuilder") {
		t.Error("expected tauri:WindowBuilder renderer_window component")
	}
}

func TestTauri_NoMatch(t *testing.T) {
	src := `
use std::collections::HashMap;

fn main() {
    let mut map = HashMap::new();
    map.insert("key", "value");
}
`
	ents := extract(t, "custom_rust_tauri", fi("main.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-tauri file, got %d", len(ents))
	}
}

func TestTauri_FixtureFile(t *testing.T) {
	src := readFixture(t, "testdata/tauri_app.rs")
	ents := extract(t, "custom_rust_tauri", fi("tauri_app.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "tauri:command:greet") {
		t.Error("expected tauri:command:greet from fixture")
	}
	if !containsEntity(ents, "SCOPE.Operation", "tauri:command:read_file") {
		t.Error("expected tauri:command:read_file from fixture")
	}
	if !containsEntity(ents, "SCOPE.Component", "tauri:generate_handler") {
		t.Error("expected tauri:generate_handler from fixture")
	}
	if !containsEntity(ents, "SCOPE.Component", "tauri:Builder::default") {
		t.Error("expected tauri:Builder::default from fixture")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "tauri:api:path") {
		t.Error("expected tauri:api:path from fixture")
	}
}
