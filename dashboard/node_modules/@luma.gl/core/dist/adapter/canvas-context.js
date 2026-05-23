// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { isBrowser } from '@probe.gl/env';
import { uid } from "../utils/uid.js";
import { withResolvers } from "../utils/promise-utils.js";
/**
 * Manages a canvas. Supports both HTML or offscreen canvas
 * - Creates a new canvas or looks up a canvas from the DOM
 * - Provides check for DOM loaded
 * @todo commit() @see https://github.com/w3ctag/design-reviews/issues/288
 * @todo transferControlToOffscreen: @see https://developer.mozilla.org/en-US/docs/Web/API/HTMLCanvasElement/transferControlToOffscreen
 */
export class CanvasContext {
    static isHTMLCanvas(canvas) {
        return typeof HTMLCanvasElement !== 'undefined' && canvas instanceof HTMLCanvasElement;
    }
    static isOffscreenCanvas(canvas) {
        return typeof OffscreenCanvas !== 'undefined' && canvas instanceof OffscreenCanvas;
    }
    static defaultProps = {
        id: undefined,
        canvas: null,
        width: 800,
        height: 600,
        useDevicePixels: true,
        autoResize: true,
        container: null,
        visible: true,
        alphaMode: 'opaque',
        colorSpace: 'srgb',
        trackPosition: false
    };
    id;
    props;
    canvas;
    /** Handle to HTML canvas */
    htmlCanvas;
    /** Handle to wrapped OffScreenCanvas */
    offscreenCanvas;
    type;
    /** Promise that resolved once the resize observer has updated the pixel size */
    initialized;
    isInitialized = false;
    /** Visibility is automatically updated (via an IntersectionObserver) */
    isVisible = true;
    /** Width of canvas in CSS units (tracked by a ResizeObserver) */
    cssWidth;
    /** Height of canvas in CSS units (tracked by a ResizeObserver) */
    cssHeight;
    /** Device pixel ratio. Automatically updated via media queries */
    devicePixelRatio;
    /** Exact width of canvas in physical pixels (tracked by a ResizeObserver) */
    devicePixelWidth;
    /** Exact height of canvas in physical pixels (tracked by a ResizeObserver) */
    devicePixelHeight;
    /** Width of drawing buffer: automatically tracks this.pixelWidth if props.autoResize is true */
    drawingBufferWidth;
    /** Height of drawing buffer: automatically tracks this.pixelHeight if props.autoResize is true */
    drawingBufferHeight;
    _initializedResolvers = withResolvers();
    _resizeObserver;
    _intersectionObserver;
    _position;
    destroyed = false;
    toString() {
        return `${this[Symbol.toStringTag]}(${this.id})`;
    }
    constructor(props) {
        this.props = { ...CanvasContext.defaultProps, ...props };
        props = this.props;
        this.initialized = this._initializedResolvers.promise;
        // Create a canvas element if needed
        if (!isBrowser()) {
            // TODO - does this prevent app from using jsdom style polyfills?
            this.canvas = { width: props.width || 1, height: props.height || 1 };
        }
        else if (!props.canvas) {
            this.canvas = createCanvasElement(props);
        }
        else if (typeof props.canvas === 'string') {
            this.canvas = getCanvasFromDOM(props.canvas);
        }
        else {
            this.canvas = props.canvas;
        }
        if (CanvasContext.isHTMLCanvas(this.canvas)) {
            this.id = props.id || this.canvas.id;
            this.type = 'html-canvas';
            this.htmlCanvas = this.canvas;
        }
        else if (CanvasContext.isOffscreenCanvas(this.canvas)) {
            this.id = props.id || 'offscreen-canvas';
            this.type = 'offscreen-canvas';
            this.offscreenCanvas = this.canvas;
        }
        else {
            // TODO - Node.js support is currently untested (was used for headless-gl in luma v8)
            this.id = props.id || 'node-canvas-context';
            this.type = 'node';
        }
        // Initialize size variables to some sane values (these will be updated by ResizeObserver)
        this.cssWidth = this.htmlCanvas?.clientWidth || this.canvas.width;
        this.cssHeight = this.htmlCanvas?.clientHeight || this.canvas.height;
        this.devicePixelWidth = this.canvas.width;
        this.devicePixelHeight = this.canvas.height;
        this.drawingBufferWidth = this.canvas.width;
        this.drawingBufferHeight = this.canvas.height;
        this.devicePixelRatio = globalThis.devicePixelRatio || 1;
        this._position = [0, 0];
        if (CanvasContext.isHTMLCanvas(this.canvas)) {
            // Track visibility changes
            this._intersectionObserver = new IntersectionObserver(entries => this._handleIntersection(entries));
            this._intersectionObserver.observe(this.canvas);
            // Track size changes
            this._resizeObserver = new ResizeObserver(entries => this._handleResize(entries));
            try {
                this._resizeObserver.observe(this.canvas, { box: 'device-pixel-content-box' });
            }
            catch {
                // Safari fallback
                this._resizeObserver.observe(this.canvas, { box: 'content-box' });
            }
            // Track device pixel ratio changes.
            // Defer call to after construction completes to ensure `this.device` is available.
            setTimeout(() => this._observeDevicePixelRatio(), 0);
            // Track top/left position changes
            if (this.props.trackPosition) {
                this._trackPosition();
            }
        }
    }
    destroy() {
        this.destroyed = true;
    }
    setProps(props) {
        if ('useDevicePixels' in props) {
            this.props.useDevicePixels = props.useDevicePixels || false;
            this._updateDrawingBufferSize();
        }
        return this;
    }
    // SIZE METHODS
    /**
     * Returns the size covered by the canvas in CSS pixels
     * @note This can be different from the actual device pixel size of a canvas due to DPR scaling, and rounding to integer pixels
     * @note This is independent of the canvas' internal drawing buffer size (.width, .height).
     */
    getCSSSize() {
        return [this.cssWidth, this.cssHeight];
    }
    getPosition() {
        return this._position;
    }
    /**
     * Returns the size covered by the canvas in actual device pixels.
     * @note This can be different from the 'CSS' size of a canvas due to DPR scaling, and rounding to integer pixels
     * @note This is independent of the canvas' internal drawing buffer size (.width, .height).
     */
    getDevicePixelSize() {
        return [this.devicePixelWidth, this.devicePixelHeight];
    }
    /** Get the drawing buffer size (number of pixels GPU is rendering into, can be different from CSS size) */
    getDrawingBufferSize() {
        return [this.drawingBufferWidth, this.drawingBufferHeight];
    }
    /** Returns the biggest allowed framebuffer size. @todo Allow the application to limit this? */
    getMaxDrawingBufferSize() {
        const maxTextureDimension = this.device.limits.maxTextureDimension2D;
        return [maxTextureDimension, maxTextureDimension];
    }
    /** Update the canvas drawing buffer size. Called automatically if props.autoResize is true. */
    setDrawingBufferSize(width, height) {
        this.canvas.width = width;
        this.canvas.height = height;
        this.drawingBufferWidth = width;
        this.drawingBufferHeight = height;
    }
    /**
     * Returns the current DPR (number of physical pixels per CSS pixel), if props.useDevicePixels is true
     * @note This can be a fractional (non-integer) number, e.g. when the user zooms in the browser.
     * @note This function handles the non-HTML canvas cases
     */
    getDevicePixelRatio() {
        const dpr = typeof window !== 'undefined' && window.devicePixelRatio;
        return dpr || 1;
    }
    // DEPRECATED METHODS
    /**
     * Maps CSS pixel position to device pixel position
     */
    cssToDevicePixels(cssPixel, yInvert = true) {
        const ratio = this.cssToDeviceRatio();
        const [width, height] = this.getDrawingBufferSize();
        return scalePixels(cssPixel, ratio, width, height, yInvert);
    }
    /** @deprecated - use .getDevicePixelSize() */
    getPixelSize() {
        return this.getDevicePixelSize();
    }
    /** @deprecated - TODO which values should we use for aspect */
    getAspect() {
        const [width, height] = this.getDevicePixelSize();
        return width / height;
    }
    /** @deprecated Returns multiplier need to convert CSS size to Device size */
    cssToDeviceRatio() {
        try {
            const [drawingBufferWidth] = this.getDrawingBufferSize();
            const [cssWidth] = this.getCSSSize();
            return cssWidth ? drawingBufferWidth / cssWidth : 1;
        }
        catch {
            return 1;
        }
    }
    /** @deprecated Use canvasContext.setDrawingBufferSize() */
    resize(size) {
        this.setDrawingBufferSize(size.width, size.height);
    }
    // IMPLEMENTATION
    /**
     * Allows subclass constructor to override the canvas id for auto created canvases.
     * This can really help when debugging DOM in apps that create multiple devices
     */
    _setAutoCreatedCanvasId(id) {
        if (this.htmlCanvas?.id === 'lumagl-auto-created-canvas') {
            this.htmlCanvas.id = id;
        }
    }
    /** reacts to an observed intersection */
    _handleIntersection(entries) {
        const entry = entries.find(entry_ => entry_.target === this.canvas);
        if (!entry) {
            return;
        }
        // TODO - store intersection rectangle?
        const isVisible = entry.isIntersecting;
        if (this.isVisible !== isVisible) {
            this.isVisible = isVisible;
            this.device.props.onVisibilityChange(this);
        }
    }
    /**
     * Reacts to an observed resize by using the most accurate pixel size information the browser can provide
     * @see https://web.dev/articles/device-pixel-content-box
     * @see https://webgpufundamentals.org/webgpu/lessons/webgpu-resizing-the-canvas.html
     */
    _handleResize(entries) {
        const entry = entries.find(entry_ => entry_.target === this.canvas);
        if (!entry) {
            return;
        }
        // Update CSS size using content box size
        this.cssWidth = entry.contentBoxSize[0].inlineSize;
        this.cssHeight = entry.contentBoxSize[0].blockSize;
        // Update our drawing buffer size variables, saving the old values for logging
        const oldPixelSize = this.getDevicePixelSize();
        // Use the most accurate drawing buffer size information the current browser can provide
        // Note: content box sizes are guaranteed to be integers
        // Note: Safari falls back to contentBoxSize
        const devicePixelWidth = entry.devicePixelContentBoxSize?.[0].inlineSize ||
            entry.contentBoxSize[0].inlineSize * devicePixelRatio;
        const devicePixelHeight = entry.devicePixelContentBoxSize?.[0].blockSize ||
            entry.contentBoxSize[0].blockSize * devicePixelRatio;
        // Make sure we don't overflow the maximum supported texture size
        const [maxDevicePixelWidth, maxDevicePixelHeight] = this.getMaxDrawingBufferSize();
        this.devicePixelWidth = Math.max(1, Math.min(devicePixelWidth, maxDevicePixelWidth));
        this.devicePixelHeight = Math.max(1, Math.min(devicePixelHeight, maxDevicePixelHeight));
        this._updateDrawingBufferSize();
        // Inform the device
        this.device.props.onResize(this, { oldPixelSize });
    }
    _updateDrawingBufferSize() {
        // Update the canvas drawing buffer size
        if (this.props.autoResize) {
            if (typeof this.props.useDevicePixels === 'number') {
                const dpr = this.props.useDevicePixels;
                this.setDrawingBufferSize(this.cssWidth * dpr, this.cssHeight * dpr);
            }
            else if (this.props.useDevicePixels) {
                this.setDrawingBufferSize(this.devicePixelWidth, this.devicePixelHeight);
            }
            else {
                this.setDrawingBufferSize(this.cssWidth, this.cssHeight);
            }
            // Inform the subclass
            this._updateDevice();
        }
        // Resolve the initialized promise
        this._initializedResolvers.resolve();
        this.isInitialized = true;
        this.updatePosition();
    }
    /** Monitor DPR changes */
    _observeDevicePixelRatio() {
        const oldRatio = this.devicePixelRatio;
        this.devicePixelRatio = window.devicePixelRatio;
        this.updatePosition();
        // Inform the device
        this.device.props.onDevicePixelRatioChange(this, { oldRatio });
        // Set up a one time query against the current resolution.
        matchMedia(`(resolution: ${this.devicePixelRatio}dppx)`).addEventListener('change', () => this._observeDevicePixelRatio(), { once: true });
    }
    /** Start tracking positions with a timer */
    _trackPosition(intervalMs = 100) {
        const intervalId = setInterval(() => {
            if (this.destroyed) {
                clearInterval(intervalId);
            }
            else {
                this.updatePosition();
            }
        }, intervalMs);
    }
    /**
     * Calculated the absolute position of the canvas
     * @note - getBoundingClientRect() is normally cheap but can be expensive
     * if called before browser has finished a reflow. Should not be the case here.
     */
    updatePosition() {
        const newRect = this.htmlCanvas?.getBoundingClientRect();
        if (newRect) {
            // We only track position since we rely on the more precise ResizeObserver for size
            const position = [newRect.left, newRect.top];
            this._position ??= position;
            const positionChanged = position[0] !== this._position[0] || position[1] !== this._position[1];
            if (positionChanged) {
                const oldPosition = this._position;
                this._position = position;
                this.device.props.onPositionChange?.(this, { oldPosition });
            }
        }
    }
}
// HELPER FUNCTIONS
/** Get a container element from a string or DOM element */
function getContainer(container) {
    if (typeof container === 'string') {
        const element = document.getElementById(container);
        if (!element) {
            throw new Error(`${container} is not an HTML element`);
        }
        return element;
    }
    if (container) {
        return container;
    }
    return document.body;
}
/** Get a Canvas element from DOM id */
function getCanvasFromDOM(canvasId) {
    const canvas = document.getElementById(canvasId);
    if (!CanvasContext.isHTMLCanvas(canvas)) {
        throw new Error('Object is not a canvas element');
    }
    return canvas;
}
/** Create a new canvas */
function createCanvasElement(props) {
    const { width, height } = props;
    const newCanvas = document.createElement('canvas');
    newCanvas.id = uid('lumagl-auto-created-canvas');
    newCanvas.width = width || 1;
    newCanvas.height = height || 1;
    newCanvas.style.width = Number.isFinite(width) ? `${width}px` : '100%';
    newCanvas.style.height = Number.isFinite(height) ? `${height}px` : '100%';
    if (!props?.visible) {
        newCanvas.style.visibility = 'hidden';
    }
    // Insert the canvas in the DOM
    const container = getContainer(props?.container || null);
    container.insertBefore(newCanvas, container.firstChild);
    return newCanvas;
}
/**
 * Scales pixels linearly, handles edge cases
 * @param pixel
 * @param ratio
 * @param width
 * @param height
 * @param yInvert
 * @returns
 */
