import type { TextureProps, SamplerProps, TextureView, Device, TypedArray, TextureFormat, ExternalImage } from '@luma.gl/core';
import { Texture, Sampler } from '@luma.gl/core';
type AsyncTextureDataProps = AsyncTexture1DProps | AsyncTexture2DProps | AsyncTexture3DProps | AsyncTextureArrayProps | AsyncTextureCubeProps | AsyncTextureCubeArrayProps;
type AsyncTexture1DProps = {
    dimension: '1d';
    data: Promise<Texture1DData> | Texture1DData | null;
};
type AsyncTexture2DProps = {
    dimension?: '2d';
    data: Promise<Texture2DData> | Texture2DData | null;
};
type AsyncTexture3DProps = {
    dimension: '3d';
    data: Promise<Texture3DData> | Texture3DData | null;
};
type AsyncTextureArrayProps = {
    dimension: '2d-array';
    data: Promise<TextureArrayData> | TextureArrayData | null;
};
type AsyncTextureCubeProps = {
    dimension: 'cube';
    data: Promise<TextureCubeData> | TextureCubeData | null;
};
type AsyncTextureCubeArrayProps = {
    dimension: 'cube-array';
    data: Promise<TextureCubeArrayData> | TextureCubeArrayData | null;
};
/** Names of cube texture faces */
export type TextureCubeFace = '+X' | '-X' | '+Y' | '-Y' | '+Z' | '-Z';
export declare const TextureCubeFaces: TextureCubeFace[];
export declare const TextureCubeFaceMap: {
    '+X': number;
    '-X': number;
    '+Y': number;
    '-Y': number;
    '+Z': number;
    '-Z': number;
};
/**
 * One mip level
 * Basic data structure is similar to `ImageData`
 * additional optional fields can describe compressed texture data.
 */
export type TextureImageData = {
    /** WebGPU style format string. Defaults to 'rgba8unorm' */
    format?: TextureFormat;
    data: TypedArray;
    width: number;
    height: number;
    compressed?: boolean;
    byteLength?: number;
    hasAlpha?: boolean;
};
export type TextureLevelSource = TextureImageData | ExternalImage;
/** Texture data can be one or more mip levels */
export type TextureData = TextureImageData | ExternalImage | (TextureImageData | ExternalImage)[];
/** @todo - define what data type is supported for 1D textures */
export type Texture1DData = TypedArray | TextureImageData;
/** Texture data can be one or more mip levels */
export type Texture2DData = TypedArray | TextureImageData | ExternalImage | (TextureImageData | ExternalImage)[];
/** 6 face textures */
export type TextureCubeData = Record<TextureCubeFace, TextureData>;
/** Array of textures */
export type Texture3DData = TextureData[];
/** Array of textures */
export type TextureArrayData = TextureData[];
/** Array of 6 face textures */
export type TextureCubeArrayData = Record<TextureCubeFace, TextureData>[];
export declare const CubeFaces: TextureCubeFace[];
/** Properties for an async texture */
export type AsyncTextureProps = Omit<TextureProps, 'data' | 'mipLevels' | 'width' | 'height'> & AsyncTextureDataProps & {
    /** Generate mipmaps after creating textures and setting data */
    mipmaps?: boolean;
    /** nipLevels can be set to 'auto' to generate max number of mipLevels */
    mipLevels?: number | 'auto';
    /** Width - can be auto-calculated when initializing from ExternalImage */
    width?: number;
    /** Height - can be auto-calculated when initializing from ExternalImage */
    height?: number;
};
/**
 * It is very convenient to be able to initialize textures with promises
 * This can add considerable complexity to the Texture class, and doesn't
 * fit with the immutable nature of WebGPU resources.
 * Instead, luma.gl offers async textures as a separate class.
 */
export declare class AsyncTexture {
    readonly device: Device;
    readonly id: string;
    props: Required<Omit<AsyncTextureProps, 'data'>>;
    texture: Texture;
    sampler: Sampler;
    view: TextureView;
    readonly ready: Promise<void>;
    isReady: boolean;
    destroyed: boolean;
    protected resolveReady: () => void;
    protected rejectReady: (error: Error) => void;
    get [Symbol.toStringTag](): string;
    toString(): string;
    constructor(device: Device, props: AsyncTextureProps);
    initAsync(props: AsyncTextureProps): Promise<void>;
    destroy(): void;
    generateMipmaps(): void;
    /** Set sampler or create and set new Sampler from SamplerProps */
    setSampler(sampler?: Sampler | SamplerProps): void;
    /**
     * Textures are immutable and cannot be resized after creation,
     * but we can create a similar texture with the same parameters but a new size.
     * @note Does not copy contents of the texture
     * @note Mipmaps may need to be regenerated after resizing / setting new data
     * @todo Abort pending promise and create a texture with the new size?
     */
    resize(size: {
        width: number;
        height: number;
    }): boolean;
    /** Check if texture data is a typed array */
    isTextureLevelData(data: TextureData): data is TextureImageData;
    /** Get the size of the texture described by the provided TextureData */
    getTextureDataSize(data: TextureData | TextureCubeData | TextureArrayData | TextureCubeArrayData | TypedArray | null): {
        width: number;
        height: number;
    } | null;
    /** Convert luma.gl cubemap face constants to depth index */
    getCubeFaceDepth(face: TextureCubeFace): number;
    setTextureData(data: TextureData): void;
    /** Experimental: Set multiple mip levels */
    _setTexture1DData(texture: Texture, data: Texture1DData): void;
    /** Experimental: Set multiple mip levels */
    _setTexture2DData(lodData: Texture2DData, depth?: number): void;
    /**
     * Experimental: Sets 3D texture data: multiple depth slices, multiple mip levels
     * @param data
     */
    _setTexture3DData(texture: Texture, data: Texture3DData): void;
    /**
     * Experimental: Set Cube texture data, multiple faces, multiple mip levels
     * @todo - could support TextureCubeArray with depth
     * @param data
     * @param index
     */
    _setTextureCubeData(texture: Texture, data: TextureCubeData): void;
    /**
     * Experimental: Sets texture array data, multiple levels, multiple depth slices
     * @param data
     */
    _setTextureArrayData(texture: Texture, data: TextureArrayData): void;
    /**
     * Experimental: Sets texture cube array, multiple faces, multiple levels, multiple mip levels
     * @param data
     */
    _setTextureCubeArrayData(texture: Texture, data: TextureCubeArrayData): void;
    /** Experimental */
    _setTextureCubeFaceData(texture: Texture, lodData: Texture2DData, face: TextureCubeFace, depth?: number): void;
    /**
     * Normalize TextureData to an array of TextureImageData / ExternalImages
     * @param data
     * @param options
     * @returns array of TextureImageData / ExternalImages
     */
    _normalizeTextureData(data: Texture2DData): (TextureImageData | ExternalImage)[];
    static defaultProps: Required<AsyncTextureProps>;
}
export {};
//# sourceMappingURL=async-texture.d.ts.map