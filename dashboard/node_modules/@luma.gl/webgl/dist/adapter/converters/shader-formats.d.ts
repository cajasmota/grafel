import { GLDataType, GLPixelType } from '@luma.gl/constants';
import { SignedDataType } from '@luma.gl/core';
/** Get shadertypes data type from GL constants */
export declare function convertGLDataTypeToDataType(type: GLDataType | GLPixelType): SignedDataType;
/** Get shader data type from GL constants *
export function getPrimitiveTypeFromGL(type: GL): PrimitiveDataType {
  switch (type) {
    case GL.INT:
      return 'i32';
    case GL.UNSIGNED_INT:
      return 'u32';
    case GL.SHORT:
      return 'i32';
    case GL.UNSIGNED_SHORT:
      return 'u32';
    case GL.BYTE:
      return 'i32';
    case GL.UNSIGNED_BYTE:
      return 'u32';
    case GL.FLOAT:
      return 'f32';
    case GL.HALF_FLOAT:
      return 'f16';
    default:
      throw new Error(String(type));
  }
}

/** Get shader attribute type from GL constants *
export function getShaderAttributeTypeFromGL(
  type: GL,
  components: 1 | 2 | 3 | 4
): AttributeShaderType {
  const dataType = getPrimitiveTypeFromGL(type);
  switch (components) {
    case 1:
      return dataType;
    case 2:
      return `vec2<${dataType}>`;
    case 3:
      return `vec2<${dataType}>`;
    case 4:
      return `vec2<${dataType}>`;
    default:
      throw new Error(String(components));
  }
}
*/
/** GetGL constant from shader data type
export function getGLFromShaderDataType(
  type: PrimitiveDataType
): GL.INT | GL.UNSIGNED_INT | GL.FLOAT | GL.HALF_FLOAT {
  switch (type) {
    // TODO
    case 'i32':
      return GL.INT;
    case 'u32':
      return GL.UNSIGNED_INT;
    case 'f32':
      return GL.FLOAT;
    case 'f16':
      return GL.HALF_FLOAT;
    default:
      throw new Error(String(type));
  }
}
*/
//# sourceMappingURL=shader-formats.d.ts.map