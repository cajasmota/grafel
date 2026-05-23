/**
 * ContextProps
 * @param onContextLost
 * @param onContextRestored *
 */
type ContextProps = {
    /** Called when a context is lost */
    onContextLost: (event: Event) => void;
    /** Called when a context is restored */
    onContextRestored: (event: Event) => void;
};
/**
 * Create a WebGL context for a canvas
 * Note calling this multiple time on the same canvas does return the same context
 * @param canvas A canvas element or offscreen canvas
 */
export declare function createBrowserContext(canvas: HTMLCanvasElement | OffscreenCanvas, props: ContextProps, webglContextAttributes: WebGLContextAttributes): WebGL2RenderingContext;
export {};
//# sourceMappingURL=create-browser-context.d.ts.map