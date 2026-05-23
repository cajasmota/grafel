import type { TypedArray } from "../../types.js";
import { AttributeInfo } from "../../adapter-utils/get-attribute-from-layouts.js";
import type { Device } from "../device.js";
import type { Buffer } from "./buffer.js";
import type { RenderPass } from "./render-pass.js";
import { Resource, ResourceProps } from "./resource.js";
import { ShaderLayout } from "../types/shader-layout.js";
import { BufferLayout } from "../types/buffer-layout.js";
/** Properties for initializing a VertexArray */
export type VertexArrayProps = ResourceProps & {
    shaderLayout: ShaderLayout;
    bufferLayout: BufferLayout[];
};
/**
 * Stores attribute bindings.
 * Makes it easy to share a render pipeline and use separate vertex arrays.
 * @note On WebGL, VertexArray allows non-constant bindings to be performed in advance
 * reducing the number of WebGL calls per draw call.
 * @note On WebGPU this is just a convenience class that collects the bindings.
 */
export declare abstract class VertexArray extends Resource<VertexArrayProps> {
    static defaultProps: Required<VertexArrayProps>;
    get [Symbol.toStringTag](): string;
    /** Max number of vertex attributes */
    readonly maxVertexAttributes: number;
    /** Attribute infos indexed by location - TODO only needed by webgl module? */
    protected readonly attributeInfos: AttributeInfo[];
    /** Index buffer */
    indexBuffer: Buffer | null;
    /** Attributes indexed by buffer slot */
    attributes: (Buffer | TypedArray | null)[];
    constructor(device: Device, props: VertexArrayProps);
    /** Set attributes (stored on pipeline and set before each call) */
    abstract setIndexBuffer(indices: Buffer | null): void;
    /** Set attributes (stored on pipeline and set before each call) */
    abstract setBuffer(bufferSlot: number, buffer: Buffer | null): void;
    abstract bindBeforeRender(renderPass: RenderPass): void;
    abstract unbindAfterRender(renderPass: RenderPass): void;
    /** @deprecated Set constant attributes (WebGL only) */
    setConstantWebGL(location: number, value: TypedArray | null): void;
}
//# sourceMappingURL=vertex-array.d.ts.map