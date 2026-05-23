import type { NumericArray } from '@math.gl/types';
/**
 * 2 Dimensional Vector
 * @module vec2
 */
/**
 * Creates a new, empty vec2
 *
 * @returns a new 2D vector
 */
export declare function create(): NumericArray;
/**
 * Creates a new vec2 initialized with values from an existing vector
 *
 * @param a vector to clone
 * @returns a new 2D vector
 */
export declare function clone(a: Readonly<NumericArray>): NumericArray;
/**
 * Creates a new vec2 initialized with the given values
 *
 * @param x X component
 * @param y Y component
 * @returns a new 2D vector
 */
export declare function fromValues(x: number, y: number): NumericArray;
/**
 * Copy the values from one vec2 to another
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the source vector
 * @returns {NumericArray} out
 */
export declare function copy(out: any, a: any): any;
/**
 * Set the components of a vec2 to the given values
 *
 * @param {NumericArray} out the receiving vector
 * @param {Number} x X component
 * @param {Number} y Y component
 * @returns {NumericArray} out
 */
export declare function set(out: any, x: any, y: any): any;
/**
 * Adds two vec2's
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the first operand
 * @param {Readonly<NumericArray>} b the second operand
 * @returns {NumericArray} out
 */
export declare function add(out: any, a: any, b: any): any;
/**
 * Subtracts vector b from vector a
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the first operand
 * @param {Readonly<NumericArray>} b the second operand
 * @returns {NumericArray} out
 */
export declare function subtract(out: any, a: any, b: any): any;
/**
 * Multiplies two vec2's
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the first operand
 * @param {Readonly<NumericArray>} b the second operand
 * @returns {NumericArray} out
 */
export declare function multiply(out: any, a: any, b: any): any;
/**
 * Divides two vec2's
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the first operand
 * @param {Readonly<NumericArray>} b the second operand
 * @returns {NumericArray} out
 */
export declare function divide(out: any, a: any, b: any): any;
/**
 * Math.ceil the components of a vec2
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a vector to ceil
 * @returns {NumericArray} out
 */
export declare function ceil(out: any, a: any): any;
/**
 * Math.floor the components of a vec2
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a vector to floor
 * @returns {NumericArray} out
 */
export declare function floor(out: any, a: any): any;
/**
 * Returns the minimum of two vec2's
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the first operand
 * @param {Readonly<NumericArray>} b the second operand
 * @returns {NumericArray} out
 */
export declare function min(out: any, a: any, b: any): any;
/**
 * Returns the maximum of two vec2's
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the first operand
 * @param {Readonly<NumericArray>} b the second operand
 * @returns {NumericArray} out
 */
export declare function max(out: any, a: any, b: any): any;
/**
 * symmetric round the components of a vec2
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a vector to round
 * @returns {NumericArray} out
 */
export declare function round(out: any, a: any): any;
/**
 * Scales a vec2 by a scalar number
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the vector to scale
 * @param {Number} b amount to scale the vector by
 * @returns {NumericArray} out
 */
export declare function scale(out: any, a: any, b: any): any;
/**
 * Adds two vec2's after scaling the second operand by a scalar value
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the first operand
 * @param {Readonly<NumericArray>} b the second operand
 * @param {Number} scale the amount to scale b by before adding
 * @returns {NumericArray} out
 */
export declare function scaleAndAdd(out: any, a: any, b: any, scale: any): any;
/**
 * Calculates the euclidian distance between two vec2's
 *
 * @param {Readonly<NumericArray>} a the first operand
 * @param {Readonly<NumericArray>} b the second operand
 * @returns {Number} distance between a and b
 */
export declare function distance(a: any, b: any): number;
/**
 * Calculates the squared euclidian distance between two vec2's
 *
 * @param {Readonly<NumericArray>} a the first operand
 * @param {Readonly<NumericArray>} b the second operand
 * @returns {Number} squared distance between a and b
 */
export declare function squaredDistance(a: any, b: any): number;
/**
 * Calculates the length of a vec2
 *
 * @param {Readonly<NumericArray>} a vector to calculate length of
 * @returns {Number} length of a
 */
export declare function length(a: any): number;
/**
 * Calculates the squared length of a vec2
 *
 * @param {Readonly<NumericArray>} a vector to calculate squared length of
 * @returns {Number} squared length of a
 */
export declare function squaredLength(a: any): number;
/**
 * Negates the components of a vec2
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a vector to negate
 * @returns {NumericArray} out
 */
export declare function negate(out: any, a: any): any;
/**
 * Returns the inverse of the components of a vec2
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a vector to invert
 * @returns {NumericArray} out
 */
export declare function inverse(out: any, a: any): any;
/**
 * Normalize a vec2
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a vector to normalize
 * @returns {NumericArray} out
 */
export declare function normalize(out: any, a: any): any;
/**
 * Calculates the dot product of two vec2's
 *
 * @param {Readonly<NumericArray>} a the first operand
 * @param {Readonly<NumericArray>} b the second operand
 * @returns {Number} dot product of a and b
 */
