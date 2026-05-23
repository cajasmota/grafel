// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { Adapter, Device, log } from '@luma.gl/core';
import { enforceWebGL2 } from "../context/polyfills/polyfill-webgl1-extensions.js";
import { loadSpectorJS, DEFAULT_SPECTOR_PROPS } from "../context/debug/spector.js";
import { loadWebGLDeveloperTools } from "../context/debug/webgl-developer-tools.js";
const LOG_LEVEL = 1;
export class WebGLAdapter extends Adapter {
    /** type of device's created by this adapter */
    type = 'webgl';
    constructor() {
        super();
        // Add spector default props to device default props, so that runtime settings are observed
        Device.defaultProps = { ...Device.defaultProps, ...DEFAULT_SPECTOR_PROPS };
    }
    /** Force any created WebGL contexts to be WebGL2 contexts, polyfilled with WebGL1 extensions */
    enforceWebGL2(enable) {
        enforceWebGL2(enable);
    }
    /** Check if WebGL 2 is available */
    isSupported() {
        return typeof WebGL2RenderingContext !== 'undefined';
    }
    isDeviceHandle(handle) {
        // WebGL
        if (typeof WebGL2RenderingContext !== 'undefined' && handle instanceof WebGL2RenderingContext) {
            return true;
        }
        if (typeof WebGLRenderingContext !== 'undefined' && handle instanceof WebGLRenderingContext) {
            log.warn('WebGL1 is not supported', handle)();
        }
        return false;
    }
    /**
     * Get a device instance from a GL context
     * Creates a WebGLCanvasContext against the contexts canvas
     * @note autoResize will be disabled, assuming that whoever created the external context will be handling resizes.
     * @param gl
     * @returns
     */
    async attach(gl, props = {}) {
        const { WebGLDevice } = await import("./webgl-device.js");
        if (gl instanceof WebGLDevice) {
            return gl;
        }
        // @ts-expect-error
        if (gl?.device instanceof WebGLDevice) {
            // @ts-expect-error
            return gl.device;
        }
        if (!isWebGL(gl)) {
            throw new Error('Invalid WebGL2RenderingContext');
        }
        const createCanvasContext = props.createCanvasContext === true ? {} : props.createCanvasContext;
        // We create a new device using the provided WebGL context and its canvas
        // Assume that whoever created the external context will be handling resizes.
        return new WebGLDevice({
            ...props,
            _handle: gl,
            createCanvasContext: { canvas: gl.canvas, autoResize: false, ...createCanvasContext }
        });
    }
    async create(props = {}) {
        const { WebGLDevice } = await import("./webgl-device.js");
        log.groupCollapsed(LOG_LEVEL, 'WebGLDevice created')();
        try {
            const promises = [];
            // Load webgl and spector debug scripts from CDN if requested
            if (props.debugWebGL || props.debug) {
                promises.push(loadWebGLDeveloperTools());
            }
            if (props.debugSpectorJS) {
                promises.push(loadSpectorJS(props));
            }
            // Wait for all the loads to settle before creating the context.
            // The Device.create() functions are async, so in contrast to the constructor, we can `await` here.
            const results = await Promise.allSettled(promises);
            for (const result of results) {
                if (result.status === 'rejected') {
                    log.error(`Failed to initialize debug libraries ${result.reason}`)();
                }
            }
            const device = new WebGLDevice(props);
            // Log some debug info about the newly created context
            const message = `\
${device._reused ? 'Reusing' : 'Created'} device with WebGL2 ${device.props.debug ? 'debug ' : ''}context: \
${device.info.vendor}, ${device.info.renderer} for canvas: ${device.canvasContext.id}`;
            log.probe(LOG_LEVEL, message)();
            log.table(LOG_LEVEL, device.info)();
            return device;
        }
        finally {
            log.groupEnd(LOG_LEVEL)();
        }
    }
}
/** Check if supplied parameter is a WebGL2RenderingContext */
function isWebGL(gl) {
    if (typeof WebGL2RenderingContext !== 'undefined' && gl instanceof WebGL2RenderingContext) {
        return true;
    }
    // Look for debug contexts, headless gl etc
    return Boolean(gl && Number.isFinite(gl._version));
}
export const webgl2Adapter = new WebGLAdapter();
//# sourceMappingURL=webgl-adapter.js.map