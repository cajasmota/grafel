/**
 * Builder for batches of binary-typed data.
 */
export class BinaryBuilder extends ValidityBuilder {
    toOffset: typeof import("../../util/numbers.js").toBigInt | typeof import("../../util/numbers.js").identity;
    init(): this;
    offsets: import("../buffer.js").Buffer;
    values: import("../buffer.js").Buffer;
    pos: number;
    set(value: any, index: any): void;
    done(): {
        offsets: import("../../types.js").TypedArray;
        values: import("../../types.js").TypedArray;
        length: number;
        nullCount: number;
        type: any;
        validity: import("../../types.js").TypedArray;
    };
}
import { ValidityBuilder } from './validity.js';
