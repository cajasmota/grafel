import type { NumericArray } from '@math.gl/types';
/**
 * Calculate WebGL 64 bit float
 * @param a  - the input float number
 * @param out - the output array. If not supplied, a new array is created.
 * @param startIndex - the index in the output array to fill from. Default 0.
 * @returns - the fp64 representation of the input number
 */
export declare function fp64ify(a: number, out?: NumericArray, startIndex?: number): NumericArray;
/**
 * Calculate the low part of a WebGL 64 bit float
 * @param a the input float number
 * @returns the lower 32 bit of the number
 */
export declare function fp64LowPart(a: number): number;
/**
 * Calculate WebGL 64 bit matrix (transposed "Float64Array")
 * @param matrix  the input matrix
 * @returns the fp64 representation of the input matrix
 */
export declare function fp64ifyMatrix4(matrix: NumericArray): Float32Array;
//# sourceMappingURL=fp64-utils.d.ts.map