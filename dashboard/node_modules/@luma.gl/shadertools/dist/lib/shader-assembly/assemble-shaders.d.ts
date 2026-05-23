import { PlatformInfo } from "./platform-info.js";
import type { ShaderInjection } from "./shader-injections.js";
import type { ShaderModule } from "../shader-module/shader-module.js";
import { ShaderHook } from "./shader-hooks.js";
/**
 * Options for `ShaderAssembler.assembleShaders()`
 */
export type AssembleShaderProps = AssembleShaderOptions & {
    platformInfo: PlatformInfo;
    /** WGSL: single shader source. */
    source?: string | null;
    /** GLSL vertex shader source. */
    vs?: string | null;
    /** GLSL fragment shader source. */
    fs?: string | null;
};
export type AssembleShaderOptions = {
    /** information about the platform (which shader language & version, extensions etc.) */
    platformInfo: PlatformInfo;
    /** Inject shader id #defines */
    id?: string;
    /** Modules to be injected */
    modules?: ShaderModule[];
    /** Defines to be injected */
    defines?: Record<string, boolean>;
    /** GLSL only: Overrides to be injected. In WGSL these are supplied during Pipeline creation time */
    constants?: Record<string, number>;
    /** Hook functions */
    hookFunctions?: (ShaderHook | string)[];
    /** Code injections */
    inject?: Record<string, string | ShaderInjection>;
    /** Whether to inject prologue */
    prologue?: boolean;
    /** logger object */
    log?: any;
};
type AssembleStageOptions = {
    /** Inject shader id #defines */
    id?: string;
    /** Vertex shader */
    source: string;
    stage: 'vertex' | 'fragment';
    /** Modules to be injected */
    modules: any[];
    /** Defines to be injected */
    defines?: Record<string, boolean>;
    /** GLSL only: Overrides to be injected. In WGSL these are supplied during Pipeline creation time */
    constants?: Record<string, number>;
    /** Hook functions */
    hookFunctions?: (ShaderHook | string)[];
    /** Code injections */
    inject?: Record<string, string | ShaderInjection>;
    /** Whether to inject prologue */
    prologue?: boolean;
    /** logger object */
    log?: any;
};
export type HookFunction = {
    hook: string;
    header: string;
    footer: string;
    signature?: string;
};
/**
 * getUniforms function returned from the shader module system
 */
export type GetUniformsFunc = (opts: Record<string, any>) => Record<string, any>;
/**
 * Inject a list of shader modules into a single shader source for WGSL
 */
export declare function assembleWGSLShader(options: AssembleShaderOptions & {
    /** Single WGSL shader */
    source: string;
}): {
    source: string;
    getUniforms: GetUniformsFunc;
};
/**
 * Injects dependent shader module sources into pair of main vertex/fragment shader sources for GLSL
 */
export declare function assembleGLSLShaderPair(options: AssembleShaderOptions & {
    /** Vertex shader */
    vs: string;
    /** Fragment shader */
    fs?: string;
}): {
    vs: string;
    fs: string;
    getUniforms: GetUniformsFunc;
};
/**
 * Pulls together complete source code for either a vertex or a fragment shader
 * adding prologues, requested module chunks, and any final injections.
 * @param gl
 * @param options
 * @returns
 */
export declare function assembleShaderWGSL(platformInfo: PlatformInfo, options: AssembleStageOptions): string;
/**
 * Returns a combined `getUniforms` covering the options for all the modules,
 * the created function will pass on options to the inidividual `getUniforms`
 * function of each shader module and combine the results into one object that
 * can be passed to setUniforms.
 * @param modules
 * @returns
 */
export declare function assembleGetUniforms(modules: ShaderModule[]): (opts: Record<string, any>) => Record<string, any>;
/** Extracts the source code chunk for the specified shader type from the named shader module */
export declare function getShaderModuleSource(module: ShaderModule, stage: 'vertex' | 'fragment' | 'wgsl'): string;
export {};
//# sourceMappingURL=assemble-shaders.d.ts.map