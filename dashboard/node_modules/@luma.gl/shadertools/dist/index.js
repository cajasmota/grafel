// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
export { initializeShaderModule, initializeShaderModules } from "./lib/shader-module/shader-module.js";
export { getShaderModuleUniforms } from "./lib/shader-module/shader-module.js";
export { getShaderModuleDependencies } from "./lib/shader-module/shader-module-dependencies.js";
export { checkShaderModuleDeprecations } from "./lib/shader-module/shader-module.js";
export { getShaderModuleSource } from "./lib/shader-assembly/assemble-shaders.js";
export { resolveModules as _resolveModules } from "./lib/shader-module/shader-module-dependencies.js";
export { getDependencyGraph as _getDependencyGraph } from "./lib/shader-module/shader-module-dependencies.js";
// ShaderAssembler
export { ShaderAssembler } from "./lib/shader-assembler.js";
// SHADER HELPERS
// Shader source introspection
export { getShaderInfo } from "./lib/glsl-utils/get-shader-info.js";
export { getQualifierDetails, getPassthroughFS, typeToChannelSuffix, typeToChannelCount, convertToVec4 } from "./lib/glsl-utils/shader-utils.js";
export { generateShaderForModule } from "./lib/shader-generator/generate-shader.js";
export { capitalize } from "./lib/shader-generator/utils/capitalize.js";
// TEST EXPORTS - Do not use in production applications
export { preprocess } from "./lib/preprocessor/preprocessor.js";
export { assembleGLSLShaderPair } from "./lib/shader-assembly/assemble-shaders.js";
export { combineInjects } from "./lib/shader-assembly/shader-injections.js";
// EXPERIMENTAL WGSL
export { getShaderLayoutFromWGSL } from "./lib/wgsl/get-shader-layout-wgsl.js";
// data utils
export { toHalfFloat, fromHalfFloat } from "./modules/math/fp16/fp16-utils.js";
export { fp64ify, fp64LowPart, fp64ifyMatrix4 } from "./modules/math/fp64/fp64-utils.js";
// math libraries
export { random } from "./modules/math/random/random.js";
export { fp32 } from "./modules/math/fp32/fp32.js";
export { fp64, fp64arithmetic } from "./modules/math/fp64/fp64.js";
export { picking } from "./modules/engine/picking/picking.js";
export { lighting } from "./modules/lighting/lights/lighting.js";
export { dirlight } from "./modules/lighting/no-material/dirlight.js";
export { gouraudMaterial } from "./modules/lighting/gouraud-material/gouraud-material.js";
export { phongMaterial } from "./modules/lighting/phong-material/phong-material.js";
export { pbrMaterial } from "./modules/lighting/pbr-material/pbr-material.js";
//# sourceMappingURL=index.js.map