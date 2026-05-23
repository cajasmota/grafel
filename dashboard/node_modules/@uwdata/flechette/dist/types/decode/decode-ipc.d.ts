/**
 * Decode [Apache Arrow IPC data][1] and return parsed schema, record batch,
 * and dictionary batch definitions. The input binary data may be either
 * an `ArrayBuffer` or `Uint8Array`. For Arrow data in the IPC 'stream' format,
 * an array of `Uint8Array` instances is also supported.
 *
 * This method stops short of generating views over field buffers. Use the
 * `createData()` method on the result to enable column data access.
 *
 * [1]: https://arrow.apache.org/docs/format/Columnar.html#serialization-and-interprocess-communication-ipc
 * @param {ArrayBufferLike | Uint8Array | Uint8Array[]} data
 *  The source byte buffer, or an array of buffers. If an array, each byte
 *  array may contain one or more self-contained messages. Messages may NOT
 *  span multiple byte arrays.
 * @returns {import('../types.js').ArrowData}
 */
export function decodeIPC(data: ArrayBufferLike | Uint8Array | Uint8Array[]): import("../types.js").ArrowData;
/**
 * Decode data in the [Arrow IPC 'stream' format][1].
 *
 * [1]: https://arrow.apache.org/docs/format/Columnar.html#ipc-streaming-format
 * @param {Uint8Array | Uint8Array[]} data The source byte buffer, or an
 *  array of buffers. If an array, each byte array may contain one or more
 *  self-contained messages. Messages may NOT span multiple byte arrays.
 * @returns {ArrowData}
 */
export function decodeIPCStream(data: Uint8Array | Uint8Array[]): ArrowData;
/**
 * Decode data in the [Arrow IPC 'file' format][1].
 *
 * [1]: https://arrow.apache.org/docs/format/Columnar.html#ipc-file-format
 * @param {Uint8Array} data The source byte buffer.
 * @returns {ArrowData}
 */
export function decodeIPCFile(data: Uint8Array): ArrowData;
import type { ArrowData } from '../types.js';
