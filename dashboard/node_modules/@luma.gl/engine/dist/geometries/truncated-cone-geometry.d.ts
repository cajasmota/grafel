import { Geometry } from "../geometry/geometry.js";
export type TruncatedConeGeometryProps = {
    topRadius?: number;
    bottomRadius?: number;
    topCap?: boolean;
    bottomCap?: boolean;
    height?: number;
    nradial?: number;
    nvertical?: number;
    verticalAxis?: 'x' | 'y' | 'z';
};
/**
 * Primitives inspired by TDL http://code.google.com/p/webglsamples/,
 * copyright 2011 Google Inc. new BSD License
 * (http://www.opensource.org/licenses/bsd-license.php).
 */
export declare class TruncatedConeGeometry extends Geometry {
    constructor(props?: TruncatedConeGeometryProps & {
        id?: string;
        attributes?: any;
    });
}
//# sourceMappingURL=truncated-cone-geometry.d.ts.map