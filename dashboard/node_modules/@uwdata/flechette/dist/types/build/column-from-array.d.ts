/**
 * Create a new column from a provided data array.
 * @template T
 * @param {Array | TypedArray} array The input data.
 * @param {DataType} [type] The data type.
 *  If not specified, type inference is attempted.
 * @param {ColumnBuilderOptions} [options]
 *  Builder options for the generated column.
 * @param {ReturnType<dictionaryContext>} [dicts]
 *  Builder context object, for internal use only.
 * @returns {Column<T>} The generated column.
 */
export function columnFromArray<T>(array: any[] | TypedArray, type?: DataType, options?: ColumnBuilderOptions, dicts?: ReturnType<typeof dictionaryContext>): Column<T>;
import type { TypedArray } from '../types.js';
import type { DataType } from '../types.js';
import type { ColumnBuilderOptions } from '../types.js';
import type { dictionaryContext } from './builders/dictionary.js';
import { Column } from '../column.js';
