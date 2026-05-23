// math.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
// Copyright (c) 2017 Uber Technologies, Inc.
import { Matrix } from "./base/matrix.js";
import { checkVector } from "../lib/validators.js";
import { vec4_transformMat3 } from "../lib/gl-matrix-extras.js";
import { fromQuat as mat3_fromQuat, determinant as mat3_determinant, transpose as mat3_transpose, invert as mat3_invert, multiply as mat3_multiply, rotate as mat3_rotate, scale as mat3_scale, translate as mat3_translate } from "../gl-matrix/mat3.js";
import { transformMat3 as vec2_transformMat3 } from "../gl-matrix/vec2.js";
import { transformMat3 as vec3_transformMat3 } from "../gl-matrix/vec3.js";
// eslint-disable-next-line no-shadow
var INDICES;
(function (INDICES) {
    INDICES[INDICES["COL0ROW0"] = 0] = "COL0ROW0";
    INDICES[INDICES["COL0ROW1"] = 1] = "COL0ROW1";
    INDICES[INDICES["COL0ROW2"] = 2] = "COL0ROW2";
    INDICES[INDICES["COL1ROW0"] = 3] = "COL1ROW0";
    INDICES[INDICES["COL1ROW1"] = 4] = "COL1ROW1";
    INDICES[INDICES["COL1ROW2"] = 5] = "COL1ROW2";
    INDICES[INDICES["COL2ROW0"] = 6] = "COL2ROW0";
    INDICES[INDICES["COL2ROW1"] = 7] = "COL2ROW1";
    INDICES[INDICES["COL2ROW2"] = 8] = "COL2ROW2";
})(INDICES || (INDICES = {}));
const IDENTITY_MATRIX = Object.freeze([1, 0, 0, 0, 1, 0, 0, 0, 1]);
/**
 * A 3x3 matrix with common linear algebra operations
 * Subclass of Array<number> meaning that it is highly compatible with other libraries
 */
export class Matrix3 extends Matrix {
    static get IDENTITY() {
        return getIdentityMatrix();
    }
    static get ZERO() {
        return getZeroMatrix();
    }
    get ELEMENTS() {
        return 9;
    }
    get RANK() {
        return 3;
    }
    get INDICES() {
        return INDICES;
    }
    constructor(array, ...args) {
        // PERF NOTE: initialize elements as double precision numbers
        super(-0, -0, -0, -0, -0, -0, -0, -0, -0);
        if (arguments.length === 1 && Array.isArray(array)) {
            this.copy(array);
        }
        else if (args.length > 0) {
            this.copy([array, ...args]);
        }
        else {
            this.identity();
        }
    }
    copy(array) {
        // Element wise copy for performance
        this[0] = array[0];
        this[1] = array[1];
        this[2] = array[2];
        this[3] = array[3];
        this[4] = array[4];
        this[5] = array[5];
        this[6] = array[6];
        this[7] = array[7];
        this[8] = array[8];
        return this.check();
    }
    // Constructors
    identity() {
        return this.copy(IDENTITY_MATRIX);
    }
    /**
     *
     * @param object
     * @returns self
     */
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    fromObject(object) {
        return this.check();
    }
    /** Calculates a 3x3 matrix from the given quaternion
     * q quat  Quaternion to create matrix from
     */
    fromQuaternion(q) {
        mat3_fromQuat(this, q);
        return this.check();
    }
    /**
     * accepts column major order, stores in column major order
     */
    // eslint-disable-next-line max-params
    set(m00, m10, m20, m01, m11, m21, m02, m12, m22) {
        this[0] = m00;
        this[1] = m10;
        this[2] = m20;
        this[3] = m01;
        this[4] = m11;
        this[5] = m21;
        this[6] = m02;
        this[7] = m12;
        this[8] = m22;
        return this.check();
    }
    /**
     * accepts row major order, stores as column major
     */
    // eslint-disable-next-line max-params
    setRowMajor(m00, m01, m02, m10, m11, m12, m20, m21, m22) {
        this[0] = m00;
        this[1] = m10;
        this[2] = m20;
        this[3] = m01;
        this[4] = m11;
        this[5] = m21;
        this[6] = m02;
        this[7] = m12;
        this[8] = m22;
        return this.check();
    }
    // Accessors
    determinant() {
        return mat3_determinant(this);
    }
    // Modifiers
    transpose() {
        mat3_transpose(this, this);
        return this.check();
    }
    /** Invert a matrix. Note that this can fail if the matrix is not invertible */
    invert() {
        mat3_invert(this, this);
        return this.check();
    }
    // Operations
    multiplyLeft(a) {
        mat3_multiply(this, a, this);
        return this.check();
    }
    multiplyRight(a) {
        mat3_multiply(this, this, a);
        return this.check();
    }
    rotate(radians) {
        mat3_rotate(this, this, radians);
        return this.check();
    }
    scale(factor) {
        if (Array.isArray(factor)) {
            mat3_scale(this, this, factor);
        }
        else {
            mat3_scale(this, this, [factor, factor]);
        }
        return this.check();
    }
    translate(vec) {
        mat3_translate(this, this, vec);
        return this.check();
    }
    // Transforms
    transform(vector, result) {
        let out;
        switch (vector.length) {
            case 2:
                out = vec2_transformMat3(result || [-0, -0], vector, this);
                break;
            case 3:
                out = vec3_transformMat3(result || [-0, -0, -0], vector, this);
                break;
            case 4:
                out = vec4_transformMat3(result || [-0, -0, -0, -0], vector, this);
                break;
            default:
                throw new Error('Illegal vector');
        }
        checkVector(out, vector.length);
        return out;
    }
    /** @deprecated */
    transformVector(vector, result) {
        return this.transform(vector, result);
    }
    /** @deprecated */
    transformVector2(vector, result) {
        return this.transform(vector, result);
    }
    /** @deprecated */
    transformVector3(vector, result) {
        return this.transform(vector, result);
    }
}
let ZERO_MATRIX3;
let IDENTITY_MATRIX3 = null;
function getZeroMatrix() {
    if (!ZERO_MATRIX3) {
        ZERO_MATRIX3 = new Matrix3([0, 0, 0, 0, 0, 0, 0, 0, 0]);
        Object.freeze(ZERO_MATRIX3);
    }
    return ZERO_MATRIX3;
}
function getIdentityMatrix() {
    if (!IDENTITY_MATRIX3) {
        IDENTITY_MATRIX3 = new Matrix3();
        Object.freeze(IDENTITY_MATRIX3);
    }
    return IDENTITY_MATRIX3;
}
//# sourceMappingURL=matrix3.js.map