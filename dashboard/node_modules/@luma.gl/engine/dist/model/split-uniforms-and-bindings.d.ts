import type { UniformValue, Binding } from '@luma.gl/core';
export declare function isUniformValue(value: unknown): value is UniformValue;
type UniformsAndBindings = {
    bindings: Record<string, Binding>;
    uniforms: Record<string, UniformValue>;
};
export declare function splitUniformsAndBindings(uniforms: Record<string, Binding | UniformValue>): UniformsAndBindings;
export {};
//# sourceMappingURL=split-uniforms-and-bindings.d.ts.map