// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { RenderPass } from '@luma.gl/core';
import { GL } from '@luma.gl/constants';
import { withGLParameters } from "../../context/state-tracker/with-parameters.js";
import { setGLParameters } from "../../context/parameters/unified-parameter-api.js";
const COLOR_CHANNELS = [0x1, 0x2, 0x4, 0x8]; // GPUColorWrite RED, GREEN, BLUE, ALPHA
export class WEBGLRenderPass extends RenderPass {
    device;
    handle = null;
    /** Parameters that should be applied before each draw call */
    glParameters = {};
    constructor(device, props) {
        super(device, props);
        this.device = device;
        // If no viewport is provided, apply reasonably defaults
        let viewport;
        if (!props?.parameters?.viewport) {
            if (props?.framebuffer) {
                // Set the viewport to the size of the framebuffer
                const { width, height } = props.framebuffer;
                viewport = [0, 0, width, height];
            }
            else {
                // Instead of using our own book-keeping, we can just read the values from the WebGL context
                const [width, height] = device.getDefaultCanvasContext().getDrawingBufferSize();
                viewport = [0, 0, width, height];
            }
        }
        // TODO - do parameters (scissorRect) affect the clear operation?
        this.device.pushState();
        this.setParameters({ viewport, ...this.props.parameters });
        // Specify mapping of draw buffer locations to color attachments
        const webglFramebuffer = this.props.framebuffer;
        // Default framebuffers can only be set to GL.BACK or GL.NONE
        if (this.props.framebuffer && webglFramebuffer?.handle) {
            const drawBuffers = this.props.framebuffer.colorAttachments.map((_, i) => 36064 + i);
            this.device.gl.drawBuffers(drawBuffers);
        }
        else {
            this.device.gl.drawBuffers([1029]);
        }
        // Hack - for now WebGL draws in "immediate mode" (instead of queueing the operations)...
        this.clear();
    }
    end() {
        this.device.popState();
        // should add commands to CommandEncoder.
    }
    pushDebugGroup(groupLabel) { }
    popDebugGroup() { }
    insertDebugMarker(markerLabel) { }
    // beginOcclusionQuery(queryIndex: number): void;
    // endOcclusionQuery(): void;
    // executeBundles(bundles: Iterable<GPURenderBundle>): void;
    /**
     * Maps RenderPass parameters to GL parameters
     */
    setParameters(parameters = {}) {
        const glParameters = { ...this.glParameters };
        // Framebuffers are specified using parameters in WebGL
        glParameters.framebuffer = this.props.framebuffer || null;
        if (this.props.depthReadOnly) {
            glParameters.depthMask = !this.props.depthReadOnly;
        }
        glParameters.stencilMask = this.props.stencilReadOnly ? 0 : 1;
        glParameters[35977] = this.props.discard;
        // Map the four renderpass parameters to WebGL parameters
        if (parameters.viewport) {
            // WebGPU viewports are 6 coordinates (X, Y, Z)
            if (parameters.viewport.length >= 6) {
                glParameters.viewport = parameters.viewport.slice(0, 4);
                glParameters.depthRange = [
                    parameters.viewport[4],
                    parameters.viewport[5]
                ];
            }
            else {
                // WebGL viewports are 4 coordinates (X, Y)
                glParameters.viewport = parameters.viewport;
            }
        }
        if (parameters.scissorRect) {
            glParameters.scissorTest = true;
            glParameters.scissor = parameters.scissorRect;
        }
        if (parameters.blendConstant) {
            glParameters.blendColor = parameters.blendConstant;
        }
        if (parameters.stencilReference) {
            // eslint-disable-next-line no-console
            console.warn('RenderPassParameters.stencilReference not yet implemented in WebGL');
            // parameters.stencilFunc = [func, ref, mask];
            // Does this work?
            glParameters[2967] = parameters.stencilReference;
        }
        if ('colorMask' in parameters) {
            glParameters.colorMask = COLOR_CHANNELS.map(channel => Boolean(channel & parameters.colorMask));
        }
        this.glParameters = glParameters;
        setGLParameters(this.device.gl, glParameters);
    }
    beginOcclusionQuery(queryIndex) {
        const webglQuerySet = this.props.occlusionQuerySet;
        webglQuerySet?.beginOcclusionQuery();
    }
    endOcclusionQuery() {
        const webglQuerySet = this.props.occlusionQuerySet;
        webglQuerySet?.endOcclusionQuery();
    }
    // PRIVATE
    /**
     * Optionally clears depth, color and stencil buffers based on parameters
     */
    clear() {
        const glParameters = { ...this.glParameters };
        let clearMask = 0;
        if (this.props.clearColors) {
            this.props.clearColors.forEach((color, drawBufferIndex) => {
                if (color) {
                    this.clearColorBuffer(drawBufferIndex, color);
                }
            });
        }
        if (this.props.clearColor !== false && this.props.clearColors === undefined) {
            clearMask |= 16384;
            glParameters.clearColor = this.props.clearColor;
        }
        if (this.props.clearDepth !== false) {
            clearMask |= 256;
            glParameters.clearDepth = this.props.clearDepth;
        }
        if (this.props.clearStencil !== false) {
            clearMask |= 1024;
            glParameters.clearStencil = this.props.clearStencil;
        }
        if (clearMask !== 0) {
            // Temporarily set any clear "colors" and call clear
            withGLParameters(this.device.gl, glParameters, () => {
                this.device.gl.clear(clearMask);
            });
        }
    }
    /**
     * WebGL2 - clear a specific color buffer
     */
    clearColorBuffer(drawBuffer = 0, value = [0, 0, 0, 0]) {
        withGLParameters(this.device.gl, { framebuffer: this.props.framebuffer }, () => {
            // Method selection per OpenGL ES 3 docs
            switch (value.constructor) {
                case Int8Array:
                case Int16Array:
                case Int32Array:
                    this.device.gl.clearBufferiv(6144, drawBuffer, value);
                    break;
                case Uint8Array:
                case Uint8ClampedArray:
                case Uint16Array:
                case Uint32Array:
                    this.device.gl.clearBufferuiv(6144, drawBuffer, value);
                    break;
                case Float32Array:
                    this.device.gl.clearBufferfv(6144, drawBuffer, value);
                    break;
                default:
                    throw new Error('clearColorBuffer: color must be typed array');
            }
        });
    }
}
//# sourceMappingURL=webgl-render-pass.js.map