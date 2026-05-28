package engine

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// electronMainFixture mirrors the main-process half of
// testdata/fixtures/typescript/electron_ipc.ts. Kept inline so the test
// exercises the REAL embedded electron.yaml rule (LoadAllRules).
const electronMainFixture = `
import { app, BrowserWindow, ipcMain } from 'electron';
const bindings = require('bindings');
const addon = require('./build/Release/native.node');

app.whenReady().then(() => {
  const win = new BrowserWindow({ width: 800, height: 600 });
  win.loadFile('index.html');
});
app.on('window-all-closed', () => app.quit());

ipcMain.handle('dialog:openFile', async () => showOpenDialog());
ipcMain.on('log:write', (event, line) => writeLog(line));
`

// electronRendererFixture is the renderer/preload side: contextBridge exposes
// an API, ipcRenderer invokes/sends the matching channels.
const electronRendererFixture = `
import { contextBridge, ipcRenderer } from 'electron';

contextBridge.exposeInMainWorld('electronAPI', {
  openFile: () => ipcRenderer.invoke('dialog:openFile'),
  log: (line) => ipcRenderer.send('log:write', line),
});
`

func detectElectron(t *testing.T, src string) map[string]bool {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "main.ts",
		Content:  []byte(src),
		Language: "javascript_typescript",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	names := map[string]bool{}
	for _, e := range result.Entities {
		names[e.Name] = true
	}
	return names
}

// TestDetect_Electron_MainProcess proves ipc_extraction (main-side handlers),
// main_renderer_split (BrowserWindow + app lifecycle), and
// native_module_imports (bindings + .node addon) from the shipped rule.
func TestDetect_Electron_MainProcess(t *testing.T) {
	names := detectElectron(t, electronMainFixture)

	// ipc_extraction — channel names captured from ipcMain.handle / .on.
	if !names["dialog:openFile"] {
		t.Errorf("expected ipcMain.handle channel 'dialog:openFile'; got %v", keys(names))
	}
	if !names["log:write"] {
		t.Errorf("expected ipcMain.on channel 'log:write'; got %v", keys(names))
	}

	// main_renderer_split — main-process markers.
	if !names["new BrowserWindow("] {
		t.Errorf("expected BrowserWindow main-process marker; got %v", keys(names))
	}
	if !names["app.whenReady("] && !names["app.on("] {
		t.Errorf("expected app lifecycle marker; got %v", keys(names))
	}

	// native_module_imports — bindings + the compiled .node addon path.
	if !names["require('bindings')"] {
		t.Errorf("expected bindings() native loader; got %v", keys(names))
	}
	if !names["./build/Release/native.node"] {
		t.Errorf("expected .node native addon import; got %v", keys(names))
	}
}

// TestDetect_Electron_RendererProcess proves the renderer/preload half of
// ipc_extraction + main_renderer_split: contextBridge exposure and
// ipcRenderer invoke/send channels (matching the main-side handlers above, so
// the two processes share channel-name entities).
func TestDetect_Electron_RendererProcess(t *testing.T) {
	names := detectElectron(t, electronRendererFixture)

	if !names["electronAPI"] {
		t.Errorf("expected contextBridge.exposeInMainWorld('electronAPI'); got %v", keys(names))
	}
	if !names["dialog:openFile"] {
		t.Errorf("expected ipcRenderer.invoke channel 'dialog:openFile'; got %v", keys(names))
	}
	if !names["log:write"] {
		t.Errorf("expected ipcRenderer.send channel 'log:write'; got %v", keys(names))
	}
}
