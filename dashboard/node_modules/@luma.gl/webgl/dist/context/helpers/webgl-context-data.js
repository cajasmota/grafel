// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
/**
 * Gets luma.gl specific state from a context
 * @returns context state
 */
export function getWebGLContextData(gl) {
    // @ts-expect-error
    const luma = gl.luma;
    if (!luma) {
        const contextState = {
            _polyfilled: false,
            _extensions: {}
        };
        // @ts-expect-error
        gl.luma = contextState;
    }
    // @ts-expect-error
    return gl.luma;
}
//# sourceMappingURL=webgl-context-data.js.map