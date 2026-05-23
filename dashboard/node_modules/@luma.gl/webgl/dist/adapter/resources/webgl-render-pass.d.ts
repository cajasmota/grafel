import { NumericArray } from '@math.gl/types';
import { RenderPass, RenderPassProps, RenderPassParameters } from '@luma.gl/core';
import { WebGLDevice } from "../webgl-device.js";
import { GLParameters } from '@luma.gl/constants';
export declare class WEBGLRenderPass extends RenderPass {
    readonly device: WebGLDevice;
    readonly handle: null;
    /** Parameters that should be applied before each draw call */
    glParameters: GLParameters;
    constructor(device: WebGLDevice, props: RenderPassProps);
    end(): void;
    pushDebugGroup(groupLabel: string): void;
    popDebugGroup(): void;
    insertDebugMarker(markerLabel: string): void;
    /**
     * Maps RenderPass parameters to GL parameters
     */
    setParameters(parameters?: RenderPassParameters): void;
    beginOcclusionQuery(queryIndex: number): void;
    endOcclusionQuery(): void;
    /**
     * Optionally clears depth, color and stencil buffers based on parameters
     */
    protected clear(): void;
    /**
     * WebGL2 - clear a specific color buffer
     */
    protected clearColorBuffer(drawBuffer?: number, value?: NumericArray): void;
}
//# sourceMappingURL=webgl-render-pass.d.ts.map