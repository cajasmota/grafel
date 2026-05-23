/**
 * Set a relative path prefix
 */
export declare function setPathPrefix(prefix: string): void;
/**
 * Loads ImageBitmap asynchronously. Respects setPathPrefix.
 * image.crossOrigin can be set via opts.crossOrigin, default to 'anonymous'
 * @returns a promise tracking the load
 */
export declare function loadImageBitmap(url: string, opts?: {
    crossOrigin?: string;
} & ImageBitmapOptions): Promise<ImageBitmap>;
/**
 * Loads image asynchronously. Respects setPathPrefix.
 * image.crossOrigin can be set via opts.crossOrigin, default to 'anonymous'
 * @returns a promise tracking the load
 * @deprecated Use `loadImageBitmap()` unless you are supporting old versions of Safari.
 */
export declare function loadImage(url: string, opts?: {
    crossOrigin?: string;
}): Promise<HTMLImageElement>;
//# sourceMappingURL=load-file.d.ts.map