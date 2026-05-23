// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { Resource } from "./resource.js";
import { Sampler } from "./sampler.js";
import { log } from "../../utils/log.js";
const BASE_DIMENSIONS = {
    '1d': '1d',
    '2d': '2d',
    '2d-array': '2d',
    cube: '2d',
    'cube-array': '2d',
    '3d': '3d'
};
/**
 * Abstract Texture interface
 * Texture Object
 * https://gpuweb.github.io/gpuweb/#gputexture
 */
export class Texture extends Resource {
    /** The texture can be bound for use as a sampled texture in a shader */
    static SAMPLE = 0x04;
    /** The texture can be bound for use as a storage texture in a shader */
    static STORAGE = 0x08;
    /** The texture can be used as a color or depth/stencil attachment in a render pass */
    static RENDER = 0x10;
    /** The texture can be used as the source of a copy operation */
    static COPY_SRC = 0x01;
    /** he texture can be used as the destination of a copy or write operation */
    static COPY_DST = 0x02;
    /** @deprecated Use Texture.SAMPLE */
    static TEXTURE = 0x04;
    /** @deprecated Use Texture.RENDER */
    static RENDER_ATTACHMENT = 0x10;
    /** dimension of this texture */
    dimension;
    /** base dimension of this texture */
    baseDimension;
    /** format of this texture */
    format;
    /** width in pixels of this texture */
    width;
    /** height in pixels of this texture */
    height;
    /** depth of this texture */
    depth;
    /** mip levels in this texture */
    mipLevels;
    /** "Time" of last update. Monotonically increasing timestamp. TODO move to AsyncTexture? */
    updateTimestamp;
    get [Symbol.toStringTag]() {
        return 'Texture';
    }
    toString() {
        return `Texture(${this.id},${this.format},${this.width}x${this.height})`;
    }
    /** Do not use directly. Create with device.createTexture() */
    constructor(device, props) {
        props = Texture.normalizeProps(device, props);
        super(device, props, Texture.defaultProps);
        this.dimension = this.props.dimension;
        this.baseDimension = BASE_DIMENSIONS[this.dimension];
        this.format = this.props.format;
        // Size
        this.width = this.props.width;
        this.height = this.props.height;
        this.depth = this.props.depth;
        this.mipLevels = this.props.mipLevels;
        // Calculate size, if not provided
        if (this.props.width === undefined || this.props.height === undefined) {
            if (device.isExternalImage(props.data)) {
                const size = device.getExternalImageSize(props.data);
                this.width = size?.width || 1;
                this.height = size?.height || 1;
            }
            else {
                this.width = 1;
                this.height = 1;
                if (this.props.width === undefined || this.props.height === undefined) {
                    log.warn(`${this} created with undefined width or height. This is deprecated. Use AsyncTexture instead.`)();
                }
            }
        }
        // TODO - perhaps this should be set on async write completion?
        this.updateTimestamp = device.incrementTimestamp();
    }
    /** Set sampler props associated with this texture */
    setSampler(sampler) {
        this.sampler = sampler instanceof Sampler ? sampler : this.device.createSampler(sampler);
    }
    /**
     * Create a new texture with the same parameters and optionally a different size
     * @note Textures are immutable and cannot be resized after creation, but we can create a similar texture with the same parameters but a new size.
     * @note Does not copy contents of the texture
     */
    clone(size) {
        return this.device.createTexture({ ...this.props, ...size });
    }
    /** Ensure we have integer coordinates */
    static normalizeProps(device, props) {
        const newProps = { ...props };
        // Ensure we have integer coordinates
        const { width, height } = newProps;
        if (typeof width === 'number') {
            newProps.width = Math.max(1, Math.ceil(width));
        }
        if (typeof height === 'number') {
            newProps.height = Math.max(1, Math.ceil(height));
        }
        return newProps;
    }
    // HELPERS
    /** Initialize texture with supplied props */
    // eslint-disable-next-line max-statements
    _initializeData(data) {
        // Store opts for accessors
        if (this.device.isExternalImage(data)) {
            this.copyExternalImage({
                image: data,
                width: this.width,
                height: this.height,
                depth: this.depth,
                mipLevel: 0,
                x: 0,
                y: 0,
                z: 0,
                aspect: 'all',
                colorSpace: 'srgb',
                premultipliedAlpha: false,
                flipY: false
            });
        }
        else if (data) {
            this.copyImageData({
                data,
                // width: this.width,
                // height: this.height,
                // depth: this.depth,
                mipLevel: 0,
                x: 0,
                y: 0,
                z: 0,
                aspect: 'all'
            });
        }
    }
    _normalizeCopyImageDataOptions(options_) {
        const { width, height, depth } = this;
        const options = { ...Texture.defaultCopyDataOptions, width, height, depth, ...options_ };
        const info = this.device.getTextureFormatInfo(this.format);
        if (!options_.bytesPerRow && !info.bytesPerPixel) {
            throw new Error(`bytesPerRow must be provided for texture format ${this.format}`);
        }
        options.bytesPerRow = options_.bytesPerRow || width * (info.bytesPerPixel || 4);
        options.rowsPerImage = options_.rowsPerImage || height;
        // WebGL will error if we try to copy outside the bounds of the texture
        // options.width = Math.min(options.width, this.width - options.x);
        // options.height = Math.min(options.height, this.height - options.y);
        return options;
    }
    _normalizeCopyExternalImageOptions(options_) {
        const size = this.device.getExternalImageSize(options_.image);
        const options = { ...Texture.defaultCopyExternalImageOptions, ...size, ...options_ };
        // WebGL will error if we try to copy outside the bounds of the texture
        options.width = Math.min(options.width, this.width - options.x);
        options.height = Math.min(options.height, this.height - options.y);
        return options;
    }
    /** Default options */
    static defaultProps = {
        ...Resource.defaultProps,
        data: null,
        dimension: '2d',
        format: 'rgba8unorm',
        usage: Texture.TEXTURE | Texture.RENDER_ATTACHMENT | Texture.COPY_DST,
        width: undefined,
        height: undefined,
        depth: 1,
        mipLevels: 1,
        samples: undefined,
        sampler: {},
        view: undefined
    };
    static defaultCopyDataOptions = {
        data: undefined,
        byteOffset: 0,
        bytesPerRow: undefined,
        rowsPerImage: undefined,
        mipLevel: 0,
        x: 0,
        y: 0,
        z: 0,
        aspect: 'all'
    };
    /** Default options */
    static defaultCopyExternalImageOptions = {
        image: undefined,
        sourceX: 0,
        sourceY: 0,
        width: undefined,
        height: undefined,
        depth: 1,
        mipLevel: 0,
        x: 0,
        y: 0,
        z: 0,
        aspect: 'all',
        colorSpace: 'srgb',
        premultipliedAlpha: false,
        flipY: false
    };
}
//# sourceMappingURL=texture.js.map