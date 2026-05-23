import { PrimitiveDataType } from "./data-types.js";
import type { VariableShaderType, AttributeShaderType, AttributeShaderTypeInfo, VariableShaderTypeAlias, AttributeShaderTypeAlias } from "./shader-types.js";
/** Split a uniform type string into type and components */
export declare function getVariableShaderTypeInfo(format: VariableShaderType): {
    type: PrimitiveDataType;
    components: number;
};
/** Decodes a vertex type, returning byte length and flags (integer, signed, normalized) */
export declare function getAttributeShaderTypeInfo(attributeType: AttributeShaderType): AttributeShaderTypeInfo;
export declare function makeShaderAttributeType(primitiveType: PrimitiveDataType, components: 1 | 2 | 3 | 4): AttributeShaderType;
export declare function resolveAttributeShaderTypeAlias(alias: AttributeShaderTypeAlias | AttributeShaderType): AttributeShaderType;
export declare function resolveVariableShaderTypeAlias(alias: VariableShaderTypeAlias | VariableShaderType): VariableShaderType;
/**  Predeclared aliases @see https://www.w3.org/TR/WGSL/#vector-types */
export declare const WGSL_ATTRIBUTE_TYPE_ALIAS_MAP: Record<AttributeShaderTypeAlias, AttributeShaderType>;
/** @todo These tables are quite big, consider parsing alias strings instead */
export declare const WGSL_VARIABLE_TYPE_ALIAS_MAP: Record<VariableShaderTypeAlias, VariableShaderType>;
//# sourceMappingURL=decode-shader-types.d.ts.map