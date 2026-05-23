/**
 * Make browser return WebGL2 contexts even if WebGL1 contexts are requested
 * @param enforce
 * @returns
 */
export declare function enforceWebGL2(enforce?: boolean): void;
/** Install WebGL1-only extensions on WebGL2 contexts */
export declare function polyfillWebGL1Extensions(gl: WebGL2RenderingContext): void;
//# sourceMappingURL=polyfill-webgl1-extensions.d.ts.map