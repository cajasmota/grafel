export type LogFunction = () => void;
/** A common interface for loggers */
export interface Logger {
    /** Warn, but only once, no console flooding */
    warn(message: string, ...args: unknown[]): LogFunction;
    /** Print an error */
    error(message: string, ...args: unknown[]): LogFunction;
    /** Log a debug message */
    log(logLevel: any, message?: any, ...args: unknown[]): LogFunction;
    /** Log a normal message */
    info(logLevel: any, message?: any, ...args: unknown[]): LogFunction;
    /** Log a normal message, but only once, no console flooding */
    once(logLevel: any, message?: any, ...args: unknown[]): LogFunction;
}
//# sourceMappingURL=logger.d.ts.map