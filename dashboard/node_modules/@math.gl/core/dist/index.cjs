var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __export = (target, all) => {
  for (var name in all)
    __defProp(target, name, { get: all[name], enumerable: true });
};
var __copyProps = (to, from, except, desc) => {
  if (from && typeof from === "object" || typeof from === "function") {
    for (let key of __getOwnPropNames(from))
      if (!__hasOwnProp.call(to, key) && key !== except)
        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
  }
  return to;
};
var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);

// dist/index.js
var dist_exports = {};
__export(dist_exports, {
  Euler: () => Euler,
  Matrix3: () => Matrix3,
  Matrix4: () => Matrix4,
  Pose: () => Pose,
  Quaternion: () => Quaternion,
  SphericalCoordinates: () => SphericalCoordinates,
  Vector2: () => Vector2,
  Vector3: () => Vector3,
  Vector4: () => Vector4,
  _Euler: () => Euler,
  _MathUtils: () => math_utils_exports,
  _Pose: () => Pose,
  _SphericalCoordinates: () => SphericalCoordinates,
  acos: () => acos,
  asin: () => asin,
  assert: () => assert,
  atan: () => atan,
  clamp: () => clamp,
  clone: () => clone,
  config: () => config,
  configure: () => configure,
  cos: () => cos,
  degrees: () => degrees,
  equals: () => equals,
  exactEquals: () => exactEquals,
  formatValue: () => formatValue,
  isArray: () => isArray,
  lerp: () => lerp,
  mat3: () => mat3_exports,
  mat4: () => mat4_exports,
  quat: () => quat_exports,
  radians: () => radians,
  sin: () => sin,
  tan: () => tan,
  toDegrees: () => toDegrees,
  toRadians: () => toRadians,
  vec2: () => vec2_exports,
  vec3: () => vec3_exports,
  vec4: () => vec4_exports,
  withEpsilon: () => withEpsilon
});
module.exports = __toCommonJS(dist_exports);

// dist/lib/common.js
var RADIANS_TO_DEGREES = 1 / Math.PI * 180;
var DEGREES_TO_RADIANS = 1 / 180 * Math.PI;
var DEFAULT_CONFIG = {
  EPSILON: 1e-12,
  debug: false,
  precision: 4,
  printTypes: false,
  printDegrees: false,
  printRowMajor: true,
  _cartographicRadians: false
};
globalThis.mathgl = globalThis.mathgl || { config: { ...DEFAULT_CONFIG } };
var config = globalThis.mathgl.config;
function configure(options) {
  Object.assign(config, options);
  return config;
}
function formatValue(value, { precision = config.precision } = {}) {
  value = round(value);
  return `${parseFloat(value.toPrecision(precision))}`;
}
function isArray(value) {
  return Array.isArray(value) || ArrayBuffer.isView(value) && !(value instanceof DataView);
}
function clone(array) {
  return "clone" in array ? array.clone() : array.slice();
}
function toRadians(degrees2) {
  return radians(degrees2);
}
function toDegrees(radians2) {
  return degrees(radians2);
}
function radians(degrees2, result) {
  return map(degrees2, (degrees3) => degrees3 * DEGREES_TO_RADIANS, result);
}
function degrees(radians2, result) {
  return map(radians2, (radians3) => radians3 * RADIANS_TO_DEGREES, result);
}
function sin(radians2, result) {
  return map(radians2, (angle3) => Math.sin(angle3), result);
}
function cos(radians2, result) {
  return map(radians2, (angle3) => Math.cos(angle3), result);
}
function tan(radians2, result) {
  return map(radians2, (angle3) => Math.tan(angle3), result);
}
function asin(radians2, result) {
  return map(radians2, (angle3) => Math.asin(angle3), result);
}
function acos(radians2, result) {
  return map(radians2, (angle3) => Math.acos(angle3), result);
}
function atan(radians2, result) {
  return map(radians2, (angle3) => Math.atan(angle3), result);
}
function clamp(value, min4, max4) {
  return map(value, (value2) => Math.max(min4, Math.min(max4, value2)));
}
function lerp(a, b, t) {
  if (isArray(a)) {
    return a.map((ai, i) => lerp(ai, b[i], t));
  }
  return t * b + (1 - t) * a;
}
function equals(a, b, epsilon) {
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
    if (typeof a === "number" && typeof b === "number") {
      return Math.abs(a - b) <= config.EPSILON * Math.max(1, Math.abs(a), Math.abs(b));
    }
    return false;
  } finally {
    config.EPSILON = oldEpsilon;
  }
}
function exactEquals(a, b) {
  if (a === b) {
    return true;
  }
  if (a && typeof a === "object" && b && typeof b === "object") {
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
function withEpsilon(epsilon, func) {
  const oldPrecision = config.EPSILON;
  config.EPSILON = epsilon;
  let value;
  try {
    value = func();
  } finally {
    config.EPSILON = oldPrecision;
  }
  return value;
}
function round(value) {
  return Math.round(value / config.EPSILON) * config.EPSILON;
}
function duplicateArray(array) {
  return array.clone ? array.clone() : new Array(array.length);
}
function map(value, func, result) {
  if (isArray(value)) {
    const array = value;
    result = result || duplicateArray(array);
    for (let i = 0; i < result.length && i < array.length; ++i) {
      const val = typeof value === "number" ? value : value[i];
      result[i] = func(val, i, result);
    }
    return result;
  }
  return func(value);
}

// dist/classes/base/math-array.js
var MathArray = class extends Array {
  // Common methods
  /**
   * Clone the current object
   * @returns a new copy of this object
   */
  clone() {
    return new this.constructor().copy(this);
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
    return Array.isArray(arrayOrObject) ? this.copy(arrayOrObject) : (
      // @ts-ignore
      this.fromObject(arrayOrObject)
    );
  }
  to(arrayOrObject) {
    if (arrayOrObject === this) {
      return this;
    }
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
    let string = "";
    for (let i = 0; i < this.ELEMENTS; ++i) {
      string += (i > 0 ? ", " : "") + formatValue(this[i], opts);
    }
    return `${opts.printTypes ? this.constructor.name : ""}[${string}]`;
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
    if (t === void 0) {
      return this.lerp(this, a, b);
    }
    for (let i = 0; i < this.ELEMENTS; ++i) {
      const ai = a[i];
      const endValue = typeof b === "number" ? b : b[i];
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
  scale(scale7) {
    if (typeof scale7 === "number") {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] *= scale7;
      }
    } else {
      for (let i = 0; i < this.ELEMENTS && i < scale7.length; ++i) {
        this[i] *= scale7[i];
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
  clampScalar(min4, max4) {
    for (let i = 0; i < this.ELEMENTS; ++i) {
      this[i] = Math.min(Math.max(this[i], min4), max4);
    }
    return this.check();
  }
  /** @deprecated */
  get elements() {
    return this;
  }
};

// dist/lib/validators.js
function validateVector(v, length5) {
  if (v.length !== length5) {
    return false;
  }
  for (let i = 0; i < v.length; ++i) {
    if (!Number.isFinite(v[i])) {
      return false;
    }
  }
  return true;
}
function checkNumber(value) {
  if (!Number.isFinite(value)) {
    throw new Error(`Invalid number ${JSON.stringify(value)}`);
  }
  return value;
}
function checkVector(v, length5, callerName = "") {
  if (config.debug && !validateVector(v, length5)) {
    throw new Error(`math.gl: ${callerName} some fields set to invalid numbers'`);
  }
  return v;
}

// dist/lib/assert.js
function assert(condition, message) {
  if (!condition) {
    throw new Error(`math.gl assertion ${message}`);
  }
}

// dist/classes/base/vector.js
var Vector = class extends MathArray {
  // ACCESSORS
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
  /**
   * Returns the length of the vector from the origin to the point described by this vector
   *
   * @note `length` is a reserved word for Arrays, so `v.length()` will return number of elements
   * Instead we provide `len` and `magnitude`
   */
  len() {
    return Math.sqrt(this.lengthSquared());
  }
  /**
   * Returns the length of the vector from the origin to the point described by this vector
   */
  magnitude() {
    return this.len();
  }
  /**
   * Returns the squared length of the vector from the origin to the point described by this vector
   */
  lengthSquared() {
    let length5 = 0;
    for (let i = 0; i < this.ELEMENTS; ++i) {
      length5 += this[i] * this[i];
    }
    return length5;
  }
  /**
   * Returns the squared length of the vector from the origin to the point described by this vector
   */
  magnitudeSquared() {
    return this.lengthSquared();
  }
  distance(mathArray) {
    return Math.sqrt(this.distanceSquared(mathArray));
  }
  distanceSquared(mathArray) {
    let length5 = 0;
    for (let i = 0; i < this.ELEMENTS; ++i) {
      const dist4 = this[i] - mathArray[i];
      length5 += dist4 * dist4;
    }
    return checkNumber(length5);
  }
  dot(mathArray) {
    let product = 0;
    for (let i = 0; i < this.ELEMENTS; ++i) {
      product += this[i] * mathArray[i];
    }
    return checkNumber(product);
  }
  // MODIFIERS
  normalize() {
    const length5 = this.magnitude();
    if (length5 !== 0) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] /= length5;
      }
    }
    return this.check();
  }
  multiply(...vectors) {
    for (const vector of vectors) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] *= vector[i];
      }
    }
    return this.check();
  }
  divide(...vectors) {
    for (const vector of vectors) {
      for (let i = 0; i < this.ELEMENTS; ++i) {
        this[i] /= vector[i];
      }
    }
    return this.check();
  }
  // THREE.js compatibility
  lengthSq() {
    return this.lengthSquared();
  }
  distanceTo(vector) {
    return this.distance(vector);
  }
  distanceToSquared(vector) {
    return this.distanceSquared(vector);
  }
  getComponent(i) {
    assert(i >= 0 && i < this.ELEMENTS, "index is out of range");
    return checkNumber(this[i]);
  }
  setComponent(i, value) {
    assert(i >= 0 && i < this.ELEMENTS, "index is out of range");
    this[i] = value;
    return this.check();
  }
  addVectors(a, b) {
    return this.copy(a).add(b);
  }
  subVectors(a, b) {
    return this.copy(a).subtract(b);
  }
  multiplyVectors(a, b) {
    return this.copy(a).multiply(b);
  }
  addScaledVector(a, b) {
    return this.add(new this.constructor(a).multiplyScalar(b));
  }
};

// dist/gl-matrix/vec2.js
var vec2_exports = {};
__export(vec2_exports, {
  add: () => add,
  angle: () => angle,
  ceil: () => ceil,
  clone: () => clone2,
  copy: () => copy,
  create: () => create,
  cross: () => cross,
  dist: () => dist,
  distance: () => distance,
  div: () => div,
  divide: () => divide,
  dot: () => dot,
  equals: () => equals2,
  exactEquals: () => exactEquals2,
  floor: () => floor,
  forEach: () => forEach,
  fromValues: () => fromValues,
  inverse: () => inverse,
  len: () => len,
  length: () => length,
  lerp: () => lerp2,
  max: () => max,
  min: () => min,
  mul: () => mul,
  multiply: () => multiply,
  negate: () => negate,
  normalize: () => normalize,
  random: () => random,
  rotate: () => rotate,
  round: () => round3,
  scale: () => scale,
  scaleAndAdd: () => scaleAndAdd,
  set: () => set,
  sqrDist: () => sqrDist,
  sqrLen: () => sqrLen,
  squaredDistance: () => squaredDistance,
  squaredLength: () => squaredLength,
  str: () => str,
  sub: () => sub,
  subtract: () => subtract,
  transformMat2: () => transformMat2,
  transformMat2d: () => transformMat2d,
  transformMat3: () => transformMat3,
  transformMat4: () => transformMat4,
  zero: () => zero
});

// dist/gl-matrix/common.js
var EPSILON = 1e-6;
var ARRAY_TYPE = typeof Float32Array !== "undefined" ? Float32Array : Array;
var RANDOM = Math.random;
function round2(a) {
  if (a >= 0)
    return Math.round(a);
  return a % 0.5 === 0 ? Math.floor(a) : Math.round(a);
}
var degree = Math.PI / 180;

// dist/gl-matrix/vec2.js
function create() {
  const out = new ARRAY_TYPE(2);
  if (ARRAY_TYPE != Float32Array) {
    out[0] = 0;
    out[1] = 0;
  }
  return out;
}
function clone2(a) {
  const out = new ARRAY_TYPE(2);
  out[0] = a[0];
  out[1] = a[1];
  return out;
}
function fromValues(x, y) {
  const out = new ARRAY_TYPE(2);
  out[0] = x;
  out[1] = y;
  return out;
}
function copy(out, a) {
  out[0] = a[0];
  out[1] = a[1];
  return out;
}
function set(out, x, y) {
  out[0] = x;
  out[1] = y;
  return out;
}
function add(out, a, b) {
  out[0] = a[0] + b[0];
  out[1] = a[1] + b[1];
  return out;
}
function subtract(out, a, b) {
  out[0] = a[0] - b[0];
  out[1] = a[1] - b[1];
  return out;
}
function multiply(out, a, b) {
  out[0] = a[0] * b[0];
  out[1] = a[1] * b[1];
  return out;
}
function divide(out, a, b) {
  out[0] = a[0] / b[0];
  out[1] = a[1] / b[1];
  return out;
}
function ceil(out, a) {
  out[0] = Math.ceil(a[0]);
  out[1] = Math.ceil(a[1]);
  return out;
}
function floor(out, a) {
  out[0] = Math.floor(a[0]);
  out[1] = Math.floor(a[1]);
  return out;
}
function min(out, a, b) {
  out[0] = Math.min(a[0], b[0]);
  out[1] = Math.min(a[1], b[1]);
  return out;
}
function max(out, a, b) {
  out[0] = Math.max(a[0], b[0]);
  out[1] = Math.max(a[1], b[1]);
  return out;
}
function round3(out, a) {
  out[0] = round2(a[0]);
  out[1] = round2(a[1]);
  return out;
}
function scale(out, a, b) {
  out[0] = a[0] * b;
  out[1] = a[1] * b;
  return out;
}
function scaleAndAdd(out, a, b, scale7) {
  out[0] = a[0] + b[0] * scale7;
  out[1] = a[1] + b[1] * scale7;
  return out;
}
function distance(a, b) {
  const x = b[0] - a[0];
  const y = b[1] - a[1];
  return Math.sqrt(x * x + y * y);
}
function squaredDistance(a, b) {
  const x = b[0] - a[0];
  const y = b[1] - a[1];
  return x * x + y * y;
}
function length(a) {
  const x = a[0];
  const y = a[1];
  return Math.sqrt(x * x + y * y);
}
function squaredLength(a) {
  const x = a[0];
  const y = a[1];
  return x * x + y * y;
}
function negate(out, a) {
  out[0] = -a[0];
  out[1] = -a[1];
  return out;
}
function inverse(out, a) {
  out[0] = 1 / a[0];
  out[1] = 1 / a[1];
  return out;
}
function normalize(out, a) {
  const x = a[0];
  const y = a[1];
  let len5 = x * x + y * y;
  if (len5 > 0) {
    len5 = 1 / Math.sqrt(len5);
  }
  out[0] = a[0] * len5;
  out[1] = a[1] * len5;
  return out;
}
function dot(a, b) {
  return a[0] * b[0] + a[1] * b[1];
}
function cross(out, a, b) {
  const z = a[0] * b[1] - a[1] * b[0];
  out[0] = out[1] = 0;
  out[2] = z;
  return out;
}
function lerp2(out, a, b, t) {
  const ax = a[0];
  const ay = a[1];
  out[0] = ax + t * (b[0] - ax);
  out[1] = ay + t * (b[1] - ay);
  return out;
}
function random(out, scale7) {
  scale7 = scale7 === void 0 ? 1 : scale7;
  const r = RANDOM() * 2 * Math.PI;
  out[0] = Math.cos(r) * scale7;
  out[1] = Math.sin(r) * scale7;
  return out;
}
function transformMat2(out, a, m) {
  const x = a[0];
  const y = a[1];
  out[0] = m[0] * x + m[2] * y;
  out[1] = m[1] * x + m[3] * y;
  return out;
}
function transformMat2d(out, a, m) {
  const x = a[0];
  const y = a[1];
  out[0] = m[0] * x + m[2] * y + m[4];
  out[1] = m[1] * x + m[3] * y + m[5];
  return out;
}
function transformMat3(out, a, m) {
  const x = a[0];
  const y = a[1];
  out[0] = m[0] * x + m[3] * y + m[6];
  out[1] = m[1] * x + m[4] * y + m[7];
  return out;
}
function transformMat4(out, a, m) {
  const x = a[0];
  const y = a[1];
  out[0] = m[0] * x + m[4] * y + m[12];
  out[1] = m[1] * x + m[5] * y + m[13];
  return out;
}
function rotate(out, a, b, rad) {
  const p0 = a[0] - b[0];
  const p1 = a[1] - b[1];
  const sinC = Math.sin(rad);
  const cosC = Math.cos(rad);
  out[0] = p0 * cosC - p1 * sinC + b[0];
  out[1] = p0 * sinC + p1 * cosC + b[1];
  return out;
}
function angle(a, b) {
  const x1 = a[0];
  const y1 = a[1];
  const x2 = b[0];
  const y2 = b[1];
  const mag = Math.sqrt((x1 * x1 + y1 * y1) * (x2 * x2 + y2 * y2));
  const cosine = mag && (x1 * x2 + y1 * y2) / mag;
  return Math.acos(Math.min(Math.max(cosine, -1), 1));
}
function zero(out) {
  out[0] = 0;
  out[1] = 0;
  return out;
}
function str(a) {
  return `vec2(${a[0]}, ${a[1]})`;
}
function exactEquals2(a, b) {
  return a[0] === b[0] && a[1] === b[1];
}
function equals2(a, b) {
  const a0 = a[0];
  const a1 = a[1];
  const b0 = b[0];
  const b1 = b[1];
  return Math.abs(a0 - b0) <= EPSILON * Math.max(1, Math.abs(a0), Math.abs(b0)) && Math.abs(a1 - b1) <= EPSILON * Math.max(1, Math.abs(a1), Math.abs(b1));
}
var len = length;
var sub = subtract;
var mul = multiply;
var div = divide;
var dist = distance;
var sqrDist = squaredDistance;
var sqrLen = squaredLength;
var forEach = function() {
  const vec = create();
  return function(a, stride, offset, count, fn, arg) {
    let i;
    let l;
    if (!stride) {
      stride = 2;
    }
    if (!offset) {
      offset = 0;
    }
    if (count) {
      l = Math.min(count * stride + offset, a.length);
    } else {
      l = a.length;
    }
    for (i = offset; i < l; i += stride) {
      vec[0] = a[i];
      vec[1] = a[i + 1];
      fn(vec, vec, arg);
      a[i] = vec[0];
      a[i + 1] = vec[1];
    }
    return a;
  };
}();

// dist/lib/gl-matrix-extras.js
function vec2_transformMat4AsVector(out, a, m) {
  const x = a[0];
  const y = a[1];
  const w = m[3] * x + m[7] * y || 1;
  out[0] = (m[0] * x + m[4] * y) / w;
  out[1] = (m[1] * x + m[5] * y) / w;
  return out;
}
function vec3_transformMat4AsVector(out, a, m) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  const w = m[3] * x + m[7] * y + m[11] * z || 1;
  out[0] = (m[0] * x + m[4] * y + m[8] * z) / w;
  out[1] = (m[1] * x + m[5] * y + m[9] * z) / w;
  out[2] = (m[2] * x + m[6] * y + m[10] * z) / w;
  return out;
}
function vec3_transformMat2(out, a, m) {
  const x = a[0];
  const y = a[1];
  out[0] = m[0] * x + m[2] * y;
  out[1] = m[1] * x + m[3] * y;
  out[2] = a[2];
  return out;
}
function vec4_transformMat2(out, a, m) {
  const x = a[0];
  const y = a[1];
  out[0] = m[0] * x + m[2] * y;
  out[1] = m[1] * x + m[3] * y;
  out[2] = a[2];
  out[3] = a[3];
  return out;
}
function vec4_transformMat3(out, a, m) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  out[0] = m[0] * x + m[3] * y + m[6] * z;
  out[1] = m[1] * x + m[4] * y + m[7] * z;
  out[2] = m[2] * x + m[5] * y + m[8] * z;
  out[3] = a[3];
  return out;
}

