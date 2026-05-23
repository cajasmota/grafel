// math.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
/** Checks if a `Bounds` is a `Bounds2D` */
export function isBounds2D(bounds) {
    return bounds.length === 2;
}
/** Accepts 2D and 3D bounds and returns a 2D bound, truncating 3D dimension if necessary */
export function getBounds2D(bounds) {
    return isBounds2D(bounds)
        ? bounds
        : [
            [bounds[0][0], bounds[0][1]],
            [bounds[1][0], bounds[1][1]]
        ];
}
//# sourceMappingURL=bounds-types.js.map