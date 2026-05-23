// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { CommandBuffer, Texture } from '@luma.gl/core';
import { GL } from '@luma.gl/constants';
import { WEBGLTexture } from "./webgl-texture.js";
import { getTextureFormatWebGL } from "../converters/webgl-texture-table.js";
export class WEBGLCommandBuffer extends CommandBuffer {
    device;
    handle = null;
    commands = [];
    constructor(device) {
        super(device, {});
        this.device = device;
    }
    _executeCommands(commands = this.commands) {
        for (const command of commands) {
            switch (command.name) {
                case 'copy-buffer-to-buffer':
                    _copyBufferToBuffer(this.device, command.options);
                    break;
                case 'copy-buffer-to-texture':
                    _copyBufferToTexture(this.device, command.options);
                    break;
                case 'copy-texture-to-buffer':
                    _copyTextureToBuffer(this.device, command.options);
                    break;
                case 'copy-texture-to-texture':
                    _copyTextureToTexture(this.device, command.options);
                    break;
                // case 'clear-texture':
                //   _clearTexture(this.device, command.options);
                //   break;
                default:
                    throw new Error(command.name);
            }
        }
    }
}
function _copyBufferToBuffer(device, options) {
    const source = options.sourceBuffer;
    const destination = options.destinationBuffer;
    // {In WebGL2 we can p}erform the copy on the GPU
    // Use GL.COPY_READ_BUFFER+GL.COPY_WRITE_BUFFER avoid disturbing other targets and locking type
    device.gl.bindBuffer(36662, source.handle);
    device.gl.bindBuffer(36663, destination.handle);
    device.gl.copyBufferSubData(36662, 36663, options.sourceOffset ?? 0, options.destinationOffset ?? 0, options.size);
    device.gl.bindBuffer(36662, null);
    device.gl.bindBuffer(36663, null);
}
/**
 * Copies data from a Buffer object into a Texture object
 * NOTE: doesn't wait for copy to be complete
 */
function _copyBufferToTexture(device, options) {
    throw new Error('Not implemented');
}
/**
 * Copies data from a Texture object into a Buffer object.
 * NOTE: doesn't wait for copy to be complete
 */
