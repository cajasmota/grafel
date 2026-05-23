// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { GL } from '@luma.gl/constants';
/** Converts to a luma shadertype to a GL data type (GL.BYTE, GL.FLOAT32 etc)  */
export function convertDataTypeToGLDataType(normalizedType) {
    return NORMALIZED_SHADER_TYPE_TO_WEBGL[normalizedType];
}
/** Converts to a luma shadertype to a GL data type (GL.BYTE, GL.FLOAT32 etc)  */
export function convertShaderVariableTypeToGLDataType(normalizedType) {
    // @ts-ignore TODO
    return NORMALIZED_SHADER_TYPE_TO_WEBGL[normalizedType];
}
/** Convert a WebGL "compisite type (e.g. GL.VEC3) into the corresponding luma shader uniform type */
export function convertGLUniformTypeToShaderVariableType(glUniformType) {
    return WEBGL_SHADER_TYPES[glUniformType];
}
/** Check if a WebGL "uniform:" is a texture binding */
export function isGLSamplerType(type) {
    // @ts-ignore TODO
    return Boolean(WEBGL_SAMPLER_TO_TEXTURE_BINDINGS[type]);
}
/* Get luma texture binding info (viewDimension and sampleType) from a WebGL "sampler" binding */
export function getTextureBindingFromGLSamplerType(glSamplerType) {
    return WEBGL_SAMPLER_TO_TEXTURE_BINDINGS[glSamplerType];
}
/** Get vertex format from GL constants */
export function getVertexFormatFromGL(type, components) {
    const base = getVertexTypeFromGL(type);
    // prettier-ignore
    switch (components) {
        case 1: return base;
        case 2: return `${base}x2`;
        // @ts-expect-error - deal with lack of "unaligned" formats
        case 3: return `${base}x3`;
        case 4: return `${base}x4`;
    }
    // @ts-ignore unreachable
    throw new Error(String(components));
}
/** Get data type from GL constants */
export function getVertexTypeFromGL(glType, normalized = false) {
    const index = normalized ? 1 : 0;
    return WEBGL_TO_NORMALIZED_DATA_TYPE[glType][index];
}
// Composite types table
// @ts-ignore TODO - fix the type confusion here
const WEBGL_SHADER_TYPES = {
    [5126]: 'f32',
    [35664]: 'vec2<f32>',
    [35665]: 'vec3<f32>',
    [35666]: 'vec4<f32>',
    [5124]: 'i32',
    [35667]: 'vec2<i32>',
    [35668]: 'vec3<i32>',
    [35669]: 'vec4<i32>',
    [5125]: 'u32',
    [36294]: 'vec2<u32>',
    [36295]: 'vec3<u32>',
    [36296]: 'vec4<u32>',
    [35670]: 'f32',
    [35671]: 'vec2<f32>',
    [35672]: 'vec3<f32>',
    [35673]: 'vec4<f32>',
    // TODO - are sizes/components below correct?
    [35674]: 'mat2x2<f32>',
    [35685]: 'mat2x3<f32>',
    [35686]: 'mat2x4<f32>',
    [35687]: 'mat3x2<f32>',
    [35675]: 'mat3x3<f32>',
    [35688]: 'mat3x4<f32>',
    [35689]: 'mat4x2<f32>',
    [35690]: 'mat4x3<f32>',
    [35676]: 'mat4x4<f32>'
};
const WEBGL_SAMPLER_TO_TEXTURE_BINDINGS = {
    [35678]: { viewDimension: '2d', sampleType: 'float' },
    [35680]: { viewDimension: 'cube', sampleType: 'float' },
    [35679]: { viewDimension: '3d', sampleType: 'float' },
    [35682]: { viewDimension: '3d', sampleType: 'depth' },
    [36289]: { viewDimension: '2d-array', sampleType: 'float' },
    [36292]: { viewDimension: '2d-array', sampleType: 'depth' },
    [36293]: { viewDimension: 'cube', sampleType: 'float' },
    [36298]: { viewDimension: '2d', sampleType: 'sint' },
    [36299]: { viewDimension: '3d', sampleType: 'sint' },
    [36300]: { viewDimension: 'cube', sampleType: 'sint' },
    [36303]: { viewDimension: '2d-array', sampleType: 'uint' },
    [36306]: { viewDimension: '2d', sampleType: 'uint' },
    [36307]: { viewDimension: '3d', sampleType: 'uint' },
    [36308]: { viewDimension: 'cube', sampleType: 'uint' },
    [36311]: { viewDimension: '2d-array', sampleType: 'uint' }
};
/** Map from WebGL normalized types to WebGL */
const NORMALIZED_SHADER_TYPE_TO_WEBGL = {
    uint8: 5121,
    sint8: 5120,
    unorm8: 5121,
    snorm8: 5120,
    uint16: 5123,
    sint16: 5122,
    unorm16: 5123,
    snorm16: 5122,
    uint32: 5125,
    sint32: 5124,
    // WebGPU does not support normalized 32 bit integer attributes
    //  'unorm32': GL.UNSIGNED_INT,
    //  'snorm32': GL.INT,
    float16: 5131,
    float32: 5126
};
/* Map from WebGL types to webgpu normalized types */
const WEBGL_TO_NORMALIZED_DATA_TYPE = {
    [5120]: ['sint8', 'snorm16'],
    [5121]: ['uint8', 'unorm8'],
    [5122]: ['sint16', 'unorm16'],
    [5123]: ['uint16', 'unorm16'],
    [5124]: ['sint32', 'sint32'],
    [5125]: ['uint32', 'uint32'],
    [5126]: ['float32', 'float32'],
    [5131]: ['float16', 'float16']
};
//# sourceMappingURL=webgl-shadertypes.js.map