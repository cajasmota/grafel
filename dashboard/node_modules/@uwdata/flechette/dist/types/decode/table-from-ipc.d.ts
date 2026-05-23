/**
 * Decode [Apache Arrow IPC data][1] and return a new Table. The input binary
 * data may be either an `ArrayBuffer` or `Uint8Array`. For Arrow data in the
 * [IPC 'stream' format][2], an array of `Uint8Array` values is also supported.
 *
 * [1]: https://arrow.apache.org/docs/format/Columnar.html#serialization-and-interprocess-communication-ipc
 * [2]: https://arrow.apache.org/docs/format/Columnar.html#ipc-streaming-format
 * @param {ArrayBufferLike | Uint8Array | Uint8Array[]} data
 *  The source byte buffer, or an array of buffers. If an array, each byte
 *  array may contain one or more self-contained messages. Messages may NOT
 *  span multiple byte arrays.
 * @param {ExtractionOptions} [options]
 *  Options for controlling how values are transformed when extracted
 *  from an Arrow binary representation.
 * @returns {Table} A Table instance.
 */
export function tableFromIPC(data: ArrayBufferLike | Uint8Array | Uint8Array[], options?: ExtractionOptions): Table;
/**
 * Create a table from parsed IPC data.
 * @param {ArrowData} data
 *  The IPC data, as returned by parseIPC.
 * @param {ExtractionOptions} [options]
 *  Options for controlling how values are transformed when extracted
 *  from am Arrow binary representation.
 * @returns {Table} A Table instance.
 */
export function createTable(data: ArrowData, options?: ExtractionOptions): Table;
import type { ExtractionOptions } from '../types.js';
import { Table } from '../table.js';
import type { ArrowData } from '../types.js';
