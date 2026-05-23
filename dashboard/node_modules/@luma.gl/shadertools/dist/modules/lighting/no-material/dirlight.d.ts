import type { NumberArray3 } from '@math.gl/core';
export type DirlightProps = {
    lightDirection?: NumberArray3;
};
export type DirlightUniforms = DirlightProps;
export declare const SOURCE_WGSL = "  \nstruct dirlightUniforms {\n  lightDirection: vec3<f32>,\n};\n\nalias DirlightNormal = vec3<f32>;\n\nstruct DirlightInputs {\n  normal: DirlightNormal,\n};\n\n@binding(1) @group(0) var<uniform> dirlight : dirlightUniforms;\n\n// For vertex\nfn dirlight_setNormal(normal: vec3<f32>) -> DirlightNormal {\n  return normalize(normal);\n}\n\n// Returns color attenuated by angle from light source\nfn dirlight_filterColor(color: vec4<f32>, inputs: DirlightInputs) -> vec4<f32> {\n  // TODO - fix default light direction\n  // let lightDirection = dirlight.lightDirection;\n  let lightDirection = vec3<f32>(1, 1, 1);\n  let d: f32 = abs(dot(inputs.normal, normalize(lightDirection)));\n  return vec4<f32>(color.rgb * d, color.a);\n}\n";
/**
 * Cheap lighting - single directional light, single dot product, one uniform
 */
export declare const dirlight: {
    readonly props: DirlightProps;
    readonly uniforms: DirlightUniforms;
    readonly name: "dirlight";
    readonly dependencies: [];
    readonly source: "  \nstruct dirlightUniforms {\n  lightDirection: vec3<f32>,\n};\n\nalias DirlightNormal = vec3<f32>;\n\nstruct DirlightInputs {\n  normal: DirlightNormal,\n};\n\n@binding(1) @group(0) var<uniform> dirlight : dirlightUniforms;\n\n// For vertex\nfn dirlight_setNormal(normal: vec3<f32>) -> DirlightNormal {\n  return normalize(normal);\n}\n\n// Returns color attenuated by angle from light source\nfn dirlight_filterColor(color: vec4<f32>, inputs: DirlightInputs) -> vec4<f32> {\n  // TODO - fix default light direction\n  // let lightDirection = dirlight.lightDirection;\n  let lightDirection = vec3<f32>(1, 1, 1);\n  let d: f32 = abs(dot(inputs.normal, normalize(lightDirection)));\n  return vec4<f32>(color.rgb * d, color.a);\n}\n";
    readonly vs: "out vec3 dirlight_vNormal;\n\nvoid dirlight_setNormal(vec3 normal) {\n  dirlight_vNormal = normalize(normal);\n}\n";
    readonly fs: "uniform dirlightUniforms {\n  vec3 lightDirection;\n} dirlight;\n\nin vec3 dirlight_vNormal;\n\n// Returns color attenuated by angle from light source\nvec4 dirlight_filterColor(vec4 color) {\n  float d = abs(dot(dirlight_vNormal, normalize(dirlight.lightDirection)));\n  return vec4(color.rgb * d, color.a);\n}\n";
    readonly uniformTypes: {
        readonly lightDirection: "vec3<f32>";
    };
    readonly defaultUniforms: {
        readonly lightDirection: [1, 1, 2];
    };
    readonly getUniforms: typeof getUniforms;
};
declare function getUniforms(opts?: DirlightProps): DirlightUniforms;
export {};
//# sourceMappingURL=dirlight.d.ts.map