function _copyTextureToBuffer(device, options) {
    const { 
    /** Texture to copy to/from. */
    sourceTexture, 
    /**  Mip-map level of the texture to copy to/from. (Default 0) */
    mipLevel = 0, 
    /** Defines which aspects of the texture to copy to/from. */
    aspect = 'all', 
    /** Width to copy */
    width = options.sourceTexture.width, 
    /** Height to copy */
    height = options.sourceTexture.height, depthOrArrayLayers = 0, 
    /** Defines the origin of the copy - the minimum corner of the texture sub-region to copy to/from. */
    origin = [0, 0], 
    /** Destination buffer */
    destinationBuffer, 
    /** Offset, in bytes, from the beginning of the buffer to the start of the image data (default 0) */
    byteOffset = 0, 
    /**
     * The stride, in bytes, between the beginning of each block row and the subsequent block row.
     * Required if there are multiple block rows (i.e. the copy height or depth is more than one block).
     */
    bytesPerRow, 
    /**
     * Number of block rows per single image of the texture.
     * rowsPerImage &times; bytesPerRow is the stride, in bytes, between the beginning of each image of data and the subsequent image.
     * Required if there are multiple images (i.e. the copy depth is more than one).
     */
    rowsPerImage } = options;
    // TODO - Not possible to read just stencil or depth part in WebGL?
    if (aspect !== 'all') {
        throw new Error('aspect not supported in WebGL');
    }
    // TODO - mipLevels are set when attaching texture to framebuffer
    if (mipLevel !== 0 || depthOrArrayLayers !== 0 || bytesPerRow || rowsPerImage) {
        throw new Error('not implemented');
    }
    // Asynchronous read (PIXEL_PACK_BUFFER) is WebGL2 only feature
    const { framebuffer, destroyFramebuffer } = getFramebuffer(sourceTexture);
    let prevHandle;
    try {
        const webglBuffer = destinationBuffer;
        const sourceWidth = width || framebuffer.width;
        const sourceHeight = height || framebuffer.height;
        const sourceParams = getTextureFormatWebGL(framebuffer.colorAttachments[0].texture.props.format);
        const sourceFormat = sourceParams.format;
        const sourceType = sourceParams.type;
        // if (!target) {
        //   // Create new buffer with enough size
        //   const components = glFormatToComponents(sourceFormat);
        //   const byteCount = glTypeToBytes(sourceType);
        //   const byteLength = byteOffset + sourceWidth * sourceHeight * components * byteCount;
        //   target = device.createBuffer({byteLength});
        // }
        device.gl.bindBuffer(35051, webglBuffer.handle);
        // @ts-expect-error native bindFramebuffer is overridden by our state tracker
        prevHandle = device.gl.bindFramebuffer(36160, framebuffer.handle);
        device.gl.readPixels(origin[0], origin[1], sourceWidth, sourceHeight, sourceFormat, sourceType, byteOffset);
    }
    finally {
        device.gl.bindBuffer(35051, null);
        // prevHandle may be unassigned if the try block failed before binding
        if (prevHandle !== undefined) {
            device.gl.bindFramebuffer(36160, prevHandle);
        }
        if (destroyFramebuffer) {
            framebuffer.destroy();
        }
    }
}
/**
 * Copies data from a Framebuffer or a Texture object into a Buffer object.
 * NOTE: doesn't wait for copy to be complete, it programs GPU to perform a DMA transfer.
export function readPixelsToBuffer(
  source: Framebuffer | Texture,
  options?: {
    sourceX?: number;
    sourceY?: number;
    sourceFormat?: number;
    target?: Buffer; // A new Buffer object is created when not provided.
    targetByteOffset?: number; // byte offset in buffer object
    // following parameters are auto deduced if not provided
    sourceWidth?: number;
    sourceHeight?: number;
    sourceType?: number;
  }
): Buffer
 */
/**
 * Copy a rectangle from a Framebuffer or Texture object into a texture (at an offset)
 */
