/**
 * Abstract class for building union-typed data batches.
 */
export class AbstractUnionBuilder extends BatchBuilder {
    children: any;
    typeMap: any;
    lookup: any;
    init(): this;
    nullCount: number;
    typeIds: import("../buffer.js").Buffer;
    set(value: any, index: any): void;
    done(): {
        length: number;
        nullCount: number;
        type: any;
        typeIds: import("../../types.js").TypedArray;
        children: any;
    };
}
/**
 * Builder for sparse union-typed data batches.
 */
export class SparseUnionBuilder extends AbstractUnionBuilder {
    update(value: any, index: any, child: any): void;
}
/**
 * Builder for dense union-typed data batches.
 */
export class DenseUnionBuilder extends AbstractUnionBuilder {
    init(): this;
    offsets: import("../buffer.js").Buffer;
    update(value: any, index: any, child: any): void;
    done(): {
        offsets: import("../../types.js").TypedArray;
        length: number;
        nullCount: number;
        type: any;
        typeIds: import("../../types.js").TypedArray;
        children: any;
    };
}
import { BatchBuilder } from './batch.js';
