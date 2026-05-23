import { NumericArray, NumericArray9 } from '@math.gl/types';
import { Matrix } from "./base/matrix.js";
declare enum INDICES {
    COL0ROW0 = 0,
    COL0ROW1 = 1,
    COL0ROW2 = 2,
    COL1ROW0 = 3,
    COL1ROW1 = 4,
    COL1ROW2 = 5,
    COL2ROW0 = 6,
    COL2ROW1 = 7,
    COL2ROW2 = 8
}
/** Helper type that captures array length for a 3x3 matrix */
export type Matrix3Like = Matrix3 | NumericArray9;
/**
 * A 3x3 matrix with common linear algebra operations
 * Subclass of Array<number> meaning that it is highly compatible with other libraries
 */
export declare class Matrix3 extends Matrix {
    static get IDENTITY(): Readonly<Matrix3>;
    static get ZERO(): Readonly<Matrix3>;
    get ELEMENTS(): number;
    get RANK(): number;
    get INDICES(): typeof INDICES;
    constructor(array?: Readonly<NumericArray>);
    /** @deprecated */
    constructor(...args: number[]);
    copy(array: Readonly<NumericArray>): this;
    identity(): this;
    /**
     *
     * @param object
     * @returns self
     */
    fromObject(object: {
        [key: string]: any;
    }): this;
    /** Calculates a 3x3 matrix from the given quaternion
     * q quat  Quaternion to create matrix from
     */
    fromQuaternion(q: Readonly<NumericArray>): this;
    /**
     * accepts column major order, stores in column major order
     */
    set(m00: number, m10: number, m20: number, m01: number, m11: number, m21: number, m02: number, m12: number, m22: number): this;
    /**
     * accepts row major order, stores as column major
     */
    setRowMajor(m00: number, m01: number, m02: number, m10: number, m11: number, m12: number, m20: number, m21: number, m22: number): this;
    determinant(): number;
    transpose(): this;
    /** Invert a matrix. Note that this can fail if the matrix is not invertible */
    invert(): this;
    multiplyLeft(a: NumericArray): this;
    multiplyRight(a: NumericArray): this;
    rotate(radians: number): this;
    scale(factor: NumericArray | number): this;
    translate(vec: NumericArray): this;
    transform(vector: Readonly<NumericArray>, result?: NumericArray): NumericArray;
    /** @deprecated */
    transformVector(vector: Readonly<NumericArray>, result?: NumericArray): NumericArray;
    /** @deprecated */
    transformVector2(vector: Readonly<NumericArray>, result?: NumericArray): NumericArray;
    /** @deprecated */
    transformVector3(vector: Readonly<NumericArray>, result?: NumericArray): NumericArray;
}
export {};
//# sourceMappingURL=matrix3.d.ts.map