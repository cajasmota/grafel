// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { log } from '@luma.gl/core';
import { GL } from '@luma.gl/constants';
import { setGLParameters } from "../../context/parameters/unified-parameter-api.js";
/* eslint-disable no-unused-expressions */ // For expression ? gl.enable() : gl.disable()
/**
 * Execute a function with a set of temporary WebGL parameter overrides
 * - Saves current "global" WebGL context settings
 * - Sets the supplies WebGL context parameters,
 * - Executes supplied function
 * - Restores parameters
 * - Returns the return value of the supplied function
 */
export function withDeviceAndGLParameters(device, parameters, glParameters, func) {
    if (isObjectEmpty(parameters)) {
        // Avoid setting state if no parameters provided. Just call and return
        return func(device);
    }
    // Wrap in a try-catch to ensure that parameters are restored on exceptions
    const webglDevice = device;
    webglDevice.pushState();
    try {
        setDeviceParameters(device, parameters);
        setGLParameters(webglDevice.gl, glParameters);
        return func(device);
    }
    finally {
        webglDevice.popState();
    }
}
/**
 * Execute a function with a set of temporary WebGL parameter overrides
 * - Saves current "global" WebGL context settings
 * - Sets the supplies WebGL context parameters,
 * - Executes supplied function
 * - Restores parameters
 * - Returns the return value of the supplied function
 * @deprecated use withDeviceParameters instead
 */
export function withGLParameters(device, parameters, func) {
    if (isObjectEmpty(parameters)) {
        // Avoid setting state if no parameters provided. Just call and return
        return func(device);
    }
    // Wrap in a try-catch to ensure that parameters are restored on exceptions
    const webglDevice = device;
    webglDevice.pushState();
    try {
        setGLParameters(webglDevice.gl, parameters);
        return func(device);
    }
    finally {
        webglDevice.popState();
    }
}
/**
 * Execute a function with a set of temporary WebGL parameter overrides
 * - Saves current "global" WebGL context settings
 * - Sets the supplies WebGL context parameters,
 * - Executes supplied function
 * - Restores parameters
 * - Returns the return value of the supplied function
 */
