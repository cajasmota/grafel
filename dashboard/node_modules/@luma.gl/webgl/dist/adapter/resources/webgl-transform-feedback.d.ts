import type { PrimitiveTopology, ShaderLayout, TransformFeedbackProps } from '@luma.gl/core';
import { TransformFeedback, Buffer, BufferRange } from '@luma.gl/core';
import { WebGLDevice } from "../webgl-device.js";
export declare class WEBGLTransformFeedback extends TransformFeedback {
    readonly device: WebGLDevice;
    readonly gl: WebGL2RenderingContext;
    readonly handle: WebGLTransformFeedback;
    /**
     * NOTE: The Model already has this information while drawing, but
     * TransformFeedback currently needs it internally, to look up
     * varying information outside of a draw() call.
     */
    readonly layout: ShaderLayout;
    buffers: Record<string, BufferRange>;
    unusedBuffers: Record<string, Buffer>;
    /**
     * Allows us to avoid a Chrome bug where a buffer that is already bound to a
     * different target cannot be bound to 'TRANSFORM_FEEDBACK_BUFFER' target.
     * This a major workaround, see: https://github.com/KhronosGroup/WebGL/issues/2346
     */
    bindOnUse: boolean;
    private _bound;
    constructor(device: WebGLDevice, props: TransformFeedbackProps);
    destroy(): void;
    begin(topology?: PrimitiveTopology): void;
    end(): void;
    setBuffers(buffers: Record<string, Buffer | BufferRange>): void;
    setBuffer(locationOrName: string | number, bufferOrRange: Buffer | BufferRange): void;
    getBuffer(locationOrName: string | number): Buffer | BufferRange | null;
    bind(funcOrHandle?: (() => void) | WebGLTransformFeedback | null): unknown;
    unbind(): void;
    /** Extract offsets for bindBufferRange */
    protected _getBufferRange(bufferOrRange: Buffer | {
        buffer: Buffer;
        byteOffset?: number;
        byteLength?: number;
    }): Required<BufferRange>;
    protected _getVaryingIndex(locationOrName: string | number): number;
    /**
     * Need to avoid chrome bug where buffer that is already bound to a different target
     * cannot be bound to 'TRANSFORM_FEEDBACK_BUFFER' target.
     */
    protected _bindBuffers(): void;
    protected _unbindBuffers(): void;
    protected _bindBuffer(index: number, buffer: Buffer, byteOffset?: number, byteLength?: number): void;
}
//# sourceMappingURL=webgl-transform-feedback.d.ts.map