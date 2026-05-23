/**
 * Builder for fixed-size-binary-typed data batches.
 */
export class FixedSizeBinaryBuilder extends ValidityBuilder {
    stride: any;
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
}
import { ValidityBuilder } from './validity.js';
