import { TypedArray, TypedArrayConstructor } from "../../types.js";
import { SignedDataType, NormalizedDataType, DataTypeInfo } from "./data-types.js";
/**
 * Gets info about a data type constant (signed or normalized)
 * @returns underlying primitive / signed types, byte length, normalization, integer, signed flags
 */
export declare function getDataTypeInfo(type: NormalizedDataType): DataTypeInfo;
/** Build a vertex format from a signed data type and a component */
export declare function getNormalizedDataType(signedDataType: SignedDataType): NormalizedDataType;
/** Align offset to 1, 2 or 4 elements (4, 8 or 16 bytes) */
export declare function alignTo(size: number, count: number): number;
/** Returns the VariableShaderType that corresponds to a typed array */
export declare function getDataType(arrayOrType: TypedArray | TypedArrayConstructor): SignedDataType;
/** Returns the TypedArray that corresponds to a shader data type */
export declare function getTypedArrayConstructor(type: NormalizedDataType): TypedArrayConstructor;
//# sourceMappingURL=decode-data-types.d.ts.map