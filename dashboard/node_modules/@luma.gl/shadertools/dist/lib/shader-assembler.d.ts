import type { ShaderModule } from "./shader-module/shader-module.js";
import { AssembleShaderProps, GetUniformsFunc } from "./shader-assembly/assemble-shaders.js";
/**
 * A stateful version of `assembleShaders` that can be used to assemble shaders.
 * Supports setting of default modules and hooks.
 */
export declare class ShaderAssembler {
    /** Default ShaderAssembler instance */
    static defaultShaderAssembler: ShaderAssembler;
    /** Hook functions */
    private readonly _hookFunctions;
    /** Shader modules */
    private _defaultModules;
    /**
     * A default shader assembler instance - the natural place to register default modules and hooks
     * @returns
     */
    static getDefaultShaderAssembler(): ShaderAssembler;
    /**
     * Add a default module that does not have to be provided with every call to assembleShaders()
     */
    addDefaultModule(module: ShaderModule): void;
    /**
     * Remove a default module
     */
    removeDefaultModule(module: ShaderModule): void;
    /**
     * Register a shader hook
     * @param hook
     * @param opts
     */
    addShaderHook(hook: string, opts?: any): void;
    /**
     * Assemble a WGSL unified shader
     * @param platformInfo
     * @param props
     * @returns
     */
    assembleWGSLShader(props: AssembleShaderProps): {
        source: string;
        getUniforms: GetUniformsFunc;
        modules: ShaderModule[];
    };
    /**
     * Assemble a pair of shaders into a single shader program
     * @param platformInfo
     * @param props
     * @returns
     */
    assembleGLSLShaderPair(props: AssembleShaderProps): {
        vs: string;
        fs: string;
        getUniforms: GetUniformsFunc;
        modules: ShaderModule[];
    };
    /**
     * Dedupe and combine with default modules
     */
    _getModuleList(appModules?: ShaderModule[]): ShaderModule[];
}
//# sourceMappingURL=shader-assembler.d.ts.map