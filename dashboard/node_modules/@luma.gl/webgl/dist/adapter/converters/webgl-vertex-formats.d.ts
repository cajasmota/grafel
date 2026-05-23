import { GL } from '@luma.gl/constants';
import { VertexFormat, NormalizedDataType } from '@luma.gl/core';
type GLDataType = GL.UNSIGNED_BYTE | GL.BYTE | GL.UNSIGNED_SHORT | GL.SHORT | GL.UNSIGNED_INT | GL.INT | GL.HALF_FLOAT | GL.FLOAT;
/** Get vertex format from GL constants */
export declare function getVertexFormatFromGL(type: GLDataType, components: 1 | 2 | 3 | 4): VertexFormat;
/** Get data type from GL constants */
export declare function getVertexTypeFromGL(type: GLDataType, normalized?: boolean): NormalizedDataType;
export declare function getGLFromVertexType(dataType: NormalizedDataType): GL.UNSIGNED_BYTE | GL.BYTE | GL.UNSIGNED_SHORT | GL.SHORT | GL.UNSIGNED_INT | GL.INT | GL.HALF_FLOAT | GL.FLOAT;
export {};
//# sourceMappingURL=webgl-vertex-formats.d.ts.map