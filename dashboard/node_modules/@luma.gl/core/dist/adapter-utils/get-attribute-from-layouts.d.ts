import type { PrimitiveDataType, NormalizedDataType } from "../shadertypes/data-types/data-types.js";
import type { AttributeShaderType } from "../shadertypes/data-types/shader-types.js";
import type { VertexFormat } from "../shadertypes/vertex-arrays/vertex-formats.js";
import type { ShaderLayout } from "../adapter/types/shader-layout.js";
import type { BufferLayout } from "../adapter/types/buffer-layout.js";
/** Resolved info for a buffer / attribute combination to help backend configure it correctly */
export type AttributeInfo = {
    /** Attribute name */
    attributeName: string;
    /** Location in shader */
    location: number;
    /** Type / precision used in shader (buffer values may be converted) */
    shaderType: AttributeShaderType;
    /** Calculations are done in this type in the shader's attribute declaration */
    primitiveType: PrimitiveDataType;
    /** Components refer to the number of components in the shader's attribute declaration */
    shaderComponents: 1 | 2 | 3 | 4;
    /** It is the shader attribute declaration that determines whether GPU will process as integer or float */
    integer: boolean;
    /** BufferName */
    bufferName: string;
    /** Format of buffer data */
    vertexFormat: VertexFormat;
    /** Memory data type refers to the data type in the buffer */
    bufferDataType: NormalizedDataType;
    /** Components refer to the number of components in the buffer's vertex format */
    bufferComponents: 1 | 2 | 3 | 4;
    /** Normalization is encoded in the buffer layout's vertex format... */
    normalized: boolean;
    /** If not specified, the step mode is inferred from the attribute name in the shader (contains string instance) */
    stepMode: 'vertex' | 'instance';
    /** The byteOffset is encoded in or calculated from the buffer layout */
    byteOffset: number;
    /** The byteStride is encoded in or calculated from the buffer layout */
    byteStride: number;
};
/**
 * Map from "attribute names" to "resolved attribute infos"
 * containing information about both buffer layouts and shader attribute declarations
 */
export declare function getAttributeInfosFromLayouts(shaderLayout: ShaderLayout, bufferLayout: BufferLayout[]): Record<string, AttributeInfo>;
/**
 * Array indexed by "location" holding "resolved attribute infos"
 */
export declare function getAttributeInfosByLocation(shaderLayout: ShaderLayout, bufferLayout: BufferLayout[], maxVertexAttributes?: number): AttributeInfo[];
//# sourceMappingURL=get-attribute-from-layouts.d.ts.map