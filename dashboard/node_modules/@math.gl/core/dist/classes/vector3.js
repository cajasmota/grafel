// math.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
// Copyright (c) 2017 Uber Technologies, Inc.
import { Vector } from "./base/vector.js";
import { config, isArray } from "../lib/common.js";
import { checkNumber } from "../lib/validators.js";
// @ts-ignore gl-matrix types
import { angle as vec3_angle, cross as vec3_cross, rotateX as vec3_rotateX, rotateY as vec3_rotateY, rotateZ as vec3_rotateZ, transformMat4 as vec3_transformMat4, transformMat3 as vec3_transformMat3, transformQuat as vec3_transformQuat } from "../gl-matrix/vec3.js";
/* eslint-disable camelcase */
import { vec3_transformMat2, vec3_transformMat4AsVector } from "../lib/gl-matrix-extras.js";
const ORIGIN = [0, 0, 0];
let ZERO;
/**
 * Three-element vector class with common linear algebra operations.
 * Subclass of Array<number> meaning that it is highly compatible with other libraries
 */
export class Vector3 extends Vector {
    static get ZERO() {
        if (!ZERO) {
            ZERO = new Vector3(0, 0, 0);
            Object.freeze(ZERO);
        }
        return ZERO;
    }
    /**
     * @class
     * @param x
     * @param y
     * @param z
     */
    constructor(x = 0, y = 0, z = 0) {
        // PERF NOTE: initialize elements as double precision numbers
        super(-0, -0, -0);
        if (arguments.length === 1 && isArray(x)) {
            this.copy(x);
        }
        else {
            // this.set(x, y, z);
            if (config.debug) {
                checkNumber(x);
                checkNumber(y);
                checkNumber(z);
            }
            // @ts-expect-error TS2412: Property '0' of type 'number | [number, number, number]' is not assignable to numeric index type 'number'
            this[0] = x;
            this[1] = y;
            this[2] = z;
        }
    }
    set(x, y, z) {
        this[0] = x;
        this[1] = y;
        this[2] = z;
        return this.check();
    }
    copy(array) {
        this[0] = array[0];
        this[1] = array[1];
        this[2] = array[2];
        return this.check();
    }
    fromObject(object) {
        if (config.debug) {
            checkNumber(object.x);
            checkNumber(object.y);
            checkNumber(object.z);
        }
        this[0] = object.x;
        this[1] = object.y;
        this[2] = object.z;
        return this.check();
    }
    toObject(object) {
        object.x = this[0];
        object.y = this[1];
        object.z = this[2];
        return object;
    }
    // Getters/setters
    get ELEMENTS() {
        return 3;
    }
    get z() {
        return this[2];
    }
    set z(value) {
        this[2] = checkNumber(value);
    }
    // ACCESSORS
    angle(vector) {
        return vec3_angle(this, vector);
    }
    // MODIFIERS
    cross(vector) {
        vec3_cross(this, this, vector);
        return this.check();
    }
    rotateX({ radians, origin = ORIGIN }) {
        vec3_rotateX(this, this, origin, radians);
        return this.check();
    }
    rotateY({ radians, origin = ORIGIN }) {
        vec3_rotateY(this, this, origin, radians);
        return this.check();
    }
    rotateZ({ radians, origin = ORIGIN }) {
        vec3_rotateZ(this, this, origin, radians);
        return this.check();
    }
    // Transforms
    // transforms as point (4th component is implicitly 1)
    transform(matrix4) {
        return this.transformAsPoint(matrix4);
    }
    // transforms as point (4th component is implicitly 1)
    transformAsPoint(matrix4) {
        vec3_transformMat4(this, this, matrix4);
        return this.check();
    }
    // transforms as vector  (4th component is implicitly 0, ignores translation. slightly faster)
    transformAsVector(matrix4) {
        vec3_transformMat4AsVector(this, this, matrix4);
        return this.check();
    }
    transformByMatrix3(matrix3) {
        vec3_transformMat3(this, this, matrix3);
        return this.check();
    }
    transformByMatrix2(matrix2) {
        vec3_transformMat2(this, this, matrix2);
        return this.check();
    }
    transformByQuaternion(quaternion) {
        vec3_transformQuat(this, this, quaternion);
        return this.check();
    }
}
//# sourceMappingURL=vector3.js.map