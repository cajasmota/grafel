/**
 * Builder for batches of bool-typed data.
 */
export class BoolBuilder extends ValidityBuilder {
    init(): this;
    values: import("../buffer.js").Bitmap;
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
