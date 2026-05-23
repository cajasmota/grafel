import { type BufferLayout } from '@luma.gl/core';
/** BufferLayoutHelper is a helper class that should not be used directly by applications */
export declare class BufferLayoutHelper {
    bufferLayouts: BufferLayout[];
    constructor(bufferLayouts: BufferLayout[]);
    getBufferLayout(name: string): BufferLayout | null;
    /** Get attribute names from a BufferLayout */
    getAttributeNamesForBuffer(bufferLayout: BufferLayout): string[];
    mergeBufferLayouts(bufferLayouts1: BufferLayout[], bufferLayouts2: BufferLayout[]): BufferLayout[];
    getBufferIndex(bufferName: string): number;
}
//# sourceMappingURL=buffer-layout-helper.d.ts.map