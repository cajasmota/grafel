import { DeviceFeature, DeviceFeatures } from '@luma.gl/core';
import { GLExtensions } from '@luma.gl/constants';
/**
 * WebGL extensions exposed as luma.gl features
 * To minimize GL log noise and improve performance, this class ensures that
 * - WebGL extensions are not queried until the corresponding feature is checked.
 * - WebGL extensions are only queried once.
 */
export declare class WebGLDeviceFeatures extends DeviceFeatures {
    protected gl: WebGL2RenderingContext;
    protected extensions: GLExtensions;
    protected testedFeatures: Set<DeviceFeature>;
    constructor(gl: WebGL2RenderingContext, extensions: GLExtensions, disabledFeatures: Partial<Record<DeviceFeature, boolean>>);
    [Symbol.iterator](): IterableIterator<DeviceFeature>;
    has(feature: DeviceFeature): boolean;
    initializeFeatures(): void;
    getFeatures(): DeviceFeature[];
    /** Extract all WebGL features */
    protected getWebGLFeature(feature: DeviceFeature): boolean;
}
//# sourceMappingURL=webgl-device-features.d.ts.map