import type { Log } from '@probe.gl/log';
import type { DeviceProps } from "./device.js";
import { Device } from "./device.js";
import { Adapter } from "./adapter.js";
import { StatsManager } from "../utils/stats-manager.js";
declare global {
    var luma: Luma;
}
/** Properties for creating a new device */
export type CreateDeviceProps = {
    /** Selects the type of device. `best-available` uses webgpu if available, then webgl. */
    type?: 'webgl' | 'webgpu' | 'null' | 'unknown' | 'best-available';
    /** List of adapters. Will also search any pre-registered adapters */
    adapters?: Adapter[];
    /**
     * Whether to wait for page to be loaded so that CanvasContext's can access the DOM.
     * The browser only supports one 'load' event listener so it may be necessary for the application to set this to false to avoid conflicts.
     */
    waitForPageLoad?: boolean;
} & DeviceProps;
/** Properties for attaching an existing WebGL context or WebGPU device to a new luma Device */
export type AttachDeviceProps = {
    /** List of adapters. Will also search any pre-registered adapters */
    adapters?: Adapter[];
} & DeviceProps;
/**
 * Entry point to the luma.gl GPU abstraction
 * Register WebGPU and/or WebGL adapters (controls application bundle size)
 * Run-time selection of the first available Device
 */
export declare class Luma {
    static defaultProps: Required<CreateDeviceProps>;
    /** Global stats for all devices */
    readonly stats: StatsManager;
    /**
     * Global log
     *
     * Assign luma.log.level in console to control logging: \
     * 0: none, 1: minimal, 2: verbose, 3: attribute/uniforms, 4: gl logs
     * luma.log.break[], set to gl funcs, luma.log.profile[] set to model names`;
     */
    readonly log: Log;
    /** Version of luma.gl */
    readonly VERSION: string;
    spector: unknown;
    protected preregisteredAdapters: Map<string, Adapter>;
    constructor();
    /** Creates a device. Asynchronously. */
    createDevice(props_?: CreateDeviceProps): Promise<Device>;
    /**
     * Attach to an existing GPU API handle (WebGL2RenderingContext or GPUDevice).
     * @param handle Externally created WebGL context or WebGPU device
     */
    attachDevice(handle: unknown, props: AttachDeviceProps): Promise<Device>;
    /**
     * Global adapter registration.
     * @deprecated Use props.adapters instead
     */
    registerAdapters(adapters: Adapter[]): void;
    /** Get type strings for supported Devices */
    getSupportedAdapters(adapters?: Adapter[]): string[];
    /** Get type strings for best available Device */
    getBestAvailableAdapterType(adapters?: Adapter[]): 'webgpu' | 'webgl' | 'null' | null;
    /** Select adapter of type from registered adapters */
    selectAdapter(type: string, adapters?: Adapter[]): Adapter | null;
    /**
     * Override `HTMLCanvasContext.getCanvas()` to always create WebGL2 contexts with additional WebGL1 compatibility.
     * Useful when attaching luma to a context from an external library does not support creating WebGL2 contexts.
     */
    enforceWebGL2(enforce?: boolean, adapters?: Adapter[]): void;
    /** @deprecated */
    setDefaultDeviceProps(props: CreateDeviceProps): void;
    /** Convert a list of adapters to a map */
    protected _getAdapterMap(adapters?: Adapter[]): Map<string, Adapter>;
    /** Get type of a handle (for attachDevice) */
    protected _getTypeFromHandle(handle: unknown, adapters?: Adapter[]): 'webgpu' | 'webgl' | 'null' | null;
}
/**
 * Entry point to the luma.gl GPU abstraction
 * Register WebGPU and/or WebGL adapters (controls application bundle size)
 * Run-time selection of the first available Device
 */
export declare const luma: Luma;
//# sourceMappingURL=luma.d.ts.map