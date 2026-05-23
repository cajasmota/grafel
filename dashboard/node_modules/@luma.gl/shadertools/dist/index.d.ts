/**
 * Marks GLSL shaders for syntax highlighting: glsl`...`
 * Install https://marketplace.visualstudio.com/items?itemName=boyswan.glsl-literal
 */
export type { PlatformInfo } from "./lib/shader-assembly/platform-info.js";
export type { ShaderModule } from "./lib/shader-module/shader-module.js";
export type { ShaderPass } from "./lib/shader-module/shader-pass.js";
export type { UniformTypes } from "./lib/utils/uniform-types.js";
export { initializeShaderModule, initializeShaderModules } from "./lib/shader-module/shader-module.js";
export { getShaderModuleUniforms } from "./lib/shader-module/shader-module.js";
export { getShaderModuleDependencies } from "./lib/shader-module/shader-module-dependencies.js";
export { checkShaderModuleDeprecations } from "./lib/shader-module/shader-module.js";
export { getShaderModuleSource } from "./lib/shader-assembly/assemble-shaders.js";
export { resolveModules as _resolveModules } from "./lib/shader-module/shader-module-dependencies.js";
export { getDependencyGraph as _getDependencyGraph } from "./lib/shader-module/shader-module-dependencies.js";
export { ShaderAssembler } from "./lib/shader-assembler.js";
export type { ShaderHook } from "./lib/shader-assembly/shader-hooks.js";
export type { ShaderInjection } from "./lib/shader-assembly/shader-injections.js";
export { getShaderInfo } from "./lib/glsl-utils/get-shader-info.js";
export { getQualifierDetails, getPassthroughFS, typeToChannelSuffix, typeToChannelCount, convertToVec4 } from "./lib/glsl-utils/shader-utils.js";
export type { ShaderGenerationOptions } from "./lib/shader-generator/generate-shader.js";
export { generateShaderForModule } from "./lib/shader-generator/generate-shader.js";
export { capitalize } from "./lib/shader-generator/utils/capitalize.js";
export { preprocess } from "./lib/preprocessor/preprocessor.js";
export { assembleGLSLShaderPair } from "./lib/shader-assembly/assemble-shaders.js";
export { combineInjects } from "./lib/shader-assembly/shader-injections.js";
export { getShaderLayoutFromWGSL } from "./lib/wgsl/get-shader-layout-wgsl.js";
export { toHalfFloat, fromHalfFloat } from "./modules/math/fp16/fp16-utils.js";
export { fp64ify, fp64LowPart, fp64ifyMatrix4 } from "./modules/math/fp64/fp64-utils.js";
export { random } from "./modules/math/random/random.js";
export { fp32 } from "./modules/math/fp32/fp32.js";
export { fp64, fp64arithmetic } from "./modules/math/fp64/fp64.js";
export type { PickingProps, PickingUniforms } from "./modules/engine/picking/picking.js";
export { picking } from "./modules/engine/picking/picking.js";
export type { LightingProps, LightingUniforms } from "./modules/lighting/lights/lighting.js";
export { lighting } from "./modules/lighting/lights/lighting.js";
export { dirlight } from "./modules/lighting/no-material/dirlight.js";
export type { GouraudMaterialProps } from "./modules/lighting/gouraud-material/gouraud-material.js";
export { gouraudMaterial } from "./modules/lighting/gouraud-material/gouraud-material.js";
export type { PhongMaterialProps } from "./modules/lighting/phong-material/phong-material.js";
export { phongMaterial } from "./modules/lighting/phong-material/phong-material.js";
export type { PBRMaterialBindings, PBRMaterialProps, PBRMaterialUniforms } from "./modules/lighting/pbr-material/pbr-material.js";
export type { PBRProjectionProps } from "./modules/lighting/pbr-material/pbr-projection.js";
export { pbrMaterial } from "./modules/lighting/pbr-material/pbr-material.js";
//# sourceMappingURL=index.d.ts.map