export declare function dot(a: any, b: any): number;
/**
 * Computes the cross product of two vec2's
 * Note that the cross product must by definition produce a 3D vector
 *
 * @param {vec3} out the receiving vector
 * @param {Readonly<NumericArray>} a the first operand
 * @param {Readonly<NumericArray>} b the second operand
 * @returns {vec3} out
 */
export declare function cross(out: any, a: any, b: any): any;
/**
 * Performs a linear interpolation between two vec2's
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the first operand
 * @param {Readonly<NumericArray>} b the second operand
 * @param {Number} t interpolation amount, in the range [0-1], between the two inputs
 * @returns {NumericArray} out
 */
export declare function lerp(out: any, a: any, b: any, t: any): any;
/**
 * Generates a random vector with the given scale
 *
 * @param {NumericArray} out the receiving vector
 * @param {Number} [scale] Length of the resulting vector. If omitted, a unit vector will be returned
 * @returns {NumericArray} out
 */
export declare function random(out: any, scale: any): any;
/**
 * Transforms the vec2 with a mat2
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the vector to transform
 * @param {ReadonlyMat2} m matrix to transform with
 * @returns {NumericArray} out
 */
export declare function transformMat2(out: any, a: any, m: any): any;
/**
 * Transforms the vec2 with a mat2d
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the vector to transform
 * @param {ReadonlyMat2d} m matrix to transform with
 * @returns {NumericArray} out
 */
export declare function transformMat2d(out: any, a: any, m: any): any;
/**
 * Transforms the vec2 with a mat3
 * 3rd vector component is implicitly '1'
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the vector to transform
 * @param {ReadonlyMat3} m matrix to transform with
 * @returns {NumericArray} out
 */
export declare function transformMat3(out: any, a: any, m: any): any;
/**
 * Transforms the vec2 with a mat4
 * 3rd vector component is implicitly '0'
 * 4th vector component is implicitly '1'
 *
 * @param {NumericArray} out the receiving vector
 * @param {Readonly<NumericArray>} a the vector to transform
 * @param {ReadonlyMat4} m matrix to transform with
 * @returns {NumericArray} out
 */
export declare function transformMat4(out: any, a: any, m: any): any;
/**
 * Rotate a 2D vector
 * @param {NumericArray} out The receiving vec2
 * @param {Readonly<NumericArray>} a The vec2 point to rotate
 * @param {Readonly<NumericArray>} b The origin of the rotation
 * @param {Number} rad The angle of rotation in radians
 * @returns {NumericArray} out
 */
export declare function rotate(out: any, a: any, b: any, rad: any): any;
/**
 * Get the angle between two 2D vectors
 * @param {Readonly<NumericArray>} a The first operand
 * @param {Readonly<NumericArray>} b The second operand
 * @returns {Number} The angle in radians
 */
export declare function angle(a: any, b: any): number;
/**
 * Set the components of a vec2 to zero
 *
 * @param {NumericArray} out the receiving vector
 * @returns {NumericArray} out
 */
export declare function zero(out: any): any;
/**
 * Returns a string representation of a vector
 *
 * @param {Readonly<NumericArray>} a vector to represent as a string
 * @returns {String} string representation of the vector
 */
export declare function str(a: any): string;
/**
 * Returns whether or not the vectors exactly have the same elements in the same position (when compared with ===)
 *
 * @param {Readonly<NumericArray>} a The first vector.
 * @param {Readonly<NumericArray>} b The second vector.
 * @returns {Boolean} True if the vectors are equal, false otherwise.
 */
export declare function exactEquals(a: any, b: any): boolean;
/**
 * Returns whether or not the vectors have approximately the same elements in the same position.
 *
 * @param {Readonly<NumericArray>} a The first vector.
 * @param {Readonly<NumericArray>} b The second vector.
 * @returns {Boolean} True if the vectors are equal, false otherwise.
 */
export declare function equals(a: any, b: any): boolean;
/**
 * Alias for {@link vec2.length}
 * @function
 */
export declare const len: typeof length;
/**
 * Alias for {@link vec2.subtract}
 * @function
 */
export declare const sub: typeof subtract;
/**
 * Alias for {@link vec2.multiply}
 * @function
 */
export declare const mul: typeof multiply;
/**
 * Alias for {@link vec2.divide}
 * @function
 */
export declare const div: typeof divide;
/**
 * Alias for {@link vec2.distance}
 * @function
 */
export declare const dist: typeof distance;
/**
 * Alias for {@link vec2.squaredDistance}
 * @function
 */
export declare const sqrDist: typeof squaredDistance;
/**
 * Alias for {@link vec2.squaredLength}
 * @function
 */
export declare const sqrLen: typeof squaredLength;
/**
 * Perform some operation over an array of vec2s.
 *
 * @param {Array} a the array of vectors to iterate over
 * @param {Number} stride Number of elements between the start of each vec2. If 0 assumes tightly packed
 * @param {Number} offset Number of elements to skip at the beginning of the array
 * @param {Number} count Number of vec2s to iterate over. If 0 iterates over entire array
 * @param {Function} fn Function to call for each vector in the array
 * @param {Object} [arg] additional argument to pass to fn
 * @returns {Array} a
 * @function
 */
export declare const forEach: (a: any, stride: any, offset: any, count: any, fn: any, arg: any) => any;
//# sourceMappingURL=vec2.d.ts.map