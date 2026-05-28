// Proving fixture for Electron ipc_extraction, main_renderer_split, and
// native_module_imports (#2865). The YAML rule
// internal/engine/rules/javascript_typescript/frameworks/electron.yaml mines:
//   - IPC: ipcMain handle/on channels, ipcRenderer invoke/send channels,
//          contextBridge exposeInMainWorld API surface
//   - process split: BrowserWindow construction + app lifecycle
//   - native modules: bindings loader, node-gyp-build, compiled .node addons
import { app, BrowserWindow, ipcMain } from 'electron';
const bindings = require('bindings');
const addon = require('./build/Release/native.node');

// Main process: window creation + lifecycle + IPC handlers.
app.whenReady().then(() => {
  const win = new BrowserWindow({ width: 800, height: 600 });
  win.loadFile('index.html');
});

app.on('window-all-closed', () => app.quit());

ipcMain.handle('dialog:openFile', async () => showOpenDialog());
ipcMain.on('log:write', (event, line) => writeLog(line));
