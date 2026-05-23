import { ShaderModule } from "../../../lib/shader-module/shader-module.js";
export type WGSLGenerationOptions = {
    shaderLanguage: 'wgsl';
};
export declare function generateWGSLForModule(module: ShaderModule, options: WGSLGenerationOptions): string;
export declare function generateWGSLUniformDeclarations(module: ShaderModule, options: WGSLGenerationOptions): string;
//# sourceMappingURL=generate-wgsl.d.ts.map