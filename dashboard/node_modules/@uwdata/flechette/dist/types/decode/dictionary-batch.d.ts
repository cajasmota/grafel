/**
 * Decode a dictionary batch.
 * @param {Uint8Array} buf A byte buffer of binary Arrow IPC data
 * @param {number} index The starting index in the byte buffer
 * @param {Version_} version Arrow version value
 * @returns {DictionaryBatch} The dictionary batch
 */
export function decodeDictionaryBatch(buf: Uint8Array, index: number, version: Version_): DictionaryBatch;
import type { Version_ } from '../types.js';
import type { DictionaryBatch } from '../types.js';
