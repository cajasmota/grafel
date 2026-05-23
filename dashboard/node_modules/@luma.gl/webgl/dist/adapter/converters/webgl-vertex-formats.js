// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { GL } from '@luma.gl/constants';
/** Get vertex format from GL constants */
export function getVertexFormatFromGL(type, components) {
    const base = getVertexTypeFromGL(type);
    // prettier-ignore
    switch (components) {
        case 1: return base;
        case 2: return `${base}x2`;
        // @ts-expect-error TODO deal with lack of "unaligned" formats
        case 3: return `${base}x3`;
        case 4: return `${base}x4`;
    }
    // @ts-ignore unreachable
    throw new Error(String(components));
}
/** Get data type from GL constants */
export function getVertexTypeFromGL(type, normalized = false) {
    // prettier-ignore
    switch (type) {
        // WebGPU does not support normalized 32 bit integer attributes
        case 5124: return normalized ? 'sint32' : 'sint32';
        case 5125: return normalized ? 'uint32' : 'uint32';
        case 5122: return normalized ? 'sint16' : 'unorm16';
        case 5123: return normalized ? 'uint16' : 'unorm16';
        case 5120: return normalized ? 'sint8' : 'snorm16';
        case 5121: return normalized ? 'uint8' : 'unorm8';
        case 5126: return 'float32';
        case 5131: return 'float16';
    }
    // @ts-ignore unreachable
    throw new Error(String(type));
}
export function getGLFromVertexType(dataType) {
    // prettier-ignore
    switch (dataType) {
        case 'uint8': return 5121;
        case 'sint8': return 5120;
        case 'unorm8': return 5121;
        case 'snorm8': return 5120;
        case 'uint16': return 5123;
        case 'sint16': return 5122;
        case 'unorm16': return 5123;
        case 'snorm16': return 5122;
        case 'uint32': return 5125;
        case 'sint32': return 5124;
        // WebGPU does not support normalized 32 bit integer attributes
        // case 'unorm32': return GL.UNSIGNED_INT;
        // case 'snorm32': return GL.INT;
        case 'float16': return 5131;
        case 'float32': return 5126;
    }
    // @ts-ignore unreachable
    throw new Error(String(dataType));
}
//# sourceMappingURL=webgl-vertex-formats.js.map