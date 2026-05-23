/**
 * Check if the input is a batch that supports direct access to
 * binary data in the form of typed arrays.
 * @param {Batch<any>?} batch The data batch to check.
 * @returns {boolean} True if a direct batch, false otherwise.
 */
export function isDirectBatch(batch: Batch<any> | null): boolean;
/**
 * Column values from a single record batch.
 * A column may contain multiple batches.
 * @template T
 */
export class Batch<T> {
    /**
     * The array type to use when extracting data from the batch.
     * A null value indicates that the array type should match
     * the type of the batch's values array.
     * @type {ArrayConstructor | TypedArrayConstructor | null}
     */
    static ArrayType: ArrayConstructor | TypedArrayConstructor | null;
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} [options.values] Values buffer
     * @param {OffsetArray} [options.offsets] Offsets buffer
     * @param {OffsetArray} [options.sizes] Sizes buffer
     * @param {Batch[]} [options.children] Children batches
     */
    constructor({ length, nullCount, type, validity, values, offsets, sizes, children }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values?: TypedArray;
        offsets?: OffsetArray;
        sizes?: OffsetArray;
        children?: Batch<any>[];
    });
    length: number;
    nullCount: number;
    type: DataType;
    validity: Uint8Array<ArrayBufferLike>;
    values: TypedArray;
    offsets: OffsetArray;
    sizes: OffsetArray;
    children: Batch<any>[];
    /**
     * Return the value at the given index.
     * @param {number} index The value index.
     * @returns {T | null} The value.
     */
    at(index: number): T | null;
    /**
     * Check if a value at the given index is valid (non-null).
     * @param {number} index The value index.
     * @returns {boolean} True if valid, false otherwise.
     */
    isValid(index: number): boolean;
    /**
     * Return the value at the given index. This method does not check the
     * validity bitmap and is intended primarily for internal use. In most
     * cases, callers should use the `at()` method instead.
     * @param {number} index The value index
     * @returns {T} The value, ignoring the validity bitmap.
     */
    value(index: number): T;
    /**
     * Extract an array of values within the given index range. Unlike
     * Array.slice, all arguments are required and may not be negative indices.
     * @param {number} start The starting index, inclusive
     * @param {number} end The ending index, exclusive
     * @returns {ValueArray<T?>} The slice of values
     */
    slice(start: number, end: number): ValueArray<T | null>;
    /**
     * Provide an informative object string tag.
     */
    get [Symbol.toStringTag](): string;
    /**
     * Return an iterator over the values in this batch.
     * @returns {Iterator<T?>}
     */
    [Symbol.iterator](): Iterator<T | null>;
}
/**
 * A batch whose value buffer can be used directly, without transformation.
 * @template T
 * @extends {Batch<T>}
 */
export class DirectBatch<T> extends Batch<T> {
    /**
     * Create a new column batch with direct value array access.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} options.values Values buffer
     */
    constructor(options: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values: TypedArray;
    });
    values: any;
}
/**
 * A batch whose values are transformed to 64-bit numbers.
 * @extends {Batch<number>}
 */
export class NumberBatch extends Batch<number> {
    static ArrayType: Float64ArrayConstructor;
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} [options.values] Values buffer
     * @param {OffsetArray} [options.offsets] Offsets buffer
     * @param {OffsetArray} [options.sizes] Sizes buffer
     * @param {Batch[]} [options.children] Children batches
     */
    constructor({ length, nullCount, type, validity, values, offsets, sizes, children }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values?: TypedArray;
        offsets?: OffsetArray;
        sizes?: OffsetArray;
        children?: Batch<any>[];
    });
}
/**
 * A batch whose values should be returned in a standard array.
 * @template T
 * @extends {Batch<T>}
 */
export class ArrayBatch<T> extends Batch<T> {
    static ArrayType: ArrayConstructor;
}
/**
 * A batch of null values only.
 * @extends {ArrayBatch<null>}
 */
export class NullBatch extends ArrayBatch<null> {
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} [options.values] Values buffer
     * @param {OffsetArray} [options.offsets] Offsets buffer
     * @param {OffsetArray} [options.sizes] Sizes buffer
     * @param {Batch[]} [options.children] Children batches
     */
    constructor({ length, nullCount, type, validity, values, offsets, sizes, children }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values?: TypedArray;
        offsets?: OffsetArray;
        sizes?: OffsetArray;
        children?: Batch<any>[];
    });
}
/**
 * A batch that coerces BigInt values to 64-bit numbers.
 * @extends {NumberBatch}
 */
