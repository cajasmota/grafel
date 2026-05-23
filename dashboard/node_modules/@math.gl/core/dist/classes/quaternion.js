// math.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
// Copyright (c) 2017 Uber Technologies, Inc.
import { MathArray } from "./base/math-array.js";
import { checkNumber, checkVector } from "../lib/validators.js";
import { Vector4 } from "./vector4.js";
// @ts-ignore gl-matrix types...
import { fromMat3 as quat_fromMat3, setAxisAngle as quat_setAxisAngle, identity as quat_identity, length as quat_length, squaredLength as quat_squaredLength, dot as quat_dot, 
// getAxisAngle as quat_getAxisAngle,
rotationTo as quat_rotationTo, add as quat_add, calculateW as quat_calculateW, conjugate as quat_conjugate, invert as quat_invert, lerp as quat_lerp, multiply as quat_multiply, rotateX as quat_rotateX, rotateY as quat_rotateY, rotateZ as quat_rotateZ, scale as quat_scale, slerp as quat_slerp } from "../gl-matrix/quat.js";
// @ts-ignore gl-matrix types...
import { transformQuat as vec4_transformQuat } from "../gl-matrix/vec4.js";
const IDENTITY_QUATERNION = [0, 0, 0, 1];
export class Quaternion extends MathArray {
    constructor(x = 0, y = 0, z = 0, w = 1) {
        // PERF NOTE: initialize elements as double precision numbers
        super(-0, -0, -0, -0);
        // eslint-disable-next-line prefer-rest-params
        if (Array.isArray(x) && arguments.length === 1) {
            this.copy(x);
        }
        else {
            this.set(x, y, z, w);
        }
    }
    copy(array) {
        this[0] = array[0];
        this[1] = array[1];
        this[2] = array[2];
        this[3] = array[3];
        return this.check();
    }
    set(x, y, z, w) {
        this[0] = x;
        this[1] = y;
        this[2] = z;
        this[3] = w;
        return this.check();
    }
    fromObject(object) {
        this[0] = object.x;
        this[1] = object.y;
        this[2] = object.z;
        this[3] = object.w;
        return this.check();
    }
    /**
     * Creates a quaternion from the given 3x3 rotation matrix.
     * NOTE: The resultant quaternion is not normalized, so you should
     * be sure to renormalize the quaternion yourself where necessary.
     * @param m
     * @returns
     */
    fromMatrix3(m) {
        quat_fromMat3(this, m);
        return this.check();
    }
    fromAxisRotation(axis, rad) {
        quat_setAxisAngle(this, axis, rad);
        return this.check();
    }
    /** Set a quat to the identity quaternion */
    identity() {
        quat_identity(this);
        return this.check();
    }
    // Set the components of a quat to the given values
    // set(i, j, k, l) {
    //   quat_set(this, i, j, k, l);
    //   return this.check();
    // }
    // Sets a quat from the given angle and rotation axis, then returns it.
    setAxisAngle(axis, rad) {
        return this.fromAxisRotation(axis, rad);
    }
    // Getters/setters
    get ELEMENTS() {
        return 4;
    }
    get x() {
        return this[0];
    }
    set x(value) {
        this[0] = checkNumber(value);
    }
    get y() {
        return this[1];
    }
    set y(value) {
        this[1] = checkNumber(value);
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
    // Calculates the length of a quat
    len() {
        return quat_length(this);
    }
    // Calculates the squared length of a quat
    lengthSquared() {
        return quat_squaredLength(this);
    }
    // Calculates the dot product of two quat's
    // @return {Number}
    dot(a) {
        return quat_dot(this, a);
    }
    // Gets the rotation axis and angle for a given quaternion.
    // If a quaternion is created with setAxisAngle, this method will
    // return the same values as providied in the original parameter
    // list OR functionally equivalent values.
    // Example: The quaternion formed by axis [0, 0, 1] and angle -90
    // is the same as the quaternion formed by [0, 0, 1] and 270.
    // This method favors the latter.
    // @return {{[x,y,z], Number}}
    // getAxisAngle() {
    //   const axis = [];
    // //   const angle = quat_getAxisAngle(axis, this);
    //   return {axis, angle};
    // }
    // MODIFIERS
    // Sets a quaternion to represent the shortest rotation from one vector
    // to another. Both vectors are assumed to be unit length.
    rotationTo(vectorA, vectorB) {
        quat_rotationTo(this, vectorA, vectorB);
        return this.check();
    }
    // Sets the specified quaternion with values corresponding to the given axes.
    // Each axis is a vec3 and is expected to be unit length and perpendicular
    // to all other specified axes.
    // setAxes() {
    //   Number
    // }
    // Performs a spherical linear interpolation with two control points
    // sqlerp() {
    //   Number;
    // }
    // Adds two quat's
    add(a) {
        quat_add(this, this, a);
        return this.check();
    }
    // Calculates the W component of a quat from the X, Y, and Z components.
    // Any existing W component will be ignored.
    calculateW() {
        quat_calculateW(this, this);
        return this.check();
    }
    // Calculates the conjugate of a quat If the quaternion is normalized,
    // this function is faster than quat_invert and produces the same result.
    conjugate() {
        quat_conjugate(this, this);
        return this.check();
    }
    // Calculates the inverse of a quat
    invert() {
        quat_invert(this, this);
        return this.check();
    }
    // Performs a linear interpolation between two quat's
    lerp(a, b, t) {
        if (t === undefined) {
            return this.lerp(this, a, b);
        }
        quat_lerp(this, a, b, t);
        return this.check();
    }
    // Multiplies two quat's
    multiplyRight(a) {
        quat_multiply(this, this, a);
        return this.check();
    }
    multiplyLeft(a) {
        quat_multiply(this, a, this);
        return this.check();
    }
    // Normalize a quat
    normalize() {
        // Handle 0 case
        const length = this.len();
        const l = length > 0 ? 1 / length : 0;
        this[0] = this[0] * l;
        this[1] = this[1] * l;
        this[2] = this[2] * l;
        this[3] = this[3] * l;
        // Set to [0, 0, 0, 1] if length is 0
        if (length === 0) {
            this[3] = 1;
        }
        return this.check();
    }
    // Rotates a quaternion by the given angle about the X axis
    rotateX(rad) {
        quat_rotateX(this, this, rad);
        return this.check();
    }
    // Rotates a quaternion by the given angle about the Y axis
    rotateY(rad) {
        quat_rotateY(this, this, rad);
        return this.check();
    }
    // Rotates a quaternion by the given angle about the Z axis
    rotateZ(rad) {
        quat_rotateZ(this, this, rad);
        return this.check();
    }
    // Scales a quat by a scalar number
    scale(b) {
        quat_scale(this, this, b);
        return this.check();
    }
    // Performs a spherical linear interpolation between two quat
    slerp(arg0, arg1, arg2) {
        let start;
        let target;
        let ratio;
        // eslint-disable-next-line prefer-rest-params
        switch (arguments.length) {
            case 1: // Deprecated signature ({start, target, ratio})
                // eslint-disable-next-line prefer-rest-params
                ({
                    start = IDENTITY_QUATERNION,
                    target,
                    ratio
                } = arg0);
                break;
            case 2: // THREE.js compatibility signature (target, ration)
                start = this; // eslint-disable-line
                target = arg0;
                ratio = arg1;
                break;
            default:
                // Default signature: (start, target, ratio)
                start = arg0;
                target = arg1;
                ratio = arg2;
        }
        quat_slerp(this, start, target, ratio);
        return this.check();
    }
    transformVector4(vector, result = new Vector4()) {
        vec4_transformQuat(result, vector, this);
        return checkVector(result, 4);
    }
    // THREE.js Math API compatibility
    lengthSq() {
        return this.lengthSquared();
    }
    setFromAxisAngle(axis, rad) {
        return this.setAxisAngle(axis, rad);
    }
    premultiply(a) {
        return this.multiplyLeft(a);
    }
    multiply(a) {
        return this.multiplyRight(a);
    }
}
//# sourceMappingURL=quaternion.js.map