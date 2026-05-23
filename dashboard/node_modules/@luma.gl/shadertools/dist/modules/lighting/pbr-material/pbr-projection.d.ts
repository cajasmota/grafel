import type { NumberArray3, NumberArray16 } from '@math.gl/core';
import { ShaderModule } from "../../../lib/shader-module/shader-module.js";
export type PBRProjectionProps = {
    modelViewProjectionMatrix: NumberArray16;
    modelMatrix: NumberArray16;
    normalMatrix: NumberArray16;
    camera: NumberArray3;
};
export declare const pbrProjection: ShaderModule<PBRProjectionProps>;
//# sourceMappingURL=pbr-projection.d.ts.map