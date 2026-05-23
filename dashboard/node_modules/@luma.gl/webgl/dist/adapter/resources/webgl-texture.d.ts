import type { Device, TextureProps, TextureViewProps, Sampler, SamplerProps, CopyExternalImageOptions, CopyImageDataOptions } from '@luma.gl/core';
import { Texture } from '@luma.gl/core';
import { GL, GLTextureTarget, GLTextureCubeMapTarget, GLTexelDataFormat, GLPixelType, GLSamplerParameters } from '@luma.gl/constants';
import { WebGLDevice } from "../webgl-device.js";
import { WEBGLSampler } from "./webgl-sampler.js";
import { WEBGLTextureView } from "./webgl-texture-view.js";
/**
 * WebGL... the texture API from hell... hopefully made simpler
 */
export declare class WEBGLTexture extends Texture {
    readonly device: WebGLDevice;
    readonly gl: WebGL2RenderingContext;
    handle: WebGLTexture;
    sampler: WEBGLSampler;
    view: WEBGLTextureView;
    /**
     * The WebGL target corresponding to the texture type
     * @note `target` cannot be modified by bind:
     * textures are special because when you first bind them to a target,
     * When you first bind a texture as a GL_TEXTURE_2D, you are saying that this texture is a 2D texture.
     * And it will always be a 2D texture; this state cannot be changed ever.
     * A texture that was first bound as a GL_TEXTURE_2D, must always be bound as a GL_TEXTURE_2D;
     * attempting to bind it as GL_TEXTURE_3D will give rise to a run-time error
     */
    glTarget: GLTextureTarget;
    /** The WebGL format - essentially channel structure */
    glFormat: GLTexelDataFormat;
    /** The WebGL data format - the type of each channel */
    glType: GLPixelType;
    /** The WebGL constant corresponding to the WebGPU style constant in format */
    glInternalFormat: GL;
    /** Whether the internal format is compressed */
    compressed: boolean;
    /** Texture binding slot - TODO - move to texture view? */
    _textureUnit: number;
    constructor(device: Device, props: TextureProps);
    destroy(): void;
    createView(props: TextureViewProps): WEBGLTextureView;
    setSampler(sampler?: Sampler | SamplerProps): void;
    copyImageData(options_: CopyImageDataOptions): void;
    copyExternalImage(options_: CopyExternalImageOptions): {
        width: number;
        height: number;
    };
    generateMipmapsWebGL(options?: {
        force?: boolean;
    }): void;
    /**
     * Sets sampler parameters on texture
     */
    _setSamplerParameters(parameters: GLSamplerParameters): void;
    _getActiveUnit(): number;
    _bind(_textureUnit?: number): number;
    _unbind(_textureUnit?: number): number | undefined;
}
/** Convert a WebGPU style texture constant to a WebGL style texture constant */
export declare function getWebGLTextureTarget(dimension: '1d' | '2d' | '2d-array' | 'cube' | 'cube-array' | '3d'): GLTextureTarget;
/**
 * In WebGL, cube maps specify faces by overriding target instead of using the depth parameter.
 * @note We still bind the texture using GL.TEXTURE_CUBE_MAP, but we need to use the face-specific target when setting mip levels.
 * @returns glTarget unchanged, if dimension !== 'cube'.
 */
export declare function getWebGLCubeFaceTarget(glTarget: GLTextureTarget, dimension: '1d' | '2d' | '2d-array' | 'cube' | 'cube-array' | '3d', level: number): GLTextureTarget | GLTextureCubeMapTarget;
//# sourceMappingURL=webgl-texture.d.ts.map