/**
 * Symmetric round
 * see https://www.npmjs.com/package/round-half-up-symmetric#user-content-detailed-background
 *
 * @param {Number} a value to round
 */
export function round(a: number): number;
/**
 * Sets the type of array used when creating new vectors and matrices
 *
 * @param {Float32ArrayConstructor | ArrayConstructor} type Array type, such as Float32Array or Array
 */
export function setMatrixArrayType(type: Float32ArrayConstructor | ArrayConstructor): void;
/**
 * Convert Degree To Radian
 *
 * @param {Number} a Angle in Degrees
 */
export function toRadian(a: number): number;
/**
 * Tests whether or not the arguments have approximately the same value, within an absolute
 * or relative tolerance of glMatrix.EPSILON (an absolute tolerance is used for values less
 * than or equal to 1.0, and a relative tolerance is used for larger values)
 *
 * @param {Number} a The first number to test.
 * @param {Number} b The second number to test.
 * @returns {Boolean} True if the numbers are approximately equal, false otherwise.
 */
export function equals(a: number, b: number): boolean;
/**
 * Common utilities
 * @module glMatrix
 */
export const EPSILON: 0.000001;
export let ARRAY_TYPE: ArrayConstructor | Float32ArrayConstructor;
export const RANDOM: () => number;
export const ANGLE_ORDER: "zyx";
//# sourceMappingURL=common.d.ts.map