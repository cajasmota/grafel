/**
 * Infer the data type for a given input array.
 * @param {(visitor: (value: any) => void) => void} visit
 *  A function that applies a callback to successive data values.
 * @returns {DataType} The data type.
 */
export function inferType(visit: (visitor: (value: any) => void) => void): DataType;
import type { DataType } from '../types.js';
