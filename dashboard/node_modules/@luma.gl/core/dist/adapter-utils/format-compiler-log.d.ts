import type { CompilerMessage } from "../adapter/types/compiler-message.js";
/** @returns annotated errors or warnings */
export declare function formatCompilerLog(shaderLog: readonly CompilerMessage[], source: string, options?: {
    /** Include source code in the log. Either just the lines before issues or all source code */
    showSourceCode?: 'no' | 'issues' | 'all';
    html?: boolean;
}): string;
//# sourceMappingURL=format-compiler-log.d.ts.map