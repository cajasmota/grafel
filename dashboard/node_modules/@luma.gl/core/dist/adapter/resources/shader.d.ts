import type { Device } from "../device.js";
import { Resource, ResourceProps } from "./resource.js";
import { CompilerMessage } from "../types/compiler-message.js";
/**
 * Properties for a Shader
 */
export type ShaderProps = ResourceProps & {
    /** Shader language (defaults to auto) */
    language?: 'glsl' | 'wgsl' | 'auto';
    /** Which stage are we compiling? Required for GLSL. Ignored for WGSL. */
    stage?: 'vertex' | 'fragment' | 'compute';
    /** Shader source code */
    source: string;
    /** Optional shader source map (WebGPU only) */
    sourceMap?: string | null;
    /** Optional shader entry point (WebGPU only) */
    entryPoint?: string;
    /** Show shader source in browser? Overrides the device.props.debugShaders setting */
    debugShaders?: 'never' | 'errors' | 'warnings' | 'always';
};
/**
 * Immutable Shader object
 * In WebGPU the handle can be copied between threads
 */
export declare abstract class Shader extends Resource<ShaderProps> {
    get [Symbol.toStringTag](): string;
    /** The stage of this shader */
    readonly stage: 'vertex' | 'fragment' | 'compute';
    /** The source code of this shader */
    readonly source: string;
    /** The compilation status of the shader. 'pending' if compilation is asynchronous, and on production */
    compilationStatus: 'pending' | 'success' | 'error';
    /** Create a new Shader instance */
    constructor(device: Device, props: ShaderProps);
    abstract get asyncCompilationStatus(): Promise<'pending' | 'success' | 'error'>;
    /** Get compiler log asynchronously */
    abstract getCompilationInfo(): Promise<readonly CompilerMessage[]>;
    /** Get compiler log synchronously (WebGL only) */
    getCompilationInfoSync(): readonly CompilerMessage[] | null;
    /** Get translated shader source in host platform's native language (HLSL, GLSL, and even GLSL ES), if available */
    getTranslatedSource(): string | null;
    /** In browser logging of errors */
    debugShader(): Promise<void>;
    /**
     * In-browser UI logging of errors
     * TODO - this HTML formatting code should not be in Device, should be pluggable
     */
    protected _displayShaderLog(messages: readonly CompilerMessage[], shaderId: string): void;
    static defaultProps: Required<ShaderProps>;
}
//# sourceMappingURL=shader.d.ts.map