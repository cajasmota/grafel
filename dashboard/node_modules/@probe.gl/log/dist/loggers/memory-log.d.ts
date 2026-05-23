import { LogFunction } from "./logger.js";
import { BaseLog, NormalizedLogArguments } from "./base-log.js";
export type MemoryLogMessage = {
    level: number;
    type: 'warning' | 'error' | 'log' | 'info' | 'once' | 'table';
    message: string;
    args: unknown[];
};
export declare class MemoryLog extends BaseLog {
    userData: Record<string, unknown>;
    messages: MemoryLogMessage[];
    onMessage?: ((message: MemoryLogMessage) => void) | undefined;
    constructor(options?: {
        onMessage?: (message: MemoryLogMessage) => void;
        level?: number;
    });
    protected _emit(type: string, normalized: NormalizedLogArguments): LogFunction;
    private _normalizeType;
}
//# sourceMappingURL=memory-log.d.ts.map