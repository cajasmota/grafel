// math.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
/**
 * Check is an array is a typed array
 * @param value value to be tested
 * @returns input with type narrowed to TypedArray, or null
 */
export function isTypedArray(value) {
    return ArrayBuffer.isView(value) && !(value instanceof DataView);
}
/**
 * Check is an array is an array of numbers)
 * @param value value to be tested
 * @returns input with type narrowed to NumberArray, or null
 */
export function isNumberArray(value) {
    if (Array.isArray(value)) {
        return value.length === 0 || typeof value[0] === 'number';
    }
    return false;
}
/**
 * Check is an array is a numeric array (typed array or array of numbers)
 * @param value value to be tested
 * @returns input with type narrowed to NumericArray, or null
 */
export function isNumericArray(value) {
    return isTypedArray(value) || isNumberArray(value);
}
//# sourceMappingURL=is-array.js.map