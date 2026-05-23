/**
 * Create a new resizable buffer instance.
 * @param {TypedArrayConstructor} [arrayType]
 *  The array type.
 * @returns {Buffer} The buffer.
 */
export function buffer(arrayType?: TypedArrayConstructor): Buffer;
/**
 * Create a new resizable bitmap instance.
 * @returns {Bitmap} The bitmap buffer.
 */
export function bitmap(): Bitmap;
/**
 * Resizable byte buffer.
 */
export class Buffer {
    /**
     * Create a new resizable buffer instance.
     * @param {TypedArrayConstructor} arrayType
     */
    constructor(arrayType?: TypedArrayConstructor);
    buf: Uint8Array<ArrayBuffer> | Float64Array<ArrayBuffer> | BigInt64Array<ArrayBuffer> | Uint32Array<ArrayBuffer> | Int32Array<ArrayBuffer> | Uint16Array<ArrayBuffer> | Int8Array<ArrayBuffer> | Int16Array<ArrayBuffer> | BigUint64Array<ArrayBuffer> | Float32Array<ArrayBuffer>;
    /**
     * Return the underlying data as a 64-bit aligned array of minimum size.
     * @param {number} size The desired minimum array size.
     * @returns {TypedArray} The 64-bit aligned array.
     */
    array(size: number): TypedArray;
    /**
     * Prepare for writes to the given index, resizing as necessary.
     * @param {number} index The array index to prepare to write to.
     */
    prep(index: number): void;
    /**
     * Return the value at the given index.
     * @param {number} index The array index.
     */
    get(index: number): number | bigint;
    /**
     * Set a value at the given index.
     * @param {number | bigint} value The value to set.
     * @param {number} index The index to write to.
     */
    set(value: number | bigint, index: number): void;
    /**
     * Write a byte array at the given index. The method should be called
     * only when the underlying buffer is of type Uint8Array.
     * @param {Uint8Array} bytes The byte array.
     * @param {number} index The starting index to write to.
     */
    write(bytes: Uint8Array, index: number): void;
}
/**
 * Resizable bitmap buffer.
 */
export class Bitmap extends Buffer {
    /**
     * Set a bit to true at the given bitmap index.
     * @param {number} index The index to write to.
     */
    set(index: number): void;
}
import type { TypedArrayConstructor } from '../types.js';
import type { TypedArray } from '../types.js';
