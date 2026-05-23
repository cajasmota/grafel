import { Device, Framebuffer } from '@luma.gl/core';
import { picking } from '@luma.gl/shadertools';
import { ShaderInputs } from "../../shader-inputs.js";
/**
 * Helper class for using the legacy picking module
 */
export declare class LegacyPickingManager {
    device: Device;
    framebuffer: Framebuffer | null;
    shaderInputs: ShaderInputs<{
        picking: typeof picking.props;
    }>;
    constructor(device: Device, shaderInputs: ShaderInputs);
    destroy(): void;
    getFramebuffer(): Framebuffer;
    /** Clear highlighted / picked object */
    clearPickState(): void;
    /** Prepare for rendering picking colors */
    beginRenderPass(): import("@luma.gl/core").RenderPass;
    updatePickState(mousePosition: [number, number]): void;
    /**
     * Get pick position in device pixel range
     * use the center pixel location in device pixel range
     */
    getPickPosition(mousePosition: [number, number]): [number, number];
}
//# sourceMappingURL=legacy-picking-manager.d.ts.map