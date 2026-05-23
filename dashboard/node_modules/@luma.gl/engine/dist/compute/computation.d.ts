import type { ComputePipelineProps, Shader, Binding } from '@luma.gl/core';
import { Device, Buffer, ComputePipeline, ComputePass, UniformStore } from '@luma.gl/core';
import type { ShaderModule, PlatformInfo } from '@luma.gl/shadertools';
import { ShaderAssembler } from '@luma.gl/shadertools';
import { TypedArray } from '@math.gl/types';
import { ShaderInputs } from "../shader-inputs.js";
import { PipelineFactory } from "../factories/pipeline-factory.js";
import { ShaderFactory } from "../factories/shader-factory.js";
export type ComputationProps = Omit<ComputePipelineProps, 'shader'> & {
    source?: string;
    /** shadertool shader modules (added to shader code) */
    modules?: ShaderModule[];
    /** Shadertool module defines (configures shader code)*/
    defines?: Record<string, boolean>;
    /** Shader inputs, used to generated uniform buffers and bindings */
    shaderInputs?: ShaderInputs;
    /** Bindings */
    bindings?: Record<string, Binding>;
    /** Show shader source in browser? */
    debugShaders?: 'never' | 'errors' | 'warnings' | 'always';
    /** Factory used to create a {@link ComputePipeline}. Defaults to {@link Device} default factory. */
    pipelineFactory?: PipelineFactory;
    /** Factory used to create a {@link Shader}. Defaults to {@link Device} default factory. */
    shaderFactory?: ShaderFactory;
    /** Shader assembler. Defaults to the ShaderAssembler.getShaderAssembler() */
    shaderAssembler?: ShaderAssembler;
};
/**
 * v9 Model API
 * A model
 * - automatically reuses pipelines (programs) when possible
 * - automatically rebuilds pipelines if necessary to accommodate changed settings
 * shadertools integration
 * - accepts modules and performs shader transpilation
 */
export declare class Computation {
    static defaultProps: Required<ComputationProps>;
    readonly device: Device;
    readonly id: string;
    readonly pipelineFactory: PipelineFactory;
    readonly shaderFactory: ShaderFactory;
    userData: {
        [key: string]: any;
    };
    /** Bindings (textures, samplers, uniform buffers) */
    bindings: Record<string, Binding>;
    /** The underlying GPU pipeline. */
    pipeline: ComputePipeline;
    /** Assembled compute shader source */
    source: string;
    /** the underlying compiled compute shader */
    shader: Shader;
    /** ShaderInputs instance */
    shaderInputs: ShaderInputs;
    _uniformStore: UniformStore;
    _pipelineNeedsUpdate: string | false;
    private _getModuleUniforms;
    private props;
    private _destroyed;
    constructor(device: Device, props: ComputationProps);
    destroy(): void;
    predraw(): void;
    dispatch(computePass: ComputePass, x: number, y?: number, z?: number): void;
    /**
     * Updates the vertex count (used in draw calls)
     * @note Any attributes with stepMode=vertex need to be at least this big
     */
    setVertexCount(vertexCount: number): void;
    /**
     * Updates the instance count (used in draw calls)
     * @note Any attributes with stepMode=instance need to be at least this big
     */
    setInstanceCount(instanceCount: number): void;
    setShaderInputs(shaderInputs: ShaderInputs): void;
    /**
     * Updates shader module settings (which results in uniforms being set)
     */
    setShaderModuleProps(props: Record<string, any>): void;
    updateShaderInputs(): void;
    /**
     * Sets bindings (textures, samplers, uniform buffers)
     */
    setBindings(bindings: Record<string, Binding>): void;
    _setPipelineNeedsUpdate(reason: string): void;
    _updatePipeline(): ComputePipeline;
    /** Throttle draw call logging */
    _lastLogTime: number;
    _logOpen: boolean;
    _logDrawCallStart(): void;
    _logDrawCallEnd(): void;
    protected _drawCount: number;
    _getBufferOrConstantValues(attribute: Buffer | TypedArray, dataType: any): string;
}
/** Create a shadertools platform info from the Device */
export declare function getPlatformInfo(device: Device): PlatformInfo;
//# sourceMappingURL=computation.d.ts.map