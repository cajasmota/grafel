/**
 * @param {Builder} builder
 * @param {DictionaryBatch} dictionaryBatch
 * @param {BodyCompression | null} compression
 * @returns {number}
 */
export function encodeDictionaryBatch(builder: Builder, dictionaryBatch: DictionaryBatch, compression: BodyCompression | null): number;
import type { Builder } from './builder.js';
import type { DictionaryBatch } from '../types.js';
import type { BodyCompression } from '../types.js';
