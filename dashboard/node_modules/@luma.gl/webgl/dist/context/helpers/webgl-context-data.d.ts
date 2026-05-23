/**
 * Stores luma.gl specific state associated with a context
 */
export interface WebGLContextData {
    _polyfilled: boolean;
    _extensions: Record<string, any>;
}
/**
 * Gets luma.gl specific state from a context
 * @returns context state
 */
export declare function getWebGLContextData(gl: WebGL2RenderingContext): WebGLContextData;
//# sourceMappingURL=webgl-context-data.d.ts.map