/**
 * Transpiles GLSL 3.00 shader source code to target GLSL version (3.00 or 1.00)
 *
 * @note We always run transpiler even if same version e.g. 3.00 => 3.00
 * @note For texture sampling transpilation, apps need to use non-standard texture* calls in GLSL 3.00 source
 * RFC: https://github.com/visgl/luma.gl/blob/7.0-release/dev-docs/RFCs/v6.0/portable-glsl-300-rfc.md
 */
export declare function transpileGLSLShader(source: string, stage: 'vertex' | 'fragment'): string;
//# sourceMappingURL=transpile-glsl-shader.d.ts.map