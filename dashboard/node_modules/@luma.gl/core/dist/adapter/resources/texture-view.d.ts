import type { Device } from "../device.js";
import type { Texture } from "./texture.js";
import type { TextureFormat } from "../../shadertypes/textures/texture-formats.js";
import { Resource, ResourceProps } from "./resource.js";
/** Properties for initializing a texture view */
export type TextureViewProps = ResourceProps & {
    /** The format of the texture view. Must be either the format of the texture or one of the viewFormats specified during its creation. */
    format?: TextureFormat;
    /** The dimension to view the texture as. */
    dimension?: '1d' | '2d' | '2d-array' | 'cube' | 'cube-array' | '3d';
    /** Which aspect(s) of the texture are accessible to the texture view. default "all"*/
    aspect?: 'all' | 'stencil-only' | 'depth-only';
    /** The first (most detailed) mipmap level accessible to the texture view.  default 0*/
    baseMipLevel?: number;
    /** How many mipmap levels, starting with baseMipLevel, are accessible to the texture view. */
    mipLevelCount: number;
    /** The index of the first array layer accessible to the texture view. default 0 */
    baseArrayLayer?: number;
    /** How many array layers, starting with baseArrayLayer, are accessible to the texture view. */
    arrayLayerCount: number;
};
/** Immutable TextureView object */
export declare abstract class TextureView extends Resource<TextureViewProps> {
    abstract texture: Texture;
    get [Symbol.toStringTag](): string;
    /** Should not be constructed directly. Use `texture.createView(props)` */
    constructor(device: Device, props: TextureViewProps & {
        texture: Texture;
    });
    static defaultProps: Required<TextureViewProps>;
}
//# sourceMappingURL=texture-view.d.ts.map