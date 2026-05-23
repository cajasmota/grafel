import { NumericArray, NumericArray3 } from '@math.gl/types';
import { Vector } from "./base/vector.js";
/** Helper type that captures array length for a 3 element vector */
export type Vector3Like = Vector3 | NumericArray3;
/**
 * Three-element vector class with common linear algebra operations.
 * Subclass of Array<number> meaning that it is highly compatible with other libraries
 */
export declare class Vector3 extends Vector {
    static get ZERO(): Vector3;
    /**
     * @class
     * @param x
     * @param y
     * @param z
     */
    constructor(x?: number | Readonly<NumericArray>, y?: number, z?: number);
    set(x: number, y: number, z: number): this;
    copy(array: Readonly<NumericArray>): this;
    fromObject(object: {
        x: number;
        y: number;
        z: number;
    }): this;
    toObject(object: {
        x?: number;
        y?: number;
        z?: number;
    }): {
        x: number;
        y: number;
        z: number;
    };
    get ELEMENTS(): number;
    get z(): number;
    set z(value: number);
    angle(vector: Readonly<NumericArray>): number;
    cross(vector: Readonly<NumericArray>): this;
    rotateX({ radians, origin }: {
        radians: number;
        origin?: Readonly<NumericArray>;
    }): this;
    rotateY({ radians, origin }: {
        radians: number;
        origin?: Readonly<NumericArray>;
    }): this;
    rotateZ({ radians, origin }: {
        radians: number;
        origin?: Readonly<NumericArray>;
    }): this;
    transform(matrix4: Readonly<NumericArray>): this;
    transformAsPoint(matrix4: Readonly<NumericArray>): this;
    transformAsVector(matrix4: Readonly<NumericArray>): this;
    transformByMatrix3(matrix3: Readonly<NumericArray>): this;
    transformByMatrix2(matrix2: Readonly<NumericArray>): this;
    transformByQuaternion(quaternion: Readonly<NumericArray>): this;
}
//# sourceMappingURL=vector3.d.ts.map