import type { Binding, UniformValue } from '@luma.gl/core';
import { ShaderModule } from '@luma.gl/shadertools';
export type ShaderInputsOptions = {
    disableWarnings?: boolean;
};
/**
 * ShaderInputs holds uniform and binding values for one or more shader modules,
 * - It can generate binary data for any uniform buffer
 * - It can manage a uniform buffer for each block
 * - It can update managed uniform buffers with a single call
 * - It performs some book keeping on what has changed to minimize unnecessary writes to uniform buffers.
 */
export declare class ShaderInputs<ShaderPropsT extends Partial<Record<string, Record<string, unknown>>> = Partial<Record<string, Record<string, unknown>>>> {
    options: Required<ShaderInputsOptions>;
    /**
     * The map of modules
     * @todo should should this include the resolved dependencies?
     */
    modules: Readonly<{
        [P in keyof ShaderPropsT]: ShaderModule<ShaderPropsT[P]>;
    }>;
    /** Stores the uniform values for each module */
    moduleUniforms: Record<keyof ShaderPropsT, Record<string, UniformValue>>;
    /** Stores the uniform bindings for each module  */
    moduleBindings: Record<keyof ShaderPropsT, Record<string, Binding>>;
    /** Tracks if uniforms have changed */
    /**
     * Create a new UniformStore instance
     * @param modules
     */
    constructor(modules: {
        [P in keyof ShaderPropsT]?: ShaderModule<ShaderPropsT[P], any>;
    }, options?: ShaderInputsOptions);
    /** Destroy */
    destroy(): void;
    /**
     * Set module props
     */
    setProps(props: Partial<{
        [P in keyof ShaderPropsT]?: Partial<ShaderPropsT[P]>;
    }>): void;
    /**
     * Return the map of modules
     * @todo should should this include the resolved dependencies?
     */
    getModules(): ShaderModule[];
    /** Get all uniform values for all modules */
    getUniformValues(): Partial<Record<keyof ShaderPropsT, Record<string, UniformValue>>>;
    /** Merges all bindings for the shader (from the various modules) */
    getBindingValues(): Record<string, Binding>;
    /** Return a debug table that can be used for console.table() or log.table() */
    getDebugTable(): Record<string, Record<string, unknown>>;
    _addModule(module: ShaderModule): void;
}
//# sourceMappingURL=shader-inputs.d.ts.map