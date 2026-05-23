import { TypedArray, NumericArray, NumberArray } from "./array-types.js";
/**
 * Check is an array is a typed array
 * @param value value to be tested
 * @returns input with type narrowed to TypedArray, or null
 */
export declare function isTypedArray(value: unknown): value is TypedArray;
/**
 * Check is an array is an array of numbers)
 * @param value value to be tested
 * @returns input with type narrowed to NumberArray, or null
 */
export declare function isNumberArray(value: unknown): value is NumberArray;
/**
 * Check is an array is a numeric array (typed array or array of numbers)
 * @param value value to be tested
 * @returns input with type narrowed to NumericArray, or null
 */
export declare function isNumericArray(value: unknown): value is NumericArray;
//# sourceMappingURL=is-array.d.ts.map