function scalePixels(pixel, ratio, width, height, yInvert) {
    const point = pixel;
    const x = scaleX(point[0], ratio, width);
    let y = scaleY(point[1], ratio, height, yInvert);
    // Find boundaries of next pixel to provide valid range of device pixel locations
    let t = scaleX(point[0] + 1, ratio, width);
    // If next pixel's position is clamped to boundary, use it as is, otherwise subtract 1 for current pixel boundary
    const xHigh = t === width - 1 ? t : t - 1;
    t = scaleY(point[1] + 1, ratio, height, yInvert);
    let yHigh;
    if (yInvert) {
        // If next pixel's position is clamped to boundary, use it as is, otherwise clamp it to valid range
        t = t === 0 ? t : t + 1;
        // swap y and yHigh
        yHigh = y;
        y = t;
    }
    else {
        // If next pixel's position is clamped to boundary, use it as is, otherwise clamp it to valid range
        yHigh = t === height - 1 ? t : t - 1;
        // y remains same
    }
    return {
        x,
        y,
        // when ratio < 1, current css pixel and next css pixel may point to same device pixel, set width/height to 1 in those cases.
        width: Math.max(xHigh - x + 1, 1),
        height: Math.max(yHigh - y + 1, 1)
    };
}
function scaleX(x, ratio, width) {
    // since we are rounding to nearest, when ratio > 1, edge pixels may point to out of bounds value, clamp to the limit
    const r = Math.min(Math.round(x * ratio), width - 1);
    return r;
}
function scaleY(y, ratio, height, yInvert) {
    // since we are rounding to nearest, when ratio > 1, edge pixels may point to out of bounds value, clamp to the limit
    return yInvert
        ? Math.max(0, height - 1 - Math.round(y * ratio))
        : Math.min(Math.round(y * ratio), height - 1);
}
//# sourceMappingURL=canvas-context.js.map