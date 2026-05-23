import { ShaderPass } from '@luma.gl/shadertools';
/**
 * Gets fragment shader source for a shader pass sub pass
 * @param options
 * @returns
 */
export declare function getFragmentShaderForRenderPass(options: {
    shaderPass: ShaderPass;
    action: 'filter' | 'sample';
    shadingLanguage: 'wgsl' | 'glsl';
}): string;
//# sourceMappingURL=get-fragment-shader.d.ts.map