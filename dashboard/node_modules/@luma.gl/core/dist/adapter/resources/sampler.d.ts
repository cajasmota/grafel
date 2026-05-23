import type { Device } from "../device.js";
import { CompareFunction } from "../types/parameters.js";
import { Resource, ResourceProps } from "./resource.js";
/** Edge values sampling mode */
export type SamplerAddressMode = 'clamp-to-edge' | 'repeat' | 'mirror-repeat';
/** Sampler filtering mode */
export type SamplerFilterMode = 'nearest' | 'linear';
/**
 * Properties for initializing a sampler
 */
export type SamplerProps = ResourceProps & {
    /** Comparison / shadow samplers are used with depth textures. See the `Sampler.compare` field */
    type?: 'color-sampler' | 'comparison-sampler';
    /** Edge value sampling in X direction */
    addressModeU?: 'clamp-to-edge' | 'repeat' | 'mirror-repeat';
    /** Edge value sampling in Y direction */
    addressModeV?: 'clamp-to-edge' | 'repeat' | 'mirror-repeat';
    /** Edge value sampling in Z direction */
    addressModeW?: 'clamp-to-edge' | 'repeat' | 'mirror-repeat';
    /** Magnification: the area of the fragment in texture space is smaller than a texel */
    magFilter?: 'nearest' | 'linear';
    /** Minification: the area of the fragment in texture space is larger than a texel */
    minFilter?: 'nearest' | 'linear';
    /** mipmapping: select between multiple mipmaps based on angle and size of the texture relative to the screen. */
    mipmapFilter?: 'none' | 'nearest' | 'linear';
    /** Affects the mipmap image selection */
    lodMinClamp?: number;
    /** Affects the mipmap image selection */
    lodMaxClamp?: number;
    /** Maximum number of samples that can be taken of the texture during any one texture fetch */
    maxAnisotropy?: number;
    /** How to compare reference values provided in shader shadow sampler calls with those pulled from the texture */
    compare?: CompareFunction;
};
export type SamplerParameters = Omit<SamplerProps, keyof ResourceProps>;
/** Immutable Sampler object */
export declare abstract class Sampler extends Resource<SamplerProps> {
    static defaultProps: Required<SamplerProps>;
    get [Symbol.toStringTag](): string;
    constructor(device: Device, props: SamplerProps);
    protected static normalizeProps(device: Device, props: SamplerProps): SamplerProps;
}
//# sourceMappingURL=sampler.d.ts.map