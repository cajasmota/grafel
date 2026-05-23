// math.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
// Copyright (c) 2017 Uber Technologies, Inc.
/* eslint-disable camelcase */
import { transformMat4 as vec4_transformMat4, transformQuat as vec4_transformQuat } from "../gl-matrix/vec3.js";
import { vec4_transformMat2, vec4_transformMat3 } from "../lib/gl-matrix-extras.js";
import { Vector } from "./base/vector.js";
import { config, isArray } from "../lib/common.js";
import { checkNumber } from "../lib/validators.js";
let ZERO;
/**
 * Four-element vector class with common linear algebra operations.
 * Subclass of Array<number> meaning that it is highly compatible with other libraries
 */
export class Vector4 extends Vector {
    static get ZERO() {
        if (!ZERO) {
            ZERO = new Vector4(0, 0, 0, 0);
            Object.freeze(ZERO);
        }
        return ZERO;
    }
    constructor(x = 0, y = 0, z = 0, w = 0) {
        // PERF NOTE: initialize elements as double precision numbers
        super(-0, -0, -0, -0);
        if (isArray(x) && arguments.length === 1) {
            this.copy(x);
        }
        else {
            // this.set(x, y, z, w);
            if (config.debug) {
                checkNumber(x);
                checkNumber(y);
                checkNumber(z);
                checkNumber(w);
            }
            this[0] = x;
            this[1] = y;
            this[2] = z;
            this[3] = w;
        }
    }
    set(x, y, z, w) {
        this[0] = x;
        this[1] = y;
        this[2] = z;
        this[3] = w;
        return this.check();
    }
    copy(array) {
        this[0] = array[0];
        this[1] = array[1];
        this[2] = array[2];
        this[3] = array[3];
        return this.check();
    }
    fromObject(object) {
        if (config.debug) {
            checkNumber(object.x);
            checkNumber(object.y);
            checkNumber(object.z);
            checkNumber(object.w);
        }
        this[0] = object.x;
        this[1] = object.y;
        this[2] = object.z;
        this[3] = object.w;
        return this;
    }
    toObject(object) {
        object.x = this[0];
        object.y = this[1];
        object.z = this[2];
        object.w = this[3];
        return object;
    }
    // Getters/setters
    /* eslint-disable no-multi-spaces, brace-style, no-return-assign */
    get ELEMENTS() {
        return 4;
    }
    get z() {
        return this[2];
    }
    set z(value) {
        this[2] = checkNumber(value);
    }
    get w() {
        return this[3];
    }
    set w(value) {
        this[3] = checkNumber(value);
    }
    transform(matrix4) {
        vec4_transformMat4(this, this, matrix4);
        return this.check();
    }
    transformByMatrix3(matrix3) {
        vec4_transformMat3(this, this, matrix3);
        return this.check();
    }
    transformByMatrix2(matrix2) {
        vec4_transformMat2(this, this, matrix2);
        return this.check();
    }
    transformByQuaternion(quaternion) {
        vec4_transformQuat(this, this, quaternion);
        return this.check();
    }
    // three.js compatibility
    applyMatrix4(m) {
        m.transform(this, this);
        return this;
    }
}
//# sourceMappingURL=vector4.js.map