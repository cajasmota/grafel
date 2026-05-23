import type { TypedArray } from '@math.gl/types';
import type { DeviceProps, DeviceInfo, DeviceTextureFormatCapabilities, CanvasContextProps, Buffer, Texture, Framebuffer, VertexArray, VertexArrayProps, BufferProps, ShaderProps, SamplerProps, TextureProps, ExternalTexture, ExternalTextureProps, FramebufferProps, RenderPipelineProps, ComputePipeline, ComputePipelineProps, CommandEncoderProps, TransformFeedbackProps, QuerySetProps, Resource, VertexFormat } from '@luma.gl/core';
import { Device, CanvasContext } from '@luma.gl/core';
import type { GLExtensions } from '@luma.gl/constants';
import { WebGLDeviceFeatures } from "./device-helpers/webgl-device-features.js";
import { WebGLDeviceLimits } from "./device-helpers/webgl-device-limits.js";
import { WebGLCanvasContext } from "./webgl-canvas-context.js";
import type { Spector } from "../context/debug/spector-types.js";
import { WEBGLBuffer } from "./resources/webgl-buffer.js";
import { WEBGLShader } from "./resources/webgl-shader.js";
import { WEBGLSampler } from "./resources/webgl-sampler.js";
import { WEBGLTexture } from "./resources/webgl-texture.js";
import { WEBGLFramebuffer } from "./resources/webgl-framebuffer.js";
import { WEBGLRenderPipeline } from "./resources/webgl-render-pipeline.js";
import { WEBGLCommandEncoder } from "./resources/webgl-command-encoder.js";
import { WEBGLCommandBuffer } from "./resources/webgl-command-buffer.js";
import { WEBGLTransformFeedback } from "./resources/webgl-transform-feedback.js";
import { WEBGLQuerySet } from "./resources/webgl-query-set.js";
/** WebGPU style Device API for a WebGL context */
export declare class WebGLDevice extends Device {
    /** type of this device */
    readonly type = "webgl";
    /** The underlying WebGL context */
    readonly handle: WebGL2RenderingContext;
    features: WebGLDeviceFeatures;
    limits: WebGLDeviceLimits;
    readonly info: DeviceInfo;
    readonly canvasContext: WebGLCanvasContext;
    readonly preferredColorFormat = "rgba8unorm";
    readonly preferredDepthFormat = "depth24plus";
    commandEncoder: WEBGLCommandEncoder;
    readonly lost: Promise<{
        reason: 'destroyed';
        message: string;
    }>;
    private _resolveContextLost?;
    /** WebGL2 context. */
    readonly gl: WebGL2RenderingContext;
    /** Store constants */
    _constants: (TypedArray | null)[];
    /** State used by luma.gl classes - TODO - not used? */
    readonly _extensions: GLExtensions;
    _polyfilled: boolean;
    /** Instance of Spector.js (if initialized) */
    spectorJS: Spector | null;
    get [Symbol.toStringTag](): string;
    toString(): string;
    isVertexFormatSupported(format: VertexFormat): boolean;
    constructor(props: DeviceProps);
    /**
     * Destroys the device
     *
     * @note "Detaches" from the WebGL context unless _reuseDevices is true.
     *
     * @note The underlying WebGL context is not immediately destroyed,
     * but may be destroyed later through normal JavaScript garbage collection.
     * This is a fundamental limitation since WebGL does not offer any
     * browser API for destroying WebGL contexts.
     */
    destroy(): void;
    get isLost(): boolean;
    getTextureByteAlignment(): number;
    createCanvasContext(props?: CanvasContextProps): CanvasContext;
    createBuffer(props: BufferProps | ArrayBuffer | ArrayBufferView): WEBGLBuffer;
    createTexture(props: TextureProps): WEBGLTexture;
    createExternalTexture(props: ExternalTextureProps): ExternalTexture;
    createSampler(props: SamplerProps): WEBGLSampler;
    createShader(props: ShaderProps): WEBGLShader;
    createFramebuffer(props: FramebufferProps): WEBGLFramebuffer;
    createVertexArray(props: VertexArrayProps): VertexArray;
    createTransformFeedback(props: TransformFeedbackProps): WEBGLTransformFeedback;
    createQuerySet(props: QuerySetProps): WEBGLQuerySet;
    createRenderPipeline(props: RenderPipelineProps): WEBGLRenderPipeline;
    createComputePipeline(props?: ComputePipelineProps): ComputePipeline;
    createCommandEncoder(props?: CommandEncoderProps): WEBGLCommandEncoder;
    /**
     * Offscreen Canvas Support: Commit the frame
     * https://developer.mozilla.org/en-US/docs/Web/API/WebGL2RenderingContext/commit
     * Chrome's offscreen canvas does not require gl.commit
     */
    submit(commandBuffer: WEBGLCommandBuffer): void;
    /** @deprecated - should use command encoder */
    readPixelsToArrayWebGL(source: Framebuffer | Texture, options?: {
        sourceX?: number;
        sourceY?: number;
        sourceFormat?: number;
        sourceAttachment?: number;
        target?: Uint8Array | Uint16Array | Float32Array;
        sourceWidth?: number;
        sourceHeight?: number;
        sourceType?: number;
    }): Uint8Array | Uint16Array | Float32Array;
    /** @deprecated - should use command encoder */
    readPixelsToBufferWebGL(source: Framebuffer | Texture, options?: {
        sourceX?: number;
        sourceY?: number;
        sourceFormat?: number;
        target?: Buffer;
        targetByteOffset?: number;
        sourceWidth?: number;
        sourceHeight?: number;
        sourceType?: number;
    }): Buffer;
    setParametersWebGL(parameters: any): void;
    getParametersWebGL(parameters: any): any;
    withParametersWebGL(parameters: any, func: any): any;
    resetWebGL(): void;
    _getDeviceSpecificTextureFormatCapabilities(capabilities: DeviceTextureFormatCapabilities): DeviceTextureFormatCapabilities;
    /**
     * Triggers device (or WebGL context) loss.
     * @note primarily intended for testing how application reacts to device loss
     */
    loseDevice(): boolean;
    /** Save current WebGL context state onto an internal stack */
    pushState(): void;
    /** Restores previously saved context state */
    popState(): void;
    /**
     * Returns the GL.<KEY> constant that corresponds to a numeric value of a GL constant
     * Be aware that there are some duplicates especially for constants that are 0,
     * so this isn't guaranteed to return the right key in all cases.
     */
    getGLKey(value: unknown, options?: {
        emptyIfUnknown?: boolean;
    }): string;
    /**
     * Returns a map with any GL.<KEY> constants mapped to strings, both for keys and values
     */
    getGLKeys(glParameters: Record<number, unknown>): Record<string, string>;
    /**
     * Set a constant value for a location. Disabled attributes at that location will read from this value
     * @note WebGL constants are stored globally on the WebGL context, not the VertexArray
     * so they need to be updated before every render
     * @todo - remember/cache values to avoid setting them unnecessarily?
     */
    setConstantAttributeWebGL(location: number, constant: TypedArray): void;
    /** Ensure extensions are only requested once */
    getExtension(name: keyof GLExtensions): GLExtensions;
    /**
     * Storing data on a special field on WebGLObjects makes that data visible in SPECTOR chrome debug extension
     * luma.gl ids and props can be inspected
     */
    _setWebGLDebugMetadata(handle: unknown, resource: Resource<any>, options: {
        spector: Record<string, unknown>;
    }): void;
}
//# sourceMappingURL=webgl-device.d.ts.map