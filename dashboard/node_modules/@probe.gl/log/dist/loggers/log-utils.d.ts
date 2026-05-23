/**
 * Get logLevel from first argument:
 * - log(logLevel, message, args) => logLevel
 * - log(message, args) => 0
 * - log({logLevel, ...}, message, args) => logLevel
 * - log({logLevel, message, args}) => logLevel
 */
export declare function normalizeLogLevel(logLevel: unknown): number;
/**
 * "Normalizes" the various argument patterns into an object with known types
 * - log(logLevel, message, args) => {logLevel, message, args}
 * - log(message, args) => {logLevel: 0, message, args}
 * - log({logLevel, ...}, message, args) => {logLevel, message, args}
 * - log({logLevel, message, args}) => {logLevel, message, args}
 */
export declare function normalizeArguments(opts: {
    logLevel: any;
    message: any;
    collapsed?: boolean;
    args?: IArguments | any[] | undefined;
    opts?: any;
}): NormalizedArguments;
export type NormalizedArguments = {
    logLevel: number;
    message: any;
    args: any[];
    tag?: unknown;
    method?: Function;
    once?: boolean;
    total?: number;
    delta?: number;
    [key: string]: any;
};
//# sourceMappingURL=log-utils.d.ts.map