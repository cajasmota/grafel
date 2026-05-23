import { NormalizedDataType } from "../data-types/data-types.js";
/**
 * Describes the **memory format** and interpretation (normalization) of a buffer that will be supplied to vertex attributes
 * @note Must be compatible with the AttributeShaderType of the shaders, see documentation.
 * @note This is a superset of WebGPU vertex formats to allow for some flexibility for WebGL only applications
 * @todo Add device.isTextureFormatSupported() method?
 */
export type VertexFormat = 'uint8' | 'uint8x2' | 'uint8x4' | 'sint8' | 'sint8x2' | 'sint8x4' | 'unorm8' | 'unorm8x2' | 'unorm8x3-webgl' | 'unorm8x4' | 'unorm8x4-bgra' | 'unorm10-10-10-2' | 'snorm8' | 'snorm8x2' | 'snorm8x3-webgl' | 'snorm8x4' | 'uint16' | 'sint16' | 'unorm16' | 'snorm16' | 'uint16x2' | 'uint16x4' | 'sint16x2' | 'sint16x4' | 'unorm16x2' | 'unorm16x4' | 'snorm16x2' | 'snorm16x4' | 'uint32' | 'uint32x2' | 'uint32x3' | 'uint32x4' | 'sint32' | 'sint32x2' | 'sint32x3' | 'sint32x4' | 'float16' | 'float16x2' | 'float16x4' | 'float32' | 'float32x2' | 'float32x3' | 'float32x4';
export type VertexFormatInfo = {
    /** Type of each component */
    type: NormalizedDataType;
    /** Length in bytes */
    byteLength: number;
    /** Number of components per vertex / row */
    components: 1 | 2 | 3 | 4;
    /** Is this an integer format (normalized integer formats are not integer) */
    integer: boolean;
    /** Is this a signed format? */
    signed: boolean;
    /** Is this a normalized format? */
    normalized: boolean;
    /** Is this a bgra format? */
    bgra?: boolean;
    /** Is this a webgl only format? */
    webglOnly?: boolean;
};
//# sourceMappingURL=vertex-formats.d.ts.map