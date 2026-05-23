/**
 * Create a new column by iterating over provided values.
 * @template T
 * @param {Iterable | ((callback: (value: any) => void) => void)} values
 *  Either an iterable object or a visitor function that applies a callback
 *  to successive data values (akin to Array.forEach).
 * @param {DataType} [type] The data type.
 * @param {ColumnBuilderOptions} [options]
 *  Builder options for the generated column.
 * @param {ReturnType<dictionaryContext>} [dicts]
 *  Dictionary context object, for internal use only.
 * @returns {Column<T>} The generated column.
 */
export function columnFromValues<T>(values: Iterable<any> | ((callback: (value: any) => void) => void), type?: DataType, options?: ColumnBuilderOptions, dicts?: ReturnType<typeof dictionaryContext>): Column<T>;
import type { DataType } from '../types.js';
import type { ColumnBuilderOptions } from '../types.js';
import type { dictionaryContext } from './builders/dictionary.js';
import { Column } from '../column.js';
