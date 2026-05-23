/**
 * Builder for batches of decimal-typed data (64-bits or more).
 */
export class DecimalBuilder extends ValidityBuilder {
    scale: number;
    stride: number;
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
