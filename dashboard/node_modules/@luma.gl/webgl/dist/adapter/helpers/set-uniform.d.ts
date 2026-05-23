import type { UniformValue } from '@luma.gl/core';
import { GLUniformType, GLSamplerType } from '@luma.gl/constants';
/** Set a raw uniform (without type conversion and caching) */
export declare function setUniform(gl: WebGL2RenderingContext, location: WebGLUniformLocation, type: GLUniformType | GLSamplerType, value: UniformValue): void;
//# sourceMappingURL=set-uniform.d.ts.map