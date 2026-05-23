/**
 * Builder for data batches that can be accessed directly as typed arrays.
 */
export class DirectBuilder extends ValidityBuilder {
    values: import("../buffer.js").Buffer;
    init(): this;
    done(): {
        values: import("../../types.js").TypedArray;
        length: number;
        nullCount: number;
        type: any;
        validity: import("../../types.js").TypedArray;
    };
}
/**
 * Builder for int64/uint64 data batches written as bigints.
 */
export class Int64Builder extends DirectBuilder {
    set(value: any, index: any): void;
}
/**
 * Builder for data batches whose values must pass through a transform
 * function prior to be written to a backing buffer.
 */
export class TransformBuilder extends DirectBuilder {
    constructor(type: any, ctx: any, transform: any);
    transform: any;
    set(value: any, index: any): void;
}
import { ValidityBuilder } from './validity.js';
