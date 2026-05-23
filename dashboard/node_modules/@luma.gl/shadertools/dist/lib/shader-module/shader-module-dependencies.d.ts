import { ShaderModule } from "./shader-module.js";
type AbstractModule = {
    name: string;
    dependencies?: AbstractModule[];
};
/**
 * Takes a list of shader module names and returns a new list of
 * shader module names that includes all dependencies, sorted so
 * that modules that are dependencies of other modules come first.
 *
 * If the shader glsl code from the returned modules is concatenated
 * in the reverse order, it is guaranteed that all functions be resolved and
 * that all function and variable definitions come before use.
 *
 * @param modules - Array of modules (inline modules or module names)
 * @return - Array of modules
 */
export declare function getShaderModuleDependencies<T extends AbstractModule>(modules: T[]): T[];
/**
 * Recursively checks module dependencies to calculate dependency level of each module.
 *
 * @param options.modules - Array of modules
 * @param options.level - Current level
 * @param options.moduleMap -
 * @param options.moduleDepth - Current level
 * @return - Map of module name to its level
 */
export declare function getDependencyGraph<T extends AbstractModule>(options: {
    modules: T[];
    level: number;
    moduleMap: Record<string, T>;
    moduleDepth: Record<string, number>;
}): void;
/**
 * Takes a list of shader module names and returns a new list of
 * shader module names that includes all dependencies, sorted so
 * that modules that are dependencies of other modules come first.
 *
 * If the shader glsl code from the returned modules is concatenated
 * in the reverse order, it is guaranteed that all functions be resolved and
 * that all function and variable definitions come before use.
 *
 * @param modules - Array of modules (inline modules or module names)
 * @return - Array of modules
 */
export declare function getShaderDependencies(modules: ShaderModule[]): ShaderModule[];
/**
 * Instantiate shader modules and resolve any dependencies
 * @deprecated Use getShaderDpendencies
 */
export declare function resolveModules(modules: ShaderModule[]): ShaderModule[];
export {};
//# sourceMappingURL=shader-module-dependencies.d.ts.map