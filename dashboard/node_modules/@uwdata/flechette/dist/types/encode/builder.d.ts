export function writeInt32(buf: any, index: any, value: any): void;
export function writeInt64(buf: any, index: any, value: any): void;
/**
 * Prepare to write an element of `size` after `additionalBytes` have been
 * written, e.g. if we write a string, we need to align such the int length
 * field is aligned to 4 bytes, and the string data follows it directly. If all
 * we need to do is alignment, `additionalBytes` will be 0.
 * @param {Builder} builder The builder to prep.
 * @param {number} size The size of the new element to write.
 * @param {number} additionalBytes Additional padding size.
 */
export function prep(builder: Builder, size: number, additionalBytes: number): void;
/** Flatbuffer binary builder. */
export class Builder {
    /**
     * Create a new builder instance.
     * @param {Sink} sink The byte consumer.
     */
    constructor(sink: Sink);
    /**
     * Sink that consumes built byte buffers.
     * @type {Sink}
     */
    sink: Sink;
    /**
     * Minimum alignment encountered so far.
     * @type {number}
     */
    minalign: number;
    /**
     * Current byte buffer.
     * @type {Uint8Array}
     */
    buf: Uint8Array;
    /**
     * Remaining space in the current buffer.
     * @type {number}
     */
    space: number;
    /**
     * List of offsets of all vtables. Used to find and
     * reuse tables upon duplicated table field schemas.
     * @type {number[]}
     */
    vtables: number[];
    /**
     * Total bytes written to sink thus far.
     */
    outputBytes: number;
    /**
     * Returns the flatbuffer offset, relative to the end of the current buffer.
     * @returns {number} Offset relative to the end of the buffer.
     */
    offset(): number;
    /**
     * Write a flatbuffer int8 value at the current buffer position
     * and advance the internal cursor.
     * @param {number} value
     */
    writeInt8(value: number): void;
    /**
     * Write a flatbuffer int16 value at the current buffer position
     * and advance the internal cursor.
     * @param {number} value
     */
    writeInt16(value: number): void;
    /**
     * Write a flatbuffer int32 value at the current buffer position
     * and advance the internal cursor.
     * @param {number} value
     */
    writeInt32(value: number): void;
    /**
     * Write a flatbuffer int64 value at the current buffer position
     * and advance the internal cursor.
     * @param {number} value
     */
    writeInt64(value: number): void;
    /**
     * Add a flatbuffer int8 value, properly aligned,
     * @param value The int8 value to add the buffer.
     */
    addInt8(value: any): void;
    /**
     * Add a flatbuffer int16 value, properly aligned,
     * @param value The int16 value to add the buffer.
     */
    addInt16(value: any): void;
    /**
     * Add a flatbuffer int32 value, properly aligned,
     * @param value The int32 value to add the buffer.
     */
    addInt32(value: any): void;
    /**
     * Add a flatbuffer int64 values, properly aligned.
     * @param value The int64 value to add the buffer.
     */
    addInt64(value: any): void;
    /**
     * Add a flatbuffer offset, relative to where it will be written.
     * @param {number} offset The offset to add.
     */
    addOffset(offset: number): void;
    /**
     * Add a flatbuffer object (vtable).
     * @param {number} numFields The maximum number of fields
     *  this object may include.
     * @param {(tableBuilder: ReturnType<objectBuilder>) => void} [addFields]
     *  A callback function that writes all fields using an object builder.
     * @returns {number} The object offset.
     */
    addObject(numFields: number, addFields?: (tableBuilder: ReturnType<typeof objectBuilder>) => void): number;
    /**
     * Add a flatbuffer vector (list).
     * @template T
     * @param {T[]} items An array of items to write.
     * @param {number} itemSize The size in bytes of a serialized item.
     * @param {number} alignment The desired byte alignment value.
     * @param {(builder: this, item: T) => void} writeItem A callback
     *  function that writes a vector item to this builder.
     * @returns {number} The vector offset.
     */
    addVector<T>(items: T[], itemSize: number, alignment: number, writeItem: (builder: this, item: T) => void): number;
    /**
     * Convenience method for writing a vector of byte buffer offsets.
     * @param {number[]} offsets
     * @returns {number} The vector offset.
     */
    addOffsetVector(offsets: number[]): number;
    /**
     * Add a flatbuffer UTF-8 string.
     * @param {string} s The string to encode.
     * @return {number} The string offset.
     */
    addString(s: string): number;
    /**
     * Finish the current flatbuffer by adding a root offset.
     * @param {number} rootOffset The root offset.
     */
    finish(rootOffset: number): void;
    /**
     * Flush the current flatbuffer byte buffer content to the sink,
     * and reset the flatbuffer builder state.
     */
    flush(): void;
    /**
     * Add a byte buffer directly to the builder sink. This method bypasses
     * any unflushed flatbuffer state and leaves it unchanged, writing the
     * buffer to the sink *before* the flatbuffer.
     * The buffer will be padded for 64-bit (8-byte) alignment as needed.
     * @param {Uint8Array} buffer The buffer to add.
     * @returns {number} The total byte count of the buffer and padding.
     */
    addBuffer(buffer: Uint8Array): number;
    /**
     * Write padding bytes directly to the builder sink. This method bypasses
     * any unflushed flatbuffer state and leaves it unchanged, writing the
     * padding bytes to the sink *before* the flatbuffer.
     * @param {number} byteCount The number of padding bytes.
     */
    addPadding(byteCount: number): void;
}
import type { Sink } from './sink.js';
/**
 * Returns a builder object for flatbuffer objects (vtables).
 * @param {Builder} builder The underlying flatbuffer builder.
 * @param {number} numFields The expected number of fields, not
 *  including the standard size fields.
 */
declare function objectBuilder(builder: Builder, numFields: number): {
    /**
     * Add an int8-valued table field.
     * @param {number} index
     * @param {number} value
     * @param {number} defaultValue
     */
    addInt8(index: number, value: number, defaultValue: number): void;
    /**
     * Add an int16-valued table field.
     * @param {number} index
     * @param {number} value
     * @param {number} defaultValue
     */
    addInt16(index: number, value: number, defaultValue: number): void;
    /**
     * Add an int32-valued table field.
     * @param {number} index
     * @param {number} value
     * @param {number} defaultValue
     */
    addInt32(index: number, value: number, defaultValue: number): void;
    /**
     * Add an int64-valued table field.
     * @param {number} index
     * @param {number} value
     * @param {number} defaultValue
     */
    addInt64(index: number, value: number, defaultValue: number): void;
    /**
     * Add a buffer offset-valued table field.
     * @param {number} index
     * @param {number} value
     * @param {number} defaultValue
     */
    addOffset(index: number, value: number, defaultValue: number): void;
    /**
     * Write the vtable to the buffer and return the table offset.
     * @returns {number} The buffer offset to the vtable.
     */
    finish(): number;
};
export {};
