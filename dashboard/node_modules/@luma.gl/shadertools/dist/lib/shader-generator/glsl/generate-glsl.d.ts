import { ShaderModule } from "../../../lib/shader-module/shader-module.js";
export type GLSLGenerationOptions = {
    shaderLanguage: 'glsl';
    uniforms: 'scoped-interface-blocks' | 'unscoped-interface-blocks' | 'uniforms';
};
export declare function generateGLSLForModule(module: ShaderModule, options: GLSLGenerationOptions): string;
//# sourceMappingURL=generate-glsl.d.ts.map