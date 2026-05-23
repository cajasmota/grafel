import type { Device } from "../device.js";
import { Resource, ResourceProps } from "./resource.js";
/** Callback for Buffer.mapAndReadAsync */
export type BufferMapCallback<T> = (arrayBuffer: ArrayBuffer, lifetime: 'mapped' | 'copied') => T;
export type BufferProps = ResourceProps & {
    /** Supply a handle to connect to an existing device-specific buffer */
    handle?: WebGLBuffer;
    /** Specifies how this buffer can be used */
    usage?: number;
    /** Length in bytes of memory to be allocated. If not specified, `byteLength` of  `props.data` will be used. */
    byteLength?: number;
    /** Byte offset into the newly created Buffer to store data at */
    byteOffset?: number;
    /** If props.usage includes Buffer.INDEX. Note: uint8 indices are automatically converted to uint16 for WebGPU compatibility */
    indexType?: 'uint8' | 'uint16' | 'uint32';
    /** Data to initialize the buffer with. */
    data?: ArrayBuffer | ArrayBufferView | null;
    /** Callback to initialize data without copy */
    onMapped?: BufferMapCallback<void>;
};
/** Abstract GPU buffer */
export declare abstract class Buffer extends Resource<BufferProps> {
    /** Index buffer */
    static INDEX: number;
    /** Vertex buffer */
    static VERTEX: number;
    /** Uniform buffer */
    static UNIFORM: number;
    /** Storage buffer */
    static STORAGE: number;
    static INDIRECT: number;
    static QUERY_RESOLVE: number;
    static MAP_READ: number;
    static MAP_WRITE: number;
    static COPY_SRC: number;
    static COPY_DST: number;
    get [Symbol.toStringTag](): string;
    /** The usage with which this buffer was created */
    readonly usage: number;
    /** For index buffers, whether indices are 8, 16 or 32 bit. Note: uint8 indices are automatically converted to uint16 for WebGPU compatibility */
    readonly indexType?: 'uint8' | 'uint16' | 'uint32';
    /** Length of buffer in bytes */
    abstract byteLength: number;
    /** "Time" of last update, can be used to check if redraw is needed */
    updateTimestamp: number;
    constructor(device: Device, props: BufferProps);
    /**
     * Create a copy of this Buffer with new byteLength, with same props but of the specified size.
     * @note Does not copy contents of the cloned Buffer.
     */
    clone(props: {
        byteLength: number;
    }): Buffer;
    /** Write data to buffer */
    abstract write(data: ArrayBufferLike | ArrayBufferView | SharedArrayBuffer, byteOffset?: number): void;
    abstract mapAndWriteAsync(onMapped: BufferMapCallback<void | Promise<void>>, byteOffset?: number, byteLength?: number): Promise<void>;
    /** Reads data asynchronously, returns a copy of the buffer data */
    abstract readAsync(byteOffset?: number, byteLength?: number): Promise<Uint8Array>;
    /** Maps buffer data to CPU memory. Mapped memory is only accessible in the callback */
    abstract mapAndReadAsync<T>(onMapped: BufferMapCallback<T>, byteOffset?: number, byteLength?: number): Promise<T>;
    /** Read data synchronously. @note WebGL2 only */
    abstract readSyncWebGL(byteOffset?: number, byteLength?: number): Uint8Array;
    /** Max amount of debug data saved. Two vec4's */
    static DEBUG_DATA_MAX_LENGTH: number;
    /** A partial CPU-side copy of the data in this buffer, for debugging purposes */
    debugData: ArrayBuffer;
    /** This doesn't handle partial non-zero offset updates correctly */
    protected _setDebugData(data: ArrayBufferView | ArrayBufferLike | null, byteOffset: number, byteLength: number): void;
    static defaultProps: Required<BufferProps>;
}
//# sourceMappingURL=buffer.d.ts.map