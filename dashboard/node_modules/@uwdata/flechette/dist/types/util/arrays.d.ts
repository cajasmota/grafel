/**
 * Check if an input value is an ArrayBuffer or SharedArrayBuffer,
 * applicable cross-realms.
 * @param {unknown} data
 * @returns {data is ArrayBufferLike}
 */
export function isArrayBufferLike(data: unknown): data is ArrayBufferLike;
/**
 * Check in an input value is a Uint8Array, applicable cross-realms.
 * @param {unknown} value
 * @returns {value is Uint8Array}
 */
export function isUint8Array(value: unknown): value is Uint8Array;
/**
 * Return the appropriate typed array constructor for the given
 * integer type metadata.
 * @param {number} bitWidth The integer size in bits.
 * @param {boolean} signed Flag indicating if the integer is signed.
 * @returns {IntArrayConstructor}
 */
export function intArrayType(bitWidth: number, signed: boolean): IntArrayConstructor;
/**
 * Check if a value is a typed array, applicable cross-realms.
 * @param {unknown} value The value to check.
 * @returns {value is TypedArray}
 *  True if value is a typed array, false otherwise.
 */
export function isTypedArray(value: unknown): value is TypedArray;
/**
 * Check if a value is either a standard array or typed array.
 * @param {*} value The value to check.
 * @returns {value is (Array | TypedArray)}
 *  True if value is an array, false otherwise.
 */
export function isArray(value: any): value is (any[] | TypedArray);
/**
 * Check if a value is an array type (constructor) for 64-bit integers,
 * one of BigInt64Array or BigUint64Array.
 * @param {*} value The value to check.
 * @returns {value is Int64ArrayConstructor}
 *  True if value is a 64-bit array type, false otherwise.
 */
export function isInt64ArrayType(value: any): value is Int64ArrayConstructor;
/**
 * Determine the correct index into an offset array for a given
 * full column row index. Assumes offset indices can be manipulated
 * as 32-bit signed integers.
 * @param {IntegerArray} offsets The offsets array.
 * @param {number} index The full column row index.
 */
export function bisect(offsets: IntegerArray, index: number): number;
/**
 * Return a 64-bit aligned version of the array.
 * @template {TypedArray} T
 * @param {T} array The array.
 * @param {number} length The current array length.
 * @returns {T} The aligned array.
 */
export function align<T extends TypedArray>(array: T, length?: number): T;
/**
 * Resize a typed array to exactly the specified length.
 * @template {TypedArray} T
 * @param {T} array The array.
 * @param {number} newLength The new length.
 * @param {number} [offset] The offset at which to copy the old array.
 * @returns {T} The resized array.
 */
export function resize<T extends TypedArray>(array: T, newLength: number, offset?: number): T;
/**
 * Grow a typed array to accommdate a minimum index. The array size is
 * doubled until it exceeds the minimum index.
 * @template {TypedArray} T
 * @param {T} array The array.
 * @param {number} index The minimum index.
 * @param {boolean} [shift] Flag to shift copied bytes to back of array.
 * @returns {T} The resized array.
 */
export function grow<T extends TypedArray>(array: T, index: number, shift?: boolean): T;
/**
 * @import { Int64ArrayConstructor, IntArrayConstructor, IntegerArray, TypedArray } from '../types.js'
 */
export const uint8Array: Uint8ArrayConstructor;
export const uint16Array: Uint16ArrayConstructor;
export const uint32Array: Uint32ArrayConstructor;
export const uint64Array: BigUint64ArrayConstructor;
export const int8Array: Int8ArrayConstructor;
export const int16Array: Int16ArrayConstructor;
export const int32Array: Int32ArrayConstructor;
export const int64Array: BigInt64ArrayConstructor;
export const float32Array: Float32ArrayConstructor;
export const float64Array: Float64ArrayConstructor;
import type { IntArrayConstructor } from '../types.js';
import type { TypedArray } from '../types.js';
import type { Int64ArrayConstructor } from '../types.js';
import type { IntegerArray } from '../types.js';
