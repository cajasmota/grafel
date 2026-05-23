import type { WebGLDevice } from "./webgl-device.js";
import { Adapter, Device, DeviceProps } from '@luma.gl/core';
export declare class WebGLAdapter extends Adapter {
    /** type of device's created by this adapter */
    readonly type: Device['type'];
    constructor();
    /** Force any created WebGL contexts to be WebGL2 contexts, polyfilled with WebGL1 extensions */
    enforceWebGL2(enable: boolean): void;
    /** Check if WebGL 2 is available */
    isSupported(): boolean;
    isDeviceHandle(handle: unknown): boolean;
    /**
     * Get a device instance from a GL context
     * Creates a WebGLCanvasContext against the contexts canvas
     * @note autoResize will be disabled, assuming that whoever created the external context will be handling resizes.
     * @param gl
     * @returns
     */
    attach(gl: Device | WebGL2RenderingContext, props?: DeviceProps): Promise<WebGLDevice>;
    create(props?: DeviceProps): Promise<WebGLDevice>;
}
export declare const webgl2Adapter: WebGLAdapter;
//# sourceMappingURL=webgl-adapter.d.ts.map