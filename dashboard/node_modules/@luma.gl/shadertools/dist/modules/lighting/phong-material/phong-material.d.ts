import { NumberArray3 } from '@math.gl/types';
import { ShaderModule } from "../../../lib/shader-module/shader-module.js";
export type PhongMaterialProps = {
    ambient?: number;
    diffuse?: number;
    /** Specularity exponent */
    shininess?: number;
    specularColor?: NumberArray3;
};
/** In Phong shading, the normal vector is linearly interpolated across the surface of the polygon from the polygon's vertex normals. */
export declare const phongMaterial: ShaderModule<PhongMaterialProps>;
//# sourceMappingURL=phong-material.d.ts.map