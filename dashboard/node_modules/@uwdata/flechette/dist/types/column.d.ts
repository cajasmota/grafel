/**
 * Build up a column from batches.
 */
export function columnBuilder(type: any): {
    add(batch: any): /*elided*/ any;
    clear: () => any[];
    done: () => Column<any>;
};
/**
 * A data column. A column provides a view over one or more value batches,
 * each drawn from an Arrow record batch. This class supports random access
 * to column values by integer index; however, extracting arrays using
 * `toArray()` or iterating over values (`for (const value of column) {...}`)
 * provide more efficient ways for bulk access or scanning.
 * @template T
 */
export class Column<T> {
    /**
     * Create a new column instance.
     * @param {Batch<T>[]} data The value batches.
     * @param {DataType} [type] The column data type.
     *  If not specified, the type is extracted from the batches.
     */
    constructor(data: Batch<T>[], type?: DataType);
    /**
     * The column data type.
     * @type {DataType}
     * @readonly
     */
    readonly type: DataType;
    /**
     * The column length.
     * @type {number}
     * @readonly
     */
    readonly length: number;
    /**
     * The count of null values in the column.
     * @type {number}
     * @readonly
     */
    readonly nullCount: number;
    /**
     * An array of column data batches.
     * @type {readonly Batch<T>[]}
     * @readonly
     */
    readonly data: readonly Batch<T>[];
    /**
     * Return the column value at the given index. If a column has multiple
     * batches, this method performs binary search over the batch lengths to
     * determine the batch from which to retrieve the value. The search makes
     * lookup less efficient than a standard array access. If making a full
     * scan of a column, consider extracting arrays via `toArray()` or using an
     * iterator (`for (const value of column) {...}`).
     * @param {number} index The row index.
     * @returns {T | null} The value.
     */
    at(index: number): T | null;
    /**
     * Index offsets for data batches.
     * Used to map a column row index to a batch-specific index.
     * @type {Int32Array}
     * @readonly
     */
    readonly offsets: Int32Array;
    /**
     * Return the column value at the given index. This method is the same as
     * `at()` and is provided for better compatibility with Apache Arrow JS.
     * @param {number} index The row index.
     * @returns {T | null} The value.
     */
    get(index: number): T | null;
    /**
     * Extract column values into a single array instance. When possible,
     * a zero-copy subarray of the input Arrow data is returned.
     * @returns {ValueArray<T?>}
     */
    toArray(): ValueArray<T | null>;
    /**
     * Return an array of cached column values.
     * Used internally to accelerate dictionary types.
     */
    cache(): ValueArray<T>;
    _cache: ValueArray<T>;
    /**
     * Provide an informative object string tag.
     */
    get [Symbol.toStringTag](): string;
    /**
     * Return an iterator over the values in this column.
     * @returns {Iterator<T?>}
     */
    [Symbol.iterator](): Iterator<T | null>;
}
import type { DataType } from './types.js';
import type { Batch } from './batch.js';
import type { ValueArray } from './types.js';
