type DebugContextProps = {
    debugWebGL?: boolean;
    traceWebGL?: boolean;
};
declare global {
    var WebGLDebugUtils: any;
}
/**
 * Loads Khronos WebGLDeveloperTools from CDN if not already installed
 * const WebGLDebugUtils = require('webgl-debug');
 * @see https://github.com/KhronosGroup/WebGLDeveloperTools
 * @see https://github.com/vorg/webgl-debug
 */
export declare function loadWebGLDeveloperTools(): Promise<void>;
export declare function makeDebugContext(gl: WebGL2RenderingContext, props?: DebugContextProps): WebGL2RenderingContext;
export {};
//# sourceMappingURL=webgl-developer-tools.d.ts.map