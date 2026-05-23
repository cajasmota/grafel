/**
 * @import { Batch } from '../../batch.js'
 */
/**
 * Abstract class for building a column data batch.
 */
export class BatchBuilder {
    constructor(type: any, ctx: any);
    type: any;
    ctx: any;
    batchClass: any;
    /**
     * Initialize the builder state.
     * @returns {this} This builder.
     */
    init(): this;
    index: number;
    /**
     * Write a value to the builder.
     * @param {*} value
     * @param {number} index
     * @returns {boolean | void}
     */
    set(value: any, index: number): boolean | void;
    /**
     * Returns a batch constructor options object.
     * Used internally to marshal batch data.
     * @returns {Record<string, any>}
     */
    done(): Record<string, any>;
    /**
     * Returns a completed batch and reinitializes the builder state.
     * @returns {Batch}
     */
    batch(): Batch<any>;
}
import type { Batch } from '../../batch.js';
