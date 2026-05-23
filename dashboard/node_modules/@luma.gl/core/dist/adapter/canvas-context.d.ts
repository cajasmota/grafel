import type { Device } from "./device.js";
import type { Framebuffer } from "./resources/framebuffer.js";
import type { TextureFormatDepthStencil } from "../shadertypes/textures/texture-formats.js";
/** Properties for a CanvasContext */
export type CanvasContextProps = {
    /** Identifier, for debugging */
    id?: string;
    /** If a canvas not supplied, one will be created and added to the DOM. If a string, a canvas with that id will be looked up in the DOM */
    canvas?: HTMLCanvasElement | OffscreenCanvas | string | null;
    /** If new canvas is created, it will be created in the specified container, otherwise is appended as a child of document.body */
    container?: HTMLElement | string | null;
    /** Width in pixels of the canvas - used when creating a new canvas */
    width?: number;
    /** Height in pixels of the canvas - used when creating a new canvas */
    height?: number;
    /** Visibility (only used if new canvas is created). */
    visible?: boolean;
    /** Whether to size the drawing buffer to the pixel size during auto resize. If a number is provided it is used as a static pixel ratio */
    useDevicePixels?: boolean | number;
    /** Whether to track window resizes. */
    autoResize?: boolean;
    /** @see https://developer.mozilla.org/en-US/docs/Web/API/GPUCanvasContext/configure#alphamode */
    alphaMode?: 'opaque' | 'premultiplied';
    /** @see https://developer.mozilla.org/en-US/docs/Web/API/GPUCanvasContext/configure#colorspace */
    colorSpace?: 'srgb';
    /** Whether to track position changes. Calls this.device.onPositionChange */
    trackPosition?: boolean;
};
export type MutableCanvasContextProps = {
    /** Whether to size the drawing buffer to the pixel size during auto resize. If a number is provided it is used as a static pixel ratio */
    useDevicePixels?: boolean | number;
};
/**
 * Manages a canvas. Supports both HTML or offscreen canvas
 * - Creates a new canvas or looks up a canvas from the DOM
 * - Provides check for DOM loaded
 * @todo commit() @see https://github.com/w3ctag/design-reviews/issues/288
 * @todo transferControlToOffscreen: @see https://developer.mozilla.org/en-US/docs/Web/API/HTMLCanvasElement/transferControlToOffscreen
 */
