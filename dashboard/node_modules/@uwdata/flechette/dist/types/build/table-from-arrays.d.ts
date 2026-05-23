/**
 * Create a new table from the provided arrays.
 * @param {[string, Array | TypedArray][]
 *  | Record<string, Array | TypedArray>} data
 *  The input data as a collection of named arrays.
 * @param {TableBuilderOptions} options
 *  Table builder options, including an optional type map.
 * @returns {Table} The new table.
 */
export function tableFromArrays(data: [string, any[] | TypedArray][] | Record<string, any[] | TypedArray>, options?: TableBuilderOptions): Table;
import type { TypedArray } from '../types.js';
import type { TableBuilderOptions } from '../types.js';
import type { Table } from '../table.js';
