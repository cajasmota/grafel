// math.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
const RADIANS_TO_DEGREES = (1 / Math.PI) * 180;
const DEGREES_TO_RADIANS = (1 / 180) * Math.PI;
const DEFAULT_CONFIG = {
    EPSILON: 1e-12,
    debug: false,
    precision: 4,
    printTypes: false,
    printDegrees: false,
    printRowMajor: true,
    _cartographicRadians: false
};
// Configuration is truly global as of v3.6 to ensure single config even if multiple copies of math.gl
// Multiple copies of config can be quite tricky to debug...
globalThis.mathgl = globalThis.mathgl || { config: { ...DEFAULT_CONFIG } };
export const config = globalThis.mathgl.config;
export function configure(options) {
    // Only copy existing keys
    Object.assign(config, options);
    return config;
}
/**
 * Formats a value into a string
 * @param value
 * @param param1
 * @returns
 */
export function formatValue(value, { precision = config.precision } = {}) {
    value = round(value);
    // get rid of trailing zeros
    return `${parseFloat(value.toPrecision(precision))}`;
}
/**
 * Check if value is an "array"
 * Returns `true` if value is either an array or a typed array
 * Note: returns `false` for `ArrayBuffer` and `DataView` instances
 * @note isTypedArray and isNumericArray are often more useful in TypeScript
 */
export function isArray(value) {
    return Array.isArray(value) || (ArrayBuffer.isView(value) && !(value instanceof DataView));
}
export function clone(array) {
    return 'clone' in array ? array.clone() : array.slice();
}
export function toRadians(degrees) {
    return radians(degrees);
}
export function toDegrees(radians) {
    return degrees(radians);
}
export function radians(degrees, result) {
    return map(degrees, (degrees) => degrees * DEGREES_TO_RADIANS, result);
}
export function degrees(radians, result) {
    return map(radians, (radians) => radians * RADIANS_TO_DEGREES, result);
}
/**
 * "GLSL equivalent" of `Math.sin`: Works on single values and vectors
 * @deprecated
 */
export function sin(radians, result) {
    return map(radians, (angle) => Math.sin(angle), result);
}
/**
 * "GLSL equivalent" of `Math.cos`: Works on single values and vectors
 * @deprecated
 */
export function cos(radians, result) {
    return map(radians, (angle) => Math.cos(angle), result);
}
/**
 * "GLSL equivalent" of `Math.tan`: Works on single values and vectors
 * @deprecated
 */
export function tan(radians, result) {
    return map(radians, (angle) => Math.tan(angle), result);
}
/**
 * "GLSL equivalent" of `Math.asin`: Works on single values and vectors
 * @deprecated
 */
export function asin(radians, result) {
    return map(radians, (angle) => Math.asin(angle), result);
}
/**
 * "GLSL equivalent" of `Math.acos`: Works on single values and vectors
 * @deprecated
 */
export function acos(radians, result) {
    return map(radians, (angle) => Math.acos(angle), result);
}
/**
 * "GLSL equivalent" of `Math.atan`: Works on single values and vectors
 * @deprecated
 */
export function atan(radians, result) {
    return map(radians, (angle) => Math.atan(angle), result);
}
export function clamp(value, min, max) {
    return map(value, (value) => Math.max(min, Math.min(max, value)));
}
export function lerp(a, b, t) {
    if (isArray(a)) {
        return a.map((ai, i) => lerp(ai, b[i], t));
    }
    return t * b + (1 - t) * a;
}
/* eslint-disable */
/**
 * Compares any two math objects, using `equals` method if available.
 * @param a
 * @param b
 * @param epsilon
 * @returns
 */
export function equals(a, b, epsilon) {
    const oldEpsilon = config.EPSILON;
    if (epsilon) {
        config.EPSILON = epsilon;
    }
    try {
        if (a === b) {
            return true;
        }
        if (isArray(a) && isArray(b)) {
            if (a.length !== b.length) {
                return false;
            }
            for (let i = 0; i < a.length; ++i) {
                // eslint-disable-next-line max-depth
                if (!equals(a[i], b[i])) {
                    return false;
                }
            }
            return true;
        }
        if (a && a.equals) {
            return a.equals(b);
        }
        if (b && b.equals) {
            return b.equals(a);
        }
        if (typeof a === 'number' && typeof b === 'number') {
            return Math.abs(a - b) <= config.EPSILON * Math.max(1, Math.abs(a), Math.abs(b));
        }
        return false;
    }
    finally {
        config.EPSILON = oldEpsilon;
    }
}
export function exactEquals(a, b) {
    if (a === b) {
        return true;
    }
    if (a && typeof a === 'object' && b && typeof b === 'object') {
        if (a.constructor !== b.constructor) {
            return false;
        }
        if (a.exactEquals) {
            return a.exactEquals(b);
        }
    }
    if (isArray(a) && isArray(b)) {
        if (a.length !== b.length) {
            return false;
        }
        for (let i = 0; i < a.length; ++i) {
            if (!exactEquals(a[i], b[i])) {
                return false;
            }
        }
        return true;
    }
    return false;
}
/* eslint-enable */
export function withEpsilon(epsilon, func) {
    const oldPrecision = config.EPSILON;
    config.EPSILON = epsilon;
    let value;
    try {
        value = func();
    }
    finally {
        config.EPSILON = oldPrecision;
    }
    return value;
}
// HELPERS
function round(value) {
    return Math.round(value / config.EPSILON) * config.EPSILON;
}
// If the array has a clone function, calls it, otherwise returns a copy
function duplicateArray(array) {
    // @ts-expect-error We check for math.gl class methods
    // eslint-disable-next-line @typescript-eslint/no-unsafe-call
    return array.clone ? array.clone() : new Array(array.length);
}
// If the argument value is an array, applies the func element wise,
// otherwise applies func to the argument value
function map(value, func, result) {
    if (isArray(value)) {
        const array = value;
        result = result || duplicateArray(array);
        for (let i = 0; i < result.length && i < array.length; ++i) {
            const val = typeof value === 'number' ? value : value[i];
            result[i] = func(val, i, result);
        }
        return result;
    }
    return func(value);
}
//# sourceMappingURL=common.js.map