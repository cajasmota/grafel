/**
 * Builder for day/time interval-typed data batches.
 */
export class IntervalDayTimeBuilder extends ValidityBuilder {
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
/**
 * Builder for month/day/nano interval-typed data batches.
 */
export class IntervalMonthDayNanoBuilder extends ValidityBuilder {
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
