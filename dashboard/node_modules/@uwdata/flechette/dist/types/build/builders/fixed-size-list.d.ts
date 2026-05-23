/**
 * Builder for fixed-size-list-typed data batches.
 */
export class FixedSizeListBuilder extends ValidityBuilder {
    child: any;
    stride: any;
    init(): this;
    set(value: any, index: any): void;
    done(): {
        children: any[];
        length: number;
        nullCount: number;
        type: any;
        validity: import("../../types.js").TypedArray;
    };
}
import { ValidityBuilder } from './validity.js';
