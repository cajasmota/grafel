/**
 * Return a boolean for a single bit in a bitmap.
 * @param {Uint8Array} bitmap The bitmap.
 * @param {number} index The bit index to read.
 * @returns {boolean} The boolean bitmap value.
 */
export function decodeBit(bitmap: Uint8Array, index: number): boolean;
/**
 * Lookup helper for flatbuffer object (table) entries.
 * @param {Uint8Array} buf The byte buffer.
 * @param {number} index The base index of the object.
 */
export function readObject(buf: Uint8Array, index: number): (index: number, read: (buf: Uint8Array, offset: number) => T, fallback?: T) => T;
/**
 * Return a buffer offset value.
 * @param {Uint8Array} buf
 * @param {number} offset
 * @returns {number}
 */
export function readOffset(buf: Uint8Array, offset: number): number;
/**
 * Return a boolean value.
 * @param {Uint8Array} buf
 * @param {number} offset
 * @returns {boolean}
 */
export function readBoolean(buf: Uint8Array, offset: number): boolean;
/**
 * Return a signed 8-bit integer value.
 * @param {Uint8Array} buf
 * @param {number} offset
 * @returns {number}
 */
export function readInt8(buf: Uint8Array, offset: number): number;
/**
 * Return an unsigned 8-bit integer value.
 * @param {Uint8Array} buf
 * @param {number} offset
 * @returns {number}
 */
export function readUint8(buf: Uint8Array, offset: number): number;
/**
 * Return a signed 16-bit integer value.
 * @param {Uint8Array} buf
 * @param {number} offset
 * @returns {number}
 */
export function readInt16(buf: Uint8Array, offset: number): number;
/**
 * Return an unsigned 16-bit integer value.
 * @param {Uint8Array} buf
 * @param {number} offset
 * @returns {number}
 */
export function readUint16(buf: Uint8Array, offset: number): number;
/**
 * Return a signed 32-bit integer value.
 * @param {Uint8Array} buf
 * @param {number} offset
 * @returns {number}
 */
export function readInt32(buf: Uint8Array, offset: number): number;
/**
 * Return an unsigned 32-bit integer value.
 * @param {Uint8Array} buf
 * @param {number} offset
 * @returns {number}
 */
export function readUint32(buf: Uint8Array, offset: number): number;
/**
 * Return a signed 64-bit integer value coerced to a JS number.
 * Throws an error if the value exceeds what a JS number can represent.
 * @param {Uint8Array} buf
 * @param {number} offset
 * @returns {number}
 */
export function readInt64(buf: Uint8Array, offset: number): number;
/**
 * Create a JavaScript string from UTF-8 data stored inside the FlatBuffer.
 * This allocates a new string and converts to wide chars upon each access.
 * @param {Uint8Array} buf The byte buffer.
 * @param {number} index The index of the string entry.
 * @returns {string} The decoded string.
 */
export function readString(buf: Uint8Array, index: number): string;
/**
 * Extract a flatbuffer vector to an array.
 * @template T
 * @param {Uint8Array} buf The byte buffer.
 * @param {number} offset The offset location of the vector.
 * @param {number} stride The stride between vector entries.
 * @param {(buf: Uint8Array, pos: number) => T} extract Vector entry extraction function.
 * @returns {T[]} The extracted vector entries.
 */
export function readVector<T>(buf: Uint8Array, offset: number, stride: number, extract: (buf: Uint8Array, pos: number) => T): T[];
/** The size in bytes of a 32-bit integer. */
export const SIZEOF_INT: 4;
/** The size in bytes of a 16-bit integer. */
export const SIZEOF_SHORT: 2;
