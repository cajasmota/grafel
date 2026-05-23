import { TypedArray } from '@math.gl/types';
import type { Device } from "../device.js";
import type { TextureFormat } from "../../shadertypes/textures/texture-formats.js";
import type { TextureView, TextureViewProps } from "./texture-view.js";
import { Resource, ResourceProps } from "./resource.js";
import { Sampler, SamplerProps } from "./sampler.js";
import { ExternalImage } from "../../image-utils/image-types.js";
/** Options for Texture.copyExternalImage */
export type CopyExternalImageOptions = {
    /** Image */
    image: ExternalImage;
    /** Copy from image x offset (default 0) */
    sourceX?: number;
    /** Copy from image y offset (default 0) */
    sourceY?: number;
    /** Copy area width (default 1) */
    width?: number;
    /** Copy area height (default 1) */
    height?: number;
    /** Copy depth, number of layers/depth slices(default 1) */
    depth?: number;
    /** Start copying into offset x (default 0) */
    x?: number;
    /** Start copying into offset y (default 0) */
    y?: number;
    /** Start copying into layer / depth slice z (default 0) */
    z?: number;
    /** Which mip-level to copy into (default 0) */
    mipLevel?: number;
    /** When copying into depth stencil textures (default 'all') */
    aspect?: 'all' | 'stencil-only' | 'depth-only';
    /** Specific color space of image data */
    colorSpace?: 'srgb';
    /** load as premultiplied alpha  */
    premultipliedAlpha?: boolean;
    /** Whether to flip the image vertically */
    flipY?: boolean;
};
/** Options for copyImageData */
export type CopyImageDataOptions = {
    /** Data to copy (array of bytes) */
    data: ArrayBuffer | SharedArrayBuffer | ArrayBufferView;
    /** Offset into the data (in addition to any offset built-in to the ArrayBufferView) */
    byteOffset?: number;
    /** The stride, in bytes, between the beginning of each texel block row and the subsequent texel block row. Required if there are multiple texel block rows (i.e. the copy height or depth is more than one block). */
    bytesPerRow?: number;
    /** Number or rows per image (needed if multiple images are being set) */
    rowsPerImage?: number;
    /** Start copying into offset x (default 0) */
    x?: number;
    /** Start copying into offset y (default 0) */
    y?: number;
    /** Start copying from depth layer z (default 0) */
    z?: number;
    /** Which mip-level to copy into (default 0) */
    mipLevel?: number;
    /** When copying into depth stencil textures (default 'all') */
    aspect?: 'all' | 'stencil-only' | 'depth-only';
};
/** Texture properties */
export type TextureProps = ResourceProps & {
    /** @deprecated Use AsyncTexture to create textures with data. */
    data?: ExternalImage | TypedArray | null;
    /** Dimension of this texture. Defaults to '2d' */
    dimension?: '1d' | '2d' | '2d-array' | 'cube' | 'cube-array' | '3d';
    /** The format (bit layout) of the textures pixel data */
    format?: TextureFormat;
    /** Width in texels */
    width: number;
    /** Width in texels */
    height: number;
    /** Number of depth layers */
    depth?: number;
    /** How this texture will be used. Defaults to TEXTURE | COPY_DST | RENDER_ATTACHMENT */
    usage?: number;
    /** How many mip levels */
    mipLevels?: number;
    /** Multi sampling */
    samples?: number;
    /** Sampler (or SamplerProps) for the default sampler for this texture. Used if no sampler provided. Note that other samplers can still be used. */
    sampler?: Sampler | SamplerProps;
    /** Props for the default TextureView for this texture. Note that other views can still be created and used. */
    view?: TextureViewProps;
};
/**
 * Abstract Texture interface
 * Texture Object
 * https://gpuweb.github.io/gpuweb/#gputexture
 */
export declare abstract class Texture extends Resource<TextureProps> {
    /** The texture can be bound for use as a sampled texture in a shader */
    static SAMPLE: number;
    /** The texture can be bound for use as a storage texture in a shader */
    static STORAGE: number;
    /** The texture can be used as a color or depth/stencil attachment in a render pass */
    static RENDER: number;
    /** The texture can be used as the source of a copy operation */
    static COPY_SRC: number;
    /** he texture can be used as the destination of a copy or write operation */
    static COPY_DST: number;
    /** @deprecated Use Texture.SAMPLE */
    static TEXTURE: number;
    /** @deprecated Use Texture.RENDER */
    static RENDER_ATTACHMENT: number;
    /** dimension of this texture */
    readonly dimension: '1d' | '2d' | '2d-array' | 'cube' | 'cube-array' | '3d';
    /** base dimension of this texture */
    readonly baseDimension: '1d' | '2d' | '3d';
    /** format of this texture */
    readonly format: TextureFormat;
    /** width in pixels of this texture */
    width: number;
    /** height in pixels of this texture */
    height: number;
    /** depth of this texture */
    depth: number;
    /** mip levels in this texture */
    mipLevels: number;
    /** Default sampler for this texture */
    abstract sampler: Sampler;
    /** Default view for this texture */
    abstract view: TextureView;
    /** "Time" of last update. Monotonically increasing timestamp. TODO move to AsyncTexture? */
    updateTimestamp: number;
    get [Symbol.toStringTag](): string;
    toString(): string;
    /** Do not use directly. Create with device.createTexture() */
    constructor(device: Device, props: TextureProps);
    /** Set sampler props associated with this texture */
    setSampler(sampler: Sampler | SamplerProps): void;
    /** Create a texture view for this texture */
    abstract createView(props: TextureViewProps): TextureView;
    /** Copy an image (e.g an ImageBitmap) into the texture */
    abstract copyExternalImage(options: CopyExternalImageOptions): {
        width: number;
        height: number;
    };
    /** Copy raw image data (bytes) into the texture */
    abstract copyImageData(options: CopyImageDataOptions): void;
    /** Generate mipmaps (WebGL only) */
    abstract generateMipmapsWebGL(): void;
    /**
     * Create a new texture with the same parameters and optionally a different size
     * @note Textures are immutable and cannot be resized after creation, but we can create a similar texture with the same parameters but a new size.
     * @note Does not copy contents of the texture
     */
    clone(size?: {
        width: number;
        height: number;
    }): Texture;
    /** Ensure we have integer coordinates */
    protected static normalizeProps(device: Device, props: TextureProps): TextureProps;
    /** Initialize texture with supplied props */
    _initializeData(data: TextureProps['data']): void;
    _normalizeCopyImageDataOptions(options_: CopyImageDataOptions): Required<CopyImageDataOptions>;
    _normalizeCopyExternalImageOptions(options_: CopyExternalImageOptions): Required<CopyExternalImageOptions>;
    /** Default options */
    static defaultProps: Required<TextureProps>;
    protected static defaultCopyDataOptions: Required<CopyImageDataOptions>;
    /** Default options */
    protected static defaultCopyExternalImageOptions: Required<CopyExternalImageOptions>;
}
//# sourceMappingURL=texture.d.ts.map