// eslint-disable-next-line complexity, max-statements
function _copyTextureToTexture(device, options) {
    const { 
    /** Texture to copy to/from. */
    sourceTexture, 
    /**  Mip-map level of the texture to copy to (Default 0) */
    destinationMipLevel = 0, 
    /** Defines which aspects of the texture to copy to/from. */
    // aspect = 'all',
    /** Defines the origin of the copy - the minimum corner of the texture sub-region to copy from. */
    origin = [0, 0], 
    /** Defines the origin of the copy - the minimum corner of the texture sub-region to copy to. */
    destinationOrigin = [0, 0], 
    /** Texture to copy to/from. */
    destinationTexture
    /**  Mip-map level of the texture to copy to/from. (Default 0) */
    // destinationMipLevel = options.mipLevel,
    /** Defines the origin of the copy - the minimum corner of the texture sub-region to copy to/from. */
    // destinationOrigin = [0, 0],
    /** Defines which aspects of the texture to copy to/from. */
    // destinationAspect = options.aspect,
     } = options;
    let { width = options.destinationTexture.width, height = options.destinationTexture.height
    // depthOrArrayLayers = 0
     } = options;
    const { framebuffer, destroyFramebuffer } = getFramebuffer(sourceTexture);
    const [sourceX, sourceY] = origin;
    const [destinationX, destinationY, destinationZ] = destinationOrigin;
    // @ts-expect-error native bindFramebuffer is overridden by our state tracker
    const prevHandle = device.gl.bindFramebuffer(36160, framebuffer.handle);
    // TODO - support gl.readBuffer (WebGL2 only)
    // const prevBuffer = gl.readBuffer(attachment);
    let texture;
    let textureTarget;
    if (destinationTexture instanceof WEBGLTexture) {
        texture = destinationTexture;
        width = Number.isFinite(width) ? width : texture.width;
        height = Number.isFinite(height) ? height : texture.height;
        texture._bind(0);
        textureTarget = texture.glTarget;
    }
    else {
        throw new Error('invalid destination');
    }
    switch (textureTarget) {
        case 3553:
        case 34067:
            device.gl.copyTexSubImage2D(textureTarget, destinationMipLevel, destinationX, destinationY, sourceX, sourceY, width, height);
            break;
        case 35866:
        case 32879:
            device.gl.copyTexSubImage3D(textureTarget, destinationMipLevel, destinationX, destinationY, destinationZ, sourceX, sourceY, width, height);
            break;
        default:
    }
    if (texture) {
        texture._unbind();
    }
    device.gl.bindFramebuffer(36160, prevHandle);
    if (destroyFramebuffer) {
        framebuffer.destroy();
    }
}
/** Clear one mip level of a texture *
function _clearTexture(device: WebGLDevice, options: ClearTextureOptions) {
  const BORDER = 0;
  const {dimension, width, height, depth = 0, mipLevel = 0} = options;
  const {glInternalFormat, glFormat, glType, compressed} = options;
  const glTarget = getWebGLCubeFaceTarget(options.glTarget, dimension, depth);

  switch (dimension) {
    case '2d-array':
    case '3d':
      if (compressed) {
        // prettier-ignore
        device.gl.compressedTexImage3D(glTarget, mipLevel, glInternalFormat, width, height, depth, BORDER, null);
      } else {
        // prettier-ignore
        device.gl.texImage3D( glTarget, mipLevel, glInternalFormat, width, height, depth, BORDER, glFormat, glType, null);
      }
      break;

    case '2d':
    case 'cube':
      if (compressed) {
        // prettier-ignore
        device.gl.compressedTexImage2D(glTarget, mipLevel, glInternalFormat, width, height, BORDER, null);
      } else {
        // prettier-ignore
        device.gl.texImage2D(glTarget, mipLevel, glInternalFormat, width, height, BORDER, glFormat, glType, null);
      }
      break;

    default:
      throw new Error(dimension);
  }
}
  */
// function _readTexture(device: WebGLDevice, options: CopyTextureToBufferOptions) {}
// HELPERS
/**
 * In WebGL, cube maps specify faces by overriding target instead of using the depth parameter.
 * @note We still bind the texture using GL.TEXTURE_CUBE_MAP, but we need to use the face-specific target when setting mip levels.
 * @returns glTarget unchanged, if dimension !== 'cube'.
 */
export function getWebGLCubeFaceTarget(glTarget, dimension, level) {
    return dimension === 'cube' ? 34069 + level : glTarget;
}
/** Wrap a texture in a framebuffer so that we can use WebGL APIs that work on framebuffers */
function getFramebuffer(source) {
    if (source instanceof Texture) {
        const { width, height, id } = source;
        const framebuffer = source.device.createFramebuffer({
            id: `framebuffer-for-${id}`,
            width,
            height,
            colorAttachments: [source]
        });
        return { framebuffer, destroyFramebuffer: true };
    }
    return { framebuffer: source, destroyFramebuffer: false };
}
/**
 * Returns number of components in a specific readPixels WebGL format
 * @todo use shadertypes utils instead?
 */
export function glFormatToComponents(format) {
    switch (format) {
        case 6406:
        case 33326:
        case 6403:
            return 1;
        case 33328:
        case 33319:
            return 2;
        case 6407:
        case 34837:
            return 3;
        case 6408:
        case 34836:
            return 4;
        // TODO: Add support for additional WebGL2 formats
        default:
            throw new Error('GLFormat');
    }
}
/**
 * Return byte count for given readPixels WebGL type
 * @todo use shadertypes utils instead?
 */
export function glTypeToBytes(type) {
    switch (type) {
        case 5121:
            return 1;
        case 33635:
        case 32819:
        case 32820:
            return 2;
        case 5126:
            return 4;
        // TODO: Add support for additional WebGL2 types
        default:
            throw new Error('GLType');
    }
}
//# sourceMappingURL=webgl-command-buffer.js.map