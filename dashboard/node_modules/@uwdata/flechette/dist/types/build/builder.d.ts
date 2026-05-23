/**
 * Create a context object for shared builder state.
 * @param {ExtractionOptions} [options]  Batch extraction options.
* @param {ReturnType<dictionaryContext>} [dictionaries]
 *  Context object for tracking dictionaries.
 */
export function builderContext(options?: ExtractionOptions, dictionaries?: ReturnType<typeof dictionaryContext>): {
    batchType: (type: any) => any;
    builder(type: any): BatchBuilder;
    dictionary(type: any): {
        type: import("../types.js").DictionaryType;
        values: BatchBuilder;
        add(batch: any): any;
        key(value: any): any;
        finish(options: any): void;
    };
    finish: () => void;
};
/**
 * Returns a batch builder for the given type and builder context.
 * @param {DataType} type A data type.
 * @param {ReturnType<builderContext>} [ctx] A builder context.
 * @returns {BatchBuilder}
 */
export function builder(type: DataType, ctx?: ReturnType<typeof builderContext>): BatchBuilder;
import type { ExtractionOptions } from '../types.js';
import { dictionaryContext } from './builders/dictionary.js';
import type { BatchBuilder } from './builders/batch.js';
import type { DataType } from '../types.js';