export class Int64Batch extends NumberBatch {
}
/**
 * A batch of 16-bit floating point numbers, accessed as unsigned
 * 16-bit ints and transformed to 64-bit numbers.
 */
export class Float16Batch extends NumberBatch {
}
/**
 * A batch of boolean values stored as a bitmap.
 * @extends {ArrayBatch<boolean>}
 */
export class BoolBatch extends ArrayBatch<boolean> {
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} [options.values] Values buffer
     * @param {OffsetArray} [options.offsets] Offsets buffer
     * @param {OffsetArray} [options.sizes] Sizes buffer
     * @param {Batch[]} [options.children] Children batches
     */
    constructor({ length, nullCount, type, validity, values, offsets, sizes, children }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values?: TypedArray;
        offsets?: OffsetArray;
        sizes?: OffsetArray;
        children?: Batch<any>[];
    });
}
/**
 * A batch of 32-bit decimal numbers, returned as converted 64-bit floating
 * point numbers. Number coercion may be lossy if the decimal precision can
 * not be represented in a 64-bit floating point format.
 * @extends {NumberBatch}
 */
export class Decimal32NumberBatch extends NumberBatch {
    constructor(options: any);
    scale: number;
}
/**
 * An abstract class for a batch of 64-, 128- or 256-bit decimal numbers,
 * accessed in strided BigUint64Arrays.
 * @template T
 * @extends {Batch<T>}
 */
export class DecimalBatch<T> extends Batch<T> {
    constructor(options: any);
    decimal: typeof fromDecimal64;
    scale: bigint;
}
/**
 * A batch of 64-, 128- or 256-bit decimal numbers, returned as converted
 * 64-bit floating point numbers. Number coercion may be lossy if the decimal
 * precision can not be represented in a 64-bit floating point format.
 * @extends {DecimalBatch<number>}
 */
export class DecimalNumberBatch extends DecimalBatch<number> {
    static ArrayType: Float64ArrayConstructor;
    constructor(options: any);
}
/**
 * A batch of 64-, 128- or 256-bit decimal numbers, returned as scaled
 * bigint values, such that all fractional digits have been shifted
 * to integer places by the decimal type scale factor.
 * @extends {DecimalBatch<bigint>}
 */
export class DecimalBigIntBatch extends DecimalBatch<bigint> {
    static ArrayType: ArrayConstructor;
    constructor(options: any);
}
/**
 * A batch of date or timestamp values that are coerced to UNIX epoch timestamps
 * and returned as JS Date objects. This batch wraps a source batch that provides
 * timestamp values.
 * @extends {ArrayBatch<Date>}
 */
export class DateBatch extends ArrayBatch<Date> {
    /**
     * Create a new date batch.
     * @param {Batch<number>} batch A batch of timestamp values.
     */
    constructor(batch: Batch<number>);
    source: Batch<number>;
}
/**
 * A batch of dates as day counts, coerced to timestamp numbers.
 */
export class DateDayBatch extends NumberBatch {
}
/**
 * A batch of dates as millisecond timestamps, coerced to numbers.
 */
export const DateDayMillisecondBatch: typeof Int64Batch;
/**
 * A batch of timestaps in seconds, coerced to millisecond numbers.
 */
export class TimestampSecondBatch extends Int64Batch {
}
/**
 * A batch of timestaps in milliseconds, coerced to numbers.
 */
export const TimestampMillisecondBatch: typeof Int64Batch;
/**
 * A batch of timestaps in microseconds, coerced to millisecond numbers.
 */
export class TimestampMicrosecondBatch extends Int64Batch {
}
/**
 * A batch of timestaps in nanoseconds, coerced to millisecond numbers.
 */
export class TimestampNanosecondBatch extends Int64Batch {
}
/**
 * A batch of day/time intervals, returned as two-element 32-bit int arrays.
 * @extends {ArrayBatch<Int32Array>}
 */
