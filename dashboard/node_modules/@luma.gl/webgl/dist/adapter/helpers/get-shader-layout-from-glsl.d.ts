import type { ShaderLayout } from '@luma.gl/core';
/**
 * Extract metadata describing binding information for a program's shaders
 * Note: `linkProgram()` needs to have been called
 * (although linking does not need to have been successful).
 */
export declare function getShaderLayoutFromGLSL(gl: WebGL2RenderingContext, program: WebGLProgram): ShaderLayout;
//# sourceMappingURL=get-shader-layout-from-glsl.d.ts.map