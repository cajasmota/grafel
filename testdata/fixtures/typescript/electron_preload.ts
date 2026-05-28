// Proving fixture (renderer/preload side) for Electron ipc_extraction +
// main_renderer_split (#2865). contextBridge.exposeInMainWorld defines the
// secure API surface; ipcRenderer.invoke/send call the channels handled by
// the main process in electron_ipc.ts, so the two processes share the same
// channel-name entities (the cross-process split made explicit).
import { contextBridge, ipcRenderer } from 'electron';

contextBridge.exposeInMainWorld('electronAPI', {
  openFile: () => ipcRenderer.invoke('dialog:openFile'),
  log: (line: string) => ipcRenderer.send('log:write', line),
});
