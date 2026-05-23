import type { TypedArray } from '@math.gl/types';
import type { RenderPipelineProps, RenderPipelineParameters, BufferLayout, VertexArray, TransformFeedback, AttributeInfo, Binding, PrimitiveTopology } from '@luma.gl/core';
import { Device, Buffer, RenderPipeline, RenderPass, UniformStore } from '@luma.gl/core';
import type { ShaderModule, PlatformInfo } from '@luma.gl/shadertools';
import { ShaderAssembler } from '@luma.gl/shadertools';
import type { Geometry } from "../geometry/geometry.js";
import { GPUGeometry } from "../geometry/gpu-geometry.js";
import { PipelineFactory } from "../factories/pipeline-factory.js";
import { ShaderFactory } from "../factories/shader-factory.js";
import { ShaderInputs } from "../shader-inputs.js";
import { AsyncTexture } from "../async-texture/async-texture.js";
export type ModelProps = Omit<RenderPipelineProps, 'vs' | 'fs' | 'bindings'> & {
    source?: string;
    vs?: string | null;
    fs?: string | null;
    /** shadertool shader modules (added to shader code) */
    modules?: ShaderModule[];
    /** Shadertool module defines (configures shader code)*/
    defines?: Record<string, boolean>;
    /** Shader inputs, used to generated uniform buffers and bindings */
    shaderInputs?: ShaderInputs;
    /** Bindings */
    bindings?: Record<string, Binding | AsyncTexture>;
    /** Parameters that are built into the pipeline */
    parameters?: RenderPipelineParameters;
    /** Geometry */
    geometry?: GPUGeometry | Geometry | null;
    /** @deprecated Use instanced rendering? Will be auto-detected in 9.1 */
    isInstanced?: boolean;
    /** instance count */
    instanceCount?: number;
    /** Vertex count */
    vertexCount?: number;
    indexBuffer?: Buffer | null;
    /** @note this is really a map of buffers, not a map of attributes */
    attributes?: Record<string, Buffer>;
    /**   */
    constantAttributes?: Record<string, TypedArray>;
    /** Some applications intentionally supply unused attributes and bindings, and want to disable warnings */
    disableWarnings?: boolean;
    /** @internal For use with {@link TransformFeedback}, WebGL only. */
    varyings?: string[];
    transformFeedback?: TransformFeedback;
    /** Show shader source in browser? */
    debugShaders?: 'never' | 'errors' | 'warnings' | 'always';
    /** Factory used to create a {@link RenderPipeline}. Defaults to {@link Device} default factory. */
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
export declare class Model {
    static defaultProps: Required<ModelProps>;
    readonly device: Device;
    readonly id: string;
    readonly source: string;
    readonly vs: string;
    readonly fs: string;
    readonly pipelineFactory: PipelineFactory;
    readonly shaderFactory: ShaderFactory;
    userData: {
        [key: string]: any;
    };
    /** The render pipeline GPU parameters, depth testing etc */
    parameters: RenderPipelineParameters;
    /** The primitive topology */
    topology: PrimitiveTopology;
    /** Buffer layout */
    bufferLayout: BufferLayout[];
    /** Use instanced rendering */
    isInstanced: boolean | undefined;
    /** instance count. `undefined` means not instanced */
    instanceCount: number;
    /** Vertex count */
    vertexCount: number;
    /** Index buffer */
    indexBuffer: Buffer | null;
    /** Buffer-valued attributes */
    bufferAttributes: Record<string, Buffer>;
    /** Constant-valued attributes */
    constantAttributes: Record<string, TypedArray>;
    /** Bindings (textures, samplers, uniform buffers) */
    bindings: Record<string, Binding | AsyncTexture>;
    /**
     * VertexArray
     * @note not implemented: if bufferLayout is updated, vertex array has to be rebuilt!
     * @todo - allow application to define multiple vertex arrays?
     * */
    vertexArray: VertexArray;
    /** TransformFeedback, WebGL 2 only. */
    transformFeedback: TransformFeedback | null;
    /** The underlying GPU "program". @note May be recreated if parameters change */
    pipeline: RenderPipeline;
    /** ShaderInputs instance */
    shaderInputs: ShaderInputs;
    _uniformStore: UniformStore;
    _attributeInfos: Record<string, AttributeInfo>;
    _gpuGeometry: GPUGeometry | null;
    private props;
    _pipelineNeedsUpdate: string | false;
    private _needsRedraw;
    private _destroyed;
    /** "Time" of last draw. Monotonically increasing timestamp */
    _lastDrawTimestamp: number;
    get [Symbol.toStringTag](): string;
    toString(): string;
    constructor(device: Device, props: ModelProps);
    destroy(): void;
    /** Query redraw status. Clears the status. */
    needsRedraw(): false | string;
    /** Mark the model as needing a redraw */
    setNeedsRedraw(reason: string): void;
    predraw(): void;
    draw(renderPass: RenderPass): boolean;
    /**
     * Updates the optional geometry
     * Geometry, set topology and bufferLayout
     * @note Can trigger a pipeline rebuild / pipeline cache fetch on WebGPU
     */
    setGeometry(geometry: GPUGeometry | Geometry | null): void;
    /**
     * Updates the primitive topology ('triangle-list', 'triangle-strip' etc).
     * @note Triggers a pipeline rebuild / pipeline cache fetch on WebGPU
     */
    setTopology(topology: PrimitiveTopology): void;
    /**
     * Updates the buffer layout.
     * @note Triggers a pipeline rebuild / pipeline cache fetch
     */
    setBufferLayout(bufferLayout: BufferLayout[]): void;
    /**
     * Set GPU parameters.
     * @note Can trigger a pipeline rebuild / pipeline cache fetch.
     * @param parameters
     */
    setParameters(parameters: RenderPipelineParameters): void;
    /**
     * Updates the instance count (used in draw calls)
     * @note Any attributes with stepMode=instance need to be at least this big
     */
    setInstanceCount(instanceCount: number): void;
    /**
     * Updates the vertex count (used in draw calls)
     * @note Any attributes with stepMode=vertex need to be at least this big
     */
    setVertexCount(vertexCount: number): void;
    /** Set the shader inputs */
    setShaderInputs(shaderInputs: ShaderInputs): void;
    /** Update uniform buffers from the model's shader inputs */
    updateShaderInputs(): void;
    /**
     * Sets bindings (textures, samplers, uniform buffers)
     */
    setBindings(bindings: Record<string, Binding | AsyncTexture>): void;
    /**
     * Updates optional transform feedback. WebGL only.
     */
    setTransformFeedback(transformFeedback: TransformFeedback | null): void;
    /**
     * Sets the index buffer
     * @todo - how to unset it if we change geometry?
     */
    setIndexBuffer(indexBuffer: Buffer | null): void;
    /**
     * Sets attributes (buffers)
     * @note Overrides any attributes previously set with the same name
     */
    setAttributes(buffers: Record<string, Buffer>, options?: {
        disableWarnings?: boolean;
    }): void;
    /**
     * Sets constant attributes
     * @note Overrides any attributes previously set with the same name
     * Constant attributes are only supported in WebGL, not in WebGPU
     * Any attribute that is disabled in the current vertex array object
     * is read from the context's global constant value for that attribute location.
     * @param constantAttributes
     */
    setConstantAttributes(attributes: Record<string, TypedArray>, options?: {
        disableWarnings?: boolean;
    }): void;
    /** Check that bindings are loaded. Returns id of first binding that is still loading. */
    _areBindingsLoading(): string | false;
    /** Extracts texture view from loaded async textures. Returns null if any textures have not yet been loaded. */
    _getBindings(): Record<string, Binding>;
    /** Get the timestamp of the latest updated bound GPU memory resource (buffer/texture). */
    _getBindingsUpdateTimestamp(): number;
    /**
     * Updates the optional geometry attributes
     * Geometry, sets several attributes, indexBuffer, and also vertex count
     * @note Can trigger a pipeline rebuild / pipeline cache fetch on WebGPU
     */
    _setGeometryAttributes(gpuGeometry: GPUGeometry): void;
    /** Mark pipeline as needing update */
    _setPipelineNeedsUpdate(reason: string): void;
    /** Update pipeline if needed */
    _updatePipeline(): RenderPipeline;
    /** Throttle draw call logging */
    _lastLogTime: number;
    _logOpen: boolean;
    _logDrawCallStart(): void;
    _logDrawCallEnd(): void;
    protected _drawCount: number;
    _logFramebuffer(renderPass: RenderPass): void;
    _getAttributeDebugTable(): Record<string, Record<string, unknown>>;
    _getBufferOrConstantValues(attribute: Buffer | TypedArray, dataType: any): string;
}
/** Create a shadertools platform info from the Device */
export declare function getPlatformInfo(device: Device): PlatformInfo;
//# sourceMappingURL=model.d.ts.map