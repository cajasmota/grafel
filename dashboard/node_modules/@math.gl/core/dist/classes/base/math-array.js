// math.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { config, formatValue, equals, isArray } from "../../lib/common.js";
/** Base class for vectors and matrices */
export class MathArray extends Array {
    // Common methods
    /**
     * Clone the current object
     * @returns a new copy of this object
     */
    clone() {
        // @ts-expect-error TS2351: Cannot use 'new' with an expression whose type lacks a call or construct signature.
        return new this.constructor().copy(this); // eslint-disable-line
    }
    fromArray(array, offset = 0) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
            this[i] = array[i + offset];
        }
        return this.check();
    }
    toArray(targetArray = [], offset = 0) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
            targetArray[offset + i] = this[i];
        }
        return targetArray;
    }
    toObject(targetObject) {
        return targetObject;
    }
    from(arrayOrObject) {
        return Array.isArray(arrayOrObject)
            ? this.copy(arrayOrObject)
            : // @ts-ignore
                this.fromObject(arrayOrObject);
    }
    to(arrayOrObject) {
        // @ts-ignore
        if (arrayOrObject === this) {
            return this;
        }
        // @ts-expect-error TS2339: Property 'toObject' does not exist on type 'MathArray'.
        return isArray(arrayOrObject) ? this.toArray(arrayOrObject) : this.toObject(arrayOrObject);
    }
    toTarget(target) {
        return target ? this.to(target) : this;
    }
    /** @deprecated */
    toFloat32Array() {
        return new Float32Array(this);
    }
    toString() {
        return this.formatString(config);
    }
    /** Formats string according to options */
    formatString(opts) {
        let string = '';
        for (let i = 0; i < this.ELEMENTS; ++i) {
            string += (i > 0 ? ', ' : '') + formatValue(this[i], opts);
        }
        return `${opts.printTypes ? this.constructor.name : ''}[${string}]`;
    }
    equals(array) {
        if (!array || this.length !== array.length) {
            return false;
        }
        for (let i = 0; i < this.ELEMENTS; ++i) {
            if (!equals(this[i], array[i])) {
                return false;
            }
        }
        return true;
    }
    exactEquals(array) {
        if (!array || this.length !== array.length) {
            return false;
        }
        for (let i = 0; i < this.ELEMENTS; ++i) {
            if (this[i] !== array[i]) {
                return false;
            }
        }
        return true;
    }
    // Modifiers
    /** Negates all values in this object */
    negate() {
        for (let i = 0; i < this.ELEMENTS; ++i) {
            this[i] = -this[i];
        }
        return this.check();
    }
    lerp(a, b, t) {
        if (t === undefined) {
            return this.lerp(this, a, b);
        }
        for (let i = 0; i < this.ELEMENTS; ++i) {
            const ai = a[i];
            const endValue = typeof b === 'number' ? b : b[i];
            this[i] = ai + t * (endValue - ai);
        }
        return this.check();
    }
    /** Minimal */
    min(vector) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
            this[i] = Math.min(vector[i], this[i]);
        }
        return this.check();
    }
    /** Maximal */
    max(vector) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
            this[i] = Math.max(vector[i], this[i]);
        }
        return this.check();
    }
    clamp(minVector, maxVector) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
            this[i] = Math.min(Math.max(this[i], minVector[i]), maxVector[i]);
        }
        return this.check();
    }
    add(...vectors) {
        for (const vector of vectors) {
            for (let i = 0; i < this.ELEMENTS; ++i) {
                this[i] += vector[i];
            }
        }
        return this.check();
    }
    subtract(...vectors) {
        for (const vector of vectors) {
            for (let i = 0; i < this.ELEMENTS; ++i) {
                this[i] -= vector[i];
            }
        }
        return this.check();
    }
    scale(scale) {
        if (typeof scale === 'number') {
            for (let i = 0; i < this.ELEMENTS; ++i) {
                this[i] *= scale;
            }
        }
        else {
            for (let i = 0; i < this.ELEMENTS && i < scale.length; ++i) {
                this[i] *= scale[i];
            }
        }
        return this.check();
    }
    /**
     * Multiplies all elements by `scale`
     * Note: `Matrix4.multiplyByScalar` only scales its 3x3 "minor"
     */
    multiplyByScalar(scalar) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
            this[i] *= scalar;
        }
        return this.check();
    }
    // Debug checks
    /** Throws an error if array length is incorrect or contains illegal values */
    check() {
        if (config.debug && !this.validate()) {
            throw new Error(`math.gl: ${this.constructor.name} some fields set to invalid numbers'`);
        }
        return this;
    }
    /** Returns false if the array length is incorrect or contains illegal values */
    validate() {
        let valid = this.length === this.ELEMENTS;
        for (let i = 0; i < this.ELEMENTS; ++i) {
            valid = valid && Number.isFinite(this[i]);
        }
        return valid;
    }
    // three.js compatibility
    /** @deprecated */
    sub(a) {
        return this.subtract(a);
    }
    /** @deprecated */
    setScalar(a) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
            this[i] = a;
        }
        return this.check();
    }
    /** @deprecated */
    addScalar(a) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
            this[i] += a;
        }
        return this.check();
    }
    /** @deprecated */
    subScalar(a) {
        return this.addScalar(-a);
    }
    /** @deprecated */
    multiplyScalar(scalar) {
        // Multiplies all elements
        // `Matrix4.scale` only scales its 3x3 "minor"
        for (let i = 0; i < this.ELEMENTS; ++i) {
            this[i] *= scalar;
        }
        return this.check();
    }
    /** @deprecated */
    divideScalar(a) {
        return this.multiplyByScalar(1 / a);
    }
    /** @deprecated */
    clampScalar(min, max) {
        for (let i = 0; i < this.ELEMENTS; ++i) {
            this[i] = Math.min(Math.max(this[i], min), max);
        }
        return this.check();
    }
    /** @deprecated */
    get elements() {
        return this;
    }
}
//# sourceMappingURL=math-array.js.map