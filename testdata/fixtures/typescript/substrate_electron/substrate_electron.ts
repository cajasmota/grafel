// Electron substrate proving fixture (#3059).
//
// Proves that the framework-blind jsts substrate sniffers fire on
// Electron main-process and preload source code:
//
//   - import_resolution_quality — named imports from 'electron', './config'
//   - env_fallback_recognition  — process.env.ELECTRON_DEV ?? 'production'
//   - constant_propagation      — const API_URL = 'https://api.example.com'
//   - http_effect               — httpFetch calls fetch()
//   - fs_effect                 — readConfig / writeLog call fs.readFile / fs.writeFile
//   - db_effect                 — queryDB calls db.findOne() / db.create()
//                                 (Electron main process runs Node.js and may
//                                 use ORM/DB libraries directly)
//   - mutation_effect           — AppState.setWindow assigns this._win
//   - dead_code_detection       — export signals to dead_module_detector
//   - def_use_chain_extraction  — const endpoint / resp / data in httpFetch
//   - pure_function_tagging     — formatTitle has no effects
//   - template_pattern_catalog  — t("app.title"), console.error, SELECT literal
//   - request_shape_extraction  — axios body shape in postEvent
//   - confidence_overlay        — effect_propagation + jsts sniffer integration
//   - reachability_analysis     — export function entry-points for reachability
//
import { app, BrowserWindow, ipcMain } from 'electron';
import { createRequire } from 'module';
import { buildConfig } from './config';

const API_URL = 'https://api.example.com';
const ENV_MODE = process.env.ELECTRON_DEV ?? 'production';
const APP_VERSION = import.meta.env.VITE_APP_VERSION ?? '1.0.0';

// ── pure helper ───────────────────────────────────────────────────────────────
// Proves pure_function_tagging: no side effects on formatTitle.
export function formatTitle(name: string): string {
  return `App — ${name}`;
}

// ── http_effect ───────────────────────────────────────────────────────────────
// Proves http_effect: httpFetch calls the Fetch API.
export async function httpFetch(path: string) {
  const endpoint = `${API_URL}/${path}`;
  const resp = await fetch(endpoint);
  const data = await resp.json();
  return data;
}

// ── fs_effect ─────────────────────────────────────────────────────────────────
// Proves fs_effect: Electron main-process uses Node.js fs directly.
export async function readConfig(path: string) {
  return await fs.readFile(path, 'utf8');
}

export async function writeLog(path: string, entry: string) {
  await fs.writeFile(path, entry);
}

// ── db_effect ─────────────────────────────────────────────────────────────────
// Proves db_effect: Electron main-process can use a Node.js ORM.
// .findOne() → jstsDBReadRe; .create() → jstsDBWriteRe
export async function queryDB(id: string) {
  return await db.findOne({ where: { id } });
}

export async function insertRecord(record: object) {
  return await db.create(record);
}

// ── mutation_effect ───────────────────────────────────────────────────────────
// Proves mutation_effect: setWindow assigns this._win.
class AppState {
  _win: BrowserWindow | null = null;

  setWindow(win: BrowserWindow) {
    this._win = win;
  }
}

// ── def_use_chain_extraction ──────────────────────────────────────────────────
// Proves def_use_chain_extraction: const endpoint / resp / data in httpFetch
// (already in httpFetch above; use a second function to confirm multiple sites).
export async function loadSettings(key: string) {
  const endpoint = `${API_URL}/settings`;
  const resp = await fetch(endpoint);
  const data = await resp.json();
  return data[key];
}

// ── template_pattern_catalog ──────────────────────────────────────────────────
// Proves template_pattern_catalog: i18n t(), log console.error, SQL literal.
export function appTitle(section: string): string {
  const title = t('app.title');
  console.error('Electron error in section: %s', section);
  const query = `SELECT id, name FROM settings WHERE key = 'active'`;
  return `${title}: ${section}`;
}

// ── request_shape_extraction ─────────────────────────────────────────────────
// Proves request_shape_extraction: axios POST body shape.
export async function postEvent(eventType: string, payload: string) {
  return await axios.post(`${API_URL}/events`, { eventType, payload });
}

// ── taint source / sink / sanitizer ──────────────────────────────────────────
// Electron main-process IPC handlers receive untrusted renderer input.
ipcMain.handle('user:render', async (event: any, req: any) => {
  const content = req.body.content;
  const clean = DOMPurify.sanitize(content);
  const unsafe = { dangerouslySetInnerHTML: { __html: content } };
  return { clean, unsafe };
});
