import type { Device } from "../device.js";
import { PrimitiveTopology } from "../types/parameters.js";
import { ShaderLayout } from "../types/shader-layout.js";
import type { Buffer } from "./buffer.js";
import { Resource, ResourceProps } from "./resource.js";
/** For bindRange */
export type BufferRange = {
    buffer: Buffer;
    byteOffset?: number;
    byteLength?: number;
};
/** Configures a set of output buffers for pipeline (WebGL only) */
export type TransformFeedbackProps = ResourceProps & {
    /** Layout of shader (for varyings) */
    layout: ShaderLayout;
    /** Buffer bindings (for varyings) */
    buffers: Record<string, Buffer | BufferRange>;
};
/** Holds a set of output buffers for pipeline (WebGL only) */
export declare abstract class TransformFeedback extends Resource<TransformFeedbackProps> {
    static defaultProps: Required<TransformFeedbackProps>;
    get [Symbol.toStringTag](): string;
    constructor(device: Device, props: TransformFeedbackProps);
    abstract begin(topology?: PrimitiveTopology): void;
    abstract end(): void;
    abstract setBuffers(buffers: Record<string, Buffer | BufferRange>): void;
    abstract setBuffer(locationOrName: string | number, bufferOrRange: Buffer | BufferRange): void;
    abstract getBuffer(locationOrName: string | number): Buffer | BufferRange | null;
}
//# sourceMappingURL=transform-feedback.d.ts.map