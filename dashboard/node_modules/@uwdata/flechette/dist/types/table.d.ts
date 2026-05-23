/**
 * A table consists of a collection of named columns (or 'children').
 * To work with table data directly in JavaScript, use `toColumns()`
 * to extract an object that maps column names to extracted value arrays,
 * or `toArray()` to extract an array of row objects. For random access
 * by row index, use `getChild()` to access data for a specific column.
 * @template {TypeMap} [T=TypeMap]
 */
export class Table<T extends TypeMap = TypeMap> {
    /**
     * Create a new table with the given schema and columns (children).
     * @param {Schema} schema The table schema.
     * @param {Column[]} children The table columns.
     * @param {boolean} [useProxy=false] Flag indicating if row proxy
     *  objects should be used to represent table rows (default `false`).
     */
    constructor(schema: Schema, children: Column<any>[], useProxy?: boolean);
    /**
     * @type {Schema}
     * @readonly
     */
    readonly schema: Schema;
    /**
     * @type {(keyof T)[]}
     * @readonly
     */
    readonly names: (keyof T)[];
    /**
     * @type {Column[]}
     * @readonly
     */
    readonly children: Column<any>[];
    /**
     * @type {StructFactory}
     * @readonly
     */
    readonly factory: StructFactory;
    /**
     * Returns a row object generator for the given batch index.
     * @private
     * @readonly
     * @param {number} b The batch index.
     * @returns {(index: number) => { [P in keyof T]: T[P] }}
     */
    private readonly getFactory;
    /**
     * The number of columns in this table.
     * @return {number} The number of columns.
     */
    get numCols(): number;
    /**
     * The number of rows in this table.
     * @return {number} The number of rows.
     */
    get numRows(): number;
    /**
     * Return the child column at the given index position.
     * @template {T[keyof T]} R
     * @param {number} index The column index.
     * @returns {Column<R>}
     */
    getChildAt<R extends T[keyof T]>(index: number): Column<R>;
    /**
     * Return the first child column with the given name.
     * @template {keyof T} P
     * @param {P} name The column name.
     * @returns {Column<T[P]>}
     */
    getChild<P extends keyof T>(name: P): Column<T[P]>;
    /**
     * Construct a new table containing only columns at the specified indices.
     * The order of columns in the new table matches the order of input indices.
     * @template {T[keyof T]} V
     * @param {number[]} indices The indices of columns to keep.
     * @param {string[]} [as] Optional new names for selected columns.
     * @returns {Table<{ [key: string]: V }>} A new table with selected columns.
     */
    selectAt<V extends T[keyof T]>(indices: number[], as?: string[]): Table<{
        [key: string]: V;
    }>;
    /**
     * Construct a new table containing only columns with the specified names.
     * If columns have duplicate names, the first (with lowest index) is used.
     * The order of columns in the new table matches the order of input names.
     * @template {keyof T} K
     * @param {K[]} names Names of columns to keep.
     * @param {string[]} [as] Optional new names for selected columns.
     * @returns A new table with columns matching the specified names.
     */
    select<K extends keyof T>(names: K[], as?: string[]): Table<{
        [key: string]: T[keyof T];
    }>;
    /**
     * Return an object mapping column names to extracted value arrays.
     * @returns {{ [P in keyof T]: ValueArray<T[P]> }}
     */
    toColumns(): { [P in keyof T]: ValueArray<T[P]>; };
    /**
     * Return an array of objects representing the rows of this table.
     * @returns {{ [P in keyof T]: T[P] }[]}
     */
    toArray(): { [P in keyof T]: T[P]; }[];
    /**
     * Return a row object for the given index.
     * @param {number} index The row index.
     * @returns {{ [P in keyof T]: T[P] }} The row object.
     */
    at(index: number): { [P in keyof T]: T[P]; };
    /**
     * Return a row object for the given index. This method is the same as
     * `at()` and is provided for better compatibility with Apache Arrow JS.
     * @param {number} index The row index.
     * @returns {{ [P in keyof T]: T[P] }} The row object.
     */
    get(index: number): { [P in keyof T]: T[P]; };
    /**
     * Provide an informative object string tag.
     */
    get [Symbol.toStringTag](): string;
    /**
     * Return an iterator over objects representing the rows of this table.
     * @returns {Generator<{ [P in keyof T]: T[P] }, any, any>}
     */
    [Symbol.iterator](): Generator<{ [P in keyof T]: T[P]; }, any, any>;
}
import type { TypeMap } from './types.js';
import type { Schema } from './types.js';
import type { Column } from './column.js';
import type { StructFactory } from './types.js';
import type { ValueArray } from './types.js';
