/**
 * Builder for run-end-encoded-typed data batches.
 */
export class RunEndEncodedBuilder extends BatchBuilder {
    children: any;
    init(): this;
    pos: number;
    key: any;
    value: any;
    next(): void;
    set(value: any, index: any): void;
    done(): {
        length: number;
        nullCount: number;
        type: any;
        children: any;
    };
}
import { BatchBuilder } from './batch.js';
