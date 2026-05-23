import type { TypedArray } from "../../types.js";
import type { PrimitiveDataType, SignedDataType } from "../data-types/data-types.js";
import type { VertexFormat, VertexFormatInfo } from "./vertex-formats.js";
/**
 * Decodes a vertex format, returning type, components, byte  length and flags (integer, signed, normalized)
 */
export declare function getVertexFormatInfo(format: VertexFormat): VertexFormatInfo;
/** Build a vertex format from a signed data type and a component */
export declare function makeVertexFormat(signedDataType: SignedDataType, components: 1 | 2 | 3 | 4, normalized?: boolean): VertexFormat;
/** Get the vertex format for an attribute with TypedArray and size */
export declare function getVertexFormatFromAttribute(typedArray: TypedArray, size: number, normalized?: boolean): VertexFormat;
/** Return a "default" vertex format for a certain shader data type
 The simplest vertex format that matches the shader attribute's data type */
export declare function getCompatibleVertexFormat(opts: {
    primitiveType: PrimitiveDataType;
    components: 1 | 2 | 3 | 4;
}): VertexFormat;
//# sourceMappingURL=decode-vertex-format.d.ts.map