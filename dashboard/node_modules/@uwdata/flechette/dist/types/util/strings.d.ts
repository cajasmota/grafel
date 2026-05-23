/**
 * Return a UTF-8 string decoded from a byte buffer.
 * @param {Uint8Array} buf The byte buffer.
 * @returns {string} The decoded string.
 */
export function decodeUtf8(buf: Uint8Array): string;
/**
 * Return a byte buffer encoded from a UTF-8 string.
 * @param {string } str The string to encode.
 * @returns {Uint8Array} The encoded byte buffer.
 */
export function encodeUtf8(str: string): Uint8Array;
/**
 * Return a string-coercible key value that uniquely identifies a value.
 * @param {*} value The input value.
 * @returns {string} The key string.
 */
export function keyString(value: any): string;
