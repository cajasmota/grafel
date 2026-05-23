// luma.gl, MIT license
// Copyright (c) vis.gl contributors
import { Texture, Sampler, log } from '@luma.gl/core';
import { loadImageBitmap } from "../application-utils/load-file.js";
import { uid } from "../utils/uid.js";
export const TextureCubeFaces = ['+X', '-X', '+Y', '-Y', '+Z', '-Z'];
// prettier-ignore
export const TextureCubeFaceMap = { '+X': 0, '-X': 1, '+Y': 2, '-Y': 3, '+Z': 4, '-Z': 5 };
export const CubeFaces = ['+X', '-X', '+Y', '-Y', '+Z', '-Z'];
/**
 * It is very convenient to be able to initialize textures with promises
 * This can add considerable complexity to the Texture class, and doesn't
 * fit with the immutable nature of WebGPU resources.
 * Instead, luma.gl offers async textures as a separate class.
 */
export class AsyncTexture {
    device;
    id;
    props;
    // TODO - should we type these as possibly `null`? It will make usage harder?
    // @ts-expect-error
    texture;
    // @ts-expect-error
    sampler;
    // @ts-expect-error
    view;
    ready;
    isReady = false;
    destroyed = false;
    resolveReady = () => { };
    rejectReady = () => { };
    get [Symbol.toStringTag]() {
        return 'AsyncTexture';
    }
    toString() {
        return `AsyncTexture:"${this.id}"(${this.isReady ? 'ready' : 'loading'})`;
    }
    constructor(device, props) {
        this.device = device;
        // TODO - if we support URL strings as data...
        const id = uid('async-texture'); // typeof props?.data === 'string' ? props.data.slice(-20) : uid('async-texture');
        this.props = { ...AsyncTexture.defaultProps, id, ...props };
        this.id = this.props.id;
        props = { ...props };
        // Signature: new AsyncTexture(device, {data: url})
        if (typeof props?.data === 'string' && props.dimension === '2d') {
            props.data = loadImageBitmap(props.data);
        }
        // If mipmaps are requested, we need to allocate space for them
        if (props.mipmaps) {
            props.mipLevels = 'auto';
        }
        this.ready = new Promise((resolve, reject) => {
            this.resolveReady = () => {
                this.isReady = true;
                resolve();
            };
            this.rejectReady = reject;
        });
        this.initAsync(props);
    }
    async initAsync(props) {
        const asyncData = props.data;
        // @ts-expect-error not clear how to convince TS that null will be returned
        const data = await awaitAllPromises(asyncData).then(undefined, this.rejectReady);
        // Check that we haven't been destroyed while waiting for texture data to load
        if (this.destroyed) {
            return;
        }
        // Now we can actually create the texture
        // Auto-deduce width and height if not supplied
        const size = this.props.width && this.props.height
            ? { width: this.props.width, height: this.props.height }
            : this.getTextureDataSize(data);
        if (!size) {
            throw new Error('Texture size could not be determined');
        }
        const syncProps = { ...size, ...props, data: undefined, mipLevels: 1 };
        // Auto-calculate the number of mip levels as a convenience
        // TODO - Should we clamp to 1-getMipLevelCount?
        const maxMips = this.device.getMipLevelCount(syncProps.width, syncProps.height);
        syncProps.mipLevels =
            this.props.mipLevels === 'auto' ? maxMips : Math.min(maxMips, this.props.mipLevels);
        this.texture = this.device.createTexture(syncProps);
        this.sampler = this.texture.sampler;
        this.view = this.texture.view;
        if (props.data) {
            switch (this.props.dimension) {
                case '1d':
                    this._setTexture1DData(this.texture, data);
                    break;
                case '2d':
                    this._setTexture2DData(data);
                    break;
                case '3d':
                    this._setTexture3DData(this.texture, data);
                    break;
                case '2d-array':
                    this._setTextureArrayData(this.texture, data);
                    break;
                case 'cube':
                    this._setTextureCubeData(this.texture, data);
                    break;
                case 'cube-array':
                    this._setTextureCubeArrayData(this.texture, data);
                    break;
            }
        }
        // Do we need to generate mipmaps?
        if (this.props.mipmaps) {
            this.generateMipmaps();
        }
        log.info(1, `${this} loaded`);
        this.resolveReady();
    }
    destroy() {
        if (this.texture) {
            this.texture.destroy();
            // @ts-expect-error
            this.texture = null;
        }
        this.destroyed = true;
    }
    generateMipmaps() {
        // if (this.device.type === 'webgl') {
        this.texture.generateMipmapsWebGL();
        // }
    }
    /** Set sampler or create and set new Sampler from SamplerProps */
    setSampler(sampler = {}) {
        this.texture.setSampler(sampler instanceof Sampler ? sampler : this.device.createSampler(sampler));
    }
    /**
     * Textures are immutable and cannot be resized after creation,
     * but we can create a similar texture with the same parameters but a new size.
     * @note Does not copy contents of the texture
     * @note Mipmaps may need to be regenerated after resizing / setting new data
     * @todo Abort pending promise and create a texture with the new size?
     */
    resize(size) {
        if (!this.isReady) {
            throw new Error('Cannot resize texture before it is ready');
        }
        if (size.width === this.texture.width && size.height === this.texture.height) {
            return false;
        }
        if (this.texture) {
            const texture = this.texture;
            this.texture = texture.clone(size);
            texture.destroy();
        }
        return true;
    }
    /** Check if texture data is a typed array */
    isTextureLevelData(data) {
        const typedArray = data?.data;
        return ArrayBuffer.isView(typedArray);
    }
    /** Get the size of the texture described by the provided TextureData */
    getTextureDataSize(data) {
        if (!data) {
            return null;
        }
        if (ArrayBuffer.isView(data)) {
            return null;
        }
        // Recurse into arrays (array of miplevels)
        if (Array.isArray(data)) {
            return this.getTextureDataSize(data[0]);
        }
        if (this.device.isExternalImage(data)) {
            return this.device.getExternalImageSize(data);
        }
        if (data && typeof data === 'object' && data.constructor === Object) {
            const textureDataArray = Object.values(data);
            const untypedData = textureDataArray[0];
            return { width: untypedData.width, height: untypedData.height };
        }
        throw new Error('texture size deduction failed');
    }
    /** Convert luma.gl cubemap face constants to depth index */
    getCubeFaceDepth(face) {
        // prettier-ignore
        switch (face) {
            case '+X': return 0;
            case '-X': return 1;
            case '+Y': return 2;
            case '-Y': return 3;
            case '+Z': return 4;
            case '-Z': return 5;
            default: throw new Error(face);
        }
    }
    // EXPERIMENTAL
    setTextureData(data) { }
    /** Experimental: Set multiple mip levels */
    _setTexture1DData(texture, data) {
        throw new Error('setTexture1DData not supported in WebGL.');
    }
    /** Experimental: Set multiple mip levels */
    _setTexture2DData(lodData, depth = 0) {
        if (!this.texture) {
            throw new Error('Texture not initialized');
        }
        const lodArray = this._normalizeTextureData(lodData);
        // If the user provides multiple LODs, then automatic mipmap
        // generation generateMipmap() should be disabled to avoid overwriting them.
        if (lodArray.length > 1 && this.props.mipmaps !== false) {
            log.warn(`Texture ${this.id} mipmap and multiple LODs.`)();
        }
        for (let mipLevel = 0; mipLevel < lodArray.length; mipLevel++) {
            const imageData = lodArray[mipLevel];
            if (this.device.isExternalImage(imageData)) {
                this.texture.copyExternalImage({ image: imageData, depth, mipLevel, flipY: true });
            }
            else {
                this.texture.copyImageData({ data: imageData.data /* , depth */, mipLevel });
            }
        }
    }
    /**
     * Experimental: Sets 3D texture data: multiple depth slices, multiple mip levels
     * @param data
     */
    _setTexture3DData(texture, data) {
        if (this.texture?.props.dimension !== '3d') {
            throw new Error(this.id);
        }
        for (let depth = 0; depth < data.length; depth++) {
            this._setTexture2DData(data[depth], depth);
        }
    }
    /**
     * Experimental: Set Cube texture data, multiple faces, multiple mip levels
     * @todo - could support TextureCubeArray with depth
     * @param data
     * @param index
     */
    _setTextureCubeData(texture, data) {
        if (this.texture?.props.dimension !== 'cube') {
            throw new Error(this.id);
        }
        for (const [face, faceData] of Object.entries(data)) {
            const faceDepth = CubeFaces.indexOf(face);
            this._setTexture2DData(faceData, faceDepth);
        }
    }
    /**
     * Experimental: Sets texture array data, multiple levels, multiple depth slices
     * @param data
     */
    _setTextureArrayData(texture, data) {
        if (this.texture?.props.dimension !== '2d-array') {
            throw new Error(this.id);
        }
        for (let depth = 0; depth < data.length; depth++) {
            this._setTexture2DData(data[depth], depth);
        }
    }
    /**
     * Experimental: Sets texture cube array, multiple faces, multiple levels, multiple mip levels
     * @param data
     */
    _setTextureCubeArrayData(texture, data) {
        throw new Error('setTextureCubeArrayData not supported in WebGL2.');
    }
    /** Experimental */
    _setTextureCubeFaceData(texture, lodData, face, depth = 0) {
        // assert(this.props.dimension === 'cube');
        // If the user provides multiple LODs, then automatic mipmap
        // generation generateMipmap() should be disabled to avoid overwriting them.
        if (Array.isArray(lodData) && lodData.length > 1 && this.props.mipmaps !== false) {
            log.warn(`${this.id} has mipmap and multiple LODs.`)();
        }
        const faceDepth = TextureCubeFaces.indexOf(face);
        this._setTexture2DData(lodData, faceDepth);
    }
    /**
     * Normalize TextureData to an array of TextureImageData / ExternalImages
     * @param data
     * @param options
     * @returns array of TextureImageData / ExternalImages
     */
    _normalizeTextureData(data) {
        const options = this.texture;
        let mipLevelArray;
        if (ArrayBuffer.isView(data)) {
            mipLevelArray = [
                {
                    // ts-expect-error does data really need to be Uint8ClampedArray?
                    data,
                    width: options.width,
                    height: options.height
                    // depth: options.depth
                }
            ];
        }
        else if (!Array.isArray(data)) {
            mipLevelArray = [data];
        }
        else {
            mipLevelArray = data;
        }
        return mipLevelArray;
    }
    static defaultProps = {
        ...Texture.defaultProps,
        data: null,
        mipmaps: false
    };
}
// TODO - Remove when texture refactor is complete
/*
setCubeMapData(options: {
  width: number;
  height: number;
  data: Record<GL, Texture2DData> | Record<TextureCubeFace, Texture2DData>;
  format?: any;
  type?: any;
  /** @deprecated Use .data *
  pixels: any;
}): void {
  const {gl} = this;

  const {width, height, pixels, data, format = GL.RGBA, type = GL.UNSIGNED_BYTE} = options;

  // pixel data (imageDataMap) is an Object from Face to Image or Promise.
  // For example:
  // {
  // GL.TEXTURE_CUBE_MAP_POSITIVE_X : Image-or-Promise,
  // GL.TEXTURE_CUBE_MAP_NEGATIVE_X : Image-or-Promise,
  // ... }
  // To provide multiple level-of-details (LODs) this can be Face to Array
  // of Image or Promise, like this
  // {
  // GL.TEXTURE_CUBE_MAP_POSITIVE_X : [Image-or-Promise-LOD-0, Image-or-Promise-LOD-1],
  // GL.TEXTURE_CUBE_MAP_NEGATIVE_X : [Image-or-Promise-LOD-0, Image-or-Promise-LOD-1],
  // ... }

  const imageDataMap = this._getImageDataMap(pixels || data);

  const resolvedFaces = WEBGLTexture.FACES.map(face => {
    const facePixels = imageDataMap[face];
    return Array.isArray(facePixels) ? facePixels : [facePixels];
  });
  this.bind();

  WEBGLTexture.FACES.forEach((face, index) => {
    if (resolvedFaces[index].length > 1 && this.props.mipmaps !== false) {
      // If the user provides multiple LODs, then automatic mipmap
      // generation generateMipmaps() should be disabled to avoid overwritting them.
      log.warn(`${this.id} has mipmap and multiple LODs.`)();
    }
    resolvedFaces[index].forEach((image, lodLevel) => {
      // TODO: adjust width & height for LOD!
      if (width && height) {
        gl.texImage2D(face, lodLevel, format, width, height, 0 /* border*, format, type, image);
      } else {
        gl.texImage2D(face, lodLevel, format, format, type, image);
      }
    });
  });

  this.unbind();
}
*/
// HELPERS
/** Resolve all promises in a nested data structure */
async function awaitAllPromises(x) {
    x = await x;
    if (Array.isArray(x)) {
        return await Promise.all(x.map(awaitAllPromises));
    }
    if (x && typeof x === 'object' && x.constructor === Object) {
        const object = x;
        const values = await Promise.all(Object.values(object));
        const keys = Object.keys(object);
        const resolvedObject = {};
        for (let i = 0; i < keys.length; i++) {
            resolvedObject[keys[i]] = values[i];
        }
        return resolvedObject;
    }
    return x;
}
//# sourceMappingURL=async-texture.js.map