import type { CanvasContextProps } from '@luma.gl/core';
import { CanvasContext } from '@luma.gl/core';
import { WebGLDevice } from "./webgl-device.js";
import { WEBGLFramebuffer } from "./resources/webgl-framebuffer.js";
/**
 * A WebGL Canvas Context which manages the canvas and handles drawing buffer resizing etc
 */
export declare class WebGLCanvasContext extends CanvasContext {
    readonly device: WebGLDevice;
    readonly handle: unknown;
    private _framebuffer;
    get [Symbol.toStringTag](): string;
    constructor(device: WebGLDevice, props: CanvasContextProps);
    getCurrentFramebuffer(): WEBGLFramebuffer;
    _updateDevice(): void;
}
//# sourceMappingURL=webgl-canvas-context.d.ts.map