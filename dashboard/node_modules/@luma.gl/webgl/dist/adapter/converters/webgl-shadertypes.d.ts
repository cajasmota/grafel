import { VariableShaderType, VertexFormat, NormalizedDataType } from '@luma.gl/core';
import { GLUniformType, GLSamplerType, GLDataType } from '@luma.gl/constants';
export type TextureBindingInfo = {
    viewDimension: '1d' | '2d' | '2d-array' | 'cube' | 'cube-array' | '3d';
    sampleType: 'float' | 'unfilterable-float' | 'depth' | 'sint' | 'uint';
};
/** Converts to a luma shadertype to a GL data type (GL.BYTE, GL.FLOAT32 etc)  */
export declare function convertDataTypeToGLDataType(normalizedType: NormalizedDataType): GLDataType;
/** Converts to a luma shadertype to a GL data type (GL.BYTE, GL.FLOAT32 etc)  */
export declare function convertShaderVariableTypeToGLDataType(normalizedType: VariableShaderType): GLDataType;
/** Convert a WebGL "compisite type (e.g. GL.VEC3) into the corresponding luma shader uniform type */
export declare function convertGLUniformTypeToShaderVariableType(glUniformType: GLUniformType): VariableShaderType;
/** Check if a WebGL "uniform:" is a texture binding */
export declare function isGLSamplerType(type: GLUniformType | GLSamplerType): type is GLSamplerType;
export declare function getTextureBindingFromGLSamplerType(glSamplerType: GLSamplerType): TextureBindingInfo;
/** Get vertex format from GL constants */
export declare function getVertexFormatFromGL(type: GLDataType, components: 1 | 2 | 3 | 4): VertexFormat;
/** Get data type from GL constants */
export declare function getVertexTypeFromGL(glType: GLDataType, normalized?: boolean): NormalizedDataType;
//# sourceMappingURL=webgl-shadertypes.d.ts.map