// dist/classes/vector2.js
var Vector2 = class extends Vector {
  // Creates a new, empty vec2
  constructor(x = 0, y = 0) {
    super(2);
    if (isArray(x) && arguments.length === 1) {
      this.copy(x);
    } else {
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
    transformMat4(this, this, matrix4);
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
    transformMat3(this, this, matrix3);
    return this.check();
  }
  transformByMatrix2x3(matrix2x3) {
    transformMat2d(this, this, matrix2x3);
    return this.check();
  }
  transformByMatrix2(matrix2) {
    transformMat2(this, this, matrix2);
    return this.check();
  }
};

// dist/gl-matrix/vec3.js
var vec3_exports = {};
__export(vec3_exports, {
  add: () => add2,
  angle: () => angle2,
  bezier: () => bezier,
  ceil: () => ceil2,
  clone: () => clone3,
  copy: () => copy2,
  create: () => create2,
  cross: () => cross2,
  dist: () => dist2,
  distance: () => distance2,
  div: () => div2,
  divide: () => divide2,
  dot: () => dot2,
  equals: () => equals3,
  exactEquals: () => exactEquals3,
  floor: () => floor2,
  forEach: () => forEach2,
  fromValues: () => fromValues2,
  hermite: () => hermite,
  inverse: () => inverse2,
  len: () => len2,
  length: () => length2,
  lerp: () => lerp3,
  max: () => max2,
  min: () => min2,
  mul: () => mul2,
  multiply: () => multiply2,
  negate: () => negate2,
  normalize: () => normalize2,
  random: () => random2,
  rotateX: () => rotateX,
  rotateY: () => rotateY,
  rotateZ: () => rotateZ,
  round: () => round4,
  scale: () => scale2,
  scaleAndAdd: () => scaleAndAdd2,
  set: () => set2,
  slerp: () => slerp,
  sqrDist: () => sqrDist2,
  sqrLen: () => sqrLen2,
  squaredDistance: () => squaredDistance2,
  squaredLength: () => squaredLength2,
  str: () => str2,
  sub: () => sub2,
  subtract: () => subtract2,
  transformMat3: () => transformMat32,
  transformMat4: () => transformMat42,
  transformQuat: () => transformQuat,
  zero: () => zero2
});
function create2() {
  const out = new ARRAY_TYPE(3);
  if (ARRAY_TYPE != Float32Array) {
    out[0] = 0;
    out[1] = 0;
    out[2] = 0;
  }
  return out;
}
function clone3(a) {
  const out = new ARRAY_TYPE(3);
  out[0] = a[0];
  out[1] = a[1];
  out[2] = a[2];
  return out;
}
function length2(a) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  return Math.sqrt(x * x + y * y + z * z);
}
function fromValues2(x, y, z) {
  const out = new ARRAY_TYPE(3);
  out[0] = x;
  out[1] = y;
  out[2] = z;
  return out;
}
function copy2(out, a) {
  out[0] = a[0];
  out[1] = a[1];
  out[2] = a[2];
  return out;
}
function set2(out, x, y, z) {
  out[0] = x;
  out[1] = y;
  out[2] = z;
  return out;
}
function add2(out, a, b) {
  out[0] = a[0] + b[0];
  out[1] = a[1] + b[1];
  out[2] = a[2] + b[2];
  return out;
}
function subtract2(out, a, b) {
  out[0] = a[0] - b[0];
  out[1] = a[1] - b[1];
  out[2] = a[2] - b[2];
  return out;
}
function multiply2(out, a, b) {
  out[0] = a[0] * b[0];
  out[1] = a[1] * b[1];
  out[2] = a[2] * b[2];
  return out;
}
function divide2(out, a, b) {
  out[0] = a[0] / b[0];
  out[1] = a[1] / b[1];
  out[2] = a[2] / b[2];
  return out;
}
function ceil2(out, a) {
  out[0] = Math.ceil(a[0]);
  out[1] = Math.ceil(a[1]);
  out[2] = Math.ceil(a[2]);
  return out;
}
function floor2(out, a) {
  out[0] = Math.floor(a[0]);
  out[1] = Math.floor(a[1]);
  out[2] = Math.floor(a[2]);
  return out;
}
function min2(out, a, b) {
  out[0] = Math.min(a[0], b[0]);
  out[1] = Math.min(a[1], b[1]);
  out[2] = Math.min(a[2], b[2]);
  return out;
}
function max2(out, a, b) {
  out[0] = Math.max(a[0], b[0]);
  out[1] = Math.max(a[1], b[1]);
  out[2] = Math.max(a[2], b[2]);
  return out;
}
function round4(out, a) {
  out[0] = round2(a[0]);
  out[1] = round2(a[1]);
  out[2] = round2(a[2]);
  return out;
}
function scale2(out, a, b) {
  out[0] = a[0] * b;
  out[1] = a[1] * b;
  out[2] = a[2] * b;
  return out;
}
function scaleAndAdd2(out, a, b, scale7) {
  out[0] = a[0] + b[0] * scale7;
  out[1] = a[1] + b[1] * scale7;
  out[2] = a[2] + b[2] * scale7;
  return out;
}
function distance2(a, b) {
  const x = b[0] - a[0];
  const y = b[1] - a[1];
  const z = b[2] - a[2];
  return Math.sqrt(x * x + y * y + z * z);
}
function squaredDistance2(a, b) {
  const x = b[0] - a[0];
  const y = b[1] - a[1];
  const z = b[2] - a[2];
  return x * x + y * y + z * z;
}
function squaredLength2(a) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  return x * x + y * y + z * z;
}
function negate2(out, a) {
  out[0] = -a[0];
  out[1] = -a[1];
  out[2] = -a[2];
  return out;
}
function inverse2(out, a) {
  out[0] = 1 / a[0];
  out[1] = 1 / a[1];
  out[2] = 1 / a[2];
  return out;
}
function normalize2(out, a) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  let len5 = x * x + y * y + z * z;
  if (len5 > 0) {
    len5 = 1 / Math.sqrt(len5);
  }
  out[0] = a[0] * len5;
  out[1] = a[1] * len5;
  out[2] = a[2] * len5;
  return out;
}
function dot2(a, b) {
  return a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
}
function cross2(out, a, b) {
  const ax = a[0];
  const ay = a[1];
  const az = a[2];
  const bx = b[0];
  const by = b[1];
  const bz = b[2];
  out[0] = ay * bz - az * by;
  out[1] = az * bx - ax * bz;
  out[2] = ax * by - ay * bx;
  return out;
}
function lerp3(out, a, b, t) {
  const ax = a[0];
  const ay = a[1];
  const az = a[2];
  out[0] = ax + t * (b[0] - ax);
  out[1] = ay + t * (b[1] - ay);
  out[2] = az + t * (b[2] - az);
  return out;
}
function slerp(out, a, b, t) {
  const angle3 = Math.acos(Math.min(Math.max(dot2(a, b), -1), 1));
  const sinTotal = Math.sin(angle3);
  const ratioA = Math.sin((1 - t) * angle3) / sinTotal;
  const ratioB = Math.sin(t * angle3) / sinTotal;
  out[0] = ratioA * a[0] + ratioB * b[0];
  out[1] = ratioA * a[1] + ratioB * b[1];
  out[2] = ratioA * a[2] + ratioB * b[2];
  return out;
}
function hermite(out, a, b, c, d, t) {
  const factorTimes2 = t * t;
  const factor1 = factorTimes2 * (2 * t - 3) + 1;
  const factor2 = factorTimes2 * (t - 2) + t;
  const factor3 = factorTimes2 * (t - 1);
  const factor4 = factorTimes2 * (3 - 2 * t);
  out[0] = a[0] * factor1 + b[0] * factor2 + c[0] * factor3 + d[0] * factor4;
  out[1] = a[1] * factor1 + b[1] * factor2 + c[1] * factor3 + d[1] * factor4;
  out[2] = a[2] * factor1 + b[2] * factor2 + c[2] * factor3 + d[2] * factor4;
  return out;
}
function bezier(out, a, b, c, d, t) {
  const inverseFactor = 1 - t;
  const inverseFactorTimesTwo = inverseFactor * inverseFactor;
  const factorTimes2 = t * t;
  const factor1 = inverseFactorTimesTwo * inverseFactor;
  const factor2 = 3 * t * inverseFactorTimesTwo;
  const factor3 = 3 * factorTimes2 * inverseFactor;
  const factor4 = factorTimes2 * t;
  out[0] = a[0] * factor1 + b[0] * factor2 + c[0] * factor3 + d[0] * factor4;
  out[1] = a[1] * factor1 + b[1] * factor2 + c[1] * factor3 + d[1] * factor4;
  out[2] = a[2] * factor1 + b[2] * factor2 + c[2] * factor3 + d[2] * factor4;
  return out;
}
function random2(out, scale7) {
  scale7 = scale7 === void 0 ? 1 : scale7;
  const r = RANDOM() * 2 * Math.PI;
  const z = RANDOM() * 2 - 1;
  const zScale = Math.sqrt(1 - z * z) * scale7;
  out[0] = Math.cos(r) * zScale;
  out[1] = Math.sin(r) * zScale;
  out[2] = z * scale7;
  return out;
}
function transformMat42(out, a, m) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  let w = m[3] * x + m[7] * y + m[11] * z + m[15];
  w = w || 1;
  out[0] = (m[0] * x + m[4] * y + m[8] * z + m[12]) / w;
  out[1] = (m[1] * x + m[5] * y + m[9] * z + m[13]) / w;
  out[2] = (m[2] * x + m[6] * y + m[10] * z + m[14]) / w;
  return out;
}
function transformMat32(out, a, m) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  out[0] = x * m[0] + y * m[3] + z * m[6];
  out[1] = x * m[1] + y * m[4] + z * m[7];
  out[2] = x * m[2] + y * m[5] + z * m[8];
  return out;
}
function transformQuat(out, a, q) {
  const qx = q[0];
  const qy = q[1];
  const qz = q[2];
  const qw = q[3];
  const x = a[0];
  const y = a[1];
  const z = a[2];
  let uvx = qy * z - qz * y;
  let uvy = qz * x - qx * z;
  let uvz = qx * y - qy * x;
  let uuvx = qy * uvz - qz * uvy;
  let uuvy = qz * uvx - qx * uvz;
  let uuvz = qx * uvy - qy * uvx;
  const w2 = qw * 2;
  uvx *= w2;
  uvy *= w2;
  uvz *= w2;
  uuvx *= 2;
  uuvy *= 2;
  uuvz *= 2;
  out[0] = x + uvx + uuvx;
  out[1] = y + uvy + uuvy;
  out[2] = z + uvz + uuvz;
  return out;
}
function rotateX(out, a, b, rad) {
  const p = [];
  const r = [];
  p[0] = a[0] - b[0];
  p[1] = a[1] - b[1];
  p[2] = a[2] - b[2];
  r[0] = p[0];
  r[1] = p[1] * Math.cos(rad) - p[2] * Math.sin(rad);
  r[2] = p[1] * Math.sin(rad) + p[2] * Math.cos(rad);
  out[0] = r[0] + b[0];
  out[1] = r[1] + b[1];
  out[2] = r[2] + b[2];
  return out;
}
function rotateY(out, a, b, rad) {
  const p = [];
  const r = [];
  p[0] = a[0] - b[0];
  p[1] = a[1] - b[1];
  p[2] = a[2] - b[2];
  r[0] = p[2] * Math.sin(rad) + p[0] * Math.cos(rad);
  r[1] = p[1];
  r[2] = p[2] * Math.cos(rad) - p[0] * Math.sin(rad);
  out[0] = r[0] + b[0];
  out[1] = r[1] + b[1];
  out[2] = r[2] + b[2];
  return out;
}
function rotateZ(out, a, b, rad) {
  const p = [];
  const r = [];
  p[0] = a[0] - b[0];
  p[1] = a[1] - b[1];
  p[2] = a[2] - b[2];
  r[0] = p[0] * Math.cos(rad) - p[1] * Math.sin(rad);
  r[1] = p[0] * Math.sin(rad) + p[1] * Math.cos(rad);
  r[2] = p[2];
  out[0] = r[0] + b[0];
  out[1] = r[1] + b[1];
  out[2] = r[2] + b[2];
  return out;
}
function angle2(a, b) {
  const ax = a[0];
  const ay = a[1];
  const az = a[2];
  const bx = b[0];
  const by = b[1];
  const bz = b[2];
  const mag = Math.sqrt((ax * ax + ay * ay + az * az) * (bx * bx + by * by + bz * bz));
  const cosine = mag && dot2(a, b) / mag;
  return Math.acos(Math.min(Math.max(cosine, -1), 1));
}
function zero2(out) {
  out[0] = 0;
  out[1] = 0;
  out[2] = 0;
  return out;
}
function str2(a) {
  return `vec3(${a[0]}, ${a[1]}, ${a[2]})`;
}
function exactEquals3(a, b) {
  return a[0] === b[0] && a[1] === b[1] && a[2] === b[2];
}
function equals3(a, b) {
  const a0 = a[0];
  const a1 = a[1];
  const a2 = a[2];
  const b0 = b[0];
  const b1 = b[1];
  const b2 = b[2];
  return Math.abs(a0 - b0) <= EPSILON * Math.max(1, Math.abs(a0), Math.abs(b0)) && Math.abs(a1 - b1) <= EPSILON * Math.max(1, Math.abs(a1), Math.abs(b1)) && Math.abs(a2 - b2) <= EPSILON * Math.max(1, Math.abs(a2), Math.abs(b2));
}
var sub2 = subtract2;
var mul2 = multiply2;
var div2 = divide2;
var dist2 = distance2;
var sqrDist2 = squaredDistance2;
var len2 = length2;
var sqrLen2 = squaredLength2;
var forEach2 = function() {
  const vec = create2();
  return function(a, stride, offset, count, fn, arg) {
    let i;
    let l;
    if (!stride) {
      stride = 3;
    }
    if (!offset) {
      offset = 0;
    }
    if (count) {
      l = Math.min(count * stride + offset, a.length);
    } else {
      l = a.length;
    }
    for (i = offset; i < l; i += stride) {
      vec[0] = a[i];
      vec[1] = a[i + 1];
      vec[2] = a[i + 2];
      fn(vec, vec, arg);
      a[i] = vec[0];
      a[i + 1] = vec[1];
      a[i + 2] = vec[2];
    }
    return a;
  };
}();

// dist/classes/vector3.js
var ORIGIN = [0, 0, 0];
var ZERO;
var Vector3 = class extends Vector {
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
    super(-0, -0, -0);
    if (arguments.length === 1 && isArray(x)) {
      this.copy(x);
    } else {
      if (config.debug) {
        checkNumber(x);
        checkNumber(y);
        checkNumber(z);
      }
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
    return angle2(this, vector);
  }
  // MODIFIERS
  cross(vector) {
    cross2(this, this, vector);
    return this.check();
  }
  rotateX({ radians: radians2, origin = ORIGIN }) {
    rotateX(this, this, origin, radians2);
    return this.check();
  }
  rotateY({ radians: radians2, origin = ORIGIN }) {
    rotateY(this, this, origin, radians2);
    return this.check();
  }
  rotateZ({ radians: radians2, origin = ORIGIN }) {
    rotateZ(this, this, origin, radians2);
    return this.check();
  }
  // Transforms
  // transforms as point (4th component is implicitly 1)
  transform(matrix4) {
    return this.transformAsPoint(matrix4);
  }
  // transforms as point (4th component is implicitly 1)
  transformAsPoint(matrix4) {
    transformMat42(this, this, matrix4);
    return this.check();
  }
  // transforms as vector  (4th component is implicitly 0, ignores translation. slightly faster)
  transformAsVector(matrix4) {
    vec3_transformMat4AsVector(this, this, matrix4);
    return this.check();
  }
  transformByMatrix3(matrix3) {
    transformMat32(this, this, matrix3);
    return this.check();
  }
  transformByMatrix2(matrix2) {
    vec3_transformMat2(this, this, matrix2);
    return this.check();
  }
  transformByQuaternion(quaternion) {
    transformQuat(this, this, quaternion);
    return this.check();
  }
};

