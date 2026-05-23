import { NumericArray, NumericArray4 } from '@math.gl/types';
import { Vector } from "./base/vector.js";
import type { Matrix4 } from "./matrix4.js";
/** Helper type that captures array length for a 4 element vector */
export type Vector4Like = Vector4 | NumericArray4;
/**
 * Four-element vector class with common linear algebra operations.
 * Subclass of Array<number> meaning that it is highly compatible with other libraries
 */
export declare class Vector4 extends Vector {
    static get ZERO(): Vector4;
    constructor(x?: number | Readonly<NumericArray>, y?: number, z?: number, w?: number);
    set(x: number, y: number, z: number, w: number): this;
    copy(array: Readonly<NumericArray>): this;
    fromObject(object: {
        x: number;
        y: number;
        z: number;
        w: number;
    }): this;
    toObject(object: {
        x?: number;
        y?: number;
        z?: number;
        w?: number;
    }): {
        x: number;
        y: number;
        z: number;
        w: number;
    };
    get ELEMENTS(): number;
    get z(): number;
    set z(value: number);
    get w(): number;
    set w(value: number);
    transform(matrix4: Readonly<NumericArray>): this;
    transformByMatrix3(matrix3: Readonly<NumericArray>): this;
    transformByMatrix2(matrix2: Readonly<NumericArray>): this;
    transformByQuaternion(quaternion: Readonly<NumericArray>): this;
    applyMatrix4(m: Matrix4): this;
}
//# sourceMappingURL=vector4.d.ts.map