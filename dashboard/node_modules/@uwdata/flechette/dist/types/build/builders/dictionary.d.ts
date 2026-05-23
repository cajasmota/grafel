/**
 * Create a context object for managing dictionary builders.
 */
export function dictionaryContext(): {
    /**
     * Get a dictionary values builder for the given dictionary type.
     * @param {DictionaryType} type
     *  The dictionary type.
     * @param {*} ctx The builder context.
     * @returns {ReturnType<dictionaryValues>}
     */
    get(type: DictionaryType, ctx: any): ReturnType<typeof dictionaryValues>;
    /**
     * Finish building dictionary values columns and assign them to
     * their corresponding dictionary batches.
     * @param {ExtractionOptions} options
     */
    finish(options: ExtractionOptions): void;
};
/**
 * Builder helper for creating dictionary values.
 * @param {DictionaryType} type
 *  The dictionary data type.
 * @param {ReturnType<builderContext>} ctx
 *  The builder context.
 */
export function dictionaryValues(type: DictionaryType, ctx: ReturnType<typeof builderContext>): {
    type: DictionaryType;
    values: import("./batch.js").BatchBuilder;
    add(batch: any): any;
    key(value: any): any;
    finish(options: any): void;
};
/**
 * Builder for dictionary-typed data batches.
 */
export class DictionaryBuilder extends ValidityBuilder {
    dict: any;
    init(): this;
    values: import("../buffer.js").Buffer;
    set(value: any, index: any): void;
    done(): {
        values: import("../../types.js").TypedArray;
        length: number;
        nullCount: number;
        type: any;
        validity: import("../../types.js").TypedArray;
    };
    batch(): any;
}
import type { DictionaryType } from '../../types.js';
import type { ExtractionOptions } from '../../types.js';
import type { builderContext } from '../builder.js';
import { ValidityBuilder } from './validity.js';
