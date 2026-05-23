import { Logger, LogFunction } from "./logger.js";
import { NormalizedArguments } from "./log-utils.js";
declare const noop: () => void;
export type NormalizedLogArguments = NormalizedArguments;
type LogType = 'log' | 'info' | 'once' | 'warn' | 'error' | 'table' | 'group' | 'groupEnd' | 'time';
type LogOptions = Partial<NormalizedLogArguments>;
/**
 * Base logger that implements log level handling and once de-duplication.
 * Concrete loggers implement `_emit` to perform actual output.
 */
export declare abstract class BaseLog implements Logger {
    userData: Record<string, unknown>;
    protected _level: number;
    protected _onceCache: Set<unknown>;
    constructor({ level }?: {
        level?: number;
    });
    set level(newLevel: number);
    get level(): number;
    setLevel(level: number): this;
    getLevel(): number;
    warn(message: string, ...args: unknown[]): LogFunction;
    error(message: string, ...args: unknown[]): LogFunction;
    log(logLevel: any, message?: any, ...args: unknown[]): LogFunction;
    info(logLevel: any, message?: any, ...args: unknown[]): LogFunction;
    once(logLevel: any, message?: any, ...args: unknown[]): LogFunction;
    protected _log(type: LogType, logLevel: unknown, message: unknown, args: unknown[], options?: LogOptions): LogFunction;
    protected _buildArgs(logLevel: unknown, message: unknown, args: unknown[]): unknown[];
    protected _createLogFunction(type: LogType, normalized: NormalizedLogArguments, options: LogOptions): LogFunction;
    protected _shouldLog(logLevel: unknown): boolean;
    protected _getOnceTag(tag: unknown): unknown;
    /** Create the actual log function for this logger implementation. */
    protected abstract _emit(type: LogType, normalized: NormalizedLogArguments): LogFunction;
}
export { noop };
//# sourceMappingURL=base-log.d.ts.map