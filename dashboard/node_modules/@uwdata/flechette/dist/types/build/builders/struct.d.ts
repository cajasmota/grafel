/**
 * Abstract class for building list-typed data batches.
 */
export class AbstractStructBuilder extends ValidityBuilder {
    children: any;
    init(): this;
    done(): {
        children: any;
        length: number;
        nullCount: number;
        type: any;
        validity: import("../../types.js").TypedArray;
    };
}
/**
 * Builder for struct-typed data batches.
 */
export class StructBuilder extends AbstractStructBuilder {
    setters: any;
    set(value: any, index: any): void;
}
import { ValidityBuilder } from './validity.js';
