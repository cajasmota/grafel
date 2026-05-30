// Tauri application backend fixture

use tauri::{Builder, Manager, AppHandle};
use tauri::api::path;

/// IPC command: greet user
#[tauri::command]
async fn greet(name: String) -> String {
    format!("Hello, {}!", name)
}

/// IPC command: read a file
#[tauri::command]
fn read_file(path: String, app: AppHandle) -> Result<String, String> {
    let base = tauri::api::path::app_data_dir(&app.config())
        .ok_or("no app data dir")?;
    Ok(format!("{:?}", base.join(path)))
}

/// Main entry point — Rust backend (main process)
fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![greet, read_file])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
