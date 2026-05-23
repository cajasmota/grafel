import type { PrimitiveDataType } from "../shadertypes/data-types/data-types.js";
import type { VariableShaderType } from "../shadertypes/data-types/shader-types.js";
import type { UniformValue } from "../adapter/types/uniforms.js";
/**
 * Std140 layout for uniform buffers
 * Supports manual listing of uniforms
 */
export declare class UniformBufferLayout {
    readonly layout: Record<string, {
        offset: number;
        size: number;
        type: PrimitiveDataType;
    }>;
    /** number of bytes needed for buffer allocation */
    readonly byteLength: number;
    /** Create a new UniformBufferLayout given a map of attributes. */
    constructor(uniformTypes: Record<string, VariableShaderType>, uniformSizes?: Record<string, number>);
    /** Get the data for the complete buffer */
    getData(uniformValues: Record<string, UniformValue>): Uint8Array;
    /** Does this layout have a field with specified name */
    has(name: string): boolean;
    /** Get offset and size for a field with specified name */
    get(name: string): {
        offset: number;
        size: number;
    } | undefined;
}
//# sourceMappingURL=uniform-buffer-layout.d.ts.map