export declare abstract class CanvasContext {
    static isHTMLCanvas(canvas: unknown): canvas is HTMLCanvasElement;
    static isOffscreenCanvas(canvas: unknown): canvas is OffscreenCanvas;
    static defaultProps: Required<CanvasContextProps>;
    abstract readonly device: Device;
    abstract readonly handle: unknown;
    readonly id: string;
    readonly props: Required<CanvasContextProps>;
    readonly canvas: HTMLCanvasElement | OffscreenCanvas;
    /** Handle to HTML canvas */
    readonly htmlCanvas?: HTMLCanvasElement;
    /** Handle to wrapped OffScreenCanvas */
    readonly offscreenCanvas?: OffscreenCanvas;
    readonly type: 'html-canvas' | 'offscreen-canvas' | 'node';
    /** Promise that resolved once the resize observer has updated the pixel size */
    initialized: Promise<void>;
    isInitialized: boolean;
    /** Visibility is automatically updated (via an IntersectionObserver) */
    isVisible: boolean;
    /** Width of canvas in CSS units (tracked by a ResizeObserver) */
    cssWidth: number;
    /** Height of canvas in CSS units (tracked by a ResizeObserver) */
    cssHeight: number;
    /** Device pixel ratio. Automatically updated via media queries */
    devicePixelRatio: number;
    /** Exact width of canvas in physical pixels (tracked by a ResizeObserver) */
    devicePixelWidth: number;
    /** Exact height of canvas in physical pixels (tracked by a ResizeObserver) */
    devicePixelHeight: number;
    /** Width of drawing buffer: automatically tracks this.pixelWidth if props.autoResize is true */
    drawingBufferWidth: number;
    /** Height of drawing buffer: automatically tracks this.pixelHeight if props.autoResize is true */
    drawingBufferHeight: number;
    protected _initializedResolvers: {
        promise: Promise<void>;
        resolve: (t: void) => void;
        reject: (error: Error) => void;
    };
    protected readonly _resizeObserver: ResizeObserver | undefined;
    protected readonly _intersectionObserver: IntersectionObserver | undefined;
    protected _position: [number, number];
    protected destroyed: boolean;
    abstract get [Symbol.toStringTag](): string;
    toString(): string;
    constructor(props?: CanvasContextProps);
    destroy(): void;
    setProps(props: MutableCanvasContextProps): this;
    /** Returns a framebuffer with properly resized current 'swap chain' textures */
    abstract getCurrentFramebuffer(options?: {
        depthStencilFormat?: TextureFormatDepthStencil | false;
    }): Framebuffer;
    /**
     * Returns the size covered by the canvas in CSS pixels
     * @note This can be different from the actual device pixel size of a canvas due to DPR scaling, and rounding to integer pixels
     * @note This is independent of the canvas' internal drawing buffer size (.width, .height).
     */
    getCSSSize(): [number, number];
    getPosition(): [number, number];
    /**
     * Returns the size covered by the canvas in actual device pixels.
     * @note This can be different from the 'CSS' size of a canvas due to DPR scaling, and rounding to integer pixels
     * @note This is independent of the canvas' internal drawing buffer size (.width, .height).
     */
    getDevicePixelSize(): [number, number];
    /** Get the drawing buffer size (number of pixels GPU is rendering into, can be different from CSS size) */
    getDrawingBufferSize(): [number, number];
    /** Returns the biggest allowed framebuffer size. @todo Allow the application to limit this? */
    getMaxDrawingBufferSize(): [number, number];
    /** Update the canvas drawing buffer size. Called automatically if props.autoResize is true. */
    setDrawingBufferSize(width: number, height: number): void;
    /**
     * Returns the current DPR (number of physical pixels per CSS pixel), if props.useDevicePixels is true
     * @note This can be a fractional (non-integer) number, e.g. when the user zooms in the browser.
     * @note This function handles the non-HTML canvas cases
     */
    getDevicePixelRatio(): number;
    /**
     * Maps CSS pixel position to device pixel position
     */
    cssToDevicePixels(cssPixel: [number, number], yInvert?: boolean): {
        x: number;
        y: number;
        width: number;
        height: number;
    };
    /** @deprecated - use .getDevicePixelSize() */
    getPixelSize(): [number, number];
    /** @deprecated - TODO which values should we use for aspect */
    getAspect(): number;
    /** @deprecated Returns multiplier need to convert CSS size to Device size */
    cssToDeviceRatio(): number;
    /** @deprecated Use canvasContext.setDrawingBufferSize() */
    resize(size: {
        width: number;
        height: number;
    }): void;
    /**
     * Performs platform specific updates (WebGPU vs WebGL)
     * Can be called after changes to size or props,
     * to give implementation an opportunity to update configurations.
     */
    protected abstract _updateDevice(): void;
    /**
     * Allows subclass constructor to override the canvas id for auto created canvases.
     * This can really help when debugging DOM in apps that create multiple devices
     */
    protected _setAutoCreatedCanvasId(id: string): void;
    /** reacts to an observed intersection */
    protected _handleIntersection(entries: IntersectionObserverEntry[]): void;
    /**
     * Reacts to an observed resize by using the most accurate pixel size information the browser can provide
     * @see https://web.dev/articles/device-pixel-content-box
     * @see https://webgpufundamentals.org/webgpu/lessons/webgpu-resizing-the-canvas.html
     */
    protected _handleResize(entries: ResizeObserverEntry[]): void;
    protected _updateDrawingBufferSize(): void;
    /** Monitor DPR changes */
    _observeDevicePixelRatio(): void;
    /** Start tracking positions with a timer */
    _trackPosition(intervalMs?: number): void;
    /**
     * Calculated the absolute position of the canvas
     * @note - getBoundingClientRect() is normally cheap but can be expensive
     * if called before browser has finished a reflow. Should not be the case here.
     */
    updatePosition(): void;
}
//# sourceMappingURL=canvas-context.d.ts.map