export class IntervalDayTimeBatch extends ArrayBatch<Int32Array<ArrayBufferLike>> {
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} [options.values] Values buffer
     * @param {OffsetArray} [options.offsets] Offsets buffer
     * @param {OffsetArray} [options.sizes] Sizes buffer
     * @param {Batch[]} [options.children] Children batches
     */
    constructor({ length, nullCount, type, validity, values, offsets, sizes, children }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values?: TypedArray;
        offsets?: OffsetArray;
        sizes?: OffsetArray;
        children?: Batch<any>[];
    });
}
/**
 * A batch of month/day/nanosecond intervals, returned as three-element arrays.
 * @extends {ArrayBatch<Float64Array>}
 */
export class IntervalMonthDayNanoBatch extends ArrayBatch<Float64Array<ArrayBufferLike>> {
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} [options.values] Values buffer
     * @param {OffsetArray} [options.offsets] Offsets buffer
     * @param {OffsetArray} [options.sizes] Sizes buffer
     * @param {Batch[]} [options.children] Children batches
     */
    constructor({ length, nullCount, type, validity, values, offsets, sizes, children }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values?: TypedArray;
        offsets?: OffsetArray;
        sizes?: OffsetArray;
        children?: Batch<any>[];
    });
    /**
     * @param {number} index The value index
     */
    value(index: number): Float64Array<ArrayBuffer>;
}
/**
 * A batch of binary blobs with variable offsets, returned as byte buffers of
 * unsigned 8-bit integers. The offsets are 32-bit ints.
 * @extends {ArrayBatch<Uint8Array>}
 */
export class BinaryBatch extends ArrayBatch<Uint8Array<ArrayBufferLike>> {
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} [options.values] Values buffer
     * @param {OffsetArray} [options.offsets] Offsets buffer
     * @param {OffsetArray} [options.sizes] Sizes buffer
     * @param {Batch[]} [options.children] Children batches
     */
    constructor({ length, nullCount, type, validity, values, offsets, sizes, children }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values?: TypedArray;
        offsets?: OffsetArray;
        sizes?: OffsetArray;
        children?: Batch<any>[];
    });
}
/**
 * A batch of binary blobs with variable offsets, returned as byte buffers of
 * unsigned 8-bit integers. The offsets are 64-bit ints. Value extraction will
 * fail if an offset exceeds `Number.MAX_SAFE_INTEGER`.
 * @extends {ArrayBatch<Uint8Array>}
 */
export class LargeBinaryBatch extends ArrayBatch<Uint8Array<ArrayBufferLike>> {
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} [options.values] Values buffer
     * @param {OffsetArray} [options.offsets] Offsets buffer
     * @param {OffsetArray} [options.sizes] Sizes buffer
     * @param {Batch[]} [options.children] Children batches
     */
    constructor({ length, nullCount, type, validity, values, offsets, sizes, children }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values?: TypedArray;
        offsets?: OffsetArray;
        sizes?: OffsetArray;
        children?: Batch<any>[];
    });
}
/**
 * A batch of UTF-8 strings with variable offsets. The offsets are 32-bit ints.
 * @extends {ArrayBatch<string>}
 */
export class Utf8Batch extends ArrayBatch<string> {
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} [options.values] Values buffer
     * @param {OffsetArray} [options.offsets] Offsets buffer
     * @param {OffsetArray} [options.sizes] Sizes buffer
     * @param {Batch[]} [options.children] Children batches
     */
    constructor({ length, nullCount, type, validity, values, offsets, sizes, children }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values?: TypedArray;
        offsets?: OffsetArray;
        sizes?: OffsetArray;
        children?: Batch<any>[];
    });
}
/**
 * A batch of UTF-8 strings with variable offsets. The offsets are 64-bit ints.
 * Value extraction will fail if an offset exceeds `Number.MAX_SAFE_INTEGER`.
 * @extends {ArrayBatch<string>}
 */
export class LargeUtf8Batch extends ArrayBatch<string> {
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} [options.values] Values buffer
     * @param {OffsetArray} [options.offsets] Offsets buffer
     * @param {OffsetArray} [options.sizes] Sizes buffer
     * @param {Batch[]} [options.children] Children batches
     */
    constructor({ length, nullCount, type, validity, values, offsets, sizes, children }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values?: TypedArray;
        offsets?: OffsetArray;
        sizes?: OffsetArray;
        children?: Batch<any>[];
    });
}
/**
 * A batch of list (array) values of variable length. The list offsets are
 * 32-bit ints.
 * @template V
 * @extends {ArrayBatch<ValueArray<V>>}
 */
