import type { TypedArray } from '@math.gl/types';
import type { Device, Buffer, VertexArrayProps } from '@luma.gl/core';
import { VertexArray } from '@luma.gl/core';
import { GL } from '@luma.gl/constants';
import { WebGLDevice } from "../webgl-device.js";
/** VertexArrayObject wrapper */
export declare class WEBGLVertexArray extends VertexArray {
    get [Symbol.toStringTag](): string;
    readonly device: WebGLDevice;
    readonly handle: WebGLVertexArrayObject;
    /** Attribute 0 buffer constant */
    private buffer;
    private bufferValue;
    /** * Attribute 0 can not be disable on most desktop OpenGL based browsers */
    static isConstantAttributeZeroSupported(device: Device): boolean;
    constructor(device: WebGLDevice, props: VertexArrayProps);
    destroy(): void;
    /**
    // Set (bind/unbind) an elements buffer, for indexed rendering.
    // Must be a Buffer bound to GL.ELEMENT_ARRAY_BUFFER or null. Constants not supported
     *
     * @param elementBuffer
     */
    setIndexBuffer(indexBuffer: Buffer | null): void;
    /** Set a location in vertex attributes array to a buffer, enables the location, sets divisor */
    setBuffer(location: number, attributeBuffer: Buffer): void;
    /** Set a location in vertex attributes array to a constant value, disables the location */
    setConstantWebGL(location: number, value: TypedArray): void;
    bindBeforeRender(): void;
    unbindAfterRender(): void;
    /**
     * Constant attributes need to be reset before every draw call
     * Any attribute that is disabled in the current vertex array object
     * is read from the context's global constant value for that attribute location.
     * @note Constant attributes are only supported in WebGL, not in WebGPU
     */
    protected _applyConstantAttributes(): void;
    /**
     * Set a location in vertex attributes array to a buffer, enables the location, sets divisor
     * @note requires vertex array to be bound
     */
    /** Get an accessor from the  */
    protected _getAccessor(location: number): {
        size: 4 | 2 | 1 | 3;
        type: GL.BYTE | GL.UNSIGNED_BYTE | GL.SHORT | GL.UNSIGNED_SHORT | GL.INT | GL.UNSIGNED_INT | GL.FLOAT | GL.HALF_FLOAT;
        stride: number;
        offset: number;
        normalized: boolean;
        integer: boolean;
        divisor: number;
    };
    /**
     * Enabling an attribute location makes it reference the currently bound buffer
     * Disabling an attribute location makes it reference the global constant value
     * TODO - handle single values for size 1 attributes?
     * TODO - convert classic arrays based on known type?
     */
    protected _enable(location: number, enable?: boolean): void;
    /**
     * Provide a means to create a buffer that is equivalent to a constant.
     * NOTE: Desktop OpenGL cannot disable attribute 0.
     * https://stackoverflow.com/questions/20305231/webgl-warning-attribute-0-is-disabled-
     * this-has-significant-performance-penalty
     */
    getConstantBuffer(elementCount: number, value: TypedArray): Buffer;
}
//# sourceMappingURL=webgl-vertex-array.d.ts.map