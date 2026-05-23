import type { BufferMapCallback, BufferProps } from '@luma.gl/core';
import { Buffer } from '@luma.gl/core';
import { GL } from '@luma.gl/constants';
import { WebGLDevice } from "../webgl-device.js";
/** WebGL Buffer interface */
export declare class WEBGLBuffer extends Buffer {
    readonly device: WebGLDevice;
    readonly gl: WebGL2RenderingContext;
    readonly handle: WebGLBuffer;
    /** Target in OpenGL defines the type of buffer */
    readonly glTarget: GL.ARRAY_BUFFER | GL.ELEMENT_ARRAY_BUFFER | GL.UNIFORM_BUFFER;
    /** Usage is a hint on how frequently the buffer will be updates */
    readonly glUsage: GL.STATIC_DRAW | GL.DYNAMIC_DRAW;
    /** Index type is needed when issuing draw calls, so we pre-compute it */
    readonly glIndexType: GL.UNSIGNED_SHORT | GL.UNSIGNED_INT;
    /** Number of bytes allocated on the GPU for this buffer */
    byteLength: number;
    /** Number of bytes used */
    bytesUsed: number;
    constructor(device: WebGLDevice, props?: BufferProps);
    destroy(): void;
    /** Allocate a new buffer and initialize to contents of typed array */
    _initWithData(data: ArrayBuffer | ArrayBufferView, byteOffset?: number, byteLength?: number): void;
    _initWithByteLength(byteLength: number): this;
    write(data: ArrayBufferLike | ArrayBufferView, byteOffset?: number): void;
    mapAndWriteAsync(callback: BufferMapCallback<void>, byteOffset?: number, byteLength?: number): Promise<void>;
    readAsync(byteOffset?: number, byteLength?: number): Promise<Uint8Array<ArrayBuffer>>;
    mapAndReadAsync<T>(callback: BufferMapCallback<T>, byteOffset?: number, byteLength?: number): Promise<T>;
    readSyncWebGL(byteOffset?: number, byteLength?: number): Uint8Array<ArrayBuffer>;
}
//# sourceMappingURL=webgl-buffer.d.ts.map