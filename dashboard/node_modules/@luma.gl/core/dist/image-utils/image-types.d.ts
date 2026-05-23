/**
 * Built-in data types that can be used to initialize textures
 * @note ImageData can be used for contiguous 8 bit data via Uint8ClampedArray
 */
export type ExternalImage = ImageBitmap | ImageData | HTMLImageElement | HTMLVideoElement | VideoFrame | HTMLCanvasElement | OffscreenCanvas;
export type ExternalImageData = {
    data: ArrayBuffer | SharedArrayBuffer | ArrayBufferView;
    byteOffset?: number;
    bytesPerRow?: number;
    rowsPerImage?: number;
};
/** Check if data is an external image */
export declare function isExternalImage(data: unknown): data is ExternalImage;
/** Determine size (width and height) of provided image data */
export declare function getExternalImageSize(data: ExternalImage): {
    width: number;
    height: number;
};
//# sourceMappingURL=image-types.d.ts.map