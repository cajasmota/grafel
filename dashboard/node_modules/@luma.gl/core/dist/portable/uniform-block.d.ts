import type { VariableShaderType } from "../shadertypes/data-types/shader-types.js";
import type { UniformValue } from "../adapter/types/uniforms.js";
import { ShaderLayout, UniformInfo } from "../adapter/types/shader-layout.js";
/**
 * A uniform block holds values of the of uniform values for one uniform block / buffer.
 * It also does some book keeping on what has changed, to minimize unnecessary writes to uniform buffers.
 */
export declare class UniformBlock<TUniforms extends Record<string, UniformValue> = Record<string, UniformValue>> {
    name: string;
    uniforms: Record<keyof TUniforms, UniformValue>;
    modifiedUniforms: Record<keyof TUniforms, boolean>;
    modified: boolean;
    readonly bindingLayout: Record<string, UniformInfo>;
    needsRedraw: string | false;
    constructor(props?: {
        name?: string;
        shaderLayout?: ShaderLayout;
        uniformTypes?: Record<keyof TUniforms, Record<string, VariableShaderType>>;
    });
    /** Set a map of uniforms */
    setUniforms(uniforms: Partial<TUniforms>): void;
    setNeedsRedraw(reason: string): void;
    /** Returns all uniforms */
    getAllUniforms(): Record<string, UniformValue>;
    /** Set a single uniform */
    private _setUniform;
}
//# sourceMappingURL=uniform-block.d.ts.map