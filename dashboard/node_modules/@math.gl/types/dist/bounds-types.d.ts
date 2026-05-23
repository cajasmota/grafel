/** 2 dimensional bounds [[minX, minY], [maxX, maxY]] */
export type Bounds2D = [[number, number], [number, number]];
/** 3 dimensional bounds [[minX, minY, minZ], [maxX, maxY, maxZ]] */
export type Bounds3D = [[number, number, number], [number, number, number]];
/** 2 or 3 dimensional bounds [[minX, minY], [maxX, maxY]] or [[minX, minY, minZ], [maxX, maxY, maxZ]] */
export type Bounds = [[number, number], [number, number]] | [[number, number, number], [number, number, number]];
/** Checks if a `Bounds` is a `Bounds2D` */
export declare function isBounds2D(bounds: Bounds): bounds is Bounds2D;
/** Accepts 2D and 3D bounds and returns a 2D bound, truncating 3D dimension if necessary */
export declare function getBounds2D(bounds: Bounds): Bounds2D;
//# sourceMappingURL=bounds-types.d.ts.map