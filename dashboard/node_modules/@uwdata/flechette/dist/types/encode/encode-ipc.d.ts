/**
 * Encode assembled data into Arrow IPC binary format.
 * @param {any} data Assembled table data.
 * @param {object} options Encoding options.
 * @param {Sink} [options.sink] IPC byte consumer.
 * @param {'stream' | 'file'} [options.format] Arrow stream or file format.
 * @param {CompressionType_} [options.codec] Compression codec to apply.
 * @returns {Sink} The sink that was passed in.
 */
export function encodeIPC(data: any, { sink, format, codec }?: {
    sink?: Sink;
    format?: "stream" | "file";
    codec?: CompressionType_;
}): Sink;
import type { Sink } from './sink.js';
import type { CompressionType_ } from '../types.js';
