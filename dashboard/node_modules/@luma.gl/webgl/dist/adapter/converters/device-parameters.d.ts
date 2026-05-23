import type { CompareFunction } from '@luma.gl/core';
import { Device, Parameters } from '@luma.gl/core';
import type { GLFunction, GLParameters } from '@luma.gl/constants';
/**
 * Execute a function with a set of temporary WebGL parameter overrides
 * - Saves current "global" WebGL context settings
 * - Sets the supplies WebGL context parameters,
 * - Executes supplied function
 * - Restores parameters
 * - Returns the return value of the supplied function
 */
export declare function withDeviceAndGLParameters<T = unknown>(device: Device, parameters: Parameters, glParameters: GLParameters, func: (_?: Device) => T): T;
/**
 * Execute a function with a set of temporary WebGL parameter overrides
 * - Saves current "global" WebGL context settings
 * - Sets the supplies WebGL context parameters,
 * - Executes supplied function
 * - Restores parameters
 * - Returns the return value of the supplied function
 * @deprecated use withDeviceParameters instead
 */
export declare function withGLParameters<T = unknown>(device: Device, parameters: GLParameters, func: (_?: Device) => T): T;
/**
 * Execute a function with a set of temporary WebGL parameter overrides
 * - Saves current "global" WebGL context settings
 * - Sets the supplies WebGL context parameters,
 * - Executes supplied function
 * - Restores parameters
 * - Returns the return value of the supplied function
 */
export declare function withDeviceParameters<T = unknown>(device: Device, parameters: Parameters, func: (_?: Device) => T): T;
/** Set WebGPU Style Parameters */
export declare function setDeviceParameters(device: Device, parameters: Parameters): void;
export declare function convertCompareFunction(parameter: string, value: CompareFunction): GLFunction;
export declare function convertToCompareFunction(parameter: string, value: GLFunction): CompareFunction;
//# sourceMappingURL=device-parameters.d.ts.map