// dist/classes/vector4.js
var ZERO2;
var Vector4 = class extends Vector {
  static get ZERO() {
    if (!ZERO2) {
      ZERO2 = new Vector4(0, 0, 0, 0);
      Object.freeze(ZERO2);
    }
    return ZERO2;
  }
  constructor(x = 0, y = 0, z = 0, w = 0) {
    super(-0, -0, -0, -0);
    if (isArray(x) && arguments.length === 1) {
      this.copy(x);
    } else {
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
    transformMat42(this, this, matrix4);
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
    transformQuat(this, this, quaternion);
    return this.check();
  }
  // three.js compatibility
  applyMatrix4(m) {
    m.transform(this, this);
    return this;
  }
};

// dist/classes/base/matrix.js
var Matrix = class extends MathArray {
  // fromObject(object) {
  //   const array = object.elements;
  //   return this.fromRowMajor(array);
  // }
  // toObject(object) {
  //   const array = object.elements;
  //   this.toRowMajor(array);
  //   return object;
  // }
  // TODO better override formatString?
  toString() {
    let string = "[";
    if (config.printRowMajor) {
      string += "row-major:";
      for (let row = 0; row < this.RANK; ++row) {
        for (let col = 0; col < this.RANK; ++col) {
          string += ` ${this[col * this.RANK + row]}`;
        }
      }
    } else {
      string += "column-major:";
      for (let i = 0; i < this.ELEMENTS; ++i) {
        string += ` ${this[i]}`;
      }
    }
    string += "]";
    return string;
  }
  getElementIndex(row, col) {
    return col * this.RANK + row;
  }
  // By default assumes row major indices
  getElement(row, col) {
    return this[col * this.RANK + row];
  }
  // By default assumes row major indices
  setElement(row, col, value) {
    this[col * this.RANK + row] = checkNumber(value);
    return this;
  }
  getColumn(columnIndex, result = new Array(this.RANK).fill(-0)) {
    const firstIndex = columnIndex * this.RANK;
    for (let i = 0; i < this.RANK; ++i) {
      result[i] = this[firstIndex + i];
    }
    return result;
  }
  setColumn(columnIndex, columnVector) {
    const firstIndex = columnIndex * this.RANK;
    for (let i = 0; i < this.RANK; ++i) {
      this[firstIndex + i] = columnVector[i];
    }
    return this;
  }
};

// dist/gl-matrix/mat3.js
var mat3_exports = {};
__export(mat3_exports, {
  add: () => add3,
  adjoint: () => adjoint,
  clone: () => clone4,
  copy: () => copy3,
  create: () => create3,
  determinant: () => determinant,
  equals: () => equals4,
  exactEquals: () => exactEquals4,
  frob: () => frob,
  fromMat2d: () => fromMat2d,
  fromMat4: () => fromMat4,
  fromQuat: () => fromQuat,
  fromRotation: () => fromRotation,
  fromScaling: () => fromScaling,
  fromTranslation: () => fromTranslation,
  fromValues: () => fromValues3,
  identity: () => identity,
  invert: () => invert,
  mul: () => mul3,
  multiply: () => multiply3,
  multiplyScalar: () => multiplyScalar,
  multiplyScalarAndAdd: () => multiplyScalarAndAdd,
  normalFromMat4: () => normalFromMat4,
  projection: () => projection,
  rotate: () => rotate2,
  scale: () => scale3,
  set: () => set3,
  str: () => str3,
  sub: () => sub3,
  subtract: () => subtract3,
  translate: () => translate,
  transpose: () => transpose
});
function create3() {
  const out = new ARRAY_TYPE(9);
  if (ARRAY_TYPE != Float32Array) {
    out[1] = 0;
    out[2] = 0;
    out[3] = 0;
    out[5] = 0;
    out[6] = 0;
    out[7] = 0;
  }
  out[0] = 1;
  out[4] = 1;
  out[8] = 1;
  return out;
}
function fromMat4(out, a) {
  out[0] = a[0];
  out[1] = a[1];
  out[2] = a[2];
  out[3] = a[4];
  out[4] = a[5];
  out[5] = a[6];
  out[6] = a[8];
  out[7] = a[9];
  out[8] = a[10];
  return out;
}
function clone4(a) {
  const out = new ARRAY_TYPE(9);
  out[0] = a[0];
  out[1] = a[1];
  out[2] = a[2];
  out[3] = a[3];
  out[4] = a[4];
  out[5] = a[5];
  out[6] = a[6];
  out[7] = a[7];
  out[8] = a[8];
  return out;
}
function copy3(out, a) {
  out[0] = a[0];
  out[1] = a[1];
  out[2] = a[2];
  out[3] = a[3];
  out[4] = a[4];
  out[5] = a[5];
  out[6] = a[6];
  out[7] = a[7];
  out[8] = a[8];
  return out;
}
function fromValues3(m00, m01, m02, m10, m11, m12, m20, m21, m22) {
  const out = new ARRAY_TYPE(9);
  out[0] = m00;
  out[1] = m01;
  out[2] = m02;
  out[3] = m10;
  out[4] = m11;
  out[5] = m12;
  out[6] = m20;
  out[7] = m21;
  out[8] = m22;
  return out;
}
function set3(out, m00, m01, m02, m10, m11, m12, m20, m21, m22) {
  out[0] = m00;
  out[1] = m01;
  out[2] = m02;
  out[3] = m10;
  out[4] = m11;
  out[5] = m12;
  out[6] = m20;
  out[7] = m21;
  out[8] = m22;
  return out;
}
function identity(out) {
  out[0] = 1;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = 1;
  out[5] = 0;
  out[6] = 0;
  out[7] = 0;
  out[8] = 1;
  return out;
}
function transpose(out, a) {
  if (out === a) {
    const a01 = a[1];
    const a02 = a[2];
    const a12 = a[5];
    out[1] = a[3];
    out[2] = a[6];
    out[3] = a01;
    out[5] = a[7];
    out[6] = a02;
    out[7] = a12;
  } else {
    out[0] = a[0];
    out[1] = a[3];
    out[2] = a[6];
    out[3] = a[1];
    out[4] = a[4];
    out[5] = a[7];
    out[6] = a[2];
    out[7] = a[5];
    out[8] = a[8];
  }
  return out;
}
function invert(out, a) {
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a10 = a[3];
  const a11 = a[4];
  const a12 = a[5];
  const a20 = a[6];
  const a21 = a[7];
  const a22 = a[8];
  const b01 = a22 * a11 - a12 * a21;
  const b11 = -a22 * a10 + a12 * a20;
  const b21 = a21 * a10 - a11 * a20;
  let det = a00 * b01 + a01 * b11 + a02 * b21;
  if (!det) {
    return null;
  }
  det = 1 / det;
  out[0] = b01 * det;
  out[1] = (-a22 * a01 + a02 * a21) * det;
  out[2] = (a12 * a01 - a02 * a11) * det;
  out[3] = b11 * det;
  out[4] = (a22 * a00 - a02 * a20) * det;
  out[5] = (-a12 * a00 + a02 * a10) * det;
  out[6] = b21 * det;
  out[7] = (-a21 * a00 + a01 * a20) * det;
  out[8] = (a11 * a00 - a01 * a10) * det;
  return out;
}
function adjoint(out, a) {
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a10 = a[3];
  const a11 = a[4];
  const a12 = a[5];
  const a20 = a[6];
  const a21 = a[7];
  const a22 = a[8];
  out[0] = a11 * a22 - a12 * a21;
  out[1] = a02 * a21 - a01 * a22;
  out[2] = a01 * a12 - a02 * a11;
  out[3] = a12 * a20 - a10 * a22;
  out[4] = a00 * a22 - a02 * a20;
  out[5] = a02 * a10 - a00 * a12;
  out[6] = a10 * a21 - a11 * a20;
  out[7] = a01 * a20 - a00 * a21;
  out[8] = a00 * a11 - a01 * a10;
  return out;
}
function determinant(a) {
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a10 = a[3];
  const a11 = a[4];
  const a12 = a[5];
  const a20 = a[6];
  const a21 = a[7];
  const a22 = a[8];
  return a00 * (a22 * a11 - a12 * a21) + a01 * (-a22 * a10 + a12 * a20) + a02 * (a21 * a10 - a11 * a20);
}
function multiply3(out, a, b) {
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a10 = a[3];
  const a11 = a[4];
  const a12 = a[5];
  const a20 = a[6];
  const a21 = a[7];
  const a22 = a[8];
  const b00 = b[0];
  const b01 = b[1];
  const b02 = b[2];
  const b10 = b[3];
  const b11 = b[4];
  const b12 = b[5];
  const b20 = b[6];
  const b21 = b[7];
  const b22 = b[8];
  out[0] = b00 * a00 + b01 * a10 + b02 * a20;
  out[1] = b00 * a01 + b01 * a11 + b02 * a21;
  out[2] = b00 * a02 + b01 * a12 + b02 * a22;
  out[3] = b10 * a00 + b11 * a10 + b12 * a20;
  out[4] = b10 * a01 + b11 * a11 + b12 * a21;
  out[5] = b10 * a02 + b11 * a12 + b12 * a22;
  out[6] = b20 * a00 + b21 * a10 + b22 * a20;
  out[7] = b20 * a01 + b21 * a11 + b22 * a21;
  out[8] = b20 * a02 + b21 * a12 + b22 * a22;
  return out;
}
function translate(out, a, v) {
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a10 = a[3];
  const a11 = a[4];
  const a12 = a[5];
  const a20 = a[6];
  const a21 = a[7];
  const a22 = a[8];
  const x = v[0];
  const y = v[1];
  out[0] = a00;
  out[1] = a01;
  out[2] = a02;
  out[3] = a10;
  out[4] = a11;
  out[5] = a12;
  out[6] = x * a00 + y * a10 + a20;
  out[7] = x * a01 + y * a11 + a21;
  out[8] = x * a02 + y * a12 + a22;
  return out;
}
function rotate2(out, a, rad) {
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a10 = a[3];
  const a11 = a[4];
  const a12 = a[5];
  const a20 = a[6];
  const a21 = a[7];
  const a22 = a[8];
  const s = Math.sin(rad);
  const c = Math.cos(rad);
  out[0] = c * a00 + s * a10;
  out[1] = c * a01 + s * a11;
  out[2] = c * a02 + s * a12;
  out[3] = c * a10 - s * a00;
  out[4] = c * a11 - s * a01;
  out[5] = c * a12 - s * a02;
  out[6] = a20;
  out[7] = a21;
  out[8] = a22;
  return out;
}
function scale3(out, a, v) {
  const x = v[0];
  const y = v[1];
  out[0] = x * a[0];
  out[1] = x * a[1];
  out[2] = x * a[2];
  out[3] = y * a[3];
  out[4] = y * a[4];
  out[5] = y * a[5];
  out[6] = a[6];
  out[7] = a[7];
  out[8] = a[8];
  return out;
}
function fromTranslation(out, v) {
  out[0] = 1;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = 1;
  out[5] = 0;
  out[6] = v[0];
  out[7] = v[1];
  out[8] = 1;
  return out;
}
function fromRotation(out, rad) {
  const s = Math.sin(rad);
  const c = Math.cos(rad);
  out[0] = c;
  out[1] = s;
  out[2] = 0;
  out[3] = -s;
  out[4] = c;
  out[5] = 0;
  out[6] = 0;
  out[7] = 0;
  out[8] = 1;
  return out;
}
function fromScaling(out, v) {
  out[0] = v[0];
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = v[1];
  out[5] = 0;
  out[6] = 0;
  out[7] = 0;
  out[8] = 1;
  return out;
}
function fromMat2d(out, a) {
  out[0] = a[0];
  out[1] = a[1];
  out[2] = 0;
  out[3] = a[2];
  out[4] = a[3];
  out[5] = 0;
  out[6] = a[4];
  out[7] = a[5];
  out[8] = 1;
  return out;
}
function fromQuat(out, q) {
  const x = q[0];
  const y = q[1];
  const z = q[2];
  const w = q[3];
  const x2 = x + x;
  const y2 = y + y;
  const z2 = z + z;
  const xx = x * x2;
  const yx = y * x2;
  const yy = y * y2;
  const zx = z * x2;
  const zy = z * y2;
  const zz = z * z2;
  const wx = w * x2;
  const wy = w * y2;
  const wz = w * z2;
  out[0] = 1 - yy - zz;
  out[3] = yx - wz;
  out[6] = zx + wy;
  out[1] = yx + wz;
  out[4] = 1 - xx - zz;
  out[7] = zy - wx;
  out[2] = zx - wy;
  out[5] = zy + wx;
  out[8] = 1 - xx - yy;
  return out;
}
function normalFromMat4(out, a) {
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a03 = a[3];
  const a10 = a[4];
  const a11 = a[5];
  const a12 = a[6];
  const a13 = a[7];
  const a20 = a[8];
  const a21 = a[9];
  const a22 = a[10];
  const a23 = a[11];
  const a30 = a[12];
  const a31 = a[13];
  const a32 = a[14];
  const a33 = a[15];
  const b00 = a00 * a11 - a01 * a10;
  const b01 = a00 * a12 - a02 * a10;
  const b02 = a00 * a13 - a03 * a10;
  const b03 = a01 * a12 - a02 * a11;
  const b04 = a01 * a13 - a03 * a11;
  const b05 = a02 * a13 - a03 * a12;
  const b06 = a20 * a31 - a21 * a30;
  const b07 = a20 * a32 - a22 * a30;
  const b08 = a20 * a33 - a23 * a30;
  const b09 = a21 * a32 - a22 * a31;
  const b10 = a21 * a33 - a23 * a31;
  const b11 = a22 * a33 - a23 * a32;
  let det = b00 * b11 - b01 * b10 + b02 * b09 + b03 * b08 - b04 * b07 + b05 * b06;
  if (!det) {
    return null;
  }
  det = 1 / det;
  out[0] = (a11 * b11 - a12 * b10 + a13 * b09) * det;
  out[1] = (a12 * b08 - a10 * b11 - a13 * b07) * det;
  out[2] = (a10 * b10 - a11 * b08 + a13 * b06) * det;
  out[3] = (a02 * b10 - a01 * b11 - a03 * b09) * det;
  out[4] = (a00 * b11 - a02 * b08 + a03 * b07) * det;
  out[5] = (a01 * b08 - a00 * b10 - a03 * b06) * det;
  out[6] = (a31 * b05 - a32 * b04 + a33 * b03) * det;
  out[7] = (a32 * b02 - a30 * b05 - a33 * b01) * det;
  out[8] = (a30 * b04 - a31 * b02 + a33 * b00) * det;
  return out;
}
function projection(out, width, height) {
  out[0] = 2 / width;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = -2 / height;
  out[5] = 0;
  out[6] = -1;
  out[7] = 1;
  out[8] = 1;
  return out;
}
function str3(a) {
  return `mat3(${a[0]}, ${a[1]}, ${a[2]}, ${a[3]}, ${a[4]}, ${a[5]}, ${a[6]}, ${a[7]}, ${a[8]})`;
}
function frob(a) {
  return Math.sqrt(a[0] * a[0] + a[1] * a[1] + a[2] * a[2] + a[3] * a[3] + a[4] * a[4] + a[5] * a[5] + a[6] * a[6] + a[7] * a[7] + a[8] * a[8]);
}
function add3(out, a, b) {
  out[0] = a[0] + b[0];
  out[1] = a[1] + b[1];
  out[2] = a[2] + b[2];
  out[3] = a[3] + b[3];
  out[4] = a[4] + b[4];
  out[5] = a[5] + b[5];
  out[6] = a[6] + b[6];
  out[7] = a[7] + b[7];
  out[8] = a[8] + b[8];
  return out;
}
function subtract3(out, a, b) {
  out[0] = a[0] - b[0];
  out[1] = a[1] - b[1];
  out[2] = a[2] - b[2];
  out[3] = a[3] - b[3];
  out[4] = a[4] - b[4];
  out[5] = a[5] - b[5];
  out[6] = a[6] - b[6];
  out[7] = a[7] - b[7];
  out[8] = a[8] - b[8];
  return out;
}
function multiplyScalar(out, a, b) {
  out[0] = a[0] * b;
  out[1] = a[1] * b;
  out[2] = a[2] * b;
  out[3] = a[3] * b;
  out[4] = a[4] * b;
  out[5] = a[5] * b;
  out[6] = a[6] * b;
  out[7] = a[7] * b;
  out[8] = a[8] * b;
  return out;
}
function multiplyScalarAndAdd(out, a, b, scale7) {
  out[0] = a[0] + b[0] * scale7;
  out[1] = a[1] + b[1] * scale7;
  out[2] = a[2] + b[2] * scale7;
  out[3] = a[3] + b[3] * scale7;
  out[4] = a[4] + b[4] * scale7;
  out[5] = a[5] + b[5] * scale7;
  out[6] = a[6] + b[6] * scale7;
  out[7] = a[7] + b[7] * scale7;
  out[8] = a[8] + b[8] * scale7;
  return out;
}
function exactEquals4(a, b) {
  return a[0] === b[0] && a[1] === b[1] && a[2] === b[2] && a[3] === b[3] && a[4] === b[4] && a[5] === b[5] && a[6] === b[6] && a[7] === b[7] && a[8] === b[8];
}
function equals4(a, b) {
  const a0 = a[0];
  const a1 = a[1];
  const a2 = a[2];
  const a3 = a[3];
  const a4 = a[4];
  const a5 = a[5];
  const a6 = a[6];
  const a7 = a[7];
  const a8 = a[8];
  const b0 = b[0];
  const b1 = b[1];
  const b2 = b[2];
  const b3 = b[3];
  const b4 = b[4];
  const b5 = b[5];
  const b6 = b[6];
  const b7 = b[7];
  const b8 = b[8];
  return Math.abs(a0 - b0) <= EPSILON * Math.max(1, Math.abs(a0), Math.abs(b0)) && Math.abs(a1 - b1) <= EPSILON * Math.max(1, Math.abs(a1), Math.abs(b1)) && Math.abs(a2 - b2) <= EPSILON * Math.max(1, Math.abs(a2), Math.abs(b2)) && Math.abs(a3 - b3) <= EPSILON * Math.max(1, Math.abs(a3), Math.abs(b3)) && Math.abs(a4 - b4) <= EPSILON * Math.max(1, Math.abs(a4), Math.abs(b4)) && Math.abs(a5 - b5) <= EPSILON * Math.max(1, Math.abs(a5), Math.abs(b5)) && Math.abs(a6 - b6) <= EPSILON * Math.max(1, Math.abs(a6), Math.abs(b6)) && Math.abs(a7 - b7) <= EPSILON * Math.max(1, Math.abs(a7), Math.abs(b7)) && Math.abs(a8 - b8) <= EPSILON * Math.max(1, Math.abs(a8), Math.abs(b8));
}
var mul3 = multiply3;
var sub3 = subtract3;

// dist/classes/matrix3.js
var INDICES;
(function(INDICES3) {
  INDICES3[INDICES3["COL0ROW0"] = 0] = "COL0ROW0";
  INDICES3[INDICES3["COL0ROW1"] = 1] = "COL0ROW1";
  INDICES3[INDICES3["COL0ROW2"] = 2] = "COL0ROW2";
  INDICES3[INDICES3["COL1ROW0"] = 3] = "COL1ROW0";
  INDICES3[INDICES3["COL1ROW1"] = 4] = "COL1ROW1";
  INDICES3[INDICES3["COL1ROW2"] = 5] = "COL1ROW2";
  INDICES3[INDICES3["COL2ROW0"] = 6] = "COL2ROW0";
  INDICES3[INDICES3["COL2ROW1"] = 7] = "COL2ROW1";
  INDICES3[INDICES3["COL2ROW2"] = 8] = "COL2ROW2";
})(INDICES || (INDICES = {}));
var IDENTITY_MATRIX = Object.freeze([1, 0, 0, 0, 1, 0, 0, 0, 1]);
var Matrix3 = class extends Matrix {
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
    super(-0, -0, -0, -0, -0, -0, -0, -0, -0);
    if (arguments.length === 1 && Array.isArray(array)) {
      this.copy(array);
    } else if (args.length > 0) {
      this.copy([array, ...args]);
    } else {
      this.identity();
    }
  }
  copy(array) {
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
    fromQuat(this, q);
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
    return determinant(this);
  }
  // Modifiers
  transpose() {
    transpose(this, this);
    return this.check();
  }
  /** Invert a matrix. Note that this can fail if the matrix is not invertible */
  invert() {
    invert(this, this);
    return this.check();
  }
  // Operations
  multiplyLeft(a) {
    multiply3(this, a, this);
    return this.check();
  }
  multiplyRight(a) {
    multiply3(this, this, a);
    return this.check();
  }
  rotate(radians2) {
    rotate2(this, this, radians2);
    return this.check();
  }
  scale(factor) {
    if (Array.isArray(factor)) {
      scale3(this, this, factor);
    } else {
      scale3(this, this, [factor, factor]);
    }
    return this.check();
  }
  translate(vec) {
    translate(this, this, vec);
    return this.check();
  }
  // Transforms
  transform(vector, result) {
    let out;
    switch (vector.length) {
      case 2:
        out = transformMat3(result || [-0, -0], vector, this);
        break;
      case 3:
        out = transformMat32(result || [-0, -0, -0], vector, this);
        break;
      case 4:
        out = vec4_transformMat3(result || [-0, -0, -0, -0], vector, this);
        break;
      default:
        throw new Error("Illegal vector");
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
};
var ZERO_MATRIX3;
var IDENTITY_MATRIX3 = null;
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

// dist/gl-matrix/mat4.js
var mat4_exports = {};
__export(mat4_exports, {
  add: () => add4,
  adjoint: () => adjoint2,
  clone: () => clone5,
  copy: () => copy4,
  create: () => create4,
  decompose: () => decompose,
  determinant: () => determinant2,
  equals: () => equals5,
  exactEquals: () => exactEquals5,
  frob: () => frob2,
  fromQuat: () => fromQuat3,
  fromQuat2: () => fromQuat2,
  fromRotation: () => fromRotation2,
  fromRotationTranslation: () => fromRotationTranslation,
  fromRotationTranslationScale: () => fromRotationTranslationScale,
  fromRotationTranslationScaleOrigin: () => fromRotationTranslationScaleOrigin,
  fromScaling: () => fromScaling2,
  fromTranslation: () => fromTranslation2,
  fromValues: () => fromValues4,
  fromXRotation: () => fromXRotation,
  fromYRotation: () => fromYRotation,
  fromZRotation: () => fromZRotation,
  frustum: () => frustum,
  getRotation: () => getRotation,
  getScaling: () => getScaling,
  getTranslation: () => getTranslation,
  identity: () => identity2,
  invert: () => invert2,
  lookAt: () => lookAt,
  mul: () => mul4,
  multiply: () => multiply4,
  multiplyScalar: () => multiplyScalar2,
  multiplyScalarAndAdd: () => multiplyScalarAndAdd2,
  ortho: () => ortho,
  orthoNO: () => orthoNO,
  orthoZO: () => orthoZO,
  perspective: () => perspective,
  perspectiveFromFieldOfView: () => perspectiveFromFieldOfView,
  perspectiveNO: () => perspectiveNO,
  perspectiveZO: () => perspectiveZO,
  rotate: () => rotate3,
  rotateX: () => rotateX2,
  rotateY: () => rotateY2,
  rotateZ: () => rotateZ2,
  scale: () => scale4,
  set: () => set4,
  str: () => str4,
  sub: () => sub4,
  subtract: () => subtract4,
  targetTo: () => targetTo,
  translate: () => translate2,
  transpose: () => transpose2
});
function create4() {
  const out = new ARRAY_TYPE(16);
  if (ARRAY_TYPE != Float32Array) {
    out[1] = 0;
    out[2] = 0;
    out[3] = 0;
    out[4] = 0;
    out[6] = 0;
    out[7] = 0;
    out[8] = 0;
    out[9] = 0;
    out[11] = 0;
    out[12] = 0;
    out[13] = 0;
    out[14] = 0;
  }
  out[0] = 1;
  out[5] = 1;
  out[10] = 1;
  out[15] = 1;
  return out;
}
function clone5(a) {
  const out = new ARRAY_TYPE(16);
  out[0] = a[0];
  out[1] = a[1];
  out[2] = a[2];
  out[3] = a[3];
  out[4] = a[4];
  out[5] = a[5];
  out[6] = a[6];
  out[7] = a[7];
  out[8] = a[8];
  out[9] = a[9];
  out[10] = a[10];
  out[11] = a[11];
  out[12] = a[12];
  out[13] = a[13];
  out[14] = a[14];
  out[15] = a[15];
  return out;
}
function copy4(out, a) {
  out[0] = a[0];
  out[1] = a[1];
  out[2] = a[2];
  out[3] = a[3];
  out[4] = a[4];
  out[5] = a[5];
  out[6] = a[6];
  out[7] = a[7];
  out[8] = a[8];
  out[9] = a[9];
  out[10] = a[10];
  out[11] = a[11];
  out[12] = a[12];
  out[13] = a[13];
  out[14] = a[14];
  out[15] = a[15];
  return out;
}
function fromValues4(m00, m01, m02, m03, m10, m11, m12, m13, m20, m21, m22, m23, m30, m31, m32, m33) {
  const out = new ARRAY_TYPE(16);
  out[0] = m00;
  out[1] = m01;
  out[2] = m02;
  out[3] = m03;
  out[4] = m10;
  out[5] = m11;
  out[6] = m12;
  out[7] = m13;
  out[8] = m20;
  out[9] = m21;
  out[10] = m22;
  out[11] = m23;
  out[12] = m30;
  out[13] = m31;
  out[14] = m32;
  out[15] = m33;
  return out;
}
function set4(out, m00, m01, m02, m03, m10, m11, m12, m13, m20, m21, m22, m23, m30, m31, m32, m33) {
  out[0] = m00;
  out[1] = m01;
  out[2] = m02;
  out[3] = m03;
  out[4] = m10;
  out[5] = m11;
  out[6] = m12;
  out[7] = m13;
  out[8] = m20;
  out[9] = m21;
  out[10] = m22;
  out[11] = m23;
  out[12] = m30;
  out[13] = m31;
  out[14] = m32;
  out[15] = m33;
  return out;
}
function identity2(out) {
  out[0] = 1;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = 0;
  out[5] = 1;
  out[6] = 0;
  out[7] = 0;
  out[8] = 0;
  out[9] = 0;
  out[10] = 1;
  out[11] = 0;
  out[12] = 0;
  out[13] = 0;
  out[14] = 0;
  out[15] = 1;
  return out;
}
function transpose2(out, a) {
  if (out === a) {
    const a01 = a[1];
    const a02 = a[2];
    const a03 = a[3];
    const a12 = a[6];
    const a13 = a[7];
    const a23 = a[11];
    out[1] = a[4];
    out[2] = a[8];
    out[3] = a[12];
    out[4] = a01;
    out[6] = a[9];
    out[7] = a[13];
    out[8] = a02;
    out[9] = a12;
    out[11] = a[14];
    out[12] = a03;
    out[13] = a13;
    out[14] = a23;
  } else {
    out[0] = a[0];
    out[1] = a[4];
    out[2] = a[8];
    out[3] = a[12];
    out[4] = a[1];
    out[5] = a[5];
    out[6] = a[9];
    out[7] = a[13];
    out[8] = a[2];
    out[9] = a[6];
    out[10] = a[10];
    out[11] = a[14];
    out[12] = a[3];
    out[13] = a[7];
    out[14] = a[11];
    out[15] = a[15];
  }
  return out;
}
function invert2(out, a) {
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a03 = a[3];
  const a10 = a[4];
  const a11 = a[5];
  const a12 = a[6];
  const a13 = a[7];
  const a20 = a[8];
  const a21 = a[9];
  const a22 = a[10];
  const a23 = a[11];
  const a30 = a[12];
  const a31 = a[13];
  const a32 = a[14];
  const a33 = a[15];
  const b00 = a00 * a11 - a01 * a10;
  const b01 = a00 * a12 - a02 * a10;
  const b02 = a00 * a13 - a03 * a10;
  const b03 = a01 * a12 - a02 * a11;
  const b04 = a01 * a13 - a03 * a11;
  const b05 = a02 * a13 - a03 * a12;
  const b06 = a20 * a31 - a21 * a30;
  const b07 = a20 * a32 - a22 * a30;
  const b08 = a20 * a33 - a23 * a30;
  const b09 = a21 * a32 - a22 * a31;
  const b10 = a21 * a33 - a23 * a31;
  const b11 = a22 * a33 - a23 * a32;
  let det = b00 * b11 - b01 * b10 + b02 * b09 + b03 * b08 - b04 * b07 + b05 * b06;
  if (!det) {
    return null;
  }
  det = 1 / det;
  out[0] = (a11 * b11 - a12 * b10 + a13 * b09) * det;
  out[1] = (a02 * b10 - a01 * b11 - a03 * b09) * det;
  out[2] = (a31 * b05 - a32 * b04 + a33 * b03) * det;
  out[3] = (a22 * b04 - a21 * b05 - a23 * b03) * det;
  out[4] = (a12 * b08 - a10 * b11 - a13 * b07) * det;
  out[5] = (a00 * b11 - a02 * b08 + a03 * b07) * det;
  out[6] = (a32 * b02 - a30 * b05 - a33 * b01) * det;
  out[7] = (a20 * b05 - a22 * b02 + a23 * b01) * det;
  out[8] = (a10 * b10 - a11 * b08 + a13 * b06) * det;
  out[9] = (a01 * b08 - a00 * b10 - a03 * b06) * det;
  out[10] = (a30 * b04 - a31 * b02 + a33 * b00) * det;
  out[11] = (a21 * b02 - a20 * b04 - a23 * b00) * det;
  out[12] = (a11 * b07 - a10 * b09 - a12 * b06) * det;
  out[13] = (a00 * b09 - a01 * b07 + a02 * b06) * det;
  out[14] = (a31 * b01 - a30 * b03 - a32 * b00) * det;
  out[15] = (a20 * b03 - a21 * b01 + a22 * b00) * det;
  return out;
}
function adjoint2(out, a) {
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a03 = a[3];
  const a10 = a[4];
  const a11 = a[5];
  const a12 = a[6];
  const a13 = a[7];
  const a20 = a[8];
  const a21 = a[9];
  const a22 = a[10];
  const a23 = a[11];
  const a30 = a[12];
  const a31 = a[13];
  const a32 = a[14];
  const a33 = a[15];
  const b00 = a00 * a11 - a01 * a10;
  const b01 = a00 * a12 - a02 * a10;
  const b02 = a00 * a13 - a03 * a10;
  const b03 = a01 * a12 - a02 * a11;
  const b04 = a01 * a13 - a03 * a11;
  const b05 = a02 * a13 - a03 * a12;
  const b06 = a20 * a31 - a21 * a30;
  const b07 = a20 * a32 - a22 * a30;
  const b08 = a20 * a33 - a23 * a30;
  const b09 = a21 * a32 - a22 * a31;
  const b10 = a21 * a33 - a23 * a31;
  const b11 = a22 * a33 - a23 * a32;
  out[0] = a11 * b11 - a12 * b10 + a13 * b09;
  out[1] = a02 * b10 - a01 * b11 - a03 * b09;
  out[2] = a31 * b05 - a32 * b04 + a33 * b03;
  out[3] = a22 * b04 - a21 * b05 - a23 * b03;
  out[4] = a12 * b08 - a10 * b11 - a13 * b07;
  out[5] = a00 * b11 - a02 * b08 + a03 * b07;
  out[6] = a32 * b02 - a30 * b05 - a33 * b01;
  out[7] = a20 * b05 - a22 * b02 + a23 * b01;
  out[8] = a10 * b10 - a11 * b08 + a13 * b06;
  out[9] = a01 * b08 - a00 * b10 - a03 * b06;
  out[10] = a30 * b04 - a31 * b02 + a33 * b00;
  out[11] = a21 * b02 - a20 * b04 - a23 * b00;
  out[12] = a11 * b07 - a10 * b09 - a12 * b06;
  out[13] = a00 * b09 - a01 * b07 + a02 * b06;
  out[14] = a31 * b01 - a30 * b03 - a32 * b00;
  out[15] = a20 * b03 - a21 * b01 + a22 * b00;
  return out;
}
function determinant2(a) {
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a03 = a[3];
  const a10 = a[4];
  const a11 = a[5];
  const a12 = a[6];
  const a13 = a[7];
  const a20 = a[8];
  const a21 = a[9];
  const a22 = a[10];
  const a23 = a[11];
  const a30 = a[12];
  const a31 = a[13];
  const a32 = a[14];
  const a33 = a[15];
  const b0 = a00 * a11 - a01 * a10;
  const b1 = a00 * a12 - a02 * a10;
  const b2 = a01 * a12 - a02 * a11;
  const b3 = a20 * a31 - a21 * a30;
  const b4 = a20 * a32 - a22 * a30;
  const b5 = a21 * a32 - a22 * a31;
  const b6 = a00 * b5 - a01 * b4 + a02 * b3;
  const b7 = a10 * b5 - a11 * b4 + a12 * b3;
  const b8 = a20 * b2 - a21 * b1 + a22 * b0;
  const b9 = a30 * b2 - a31 * b1 + a32 * b0;
  return a13 * b6 - a03 * b7 + a33 * b8 - a23 * b9;
}
function multiply4(out, a, b) {
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a03 = a[3];
  const a10 = a[4];
  const a11 = a[5];
  const a12 = a[6];
  const a13 = a[7];
  const a20 = a[8];
  const a21 = a[9];
  const a22 = a[10];
  const a23 = a[11];
  const a30 = a[12];
  const a31 = a[13];
  const a32 = a[14];
  const a33 = a[15];
  let b0 = b[0];
  let b1 = b[1];
  let b2 = b[2];
  let b3 = b[3];
  out[0] = b0 * a00 + b1 * a10 + b2 * a20 + b3 * a30;
  out[1] = b0 * a01 + b1 * a11 + b2 * a21 + b3 * a31;
  out[2] = b0 * a02 + b1 * a12 + b2 * a22 + b3 * a32;
  out[3] = b0 * a03 + b1 * a13 + b2 * a23 + b3 * a33;
  b0 = b[4];
  b1 = b[5];
  b2 = b[6];
  b3 = b[7];
  out[4] = b0 * a00 + b1 * a10 + b2 * a20 + b3 * a30;
  out[5] = b0 * a01 + b1 * a11 + b2 * a21 + b3 * a31;
  out[6] = b0 * a02 + b1 * a12 + b2 * a22 + b3 * a32;
  out[7] = b0 * a03 + b1 * a13 + b2 * a23 + b3 * a33;
  b0 = b[8];
  b1 = b[9];
  b2 = b[10];
  b3 = b[11];
  out[8] = b0 * a00 + b1 * a10 + b2 * a20 + b3 * a30;
  out[9] = b0 * a01 + b1 * a11 + b2 * a21 + b3 * a31;
  out[10] = b0 * a02 + b1 * a12 + b2 * a22 + b3 * a32;
  out[11] = b0 * a03 + b1 * a13 + b2 * a23 + b3 * a33;
  b0 = b[12];
  b1 = b[13];
  b2 = b[14];
  b3 = b[15];
  out[12] = b0 * a00 + b1 * a10 + b2 * a20 + b3 * a30;
  out[13] = b0 * a01 + b1 * a11 + b2 * a21 + b3 * a31;
  out[14] = b0 * a02 + b1 * a12 + b2 * a22 + b3 * a32;
  out[15] = b0 * a03 + b1 * a13 + b2 * a23 + b3 * a33;
  return out;
}
function translate2(out, a, v) {
  const x = v[0];
  const y = v[1];
  const z = v[2];
  let a00;
  let a01;
  let a02;
  let a03;
  let a10;
  let a11;
  let a12;
  let a13;
  let a20;
  let a21;
  let a22;
  let a23;
  if (a === out) {
    out[12] = a[0] * x + a[4] * y + a[8] * z + a[12];
    out[13] = a[1] * x + a[5] * y + a[9] * z + a[13];
    out[14] = a[2] * x + a[6] * y + a[10] * z + a[14];
    out[15] = a[3] * x + a[7] * y + a[11] * z + a[15];
  } else {
    a00 = a[0];
    a01 = a[1];
    a02 = a[2];
    a03 = a[3];
    a10 = a[4];
    a11 = a[5];
    a12 = a[6];
    a13 = a[7];
    a20 = a[8];
    a21 = a[9];
    a22 = a[10];
    a23 = a[11];
    out[0] = a00;
    out[1] = a01;
    out[2] = a02;
    out[3] = a03;
    out[4] = a10;
    out[5] = a11;
    out[6] = a12;
    out[7] = a13;
    out[8] = a20;
    out[9] = a21;
    out[10] = a22;
    out[11] = a23;
    out[12] = a00 * x + a10 * y + a20 * z + a[12];
    out[13] = a01 * x + a11 * y + a21 * z + a[13];
    out[14] = a02 * x + a12 * y + a22 * z + a[14];
    out[15] = a03 * x + a13 * y + a23 * z + a[15];
  }
  return out;
}
function scale4(out, a, v) {
  const x = v[0];
  const y = v[1];
  const z = v[2];
  out[0] = a[0] * x;
  out[1] = a[1] * x;
  out[2] = a[2] * x;
  out[3] = a[3] * x;
  out[4] = a[4] * y;
  out[5] = a[5] * y;
  out[6] = a[6] * y;
  out[7] = a[7] * y;
  out[8] = a[8] * z;
  out[9] = a[9] * z;
  out[10] = a[10] * z;
  out[11] = a[11] * z;
  out[12] = a[12];
  out[13] = a[13];
  out[14] = a[14];
  out[15] = a[15];
  return out;
}
function rotate3(out, a, rad, axis) {
  let x = axis[0];
  let y = axis[1];
  let z = axis[2];
  let len5 = Math.sqrt(x * x + y * y + z * z);
  let c;
  let s;
  let t;
  let a00;
  let a01;
  let a02;
  let a03;
  let a10;
  let a11;
  let a12;
  let a13;
  let a20;
  let a21;
  let a22;
  let a23;
  let b00;
  let b01;
  let b02;
  let b10;
  let b11;
  let b12;
  let b20;
  let b21;
  let b22;
  if (len5 < EPSILON) {
    return null;
  }
  len5 = 1 / len5;
  x *= len5;
  y *= len5;
  z *= len5;
  s = Math.sin(rad);
  c = Math.cos(rad);
  t = 1 - c;
  a00 = a[0];
  a01 = a[1];
  a02 = a[2];
  a03 = a[3];
  a10 = a[4];
  a11 = a[5];
  a12 = a[6];
  a13 = a[7];
  a20 = a[8];
  a21 = a[9];
  a22 = a[10];
  a23 = a[11];
  b00 = x * x * t + c;
  b01 = y * x * t + z * s;
  b02 = z * x * t - y * s;
  b10 = x * y * t - z * s;
  b11 = y * y * t + c;
  b12 = z * y * t + x * s;
  b20 = x * z * t + y * s;
  b21 = y * z * t - x * s;
  b22 = z * z * t + c;
  out[0] = a00 * b00 + a10 * b01 + a20 * b02;
  out[1] = a01 * b00 + a11 * b01 + a21 * b02;
  out[2] = a02 * b00 + a12 * b01 + a22 * b02;
  out[3] = a03 * b00 + a13 * b01 + a23 * b02;
  out[4] = a00 * b10 + a10 * b11 + a20 * b12;
  out[5] = a01 * b10 + a11 * b11 + a21 * b12;
  out[6] = a02 * b10 + a12 * b11 + a22 * b12;
  out[7] = a03 * b10 + a13 * b11 + a23 * b12;
  out[8] = a00 * b20 + a10 * b21 + a20 * b22;
  out[9] = a01 * b20 + a11 * b21 + a21 * b22;
  out[10] = a02 * b20 + a12 * b21 + a22 * b22;
  out[11] = a03 * b20 + a13 * b21 + a23 * b22;
  if (a !== out) {
    out[12] = a[12];
    out[13] = a[13];
    out[14] = a[14];
    out[15] = a[15];
  }
  return out;
}
function rotateX2(out, a, rad) {
  const s = Math.sin(rad);
  const c = Math.cos(rad);
  const a10 = a[4];
  const a11 = a[5];
  const a12 = a[6];
  const a13 = a[7];
  const a20 = a[8];
  const a21 = a[9];
  const a22 = a[10];
  const a23 = a[11];
  if (a !== out) {
    out[0] = a[0];
    out[1] = a[1];
    out[2] = a[2];
    out[3] = a[3];
    out[12] = a[12];
    out[13] = a[13];
    out[14] = a[14];
    out[15] = a[15];
  }
  out[4] = a10 * c + a20 * s;
  out[5] = a11 * c + a21 * s;
  out[6] = a12 * c + a22 * s;
  out[7] = a13 * c + a23 * s;
  out[8] = a20 * c - a10 * s;
  out[9] = a21 * c - a11 * s;
  out[10] = a22 * c - a12 * s;
  out[11] = a23 * c - a13 * s;
  return out;
}
function rotateY2(out, a, rad) {
  const s = Math.sin(rad);
  const c = Math.cos(rad);
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a03 = a[3];
  const a20 = a[8];
  const a21 = a[9];
  const a22 = a[10];
  const a23 = a[11];
  if (a !== out) {
    out[4] = a[4];
    out[5] = a[5];
    out[6] = a[6];
    out[7] = a[7];
    out[12] = a[12];
    out[13] = a[13];
    out[14] = a[14];
    out[15] = a[15];
  }
  out[0] = a00 * c - a20 * s;
  out[1] = a01 * c - a21 * s;
  out[2] = a02 * c - a22 * s;
  out[3] = a03 * c - a23 * s;
  out[8] = a00 * s + a20 * c;
  out[9] = a01 * s + a21 * c;
  out[10] = a02 * s + a22 * c;
  out[11] = a03 * s + a23 * c;
  return out;
}
function rotateZ2(out, a, rad) {
  const s = Math.sin(rad);
  const c = Math.cos(rad);
  const a00 = a[0];
  const a01 = a[1];
  const a02 = a[2];
  const a03 = a[3];
  const a10 = a[4];
  const a11 = a[5];
  const a12 = a[6];
  const a13 = a[7];
  if (a !== out) {
    out[8] = a[8];
    out[9] = a[9];
    out[10] = a[10];
    out[11] = a[11];
    out[12] = a[12];
    out[13] = a[13];
    out[14] = a[14];
    out[15] = a[15];
  }
  out[0] = a00 * c + a10 * s;
  out[1] = a01 * c + a11 * s;
  out[2] = a02 * c + a12 * s;
  out[3] = a03 * c + a13 * s;
  out[4] = a10 * c - a00 * s;
  out[5] = a11 * c - a01 * s;
  out[6] = a12 * c - a02 * s;
  out[7] = a13 * c - a03 * s;
  return out;
}
function fromTranslation2(out, v) {
  out[0] = 1;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = 0;
  out[5] = 1;
  out[6] = 0;
  out[7] = 0;
  out[8] = 0;
  out[9] = 0;
  out[10] = 1;
  out[11] = 0;
  out[12] = v[0];
  out[13] = v[1];
  out[14] = v[2];
  out[15] = 1;
  return out;
}
function fromScaling2(out, v) {
  out[0] = v[0];
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = 0;
  out[5] = v[1];
  out[6] = 0;
  out[7] = 0;
  out[8] = 0;
  out[9] = 0;
  out[10] = v[2];
  out[11] = 0;
  out[12] = 0;
  out[13] = 0;
  out[14] = 0;
  out[15] = 1;
  return out;
}
function fromRotation2(out, rad, axis) {
  let x = axis[0];
  let y = axis[1];
  let z = axis[2];
  let len5 = Math.sqrt(x * x + y * y + z * z);
  let c;
  let s;
  let t;
  if (len5 < EPSILON) {
    return null;
  }
  len5 = 1 / len5;
  x *= len5;
  y *= len5;
  z *= len5;
  s = Math.sin(rad);
  c = Math.cos(rad);
  t = 1 - c;
  out[0] = x * x * t + c;
  out[1] = y * x * t + z * s;
  out[2] = z * x * t - y * s;
  out[3] = 0;
  out[4] = x * y * t - z * s;
  out[5] = y * y * t + c;
  out[6] = z * y * t + x * s;
  out[7] = 0;
  out[8] = x * z * t + y * s;
  out[9] = y * z * t - x * s;
  out[10] = z * z * t + c;
  out[11] = 0;
  out[12] = 0;
  out[13] = 0;
  out[14] = 0;
  out[15] = 1;
  return out;
}
function fromXRotation(out, rad) {
  const s = Math.sin(rad);
  const c = Math.cos(rad);
  out[0] = 1;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = 0;
  out[5] = c;
  out[6] = s;
  out[7] = 0;
  out[8] = 0;
  out[9] = -s;
  out[10] = c;
  out[11] = 0;
  out[12] = 0;
  out[13] = 0;
  out[14] = 0;
  out[15] = 1;
  return out;
}
function fromYRotation(out, rad) {
  const s = Math.sin(rad);
  const c = Math.cos(rad);
  out[0] = c;
  out[1] = 0;
  out[2] = -s;
  out[3] = 0;
  out[4] = 0;
  out[5] = 1;
  out[6] = 0;
  out[7] = 0;
  out[8] = s;
  out[9] = 0;
  out[10] = c;
  out[11] = 0;
  out[12] = 0;
  out[13] = 0;
  out[14] = 0;
  out[15] = 1;
  return out;
}
function fromZRotation(out, rad) {
  const s = Math.sin(rad);
  const c = Math.cos(rad);
  out[0] = c;
  out[1] = s;
  out[2] = 0;
  out[3] = 0;
  out[4] = -s;
  out[5] = c;
  out[6] = 0;
  out[7] = 0;
  out[8] = 0;
  out[9] = 0;
  out[10] = 1;
  out[11] = 0;
  out[12] = 0;
  out[13] = 0;
  out[14] = 0;
  out[15] = 1;
  return out;
}
function fromRotationTranslation(out, q, v) {
  const x = q[0];
  const y = q[1];
  const z = q[2];
  const w = q[3];
  const x2 = x + x;
  const y2 = y + y;
  const z2 = z + z;
  const xx = x * x2;
  const xy = x * y2;
  const xz = x * z2;
  const yy = y * y2;
  const yz = y * z2;
  const zz = z * z2;
  const wx = w * x2;
  const wy = w * y2;
  const wz = w * z2;
  out[0] = 1 - (yy + zz);
  out[1] = xy + wz;
  out[2] = xz - wy;
  out[3] = 0;
  out[4] = xy - wz;
  out[5] = 1 - (xx + zz);
  out[6] = yz + wx;
  out[7] = 0;
  out[8] = xz + wy;
  out[9] = yz - wx;
  out[10] = 1 - (xx + yy);
  out[11] = 0;
  out[12] = v[0];
  out[13] = v[1];
  out[14] = v[2];
  out[15] = 1;
  return out;
}
function fromQuat2(out, a) {
  const translation = new ARRAY_TYPE(3);
  const bx = -a[0];
  const by = -a[1];
  const bz = -a[2];
  const bw = a[3];
  const ax = a[4];
  const ay = a[5];
  const az = a[6];
  const aw = a[7];
  const magnitude = bx * bx + by * by + bz * bz + bw * bw;
  if (magnitude > 0) {
    translation[0] = (ax * bw + aw * bx + ay * bz - az * by) * 2 / magnitude;
    translation[1] = (ay * bw + aw * by + az * bx - ax * bz) * 2 / magnitude;
    translation[2] = (az * bw + aw * bz + ax * by - ay * bx) * 2 / magnitude;
  } else {
    translation[0] = (ax * bw + aw * bx + ay * bz - az * by) * 2;
    translation[1] = (ay * bw + aw * by + az * bx - ax * bz) * 2;
    translation[2] = (az * bw + aw * bz + ax * by - ay * bx) * 2;
  }
  fromRotationTranslation(out, a, translation);
  return out;
}
function getTranslation(out, mat) {
  out[0] = mat[12];
  out[1] = mat[13];
  out[2] = mat[14];
  return out;
}
function getScaling(out, mat) {
  const m11 = mat[0];
  const m12 = mat[1];
  const m13 = mat[2];
  const m21 = mat[4];
  const m22 = mat[5];
  const m23 = mat[6];
  const m31 = mat[8];
  const m32 = mat[9];
  const m33 = mat[10];
  out[0] = Math.sqrt(m11 * m11 + m12 * m12 + m13 * m13);
  out[1] = Math.sqrt(m21 * m21 + m22 * m22 + m23 * m23);
  out[2] = Math.sqrt(m31 * m31 + m32 * m32 + m33 * m33);
  return out;
}
function getRotation(out, mat) {
  const scaling = new ARRAY_TYPE(3);
  getScaling(scaling, mat);
  const is1 = 1 / scaling[0];
  const is2 = 1 / scaling[1];
  const is3 = 1 / scaling[2];
  const sm11 = mat[0] * is1;
  const sm12 = mat[1] * is2;
  const sm13 = mat[2] * is3;
  const sm21 = mat[4] * is1;
  const sm22 = mat[5] * is2;
  const sm23 = mat[6] * is3;
  const sm31 = mat[8] * is1;
  const sm32 = mat[9] * is2;
  const sm33 = mat[10] * is3;
  const trace = sm11 + sm22 + sm33;
  let S = 0;
  if (trace > 0) {
    S = Math.sqrt(trace + 1) * 2;
    out[3] = 0.25 * S;
    out[0] = (sm23 - sm32) / S;
    out[1] = (sm31 - sm13) / S;
    out[2] = (sm12 - sm21) / S;
  } else if (sm11 > sm22 && sm11 > sm33) {
    S = Math.sqrt(1 + sm11 - sm22 - sm33) * 2;
    out[3] = (sm23 - sm32) / S;
    out[0] = 0.25 * S;
    out[1] = (sm12 + sm21) / S;
    out[2] = (sm31 + sm13) / S;
  } else if (sm22 > sm33) {
    S = Math.sqrt(1 + sm22 - sm11 - sm33) * 2;
    out[3] = (sm31 - sm13) / S;
    out[0] = (sm12 + sm21) / S;
    out[1] = 0.25 * S;
    out[2] = (sm23 + sm32) / S;
  } else {
    S = Math.sqrt(1 + sm33 - sm11 - sm22) * 2;
    out[3] = (sm12 - sm21) / S;
    out[0] = (sm31 + sm13) / S;
    out[1] = (sm23 + sm32) / S;
    out[2] = 0.25 * S;
  }
  return out;
}
function decompose(out_r, out_t, out_s, mat) {
  out_t[0] = mat[12];
  out_t[1] = mat[13];
  out_t[2] = mat[14];
  const m11 = mat[0];
  const m12 = mat[1];
  const m13 = mat[2];
  const m21 = mat[4];
  const m22 = mat[5];
  const m23 = mat[6];
  const m31 = mat[8];
  const m32 = mat[9];
  const m33 = mat[10];
  out_s[0] = Math.sqrt(m11 * m11 + m12 * m12 + m13 * m13);
  out_s[1] = Math.sqrt(m21 * m21 + m22 * m22 + m23 * m23);
  out_s[2] = Math.sqrt(m31 * m31 + m32 * m32 + m33 * m33);
  const is1 = 1 / out_s[0];
  const is2 = 1 / out_s[1];
  const is3 = 1 / out_s[2];
  const sm11 = m11 * is1;
  const sm12 = m12 * is2;
  const sm13 = m13 * is3;
  const sm21 = m21 * is1;
  const sm22 = m22 * is2;
  const sm23 = m23 * is3;
  const sm31 = m31 * is1;
  const sm32 = m32 * is2;
  const sm33 = m33 * is3;
  const trace = sm11 + sm22 + sm33;
  let S = 0;
  if (trace > 0) {
    S = Math.sqrt(trace + 1) * 2;
    out_r[3] = 0.25 * S;
    out_r[0] = (sm23 - sm32) / S;
    out_r[1] = (sm31 - sm13) / S;
    out_r[2] = (sm12 - sm21) / S;
  } else if (sm11 > sm22 && sm11 > sm33) {
    S = Math.sqrt(1 + sm11 - sm22 - sm33) * 2;
    out_r[3] = (sm23 - sm32) / S;
    out_r[0] = 0.25 * S;
    out_r[1] = (sm12 + sm21) / S;
    out_r[2] = (sm31 + sm13) / S;
  } else if (sm22 > sm33) {
    S = Math.sqrt(1 + sm22 - sm11 - sm33) * 2;
    out_r[3] = (sm31 - sm13) / S;
    out_r[0] = (sm12 + sm21) / S;
    out_r[1] = 0.25 * S;
    out_r[2] = (sm23 + sm32) / S;
  } else {
    S = Math.sqrt(1 + sm33 - sm11 - sm22) * 2;
    out_r[3] = (sm12 - sm21) / S;
    out_r[0] = (sm31 + sm13) / S;
    out_r[1] = (sm23 + sm32) / S;
    out_r[2] = 0.25 * S;
  }
  return out_r;
}
function fromRotationTranslationScale(out, q, v, s) {
  const x = q[0];
  const y = q[1];
  const z = q[2];
  const w = q[3];
  const x2 = x + x;
  const y2 = y + y;
  const z2 = z + z;
  const xx = x * x2;
  const xy = x * y2;
  const xz = x * z2;
  const yy = y * y2;
  const yz = y * z2;
  const zz = z * z2;
  const wx = w * x2;
  const wy = w * y2;
  const wz = w * z2;
  const sx = s[0];
  const sy = s[1];
  const sz = s[2];
  out[0] = (1 - (yy + zz)) * sx;
  out[1] = (xy + wz) * sx;
  out[2] = (xz - wy) * sx;
  out[3] = 0;
  out[4] = (xy - wz) * sy;
  out[5] = (1 - (xx + zz)) * sy;
  out[6] = (yz + wx) * sy;
  out[7] = 0;
  out[8] = (xz + wy) * sz;
  out[9] = (yz - wx) * sz;
  out[10] = (1 - (xx + yy)) * sz;
  out[11] = 0;
  out[12] = v[0];
  out[13] = v[1];
  out[14] = v[2];
  out[15] = 1;
  return out;
}
function fromRotationTranslationScaleOrigin(out, q, v, s, o) {
  const x = q[0];
  const y = q[1];
  const z = q[2];
  const w = q[3];
  const x2 = x + x;
  const y2 = y + y;
  const z2 = z + z;
  const xx = x * x2;
  const xy = x * y2;
  const xz = x * z2;
  const yy = y * y2;
  const yz = y * z2;
  const zz = z * z2;
  const wx = w * x2;
  const wy = w * y2;
  const wz = w * z2;
  const sx = s[0];
  const sy = s[1];
  const sz = s[2];
  const ox = o[0];
  const oy = o[1];
  const oz = o[2];
  const out0 = (1 - (yy + zz)) * sx;
  const out1 = (xy + wz) * sx;
  const out2 = (xz - wy) * sx;
  const out4 = (xy - wz) * sy;
  const out5 = (1 - (xx + zz)) * sy;
  const out6 = (yz + wx) * sy;
  const out8 = (xz + wy) * sz;
  const out9 = (yz - wx) * sz;
  const out10 = (1 - (xx + yy)) * sz;
  out[0] = out0;
  out[1] = out1;
  out[2] = out2;
  out[3] = 0;
  out[4] = out4;
  out[5] = out5;
  out[6] = out6;
  out[7] = 0;
  out[8] = out8;
  out[9] = out9;
  out[10] = out10;
  out[11] = 0;
  out[12] = v[0] + ox - (out0 * ox + out4 * oy + out8 * oz);
  out[13] = v[1] + oy - (out1 * ox + out5 * oy + out9 * oz);
  out[14] = v[2] + oz - (out2 * ox + out6 * oy + out10 * oz);
  out[15] = 1;
  return out;
}
function fromQuat3(out, q) {
  const x = q[0];
  const y = q[1];
  const z = q[2];
  const w = q[3];
  const x2 = x + x;
  const y2 = y + y;
  const z2 = z + z;
  const xx = x * x2;
  const yx = y * x2;
  const yy = y * y2;
  const zx = z * x2;
  const zy = z * y2;
  const zz = z * z2;
  const wx = w * x2;
  const wy = w * y2;
  const wz = w * z2;
  out[0] = 1 - yy - zz;
  out[1] = yx + wz;
  out[2] = zx - wy;
  out[3] = 0;
  out[4] = yx - wz;
  out[5] = 1 - xx - zz;
  out[6] = zy + wx;
  out[7] = 0;
  out[8] = zx + wy;
  out[9] = zy - wx;
  out[10] = 1 - xx - yy;
  out[11] = 0;
  out[12] = 0;
  out[13] = 0;
  out[14] = 0;
  out[15] = 1;
  return out;
}
function frustum(out, left, right, bottom, top, near, far) {
  const rl = 1 / (right - left);
  const tb = 1 / (top - bottom);
  const nf = 1 / (near - far);
  out[0] = near * 2 * rl;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = 0;
  out[5] = near * 2 * tb;
  out[6] = 0;
  out[7] = 0;
  out[8] = (right + left) * rl;
  out[9] = (top + bottom) * tb;
  out[10] = (far + near) * nf;
  out[11] = -1;
  out[12] = 0;
  out[13] = 0;
  out[14] = far * near * 2 * nf;
  out[15] = 0;
  return out;
}
function perspectiveNO(out, fovy, aspect, near, far) {
  const f = 1 / Math.tan(fovy / 2);
  out[0] = f / aspect;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = 0;
  out[5] = f;
  out[6] = 0;
  out[7] = 0;
  out[8] = 0;
  out[9] = 0;
  out[11] = -1;
  out[12] = 0;
  out[13] = 0;
  out[15] = 0;
  if (far != null && far !== Infinity) {
    const nf = 1 / (near - far);
    out[10] = (far + near) * nf;
    out[14] = 2 * far * near * nf;
  } else {
    out[10] = -1;
    out[14] = -2 * near;
  }
  return out;
}
var perspective = perspectiveNO;
function perspectiveZO(out, fovy, aspect, near, far) {
  const f = 1 / Math.tan(fovy / 2);
  out[0] = f / aspect;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = 0;
  out[5] = f;
  out[6] = 0;
  out[7] = 0;
  out[8] = 0;
  out[9] = 0;
  out[11] = -1;
  out[12] = 0;
  out[13] = 0;
  out[15] = 0;
  if (far != null && far !== Infinity) {
    const nf = 1 / (near - far);
    out[10] = far * nf;
    out[14] = far * near * nf;
  } else {
    out[10] = -1;
    out[14] = -near;
  }
  return out;
}
function perspectiveFromFieldOfView(out, fov, near, far) {
  const upTan = Math.tan(fov.upDegrees * Math.PI / 180);
  const downTan = Math.tan(fov.downDegrees * Math.PI / 180);
  const leftTan = Math.tan(fov.leftDegrees * Math.PI / 180);
  const rightTan = Math.tan(fov.rightDegrees * Math.PI / 180);
  const xScale = 2 / (leftTan + rightTan);
  const yScale = 2 / (upTan + downTan);
  out[0] = xScale;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = 0;
  out[5] = yScale;
  out[6] = 0;
  out[7] = 0;
  out[8] = -((leftTan - rightTan) * xScale * 0.5);
  out[9] = (upTan - downTan) * yScale * 0.5;
  out[10] = far / (near - far);
  out[11] = -1;
  out[12] = 0;
  out[13] = 0;
  out[14] = far * near / (near - far);
  out[15] = 0;
  return out;
}
function orthoNO(out, left, right, bottom, top, near, far) {
  const lr = 1 / (left - right);
  const bt = 1 / (bottom - top);
  const nf = 1 / (near - far);
  out[0] = -2 * lr;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = 0;
  out[5] = -2 * bt;
  out[6] = 0;
  out[7] = 0;
  out[8] = 0;
  out[9] = 0;
  out[10] = 2 * nf;
  out[11] = 0;
  out[12] = (left + right) * lr;
  out[13] = (top + bottom) * bt;
  out[14] = (far + near) * nf;
  out[15] = 1;
  return out;
}
var ortho = orthoNO;
function orthoZO(out, left, right, bottom, top, near, far) {
  const lr = 1 / (left - right);
  const bt = 1 / (bottom - top);
  const nf = 1 / (near - far);
  out[0] = -2 * lr;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  out[4] = 0;
  out[5] = -2 * bt;
  out[6] = 0;
  out[7] = 0;
  out[8] = 0;
  out[9] = 0;
  out[10] = nf;
  out[11] = 0;
  out[12] = (left + right) * lr;
  out[13] = (top + bottom) * bt;
  out[14] = near * nf;
  out[15] = 1;
  return out;
}
function lookAt(out, eye, center, up) {
  let len5;
  let x0;
  let x1;
  let x2;
  let y0;
  let y1;
  let y2;
  let z0;
  let z1;
  let z2;
  const eyex = eye[0];
  const eyey = eye[1];
  const eyez = eye[2];
  const upx = up[0];
  const upy = up[1];
  const upz = up[2];
  const centerx = center[0];
  const centery = center[1];
  const centerz = center[2];
  if (Math.abs(eyex - centerx) < EPSILON && Math.abs(eyey - centery) < EPSILON && Math.abs(eyez - centerz) < EPSILON) {
    return identity2(out);
  }
  z0 = eyex - centerx;
  z1 = eyey - centery;
  z2 = eyez - centerz;
  len5 = 1 / Math.sqrt(z0 * z0 + z1 * z1 + z2 * z2);
  z0 *= len5;
  z1 *= len5;
  z2 *= len5;
  x0 = upy * z2 - upz * z1;
  x1 = upz * z0 - upx * z2;
  x2 = upx * z1 - upy * z0;
  len5 = Math.sqrt(x0 * x0 + x1 * x1 + x2 * x2);
  if (!len5) {
    x0 = 0;
    x1 = 0;
    x2 = 0;
  } else {
    len5 = 1 / len5;
    x0 *= len5;
    x1 *= len5;
    x2 *= len5;
  }
  y0 = z1 * x2 - z2 * x1;
  y1 = z2 * x0 - z0 * x2;
  y2 = z0 * x1 - z1 * x0;
  len5 = Math.sqrt(y0 * y0 + y1 * y1 + y2 * y2);
  if (!len5) {
    y0 = 0;
    y1 = 0;
    y2 = 0;
  } else {
    len5 = 1 / len5;
    y0 *= len5;
    y1 *= len5;
    y2 *= len5;
  }
  out[0] = x0;
  out[1] = y0;
  out[2] = z0;
  out[3] = 0;
  out[4] = x1;
  out[5] = y1;
  out[6] = z1;
  out[7] = 0;
  out[8] = x2;
  out[9] = y2;
  out[10] = z2;
  out[11] = 0;
  out[12] = -(x0 * eyex + x1 * eyey + x2 * eyez);
  out[13] = -(y0 * eyex + y1 * eyey + y2 * eyez);
  out[14] = -(z0 * eyex + z1 * eyey + z2 * eyez);
  out[15] = 1;
  return out;
}
function targetTo(out, eye, target, up) {
  const eyex = eye[0];
  const eyey = eye[1];
  const eyez = eye[2];
  const upx = up[0];
  const upy = up[1];
  const upz = up[2];
  let z0 = eyex - target[0];
  let z1 = eyey - target[1];
  let z2 = eyez - target[2];
  let len5 = z0 * z0 + z1 * z1 + z2 * z2;
  if (len5 > 0) {
    len5 = 1 / Math.sqrt(len5);
    z0 *= len5;
    z1 *= len5;
    z2 *= len5;
  }
  let x0 = upy * z2 - upz * z1;
  let x1 = upz * z0 - upx * z2;
  let x2 = upx * z1 - upy * z0;
  len5 = x0 * x0 + x1 * x1 + x2 * x2;
  if (len5 > 0) {
    len5 = 1 / Math.sqrt(len5);
    x0 *= len5;
    x1 *= len5;
    x2 *= len5;
  }
  out[0] = x0;
  out[1] = x1;
  out[2] = x2;
  out[3] = 0;
  out[4] = z1 * x2 - z2 * x1;
  out[5] = z2 * x0 - z0 * x2;
  out[6] = z0 * x1 - z1 * x0;
  out[7] = 0;
  out[8] = z0;
  out[9] = z1;
  out[10] = z2;
  out[11] = 0;
  out[12] = eyex;
  out[13] = eyey;
  out[14] = eyez;
  out[15] = 1;
  return out;
}
function str4(a) {
  return `mat4(${a[0]}, ${a[1]}, ${a[2]}, ${a[3]}, ${a[4]}, ${a[5]}, ${a[6]}, ${a[7]}, ${a[8]}, ${a[9]}, ${a[10]}, ${a[11]}, ${a[12]}, ${a[13]}, ${a[14]}, ${a[15]})`;
}
function frob2(a) {
  return Math.sqrt(a[0] * a[0] + a[1] * a[1] + a[2] * a[2] + a[3] * a[3] + a[4] * a[4] + a[5] * a[5] + a[6] * a[6] + a[7] * a[7] + a[8] * a[8] + a[9] * a[9] + a[10] * a[10] + a[11] * a[11] + a[12] * a[12] + a[13] * a[13] + a[14] * a[14] + a[15] * a[15]);
}
function add4(out, a, b) {
  out[0] = a[0] + b[0];
  out[1] = a[1] + b[1];
  out[2] = a[2] + b[2];
  out[3] = a[3] + b[3];
  out[4] = a[4] + b[4];
  out[5] = a[5] + b[5];
  out[6] = a[6] + b[6];
  out[7] = a[7] + b[7];
  out[8] = a[8] + b[8];
  out[9] = a[9] + b[9];
  out[10] = a[10] + b[10];
  out[11] = a[11] + b[11];
  out[12] = a[12] + b[12];
  out[13] = a[13] + b[13];
  out[14] = a[14] + b[14];
  out[15] = a[15] + b[15];
  return out;
}
function subtract4(out, a, b) {
  out[0] = a[0] - b[0];
  out[1] = a[1] - b[1];
  out[2] = a[2] - b[2];
  out[3] = a[3] - b[3];
  out[4] = a[4] - b[4];
  out[5] = a[5] - b[5];
  out[6] = a[6] - b[6];
  out[7] = a[7] - b[7];
  out[8] = a[8] - b[8];
  out[9] = a[9] - b[9];
  out[10] = a[10] - b[10];
  out[11] = a[11] - b[11];
  out[12] = a[12] - b[12];
  out[13] = a[13] - b[13];
  out[14] = a[14] - b[14];
  out[15] = a[15] - b[15];
  return out;
}
function multiplyScalar2(out, a, b) {
  out[0] = a[0] * b;
  out[1] = a[1] * b;
  out[2] = a[2] * b;
  out[3] = a[3] * b;
  out[4] = a[4] * b;
  out[5] = a[5] * b;
  out[6] = a[6] * b;
  out[7] = a[7] * b;
  out[8] = a[8] * b;
  out[9] = a[9] * b;
  out[10] = a[10] * b;
  out[11] = a[11] * b;
  out[12] = a[12] * b;
  out[13] = a[13] * b;
  out[14] = a[14] * b;
  out[15] = a[15] * b;
  return out;
}
function multiplyScalarAndAdd2(out, a, b, scale7) {
  out[0] = a[0] + b[0] * scale7;
  out[1] = a[1] + b[1] * scale7;
  out[2] = a[2] + b[2] * scale7;
  out[3] = a[3] + b[3] * scale7;
  out[4] = a[4] + b[4] * scale7;
  out[5] = a[5] + b[5] * scale7;
  out[6] = a[6] + b[6] * scale7;
  out[7] = a[7] + b[7] * scale7;
  out[8] = a[8] + b[8] * scale7;
  out[9] = a[9] + b[9] * scale7;
  out[10] = a[10] + b[10] * scale7;
  out[11] = a[11] + b[11] * scale7;
  out[12] = a[12] + b[12] * scale7;
  out[13] = a[13] + b[13] * scale7;
  out[14] = a[14] + b[14] * scale7;
  out[15] = a[15] + b[15] * scale7;
  return out;
}
function exactEquals5(a, b) {
  return a[0] === b[0] && a[1] === b[1] && a[2] === b[2] && a[3] === b[3] && a[4] === b[4] && a[5] === b[5] && a[6] === b[6] && a[7] === b[7] && a[8] === b[8] && a[9] === b[9] && a[10] === b[10] && a[11] === b[11] && a[12] === b[12] && a[13] === b[13] && a[14] === b[14] && a[15] === b[15];
}
function equals5(a, b) {
  const a0 = a[0];
  const a1 = a[1];
  const a2 = a[2];
  const a3 = a[3];
  const a4 = a[4];
  const a5 = a[5];
  const a6 = a[6];
  const a7 = a[7];
  const a8 = a[8];
  const a9 = a[9];
  const a10 = a[10];
  const a11 = a[11];
  const a12 = a[12];
  const a13 = a[13];
  const a14 = a[14];
  const a15 = a[15];
  const b0 = b[0];
  const b1 = b[1];
  const b2 = b[2];
  const b3 = b[3];
  const b4 = b[4];
  const b5 = b[5];
  const b6 = b[6];
  const b7 = b[7];
  const b8 = b[8];
  const b9 = b[9];
  const b10 = b[10];
  const b11 = b[11];
  const b12 = b[12];
  const b13 = b[13];
  const b14 = b[14];
  const b15 = b[15];
  return Math.abs(a0 - b0) <= EPSILON * Math.max(1, Math.abs(a0), Math.abs(b0)) && Math.abs(a1 - b1) <= EPSILON * Math.max(1, Math.abs(a1), Math.abs(b1)) && Math.abs(a2 - b2) <= EPSILON * Math.max(1, Math.abs(a2), Math.abs(b2)) && Math.abs(a3 - b3) <= EPSILON * Math.max(1, Math.abs(a3), Math.abs(b3)) && Math.abs(a4 - b4) <= EPSILON * Math.max(1, Math.abs(a4), Math.abs(b4)) && Math.abs(a5 - b5) <= EPSILON * Math.max(1, Math.abs(a5), Math.abs(b5)) && Math.abs(a6 - b6) <= EPSILON * Math.max(1, Math.abs(a6), Math.abs(b6)) && Math.abs(a7 - b7) <= EPSILON * Math.max(1, Math.abs(a7), Math.abs(b7)) && Math.abs(a8 - b8) <= EPSILON * Math.max(1, Math.abs(a8), Math.abs(b8)) && Math.abs(a9 - b9) <= EPSILON * Math.max(1, Math.abs(a9), Math.abs(b9)) && Math.abs(a10 - b10) <= EPSILON * Math.max(1, Math.abs(a10), Math.abs(b10)) && Math.abs(a11 - b11) <= EPSILON * Math.max(1, Math.abs(a11), Math.abs(b11)) && Math.abs(a12 - b12) <= EPSILON * Math.max(1, Math.abs(a12), Math.abs(b12)) && Math.abs(a13 - b13) <= EPSILON * Math.max(1, Math.abs(a13), Math.abs(b13)) && Math.abs(a14 - b14) <= EPSILON * Math.max(1, Math.abs(a14), Math.abs(b14)) && Math.abs(a15 - b15) <= EPSILON * Math.max(1, Math.abs(a15), Math.abs(b15));
}
var mul4 = multiply4;
var sub4 = subtract4;

// dist/gl-matrix/vec4.js
var vec4_exports = {};
__export(vec4_exports, {
  add: () => add5,
  ceil: () => ceil3,
  clone: () => clone6,
  copy: () => copy5,
  create: () => create5,
  cross: () => cross3,
  dist: () => dist3,
  distance: () => distance3,
  div: () => div3,
  divide: () => divide3,
  dot: () => dot3,
  equals: () => equals6,
  exactEquals: () => exactEquals6,
  floor: () => floor3,
  forEach: () => forEach3,
  fromValues: () => fromValues5,
  inverse: () => inverse3,
  len: () => len3,
  length: () => length3,
  lerp: () => lerp4,
  max: () => max3,
  min: () => min3,
  mul: () => mul5,
  multiply: () => multiply5,
  negate: () => negate3,
  normalize: () => normalize3,
  random: () => random3,
  round: () => round5,
  scale: () => scale5,
  scaleAndAdd: () => scaleAndAdd3,
  set: () => set5,
  sqrDist: () => sqrDist3,
  sqrLen: () => sqrLen3,
  squaredDistance: () => squaredDistance3,
  squaredLength: () => squaredLength3,
  str: () => str5,
  sub: () => sub5,
  subtract: () => subtract5,
  transformMat4: () => transformMat43,
  transformQuat: () => transformQuat2,
  zero: () => zero3
});
function create5() {
  const out = new ARRAY_TYPE(4);
  if (ARRAY_TYPE != Float32Array) {
    out[0] = 0;
    out[1] = 0;
    out[2] = 0;
    out[3] = 0;
  }
  return out;
}
function clone6(a) {
  const out = new ARRAY_TYPE(4);
  out[0] = a[0];
  out[1] = a[1];
  out[2] = a[2];
  out[3] = a[3];
  return out;
}
function fromValues5(x, y, z, w) {
  const out = new ARRAY_TYPE(4);
  out[0] = x;
  out[1] = y;
  out[2] = z;
  out[3] = w;
  return out;
}
function copy5(out, a) {
  out[0] = a[0];
  out[1] = a[1];
  out[2] = a[2];
  out[3] = a[3];
  return out;
}
function set5(out, x, y, z, w) {
  out[0] = x;
  out[1] = y;
  out[2] = z;
  out[3] = w;
  return out;
}
function add5(out, a, b) {
  out[0] = a[0] + b[0];
  out[1] = a[1] + b[1];
  out[2] = a[2] + b[2];
  out[3] = a[3] + b[3];
  return out;
}
function subtract5(out, a, b) {
  out[0] = a[0] - b[0];
  out[1] = a[1] - b[1];
  out[2] = a[2] - b[2];
  out[3] = a[3] - b[3];
  return out;
}
function multiply5(out, a, b) {
  out[0] = a[0] * b[0];
  out[1] = a[1] * b[1];
  out[2] = a[2] * b[2];
  out[3] = a[3] * b[3];
  return out;
}
function divide3(out, a, b) {
  out[0] = a[0] / b[0];
  out[1] = a[1] / b[1];
  out[2] = a[2] / b[2];
  out[3] = a[3] / b[3];
  return out;
}
function ceil3(out, a) {
  out[0] = Math.ceil(a[0]);
  out[1] = Math.ceil(a[1]);
  out[2] = Math.ceil(a[2]);
  out[3] = Math.ceil(a[3]);
  return out;
}
function floor3(out, a) {
  out[0] = Math.floor(a[0]);
  out[1] = Math.floor(a[1]);
  out[2] = Math.floor(a[2]);
  out[3] = Math.floor(a[3]);
  return out;
}
function min3(out, a, b) {
  out[0] = Math.min(a[0], b[0]);
  out[1] = Math.min(a[1], b[1]);
  out[2] = Math.min(a[2], b[2]);
  out[3] = Math.min(a[3], b[3]);
  return out;
}
function max3(out, a, b) {
  out[0] = Math.max(a[0], b[0]);
  out[1] = Math.max(a[1], b[1]);
  out[2] = Math.max(a[2], b[2]);
  out[3] = Math.max(a[3], b[3]);
  return out;
}
function round5(out, a) {
  out[0] = round2(a[0]);
  out[1] = round2(a[1]);
  out[2] = round2(a[2]);
  out[3] = round2(a[3]);
  return out;
}
function scale5(out, a, b) {
  out[0] = a[0] * b;
  out[1] = a[1] * b;
  out[2] = a[2] * b;
  out[3] = a[3] * b;
  return out;
}
function scaleAndAdd3(out, a, b, scale7) {
  out[0] = a[0] + b[0] * scale7;
  out[1] = a[1] + b[1] * scale7;
  out[2] = a[2] + b[2] * scale7;
  out[3] = a[3] + b[3] * scale7;
  return out;
}
function distance3(a, b) {
  const x = b[0] - a[0];
  const y = b[1] - a[1];
  const z = b[2] - a[2];
  const w = b[3] - a[3];
  return Math.sqrt(x * x + y * y + z * z + w * w);
}
function squaredDistance3(a, b) {
  const x = b[0] - a[0];
  const y = b[1] - a[1];
  const z = b[2] - a[2];
  const w = b[3] - a[3];
  return x * x + y * y + z * z + w * w;
}
function length3(a) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  const w = a[3];
  return Math.sqrt(x * x + y * y + z * z + w * w);
}
function squaredLength3(a) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  const w = a[3];
  return x * x + y * y + z * z + w * w;
}
function negate3(out, a) {
  out[0] = -a[0];
  out[1] = -a[1];
  out[2] = -a[2];
  out[3] = -a[3];
  return out;
}
function inverse3(out, a) {
  out[0] = 1 / a[0];
  out[1] = 1 / a[1];
  out[2] = 1 / a[2];
  out[3] = 1 / a[3];
  return out;
}
function normalize3(out, a) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  const w = a[3];
  let len5 = x * x + y * y + z * z + w * w;
  if (len5 > 0) {
    len5 = 1 / Math.sqrt(len5);
  }
  out[0] = x * len5;
  out[1] = y * len5;
  out[2] = z * len5;
  out[3] = w * len5;
  return out;
}
function dot3(a, b) {
  return a[0] * b[0] + a[1] * b[1] + a[2] * b[2] + a[3] * b[3];
}
function cross3(out, u, v, w) {
  const A = v[0] * w[1] - v[1] * w[0];
  const B = v[0] * w[2] - v[2] * w[0];
  const C = v[0] * w[3] - v[3] * w[0];
  const D = v[1] * w[2] - v[2] * w[1];
  const E = v[1] * w[3] - v[3] * w[1];
  const F = v[2] * w[3] - v[3] * w[2];
  const G = u[0];
  const H = u[1];
  const I = u[2];
  const J = u[3];
  out[0] = H * F - I * E + J * D;
  out[1] = -(G * F) + I * C - J * B;
  out[2] = G * E - H * C + J * A;
  out[3] = -(G * D) + H * B - I * A;
  return out;
}
function lerp4(out, a, b, t) {
  const ax = a[0];
  const ay = a[1];
  const az = a[2];
  const aw = a[3];
  out[0] = ax + t * (b[0] - ax);
  out[1] = ay + t * (b[1] - ay);
  out[2] = az + t * (b[2] - az);
  out[3] = aw + t * (b[3] - aw);
  return out;
}
function random3(out, scale7) {
  scale7 = scale7 === void 0 ? 1 : scale7;
  let v1;
  let v2;
  let v3;
  let v4;
  let s1;
  let s2;
  do {
    v1 = RANDOM() * 2 - 1;
    v2 = RANDOM() * 2 - 1;
    s1 = v1 * v1 + v2 * v2;
  } while (s1 >= 1);
  do {
    v3 = RANDOM() * 2 - 1;
    v4 = RANDOM() * 2 - 1;
    s2 = v3 * v3 + v4 * v4;
  } while (s2 >= 1);
  const d = Math.sqrt((1 - s1) / s2);
  out[0] = scale7 * v1;
  out[1] = scale7 * v2;
  out[2] = scale7 * v3 * d;
  out[3] = scale7 * v4 * d;
  return out;
}
function transformMat43(out, a, m) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  const w = a[3];
  out[0] = m[0] * x + m[4] * y + m[8] * z + m[12] * w;
  out[1] = m[1] * x + m[5] * y + m[9] * z + m[13] * w;
  out[2] = m[2] * x + m[6] * y + m[10] * z + m[14] * w;
  out[3] = m[3] * x + m[7] * y + m[11] * z + m[15] * w;
  return out;
}
function transformQuat2(out, a, q) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  const qx = q[0];
  const qy = q[1];
  const qz = q[2];
  const qw = q[3];
  const ix = qw * x + qy * z - qz * y;
  const iy = qw * y + qz * x - qx * z;
  const iz = qw * z + qx * y - qy * x;
  const iw = -qx * x - qy * y - qz * z;
  out[0] = ix * qw + iw * -qx + iy * -qz - iz * -qy;
  out[1] = iy * qw + iw * -qy + iz * -qx - ix * -qz;
  out[2] = iz * qw + iw * -qz + ix * -qy - iy * -qx;
  out[3] = a[3];
  return out;
}
function zero3(out) {
  out[0] = 0;
  out[1] = 0;
  out[2] = 0;
  out[3] = 0;
  return out;
}
function str5(a) {
  return `vec4(${a[0]}, ${a[1]}, ${a[2]}, ${a[3]})`;
}
function exactEquals6(a, b) {
  return a[0] === b[0] && a[1] === b[1] && a[2] === b[2] && a[3] === b[3];
}
function equals6(a, b) {
  const a0 = a[0];
  const a1 = a[1];
  const a2 = a[2];
  const a3 = a[3];
  const b0 = b[0];
  const b1 = b[1];
  const b2 = b[2];
  const b3 = b[3];
  return Math.abs(a0 - b0) <= EPSILON * Math.max(1, Math.abs(a0), Math.abs(b0)) && Math.abs(a1 - b1) <= EPSILON * Math.max(1, Math.abs(a1), Math.abs(b1)) && Math.abs(a2 - b2) <= EPSILON * Math.max(1, Math.abs(a2), Math.abs(b2)) && Math.abs(a3 - b3) <= EPSILON * Math.max(1, Math.abs(a3), Math.abs(b3));
}
var sub5 = subtract5;
var mul5 = multiply5;
var div3 = divide3;
var dist3 = distance3;
var sqrDist3 = squaredDistance3;
var len3 = length3;
var sqrLen3 = squaredLength3;
var forEach3 = function() {
  const vec = create5();
  return function(a, stride, offset, count, fn, arg) {
    let i;
    let l;
    if (!stride) {
      stride = 4;
    }
    if (!offset) {
      offset = 0;
    }
    if (count) {
      l = Math.min(count * stride + offset, a.length);
    } else {
      l = a.length;
    }
    for (i = offset; i < l; i += stride) {
      vec[0] = a[i];
      vec[1] = a[i + 1];
      vec[2] = a[i + 2];
      vec[3] = a[i + 3];
      fn(vec, vec, arg);
      a[i] = vec[0];
      a[i + 1] = vec[1];
      a[i + 2] = vec[2];
      a[i + 3] = vec[3];
    }
    return a;
  };
}();

