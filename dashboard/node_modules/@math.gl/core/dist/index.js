// math.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
// classes
export { Vector2 } from "./classes/vector2.js";
export { Vector3 } from "./classes/vector3.js";
export { Vector4 } from "./classes/vector4.js";
export { Matrix3 } from "./classes/matrix3.js";
export { Matrix4 } from "./classes/matrix4.js";
export { Quaternion } from "./classes/quaternion.js";
// experimental
export { SphericalCoordinates } from "./classes/spherical-coordinates.js";
export { Pose } from "./classes/pose.js";
export { Euler } from "./classes/euler.js";
export * as _MathUtils from "./lib/math-utils.js";
// lib
export { assert } from "./lib/assert.js";
export { 
// math.gl global utility methods
config, configure, formatValue, isArray, clone, equals, exactEquals, toRadians, toDegrees, 
// math.gl "GLSL"-style functions
radians, degrees, sin, cos, tan, asin, acos, atan, clamp, lerp, withEpsilon } from "./lib/common.js";
// DEPRECATED
export { SphericalCoordinates as _SphericalCoordinates } from "./classes/spherical-coordinates.js";
export { Pose as _Pose } from "./classes/pose.js";
export { Euler as _Euler } from "./classes/euler.js";
/** @deprecated Use Matrix3 */
export * as mat3 from "./gl-matrix/mat3.js";
/** @deprecated Use Matrix4 */
export * as mat4 from "./gl-matrix/mat4.js";
/** @deprecated Use Quaterinion */
export * as quat from "./gl-matrix/quat.js";
/** @deprecated UseVector */
export * as vec2 from "./gl-matrix/vec2.js";
/** @deprecated Use Vector3 */
export * as vec3 from "./gl-matrix/vec3.js";
/** @deprecated Use Vector4 */
export * as vec4 from "./gl-matrix/vec4.js";
//# sourceMappingURL=index.js.map