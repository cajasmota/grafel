import { Device, Framebuffer } from '@luma.gl/core';
import { ShaderInputs } from "../../shader-inputs.js";
import { pickingUniforms } from "./picking-uniforms.js";
/** Information about picked object */
export type PickInfo = {
    batchIndex: number | null;
    objectIndex: number | null;
};
export type PickingManagerProps = {
    /** Shader Inputs from models to pick */
    shaderInputs?: ShaderInputs<{
        picking: typeof pickingUniforms.props;
    }>;
    /** Callback */
    onObjectPicked?: (info: PickInfo) => void;
};
/**
 * Helper class for using the new picking module
 * @todo Port to WebGPU
 * @todo Support multiple models
 * @todo Switching picking module
 */
export declare class PickingManager {
    device: Device;
    props: Required<PickingManagerProps>;
    /** Info from latest pick operation */
    pickInfo: PickInfo;
    /** Framebuffer used for picking */
    framebuffer: Framebuffer | null;
    static defaultProps: Required<PickingManagerProps>;
    constructor(device: Device, props: PickingManagerProps);
    destroy(): void;
    getFramebuffer(): Framebuffer;
    /** Clear highlighted / picked object */
    clearPickState(): void;
    /** Prepare for rendering picking colors */
    beginRenderPass(): import("@luma.gl/core").RenderPass;
    updatePickInfo(mousePosition: [number, number]): Promise<PickInfo | null>;
    /**
     * Get pick position in device pixel range
     * use the center pixel location in device pixel range
     */
    getPickPosition(mousePosition: [number, number]): [number, number];
}
//# sourceMappingURL=picking-manager.d.ts.map