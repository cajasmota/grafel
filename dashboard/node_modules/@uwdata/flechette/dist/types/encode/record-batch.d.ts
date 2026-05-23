/**
 * @param {Builder} builder
 * @param {RecordBatch} batch
 * @param {BodyCompression | null} [compression]
 * @returns {number}
 */
export function encodeRecordBatch(builder: Builder, batch: RecordBatch, compression?: BodyCompression | null): number;
import type { Builder } from './builder.js';
import type { RecordBatch } from '../types.js';
import type { BodyCompression } from '../types.js';
