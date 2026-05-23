import type { TypedArray, NumberArray } from "../types.js";
/**
 * Check is an array is a typed array
 * @param value value to be tested
 * @returns input as TypedArray, or null
 * @todo this should be provided by @math.gl/types
 */
export declare function isTypedArray(value: unknown): value is TypedArray;
/**
 * Check is an array is a numeric array (typed array or array of numbers)
 * @param value value to be tested
 * @returns input as NumberArray, or null
 * @todo this should be provided by @math.gl/types
 */
export declare function isNumberArray(value: unknown): value is NumberArray;
//# sourceMappingURL=is-array.d.ts.map