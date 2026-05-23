import type { CopyBufferToBufferOptions, CopyBufferToTextureOptions, CopyTextureToBufferOptions, CopyTextureToTextureOptions } from '@luma.gl/core';
import { CommandBuffer } from '@luma.gl/core';
import { GL, GLTextureTarget, GLTextureCubeMapTarget } from '@luma.gl/constants';
import { WebGLDevice } from "../webgl-device.js";
type CopyBufferToBufferCommand = {
    name: 'copy-buffer-to-buffer';
    options: CopyBufferToBufferOptions;
};
type CopyBufferToTextureCommand = {
    name: 'copy-buffer-to-texture';
    options: CopyBufferToTextureOptions;
};
type CopyTextureToBufferCommand = {
    name: 'copy-texture-to-buffer';
    options: CopyTextureToBufferOptions;
};
type CopyTextureToTextureCommand = {
    name: 'copy-texture-to-texture';
    options: CopyTextureToTextureOptions;
};
type ClearTextureCommand = {
    name: 'clear-texture';
    options: {};
};
type ReadTextureCommand = {
    name: 'read-texture';
    options: {};
};
type Command = CopyBufferToBufferCommand | CopyBufferToTextureCommand | CopyTextureToBufferCommand | CopyTextureToTextureCommand | ClearTextureCommand | ReadTextureCommand;
export declare class WEBGLCommandBuffer extends CommandBuffer {
    readonly device: WebGLDevice;
    readonly handle: null;
    commands: Command[];
    constructor(device: WebGLDevice);
    _executeCommands(commands?: Command[]): void;
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
/**
 * In WebGL, cube maps specify faces by overriding target instead of using the depth parameter.
 * @note We still bind the texture using GL.TEXTURE_CUBE_MAP, but we need to use the face-specific target when setting mip levels.
 * @returns glTarget unchanged, if dimension !== 'cube'.
 */
export declare function getWebGLCubeFaceTarget(glTarget: GLTextureTarget, dimension: '1d' | '2d' | '2d-array' | 'cube' | 'cube-array' | '3d', level: number): GLTextureTarget | GLTextureCubeMapTarget;
/**
 * Returns number of components in a specific readPixels WebGL format
 * @todo use shadertypes utils instead?
 */
export declare function glFormatToComponents(format: GL): 1 | 2 | 3 | 4;
/**
 * Return byte count for given readPixels WebGL type
 * @todo use shadertypes utils instead?
 */
export declare function glTypeToBytes(type: GL): 1 | 2 | 4;
export {};
//# sourceMappingURL=webgl-command-buffer.d.ts.map