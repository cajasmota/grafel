/**
 * Returns a row proxy object factory. The resulting method takes a
 * batch-level row index as input and returns an object that proxies
 * access to underlying batches.
 * @param {string[]} names The column (property) names
 * @param {Batch[]} batches The value batches.
 * @returns {(index: number) => Record<string, any>}
 */
export function proxyFactory(names: string[], batches: Batch<any>[]): (index: number) => Record<string, any>;
/**
 * Returns a row object factory. The resulting method takes a
 * batch-level row index as input and returns an object whose property
 * values have been extracted from the batches.
 * @param {string[]} names The column (property) names
 * @param {Batch[]} batches The value batches.
 * @returns {(index: number) => Record<string, any>}
 */
export function objectFactory(names: string[], batches: Batch<any>[]): (index: number) => Record<string, any>;
/**
 * Return a vanilla object representing a struct (row object) type.
 * @param {string[]} names The column (property) names
 * @param {Batch[]} batches The value batches.
 * @param {number} index The record batch row index.
 * @returns {Record<string, any>}
 */
export function structObject(names: string[], batches: Batch<any>[], index: number): Record<string, any>;
/**
 * @import { Batch } from '../batch.js';
 */
/**
 * Symbol for the row index value of a struct object proxy.
 */
export const RowIndex: unique symbol;
import type { Batch } from '../batch.js';
