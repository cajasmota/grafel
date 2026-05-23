export type FormatValueOptions = {
    isInteger?: boolean;
    maxElts?: number;
    size?: number;
};
/**
 * Format time
 */
export declare function formatTime(ms: number): string;
export declare function leftPad(string: string, length?: number): string;
export declare function rightPad(string: string, length?: number): string;
export declare function formatValue(v: unknown, options?: FormatValueOptions): string;
//# sourceMappingURL=formatters.d.ts.map