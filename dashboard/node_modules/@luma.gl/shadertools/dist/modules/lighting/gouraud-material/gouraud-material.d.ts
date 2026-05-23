import { ShaderModule } from "../../../lib/shader-module/shader-module.js";
export type GouraudMaterialProps = {
    ambient?: number;
    diffuse?: number;
    /** Specularity exponent */
    shininess?: number;
    specularColor?: [number, number, number];
};
/** In Gouraud shading, color is calculated for each triangle vertex normal, and then color is interpolated colors across the triangle */
export declare const gouraudMaterial: ShaderModule<GouraudMaterialProps>;
//# sourceMappingURL=gouraud-material.d.ts.map