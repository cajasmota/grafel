import type { NumberArray3 } from '@math.gl/core';
/** Shader type field for lights */
export declare enum LIGHT_TYPE {
    POINT = 0,
    DIRECTIONAL = 1
}
/** Lighting helper types */
export type Light = AmbientLight | PointLight | DirectionalLight;
export type AmbientLight = {
    type: 'ambient';
    color?: Readonly<NumberArray3>;
    intensity?: number;
};
export type PointLight = {
    type: 'point';
    position: Readonly<NumberArray3>;
    color?: Readonly<NumberArray3>;
    intensity?: number;
    attenuation?: Readonly<NumberArray3>;
};
export type DirectionalLight = {
    type: 'directional';
    direction: Readonly<NumberArray3>;
    color?: Readonly<NumberArray3>;
    intensity?: number;
};
export type LightingProps = {
    enabled?: boolean;
    lights?: Light[];
    /** @deprecated */
    ambientLight?: AmbientLight;
    /** @deprecated */
    pointLights?: PointLight[];
    /** @deprecated */
    directionalLights?: DirectionalLight[];
};
export type LightingUniforms = {
    enabled: number;
    ambientColor: Readonly<NumberArray3>;
    directionalLightCount: number;
    pointLightCount: number;
    lightType: number;
    lightColor0: Readonly<NumberArray3>;
    lightPosition0: Readonly<NumberArray3>;
    lightDirection0: Readonly<NumberArray3>;
    lightAttenuation0: Readonly<NumberArray3>;
    lightColor1: Readonly<NumberArray3>;
    lightPosition1: Readonly<NumberArray3>;
    lightDirection1: Readonly<NumberArray3>;
    lightAttenuation1: Readonly<NumberArray3>;
    lightColor2: Readonly<NumberArray3>;
    lightPosition2: Readonly<NumberArray3>;
    lightDirection2: Readonly<NumberArray3>;
    lightAttenuation2: Readonly<NumberArray3>;
};
/** UBO ready lighting module */
export declare const lighting: {
    readonly props: LightingProps;
    readonly uniforms: LightingUniforms;
    readonly name: "lighting";
    readonly defines: {};
    readonly uniformTypes: {
        readonly enabled: "i32";
        readonly lightType: "i32";
        readonly directionalLightCount: "i32";
        readonly pointLightCount: "i32";
        readonly ambientColor: "vec3<f32>";
        readonly lightColor0: "vec3<f32>";
        readonly lightPosition0: "vec3<f32>";
        readonly lightDirection0: "vec3<f32>";
        readonly lightAttenuation0: "vec3<f32>";
        readonly lightColor1: "vec3<f32>";
        readonly lightPosition1: "vec3<f32>";
        readonly lightDirection1: "vec3<f32>";
        readonly lightAttenuation1: "vec3<f32>";
        readonly lightColor2: "vec3<f32>";
        readonly lightPosition2: "vec3<f32>";
        readonly lightDirection2: "vec3<f32>";
        readonly lightAttenuation2: "vec3<f32>";
    };
    readonly defaultUniforms: {
        readonly enabled: 1;
        readonly lightType: LIGHT_TYPE.POINT;
        readonly directionalLightCount: 0;
        readonly pointLightCount: 0;
        readonly ambientColor: readonly [0.1, 0.1, 0.1];
        readonly lightColor0: readonly [1, 1, 1];
        readonly lightPosition0: readonly [1, 1, 2];
        readonly lightDirection0: readonly [1, 1, 1];
        readonly lightAttenuation0: readonly [1, 0, 0];
        readonly lightColor1: readonly [1, 1, 1];
        readonly lightPosition1: readonly [1, 1, 2];
        readonly lightDirection1: readonly [1, 1, 1];
        readonly lightAttenuation1: readonly [1, 0, 0];
        readonly lightColor2: readonly [1, 1, 1];
        readonly lightPosition2: readonly [1, 1, 2];
        readonly lightDirection2: readonly [1, 1, 1];
        readonly lightAttenuation2: readonly [1, 0, 0];
    };
    readonly source: "// #if (defined(SHADER_TYPE_FRAGMENT) && defined(LIGHTING_FRAGMENT)) || (defined(SHADER_TYPE_VERTEX) && defined(LIGHTING_VERTEX))\nstruct AmbientLight {\n  color: vec3<f32>,\n};\n\nstruct PointLight {\n  color: vec3<f32>,\n  position: vec3<f32>,\n  attenuation: vec3<f32>, // 2nd order x:Constant-y:Linear-z:Exponential\n};\n\nstruct DirectionalLight {\n  color: vec3<f32>,\n  direction: vec3<f32>,\n};\n\nstruct lightingUniforms {\n  enabled: i32,\n  pointLightCount: i32,\n  directionalLightCount: i32,\n\n  ambientColor: vec3<f32>,\n\n  // TODO - support multiple lights by uncommenting arrays below\n  lightType: i32,\n  lightColor: vec3<f32>,\n  lightDirection: vec3<f32>,\n  lightPosition: vec3<f32>,\n  lightAttenuation: vec3<f32>,\n\n  // AmbientLight ambientLight;\n  // PointLight pointLight[MAX_LIGHTS];\n  // DirectionalLight directionalLight[MAX_LIGHTS];\n};\n\n// Binding 0:1 is reserved for lighting (Note: could go into separate bind group as it is stable across draw calls)\n@binding(1) @group(0) var<uniform> lighting : lightingUniforms;\n\nfn lighting_getPointLight(index: i32) -> PointLight {\n  return PointLight(lighting.lightColor, lighting.lightPosition, lighting.lightAttenuation);\n}\n\nfn lighting_getDirectionalLight(index: i32) -> DirectionalLight {\n  return DirectionalLight(lighting.lightColor, lighting.lightDirection);\n} \n\nfn getPointLightAttenuation(pointLight: PointLight, distance: f32) -> f32 {\n  return pointLight.attenuation.x\n       + pointLight.attenuation.y * distance\n       + pointLight.attenuation.z * distance * distance;\n}\n";
    readonly vs: "precision highp int;\n\n// #if (defined(SHADER_TYPE_FRAGMENT) && defined(LIGHTING_FRAGMENT)) || (defined(SHADER_TYPE_VERTEX) && defined(LIGHTING_VERTEX))\nstruct AmbientLight {\n  vec3 color;\n};\n\nstruct PointLight {\n  vec3 color;\n  vec3 position;\n  vec3 attenuation; // 2nd order x:Constant-y:Linear-z:Exponential\n};\n\nstruct DirectionalLight {\n  vec3 color;\n  vec3 direction;\n};\n\nuniform lightingUniforms {\n  int enabled;\n  int lightType;\n\n  int directionalLightCount;\n  int pointLightCount;\n\n  vec3 ambientColor;\n\n  vec3 lightColor0;\n  vec3 lightPosition0;\n  vec3 lightDirection0;\n  vec3 lightAttenuation0;\n\n  vec3 lightColor1;\n  vec3 lightPosition1;\n  vec3 lightDirection1;\n  vec3 lightAttenuation1;\n\n  vec3 lightColor2;\n  vec3 lightPosition2;\n  vec3 lightDirection2;\n  vec3 lightAttenuation2;\n} lighting;\n\nPointLight lighting_getPointLight(int index) {\n  switch (index) {\n    case 0:\n      return PointLight(lighting.lightColor0, lighting.lightPosition0, lighting.lightAttenuation0);\n    case 1:\n      return PointLight(lighting.lightColor1, lighting.lightPosition1, lighting.lightAttenuation1);\n    case 2:\n    default:  \n      return PointLight(lighting.lightColor2, lighting.lightPosition2, lighting.lightAttenuation2);\n  }\n}\n\nDirectionalLight lighting_getDirectionalLight(int index) {\n  switch (index) {\n    case 0:\n      return DirectionalLight(lighting.lightColor0, lighting.lightDirection0);\n    case 1:\n      return DirectionalLight(lighting.lightColor1, lighting.lightDirection1);\n    case 2:\n    default:   \n      return DirectionalLight(lighting.lightColor2, lighting.lightDirection2);\n  }\n} \n\nfloat getPointLightAttenuation(PointLight pointLight, float distance) {\n  return pointLight.attenuation.x\n       + pointLight.attenuation.y * distance\n       + pointLight.attenuation.z * distance * distance;\n}\n\n// #endif\n";
    readonly fs: "precision highp int;\n\n// #if (defined(SHADER_TYPE_FRAGMENT) && defined(LIGHTING_FRAGMENT)) || (defined(SHADER_TYPE_VERTEX) && defined(LIGHTING_VERTEX))\nstruct AmbientLight {\n  vec3 color;\n};\n\nstruct PointLight {\n  vec3 color;\n  vec3 position;\n  vec3 attenuation; // 2nd order x:Constant-y:Linear-z:Exponential\n};\n\nstruct DirectionalLight {\n  vec3 color;\n  vec3 direction;\n};\n\nuniform lightingUniforms {\n  int enabled;\n  int lightType;\n\n  int directionalLightCount;\n  int pointLightCount;\n\n  vec3 ambientColor;\n\n  vec3 lightColor0;\n  vec3 lightPosition0;\n  vec3 lightDirection0;\n  vec3 lightAttenuation0;\n\n  vec3 lightColor1;\n  vec3 lightPosition1;\n  vec3 lightDirection1;\n  vec3 lightAttenuation1;\n\n  vec3 lightColor2;\n  vec3 lightPosition2;\n  vec3 lightDirection2;\n  vec3 lightAttenuation2;\n} lighting;\n\nPointLight lighting_getPointLight(int index) {\n  switch (index) {\n    case 0:\n      return PointLight(lighting.lightColor0, lighting.lightPosition0, lighting.lightAttenuation0);\n    case 1:\n      return PointLight(lighting.lightColor1, lighting.lightPosition1, lighting.lightAttenuation1);\n    case 2:\n    default:  \n      return PointLight(lighting.lightColor2, lighting.lightPosition2, lighting.lightAttenuation2);\n  }\n}\n\nDirectionalLight lighting_getDirectionalLight(int index) {\n  switch (index) {\n    case 0:\n      return DirectionalLight(lighting.lightColor0, lighting.lightDirection0);\n    case 1:\n      return DirectionalLight(lighting.lightColor1, lighting.lightDirection1);\n    case 2:\n    default:   \n      return DirectionalLight(lighting.lightColor2, lighting.lightDirection2);\n  }\n} \n\nfloat getPointLightAttenuation(PointLight pointLight, float distance) {\n  return pointLight.attenuation.x\n       + pointLight.attenuation.y * distance\n       + pointLight.attenuation.z * distance * distance;\n}\n\n// #endif\n";
    readonly getUniforms: typeof getUniforms;
};
declare function getUniforms(props?: LightingProps, prevUniforms?: Partial<LightingUniforms>): LightingUniforms;
export {};
//# sourceMappingURL=lighting.d.ts.map