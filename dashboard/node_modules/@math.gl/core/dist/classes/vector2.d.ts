import { NumericArray, NumericArray2 } from '@math.gl/types';
import { Vector } from "./base/vector.js";
/** Helper type that captures array length for a 2 element vector */
export type Vector2Like = Vector2 | NumericArray2;
/**
 * Two-element vector class with common linear algebra operations.
 * Subclass of Array<number> meaning that it is highly compatible with other libraries
 */
export declare class Vector2 extends Vector {
    constructor(x?: number | Readonly<NumericArray>, y?: number);
    set(x: number, y: number): this;
    copy(array: Readonly<NumericArray>): this;
    fromObject(object: {
        x: number;
        y: number;
    }): this;
    toObject(object: {
        x?: number;
        y?: number;
    }): {
        x: number;
        y: number;
    };
    get ELEMENTS(): number;
    /**
     * Returns angle from x axis
     * @returns
     */
    horizontalAngle(): number;
    /**
     * Returns angle from y axis
     * @returns
     */
    verticalAngle(): number;
    /**
     * Transforms as point
     * @param matrix4
     * @returns
     */
    transform(matrix4: Readonly<NumericArray>): this;
    /**
     * transforms as point (4th component is implicitly 1)
     * @param matrix4
     * @returns
     */
    transformAsPoint(matrix4: Readonly<NumericArray>): this;
    /**
     * transforms as vector (4th component is implicitly 0, ignores translation. slightly faster)
     * @param matrix4
     * @returns
     */
    transformAsVector(matrix4: Readonly<NumericArray>): this;
    transformByMatrix3(matrix3: Readonly<NumericArray>): this;
    transformByMatrix2x3(matrix2x3: Readonly<NumericArray>): this;
    transformByMatrix2(matrix2: Readonly<NumericArray>): this;
}
//# sourceMappingURL=vector2.d.ts.map