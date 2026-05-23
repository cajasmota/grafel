import { TruncatedConeGeometry } from "./truncated-cone-geometry.js";
export type ConeGeometryProps = {
    id?: string;
    radius?: number;
    cap?: boolean;
};
export declare class ConeGeometry extends TruncatedConeGeometry {
    constructor(props?: ConeGeometryProps);
}
//# sourceMappingURL=cone-geometry.d.ts.map