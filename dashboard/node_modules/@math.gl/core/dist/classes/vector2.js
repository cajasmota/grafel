// math.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
// Copyright (c) 2017 Uber Technologies, Inc.
import { Vector } from "./base/vector.js";
import { config, isArray } from "../lib/common.js";
import { checkNumber } from "../lib/validators.js";
import { transformMat4 as vec2_transformMat4, transformMat3 as vec2_transformMat3, transformMat2d as vec2_transformMat2d, transformMat2 as vec2_transformMat2 } from "../gl-matrix/vec2.js";
import { vec2_transformMat4AsVector } from "../lib/gl-matrix-extras.js";
/**
 * Two-element vector class with common linear algebra operations.
 * Subclass of Array<number> meaning that it is highly compatible with other libraries
 */
export class Vector2 extends Vector {
    // Creates a new, empty vec2
    constructor(x = 0, y = 0) {
        // PERF NOTE: initialize elements as double precision numbers
        super(2); // -0, -0);
        if (isArray(x) && arguments.length === 1) {
            this.copy(x);
        }
        else {
            if (config.debug) {
                checkNumber(x);
                checkNumber(y);
            }
            this[0] = x;
            this[1] = y;
        }
    }
    set(x, y) {
        this[0] = x;
        this[1] = y;
        return this.check();
    }
    copy(array) {
        this[0] = array[0];
        this[1] = array[1];
        return this.check();
    }
    fromObject(object) {
        if (config.debug) {
            checkNumber(object.x);
            checkNumber(object.y);
        }
        this[0] = object.x;
        this[1] = object.y;
        return this.check();
    }
    toObject(object) {
        object.x = this[0];
        object.y = this[1];
        return object;
    }
    // Getters/setters
    get ELEMENTS() {
        return 2;
    }
    /**
     * Returns angle from x axis
     * @returns
     */
    horizontalAngle() {
        return Math.atan2(this.y, this.x);
    }
    /**
     * Returns angle from y axis
     * @returns
     */
    verticalAngle() {
        return Math.atan2(this.x, this.y);
    }
    // Transforms
    /**
     * Transforms as point
     * @param matrix4
     * @returns
     */
    transform(matrix4) {
        return this.transformAsPoint(matrix4);
    }
    /**
     * transforms as point (4th component is implicitly 1)
     * @param matrix4
     * @returns
     */
    transformAsPoint(matrix4) {
        vec2_transformMat4(this, this, matrix4);
        return this.check();
    }
    /**
     * transforms as vector (4th component is implicitly 0, ignores translation. slightly faster)
     * @param matrix4
     * @returns
     */
    transformAsVector(matrix4) {
        vec2_transformMat4AsVector(this, this, matrix4);
        return this.check();
    }
    transformByMatrix3(matrix3) {
        vec2_transformMat3(this, this, matrix3);
        return this.check();
    }
    transformByMatrix2x3(matrix2x3) {
        vec2_transformMat2d(this, this, matrix2x3);
        return this.check();
    }
    transformByMatrix2(matrix2) {
        vec2_transformMat2(this, this, matrix2);
        return this.check();
    }
}
//# sourceMappingURL=vector2.js.map