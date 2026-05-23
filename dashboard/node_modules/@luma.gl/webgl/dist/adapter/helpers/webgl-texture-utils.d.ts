import type { Buffer, Texture, FramebufferProps } from '@luma.gl/core';
import { Framebuffer } from '@luma.gl/core';
import { GL, GLTextureTarget, GLTextureCubeMapTarget, GLTexelDataFormat, GLPixelType } from '@luma.gl/constants';
import { WEBGLFramebuffer } from "../resources/webgl-framebuffer.js";
import { WEBGLBuffer } from "../resources/webgl-buffer.js";
/**
 * Options for setting data into a texture
 */
export type WebGLSetTextureOptions = {
    dimension: '1d' | '2d' | '2d-array' | 'cube' | 'cube-array' | '3d';
    height: number;
    width: number;
    depth: number;
    mipLevel?: number;
    glTarget: GLTextureTarget;
    glInternalFormat: GL;
    glFormat: GLTexelDataFormat;
    glType: GLPixelType;
    compressed?: boolean;
    byteOffset?: number;
    byteLength?: number;
};
/**
 * Options for copying an image or data into a texture
 *
 * @param {GLenum} format - internal format of image data.
 * @param {GLenum} type
 *  - format of array (autodetect from type) or
 *  - (WEBGL2) format of buffer or ArrayBufferView
 * @param {GLenum} dataFormat - format of image data.
 * @param {Number} offset - (WEBGL2) offset from start of buffer
 * @parameters - temporary settings to be applied, can be used to supply pixel store settings.
 */
export type WebGLCopyTextureOptions = {
    dimension: '1d' | '2d' | '2d-array' | 'cube' | 'cube-array' | '3d';
    /** mip level to be updated */
    mipLevel?: number;
    /** width of the sub image to be updated */
    width: number;
    /** height of the sub image to be updated */
    height: number;
    /** depth of texture to be updated */
    depth?: number;
    /** xOffset from where texture to be updated */
    x?: number;
    /** yOffset from where texture to be updated */
    y?: number;
    /** yOffset from where texture to be updated */
    z?: number;
    glTarget: GLTextureTarget;
    glInternalFormat: GL;
    glFormat: GL;
    glType: GL;
    compressed?: boolean;
    byteOffset?: number;
    byteLength?: number;
};
/**
 * Copy a region of compressed data from a GPU memory buffer into this texture.
 */
export declare function copyGPUBufferToMipLevel(gl: WebGL2RenderingContext, webglBuffer: WebGLBuffer, byteLength: number, options: WebGLCopyTextureOptions): void;
/** Convert a WebGPU style texture constant to a WebGL style texture constant */
export declare function getWebGLTextureTarget(dimension: '1d' | '2d' | '2d-array' | 'cube' | 'cube-array' | '3d'): GLTextureTarget;
/**
 * In WebGL, cube maps specify faces by overriding target instead of using the depth parameter.
 * @note We still bind the texture using GL.TEXTURE_CUBE_MAP, but we need to use the face-specific target when setting mip levels.
 * @returns glTarget unchanged, if dimension !== 'cube'.
 */
export declare function getWebGLCubeFaceTarget(glTarget: GLTextureTarget, dimension: '1d' | '2d' | '2d-array' | 'cube' | 'cube-array' | '3d', level: number): GLTextureTarget | GLTextureCubeMapTarget;
export type ReadPixelsToArrayOptions = {
    sourceX?: number;
    sourceY?: number;
    sourceFormat?: number;
    sourceAttachment?: number;
    target?: Uint8Array | Uint16Array | Float32Array;
    sourceWidth?: number;
    sourceHeight?: number;
    sourceDepth?: number;
    sourceType?: number;
};
export type ReadPixelsToBufferOptions = {
    sourceX?: number;
    sourceY?: number;
    sourceFormat?: number;
    target?: Buffer;
    targetByteOffset?: number;
    sourceWidth?: number;
    sourceHeight?: number;
    sourceType?: number;
};
/**
 * Copies data from a type  or a Texture object into ArrayBuffer object.
 * App can provide targetPixelArray or have it auto allocated by this method
 *  newly allocated by this method unless provided by app.
 * @deprecated Use CommandEncoder.copyTextureToBuffer and Buffer.read
 * @note Slow requires roundtrip to GPU
 *
 * @param source
 * @param options
 * @returns pixel array,
 */
export declare function readPixelsToArray(source: Framebuffer | Texture, options?: ReadPixelsToArrayOptions): Uint8Array | Uint16Array | Float32Array;
/**
 * Copies data from a Framebuffer or a Texture object into a Buffer object.
 * NOTE: doesn't wait for copy to be complete, it programs GPU to perform a DMA transffer.
 * @deprecated Use CommandEncoder
 * @param source
 * @param options
 */
export declare function readPixelsToBuffer(source: Framebuffer | Texture, options?: ReadPixelsToBufferOptions): WEBGLBuffer;
/**
 * Copy a rectangle from a Framebuffer or Texture object into a texture (at an offset)
 * @deprecated Use CommandEncoder
 */
export declare function copyToTexture(sourceTexture: Framebuffer | Texture, destinationTexture: Texture | GL, options?: {
    sourceX?: number;
    sourceY?: number;
    targetX?: number;
    targetY?: number;
    targetZ?: number;
    targetMipmaplevel?: number;
    targetInternalFormat?: number;
    width?: number;
    height?: number;
}): Texture;
/**
 * Wraps a given texture into a framebuffer object, that can be further used
 * to read data from the texture object.
 */
export declare function toFramebuffer(texture: Texture, props?: FramebufferProps): WEBGLFramebuffer;
//# sourceMappingURL=webgl-texture-utils.d.ts.map