import { Geometry } from "../geometry/geometry.js";
export type SphereGeometryProps = {
    id?: string;
    radius?: number;
    nlat?: number;
    nlong?: number;
    attributes?: any;
};
export declare class SphereGeometry extends Geometry {
    constructor(props?: SphereGeometryProps);
}
//# sourceMappingURL=sphere-geometry.d.ts.map