// dist/classes/matrix4.js
var INDICES2;
(function(INDICES3) {
  INDICES3[INDICES3["COL0ROW0"] = 0] = "COL0ROW0";
  INDICES3[INDICES3["COL0ROW1"] = 1] = "COL0ROW1";
  INDICES3[INDICES3["COL0ROW2"] = 2] = "COL0ROW2";
  INDICES3[INDICES3["COL0ROW3"] = 3] = "COL0ROW3";
  INDICES3[INDICES3["COL1ROW0"] = 4] = "COL1ROW0";
  INDICES3[INDICES3["COL1ROW1"] = 5] = "COL1ROW1";
  INDICES3[INDICES3["COL1ROW2"] = 6] = "COL1ROW2";
  INDICES3[INDICES3["COL1ROW3"] = 7] = "COL1ROW3";
  INDICES3[INDICES3["COL2ROW0"] = 8] = "COL2ROW0";
  INDICES3[INDICES3["COL2ROW1"] = 9] = "COL2ROW1";
  INDICES3[INDICES3["COL2ROW2"] = 10] = "COL2ROW2";
  INDICES3[INDICES3["COL2ROW3"] = 11] = "COL2ROW3";
  INDICES3[INDICES3["COL3ROW0"] = 12] = "COL3ROW0";
  INDICES3[INDICES3["COL3ROW1"] = 13] = "COL3ROW1";
  INDICES3[INDICES3["COL3ROW2"] = 14] = "COL3ROW2";
  INDICES3[INDICES3["COL3ROW3"] = 15] = "COL3ROW3";
})(INDICES2 || (INDICES2 = {}));
var DEFAULT_FOVY = 45 * Math.PI / 180;
var DEFAULT_ASPECT = 1;
var DEFAULT_NEAR = 0.1;
var DEFAULT_FAR = 500;
var IDENTITY_MATRIX2 = Object.freeze([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);
var Matrix4 = class extends Matrix {
  static get IDENTITY() {
    return getIdentityMatrix2();
  }
  static get ZERO() {
    return getZeroMatrix2();
  }
  get ELEMENTS() {
    return 16;
  }
  get RANK() {
    return 4;
  }
  get INDICES() {
    return INDICES2;
  }
  constructor(array) {
    super(-0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0);
    if (arguments.length === 1 && Array.isArray(array)) {
      this.copy(array);
    } else {
      this.identity();
    }
  }
  copy(array) {
    this[0] = array[0];
    this[1] = array[1];
    this[2] = array[2];
    this[3] = array[3];
    this[4] = array[4];
    this[5] = array[5];
    this[6] = array[6];
    this[7] = array[7];
    this[8] = array[8];
    this[9] = array[9];
    this[10] = array[10];
    this[11] = array[11];
    this[12] = array[12];
    this[13] = array[13];
    this[14] = array[14];
    this[15] = array[15];
    return this.check();
  }
  // eslint-disable-next-line max-params
  set(m00, m10, m20, m30, m01, m11, m21, m31, m02, m12, m22, m32, m03, m13, m23, m33) {
    this[0] = m00;
    this[1] = m10;
    this[2] = m20;
    this[3] = m30;
    this[4] = m01;
    this[5] = m11;
    this[6] = m21;
    this[7] = m31;
    this[8] = m02;
    this[9] = m12;
    this[10] = m22;
    this[11] = m32;
    this[12] = m03;
    this[13] = m13;
    this[14] = m23;
    this[15] = m33;
    return this.check();
  }
  // accepts row major order, stores as column major
  // eslint-disable-next-line max-params
  setRowMajor(m00, m01, m02, m03, m10, m11, m12, m13, m20, m21, m22, m23, m30, m31, m32, m33) {
    this[0] = m00;
    this[1] = m10;
    this[2] = m20;
    this[3] = m30;
    this[4] = m01;
    this[5] = m11;
    this[6] = m21;
    this[7] = m31;
    this[8] = m02;
    this[9] = m12;
    this[10] = m22;
    this[11] = m32;
    this[12] = m03;
    this[13] = m13;
    this[14] = m23;
    this[15] = m33;
    return this.check();
  }
  toRowMajor(result) {
    result[0] = this[0];
    result[1] = this[4];
    result[2] = this[8];
    result[3] = this[12];
    result[4] = this[1];
    result[5] = this[5];
    result[6] = this[9];
    result[7] = this[13];
    result[8] = this[2];
    result[9] = this[6];
    result[10] = this[10];
    result[11] = this[14];
    result[12] = this[3];
    result[13] = this[7];
    result[14] = this[11];
    result[15] = this[15];
    return result;
  }
  // Constructors
  /** Set to identity matrix */
  identity() {
    return this.copy(IDENTITY_MATRIX2);
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
  /**
   * Calculates a 4x4 matrix from the given quaternion
   * @param quaternion Quaternion to create matrix from
   * @returns self
   */
  fromQuaternion(quaternion) {
    fromQuat3(this, quaternion);
    return this.check();
  }
  /**
   * Generates a frustum matrix with the given bounds
   * @param view.left - Left bound of the frustum
   * @param view.right - Right bound of the frustum
   * @param view.bottom - Bottom bound of the frustum
   * @param view.top - Top bound of the frustum
   * @param view.near - Near bound of the frustum
   * @param view.far - Far bound of the frustum. Can be set to Infinity.
   * @returns self
   */
  frustum(view) {
    const { left, right, bottom, top, near = DEFAULT_NEAR, far = DEFAULT_FAR } = view;
    if (far === Infinity) {
      computeInfinitePerspectiveOffCenter(this, left, right, bottom, top, near);
    } else {
      frustum(this, left, right, bottom, top, near, far);
    }
    return this.check();
  }
  /**
   * Generates a look-at matrix with the given eye position, focal point,
   * and up axis
   * @param view.eye - (vector) Position of the viewer
   * @param view.center - (vector) Point the viewer is looking at
   * @param view.up - (vector) Up axis
   * @returns self
   */
  lookAt(view) {
    const { eye, center = [0, 0, 0], up = [0, 1, 0] } = view;
    lookAt(this, eye, center, up);
    return this.check();
  }
  /**
   * Generates a orthogonal projection matrix with the given bounds
   * from "traditional" view space parameters
   * @param view.left - Left bound of the frustum
   * @param view.right number  Right bound of the frustum
   * @param view.bottom - Bottom bound of the frustum
   * @param view.top number  Top bound of the frustum
   * @param view.near - Near bound of the frustum
   * @param view.far number  Far bound of the frustum
   * @returns self
   */
  ortho(view) {
    const { left, right, bottom, top, near = DEFAULT_NEAR, far = DEFAULT_FAR } = view;
    ortho(this, left, right, bottom, top, near, far);
    return this.check();
  }
  /**
   * Generates an orthogonal projection matrix with the same parameters
   * as a perspective matrix (plus focalDistance)
   * @param view.fovy Vertical field of view in radians
   * @param view.aspect Aspect ratio. Typically viewport width / viewport height
   * @param view.focalDistance Distance in the view frustum used for extent calculations
   * @param view.near Near bound of the frustum
   * @param view.far Far bound of the frustum
   * @returns self
   */
  orthographic(view) {
    const { fovy = DEFAULT_FOVY, aspect = DEFAULT_ASPECT, focalDistance = 1, near = DEFAULT_NEAR, far = DEFAULT_FAR } = view;
    checkRadians(fovy);
    const halfY = fovy / 2;
    const top = focalDistance * Math.tan(halfY);
    const right = top * aspect;
    return this.ortho({
      left: -right,
      right,
      bottom: -top,
      top,
      near,
      far
    });
  }
  /**
   * Generates a perspective projection matrix with the given bounds
   * @param view.fovy Vertical field of view in radians
   * @param view.aspect Aspect ratio. typically viewport width/height
   * @param view.near Near bound of the frustum
   * @param view.far Far bound of the frustum
   * @returns self
   */
  perspective(view) {
    const { fovy = 45 * Math.PI / 180, aspect = 1, near = 0.1, far = 500 } = view;
    checkRadians(fovy);
    perspective(this, fovy, aspect, near, far);
    return this.check();
  }
  // Accessors
  determinant() {
    return determinant2(this);
  }
  /**
   * Extracts the non-uniform scale assuming the matrix is an affine transformation.
   * The scales are the "lengths" of the column vectors in the upper-left 3x3 matrix.
   * @param result
   * @returns self
   */
  getScale(result = [-0, -0, -0]) {
    result[0] = Math.sqrt(this[0] * this[0] + this[1] * this[1] + this[2] * this[2]);
    result[1] = Math.sqrt(this[4] * this[4] + this[5] * this[5] + this[6] * this[6]);
    result[2] = Math.sqrt(this[8] * this[8] + this[9] * this[9] + this[10] * this[10]);
    return result;
  }
  /**
   * Gets the translation portion, assuming the matrix is a affine transformation matrix.
   * @param result
   * @returns self
   */
  getTranslation(result = [-0, -0, -0]) {
    result[0] = this[12];
    result[1] = this[13];
    result[2] = this[14];
    return result;
  }
  /**
   * Gets upper left 3x3 pure rotation matrix (non-scaling), assume affine transformation matrix
   * @param result
   * @param scaleResult
   * @returns self
   */
  getRotation(result, scaleResult) {
    result = result || [-0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0];
    scaleResult = scaleResult || [-0, -0, -0];
    const scale7 = this.getScale(scaleResult);
    const inverseScale0 = 1 / scale7[0];
    const inverseScale1 = 1 / scale7[1];
    const inverseScale2 = 1 / scale7[2];
    result[0] = this[0] * inverseScale0;
    result[1] = this[1] * inverseScale1;
    result[2] = this[2] * inverseScale2;
    result[3] = 0;
    result[4] = this[4] * inverseScale0;
    result[5] = this[5] * inverseScale1;
    result[6] = this[6] * inverseScale2;
    result[7] = 0;
    result[8] = this[8] * inverseScale0;
    result[9] = this[9] * inverseScale1;
    result[10] = this[10] * inverseScale2;
    result[11] = 0;
    result[12] = 0;
    result[13] = 0;
    result[14] = 0;
    result[15] = 1;
    return result;
  }
  /**
   *
   * @param result
   * @param scaleResult
   * @returns self
   */
  getRotationMatrix3(result, scaleResult) {
    result = result || [-0, -0, -0, -0, -0, -0, -0, -0, -0];
    scaleResult = scaleResult || [-0, -0, -0];
    const scale7 = this.getScale(scaleResult);
    const inverseScale0 = 1 / scale7[0];
    const inverseScale1 = 1 / scale7[1];
    const inverseScale2 = 1 / scale7[2];
    result[0] = this[0] * inverseScale0;
    result[1] = this[1] * inverseScale1;
    result[2] = this[2] * inverseScale2;
    result[3] = this[4] * inverseScale0;
    result[4] = this[5] * inverseScale1;
    result[5] = this[6] * inverseScale2;
    result[6] = this[8] * inverseScale0;
    result[7] = this[9] * inverseScale1;
    result[8] = this[10] * inverseScale2;
    return result;
  }
  // Modifiers
  transpose() {
    transpose2(this, this);
    return this.check();
  }
  invert() {
    invert2(this, this);
    return this.check();
  }
  // Operations
  multiplyLeft(a) {
    multiply4(this, a, this);
    return this.check();
  }
  multiplyRight(a) {
    multiply4(this, this, a);
    return this.check();
  }
  // Rotates a matrix by the given angle around the X axis
  rotateX(radians2) {
    rotateX2(this, this, radians2);
    return this.check();
  }
  // Rotates a matrix by the given angle around the Y axis.
  rotateY(radians2) {
    rotateY2(this, this, radians2);
    return this.check();
  }
  /**
   * Rotates a matrix by the given angle around the Z axis.
   * @param radians
   * @returns self
   */
  rotateZ(radians2) {
    rotateZ2(this, this, radians2);
    return this.check();
  }
  /**
   *
   * @param param0
   * @returns self
   */
  rotateXYZ(angleXYZ) {
    return this.rotateX(angleXYZ[0]).rotateY(angleXYZ[1]).rotateZ(angleXYZ[2]);
  }
  /**
   *
   * @param radians
   * @param axis
   * @returns self
   */
  rotateAxis(radians2, axis) {
    rotate3(this, this, radians2, axis);
    return this.check();
  }
  /**
   *
   * @param factor
   * @returns self
   */
  scale(factor) {
    scale4(this, this, Array.isArray(factor) ? factor : [factor, factor, factor]);
    return this.check();
  }
  /**
   *
   * @param vec
   * @returns self
   */
  translate(vector) {
    translate2(this, this, vector);
    return this.check();
  }
  // Transforms
  /**
   * Transforms any 2, 3 or 4 element vector. 2 and 3 elements are treated as points
   * @param vector
   * @param result
   * @returns self
   */
  transform(vector, result) {
    if (vector.length === 4) {
      result = transformMat43(result || [-0, -0, -0, -0], vector, this);
      checkVector(result, 4);
      return result;
    }
    return this.transformAsPoint(vector, result);
  }
  /**
   * Transforms any 2 or 3 element array as point (w implicitly 1)
   * @param vector
   * @param result
   * @returns self
   */
  transformAsPoint(vector, result) {
    const { length: length5 } = vector;
    let out;
    switch (length5) {
      case 2:
        out = transformMat4(result || [-0, -0], vector, this);
        break;
      case 3:
        out = transformMat42(result || [-0, -0, -0], vector, this);
        break;
      default:
        throw new Error("Illegal vector");
    }
    checkVector(out, vector.length);
    return out;
  }
  /**
   * Transforms any 2 or 3 element array as vector (w implicitly 0)
   * @param vector
   * @param result
   * @returns self
   */
  transformAsVector(vector, result) {
    let out;
    switch (vector.length) {
      case 2:
        out = vec2_transformMat4AsVector(result || [-0, -0], vector, this);
        break;
      case 3:
        out = vec3_transformMat4AsVector(result || [-0, -0, -0], vector, this);
        break;
      default:
        throw new Error("Illegal vector");
    }
    checkVector(out, vector.length);
    return out;
  }
  /** @deprecated */
  transformPoint(vector, result) {
    return this.transformAsPoint(vector, result);
  }
  /** @deprecated */
  transformVector(vector, result) {
    return this.transformAsPoint(vector, result);
  }
  /** @deprecated */
  transformDirection(vector, result) {
    return this.transformAsVector(vector, result);
  }
  // three.js math API compatibility
  makeRotationX(radians2) {
    return this.identity().rotateX(radians2);
  }
  makeTranslation(x, y, z) {
    return this.identity().translate([x, y, z]);
  }
};
var ZERO3;
var IDENTITY;
function getZeroMatrix2() {
  if (!ZERO3) {
    ZERO3 = new Matrix4([0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
    Object.freeze(ZERO3);
  }
  return ZERO3;
}
function getIdentityMatrix2() {
  if (!IDENTITY) {
    IDENTITY = new Matrix4();
    Object.freeze(IDENTITY);
  }
  return IDENTITY;
}
function checkRadians(possiblyDegrees) {
  if (possiblyDegrees > Math.PI * 2) {
    throw Error("expected radians");
  }
}
function computeInfinitePerspectiveOffCenter(result, left, right, bottom, top, near) {
  const column0Row0 = 2 * near / (right - left);
  const column1Row1 = 2 * near / (top - bottom);
  const column2Row0 = (right + left) / (right - left);
  const column2Row1 = (top + bottom) / (top - bottom);
  const column2Row2 = -1;
  const column2Row3 = -1;
  const column3Row2 = -2 * near;
  result[0] = column0Row0;
  result[1] = 0;
  result[2] = 0;
  result[3] = 0;
  result[4] = 0;
  result[5] = column1Row1;
  result[6] = 0;
  result[7] = 0;
  result[8] = column2Row0;
  result[9] = column2Row1;
  result[10] = column2Row2;
  result[11] = column2Row3;
  result[12] = 0;
  result[13] = 0;
  result[14] = column3Row2;
  result[15] = 0;
  return result;
}

// dist/gl-matrix/quat.js
var quat_exports = {};
__export(quat_exports, {
  add: () => add6,
  calculateW: () => calculateW,
  clone: () => clone7,
  conjugate: () => conjugate,
  copy: () => copy6,
  create: () => create6,
  dot: () => dot4,
  equals: () => equals7,
  exactEquals: () => exactEquals7,
  exp: () => exp,
  fromMat3: () => fromMat3,
  fromValues: () => fromValues6,
  getAngle: () => getAngle,
  getAxisAngle: () => getAxisAngle,
  identity: () => identity3,
  invert: () => invert3,
  len: () => len4,
  length: () => length4,
  lerp: () => lerp5,
  ln: () => ln,
  mul: () => mul6,
  multiply: () => multiply6,
  normalize: () => normalize4,
  pow: () => pow,
  rotateX: () => rotateX3,
  rotateY: () => rotateY3,
  rotateZ: () => rotateZ3,
  rotationTo: () => rotationTo,
  scale: () => scale6,
  set: () => set6,
  setAxes: () => setAxes,
  setAxisAngle: () => setAxisAngle,
  slerp: () => slerp2,
  sqlerp: () => sqlerp,
  sqrLen: () => sqrLen4,
  squaredLength: () => squaredLength4,
  str: () => str6
});
function create6() {
  const out = new ARRAY_TYPE(4);
  if (ARRAY_TYPE != Float32Array) {
    out[0] = 0;
    out[1] = 0;
    out[2] = 0;
  }
  out[3] = 1;
  return out;
}
function identity3(out) {
  out[0] = 0;
  out[1] = 0;
  out[2] = 0;
  out[3] = 1;
  return out;
}
function setAxisAngle(out, axis, rad) {
  rad = rad * 0.5;
  const s = Math.sin(rad);
  out[0] = s * axis[0];
  out[1] = s * axis[1];
  out[2] = s * axis[2];
  out[3] = Math.cos(rad);
  return out;
}
function getAxisAngle(out_axis, q) {
  const rad = Math.acos(q[3]) * 2;
  const s = Math.sin(rad / 2);
  if (s > EPSILON) {
    out_axis[0] = q[0] / s;
    out_axis[1] = q[1] / s;
    out_axis[2] = q[2] / s;
  } else {
    out_axis[0] = 1;
    out_axis[1] = 0;
    out_axis[2] = 0;
  }
  return rad;
}
function getAngle(a, b) {
  const dotproduct = dot4(a, b);
  return Math.acos(2 * dotproduct * dotproduct - 1);
}
function multiply6(out, a, b) {
  const ax = a[0];
  const ay = a[1];
  const az = a[2];
  const aw = a[3];
  const bx = b[0];
  const by = b[1];
  const bz = b[2];
  const bw = b[3];
  out[0] = ax * bw + aw * bx + ay * bz - az * by;
  out[1] = ay * bw + aw * by + az * bx - ax * bz;
  out[2] = az * bw + aw * bz + ax * by - ay * bx;
  out[3] = aw * bw - ax * bx - ay * by - az * bz;
  return out;
}
function rotateX3(out, a, rad) {
  rad *= 0.5;
  const ax = a[0];
  const ay = a[1];
  const az = a[2];
  const aw = a[3];
  const bx = Math.sin(rad);
  const bw = Math.cos(rad);
  out[0] = ax * bw + aw * bx;
  out[1] = ay * bw + az * bx;
  out[2] = az * bw - ay * bx;
  out[3] = aw * bw - ax * bx;
  return out;
}
function rotateY3(out, a, rad) {
  rad *= 0.5;
  const ax = a[0];
  const ay = a[1];
  const az = a[2];
  const aw = a[3];
  const by = Math.sin(rad);
  const bw = Math.cos(rad);
  out[0] = ax * bw - az * by;
  out[1] = ay * bw + aw * by;
  out[2] = az * bw + ax * by;
  out[3] = aw * bw - ay * by;
  return out;
}
function rotateZ3(out, a, rad) {
  rad *= 0.5;
  const ax = a[0];
  const ay = a[1];
  const az = a[2];
  const aw = a[3];
  const bz = Math.sin(rad);
  const bw = Math.cos(rad);
  out[0] = ax * bw + ay * bz;
  out[1] = ay * bw - ax * bz;
  out[2] = az * bw + aw * bz;
  out[3] = aw * bw - az * bz;
  return out;
}
function calculateW(out, a) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  out[0] = x;
  out[1] = y;
  out[2] = z;
  out[3] = Math.sqrt(Math.abs(1 - x * x - y * y - z * z));
  return out;
}
function exp(out, a) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  const w = a[3];
  const r = Math.sqrt(x * x + y * y + z * z);
  const et = Math.exp(w);
  const s = r > 0 ? et * Math.sin(r) / r : 0;
  out[0] = x * s;
  out[1] = y * s;
  out[2] = z * s;
  out[3] = et * Math.cos(r);
  return out;
}
function ln(out, a) {
  const x = a[0];
  const y = a[1];
  const z = a[2];
  const w = a[3];
  const r = Math.sqrt(x * x + y * y + z * z);
  const t = r > 0 ? Math.atan2(r, w) / r : 0;
  out[0] = x * t;
  out[1] = y * t;
  out[2] = z * t;
  out[3] = 0.5 * Math.log(x * x + y * y + z * z + w * w);
  return out;
}
function pow(out, a, b) {
  ln(out, a);
  scale6(out, out, b);
  exp(out, out);
  return out;
}
function slerp2(out, a, b, t) {
  const ax = a[0];
  const ay = a[1];
  const az = a[2];
  const aw = a[3];
  let bx = b[0];
  let by = b[1];
  let bz = b[2];
  let bw = b[3];
  let cosom;
  let omega;
  let scale0;
  let scale1;
  let sinom;
  cosom = ax * bx + ay * by + az * bz + aw * bw;
  if (cosom < 0) {
    cosom = -cosom;
    bx = -bx;
    by = -by;
    bz = -bz;
    bw = -bw;
  }
  if (1 - cosom > EPSILON) {
    omega = Math.acos(cosom);
    sinom = Math.sin(omega);
    scale0 = Math.sin((1 - t) * omega) / sinom;
    scale1 = Math.sin(t * omega) / sinom;
  } else {
    scale0 = 1 - t;
    scale1 = t;
  }
  out[0] = scale0 * ax + scale1 * bx;
  out[1] = scale0 * ay + scale1 * by;
  out[2] = scale0 * az + scale1 * bz;
  out[3] = scale0 * aw + scale1 * bw;
  return out;
}
function invert3(out, a) {
  const a0 = a[0];
  const a1 = a[1];
  const a2 = a[2];
  const a3 = a[3];
  const dot5 = a0 * a0 + a1 * a1 + a2 * a2 + a3 * a3;
  const invDot = dot5 ? 1 / dot5 : 0;
  out[0] = -a0 * invDot;
  out[1] = -a1 * invDot;
  out[2] = -a2 * invDot;
  out[3] = a3 * invDot;
  return out;
}
function conjugate(out, a) {
  out[0] = -a[0];
  out[1] = -a[1];
  out[2] = -a[2];
  out[3] = a[3];
  return out;
}
function fromMat3(out, m) {
  const fTrace = m[0] + m[4] + m[8];
  let fRoot;
  if (fTrace > 0) {
    fRoot = Math.sqrt(fTrace + 1);
    out[3] = 0.5 * fRoot;
    fRoot = 0.5 / fRoot;
    out[0] = (m[5] - m[7]) * fRoot;
    out[1] = (m[6] - m[2]) * fRoot;
    out[2] = (m[1] - m[3]) * fRoot;
  } else {
    let i = 0;
    if (m[4] > m[0])
      i = 1;
    if (m[8] > m[i * 3 + i])
      i = 2;
    const j = (i + 1) % 3;
    const k = (i + 2) % 3;
    fRoot = Math.sqrt(m[i * 3 + i] - m[j * 3 + j] - m[k * 3 + k] + 1);
    out[i] = 0.5 * fRoot;
    fRoot = 0.5 / fRoot;
    out[3] = (m[j * 3 + k] - m[k * 3 + j]) * fRoot;
    out[j] = (m[j * 3 + i] + m[i * 3 + j]) * fRoot;
    out[k] = (m[k * 3 + i] + m[i * 3 + k]) * fRoot;
  }
  return out;
}
function str6(a) {
  return `quat(${a[0]}, ${a[1]}, ${a[2]}, ${a[3]})`;
}
var clone7 = clone6;
var fromValues6 = fromValues5;
var copy6 = copy5;
var set6 = set5;
var add6 = add5;
var mul6 = multiply6;
var scale6 = scale5;
var dot4 = dot3;
var lerp5 = lerp4;
var length4 = length3;
var len4 = length4;
var squaredLength4 = squaredLength3;
var sqrLen4 = squaredLength4;
var normalize4 = normalize3;
var exactEquals7 = exactEquals6;
function equals7(a, b) {
  return Math.abs(dot3(a, b)) >= 1 - EPSILON;
}
var rotationTo = function() {
  const tmpvec3 = create2();
  const xUnitVec3 = fromValues2(1, 0, 0);
  const yUnitVec3 = fromValues2(0, 1, 0);
  return function(out, a, b) {
    const dot5 = dot2(a, b);
    if (dot5 < -0.999999) {
      cross2(tmpvec3, xUnitVec3, a);
      if (len2(tmpvec3) < 1e-6)
        cross2(tmpvec3, yUnitVec3, a);
      normalize2(tmpvec3, tmpvec3);
      setAxisAngle(out, tmpvec3, Math.PI);
      return out;
    } else if (dot5 > 0.999999) {
      out[0] = 0;
      out[1] = 0;
      out[2] = 0;
      out[3] = 1;
      return out;
    }
    cross2(tmpvec3, a, b);
    out[0] = tmpvec3[0];
    out[1] = tmpvec3[1];
    out[2] = tmpvec3[2];
    out[3] = 1 + dot5;
    return normalize4(out, out);
  };
}();
var sqlerp = function() {
  const temp1 = create6();
  const temp2 = create6();
  return function(out, a, b, c, d, t) {
    slerp2(temp1, a, d, t);
    slerp2(temp2, b, c, t);
    slerp2(out, temp1, temp2, 2 * t * (1 - t));
    return out;
  };
}();
var setAxes = function() {
  const matr = create3();
  return function(out, view, right, up) {
    matr[0] = right[0];
    matr[3] = right[1];
    matr[6] = right[2];
    matr[1] = up[0];
    matr[4] = up[1];
    matr[7] = up[2];
    matr[2] = -view[0];
    matr[5] = -view[1];
    matr[8] = -view[2];
    return normalize4(out, fromMat3(out, matr));
  };
}();

// dist/classes/quaternion.js
var IDENTITY_QUATERNION = [0, 0, 0, 1];
var Quaternion = class extends MathArray {
  constructor(x = 0, y = 0, z = 0, w = 1) {
    super(-0, -0, -0, -0);
    if (Array.isArray(x) && arguments.length === 1) {
      this.copy(x);
    } else {
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
    fromMat3(this, m);
    return this.check();
  }
  fromAxisRotation(axis, rad) {
    setAxisAngle(this, axis, rad);
    return this.check();
  }
  /** Set a quat to the identity quaternion */
  identity() {
    identity3(this);
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
    return length4(this);
  }
  // Calculates the squared length of a quat
  lengthSquared() {
    return squaredLength4(this);
  }
  // Calculates the dot product of two quat's
  // @return {Number}
  dot(a) {
    return dot4(this, a);
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
    rotationTo(this, vectorA, vectorB);
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
    add6(this, this, a);
    return this.check();
  }
  // Calculates the W component of a quat from the X, Y, and Z components.
  // Any existing W component will be ignored.
  calculateW() {
    calculateW(this, this);
    return this.check();
  }
  // Calculates the conjugate of a quat If the quaternion is normalized,
  // this function is faster than quat_invert and produces the same result.
  conjugate() {
    conjugate(this, this);
    return this.check();
  }
  // Calculates the inverse of a quat
  invert() {
    invert3(this, this);
    return this.check();
  }
  // Performs a linear interpolation between two quat's
  lerp(a, b, t) {
    if (t === void 0) {
      return this.lerp(this, a, b);
    }
    lerp5(this, a, b, t);
    return this.check();
  }
  // Multiplies two quat's
  multiplyRight(a) {
    multiply6(this, this, a);
    return this.check();
  }
  multiplyLeft(a) {
    multiply6(this, a, this);
    return this.check();
  }
  // Normalize a quat
  normalize() {
    const length5 = this.len();
    const l = length5 > 0 ? 1 / length5 : 0;
    this[0] = this[0] * l;
    this[1] = this[1] * l;
    this[2] = this[2] * l;
    this[3] = this[3] * l;
    if (length5 === 0) {
      this[3] = 1;
    }
    return this.check();
  }
  // Rotates a quaternion by the given angle about the X axis
  rotateX(rad) {
    rotateX3(this, this, rad);
    return this.check();
  }
  // Rotates a quaternion by the given angle about the Y axis
  rotateY(rad) {
    rotateY3(this, this, rad);
    return this.check();
  }
  // Rotates a quaternion by the given angle about the Z axis
  rotateZ(rad) {
    rotateZ3(this, this, rad);
    return this.check();
  }
  // Scales a quat by a scalar number
  scale(b) {
    scale6(this, this, b);
    return this.check();
  }
  // Performs a spherical linear interpolation between two quat
  slerp(arg0, arg1, arg2) {
    let start;
    let target;
    let ratio;
    switch (arguments.length) {
      case 1:
        ({
          start = IDENTITY_QUATERNION,
          target,
          ratio
        } = arg0);
        break;
      case 2:
        start = this;
        target = arg0;
        ratio = arg1;
        break;
      default:
        start = arg0;
        target = arg1;
        ratio = arg2;
    }
    slerp2(this, start, target, ratio);
    return this.check();
  }
  transformVector4(vector, result = new Vector4()) {
    transformQuat2(result, vector, this);
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
};

// dist/classes/spherical-coordinates.js
var EPSILON2 = 1e-6;
var EARTH_RADIUS_METERS = 6371e3;
var SphericalCoordinates = class {
  // bearing: number;
  // pitch: number;
  // altitude: number;
  // lnglatZ coordinates
  // longitude: number;
  // latitude: number;
  // lng: number;
  // lat: number;
  // z: number;
  /**
   * Creates a new SphericalCoordinates object
   * @param options
   * @param [options.phi] =0 - rotation around X (latitude)
   * @param [options.theta] =0 - rotation around Y (longitude)
   * @param [options.radius] =1 - Distance from center
   * @param [options.bearing]
   * @param [options.pitch]
   * @param [options.altitude]
   * @param [options.radiusScale] =1
   */
  // eslint-disable-next-line complexity
  constructor({ phi = 0, theta = 0, radius = 1, bearing, pitch, altitude, radiusScale = EARTH_RADIUS_METERS } = {}) {
    this.phi = phi;
    this.theta = theta;
    this.radius = radius || altitude || 1;
    this.radiusScale = radiusScale || 1;
    if (bearing !== void 0) {
      this.bearing = bearing;
    }
    if (pitch !== void 0) {
      this.pitch = pitch;
    }
    this.check();
  }
  toString() {
    return this.formatString(config);
  }
  formatString({ printTypes = false }) {
    const f = formatValue;
    return `${printTypes ? "Spherical" : ""}[rho:${f(this.radius)},theta:${f(this.theta)},phi:${f(this.phi)}]`;
  }
  equals(other) {
    return equals(this.radius, other.radius) && equals(this.theta, other.theta) && equals(this.phi, other.phi);
  }
  exactEquals(other) {
    return this.radius === other.radius && this.theta === other.theta && this.phi === other.phi;
  }
  /* eslint-disable brace-style */
  // Cartographic (bearing 0 north, pitch 0 look from above)
  get bearing() {
    return 180 - degrees(this.phi);
  }
  set bearing(v) {
    this.phi = Math.PI - radians(v);
  }
  get pitch() {
    return degrees(this.theta);
  }
  set pitch(v) {
    this.theta = radians(v);
  }
  // get pitch() { return 90 - degrees(this.phi); }
  // set pitch(v) { this.phi = radians(v) + Math.PI / 2; }
  // get altitude() { return this.radius - 1; } // relative altitude
  // lnglatZ coordinates
  get longitude() {
    return degrees(this.phi);
  }
  get latitude() {
    return degrees(this.theta);
  }
  get lng() {
    return degrees(this.phi);
  }
  get lat() {
    return degrees(this.theta);
  }
  get z() {
    return (this.radius - 1) * this.radiusScale;
  }
  /* eslint-enable brace-style */
  set(radius, phi, theta) {
    this.radius = radius;
    this.phi = phi;
    this.theta = theta;
    return this.check();
  }
  clone() {
    return new SphericalCoordinates().copy(this);
  }
  copy(other) {
    this.radius = other.radius;
    this.phi = other.phi;
    this.theta = other.theta;
    return this.check();
  }
  fromLngLatZ([lng, lat, z]) {
    this.radius = 1 + z / this.radiusScale;
    this.phi = radians(lat);
    this.theta = radians(lng);
    return this.check();
  }
  fromVector3(v) {
    this.radius = length2(v);
    if (this.radius > 0) {
      this.theta = Math.atan2(v[0], v[1]);
      this.phi = Math.acos(clamp(v[2] / this.radius, -1, 1));
    }
    return this.check();
  }
  toVector3() {
    return new Vector3(0, 0, this.radius).rotateX({ radians: this.theta }).rotateZ({ radians: this.phi });
  }
  // restrict phi to be betwee EPS and PI-EPS
  makeSafe() {
    this.phi = Math.max(EPSILON2, Math.min(Math.PI - EPSILON2, this.phi));
    return this;
  }
  check() {
    if (!Number.isFinite(this.phi) || !Number.isFinite(this.theta) || !(this.radius > 0)) {
      throw new Error("SphericalCoordinates: some fields set to invalid numbers");
    }
    return this;
  }
};

// dist/classes/euler.js
var ERR_UNKNOWN_ORDER = "Unknown Euler angle order";
var ALMOST_ONE = 0.99999;
var RotationOrder;
(function(RotationOrder2) {
  RotationOrder2[RotationOrder2["ZYX"] = 0] = "ZYX";
  RotationOrder2[RotationOrder2["YXZ"] = 1] = "YXZ";
  RotationOrder2[RotationOrder2["XZY"] = 2] = "XZY";
  RotationOrder2[RotationOrder2["ZXY"] = 3] = "ZXY";
  RotationOrder2[RotationOrder2["YZX"] = 4] = "YZX";
  RotationOrder2[RotationOrder2["XYZ"] = 5] = "XYZ";
})(RotationOrder || (RotationOrder = {}));
var Euler = class extends MathArray {
  // Constants
  static get ZYX() {
    return RotationOrder.ZYX;
  }
  static get YXZ() {
    return RotationOrder.YXZ;
  }
  static get XZY() {
    return RotationOrder.XZY;
  }
  static get ZXY() {
    return RotationOrder.ZXY;
  }
  static get YZX() {
    return RotationOrder.YZX;
  }
  static get XYZ() {
    return RotationOrder.XYZ;
  }
  static get RollPitchYaw() {
    return RotationOrder.ZYX;
  }
  static get DefaultOrder() {
    return RotationOrder.ZYX;
  }
  static get RotationOrders() {
    return RotationOrder;
  }
  static rotationOrder(order) {
    return RotationOrder[order];
  }
  get ELEMENTS() {
    return 4;
  }
  /**
   * @class
   * @param {Number | Number[]} x
   * @param {Number=} [y]
   * @param {Number=} [z]
   * @param {Number=} [order]
   */
  constructor(x = 0, y = 0, z = 0, order = Euler.DefaultOrder) {
    super(-0, -0, -0, -0);
    if (arguments.length > 0 && Array.isArray(arguments[0])) {
      this.fromVector3(...arguments);
    } else {
      this.set(x, y, z, order);
    }
  }
  fromQuaternion(quaternion) {
    const [x, y, z, w] = quaternion;
    const ysqr = y * y;
    const t0 = -2 * (ysqr + z * z) + 1;
    const t1 = 2 * (x * y + w * z);
    let t2 = -2 * (x * z - w * y);
    const t3 = 2 * (y * z + w * x);
    const t4 = -2 * (x * x + ysqr) + 1;
    t2 = t2 > 1 ? 1 : t2;
    t2 = t2 < -1 ? -1 : t2;
    const roll = Math.atan2(t3, t4);
    const pitch = Math.asin(t2);
    const yaw = Math.atan2(t1, t0);
    return this.set(roll, pitch, yaw, Euler.RollPitchYaw);
  }
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  fromObject(object) {
    throw new Error("not implemented");
  }
  // fromQuaternion(q, order) {
  //   this._fromRotationMat[-0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0];
  //   return this.check();
  // }
  // If copied array does contain fourth element, preserves currently set order
  copy(array) {
    this[0] = array[0];
    this[1] = array[1];
    this[2] = array[2];
    this[3] = Number.isFinite(array[3]) || this.order;
    return this.check();
  }
  // Sets the three angles, and optionally sets the rotation order
  // If order is not specified, preserves currently set order
  set(x = 0, y = 0, z = 0, order) {
    this[0] = x;
    this[1] = y;
    this[2] = z;
    this[3] = Number.isFinite(order) ? order : this[3];
    return this.check();
  }
  validate() {
    return validateOrder(this[3]) && Number.isFinite(this[0]) && Number.isFinite(this[1]) && Number.isFinite(this[2]);
  }
  // Does not copy the orientation element
  toArray(array = [], offset = 0) {
    array[offset] = this[0];
    array[offset + 1] = this[1];
    array[offset + 2] = this[2];
    return array;
  }
  // Copies the orientation element
  toArray4(array = [], offset = 0) {
    array[offset] = this[0];
    array[offset + 1] = this[1];
    array[offset + 2] = this[2];
    array[offset + 3] = this[3];
    return array;
  }
  toVector3(result = [-0, -0, -0]) {
    result[0] = this[0];
    result[1] = this[1];
    result[2] = this[2];
    return result;
  }
  /* eslint-disable no-multi-spaces, brace-style, no-return-assign */
  // x, y, z angle notation (note: only corresponds to axis in XYZ orientation)
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
  // alpha, beta, gamma angle notation
  get alpha() {
    return this[0];
  }
  set alpha(value) {
    this[0] = checkNumber(value);
  }
  get beta() {
    return this[1];
  }
  set beta(value) {
    this[1] = checkNumber(value);
  }
  get gamma() {
    return this[2];
  }
  set gamma(value) {
    this[2] = checkNumber(value);
  }
  // phi, theta, psi angle notation
  get phi() {
    return this[0];
  }
  set phi(value) {
    this[0] = checkNumber(value);
  }
  get theta() {
    return this[1];
  }
  set theta(value) {
    this[1] = checkNumber(value);
  }
  get psi() {
    return this[2];
  }
  set psi(value) {
    this[2] = checkNumber(value);
  }
  // roll, pitch, yaw angle notation
  get roll() {
    return this[0];
  }
  set roll(value) {
    this[0] = checkNumber(value);
  }
  get pitch() {
    return this[1];
  }
  set pitch(value) {
    this[1] = checkNumber(value);
  }
  get yaw() {
    return this[2];
  }
  set yaw(value) {
    this[2] = checkNumber(value);
  }
  // rotation order, in all three angle notations
  get order() {
    return this[3];
  }
  set order(value) {
    this[3] = checkOrder(value);
  }
  // Constructors
  fromVector3(v, order) {
    return this.set(v[0], v[1], v[2], Number.isFinite(order) ? order : this[3]);
  }
  // TODO - with and without 4th element
  fromArray(array, offset = 0) {
    this[0] = array[0 + offset];
    this[1] = array[1 + offset];
    this[2] = array[2 + offset];
    if (array[3] !== void 0) {
      this[3] = array[3];
    }
    return this.check();
  }
  // Common ZYX rotation order
  fromRollPitchYaw(roll, pitch, yaw) {
    return this.set(roll, pitch, yaw, RotationOrder.ZYX);
  }
  fromRotationMatrix(m, order = Euler.DefaultOrder) {
    this._fromRotationMatrix(m, order);
    return this.check();
  }
  // ACCESSORS
  getRotationMatrix(m) {
    return this._getRotationMatrix(m);
  }
  // TODO - move to Quaternion
  getQuaternion() {
    const q = new Quaternion();
    switch (this[3]) {
      case RotationOrder.XYZ:
        return q.rotateX(this[0]).rotateY(this[1]).rotateZ(this[2]);
      case RotationOrder.YXZ:
        return q.rotateY(this[0]).rotateX(this[1]).rotateZ(this[2]);
      case RotationOrder.ZXY:
        return q.rotateZ(this[0]).rotateX(this[1]).rotateY(this[2]);
      case RotationOrder.ZYX:
        return q.rotateZ(this[0]).rotateY(this[1]).rotateX(this[2]);
      case RotationOrder.YZX:
        return q.rotateY(this[0]).rotateZ(this[1]).rotateX(this[2]);
      case RotationOrder.XZY:
        return q.rotateX(this[0]).rotateZ(this[1]).rotateY(this[2]);
      default:
        throw new Error(ERR_UNKNOWN_ORDER);
    }
  }
  // INTERNAL METHODS
  // Conversion from Euler to rotation matrix and from matrix to Euler
  // Adapted from three.js under MIT license
  // // WARNING: this discards revolution information -bhouston
  // reorder(newOrder) {
  //   const q = new Quaternion().setFromEuler(this);
  //   return this.setFromQuaternion(q, newOrder);
  /* eslint-disable complexity, max-statements, one-var */
  _fromRotationMatrix(m, order = Euler.DefaultOrder) {
    const m11 = m[0], m12 = m[4], m13 = m[8];
    const m21 = m[1], m22 = m[5], m23 = m[9];
    const m31 = m[2], m32 = m[6], m33 = m[10];
    order = order || this[3];
    switch (order) {
      case Euler.XYZ:
        this[1] = Math.asin(clamp(m13, -1, 1));
        if (Math.abs(m13) < ALMOST_ONE) {
          this[0] = Math.atan2(-m23, m33);
          this[2] = Math.atan2(-m12, m11);
        } else {
          this[0] = Math.atan2(m32, m22);
          this[2] = 0;
        }
        break;
      case Euler.YXZ:
        this[0] = Math.asin(-clamp(m23, -1, 1));
        if (Math.abs(m23) < ALMOST_ONE) {
          this[1] = Math.atan2(m13, m33);
          this[2] = Math.atan2(m21, m22);
        } else {
          this[1] = Math.atan2(-m31, m11);
          this[2] = 0;
        }
        break;
      case Euler.ZXY:
        this[0] = Math.asin(clamp(m32, -1, 1));
        if (Math.abs(m32) < ALMOST_ONE) {
          this[1] = Math.atan2(-m31, m33);
          this[2] = Math.atan2(-m12, m22);
        } else {
          this[1] = 0;
          this[2] = Math.atan2(m21, m11);
        }
        break;
      case Euler.ZYX:
        this[1] = Math.asin(-clamp(m31, -1, 1));
        if (Math.abs(m31) < ALMOST_ONE) {
          this[0] = Math.atan2(m32, m33);
          this[2] = Math.atan2(m21, m11);
        } else {
          this[0] = 0;
          this[2] = Math.atan2(-m12, m22);
        }
        break;
      case Euler.YZX:
        this[2] = Math.asin(clamp(m21, -1, 1));
        if (Math.abs(m21) < ALMOST_ONE) {
          this[0] = Math.atan2(-m23, m22);
          this[1] = Math.atan2(-m31, m11);
        } else {
          this[0] = 0;
          this[1] = Math.atan2(m13, m33);
        }
        break;
      case Euler.XZY:
        this[2] = Math.asin(-clamp(m12, -1, 1));
        if (Math.abs(m12) < ALMOST_ONE) {
          this[0] = Math.atan2(m32, m22);
          this[1] = Math.atan2(m13, m11);
        } else {
          this[0] = Math.atan2(-m23, m33);
          this[1] = 0;
        }
        break;
      default:
        throw new Error(ERR_UNKNOWN_ORDER);
    }
    this[3] = order;
    return this;
  }
  _getRotationMatrix(result) {
    const te = result || [-0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0, -0];
    const x = this.x, y = this.y, z = this.z;
    const a = Math.cos(x);
    const c = Math.cos(y);
    const e = Math.cos(z);
    const b = Math.sin(x);
    const d = Math.sin(y);
    const f = Math.sin(z);
    switch (this[3]) {
      case Euler.XYZ: {
        const ae = a * e, af = a * f, be = b * e, bf = b * f;
        te[0] = c * e;
        te[4] = -c * f;
        te[8] = d;
        te[1] = af + be * d;
        te[5] = ae - bf * d;
        te[9] = -b * c;
        te[2] = bf - ae * d;
        te[6] = be + af * d;
        te[10] = a * c;
        break;
      }
      case Euler.YXZ: {
        const ce = c * e, cf = c * f, de = d * e, df = d * f;
        te[0] = ce + df * b;
        te[4] = de * b - cf;
        te[8] = a * d;
        te[1] = a * f;
        te[5] = a * e;
        te[9] = -b;
        te[2] = cf * b - de;
        te[6] = df + ce * b;
        te[10] = a * c;
        break;
      }
      case Euler.ZXY: {
        const ce = c * e, cf = c * f, de = d * e, df = d * f;
        te[0] = ce - df * b;
        te[4] = -a * f;
        te[8] = de + cf * b;
        te[1] = cf + de * b;
        te[5] = a * e;
        te[9] = df - ce * b;
        te[2] = -a * d;
        te[6] = b;
        te[10] = a * c;
        break;
      }
      case Euler.ZYX: {
        const ae = a * e, af = a * f, be = b * e, bf = b * f;
        te[0] = c * e;
        te[4] = be * d - af;
        te[8] = ae * d + bf;
        te[1] = c * f;
        te[5] = bf * d + ae;
        te[9] = af * d - be;
        te[2] = -d;
        te[6] = b * c;
        te[10] = a * c;
        break;
      }
      case Euler.YZX: {
        const ac = a * c, ad = a * d, bc = b * c, bd = b * d;
        te[0] = c * e;
        te[4] = bd - ac * f;
        te[8] = bc * f + ad;
        te[1] = f;
        te[5] = a * e;
        te[9] = -b * e;
        te[2] = -d * e;
        te[6] = ad * f + bc;
        te[10] = ac - bd * f;
        break;
      }
      case Euler.XZY: {
        const ac = a * c, ad = a * d, bc = b * c, bd = b * d;
        te[0] = c * e;
        te[4] = -f;
        te[8] = d * e;
        te[1] = ac * f + bd;
        te[5] = a * e;
        te[9] = ad * f - bc;
        te[2] = bc * f - ad;
        te[6] = b * e;
        te[10] = bd * f + ac;
        break;
      }
      default:
        throw new Error(ERR_UNKNOWN_ORDER);
    }
    te[3] = 0;
    te[7] = 0;
    te[11] = 0;
    te[12] = 0;
    te[13] = 0;
    te[14] = 0;
    te[15] = 1;
    return te;
  }
  toQuaternion() {
    const cy = Math.cos(this.yaw * 0.5);
    const sy = Math.sin(this.yaw * 0.5);
    const cr = Math.cos(this.roll * 0.5);
    const sr = Math.sin(this.roll * 0.5);
    const cp = Math.cos(this.pitch * 0.5);
    const sp = Math.sin(this.pitch * 0.5);
    const w = cy * cr * cp + sy * sr * sp;
    const x = cy * sr * cp - sy * cr * sp;
    const y = cy * cr * sp + sy * sr * cp;
    const z = sy * cr * cp - cy * sr * sp;
    return new Quaternion(x, y, z, w);
  }
};
function validateOrder(value) {
  return value >= 0 && value < 6;
}
function checkOrder(value) {
  if (value < 0 && value >= 6) {
    throw new Error(ERR_UNKNOWN_ORDER);
  }
  return value;
}

// dist/classes/pose.js
var Pose = class {
  constructor({ x = 0, y = 0, z = 0, roll = 0, pitch = 0, yaw = 0, position, orientation } = {}) {
    if (Array.isArray(position) && position.length === 3) {
      this.position = new Vector3(position);
    } else {
      this.position = new Vector3(x, y, z);
    }
    if (Array.isArray(orientation) && orientation.length === 4) {
      this.orientation = new Euler(orientation, orientation[3]);
    } else {
      this.orientation = new Euler(roll, pitch, yaw, Euler.RollPitchYaw);
    }
  }
  get x() {
    return this.position.x;
  }
  set x(value) {
    this.position.x = value;
  }
  get y() {
    return this.position.y;
  }
  set y(value) {
    this.position.y = value;
  }
  get z() {
    return this.position.z;
  }
  set z(value) {
    this.position.z = value;
  }
  get roll() {
    return this.orientation.roll;
  }
  set roll(value) {
    this.orientation.roll = value;
  }
  get pitch() {
    return this.orientation.pitch;
  }
  set pitch(value) {
    this.orientation.pitch = value;
  }
  get yaw() {
    return this.orientation.yaw;
  }
  set yaw(value) {
    this.orientation.yaw = value;
  }
  getPosition() {
    return this.position;
  }
  getOrientation() {
    return this.orientation;
  }
  equals(pose) {
    if (!pose) {
      return false;
    }
    return this.position.equals(pose.position) && this.orientation.equals(pose.orientation);
  }
  exactEquals(pose) {
    if (!pose) {
      return false;
    }
    return this.position.exactEquals(pose.position) && this.orientation.exactEquals(pose.orientation);
  }
  getTransformationMatrix() {
    const sr = Math.sin(this.roll);
    const sp = Math.sin(this.pitch);
    const sw = Math.sin(this.yaw);
    const cr = Math.cos(this.roll);
    const cp = Math.cos(this.pitch);
    const cw = Math.cos(this.yaw);
    return new Matrix4().setRowMajor(
      cw * cp,
      // 0,0
      -sw * cr + cw * sp * sr,
      // 0,1
      sw * sr + cw * sp * cr,
      // 0,2
      this.x,
      // 0,3
      sw * cp,
      // 1,0
      cw * cr + sw * sp * sr,
      // 1,1
      -cw * sr + sw * sp * cr,
      // 1,2
      this.y,
      // 1,3
      -sp,
      // 2,0
      cp * sr,
      // 2,1
      cp * cr,
      // 2,2
      this.z,
      // 2,3
      0,
      0,
      0,
      1
    );
  }
  getTransformationMatrixFromPose(pose) {
    return new Matrix4().multiplyRight(this.getTransformationMatrix()).multiplyRight(pose.getTransformationMatrix().invert());
  }
  getTransformationMatrixToPose(pose) {
    return new Matrix4().multiplyRight(pose.getTransformationMatrix()).multiplyRight(this.getTransformationMatrix().invert());
  }
};

// dist/lib/math-utils.js
var math_utils_exports = {};
__export(math_utils_exports, {
  EPSILON1: () => EPSILON1,
  EPSILON10: () => EPSILON10,
  EPSILON11: () => EPSILON11,
  EPSILON12: () => EPSILON12,
  EPSILON13: () => EPSILON13,
  EPSILON14: () => EPSILON14,
  EPSILON15: () => EPSILON15,
  EPSILON16: () => EPSILON16,
  EPSILON17: () => EPSILON17,
  EPSILON18: () => EPSILON18,
  EPSILON19: () => EPSILON19,
  EPSILON2: () => EPSILON22,
  EPSILON20: () => EPSILON20,
  EPSILON3: () => EPSILON3,
  EPSILON4: () => EPSILON4,
  EPSILON5: () => EPSILON5,
  EPSILON6: () => EPSILON6,
  EPSILON7: () => EPSILON7,
  EPSILON8: () => EPSILON8,
  EPSILON9: () => EPSILON9,
  PI_OVER_FOUR: () => PI_OVER_FOUR,
  PI_OVER_SIX: () => PI_OVER_SIX,
  PI_OVER_TWO: () => PI_OVER_TWO,
  TWO_PI: () => TWO_PI
});
var EPSILON1 = 0.1;
var EPSILON22 = 0.01;
var EPSILON3 = 1e-3;
var EPSILON4 = 1e-4;
var EPSILON5 = 1e-5;
var EPSILON6 = 1e-6;
var EPSILON7 = 1e-7;
var EPSILON8 = 1e-8;
var EPSILON9 = 1e-9;
var EPSILON10 = 1e-10;
var EPSILON11 = 1e-11;
var EPSILON12 = 1e-12;
var EPSILON13 = 1e-13;
var EPSILON14 = 1e-14;
var EPSILON15 = 1e-15;
var EPSILON16 = 1e-16;
var EPSILON17 = 1e-17;
var EPSILON18 = 1e-18;
var EPSILON19 = 1e-19;
var EPSILON20 = 1e-20;
var PI_OVER_TWO = Math.PI / 2;
var PI_OVER_FOUR = Math.PI / 4;
var PI_OVER_SIX = Math.PI / 6;
var TWO_PI = Math.PI * 2;
//# sourceMappingURL=index.cjs.map
