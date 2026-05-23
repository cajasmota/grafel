import type { Device, TextureViewProps } from '@luma.gl/core';
import { TextureView } from '@luma.gl/core';
import { WebGLDevice } from "../webgl-device.js";
import { WEBGLTexture } from "./webgl-texture.js";
export declare class WEBGLTextureView extends TextureView {
    readonly device: WebGLDevice;
    readonly gl: WebGL2RenderingContext;
    readonly handle: null;
    readonly texture: WEBGLTexture;
    constructor(device: Device, props: TextureViewProps & {
        texture: WEBGLTexture;
    });
}
//# sourceMappingURL=webgl-texture-view.d.ts.map