export function withDeviceParameters(device, parameters, func) {
    if (isObjectEmpty(parameters)) {
        // Avoid setting state if no parameters provided. Just call and return
        return func(device);
    }
    // Wrap in a try-catch to ensure that parameters are restored on exceptions'
    const webglDevice = device;
    webglDevice.pushState();
    try {
        setDeviceParameters(device, parameters);
        return func(device);
    }
    finally {
        webglDevice.popState();
    }
}
/** Set WebGPU Style Parameters */
export function setDeviceParameters(device, parameters) {
    const webglDevice = device;
    const { gl } = webglDevice;
    // RASTERIZATION SETTINGS
    if (parameters.cullMode) {
        switch (parameters.cullMode) {
            case 'none':
                gl.disable(2884);
                break;
            case 'front':
                gl.enable(2884);
                gl.cullFace(1028);
                break;
            case 'back':
                gl.enable(2884);
                gl.cullFace(1029);
                break;
        }
    }
    if (parameters.frontFace) {
        gl.frontFace(map('frontFace', parameters.frontFace, {
            ccw: 2305,
            cw: 2304
        }));
    }
    if (parameters.unclippedDepth) {
        if (device.features.has('depth-clip-control')) {
            // EXT_depth_clamp
            gl.enable(34383);
        }
    }
    if (parameters.depthBias !== undefined) {
        gl.enable(32823);
        gl.polygonOffset(parameters.depthBias, parameters.depthBiasSlopeScale || 0);
    }
    // depthBiasSlopeScale: {
    //   // Handled by depthBias
    // },
    // WEBGL EXTENSIONS
    if (parameters.provokingVertex) {
        if (device.features.has('provoking-vertex-webgl')) {
            const extensions = webglDevice.getExtension('WEBGL_provoking_vertex');
            const ext = extensions.WEBGL_provoking_vertex;
            const vertex = map('provokingVertex', parameters.provokingVertex, {
                first: 36429,
                last: 36430
            });
            ext?.provokingVertexWEBGL(vertex);
        }
    }
    if (parameters.polygonMode || parameters.polygonOffsetLine) {
        if (device.features.has('polygon-mode-webgl')) {
            if (parameters.polygonMode) {
                const extensions = webglDevice.getExtension('WEBGL_polygon_mode');
                const ext = extensions.WEBGL_polygon_mode;
                const mode = map('polygonMode', parameters.polygonMode, {
                    fill: 6914,
                    line: 6913
                });
                ext?.polygonModeWEBGL(1028, mode);
                ext?.polygonModeWEBGL(1029, mode);
            }
            if (parameters.polygonOffsetLine) {
                gl.enable(10754);
            }
        }
    }
    if (device.features.has('shader-clip-cull-distance-webgl')) {
        if (parameters.clipDistance0) {
            gl.enable(12288);
        }
        if (parameters.clipDistance1) {
            gl.enable(12289);
        }
        if (parameters.clipDistance2) {
            gl.enable(12290);
        }
        if (parameters.clipDistance3) {
            gl.enable(12291);
        }
        if (parameters.clipDistance4) {
            gl.enable(12292);
        }
        if (parameters.clipDistance5) {
            gl.enable(12293);
        }
        if (parameters.clipDistance6) {
            gl.enable(12294);
        }
        if (parameters.clipDistance7) {
            gl.enable(12295);
        }
    }
    // DEPTH STENCIL
    if (parameters.depthWriteEnabled !== undefined) {
        gl.depthMask(mapBoolean('depthWriteEnabled', parameters.depthWriteEnabled));
    }
    if (parameters.depthCompare) {
        parameters.depthCompare !== 'always' ? gl.enable(2929) : gl.disable(2929);
        gl.depthFunc(convertCompareFunction('depthCompare', parameters.depthCompare));
    }
    if (parameters.stencilWriteMask) {
        const mask = parameters.stencilWriteMask;
        gl.stencilMaskSeparate(1028, mask);
        gl.stencilMaskSeparate(1029, mask);
    }
    if (parameters.stencilReadMask) {
        // stencilReadMask is handle inside stencil***Compare.
        log.warn('stencilReadMask not supported under WebGL');
    }
    if (parameters.stencilCompare) {
        const mask = parameters.stencilReadMask || 0xffffffff;
        const glValue = convertCompareFunction('depthCompare', parameters.stencilCompare);
        // TODO - ensure back doesn't overwrite
        parameters.stencilCompare !== 'always'
            ? gl.enable(2960)
            : gl.disable(2960);
        gl.stencilFuncSeparate(1028, glValue, 0, mask);
        gl.stencilFuncSeparate(1029, glValue, 0, mask);
    }
    if (parameters.stencilPassOperation &&
        parameters.stencilFailOperation &&
        parameters.stencilDepthFailOperation) {
        const dppass = convertStencilOperation('stencilPassOperation', parameters.stencilPassOperation);
        const sfail = convertStencilOperation('stencilFailOperation', parameters.stencilFailOperation);
        const dpfail = convertStencilOperation('stencilDepthFailOperation', parameters.stencilDepthFailOperation);
        gl.stencilOpSeparate(1028, sfail, dpfail, dppass);
        gl.stencilOpSeparate(1029, sfail, dpfail, dppass);
    }
    // stencilDepthFailOperation() {
    //   // handled by stencilPassOperation
    // },
    // stencilFailOperation() {
    //   // handled by stencilPassOperation
    // },
    // COLOR STATE
    switch (parameters.blend) {
        case true:
            gl.enable(3042);
            break;
        case false:
            gl.disable(3042);
            break;
        default:
        // leave WebGL blend state unchanged if `parameters.blend` is not set
    }
    if (parameters.blendColorOperation || parameters.blendAlphaOperation) {
        const colorEquation = convertBlendOperationToEquation('blendColorOperation', parameters.blendColorOperation || 'add');
        const alphaEquation = convertBlendOperationToEquation('blendAlphaOperation', parameters.blendAlphaOperation || 'add');
        gl.blendEquationSeparate(colorEquation, alphaEquation);
        const colorSrcFactor = convertBlendFactorToFunction('blendColorSrcFactor', parameters.blendColorSrcFactor || 'one');
        const colorDstFactor = convertBlendFactorToFunction('blendColorDstFactor', parameters.blendColorDstFactor || 'zero');
        const alphaSrcFactor = convertBlendFactorToFunction('blendAlphaSrcFactor', parameters.blendAlphaSrcFactor || 'one');
        const alphaDstFactor = convertBlendFactorToFunction('blendAlphaDstFactor', parameters.blendAlphaDstFactor || 'zero');
        gl.blendFuncSeparate(colorSrcFactor, colorDstFactor, alphaSrcFactor, alphaDstFactor);
    }
}
/*
      rasterizationState: {
        cullMode: "back",
      },

      depthStencilState: {
        depthWriteEnabled: true,
        depthCompare: "less",
        format: "depth24plus-stencil8",
      },

      colorStates: [
        {
          format: "bgra8unorm",
          // colorBlend.srcFactor = wgpu::BlendFactor::SrcAlpha;
          // colorBlend.dstFactor = wgpu::BlendFactor::OneMinusSrcAlpha;
          // alphaBlend.srcFactor = wgpu::BlendFactor::SrcAlpha;
          // alphaBlend.dstFactor = wgpu::BlendFactor::OneMinusSrcAlpha;
        },
      ],
    });
*/
export function convertCompareFunction(parameter, value) {
    return map(parameter, value, {
        never: 512,
        less: 513,
        equal: 514,
        'less-equal': 515,
        greater: 516,
        'not-equal': 517,
        'greater-equal': 518,
        always: 519
    });
}
export function convertToCompareFunction(parameter, value) {
    return map(parameter, value, {
        [512]: 'never',
        [513]: 'less',
        [514]: 'equal',
        [515]: 'less-equal',
        [516]: 'greater',
        [517]: 'not-equal',
        [518]: 'greater-equal',
        [519]: 'always'
    });
}
function convertStencilOperation(parameter, value) {
    return map(parameter, value, {
        keep: 7680,
        zero: 0,
        replace: 7681,
        invert: 5386,
        'increment-clamp': 7682,
        'decrement-clamp': 7683,
        'increment-wrap': 34055,
        'decrement-wrap': 34056
    });
}
function convertBlendOperationToEquation(parameter, value) {
    return map(parameter, value, {
        add: 32774,
        subtract: 32778,
        'reverse-subtract': 32779,
        min: 32775,
        max: 32776
    });
}
function convertBlendFactorToFunction(parameter, value, type = 'color') {
    return map(parameter, value, {
        one: 1,
        zero: 0,
        src: 768,
        'one-minus-src': 769,
        dst: 774,
        'one-minus-dst': 775,
        'src-alpha': 770,
        'one-minus-src-alpha': 771,
        'dst-alpha': 772,
        'one-minus-dst-alpha': 773,
        'src-alpha-saturated': 776,
        constant: type === 'color' ? 32769 : 32771,
        'one-minus-constant': type === 'color' ? 32770 : 32772,
        // 'constant-alpha': GL.CONSTANT_ALPHA,
        // 'one-minus-constant-alpha': GL.ONE_MINUS_CONSTANT_ALPHA,
        // TODO not supported in WebGL2
        src1: 768,
        'one-minus-src1': 769,
        'src1-alpha': 770,
        'one-minus-src1-alpha': 771
    });
}
function message(parameter, value) {
    return `Illegal parameter ${value} for ${parameter}`;
}
function map(parameter, value, valueMap) {
    if (!(value in valueMap)) {
        throw new Error(message(parameter, value));
    }
    return valueMap[value];
}
function mapBoolean(parameter, value) {
    return value;
}
/** Returns true if given object is empty, false otherwise. */
function isObjectEmpty(obj) {
    let isEmpty = true;
    // @ts-ignore key is unused
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    for (const key in obj) {
        isEmpty = false;
        break;
    }
    return isEmpty;
}
//# sourceMappingURL=device-parameters.js.map