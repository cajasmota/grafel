import { DuckDBFileInfo, DuckDBGlobalFileInfo, DuckDBRuntime } from './runtime';
import { DuckDBModule } from './duckdb_module';
export declare const BROWSER_RUNTIME: DuckDBRuntime & {
    _files: Map<string, any>;
    _fileInfoCache: Map<number, DuckDBFileInfo>;
    _globalFileInfo: DuckDBGlobalFileInfo | null;
    _preparedHandles: Record<string, FileSystemSyncAccessHandle>;
    _opfsRoot: FileSystemDirectoryHandle | null;
    getFileInfo(mod: DuckDBModule, fileId: number): DuckDBFileInfo | null;
    getGlobalFileInfo(mod: DuckDBModule): DuckDBGlobalFileInfo | null;
    assignOPFSRoot(): Promise<void>;
};
export default BROWSER_RUNTIME;
