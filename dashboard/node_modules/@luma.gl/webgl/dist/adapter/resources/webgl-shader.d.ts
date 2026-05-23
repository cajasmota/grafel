import { Shader, ShaderProps, CompilerMessage } from '@luma.gl/core';
import { WebGLDevice } from "../webgl-device.js";
/**
 * An immutable compiled shader program that execute portions of the GPU Pipeline
 */
export declare class WEBGLShader extends Shader {
    readonly device: WebGLDevice;
    readonly handle: WebGLShader;
    constructor(device: WebGLDevice, props: ShaderProps);
    destroy(): void;
    get asyncCompilationStatus(): Promise<'pending' | 'success' | 'error'>;
    getCompilationInfo(): Promise<readonly CompilerMessage[]>;
    getCompilationInfoSync(): readonly CompilerMessage[];
    getTranslatedSource(): string | null;
    /** Compile a shader and get compilation status */
    protected _compile(source: string): Promise<void>;
    /** Use KHR_parallel_shader_compile extension if available */
    protected _waitForCompilationComplete(): Promise<void>;
    /**
     * Get the shader compilation status
     * TODO - Load log even when no error reported, to catch warnings?
     * https://gamedev.stackexchange.com/questions/30429/how-to-detect-glsl-warnings
     */
    protected _getCompilationStatus(): void;
}
//# sourceMappingURL=webgl-shader.d.ts.map