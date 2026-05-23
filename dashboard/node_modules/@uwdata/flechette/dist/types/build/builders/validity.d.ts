/**
 * Builder for validity bitmaps within batches.
 */
export class ValidityBuilder extends BatchBuilder {
    init(): this;
    nullCount: number;
    validity: import("../buffer.js").Bitmap;
    done(): {
        length: number;
        nullCount: number;
        type: any;
        validity: import("../../types.js").TypedArray;
    };
}
import { BatchBuilder } from './batch.js';
