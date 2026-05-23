// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { GL } from '@luma.gl/constants';
// Counts the number of complete primitives given a number of vertices and a drawMode
export function getPrimitiveDrawMode(drawMode) {
    switch (drawMode) {
        case 0:
            return 0;
        case 1:
            return 1;
        case 3:
            return 1;
        case 2:
            return 1;
        case 4:
            return 4;
        case 5:
            return 4;
        case 6:
            return 4;
        default:
            throw new Error('drawMode');
    }
}
// Counts the number of complete "primitives" given a number of vertices and a drawMode
export function getPrimitiveCount(options) {
    const { drawMode, vertexCount } = options;
    switch (drawMode) {
        case 0:
        case 2:
            return vertexCount;
        case 1:
            return vertexCount / 2;
        case 3:
            return vertexCount - 1;
        case 4:
            return vertexCount / 3;
        case 5:
        case 6:
            return vertexCount - 2;
        default:
            throw new Error('drawMode');
    }
}
// Counts the number of vertices after splitting the vertex stream into separate "primitives"
export function getVertexCount(options) {
    const { drawMode, vertexCount } = options;
    const primitiveCount = getPrimitiveCount({ drawMode, vertexCount });
    switch (getPrimitiveDrawMode(drawMode)) {
        case 0:
            return primitiveCount;
        case 1:
            return primitiveCount * 2;
        case 4:
            return primitiveCount * 3;
        default:
            throw new Error('drawMode');
    }
}
/** Get the primitive type for draw */
export function getGLDrawMode(topology) {
    // prettier-ignore
    switch (topology) {
        case 'point-list': return 0;
        case 'line-list': return 1;
        case 'line-strip': return 3;
        case 'triangle-list': return 4;
        case 'triangle-strip': return 5;
        default: throw new Error(topology);
    }
}
/** Get the primitive type for transform feedback */
export function getGLPrimitive(topology) {
    // prettier-ignore
    switch (topology) {
        case 'point-list': return 0;
        case 'line-list': return 1;
        case 'line-strip': return 1;
        case 'triangle-list': return 4;
        case 'triangle-strip': return 4;
        default: throw new Error(topology);
    }
}
//# sourceMappingURL=webgl-topology-utils.js.map