// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
/**
 * Create a WebGL context for a canvas
 * Note calling this multiple time on the same canvas does return the same context
 * @param canvas A canvas element or offscreen canvas
 */
export function createBrowserContext(canvas, props, webglContextAttributes) {
    // Try to extract any extra information about why context creation failed
    let errorMessage = '';
    // const onCreateError = error => (errorMessage = error.statusMessage || errorMessage);
    // Avoid multiple listeners?
    // canvas.removeEventListener('webglcontextcreationerror', onCreateError, false);
    // canvas.addEventListener('webglcontextcreationerror', onCreateError, false);
    const webglProps = {
        preserveDrawingBuffer: true,
        // failIfMajorPerformanceCaveat: true,
        ...webglContextAttributes
    };
    // Create the desired context
    let gl = null;
    // Create a webgl2 context
    gl ||= canvas.getContext('webgl2', webglProps);
    if (webglProps.failIfMajorPerformanceCaveat) {
        errorMessage ||=
            'Only software GPU is available. Set `failIfMajorPerformanceCaveat: false` to allow.';
    }
    // Creation failed with failIfMajorPerformanceCaveat - Try a Software GPU
    if (!gl && !webglContextAttributes.failIfMajorPerformanceCaveat) {
        webglProps.failIfMajorPerformanceCaveat = false;
        gl = canvas.getContext('webgl2', webglProps);
        // @ts-expect-error
        gl.luma ||= {};
        // @ts-expect-error
        gl.luma.softwareRenderer = true;
    }
    if (!gl) {
        gl = canvas.getContext('webgl', {});
        if (gl) {
            gl = null;
            errorMessage ||= 'Your browser only supports WebGL1';
        }
    }
    if (!gl) {
        errorMessage ||= 'Your browser does not support WebGL';
        throw new Error(`Failed to create WebGL context: ${errorMessage}`);
    }
    // Carefully extract and wrap callbacks to prevent addEventListener from rebinding them.
    const { onContextLost, onContextRestored } = props;
    canvas.addEventListener('webglcontextlost', (event) => onContextLost(event), false);
    canvas.addEventListener('webglcontextrestored', (event) => onContextRestored(event), false);
    // @ts-expect-error
    gl.luma ||= {};
    return gl;
}
/* TODO - can we call this asynchronously to catch the error events?
export async function createBrowserContextAsync(canvas: HTMLCanvasElement | OffscreenCanvas, props: ContextProps): Promise<WebGL2RenderingContext> {
  props = {...DEFAULT_CONTEXT_PROPS, ...props};

 // Try to extract any extra information about why context creation failed
 let errorMessage = null;
 const onCreateError = (error) => (errorMessage = error.statusMessage || errorMessage);
 canvas.addEventListener('webglcontextcreationerror', onCreateError, false);

 const gl = createBrowserContext(canvas, props);

 // Give the listener a chance to fire
 await new Promise(resolve => setTimeout(resolve, 0));

 canvas.removeEventListener('webglcontextcreationerror', onCreateError, false);

 return gl;
}
*/
//# sourceMappingURL=create-browser-context.js.map