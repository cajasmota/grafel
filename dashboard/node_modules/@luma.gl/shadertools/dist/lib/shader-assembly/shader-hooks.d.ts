import { ShaderInjection } from "./shader-injections.js";
/**
 * The shader hook mechanism allows the application to create shaders
 * that can be automatically extended by the shader modules the application
 * includes.
 *
 * A shader hook function that shader modules can inject code into.
 * Shaders can call these functions, which will be no-ops by default.
 *
 * If a shader module injects code it will be executed upon the hook
 * function call.
 */
export type ShaderHook = {
    /** `vs:` or `fs:` followed by the name and arguments of the function, e.g. `vs:MYHOOK_func(inout vec4 value)`. Hook name without arguments
    will also be used as the name of the shader hook */
    hook: string;
    /** Code always included at the beginning of a hook function */
    header: string;
    /** Code always included at the end of a hook function */
    footer: string;
    /** To Be Documented */
    signature?: string;
};
/** Normalized shader hooks per shader */
export type ShaderHooks = {
    /** Normalized shader hooks for vertex shader */
    vertex: Record<string, ShaderHook>;
    /** Normalized shader hooks for fragment shader */
    fragment: Record<string, ShaderHook>;
};
/** Generate hook source code */
export declare function getShaderHooks(hookFunctions: Record<string, ShaderHook>, hookInjections: Record<string, ShaderInjection[]>): string;
/**
 * Parse string based hook functions
 * And split per shader
 */
export declare function normalizeShaderHooks(hookFunctions: (string | ShaderHook)[]): ShaderHooks;
//# sourceMappingURL=shader-hooks.d.ts.map