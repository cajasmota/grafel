// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { Texture, log } from '@luma.gl/core';
import { GL } from '@luma.gl/constants';
import { getTextureFormatWebGL } from "../converters/webgl-texture-table.js";
import { convertSamplerParametersToWebGL } from "../converters/sampler-parameters.js";
import { withGLParameters } from "../../context/state-tracker/with-parameters.js";
import { WEBGLTextureView } from "./webgl-texture-view.js";
/**
 * WebGL... the texture API from hell... hopefully made simpler
 */
export class WEBGLTexture extends Texture {
    // readonly MAX_ATTRIBUTES: number;
    device;
    gl;
    handle;
    // @ts-ignore TODO - currently unused in WebGL. Create dummy sampler?
    sampler = undefined;
    view;
    /**
     * The WebGL target corresponding to the texture type
     * @note `target` cannot be modified by bind:
     * textures are special because when you first bind them to a target,
     * When you first bind a texture as a GL_TEXTURE_2D, you are saying that this texture is a 2D texture.
     * And it will always be a 2D texture; this state cannot be changed ever.
     * A texture that was first bound as a GL_TEXTURE_2D, must always be bound as a GL_TEXTURE_2D;
     * attempting to bind it as GL_TEXTURE_3D will give rise to a run-time error
     */
    glTarget;
    /** The WebGL format - essentially channel structure */
    glFormat;
    /** The WebGL data format - the type of each channel */
    glType;
    /** The WebGL constant corresponding to the WebGPU style constant in format */
    glInternalFormat;
    /** Whether the internal format is compressed */
    compressed;
    // state
    /** Texture binding slot - TODO - move to texture view? */
    _textureUnit = 0;
    constructor(device, props) {
        super(device, props);
        this.device = device;
        this.gl = this.device.gl;
        const formatInfo = getTextureFormatWebGL(this.props.format);
        // Note: In WebGL the texture target defines the type of texture on first bind.
        this.glTarget = getWebGLTextureTarget(this.props.dimension);
        this.glInternalFormat = formatInfo.internalFormat;
        this.glFormat = formatInfo.format;
        this.glType = formatInfo.type;
        this.compressed = formatInfo.compressed;
        this.handle = this.props.handle || this.gl.createTexture();
        this.device._setWebGLDebugMetadata(this.handle, this, { spector: this.props });
        /**
         * Use WebGL immutable texture storage to allocate and clear texture memory.
         * - texStorage2D should be considered a preferred alternative to texImage2D. It may have lower memory costs than texImage2D in some implementations.
         * - Once texStorage*D has been called, the texture is immutable and can only be updated with texSubImage*(), not texImage()
         * @see https://registry.khronos.org/webgl/specs/latest/2.0/ WebGL 2 spec section 3.7.6
         */
        this.gl.bindTexture(this.glTarget, this.handle);
        const { dimension, width, height, depth, mipLevels, glTarget, glInternalFormat } = this;
        switch (dimension) {
            case '2d':
            case 'cube':
                this.gl.texStorage2D(glTarget, mipLevels, glInternalFormat, width, height);
                break;
            case '2d-array':
            case '3d':
                this.gl.texStorage3D(glTarget, mipLevels, glInternalFormat, width, height, depth);
                break;
            default:
                throw new Error(dimension);
        }
        this.gl.bindTexture(this.glTarget, null);
        // Set data
        this._initializeData(props.data);
        // Set texture sampler parameters
        this.setSampler(this.props.sampler);
        // @ts-ignore TODO - fix types
        this.view = new WEBGLTextureView(this.device, { ...this.props, texture: this });
        Object.seal(this);
    }
    destroy() {
        if (this.handle) {
            this.gl.deleteTexture(this.handle);
            this.removeStats();
            this.trackDeallocatedMemory('Texture');
            // this.handle = null;
            this.destroyed = true;
        }
    }
    createView(props) {
        return new WEBGLTextureView(this.device, { ...props, texture: this });
    }
    setSampler(sampler = {}) {
        super.setSampler(sampler);
        // Apply sampler parameters to texture
        const parameters = convertSamplerParametersToWebGL(this.sampler.props);
        this._setSamplerParameters(parameters);
    }
    copyImageData(options_) {
        const options = this._normalizeCopyImageDataOptions(options_);
        const typedArray = options.data;
        const { width, height, depth } = this;
        const { mipLevel = 0, byteOffset = 0, x = 0, y = 0, z = 0 } = options;
        const { glFormat, glType, compressed } = this;
        // Target used for face updates, but not for binding
        const glTarget = getWebGLCubeFaceTarget(this.glTarget, this.dimension, z);
        let unpackRowLength;
        if (!this.compressed) {
            const { bytesPerPixel } = this.device.getTextureFormatInfo(this.format);
            if (bytesPerPixel) {
                if (options.bytesPerRow % bytesPerPixel !== 0) {
                    throw new Error(`bytesPerRow (${options.bytesPerRow}) must be a multiple of bytesPerPixel (${bytesPerPixel}) for ${this.format}`);
                }
                unpackRowLength = options.bytesPerRow / bytesPerPixel;
            }
        }
        const glParameters = !this.compressed
            ? {
                ...(unpackRowLength !== undefined ? { [3314]: unpackRowLength } : {}),
                [32878]: options.rowsPerImage
            }
            : {};
        this.gl.bindTexture(glTarget, this.handle);
        withGLParameters(this.gl, glParameters, () => {
            switch (this.dimension) {
                case '2d':
                case 'cube':
                    if (compressed) {
                        // prettier-ignore
                        this.gl.compressedTexSubImage2D(glTarget, mipLevel, x, y, width, height, glFormat, typedArray, byteOffset); // , byteLength
                    }
                    else {
                        // prettier-ignore
                        this.gl.texSubImage2D(glTarget, mipLevel, x, y, width, height, glFormat, glType, typedArray, byteOffset); // , byteLength
                    }
                    break;
                case '2d-array':
                case '3d':
                    if (compressed) {
                        // prettier-ignore
                        this.gl.compressedTexSubImage3D(glTarget, mipLevel, x, y, z, width, height, depth, glFormat, typedArray, byteOffset); // , byteLength
                    }
                    else {
                        // prettier-ignore
                        this.gl.texSubImage3D(glTarget, mipLevel, x, y, z, width, height, depth, glFormat, glType, typedArray, byteOffset); // , byteLength
                    }
                    break;
                default:
                // Can never happen in WebGL
            }
        });
        this.gl.bindTexture(glTarget, null);
    }
    copyExternalImage(options_) {
        const options = this._normalizeCopyExternalImageOptions(options_);
        if (options.sourceX || options.sourceY) {
            // requires copyTexSubImage2D from a framebuffer'
            throw new Error('WebGL does not support sourceX/sourceY)');
        }
        const { glFormat, glType } = this;
        const { image, depth, mipLevel, x, y, z, width, height } = options;
        // WebGL cube maps specify faces by overriding target instead of using the depth parameter
        const glTarget = getWebGLCubeFaceTarget(this.glTarget, this.dimension, depth);
        const glParameters = options.flipY ? { [37440]: true } : {};
        this.gl.bindTexture(this.glTarget, this.handle);
        withGLParameters(this.gl, glParameters, () => {
            switch (this.dimension) {
                case '2d':
                case 'cube':
                    // prettier-ignore
                    this.gl.texSubImage2D(glTarget, mipLevel, x, y, width, height, glFormat, glType, image);
                    break;
                case '2d-array':
                case '3d':
                    // prettier-ignore
                    this.gl.texSubImage3D(glTarget, mipLevel, x, y, z, width, height, depth, glFormat, glType, image);
                    break;
                default:
                // Can never happen in WebGL
            }
        });
        this.gl.bindTexture(this.glTarget, null);
        return { width: options.width, height: options.height };
    }
    // WEBGL SPECIFIC
    generateMipmapsWebGL(options) {
        const isFilterableAndRenderable = this.device.isTextureFormatRenderable(this.props.format) &&
            this.device.isTextureFormatFilterable(this.props.format);
        if (!isFilterableAndRenderable) {
            log.warn(`${this} is not renderable or filterable, may not be able to generate mipmaps`)();
            if (!options?.force) {
                return;
            }
        }
        try {
            this.gl.bindTexture(this.glTarget, this.handle);
            this.gl.generateMipmap(this.glTarget);
        }
        catch (error) {
            log.warn(`Error generating mipmap for ${this}: ${error.message}`)();
        }
        finally {
            this.gl.bindTexture(this.glTarget, null);
        }
    }
    // INTERNAL
    /**
     * Sets sampler parameters on texture
     */
    _setSamplerParameters(parameters) {
        log.log(2, `${this.id} sampler parameters`, this.device.getGLKeys(parameters))();
        this.gl.bindTexture(this.glTarget, this.handle);
        for (const [pname, pvalue] of Object.entries(parameters)) {
            const param = Number(pname);
            const value = pvalue;
            // Apparently integer/float issues require two different texture parameter setting functions in JavaScript.
            // For now, pick the float version for parameters specified as GLfloat.
            switch (param) {
                case 33082:
                case 33083:
                    this.gl.texParameterf(this.glTarget, param, value);
                    break;
                case 10240:
                case 10241:
                    this.gl.texParameteri(this.glTarget, param, value);
                    break;
                case 10242:
                case 10243:
                case 32882:
                    this.gl.texParameteri(this.glTarget, param, value);
                    break;
                case 34046:
                    // We have to query feature before using it
                    if (this.device.features.has('texture-filterable-anisotropic-webgl')) {
                        this.gl.texParameteri(this.glTarget, param, value);
                    }
                    break;
                case 34892:
                case 34893:
                    this.gl.texParameteri(this.glTarget, param, value);
                    break;
            }
        }
        this.gl.bindTexture(this.glTarget, null);
    }
    _getActiveUnit() {
        return this.gl.getParameter(34016) - 33984;
    }
    _bind(_textureUnit) {
        const { gl } = this;
        if (_textureUnit !== undefined) {
            this._textureUnit = _textureUnit;
            gl.activeTexture(33984 + _textureUnit);
        }
        gl.bindTexture(this.glTarget, this.handle);
        // @ts-ignore TODO fix types
        return _textureUnit;
    }
    _unbind(_textureUnit) {
        const { gl } = this;
        if (_textureUnit !== undefined) {
            this._textureUnit = _textureUnit;
            gl.activeTexture(33984 + _textureUnit);
        }
        gl.bindTexture(this.glTarget, null);
        return _textureUnit;
    }
}
// INTERNAL HELPERS
/** Convert a WebGPU style texture constant to a WebGL style texture constant */
export function getWebGLTextureTarget(dimension) {
    // prettier-ignore
    switch (dimension) {
        case '1d': break; // not supported in any WebGL version
        case '2d': return 3553; // supported in WebGL1
        case '3d': return 32879; // supported in WebGL2
        case 'cube': return 34067; // supported in WebGL1
        case '2d-array': return 35866; // supported in WebGL2
        case 'cube-array': break; // not supported in any WebGL version
    }
    throw new Error(dimension);
}
/**
 * In WebGL, cube maps specify faces by overriding target instead of using the depth parameter.
 * @note We still bind the texture using GL.TEXTURE_CUBE_MAP, but we need to use the face-specific target when setting mip levels.
 * @returns glTarget unchanged, if dimension !== 'cube'.
 */
export function getWebGLCubeFaceTarget(glTarget, dimension, level) {
    return dimension === 'cube' ? 34069 + level : glTarget;
}
//# sourceMappingURL=webgl-texture.js.map