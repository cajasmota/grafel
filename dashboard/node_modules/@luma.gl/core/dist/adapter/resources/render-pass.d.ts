import type { NumberArray4, TypedArray } from '@math.gl/types';
import type { Device } from "../device.js";
import type { RenderPassParameters } from "../types/parameters.js";
import { Resource, ResourceProps } from "./resource.js";
import { Framebuffer } from "./framebuffer.js";
import { QuerySet } from "./query-set.js";
/**
 * Properties for a RenderPass instance is a required parameter to all draw calls.
 */
export type RenderPassProps = ResourceProps & {
    /** Framebuffer specifies which textures to render into. Default gets framebuffer from canvas context. */
    framebuffer?: Framebuffer | null;
    /** Control viewport, scissor rect, blend constant and stencil ref */
    parameters?: RenderPassParameters;
    /** Clear value for color attachment, or false to preserve the previous value */
    clearColor?: NumberArray4 | TypedArray | false;
    /** Experimental: Clear color values for multiple color attachments. Must specify typed arrays. props.clearColor will be ignored. */
    clearColors?: (TypedArray | false)[];
    /** Clear value for depth attachment (true === `1`), or false to preserve the previous value. Must be between 0.0 (near) and 1.0 (far), inclusive. */
    clearDepth?: number | false;
    /** Clear value for stencil attachment (true === `0`), or false to preserve the previous value. Converted to the type and number of LSBs as the number of bits in the stencil aspect */
    clearStencil?: number | false;
    /** Indicates that the depth component is read only. */
    depthReadOnly?: boolean;
    /** Indicates that the stencil component is read only. */
    stencilReadOnly?: boolean;
    /** Whether to disable / discard the output of the rasterizer */
    discard?: boolean;
    /** QuerySet to write begin/end timestamps to */
    occlusionQuerySet?: QuerySet;
    /** QuerySet to write begin/end timestamps to */
    timestampQuerySet?: QuerySet;
    /** QuerySet index to write begin timestamp to. No timestamp is written if not provided. */
    beginTimestampIndex?: number;
    /** QuerySet index to write end timestamp to. No timestamp is written if not provided. */
    endTimestampIndex?: number;
};
/**
 * A RenderPass instance is a required parameter to all draw calls.
 *
 * It holds a combination of
 * - render targets (specified via a framebuffer)
 * - clear colors, read/write, discard information for the framebuffer attachments
 * - a couple of mutable parameters ()
 */
export declare abstract class RenderPass extends Resource<RenderPassProps> {
    /** TODO - should be [0, 0, 0, 0], update once deck.gl tests run clean */
    static defaultClearColor: [number, number, number, number];
    /** Depth 1.0 represents the far plance */
    static defaultClearDepth: number;
    /** Clears all stencil bits */
    static defaultClearStencil: number;
    get [Symbol.toStringTag](): string;
    constructor(device: Device, props: RenderPassProps);
    /** Call when rendering is done in this pass. */
    abstract end(): void;
    /** A few parameters can be changed at any time (viewport, scissorRect, blendColor, stencilReference) */
    abstract setParameters(parameters: RenderPassParameters): void;
    /** Being an occlusion query. Value will be stored in the occlusionQuerySet at the index. Occlusion queries cannot be nested. */
    abstract beginOcclusionQuery(queryIndex: number): void;
    /** End an occlusion query. Stores result in the index specified in beginOcclusionQuery. */
    abstract endOcclusionQuery(): void;
    /** Begins a labeled debug group containing subsequent commands */
    abstract pushDebugGroup(groupLabel: string): void;
    /** Ends the labeled debug group most recently started by pushDebugGroup() */
    abstract popDebugGroup(): void;
    /** Marks a point in a stream of commands with a label */
    abstract insertDebugMarker(markerLabel: string): void;
    protected static normalizeProps(device: Device, props: RenderPassProps): RenderPassProps;
    /** Default properties for RenderPass */
    static defaultProps: Required<RenderPassProps>;
}
//# sourceMappingURL=render-pass.d.ts.map