import { TypedArray } from '@math.gl/core';
/**
 * Flip rows (can be used on arrays returned from `Framebuffer.readPixels`)
 * https: *stackoverflow.com/questions/41969562/
 * how-can-i-flip-the-result-of-webglrenderingcontext-readpixels
 * @param param0
 */
export declare function flipRows(options: {
    data: TypedArray;
    width: number;
    height: number;
    bytesPerPixel?: number;
    temp?: Uint8Array;
}): void;
export declare function scalePixels(options: {
    data: TypedArray;
    width: number;
    height: number;
}): {
    data: Uint8Array;
    width: number;
    height: number;
};
//# sourceMappingURL=pixel-data-utils.d.ts.map