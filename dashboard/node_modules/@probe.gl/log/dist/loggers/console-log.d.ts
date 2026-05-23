import { LogFunction } from "./logger.js";
import { BaseLog, NormalizedLogArguments } from "./base-log.js";
/**
 * Minimal console-backed logger. Does not persist configuration or implement log levels.
 * Intended as a lightweight Logger implementation.
 */
export declare class ConsoleLog extends BaseLog {
    warn(message: string, ...args: unknown[]): LogFunction;
    error(message: string, ...args: unknown[]): LogFunction;
    log(logLevel: any, message?: any, ...args: unknown[]): LogFunction;
    info(logLevel: any, message?: any, ...args: unknown[]): LogFunction;
    once(logLevel: any, message?: any, ...args: unknown[]): LogFunction;
    protected _emit(_type: string, normalized: NormalizedLogArguments): LogFunction;
}
//# sourceMappingURL=console-log.d.ts.map