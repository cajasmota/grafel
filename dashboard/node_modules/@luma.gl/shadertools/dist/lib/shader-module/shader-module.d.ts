import { UniformFormat } from "../../types.js";
import { PropType, PropValidator } from "../filters/prop-types.js";
import type { UniformSizes, UniformTypes, UniformValue } from "../utils/uniform-types.js";
import { ShaderInjection } from "../shader-assembly/shader-injections.js";
type Binding = unknown;
export type UniformInfo = {
    format?: UniformFormat;
} & PropType;
type BindingKeys<T> = {
    [K in keyof T]: T[K] extends UniformValue ? never : K;
}[keyof T];
type UniformKeys<T> = {
    [K in keyof T]: T[K] extends UniformValue ? K : never;
}[keyof T];
export type PickBindings<T> = {
    [K in BindingKeys<Required<T>>]: T[K];
};
export type PickUniforms<T> = {
    [K in UniformKeys<Required<T>>]: T[K];
};
/**
 * A shader module definition object
 *
 * @note Needs to be initialized with `initializeShaderModules`
 * @note `UniformsT` & `BindingsT` are deduced from `PropsT` by default. If
 * a custom type for `UniformsT` is used, `BindingsT` should be also be provided.
 */
export type ShaderModule<PropsT extends Record<string, any> = Record<string, any>, UniformsT extends Record<string, UniformValue> = PickUniforms<PropsT>, BindingsT extends Record<string, Binding> = PickBindings<PropsT>> = {
    /** Used for type inference not for values */
    props?: PropsT;
    /** Used for type inference, not currently used for values */
    uniforms?: UniformsT;
    /** Used for type inference, not currently used for values */
    bindings?: BindingsT;
    name: string;
    /** WGSL code */
    source?: string;
    /** GLSL fragment shader code */
    fs?: string;
    /** GLSL vertex shader code */
    vs?: string;
    /** Uniform shader types @note: Both order and types MUST match uniform block declarations in shader */
    uniformTypes?: Required<UniformTypes<UniformsT>>;
    /** Uniform shader array sizes (default 1) */
    uniformSizes?: Required<UniformSizes<UniformsT>>;
    /** Uniform JS prop types  */
    propTypes?: Record<keyof UniformsT, UniformInfo>;
    /** Default uniform values */
    defaultUniforms?: Required<UniformsT>;
    /** Function that maps props to uniforms & bindings */
    getUniforms?: (props: Partial<PropsT>, prevUniforms?: UniformsT) => Partial<UniformsT & BindingsT>;
    defines?: Record<string, boolean>;
    /** Injections */
    inject?: Record<string, string | {
        injection: string;
        order: number;
    }>;
    dependencies?: ShaderModule<any, any>[];
    /** Information on deprecated properties */
    deprecations?: ShaderModuleDeprecation[];
    /** The instance field contains information that is generated at run-time */
    instance?: {
        propValidators?: Record<string, PropValidator>;
        parsedDeprecations: ShaderModuleDeprecation[];
        normalizedInjections: {
            vertex: Record<string, ShaderInjection>;
            fragment: Record<string, ShaderInjection>;
        };
    };
};
/** Use to generate deprecations when shader module is used */
export type ShaderModuleDeprecation = {
    type: string;
    regex?: RegExp;
    new: string;
    old: string;
    deprecated?: boolean;
};
export declare function initializeShaderModules(modules: ShaderModule[]): void;
export declare function initializeShaderModule(module: ShaderModule): void;
/** Convert module props to uniforms */
export declare function getShaderModuleUniforms<ShaderModuleT extends ShaderModule<Record<string, unknown>, Record<string, UniformValue>>>(module: ShaderModuleT, props?: ShaderModuleT['props'], oldUniforms?: ShaderModuleT['uniforms']): Record<string, Binding | UniformValue>;
export declare function checkShaderModuleDeprecations(shaderModule: ShaderModule, shaderSource: string, log: any): void;
export {};
//# sourceMappingURL=shader-module.d.ts.map