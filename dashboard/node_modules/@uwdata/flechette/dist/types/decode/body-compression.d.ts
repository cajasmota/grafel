/**
 * Decode record batch body compression metadata.
 * @param {Uint8Array} buf A byte buffer of binary Arrow IPC data
 * @param {number} index The starting index in the byte buffer
 * @returns {BodyCompression | undefined} The body compression metadata
 */
export function decodeBodyCompression(buf: Uint8Array, index: number): BodyCompression | undefined;
import type { BodyCompression } from '../types.js';