export class ListBatch<V> extends ArrayBatch<ValueArray<V>> {
}
/**
 * A batch of list (array) values of variable length. The list offsets are
 * 64-bit ints. Value extraction will fail if an offset exceeds
 * `Number.MAX_SAFE_INTEGER`.
 * @template V
 * @extends {ArrayBatch<ValueArray<V>>}
 */
export class LargeListBatch<V> extends ArrayBatch<ValueArray<V>> {
}
/**
 * A batch of list (array) values of variable length. The list offsets and
 * sizes are 32-bit ints.
 * @template V
 * @extends {ArrayBatch<ValueArray<V>>}
 */
export class ListViewBatch<V> extends ArrayBatch<ValueArray<V>> {
}
/**
 * A batch of list (array) values of variable length. The list offsets and
 * sizes are 64-bit ints. Value extraction will fail if an offset or size
 * exceeds `Number.MAX_SAFE_INTEGER`.
 * @template V
 * @extends {ArrayBatch<ValueArray<V>>}
 */
export class LargeListViewBatch<V> extends ArrayBatch<ValueArray<V>> {
}
/**
 * A batch of binary blobs of fixed size, returned as byte buffers of unsigned
 * 8-bit integers.
 * @extends {FixedBatch<Uint8Array>}
 */
export class FixedBinaryBatch extends FixedBatch<Uint8Array<ArrayBufferLike>> {
    constructor(options: any);
}
/**
 * A batch of list (array) values of fixed length.
 * @template V
 * @extends {FixedBatch<ValueArray<V>>}
 */
export class FixedListBatch<V> extends FixedBatch<ValueArray<V>> {
}
/**
 * A batch of map (key, value) values. The map is represented as a list of
 * key-value structs.
 * @template K, V
 * @extends {ArrayBatch<[K, V][]>}
 */
export class MapEntryBatch<K, V> extends ArrayBatch<[K, V][]> {
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} [options.values] Values buffer
     * @param {OffsetArray} [options.offsets] Offsets buffer
     * @param {OffsetArray} [options.sizes] Sizes buffer
     * @param {Batch[]} [options.children] Children batches
     */
    constructor({ length, nullCount, type, validity, values, offsets, sizes, children }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values?: TypedArray;
        offsets?: OffsetArray;
        sizes?: OffsetArray;
        children?: Batch<any>[];
    });
}
/**
 * A batch of map (key, value) values. The map is represented as a list of
 * key-value structs.
 * @template K, V
 * @extends {ArrayBatch<Map<K, V>>}
 */
export class MapBatch<K, V> extends ArrayBatch<Map<K, V>> {
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {TypedArray} [options.values] Values buffer
     * @param {OffsetArray} [options.offsets] Offsets buffer
     * @param {OffsetArray} [options.sizes] Sizes buffer
     * @param {Batch[]} [options.children] Children batches
     */
    constructor({ length, nullCount, type, validity, values, offsets, sizes, children }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values?: TypedArray;
        offsets?: OffsetArray;
        sizes?: OffsetArray;
        children?: Batch<any>[];
    });
}
/**
 * A batch of union-type values with a sparse layout, enabling direct
 * lookup from the child value batches.
 * @template T
 * @extends {ArrayBatch<T>}
 */
export class SparseUnionBatch<T> extends ArrayBatch<T> {
    /**
     * Create a new column batch.
     * @param {object} options
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {Int32Array} [options.offsets] Offsets buffer
     * @param {Batch[]} options.children Children batches
     * @param {Int8Array} options.typeIds Union type ids buffer
     * @param {Record<string, number>} options.map A typeId to children index map
     */
    constructor({ typeIds, ...options }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        offsets?: Int32Array;
        children: Batch<any>[];
        typeIds: Int8Array;
        map: Record<string, number>;
    });
    /** @type {Int8Array} */
    typeIds: Int8Array;
    /** @type {Record<string, number>} */
    typeMap: Record<string, number>;
    /**
     * @param {number} index The value index.
     */
    value(index: number, offset?: number): any;
}
/**
 * A batch of union-type values with a dense layout, reqiring offset
 * lookups from the child value batches.
 * @template T
 * @extends {SparseUnionBatch<T>}
 */
