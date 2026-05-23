export declare enum StatusCode {
    SUCCESS = 0,
    MAX_ARROW_ERROR = 255,
    DUCKDB_WASM_RETRY = 256
}
export declare function IsArrowBuffer(status: StatusCode): boolean;
export declare function IsDuckDBWasmRetry(status: StatusCode): boolean;
