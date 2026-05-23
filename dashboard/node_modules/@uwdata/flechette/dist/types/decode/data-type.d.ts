/**
 * Decode a data type definition for a field.
 * @param {Uint8Array} buf A byte buffer of binary Arrow IPC data.
 * @param {number} index The starting index in the byte buffer.
 * @param {number} typeId The data type id.
 * @param {Field[]} [children] A list of parsed child fields.
 * @returns {DataType} The data type.
 */
export function decodeDataType(buf: Uint8Array, index: number, typeId: number, children?: Field[]): DataType;
import type { Field } from '../types.js';
import type { DataType } from '../types.js';