export class DenseUnionBatch<T> extends SparseUnionBatch<T> {
    /**
     * @param {number} index The value index.
     */
    value(index: number): any;
}
/**
 * A batch of struct values, containing a set of named properties.
 * Struct property values are extracted and returned as JS objects.
 * @extends {ArrayBatch<Record<string, any>>}
 */
export class StructBatch extends ArrayBatch<Record<string, any>> {
    constructor(options: any, factory?: typeof objectFactory);
    /** @type {string[]} */
    names: string[];
    factory: (index: number) => Record<string, any>;
}
/**
 * A batch of struct values, containing a set of named properties.
 * Structs are returned as proxy objects that extract data directly
 * from underlying Arrow batches.
 * @extends {StructBatch}
 */
export class StructProxyBatch extends StructBatch {
    constructor(options: any);
}
/**
 * A batch of run-end-encoded values.
 * @template T
 * @extends {ArrayBatch<T>}
 */
export class RunEndEncodedBatch<T> extends ArrayBatch<T> {
    /**
     * @param {number} index The value index.
     */
    value(index: number): any;
}
/**
 * A batch of dictionary-encoded values.
 * @template T
 * @extends {ArrayBatch<T>}
 */
export class DictionaryBatch<T> extends ArrayBatch<T> {
    /**
     * Register the backing dictionary. Dictionaries are added
     * after batch creation as the complete dictionary may not
     * be finished across multiple record batches.
     * @param {Column<T>} dictionary
     * The dictionary of column values.
     */
    setDictionary(dictionary: Column<T>): this;
    dictionary: Column<T>;
    cache: ValueArray<T>;
    /**
     * @param {number} index The value index.
     * @returns {number} The dictionary key
     */
    key(index: number): number;
}
/**
 * A batch of binary blobs from variable data buffers, returned as byte
 * buffers of unsigned 8-bit integers.
 * @extends {ViewBatch<Uint8Array>}
 */
export class BinaryViewBatch extends ViewBatch<Uint8Array<ArrayBufferLike>> {
    /**
     * Create a new view batch.
     * @param {object} options Batch options.
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {Uint8Array} options.values Values buffer
     * @param {Uint8Array[]} options.data View data buffers
     */
    constructor({ data, ...options }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values: Uint8Array;
        data: Uint8Array[];
    });
}
/**
 * A batch of UTF-8 strings from variable data buffers.
 * @extends {ViewBatch<string>}
 */
export class Utf8ViewBatch extends ViewBatch<string> {
    /**
     * Create a new view batch.
     * @param {object} options Batch options.
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {Uint8Array} options.values Values buffer
     * @param {Uint8Array[]} options.data View data buffers
     */
    constructor({ data, ...options }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values: Uint8Array;
        data: Uint8Array[];
    });
}
import type { DataType } from './types.js';
import type { TypedArray } from './types.js';
import type { OffsetArray } from './types.js';
import type { ValueArray } from './types.js';
import type { TypedArrayConstructor } from './types.js';
import { fromDecimal64 } from './util/numbers.js';
/**
 * A batch with a fixed stride.
 * @template T
 * @extends {ArrayBatch<T>}
 */
declare class FixedBatch<T> extends ArrayBatch<T> {
    constructor(options: any);
    /** @type {number} */
    stride: number;
}
import { objectFactory } from './util/struct.js';
import type { Column } from './column.js';
/**
 * @template T
 * @extends {ArrayBatch<T>}
 */
declare class ViewBatch<T> extends ArrayBatch<T> {
    /**
     * Create a new view batch.
     * @param {object} options Batch options.
     * @param {number} options.length The length of the batch
     * @param {number} options.nullCount The null value count
     * @param {DataType} options.type The data type.
     * @param {Uint8Array} [options.validity] Validity bitmap buffer
     * @param {Uint8Array} options.values Values buffer
     * @param {Uint8Array[]} options.data View data buffers
     */
    constructor({ data, ...options }: {
        length: number;
        nullCount: number;
        type: DataType;
        validity?: Uint8Array;
        values: Uint8Array;
        data: Uint8Array[];
    });
    data: Uint8Array<ArrayBufferLike>[];
    /**
     * Get the binary data at the provided index.
     * @param {number} index The value index.
     * @returns {Uint8Array}
     */
    view(index: number): Uint8Array;
}
export {};
