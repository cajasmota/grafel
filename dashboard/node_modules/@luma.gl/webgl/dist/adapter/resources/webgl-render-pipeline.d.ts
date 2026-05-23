import type { RenderPipelineProps, RenderPipelineParameters, PrimitiveTopology, ShaderLayout, UniformValue, Binding, RenderPass, VertexArray } from '@luma.gl/core';
import { RenderPipeline } from '@luma.gl/core';
import { WebGLDevice } from "../webgl-device.js";
import { WEBGLShader } from "./webgl-shader.js";
import { WEBGLTransformFeedback } from "./webgl-transform-feedback.js";
/** Creates a new render pipeline */
export declare class WEBGLRenderPipeline extends RenderPipeline {
    /** The WebGL device that created this render pipeline */
    readonly device: WebGLDevice;
    /** Handle to underlying WebGL program */
    readonly handle: WebGLProgram;
    /** vertex shader */
    vs: WEBGLShader;
    /** fragment shader */
    fs: WEBGLShader;
    /** The layout extracted from shader by WebGL introspection APIs */
    introspectedLayout: ShaderLayout;
    /** Uniforms set on this model */
    uniforms: Record<string, UniformValue>;
    /** Bindings set on this model */
    bindings: Record<string, Binding>;
    /** WebGL varyings */
    varyings: string[] | null;
    _uniformCount: number;
    _uniformSetters: Record<string, Function>;
    get [Symbol.toStringTag](): string;
    constructor(device: WebGLDevice, props: RenderPipelineProps);
    destroy(): void;
    /**
     * Bindings include: textures, samplers and uniform buffers
     * @todo needed for portable model
     */
    setBindings(bindings: Record<string, Binding>, options?: {
        disableWarnings?: boolean;
    }): void;
    /** @todo needed for portable model
     * @note The WebGL API is offers many ways to draw things
     * This function unifies those ways into a single call using common parameters with sane defaults
     */
    draw(options: {
        renderPass: RenderPass;
        parameters?: RenderPipelineParameters;
        topology?: PrimitiveTopology;
        vertexArray: VertexArray;
        isInstanced?: boolean;
        vertexCount?: number;
        indexCount?: number;
        instanceCount?: number;
        firstVertex?: number;
        firstIndex?: number;
        firstInstance?: number;
        baseVertex?: number;
        transformFeedback?: WEBGLTransformFeedback;
    }): boolean;
    protected _linkShaders(): Promise<void>;
    /** Report link status. First, check for shader compilation failures if linking fails */
    _reportLinkStatus(status: 'success' | 'link-error' | 'validation-error'): Promise<void>;
    /**
     * Get the shader compilation status
     * TODO - Load log even when no error reported, to catch warnings?
     * https://gamedev.stackexchange.com/questions/30429/how-to-detect-glsl-warnings
     */
    _getLinkStatus(): 'success' | 'link-error' | 'validation-error';
    /** Use KHR_parallel_shader_compile extension if available */
    _waitForLinkComplete(): Promise<void>;
    /**
     * Checks if all texture-values uniforms are renderable (i.e. loaded)
     * Update a texture if needed (e.g. from video)
     * Note: This is currently done before every draw call
     */
    _areTexturesRenderable(): boolean;
    /** Apply any bindings (before each draw call) */
    _applyBindings(): void;
    /**
     * Due to program sharing, uniforms need to be reset before every draw call
     * (though caching will avoid redundant WebGL calls)
     */
    _applyUniforms(): void;
}
//# sourceMappingURL=webgl-render-pipeline.d.ts.map