import { Device, Buffer, BufferRange, TransformFeedback, RenderPassProps } from '@luma.gl/core';
import { Model } from "../model/model.js";
import type { ModelProps } from "../model/model.js";
/**
 * Properties for creating a {@link BufferTransform}
 * @note Only works under WebGL2.
 */
export type BufferTransformProps = Omit<ModelProps, 'fs'> & {
    /** Optional fragment shader - normally not used in transforms */
    fs?: ModelProps['fs'];
    /** A list of named outputs corresponding to shader declarations (varyings in WebGL) */
    outputs?: string[];
    /** @deprecated Use run({outputBuffers}) instead - Map of output buffers that the shaders will write results of computations to */
    feedbackBuffers?: Record<string, Buffer | BufferRange>;
};
/**
 * Manages a WebGL program (pipeline) for buffer→buffer transforms.
 * @note Only works under WebGL2.
 */
export declare class BufferTransform {
    readonly device: Device;
    readonly model: Model;
    readonly transformFeedback: TransformFeedback;
    static defaultProps: Required<BufferTransformProps>;
    static isSupported(device: Device): boolean;
    constructor(device: Device, props?: BufferTransformProps);
    /** Destroy owned resources. */
    destroy(): void;
    /** @deprecated Use {@link destroy}. */
    delete(): void;
    /** Run one transform loop. */
    run(options?: RenderPassProps & {
        inputBuffers?: Record<string, Buffer>;
        outputBuffers?: Record<string, Buffer>;
    }): void;
    /** @deprecated App knows what buffers it is passing in - Returns the {@link Buffer} or {@link BufferRange} for given varying name. */
    getBuffer(varyingName: string): Buffer | BufferRange | null;
    /** @deprecated App knows what buffers it is passing in - Reads the {@link Buffer} or {@link BufferRange} for given varying name. */
    readAsync(varyingName: string): Promise<Uint8Array>;
}
//# sourceMappingURL=buffer-transform.d.ts.map