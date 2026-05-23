import { GL, GLPrimitiveTopology, GLPrimitive } from '@luma.gl/constants';
import { PrimitiveTopology } from '@luma.gl/core';
export declare function getPrimitiveDrawMode(drawMode: GLPrimitiveTopology): GLPrimitive;
export declare function getPrimitiveCount(options: {
    drawMode: GLPrimitiveTopology;
    vertexCount: number;
}): number;
export declare function getVertexCount(options: {
    drawMode: GLPrimitiveTopology;
    vertexCount: number;
}): number;
/** Get the primitive type for draw */
export declare function getGLDrawMode(topology: PrimitiveTopology): GL.POINTS | GL.LINES | GL.LINE_STRIP | GL.LINE_LOOP | GL.TRIANGLES | GL.TRIANGLE_STRIP | GL.TRIANGLE_FAN;
/** Get the primitive type for transform feedback */
export declare function getGLPrimitive(topology: PrimitiveTopology): GL.POINTS | GL.LINES | GL.TRIANGLES;
//# sourceMappingURL=webgl-topology-utils.d.ts.map