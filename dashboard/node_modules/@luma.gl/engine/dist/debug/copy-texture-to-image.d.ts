import { Texture, Framebuffer, TypedArray } from '@luma.gl/core';
/**
 * Options for copying texture pixels to image
 * @todo - support gl.readBuffer
 */
export type CopyTextureToImageOptions = {
    sourceAttachment?: number;
    targetMaxHeight?: number;
    targetImage?: HTMLImageElement;
};
/**
 * Reads pixels from a Framebuffer or Texture object into an HTML Image
 * @todo - can we move this to @luma.gl/core?
 * @param source
 * @param options options passed to copyToDataUrl
 * @returns
 */
export declare function copyTextureToImage(source: Texture | Framebuffer, options?: CopyTextureToImageOptions): HTMLImageElement;
/**
 * Reads pixels from a Framebuffer or Texture object to a dataUrl
 * @todo - can we move this to @luma.gl/core?
 * @param source texture or framebuffer to read from
 * @param options
 */
export declare function copyTextureToDataUrl(source: Texture | Framebuffer, options?: CopyTextureToImageOptions): string;
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
//# sourceMappingURL=copy-texture-to-image.d.ts.map