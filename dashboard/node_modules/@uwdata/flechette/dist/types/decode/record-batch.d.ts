/**
 * Decode a record batch.
 * @param {Uint8Array} buf A byte buffer of binary Arrow IPC data
 * @param {number} index The starting index in the byte buffer
 * @param {Version_} version Arrow version value
 * @returns {RecordBatch} The record batch
 */
export function decodeRecordBatch(buf: Uint8Array, index: number, version: Version_): RecordBatch;
import type { Version_ } from '../types.js';
import type { RecordBatch } from '../types.js';
