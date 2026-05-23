/**
 * Abstract class for building list data batches.
 */
export class AbstractListBuilder extends ValidityBuilder {
    constructor(type: any, ctx: any, child: any);
    child: any;
    init(): this;
    offsets: import("../buffer.js").Buffer;
    toOffset: typeof import("../../util/numbers.js").toBigInt | typeof import("../../util/numbers.js").identity;
    pos: number;
    done(): {
        offsets: import("../../types.js").TypedArray;
        children: any[];
        length: number;
        nullCount: number;
        type: any;
        validity: import("../../types.js").TypedArray;
    };
}
/**
 * Builder for list-typed data batches.
 */
export class ListBuilder extends AbstractListBuilder {
    constructor(type: any, ctx: any);
    set(value: any, index: any): void;
}
import { ValidityBuilder } from './validity.js';
