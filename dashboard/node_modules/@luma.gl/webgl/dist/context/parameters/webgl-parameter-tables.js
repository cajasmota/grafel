// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
// @ts-nocheck TODO fix
// Tables describing WebGL parameters
import { GL } from '@luma.gl/constants';
// DEFAULT SETTINGS - FOR FAST CACHE INITIALIZATION AND CONTEXT RESETS
/* eslint-disable no-shadow */
export const GL_PARAMETER_DEFAULTS = {
    [3042]: false,
    [32773]: new Float32Array([0, 0, 0, 0]),
    [32777]: 32774,
    [34877]: 32774,
    [32969]: 1,
    [32968]: 0,
    [32971]: 1,
    [32970]: 0,
    [3106]: new Float32Array([0, 0, 0, 0]), // TBD
    [3107]: [true, true, true, true],
    [2884]: false,
    [2885]: 1029,
    [2929]: false,
    [2931]: 1,
    [2932]: 513,
    [2928]: new Float32Array([0, 1]), // TBD
    [2930]: true,
    [3024]: true,
    [35725]: null,
    // FRAMEBUFFER_BINDING and DRAW_FRAMEBUFFER_BINDING(WebGL2) refer same state.
    [36006]: null,
    [36007]: null,
    [34229]: null,
    [34964]: null,
    [2886]: 2305,
    [33170]: 4352,
    [2849]: 1,
    [32823]: false,
    [32824]: 0,
    [10752]: 0,
    [32926]: false,
    [32928]: false,
    [32938]: 1.0,
    [32939]: false,
    [3089]: false,
    // Note: Dynamic value. If scissor test enabled we expect users to set correct scissor box
    [3088]: new Int32Array([0, 0, 1024, 1024]),
    [2960]: false,
    [2961]: 0,
    [2968]: 0xffffffff,
    [36005]: 0xffffffff,
    [2962]: 519,
    [2967]: 0,
    [2963]: 0xffffffff,
    [34816]: 519,
    [36003]: 0,
    [36004]: 0xffffffff,
    [2964]: 7680,
    [2965]: 7680,
    [2966]: 7680,
    [34817]: 7680,
    [34818]: 7680,
    [34819]: 7680,
    // Dynamic value: We use [0, 0, 1024, 1024] as default, but usually this is updated in each frame.
    [2978]: [0, 0, 1024, 1024],
    [36389]: null,
    [36662]: null,
    [36663]: null,
    [35053]: null,
    [35055]: null,
    [35723]: 4352,
    [36010]: null,
    [35977]: false,
    [3333]: 4,
    [3317]: 4,
    [37440]: false,
    [37441]: false,
    [37443]: 37444,
    [3330]: 0,
    [3332]: 0,
    [3331]: 0,
    [3314]: 0,
    [32878]: 0,
    [3316]: 0,
    [3315]: 0,
    [32877]: 0
};
// SETTER TABLES - ENABLES SETTING ANY PARAMETER WITH A COMMON API
const enable = (gl, value, key) => value ? gl.enable(key) : gl.disable(key);
const hint = (gl, value, key) => gl.hint(key, value);
const pixelStorei = (gl, value, key) => gl.pixelStorei(key, value);
const bindFramebuffer = (gl, value, key) => {
    const target = key === 36006 ? 36009 : 36008;
    return gl.bindFramebuffer(target, value);
};
const bindBuffer = (gl, value, key) => {
    const bindingMap = {
        [34964]: 34962,
        [36662]: 36662,
        [36663]: 36663,
        [35053]: 35051,
        [35055]: 35052
    };
    const glTarget = bindingMap[key];
    gl.bindBuffer(glTarget, value);
};
// Utility
function isArray(array) {
    return Array.isArray(array) || (ArrayBuffer.isView(array) && !(array instanceof DataView));
}
// Map from WebGL parameter names to corresponding WebGL setter functions
// WegGL constants are read by parameter names, but set by function names
// NOTE: When value type is a string, it will be handled by 'GL_COMPOSITE_PARAMETER_SETTERS'
export const GL_PARAMETER_SETTERS = {
    [3042]: enable,
    [32773]: (gl, value) => gl.blendColor(...value),
    [32777]: 'blendEquation',
    [34877]: 'blendEquation',
    [32969]: 'blendFunc',
    [32968]: 'blendFunc',
    [32971]: 'blendFunc',
    [32970]: 'blendFunc',
    [3106]: (gl, value) => gl.clearColor(...value),
    [3107]: (gl, value) => gl.colorMask(...value),
    [2884]: enable,
    [2885]: (gl, value) => gl.cullFace(value),
    [2929]: enable,
    [2931]: (gl, value) => gl.clearDepth(value),
    [2932]: (gl, value) => gl.depthFunc(value),
    [2928]: (gl, value) => gl.depthRange(...value),
    [2930]: (gl, value) => gl.depthMask(value),
    [3024]: enable,
    [35723]: hint,
    [35725]: (gl, value) => gl.useProgram(value),
    [36007]: (gl, value) => gl.bindRenderbuffer(36161, value),
    [36389]: (gl, value) => gl.bindTransformFeedback?.(36386, value),
    [34229]: (gl, value) => gl.bindVertexArray(value),
    // NOTE: FRAMEBUFFER_BINDING and DRAW_FRAMEBUFFER_BINDING(WebGL2) refer same state.
    [36006]: bindFramebuffer,
    [36010]: bindFramebuffer,
    // Buffers
    [34964]: bindBuffer,
    [36662]: bindBuffer,
    [36663]: bindBuffer,
    [35053]: bindBuffer,
    [35055]: bindBuffer,
    [2886]: (gl, value) => gl.frontFace(value),
    [33170]: hint,
    [2849]: (gl, value) => gl.lineWidth(value),
    [32823]: enable,
    [32824]: 'polygonOffset',
    [10752]: 'polygonOffset',
    [35977]: enable,
    [32926]: enable,
    [32928]: enable,
    [32938]: 'sampleCoverage',
    [32939]: 'sampleCoverage',
    [3089]: enable,
    [3088]: (gl, value) => gl.scissor(...value),
    [2960]: enable,
    [2961]: (gl, value) => gl.clearStencil(value),
    [2968]: (gl, value) => gl.stencilMaskSeparate(1028, value),
    [36005]: (gl, value) => gl.stencilMaskSeparate(1029, value),
    [2962]: 'stencilFuncFront',
    [2967]: 'stencilFuncFront',
    [2963]: 'stencilFuncFront',
    [34816]: 'stencilFuncBack',
    [36003]: 'stencilFuncBack',
    [36004]: 'stencilFuncBack',
    [2964]: 'stencilOpFront',
    [2965]: 'stencilOpFront',
    [2966]: 'stencilOpFront',
    [34817]: 'stencilOpBack',
    [34818]: 'stencilOpBack',
    [34819]: 'stencilOpBack',
    [2978]: (gl, value) => gl.viewport(...value),
    // WEBGL2 EXTENSIONS
    // EXT_depth_clamp https://registry.khronos.org/webgl/extensions/EXT_depth_clamp/
    [34383]: enable,
    // WEBGL_provoking_vertex https://registry.khronos.org/webgl/extensions/WEBGL_provoking_vertex/
    // [GL.PROVOKING_VERTEX_WEBL]: TODO - extension function needed
    // WEBGL_polygon_mode https://registry.khronos.org/webgl/extensions/WEBGL_polygon_mode/
    // POLYGON_MODE_WEBGL  TODO - extension function needed
    [10754]: enable,
    // WEBGL_clip_cull_distance https://registry.khronos.org/webgl/extensions/WEBGL_clip_cull_distance/
    [12288]: enable,
    [12289]: enable,
    [12290]: enable,
    [12291]: enable,
    [12292]: enable,
    [12293]: enable,
    [12294]: enable,
    [12295]: enable,
    // PIXEL PACK/UNPACK MODES
    [3333]: pixelStorei,
    [3317]: pixelStorei,
    [37440]: pixelStorei,
    [37441]: pixelStorei,
    [37443]: pixelStorei,
    [3330]: pixelStorei,
    [3332]: pixelStorei,
    [3331]: pixelStorei,
    [3314]: pixelStorei,
    [32878]: pixelStorei,
    [3316]: pixelStorei,
    [3315]: pixelStorei,
    [32877]: pixelStorei,
    // Function-style setters
    framebuffer: (gl, framebuffer) => {
        // accepts 1) a WebGLFramebuffer 2) null (default framebuffer), or 3) luma.gl Framebuffer class
        // framebuffer is null when restoring to default framebuffer, otherwise use the WebGL handle.
        const handle = framebuffer && 'handle' in framebuffer ? framebuffer.handle : framebuffer;
        return gl.bindFramebuffer(36160, handle);
    },
    blend: (gl, value) => value ? gl.enable(3042) : gl.disable(3042),
    blendColor: (gl, value) => gl.blendColor(...value),
    blendEquation: (gl, args) => {
        const separateModes = typeof args === 'number' ? [args, args] : args;
        gl.blendEquationSeparate(...separateModes);
    },
    blendFunc: (gl, args) => {
        const separateFuncs = args?.length === 2 ? [...args, ...args] : args;
        gl.blendFuncSeparate(...separateFuncs);
    },
    clearColor: (gl, value) => gl.clearColor(...value),
    clearDepth: (gl, value) => gl.clearDepth(value),
    clearStencil: (gl, value) => gl.clearStencil(value),
    colorMask: (gl, value) => gl.colorMask(...value),
    cull: (gl, value) => value ? gl.enable(2884) : gl.disable(2884),
    cullFace: (gl, value) => gl.cullFace(value),
    depthTest: (gl, value) => value ? gl.enable(2929) : gl.disable(2929),
    depthFunc: (gl, value) => gl.depthFunc(value),
    depthMask: (gl, value) => gl.depthMask(value),
    depthRange: (gl, value) => gl.depthRange(...value),
    dither: (gl, value) => value ? gl.enable(3024) : gl.disable(3024),
    derivativeHint: (gl, value) => {
        // gl1: 'OES_standard_derivatives'
        gl.hint(35723, value);
    },
    frontFace: (gl, value) => gl.frontFace(value),
    mipmapHint: (gl, value) => gl.hint(33170, value),
    lineWidth: (gl, value) => gl.lineWidth(value),
    polygonOffsetFill: (gl, value) => value ? gl.enable(32823) : gl.disable(32823),
    polygonOffset: (gl, value) => gl.polygonOffset(...value),
    sampleCoverage: (gl, value) => gl.sampleCoverage(value[0], value[1] || false),
    scissorTest: (gl, value) => value ? gl.enable(3089) : gl.disable(3089),
    scissor: (gl, value) => gl.scissor(...value),
    stencilTest: (gl, value) => value ? gl.enable(2960) : gl.disable(2960),
    stencilMask: (gl, value) => {
        value = isArray(value) ? value : [value, value];
        const [mask, backMask] = value;
        gl.stencilMaskSeparate(1028, mask);
        gl.stencilMaskSeparate(1029, backMask);
    },
    stencilFunc: (gl, args) => {
        args = isArray(args) && args.length === 3 ? [...args, ...args] : args;
        const [func, ref, mask, backFunc, backRef, backMask] = args;
        gl.stencilFuncSeparate(1028, func, ref, mask);
        gl.stencilFuncSeparate(1029, backFunc, backRef, backMask);
    },
    stencilOp: (gl, args) => {
        args = isArray(args) && args.length === 3 ? [...args, ...args] : args;
        const [sfail, dpfail, dppass, backSfail, backDpfail, backDppass] = args;
        gl.stencilOpSeparate(1028, sfail, dpfail, dppass);
        gl.stencilOpSeparate(1029, backSfail, backDpfail, backDppass);
    },
    viewport: (gl, value) => gl.viewport(...value)
};
function getValue(glEnum, values, cache) {
    return values[glEnum] !== undefined ? values[glEnum] : cache[glEnum];
}
// COMPOSITE_WEBGL_PARAMETER_
export const GL_COMPOSITE_PARAMETER_SETTERS = {
    blendEquation: (gl, values, cache) => gl.blendEquationSeparate(getValue(32777, values, cache), getValue(34877, values, cache)),
    blendFunc: (gl, values, cache) => gl.blendFuncSeparate(getValue(32969, values, cache), getValue(32968, values, cache), getValue(32971, values, cache), getValue(32970, values, cache)),
    polygonOffset: (gl, values, cache) => gl.polygonOffset(getValue(32824, values, cache), getValue(10752, values, cache)),
    sampleCoverage: (gl, values, cache) => gl.sampleCoverage(getValue(32938, values, cache), getValue(32939, values, cache)),
    stencilFuncFront: (gl, values, cache) => gl.stencilFuncSeparate(1028, getValue(2962, values, cache), getValue(2967, values, cache), getValue(2963, values, cache)),
    stencilFuncBack: (gl, values, cache) => gl.stencilFuncSeparate(1029, getValue(34816, values, cache), getValue(36003, values, cache), getValue(36004, values, cache)),
    stencilOpFront: (gl, values, cache) => gl.stencilOpSeparate(1028, getValue(2964, values, cache), getValue(2965, values, cache), getValue(2966, values, cache)),
    stencilOpBack: (gl, values, cache) => gl.stencilOpSeparate(1029, getValue(34817, values, cache), getValue(34818, values, cache), getValue(34819, values, cache))
};
// Setter functions intercepted for cache updates
export const GL_HOOKED_SETTERS = {
    // GENERIC SETTERS
    enable: (update, capability) => update({
        [capability]: true
    }),
    disable: (update, capability) => update({
        [capability]: false
    }),
    pixelStorei: (update, pname, value) => update({
        [pname]: value
    }),
    hint: (update, pname, value) => update({
        [pname]: value
    }),
    // SPECIFIC SETTERS
    useProgram: (update, value) => update({
        [35725]: value
    }),
    bindRenderbuffer: (update, target, value) => update({
        [36007]: value
    }),
    bindTransformFeedback: (update, target, value) => update({
        [36389]: value
    }),
    bindVertexArray: (update, value) => update({
        [34229]: value
    }),
    bindFramebuffer: (update, target, framebuffer) => {
        switch (target) {
            case 36160:
                return update({
                    [36006]: framebuffer,
                    [36010]: framebuffer
                });
            case 36009:
                return update({ [36006]: framebuffer });
            case 36008:
                return update({ [36010]: framebuffer });
            default:
                return null;
        }
    },
    bindBuffer: (update, target, buffer) => {
        const pname = {
            [34962]: [34964],
            [36662]: [36662],
            [36663]: [36663],
            [35051]: [35053],
            [35052]: [35055]
        }[target];
        if (pname) {
            return update({ [pname]: buffer });
        }
        // targets that should not be cached
        return { valueChanged: true };
    },
    blendColor: (update, r, g, b, a) => update({
        [32773]: new Float32Array([r, g, b, a])
    }),
    blendEquation: (update, mode) => update({
        [32777]: mode,
        [34877]: mode
    }),
    blendEquationSeparate: (update, modeRGB, modeAlpha) => update({
        [32777]: modeRGB,
        [34877]: modeAlpha
    }),
    blendFunc: (update, src, dst) => update({
        [32969]: src,
        [32968]: dst,
        [32971]: src,
        [32970]: dst
    }),
    blendFuncSeparate: (update, srcRGB, dstRGB, srcAlpha, dstAlpha) => update({
        [32969]: srcRGB,
        [32968]: dstRGB,
        [32971]: srcAlpha,
        [32970]: dstAlpha
    }),
    clearColor: (update, r, g, b, a) => update({
        [3106]: new Float32Array([r, g, b, a])
    }),
    clearDepth: (update, depth) => update({
        [2931]: depth
    }),
    clearStencil: (update, s) => update({
        [2961]: s
    }),
    colorMask: (update, r, g, b, a) => update({
        [3107]: [r, g, b, a]
    }),
    cullFace: (update, mode) => update({
        [2885]: mode
    }),
    depthFunc: (update, func) => update({
        [2932]: func
    }),
    depthRange: (update, zNear, zFar) => update({
        [2928]: new Float32Array([zNear, zFar])
    }),
    depthMask: (update, mask) => update({
        [2930]: mask
    }),
    frontFace: (update, face) => update({
        [2886]: face
    }),
    lineWidth: (update, width) => update({
        [2849]: width
    }),
    polygonOffset: (update, factor, units) => update({
        [32824]: factor,
        [10752]: units
    }),
    sampleCoverage: (update, value, invert) => update({
        [32938]: value,
        [32939]: invert
    }),
    scissor: (update, x, y, width, height) => update({
        [3088]: new Int32Array([x, y, width, height])
    }),
    stencilMask: (update, mask) => update({
        [2968]: mask,
        [36005]: mask
    }),
    stencilMaskSeparate: (update, face, mask) => update({
        [face === 1028 ? 2968 : 36005]: mask
    }),
    stencilFunc: (update, func, ref, mask) => update({
        [2962]: func,
        [2967]: ref,
        [2963]: mask,
        [34816]: func,
        [36003]: ref,
        [36004]: mask
    }),
    stencilFuncSeparate: (update, face, func, ref, mask) => update({
        [face === 1028 ? 2962 : 34816]: func,
        [face === 1028 ? 2967 : 36003]: ref,
        [face === 1028 ? 2963 : 36004]: mask
    }),
    stencilOp: (update, fail, zfail, zpass) => update({
        [2964]: fail,
        [2965]: zfail,
        [2966]: zpass,
        [34817]: fail,
        [34818]: zfail,
        [34819]: zpass
    }),
    stencilOpSeparate: (update, face, fail, zfail, zpass) => update({
        [face === 1028 ? 2964 : 34817]: fail,
        [face === 1028 ? 2965 : 34818]: zfail,
        [face === 1028 ? 2966 : 34819]: zpass
    }),
    viewport: (update, x, y, width, height) => update({
        [2978]: [x, y, width, height]
    })
};
// GETTER TABLE - FOR READING OUT AN ENTIRE CONTEXT
const isEnabled = (gl, key) => gl.isEnabled(key);
// Exceptions for any keys that cannot be queried by gl.getParameters
export const GL_PARAMETER_GETTERS = {
    [3042]: isEnabled,
    [2884]: isEnabled,
    [2929]: isEnabled,
    [3024]: isEnabled,
    [32823]: isEnabled,
    [32926]: isEnabled,
    [32928]: isEnabled,
    [3089]: isEnabled,
    [2960]: isEnabled,
    [35977]: isEnabled
};
export const NON_CACHE_PARAMETERS = new Set([
    34016,
    36388,
    36387,
    35983,
    35368,
    34965,
    35739,
    35738,
    3074,
    34853,
    34854,
    34855,
    34856,
    34857,
    34858,
    34859,
    34860,
    34861,
    34862,
    34863,
    34864,
    34865,
    34866,
    34867,
    34868,
    35097,
    32873,
    35869,
    32874,
    34068
]);
//# sourceMappingURL=webgl-parameter-tables.js.map