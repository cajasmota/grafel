import type { FramebufferProps } from '@luma.gl/core';
import { Framebuffer } from '@luma.gl/core';
import { GL } from '@luma.gl/constants';
import { WebGLDevice } from "../webgl-device.js";
import { WEBGLTexture } from "./webgl-texture.js";
import { WEBGLTextureView } from "./webgl-texture-view.js";
export type Attachment = WEBGLTextureView | WEBGLTexture;
/** luma.gl Framebuffer, WebGL implementation  */
export declare class WEBGLFramebuffer extends Framebuffer {
    readonly device: WebGLDevice;
    gl: WebGL2RenderingContext;
    readonly handle: WebGLFramebuffer;
    colorAttachments: WEBGLTextureView[];
    depthStencilAttachment: WEBGLTextureView | null;
    constructor(device: WebGLDevice, props: FramebufferProps);
    /** destroys any auto created resources etc. */
    destroy(): void;
    protected updateAttachments(): void;
    /** In WebGL we must use renderbuffers for depth/stencil attachments (unless we have extensions) */
    /**
     * @param attachment
     * @param texture
     * @param layer = 0 - index into WEBGLTextureArray and Texture3D or face for `TextureCubeMap`
     * @param level = 0 - mipmapLevel
     */
    protected _attachTextureView(attachment: GL, textureView: WEBGLTextureView): void;
}
/**
 * Attachment resize is expected to be a noop if size is same
 *
protected override resizeAttachments(width: number, height: number): this {
  // for default framebuffer, just update the stored size
  if (this.handle === null) {
    // assert(width === undefined && height === undefined);
    this.width = this.gl.drawingBufferWidth;
    this.height = this.gl.drawingBufferHeight;
    return this;
  }

  if (width === undefined) {
    width = this.gl.drawingBufferWidth;
  }
  if (height === undefined) {
    height = this.gl.drawingBufferHeight;
  }

  // TODO Not clear that this is better than default destroy/create implementation

  for (const colorAttachment of this.colorAttachments) {
    colorAttachment.texture.clone({width, height});
  }
  if (this.depthStencilAttachment) {
    this.depthStencilAttachment.texture.resize({width, height});
  }
  return this;
}
*/
//# sourceMappingURL=webgl-framebuffer.d.ts.map