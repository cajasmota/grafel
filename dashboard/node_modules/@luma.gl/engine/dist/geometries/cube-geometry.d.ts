import { Geometry } from "../geometry/geometry.js";
export type CubeGeometryProps = {
    id?: string;
    indices?: boolean;
    attributes?: any;
};
export declare class CubeGeometry extends Geometry {
    constructor(props?: CubeGeometryProps);
}
export declare const CUBE_NON_INDEXED_POSITIONS: Float32Array<ArrayBuffer>;
export declare const CUBE_NON_INDEXED_TEX_COORDS: Float32Array<ArrayBuffer>;
export declare const CUBE_NON_INDEXED_COLORS: Float32Array<ArrayBuffer>;
//# sourceMappingURL=cube-geometry.d.ts.map