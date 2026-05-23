import type { CompilerMessage } from '@luma.gl/core';
/**
 * Parse a WebGL-format GLSL compilation log into an array of WebGPU style message records.
 * This follows documented WebGL conventions for compilation logs.
 * Based on https://github.com/wwwtyro/gl-format-compiler-error (public domain)
 */
export declare function parseShaderCompilerLog(errLog: string): readonly CompilerMessage[];
//# sourceMappingURL=parse-shader-compiler-log.d.ts.map