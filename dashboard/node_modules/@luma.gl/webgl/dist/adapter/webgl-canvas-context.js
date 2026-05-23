// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { CanvasContext } from '@luma.gl/core';
import { WEBGLFramebuffer } from "./resources/webgl-framebuffer.js";
/**
 * A WebGL Canvas Context which manages the canvas and handles drawing buffer resizing etc
 */
export class WebGLCanvasContext extends CanvasContext {
    device;
    handle = null;
    _framebuffer = null;
    get [Symbol.toStringTag]() {
        return 'WebGLCanvasContext';
    }
    constructor(device, props) {
        // Note: Base class creates / looks up the canvas (unless under Node.js)
        super(props);
        this.device = device;
        // Base class constructor cannot access derived methods/fields, so we need to call these functions in the subclass constructor
        this._setAutoCreatedCanvasId(`${this.device.id}-canvas`);
        this._updateDevice();
    }
    getCurrentFramebuffer() {
        // Setting handle to null returns a reference to the default framebuffer
        this._framebuffer = this._framebuffer || new WEBGLFramebuffer(this.device, { handle: null });
        return this._framebuffer;
    }
    // IMPLEMENTATION OF ABSTRACT METHODS
    _updateDevice() { }
}
//# sourceMappingURL=webgl-canvas-context.js.map