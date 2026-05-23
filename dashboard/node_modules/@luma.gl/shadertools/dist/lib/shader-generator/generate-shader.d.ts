import { ShaderModule } from "../shader-module/shader-module.js";
import type { GLSLGenerationOptions } from "./glsl/generate-glsl.js";
import type { WGSLGenerationOptions } from "./wgsl/generate-wgsl.js";
/** Options for how to generate shader code from a module */
export type ShaderGenerationOptions = GLSLGenerationOptions | WGSLGenerationOptions;
/** Generates shader code for a module */
export declare function generateShaderForModule(module: ShaderModule<Record<string, unknown>>, options: ShaderGenerationOptions): string;
//# sourceMappingURL=generate-shader.d.ts.map