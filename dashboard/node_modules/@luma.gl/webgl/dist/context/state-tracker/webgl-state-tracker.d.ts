/**
 * Support for listening to context state changes and intercepting state queries
 * NOTE: this system does not handle buffer bindings
 */
export declare class WebGLStateTracker {
    static get(gl: WebGL2RenderingContext): WebGLStateTracker;
    gl: WebGL2RenderingContext;
    program: unknown;
    stateStack: object[];
    enable: boolean;
    cache: Record<string, any>;
    log: any;
    protected initialized: boolean;
    constructor(gl: WebGL2RenderingContext, props?: {
        log: any;
    });
    push(values?: {}): void;
    pop(): void;
    /**
     * Initialize WebGL state caching on a context
     * can be called multiple times to enable/disable
     *
     * @note After calling this function, context state will be cached
     * .push() and .pop() will be available for saving,
     * temporarily modifying, and then restoring state.
     */
    trackState(gl: WebGL2RenderingContext, options?: {
        copyState?: boolean;
    }): void;
    /**
    // interceptor for context set functions - update our cache and our stack
    // values (Object) - the key values for this setter
     * @param values
     * @returns
     */
    _updateCache(values: {
        [key: number | string]: any;
    }): {
        valueChanged: boolean;
        oldValue: any;
    };
}
//# sourceMappingURL=webgl-state-tracker.d.ts.map