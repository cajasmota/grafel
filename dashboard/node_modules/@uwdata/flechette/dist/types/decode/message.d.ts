/**
 * A "message" contains a block of Apache Arrow data, such as a schema,
 * record batch, or dictionary batch. This message decodes a single
 * message, returning its associated metadata and content.
 * @param {Uint8Array} buf A byte buffer of binary Arrow IPC data
 * @param {number} index The starting index in the byte buffer
 * @returns {Message} The decoded message.
 */
export function decodeMessage(buf: Uint8Array, index: number): Message;
import type { Message } from '../types.js';
