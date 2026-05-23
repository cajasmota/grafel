"use strict";
var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __defNormalProp = (obj, key, value) => key in obj ? __defProp(obj, key, { enumerable: true, configurable: true, writable: true, value }) : obj[key] = value;
var __export = (target, all) => {
  for (var name in all)
    __defProp(target, name, { get: all[name], enumerable: true });
};
var __copyProps = (to, from, except, desc) => {
  if (from && typeof from === "object" || typeof from === "function") {
    for (let key of __getOwnPropNames(from))
      if (!__hasOwnProp.call(to, key) && key !== except)
        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
  }
  return to;
};
var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);
var __publicField = (obj, key, value) => {
  __defNormalProp(obj, typeof key !== "symbol" ? key + "" : key, value);
  return value;
};

// dist/index.js
var dist_exports = {};
__export(dist_exports, {
  ShaderAssembler: () => ShaderAssembler,
  _getDependencyGraph: () => getDependencyGraph,
  _resolveModules: () => resolveModules,
  assembleGLSLShaderPair: () => assembleGLSLShaderPair,
  capitalize: () => capitalize,
  checkShaderModuleDeprecations: () => checkShaderModuleDeprecations,
  combineInjects: () => combineInjects,
  convertToVec4: () => convertToVec4,
  dirlight: () => dirlight,
  fp32: () => fp32,
  fp64: () => fp64,
  fp64LowPart: () => fp64LowPart,
  fp64arithmetic: () => fp64arithmetic,
  fp64ify: () => fp64ify,
  fp64ifyMatrix4: () => fp64ifyMatrix4,
  fromHalfFloat: () => fromHalfFloat,
  generateShaderForModule: () => generateShaderForModule,
  getPassthroughFS: () => getPassthroughFS,
  getQualifierDetails: () => getQualifierDetails,
  getShaderInfo: () => getShaderInfo,
  getShaderLayoutFromWGSL: () => getShaderLayoutFromWGSL,
  getShaderModuleDependencies: () => getShaderModuleDependencies,
  getShaderModuleSource: () => getShaderModuleSource,
  getShaderModuleUniforms: () => getShaderModuleUniforms,
  gouraudMaterial: () => gouraudMaterial,
  initializeShaderModule: () => initializeShaderModule,
  initializeShaderModules: () => initializeShaderModules,
  lighting: () => lighting,
  pbrMaterial: () => pbrMaterial,
  phongMaterial: () => phongMaterial,
  picking: () => picking,
  preprocess: () => preprocess,
  random: () => random,
  toHalfFloat: () => toHalfFloat,
  typeToChannelCount: () => typeToChannelCount,
  typeToChannelSuffix: () => typeToChannelSuffix
});
module.exports = __toCommonJS(dist_exports);

// dist/lib/utils/assert.js
function assert(condition, message) {
  if (!condition) {
    throw new Error(message || "shadertools: assertion failed.");
  }
}

// dist/lib/filters/prop-types.js
var DEFAULT_PROP_VALIDATORS = {
  number: {
    type: "number",
    validate(value, propType) {
      return Number.isFinite(value) && typeof propType === "object" && (propType.max === void 0 || value <= propType.max) && (propType.min === void 0 || value >= propType.min);
    }
  },
  array: {
    type: "array",
    validate(value, propType) {
      return Array.isArray(value) || ArrayBuffer.isView(value);
    }
  }
};
function makePropValidators(propTypes) {
  const propValidators = {};
  for (const [name, propType] of Object.entries(propTypes)) {
    propValidators[name] = makePropValidator(propType);
  }
  return propValidators;
}
function getValidatedProperties(properties, propValidators, errorMessage) {
  const validated = {};
  for (const [key, propsValidator] of Object.entries(propValidators)) {
    if (properties && key in properties && !propsValidator.private) {
      if (propsValidator.validate) {
        assert(propsValidator.validate(properties[key], propsValidator), `${errorMessage}: invalid ${key}`);
      }
      validated[key] = properties[key];
    } else {
      validated[key] = propsValidator.value;
    }
  }
  return validated;
}
function makePropValidator(propType) {
  let type = getTypeOf(propType);
  if (type !== "object") {
    return { value: propType, ...DEFAULT_PROP_VALIDATORS[type], type };
  }
  if (typeof propType === "object") {
    if (!propType) {
      return { type: "object", value: null };
    }
    if (propType.type !== void 0) {
      return { ...propType, ...DEFAULT_PROP_VALIDATORS[propType.type], type: propType.type };
    }
    if (propType.value === void 0) {
      return { type: "object", value: propType };
    }
    type = getTypeOf(propType.value);
    return { ...propType, ...DEFAULT_PROP_VALIDATORS[type], type };
  }
  throw new Error("props");
}
function getTypeOf(value) {
  if (Array.isArray(value) || ArrayBuffer.isView(value)) {
    return "array";
  }
  return typeof value;
}

// dist/module-injectors.js
var MODULE_INJECTORS_VS = (
  /* glsl */
  `#ifdef MODULE_LOGDEPTH
  logdepth_adjustPosition(gl_Position);
#endif
`
);
var MODULE_INJECTORS_FS = (
  /* glsl */
  `#ifdef MODULE_MATERIAL
  fragColor = material_filterColor(fragColor);
#endif

#ifdef MODULE_LIGHTING
  fragColor = lighting_filterColor(fragColor);
#endif

#ifdef MODULE_FOG
  fragColor = fog_filterColor(fragColor);
#endif

#ifdef MODULE_PICKING
  fragColor = picking_filterHighlightColor(fragColor);
  fragColor = picking_filterPickingColor(fragColor);
#endif

#ifdef MODULE_LOGDEPTH
  logdepth_setFragDepth();
#endif
`
);

// dist/lib/shader-assembly/shader-injections.js
var MODULE_INJECTORS = {
  vertex: MODULE_INJECTORS_VS,
  fragment: MODULE_INJECTORS_FS
};
var REGEX_START_OF_MAIN = /void\s+main\s*\([^)]*\)\s*\{\n?/;
var REGEX_END_OF_MAIN = /}\n?[^{}]*$/;
var fragments = [];
var DECLARATION_INJECT_MARKER = "__LUMA_INJECT_DECLARATIONS__";
function normalizeInjections(injections) {
  const result = { vertex: {}, fragment: {} };
  for (const hook in injections) {
    let injection = injections[hook];
    const stage = getHookStage(hook);
    if (typeof injection === "string") {
      injection = {
        order: 0,
        injection
      };
    }
    result[stage][hook] = injection;
  }
  return result;
}
function getHookStage(hook) {
  const type = hook.slice(0, 2);
  switch (type) {
    case "vs":
      return "vertex";
    case "fs":
      return "fragment";
    default:
      throw new Error(type);
  }
}
function injectShader(source3, stage, inject, injectStandardStubs = false) {
  const isVertex = stage === "vertex";
  for (const key in inject) {
    const fragmentData = inject[key];
    fragmentData.sort((a, b) => a.order - b.order);
    fragments.length = fragmentData.length;
    for (let i = 0, len = fragmentData.length; i < len; ++i) {
      fragments[i] = fragmentData[i].injection;
    }
    const fragmentString = `${fragments.join("\n")}
`;
    switch (key) {
      case "vs:#decl":
        if (isVertex) {
          source3 = source3.replace(DECLARATION_INJECT_MARKER, fragmentString);
        }
        break;
      case "vs:#main-start":
        if (isVertex) {
          source3 = source3.replace(REGEX_START_OF_MAIN, (match) => match + fragmentString);
        }
        break;
      case "vs:#main-end":
        if (isVertex) {
          source3 = source3.replace(REGEX_END_OF_MAIN, (match) => fragmentString + match);
        }
        break;
      case "fs:#decl":
        if (!isVertex) {
          source3 = source3.replace(DECLARATION_INJECT_MARKER, fragmentString);
        }
        break;
      case "fs:#main-start":
        if (!isVertex) {
          source3 = source3.replace(REGEX_START_OF_MAIN, (match) => match + fragmentString);
        }
        break;
      case "fs:#main-end":
        if (!isVertex) {
          source3 = source3.replace(REGEX_END_OF_MAIN, (match) => fragmentString + match);
        }
        break;
      default:
        source3 = source3.replace(key, (match) => match + fragmentString);
    }
  }
  source3 = source3.replace(DECLARATION_INJECT_MARKER, "");
  if (injectStandardStubs) {
    source3 = source3.replace(/\}\s*$/, (match) => match + MODULE_INJECTORS[stage]);
  }
  return source3;
}
function combineInjects(injects) {
  const result = {};
  assert(Array.isArray(injects) && injects.length > 1);
  injects.forEach((inject) => {
    for (const key in inject) {
      result[key] = result[key] ? `${result[key]}
${inject[key]}` : inject[key];
    }
  });
  return result;
}

// dist/lib/shader-module/shader-module.js
function initializeShaderModules(modules) {
  modules.map((module2) => initializeShaderModule(module2));
}
function initializeShaderModule(module2) {
  if (module2.instance) {
    return;
  }
  initializeShaderModules(module2.dependencies || []);
  const {
    propTypes = {},
    deprecations = [],
    // defines = {},
    inject = {}
  } = module2;
  const instance = {
    normalizedInjections: normalizeInjections(inject),
    parsedDeprecations: parseDeprecationDefinitions(deprecations)
  };
  if (propTypes) {
    instance.propValidators = makePropValidators(propTypes);
  }
  module2.instance = instance;
  let defaultProps = {};
  if (propTypes) {
    defaultProps = Object.entries(propTypes).reduce((obj, [key, propType]) => {
      const value = propType == null ? void 0 : propType.value;
      if (value) {
        obj[key] = value;
      }
      return obj;
    }, {});
  }
  module2.defaultUniforms = { ...module2.defaultUniforms, ...defaultProps };
}
function getShaderModuleUniforms(module2, props, oldUniforms) {
  var _a;
  initializeShaderModule(module2);
  const uniforms = oldUniforms || { ...module2.defaultUniforms };
  if (props && module2.getUniforms) {
    return module2.getUniforms(props, uniforms);
  }
  return getValidatedProperties(props, (_a = module2.instance) == null ? void 0 : _a.propValidators, module2.name);
}
function checkShaderModuleDeprecations(shaderModule, shaderSource, log3) {
  var _a;
  (_a = shaderModule.deprecations) == null ? void 0 : _a.forEach((def) => {
    var _a2;
    if ((_a2 = def.regex) == null ? void 0 : _a2.test(shaderSource)) {
      if (def.deprecated) {
        log3.deprecated(def.old, def.new)();
      } else {
        log3.removed(def.old, def.new)();
      }
    }
  });
}
function parseDeprecationDefinitions(deprecations) {
  deprecations.forEach((def) => {
    switch (def.type) {
      case "function":
        def.regex = new RegExp(`\\b${def.old}\\(`);
        break;
      default:
        def.regex = new RegExp(`${def.type} ${def.old};`);
    }
  });
  return deprecations;
}

// dist/lib/shader-module/shader-module-dependencies.js
function getShaderModuleDependencies(modules) {
  initializeShaderModules(modules);
  const moduleMap = {};
  const moduleDepth = {};
  getDependencyGraph({ modules, level: 0, moduleMap, moduleDepth });
  const dependencies = Object.keys(moduleDepth).sort((a, b) => moduleDepth[b] - moduleDepth[a]).map((name) => moduleMap[name]);
  initializeShaderModules(dependencies);
  return dependencies;
}
function getDependencyGraph(options) {
  const { modules, level, moduleMap, moduleDepth } = options;
  if (level >= 5) {
    throw new Error("Possible loop in shader dependency graph");
  }
  for (const module2 of modules) {
    moduleMap[module2.name] = module2;
    if (moduleDepth[module2.name] === void 0 || moduleDepth[module2.name] < level) {
      moduleDepth[module2.name] = level;
    }
  }
  for (const module2 of modules) {
    if (module2.dependencies) {
      getDependencyGraph({ modules: module2.dependencies, level: level + 1, moduleMap, moduleDepth });
    }
  }
}
function getShaderDependencies(modules) {
  initializeShaderModules(modules);
  const moduleMap = {};
  const moduleDepth = {};
  getDependencyGraph({ modules, level: 0, moduleMap, moduleDepth });
  modules = Object.keys(moduleDepth).sort((a, b) => moduleDepth[b] - moduleDepth[a]).map((name) => moduleMap[name]);
  initializeShaderModules(modules);
  return modules;
}
function resolveModules(modules) {
  return getShaderDependencies(modules);
}

// dist/lib/shader-assembly/platform-defines.js
function getPlatformShaderDefines(platformInfo) {
  switch (platformInfo == null ? void 0 : platformInfo.gpu.toLowerCase()) {
    case "apple":
      return (
        /* glsl */
        `#define APPLE_GPU
// Apple optimizes away the calculation necessary for emulated fp64
#define LUMA_FP64_CODE_ELIMINATION_WORKAROUND 1
#define LUMA_FP32_TAN_PRECISION_WORKAROUND 1
// Intel GPU doesn't have full 32 bits precision in same cases, causes overflow
#define LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND 1
`
      );
    case "nvidia":
      return (
        /* glsl */
        `#define NVIDIA_GPU
// Nvidia optimizes away the calculation necessary for emulated fp64
#define LUMA_FP64_CODE_ELIMINATION_WORKAROUND 1
`
      );
    case "intel":
      return (
        /* glsl */
        `#define INTEL_GPU
// Intel optimizes away the calculation necessary for emulated fp64
#define LUMA_FP64_CODE_ELIMINATION_WORKAROUND 1
// Intel's built-in 'tan' function doesn't have acceptable precision
#define LUMA_FP32_TAN_PRECISION_WORKAROUND 1
// Intel GPU doesn't have full 32 bits precision in same cases, causes overflow
#define LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND 1
`
      );
    case "amd":
      return (
        /* glsl */
        `#define AMD_GPU
`
      );
    default:
      return (
        /* glsl */
        `#define DEFAULT_GPU
// Prevent driver from optimizing away the calculation necessary for emulated fp64
#define LUMA_FP64_CODE_ELIMINATION_WORKAROUND 1
// Headless Chrome's software shader 'tan' function doesn't have acceptable precision
#define LUMA_FP32_TAN_PRECISION_WORKAROUND 1
// If the GPU doesn't have full 32 bits precision, will causes overflow
#define LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND 1
`
      );
  }
}

// dist/lib/shader-transpiler/transpile-glsl-shader.js
function transpileGLSLShader(source3, stage) {
  var _a;
  const sourceGLSLVersion = Number(((_a = source3.match(/^#version[ \t]+(\d+)/m)) == null ? void 0 : _a[1]) || 100);
  if (sourceGLSLVersion !== 300) {
    throw new Error("luma.gl v9 only supports GLSL 3.00 shader sources");
  }
  switch (stage) {
    case "vertex":
      source3 = convertShader(source3, ES300_VERTEX_REPLACEMENTS);
      return source3;
    case "fragment":
      source3 = convertShader(source3, ES300_FRAGMENT_REPLACEMENTS);
      return source3;
    default:
      throw new Error(stage);
  }
}
var ES300_REPLACEMENTS = [
  // Fix poorly formatted version directive
  [/^(#version[ \t]+(100|300[ \t]+es))?[ \t]*\n/, "#version 300 es\n"],
  // The individual `texture...()` functions were replaced with `texture()` overloads
  [/\btexture(2D|2DProj|Cube)Lod(EXT)?\(/g, "textureLod("],
  [/\btexture(2D|2DProj|Cube)(EXT)?\(/g, "texture("]
];
var ES300_VERTEX_REPLACEMENTS = [
  ...ES300_REPLACEMENTS,
  // `attribute` keyword replaced with `in`
  [makeVariableTextRegExp("attribute"), "in $1"],
  // `varying` keyword replaced with `out`
  [makeVariableTextRegExp("varying"), "out $1"]
];
var ES300_FRAGMENT_REPLACEMENTS = [
  ...ES300_REPLACEMENTS,
  // `varying` keyword replaced with `in`
  [makeVariableTextRegExp("varying"), "in $1"]
];
function convertShader(source3, replacements) {
  for (const [pattern, replacement] of replacements) {
    source3 = source3.replace(pattern, replacement);
  }
  return source3;
}
function makeVariableTextRegExp(qualifier) {
  return new RegExp(`\\b${qualifier}[ \\t]+(\\w+[ \\t]+\\w+(\\[\\w+\\])?;)`, "g");
}

// dist/lib/shader-assembly/shader-hooks.js
function getShaderHooks(hookFunctions, hookInjections) {
  let result = "";
  for (const hookName in hookFunctions) {
    const hookFunction = hookFunctions[hookName];
    result += `void ${hookFunction.signature} {
`;
    if (hookFunction.header) {
      result += `  ${hookFunction.header}`;
    }
    if (hookInjections[hookName]) {
      const injections = hookInjections[hookName];
      injections.sort((a, b) => a.order - b.order);
      for (const injection of injections) {
        result += `  ${injection.injection}
`;
      }
    }
    if (hookFunction.footer) {
      result += `  ${hookFunction.footer}`;
    }
    result += "}\n";
  }
  return result;
}
function normalizeShaderHooks(hookFunctions) {
  const result = { vertex: {}, fragment: {} };
  for (const hookFunction of hookFunctions) {
    let opts;
    let hook;
    if (typeof hookFunction !== "string") {
      opts = hookFunction;
      hook = opts.hook;
    } else {
      opts = {};
      hook = hookFunction;
    }
    hook = hook.trim();
    const [shaderStage, signature] = hook.split(":");
    const name = hook.replace(/\(.+/, "");
    const normalizedHook = Object.assign(opts, { signature });
    switch (shaderStage) {
      case "vs":
        result.vertex[name] = normalizedHook;
        break;
      case "fs":
        result.fragment[name] = normalizedHook;
        break;
      default:
        throw new Error(shaderStage);
    }
  }
  return result;
}

// dist/lib/glsl-utils/get-shader-info.js
function getShaderInfo(source3, defaultName) {
  return {
    name: getShaderName(source3, defaultName),
    language: "glsl",
    version: getShaderVersion(source3)
  };
}
function getShaderName(shader, defaultName = "unnamed") {
  const SHADER_NAME_REGEXP = /#define[^\S\r\n]*SHADER_NAME[^\S\r\n]*([A-Za-z0-9_-]+)\s*/;
  const match = SHADER_NAME_REGEXP.exec(shader);
  return match ? match[1] : defaultName;
}
function getShaderVersion(source3) {
  let version = 100;
  const words = source3.match(/[^\s]+/g);
  if (words && words.length >= 2 && words[0] === "#version") {
    const parsedVersion = parseInt(words[1], 10);
    if (Number.isFinite(parsedVersion)) {
      version = parsedVersion;
    }
  }
  if (version !== 100 && version !== 300) {
    throw new Error(`Invalid GLSL version ${version}`);
  }
  return version;
}

// dist/lib/shader-assembly/assemble-shaders.js
var INJECT_SHADER_DECLARATIONS = `

${DECLARATION_INJECT_MARKER}
`;
var FRAGMENT_SHADER_PROLOGUE = (
  /* glsl */
  `precision highp float;
`
);
function assembleWGSLShader(options) {
  const modules = getShaderModuleDependencies(options.modules || []);
  return {
    source: assembleShaderWGSL(options.platformInfo, {
      ...options,
      source: options.source,
      stage: "vertex",
      modules
    }),
    getUniforms: assembleGetUniforms(modules)
  };
}
function assembleGLSLShaderPair(options) {
  const { vs: vs3, fs: fs4 } = options;
  const modules = getShaderModuleDependencies(options.modules || []);
  return {
    vs: assembleShaderGLSL(options.platformInfo, {
      ...options,
      source: vs3,
      stage: "vertex",
      modules
    }),
    fs: assembleShaderGLSL(options.platformInfo, {
      ...options,
      // @ts-expect-error
      source: fs4,
      stage: "fragment",
      modules
    }),
    getUniforms: assembleGetUniforms(modules)
  };
}
function assembleShaderWGSL(platformInfo, options) {
  var _a;
  const {
    // id,
    source: source3,
    stage,
    modules,
    // defines = {},
    hookFunctions = [],
    inject = {},
    log: log3
  } = options;
  assert(typeof source3 === "string", "shader source must be a string");
  const coreSource = source3;
  let assembledSource = "";
  const hookFunctionMap = normalizeShaderHooks(hookFunctions);
  const hookInjections = {};
  const declInjections = {};
  const mainInjections = {};
  for (const key in inject) {
    const injection = typeof inject[key] === "string" ? { injection: inject[key], order: 0 } : inject[key];
    const match = /^(v|f)s:(#)?([\w-]+)$/.exec(key);
    if (match) {
      const hash = match[2];
      const name = match[3];
      if (hash) {
        if (name === "decl") {
          declInjections[key] = [injection];
        } else {
          mainInjections[key] = [injection];
        }
      } else {
        hookInjections[key] = [injection];
      }
    } else {
      mainInjections[key] = [injection];
    }
  }
  const modulesToInject = modules;
  for (const module2 of modulesToInject) {
    if (log3) {
      checkShaderModuleDeprecations(module2, coreSource, log3);
    }
    const moduleSource = getShaderModuleSource(module2, "wgsl");
    assembledSource += moduleSource;
    const injections = ((_a = module2.injections) == null ? void 0 : _a[stage]) || {};
    for (const key in injections) {
      const match = /^(v|f)s:#([\w-]+)$/.exec(key);
      if (match) {
        const name = match[2];
        const injectionType = name === "decl" ? declInjections : mainInjections;
        injectionType[key] = injectionType[key] || [];
        injectionType[key].push(injections[key]);
      } else {
        hookInjections[key] = hookInjections[key] || [];
        hookInjections[key].push(injections[key]);
      }
    }
  }
  assembledSource += INJECT_SHADER_DECLARATIONS;
  assembledSource = injectShader(assembledSource, stage, declInjections);
  assembledSource += getShaderHooks(hookFunctionMap[stage], hookInjections);
  assembledSource += coreSource;
  assembledSource = injectShader(assembledSource, stage, mainInjections);
  return assembledSource;
}
function assembleShaderGLSL(platformInfo, options) {
  var _a;
  const { source: source3, stage, language = "glsl", modules, defines = {}, hookFunctions = [], inject = {}, prologue = true, log: log3 } = options;
  assert(typeof source3 === "string", "shader source must be a string");
  const sourceVersion = language === "glsl" ? getShaderInfo(source3).version : -1;
  const targetVersion = platformInfo.shaderLanguageVersion;
  const sourceVersionDirective = sourceVersion === 100 ? "#version 100" : "#version 300 es";
  const sourceLines = source3.split("\n");
  const coreSource = sourceLines.slice(1).join("\n");
  const allDefines = {};
  modules.forEach((module2) => {
    Object.assign(allDefines, module2.defines);
  });
  Object.assign(allDefines, defines);
  let assembledSource = "";
  switch (language) {
    case "wgsl":
      break;
    case "glsl":
      assembledSource = prologue ? `${sourceVersionDirective}

// ----- PROLOGUE -------------------------
${`#define SHADER_TYPE_${stage.toUpperCase()}`}

${getPlatformShaderDefines(platformInfo)}
${stage === "fragment" ? FRAGMENT_SHADER_PROLOGUE : ""}

// ----- APPLICATION DEFINES -------------------------

${getApplicationDefines(allDefines)}

` : `${sourceVersionDirective}
`;
      break;
  }
  const hookFunctionMap = normalizeShaderHooks(hookFunctions);
  const hookInjections = {};
  const declInjections = {};
  const mainInjections = {};
  for (const key in inject) {
    const injection = typeof inject[key] === "string" ? { injection: inject[key], order: 0 } : inject[key];
    const match = /^(v|f)s:(#)?([\w-]+)$/.exec(key);
    if (match) {
      const hash = match[2];
      const name = match[3];
      if (hash) {
        if (name === "decl") {
          declInjections[key] = [injection];
        } else {
          mainInjections[key] = [injection];
        }
      } else {
        hookInjections[key] = [injection];
      }
    } else {
      mainInjections[key] = [injection];
    }
  }
  for (const module2 of modules) {
    if (log3) {
      checkShaderModuleDeprecations(module2, coreSource, log3);
    }
    const moduleSource = getShaderModuleSource(module2, stage);
    assembledSource += moduleSource;
    const injections = ((_a = module2.instance) == null ? void 0 : _a.normalizedInjections[stage]) || {};
    for (const key in injections) {
      const match = /^(v|f)s:#([\w-]+)$/.exec(key);
      if (match) {
        const name = match[2];
        const injectionType = name === "decl" ? declInjections : mainInjections;
        injectionType[key] = injectionType[key] || [];
        injectionType[key].push(injections[key]);
      } else {
        hookInjections[key] = hookInjections[key] || [];
        hookInjections[key].push(injections[key]);
      }
    }
  }
  assembledSource += "// ----- MAIN SHADER SOURCE -------------------------";
  assembledSource += INJECT_SHADER_DECLARATIONS;
  assembledSource = injectShader(assembledSource, stage, declInjections);
  assembledSource += getShaderHooks(hookFunctionMap[stage], hookInjections);
  assembledSource += coreSource;
  assembledSource = injectShader(assembledSource, stage, mainInjections);
  if (language === "glsl" && sourceVersion !== targetVersion) {
    assembledSource = transpileGLSLShader(assembledSource, stage);
  }
  return assembledSource.trim();
}
function assembleGetUniforms(modules) {
  return function getUniforms4(opts) {
    var _a;
    const uniforms = {};
    for (const module2 of modules) {
      const moduleUniforms = (_a = module2.getUniforms) == null ? void 0 : _a.call(module2, opts, uniforms);
      Object.assign(uniforms, moduleUniforms);
    }
    return uniforms;
  };
}
function getApplicationDefines(defines = {}) {
  let sourceText = "";
  for (const define in defines) {
    const value = defines[define];
    if (value || Number.isFinite(value)) {
      sourceText += `#define ${define.toUpperCase()} ${defines[define]}
`;
    }
  }
  return sourceText;
}
function getShaderModuleSource(module2, stage) {
  let moduleSource;
  switch (stage) {
    case "vertex":
      moduleSource = module2.vs || "";
      break;
    case "fragment":
      moduleSource = module2.fs || "";
      break;
    case "wgsl":
      moduleSource = module2.source || "";
      break;
    default:
      assert(false);
  }
  if (!module2.name) {
    throw new Error("Shader module must have a name");
  }
  const moduleName = module2.name.toUpperCase().replace(/[^0-9a-z]/gi, "_");
  let source3 = `// ----- MODULE ${module2.name} ---------------

`;
  if (stage !== "wgsl") {
    source3 += `#define MODULE_${moduleName}
`;
  }
  source3 += `${moduleSource}
`;
  return source3;
}

// dist/lib/preprocessor/preprocessor.js
var IFDEF_REGEXP = /^\s*\#\s*ifdef\s*([a-zA-Z_]+)\s*$/;
var ENDIF_REGEXP = /^\s*\#\s*endif\s*$/;
function preprocess(source3, options) {
  var _a;
  const lines = source3.split("\n");
  const output = [];
  let conditional = true;
  let currentDefine = null;
  for (const line of lines) {
    const matchIf = line.match(IFDEF_REGEXP);
    const matchEnd = line.match(ENDIF_REGEXP);
    if (matchIf) {
      currentDefine = matchIf[1];
      conditional = Boolean((_a = options == null ? void 0 : options.defines) == null ? void 0 : _a[currentDefine]);
    } else if (matchEnd) {
      conditional = true;
    } else if (conditional) {
      output.push(line);
    }
  }
  return output.join("\n");
}

// dist/lib/shader-assembler.js
var _ShaderAssembler = class {
  /** Hook functions */
  _hookFunctions = [];
  /** Shader modules */
  _defaultModules = [];
  /**
   * A default shader assembler instance - the natural place to register default modules and hooks
   * @returns
   */
  static getDefaultShaderAssembler() {
    _ShaderAssembler.defaultShaderAssembler = _ShaderAssembler.defaultShaderAssembler || new _ShaderAssembler();
    return _ShaderAssembler.defaultShaderAssembler;
  }
  /**
   * Add a default module that does not have to be provided with every call to assembleShaders()
   */
  addDefaultModule(module2) {
    if (!this._defaultModules.find((m) => m.name === (typeof module2 === "string" ? module2 : module2.name))) {
      this._defaultModules.push(module2);
    }
  }
  /**
   * Remove a default module
   */
  removeDefaultModule(module2) {
    const moduleName = typeof module2 === "string" ? module2 : module2.name;
    this._defaultModules = this._defaultModules.filter((m) => m.name !== moduleName);
  }
  /**
   * Register a shader hook
   * @param hook
   * @param opts
   */
  addShaderHook(hook, opts) {
    if (opts) {
      hook = Object.assign(opts, { hook });
    }
    this._hookFunctions.push(hook);
  }
  /**
   * Assemble a WGSL unified shader
   * @param platformInfo
   * @param props
   * @returns
   */
  assembleWGSLShader(props) {
    const modules = this._getModuleList(props.modules);
    const hookFunctions = this._hookFunctions;
    const { source: source3, getUniforms: getUniforms4 } = assembleWGSLShader({
      ...props,
      // @ts-expect-error
      source: props.source,
      modules,
      hookFunctions
    });
    const preprocessedSource = props.platformInfo.shaderLanguage === "wgsl" ? preprocess(source3) : source3;
    return { source: preprocessedSource, getUniforms: getUniforms4, modules };
  }
  /**
   * Assemble a pair of shaders into a single shader program
   * @param platformInfo
   * @param props
   * @returns
   */
  assembleGLSLShaderPair(props) {
    const modules = this._getModuleList(props.modules);
    const hookFunctions = this._hookFunctions;
    const assembled = assembleGLSLShaderPair({
      ...props,
      // @ts-expect-error
      vs: props.vs,
      // @ts-expect-error
      fs: props.fs,
      modules,
      hookFunctions
    });
    return { ...assembled, modules };
  }
  /**
   * Dedupe and combine with default modules
   */
  _getModuleList(appModules = []) {
    const modules = new Array(this._defaultModules.length + appModules.length);
    const seen = {};
    let count = 0;
    for (let i = 0, len = this._defaultModules.length; i < len; ++i) {
      const module2 = this._defaultModules[i];
      const name = module2.name;
      modules[count++] = module2;
      seen[name] = true;
    }
    for (let i = 0, len = appModules.length; i < len; ++i) {
      const module2 = appModules[i];
      const name = module2.name;
      if (!seen[name]) {
        modules[count++] = module2;
        seen[name] = true;
      }
    }
    modules.length = count;
    initializeShaderModules(modules);
    return modules;
  }
};
var ShaderAssembler = _ShaderAssembler;
/** Default ShaderAssembler instance */
__publicField(ShaderAssembler, "defaultShaderAssembler");

// dist/lib/glsl-utils/shader-utils.js
var FS_GLES = (
  /* glsl */
  `out vec4 transform_output;
void main() {
  transform_output = vec4(0);
}`
);
var FS300 = `#version 300 es
${FS_GLES}`;
function getQualifierDetails(line, qualifiers) {
  qualifiers = Array.isArray(qualifiers) ? qualifiers : [qualifiers];
  const words = line.replace(/^\s+/, "").split(/\s+/);
  const [qualifier, type, definition] = words;
  if (!qualifiers.includes(qualifier) || !type || !definition) {
    return null;
  }
  const name = definition.split(";")[0];
  return { qualifier, type, name };
}
function getPassthroughFS(options) {
  const { input, inputChannels, output } = options || {};
  if (!input) {
    return FS300;
  }
  if (!inputChannels) {
    throw new Error("inputChannels");
  }
  const inputType = channelCountToType(inputChannels);
  const outputValue = convertToVec4(input, inputChannels);
  return `#version 300 es
in ${inputType} ${input};
out vec4 ${output};
void main() {
  ${output} = ${outputValue};
}`;
}
function typeToChannelSuffix(type) {
  switch (type) {
    case "float":
      return "x";
    case "vec2":
      return "xy";
    case "vec3":
      return "xyz";
    case "vec4":
      return "xyzw";
    default:
      throw new Error(type);
  }
}
function typeToChannelCount(type) {
  switch (type) {
    case "float":
      return 1;
    case "vec2":
      return 2;
    case "vec3":
      return 3;
    case "vec4":
      return 4;
    default:
      throw new Error(type);
  }
}
function channelCountToType(channels) {
  switch (channels) {
    case 1:
      return "float";
    case 2:
      return "vec2";
    case 3:
      return "vec3";
    case 4:
      return "vec4";
    default:
      throw new Error(`invalid channels: ${channels}`);
  }
}
function convertToVec4(variable, channels) {
  switch (channels) {
    case 1:
      return `vec4(${variable}, 0.0, 0.0, 1.0)`;
    case 2:
      return `vec4(${variable}, 0.0, 1.0)`;
    case 3:
      return `vec4(${variable}, 1.0)`;
    case 4:
      return variable;
    default:
      throw new Error(`invalid channels: ${channels}`);
  }
}

// dist/lib/shader-generator/utils/capitalize.js
function capitalize(str) {
  return typeof str === "string" ? str.charAt(0).toUpperCase() + str.slice(1) : str;
}

// dist/lib/shader-generator/glsl/generate-glsl.js
function generateGLSLForModule(module2, options) {
  return generateGLSLUniformDeclarations(module2, options);
}
function generateGLSLUniformDeclarations(module2, options) {
  const glsl = [];
  switch (options.uniforms) {
    case "scoped-interface-blocks":
    case "unscoped-interface-blocks":
      glsl.push(`uniform ${capitalize(module2.name)} {`);
      break;
    case "uniforms":
  }
  for (const [uniformName, uniformFormat] of Object.entries(module2.uniformTypes || {})) {
    const glslUniformType = getGLSLUniformType(uniformFormat);
    switch (options.uniforms) {
      case "scoped-interface-blocks":
        glsl.push(`  ${glslUniformType} ${uniformName};`);
        break;
      case "unscoped-interface-blocks":
        glsl.push(`  ${glslUniformType} ${module2.name}_${uniformName};`);
        break;
      case "uniforms":
        glsl.push(`uniform ${glslUniformType} ${module2.name}_${uniformName};`);
    }
  }
  switch (options.uniforms) {
    case "scoped-interface-blocks":
      glsl.push(`} ${module2.name};`);
      break;
    case "unscoped-interface-blocks":
      glsl.push("};");
      break;
    case "uniforms":
  }
  glsl.push("");
  return glsl.join("\n");
}
function getGLSLUniformType(uniformFormat) {
  const UNIFORM_TYPE_TO_GLSL = {
    f32: "float",
    i32: "int",
    u32: "uint",
    "vec2<f32>": "vec2",
    "vec3<f32>": "vec3",
    "vec4<f32>": "vec4",
    "vec2<i32>": "ivec2",
    "vec3<i32>": "ivec3",
    "vec4<i32>": "ivec4",
    "vec2<u32>": "uvec2",
    "vec3<u32>": "uvec3",
    "vec4<u32>": "uvec4",
    "mat2x2<f32>": "mat2",
    "mat2x3<f32>": "mat2x3",
    "mat2x4<f32>": "mat2x4",
    "mat3x2<f32>": "mat3x2",
    "mat3x3<f32>": "mat3",
    "mat3x4<f32>": "mat3x4",
    "mat4x2<f32>": "mat4x2",
    "mat4x3<f32>": "mat4x3",
    "mat4x4<f32>": "mat4"
  };
  const glsl = UNIFORM_TYPE_TO_GLSL[uniformFormat];
  return glsl;
}

// dist/lib/shader-generator/wgsl/generate-wgsl.js
function generateWGSLForModule(module2, options) {
  return generateWGSLUniformDeclarations(module2, options);
}
function generateWGSLUniformDeclarations(module2, options) {
  const wgsl = [];
  wgsl.push(`struct ${capitalize(module2.name)} {`);
  for (const [uniformName, uniformFormat] of Object.entries((module2 == null ? void 0 : module2.uniformTypes) || {})) {
    const wgslUniformType = uniformFormat;
    wgsl.push(`  ${uniformName} : ${wgslUniformType};`);
  }
  wgsl.push("};");
  wgsl.push(`var<uniform> ${module2.name} : ${capitalize(module2.name)};`);
  return wgsl.join("\n");
}

// dist/lib/shader-generator/generate-shader.js
function generateShaderForModule(module2, options) {
  switch (options.shaderLanguage) {
    case "glsl":
      return generateGLSLForModule(module2, options);
    case "wgsl":
      return generateWGSLForModule(module2, options);
  }
}

// dist/lib/wgsl/get-shader-layout-wgsl.js
var import_core = require("@luma.gl/core");
var import_wgsl_reflect = require("wgsl_reflect");
function getShaderLayoutFromWGSL(source3) {
  var _a;
  const shaderLayout = { attributes: [], bindings: [] };
  let parsedWGSL;
  try {
    parsedWGSL = parseWGSL(source3);
  } catch (error) {
    import_core.log.error(error.message)();
    return shaderLayout;
  }
  for (const uniform of parsedWGSL.uniforms) {
    const members = [];
    for (const attribute of ((_a = uniform.type) == null ? void 0 : _a.members) || []) {
      members.push({
        name: attribute.name,
        type: getType(attribute.type)
      });
    }
    shaderLayout.bindings.push({
      type: "uniform",
      name: uniform.name,
      group: uniform.group,
      location: uniform.binding,
      // @ts-expect-error TODO - unused for now but needs fixing
      members
    });
  }
  for (const texture of parsedWGSL.textures) {
    shaderLayout.bindings.push({
      type: "texture",
      name: texture.name,
      group: texture.group,
      location: texture.binding
    });
  }
  for (const sampler of parsedWGSL.samplers) {
    shaderLayout.bindings.push({
      type: "sampler",
      name: sampler.name,
      group: sampler.group,
      location: sampler.binding
    });
  }
  const vertex = parsedWGSL.entry.vertex[0];
  const attributeCount = (vertex == null ? void 0 : vertex.inputs.length) || 0;
  for (let i = 0; i < attributeCount; i++) {
    const wgslAttribute = vertex.inputs[i];
    if (wgslAttribute.locationType === "location") {
      const type = getType(wgslAttribute.type);
      shaderLayout.attributes.push({
        name: wgslAttribute.name,
        location: Number(wgslAttribute.location),
        type
      });
    }
  }
  return shaderLayout;
}
function getType(type) {
  return (type == null ? void 0 : type.format) ? `${type.name}<${type.format.name}>` : type.name;
}
function parseWGSL(source3) {
  try {
    return new import_wgsl_reflect.WgslReflect(source3);
  } catch (error) {
    if (error instanceof Error) {
      throw error;
    }
    let message = "WGSL parse error";
    if (typeof error === "object" && (error == null ? void 0 : error.message)) {
      message += `: ${error.message} `;
    }
    if (typeof error === "object" && (error == null ? void 0 : error.token)) {
      message += error.token.line || "";
    }
    throw new Error(message, { cause: error });
  }
}

// dist/modules/math/fp16/fp16-utils.js
var import_core2 = require("@math.gl/core");
var float16Tables = null;
var buffer = new ArrayBuffer(4);
var floatView = new Float32Array(buffer);
var uint32View = new Uint32Array(buffer);
function toHalfFloat(val) {
  float16Tables ||= generateFloat16Tables();
  val = (0, import_core2.clamp)(val, -65504, 65504);
  floatView[0] = val;
  const f = uint32View[0];
  const e = f >> 23 & 511;
  return float16Tables.baseTable[e] + ((f & 8388607) >> float16Tables.shiftTable[e]);
}
function fromHalfFloat(val) {
  float16Tables ||= generateFloat16Tables();
  const m = val >> 10;
  uint32View[0] = float16Tables.mantissaTable[float16Tables.offsetTable[m] + (val & 1023)] + float16Tables.exponentTable[m];
  return floatView[0];
}
function generateFloat16Tables() {
  const baseTable = new Uint32Array(512);
  const shiftTable = new Uint32Array(512);
  for (let i = 0; i < 256; ++i) {
    const e = i - 127;
    if (e < -27) {
      baseTable[i] = 0;
      baseTable[i | 256] = 32768;
      shiftTable[i] = 24;
      shiftTable[i | 256] = 24;
    } else if (e < -14) {
      baseTable[i] = 1024 >> -e - 14;
      baseTable[i | 256] = 1024 >> -e - 14 | 32768;
      shiftTable[i] = -e - 1;
      shiftTable[i | 256] = -e - 1;
    } else if (e <= 15) {
      baseTable[i] = e + 15 << 10;
      baseTable[i | 256] = e + 15 << 10 | 32768;
      shiftTable[i] = 13;
      shiftTable[i | 256] = 13;
    } else if (e < 128) {
      baseTable[i] = 31744;
      baseTable[i | 256] = 64512;
      shiftTable[i] = 24;
      shiftTable[i | 256] = 24;
    } else {
      baseTable[i] = 31744;
      baseTable[i | 256] = 64512;
      shiftTable[i] = 13;
      shiftTable[i | 256] = 13;
    }
  }
  const mantissaTable = new Uint32Array(2048);
  const exponentTable = new Uint32Array(64);
  const offsetTable = new Uint32Array(64);
  for (let i = 1; i < 1024; ++i) {
    let m = i << 13;
    let e = 0;
    while ((m & 8388608) === 0) {
      m <<= 1;
      e -= 8388608;
    }
    m &= ~8388608;
    e += 947912704;
    mantissaTable[i] = m | e;
  }
  for (let i = 1024; i < 2048; ++i) {
    mantissaTable[i] = 939524096 + (i - 1024 << 13);
  }
  for (let i = 1; i < 31; ++i) {
    exponentTable[i] = i << 23;
  }
  exponentTable[31] = 1199570944;
  exponentTable[32] = 2147483648;
  for (let i = 33; i < 63; ++i) {
    exponentTable[i] = 2147483648 + (i - 32 << 23);
  }
  exponentTable[63] = 3347054592;
  for (let i = 1; i < 64; ++i) {
    if (i !== 32) {
      offsetTable[i] = 1024;
    }
  }
  return { baseTable, shiftTable, mantissaTable, exponentTable, offsetTable };
}

// dist/modules/math/fp64/fp64-utils.js
function fp64ify(a, out = [], startIndex = 0) {
  const hiPart = Math.fround(a);
  const loPart = a - hiPart;
  out[startIndex] = hiPart;
  out[startIndex + 1] = loPart;
  return out;
}
function fp64LowPart(a) {
  return a - Math.fround(a);
}
function fp64ifyMatrix4(matrix) {
  const matrixFP64 = new Float32Array(32);
  for (let i = 0; i < 4; ++i) {
    for (let j = 0; j < 4; ++j) {
      const index = i * 4 + j;
      fp64ify(matrix[j * 4 + i], matrixFP64, index * 2);
    }
  }
  return matrixFP64;
}

// dist/modules/math/random/random.js
var source = (
  /* wgsl */
  `fn random(scale: vec3f, seed: float) -> f32 {
  /* use the fragment position for a different seed per-pixel */
  return fract(sin(dot(gl_FragCoord.xyz + seed, scale)) * 43758.5453 + seed);
}
`
);
var fs = (
  /* glsl */
  `float random(vec3 scale, float seed) {
  /* use the fragment position for a different seed per-pixel */
  return fract(sin(dot(gl_FragCoord.xyz + seed, scale)) * 43758.5453 + seed);
}
`
);
var random = {
  name: "random",
  source,
  fs
};

// dist/modules/math/fp32/fp32.js
var fp32shader = (
  /* glsl */
  `#ifdef LUMA_FP32_TAN_PRECISION_WORKAROUND

// All these functions are for substituting tan() function from Intel GPU only
const float TWO_PI = 6.2831854820251465;
const float PI_2 = 1.5707963705062866;
const float PI_16 = 0.1963495463132858;

const float SIN_TABLE_0 = 0.19509032368659973;
const float SIN_TABLE_1 = 0.3826834261417389;
const float SIN_TABLE_2 = 0.5555702447891235;
const float SIN_TABLE_3 = 0.7071067690849304;

const float COS_TABLE_0 = 0.9807852506637573;
const float COS_TABLE_1 = 0.9238795042037964;
const float COS_TABLE_2 = 0.8314695954322815;
const float COS_TABLE_3 = 0.7071067690849304;

const float INVERSE_FACTORIAL_3 = 1.666666716337204e-01; // 1/3!
const float INVERSE_FACTORIAL_5 = 8.333333767950535e-03; // 1/5!
const float INVERSE_FACTORIAL_7 = 1.9841270113829523e-04; // 1/7!
const float INVERSE_FACTORIAL_9 = 2.75573188446287533e-06; // 1/9!

float sin_taylor_fp32(float a) {
  float r, s, t, x;

  if (a == 0.0) {
    return 0.0;
  }

  x = -a * a;
  s = a;
  r = a;

  r = r * x;
  t = r * INVERSE_FACTORIAL_3;
  s = s + t;

  r = r * x;
  t = r * INVERSE_FACTORIAL_5;
  s = s + t;

  r = r * x;
  t = r * INVERSE_FACTORIAL_7;
  s = s + t;

  r = r * x;
  t = r * INVERSE_FACTORIAL_9;
  s = s + t;

  return s;
}

void sincos_taylor_fp32(float a, out float sin_t, out float cos_t) {
  if (a == 0.0) {
    sin_t = 0.0;
    cos_t = 1.0;
  }
  sin_t = sin_taylor_fp32(a);
  cos_t = sqrt(1.0 - sin_t * sin_t);
}

float tan_taylor_fp32(float a) {
    float sin_a;
    float cos_a;

    if (a == 0.0) {
        return 0.0;
    }

    // 2pi range reduction
    float z = floor(a / TWO_PI);
    float r = a - TWO_PI * z;

    float t;
    float q = floor(r / PI_2 + 0.5);
    int j = int(q);

    if (j < -2 || j > 2) {
        return 1.0 / 0.0;
    }

    t = r - PI_2 * q;

    q = floor(t / PI_16 + 0.5);
    int k = int(q);
    int abs_k = int(abs(float(k)));

    if (abs_k > 4) {
        return 1.0 / 0.0;
    } else {
        t = t - PI_16 * q;
    }

    float u = 0.0;
    float v = 0.0;

    float sin_t, cos_t;
    float s, c;
    sincos_taylor_fp32(t, sin_t, cos_t);

    if (k == 0) {
        s = sin_t;
        c = cos_t;
    } else {
        if (abs(float(abs_k) - 1.0) < 0.5) {
            u = COS_TABLE_0;
            v = SIN_TABLE_0;
        } else if (abs(float(abs_k) - 2.0) < 0.5) {
            u = COS_TABLE_1;
            v = SIN_TABLE_1;
        } else if (abs(float(abs_k) - 3.0) < 0.5) {
            u = COS_TABLE_2;
            v = SIN_TABLE_2;
        } else if (abs(float(abs_k) - 4.0) < 0.5) {
            u = COS_TABLE_3;
            v = SIN_TABLE_3;
        }
        if (k > 0) {
            s = u * sin_t + v * cos_t;
            c = u * cos_t - v * sin_t;
        } else {
            s = u * sin_t - v * cos_t;
            c = u * cos_t + v * sin_t;
        }
    }

    if (j == 0) {
        sin_a = s;
        cos_a = c;
    } else if (j == 1) {
        sin_a = c;
        cos_a = -s;
    } else if (j == -1) {
        sin_a = -c;
        cos_a = s;
    } else {
        sin_a = -s;
        cos_a = -c;
    }
    return sin_a / cos_a;
}
#endif

float tan_fp32(float a) {
#ifdef LUMA_FP32_TAN_PRECISION_WORKAROUND
  return tan_taylor_fp32(a);
#else
  return tan(a);
#endif
}
`
);
var fp32 = {
  name: "fp32",
  vs: fp32shader
};

// dist/modules/math/fp64/fp64-arithmetic-glsl.js
var fp64arithmeticShader = (
  /* glsl */
  `
uniform fp64arithmeticUniforms {
  uniform float ONE;
} fp64;

/*
About LUMA_FP64_CODE_ELIMINATION_WORKAROUND

The purpose of this workaround is to prevent shader compilers from
optimizing away necessary arithmetic operations by swapping their sequences
or transform the equation to some 'equivalent' form.

The method is to multiply an artifical variable, ONE, which will be known to
the compiler to be 1 only at runtime. The whole expression is then represented
as a polynomial with respective to ONE. In the coefficients of all terms, only one a
and one b should appear

err = (a + b) * ONE^6 - a * ONE^5 - (a + b) * ONE^4 + a * ONE^3 - b - (a + b) * ONE^2 + a * ONE
*/

// Divide float number to high and low floats to extend fraction bits
vec2 split(float a) {
  const float SPLIT = 4097.0;
  float t = a * SPLIT;
#if defined(LUMA_FP64_CODE_ELIMINATION_WORKAROUND)
  float a_hi = t * fp64.ONE - (t - a);
  float a_lo = a * fp64.ONE - a_hi;
#else
  float a_hi = t - (t - a);
  float a_lo = a - a_hi;
#endif
  return vec2(a_hi, a_lo);
}

// Divide float number again when high float uses too many fraction bits
vec2 split2(vec2 a) {
  vec2 b = split(a.x);
  b.y += a.y;
  return b;
}

// Special sum operation when a > b
vec2 quickTwoSum(float a, float b) {
#if defined(LUMA_FP64_CODE_ELIMINATION_WORKAROUND)
  float sum = (a + b) * fp64.ONE;
  float err = b - (sum - a) * fp64.ONE;
#else
  float sum = a + b;
  float err = b - (sum - a);
#endif
  return vec2(sum, err);
}

// General sum operation
vec2 twoSum(float a, float b) {
  float s = (a + b);
#if defined(LUMA_FP64_CODE_ELIMINATION_WORKAROUND)
  float v = (s * fp64.ONE - a) * fp64.ONE;
  float err = (a - (s - v) * fp64.ONE) * fp64.ONE * fp64.ONE * fp64.ONE + (b - v);
#else
  float v = s - a;
  float err = (a - (s - v)) + (b - v);
#endif
  return vec2(s, err);
}

vec2 twoSub(float a, float b) {
  float s = (a - b);
#if defined(LUMA_FP64_CODE_ELIMINATION_WORKAROUND)
  float v = (s * fp64.ONE - a) * fp64.ONE;
  float err = (a - (s - v) * fp64.ONE) * fp64.ONE * fp64.ONE * fp64.ONE - (b + v);
#else
  float v = s - a;
  float err = (a - (s - v)) - (b + v);
#endif
  return vec2(s, err);
}

vec2 twoSqr(float a) {
  float prod = a * a;
  vec2 a_fp64 = split(a);
#if defined(LUMA_FP64_CODE_ELIMINATION_WORKAROUND)
  float err = ((a_fp64.x * a_fp64.x - prod) * fp64.ONE + 2.0 * a_fp64.x *
    a_fp64.y * fp64.ONE * fp64.ONE) + a_fp64.y * a_fp64.y * fp64.ONE * fp64.ONE * fp64.ONE;
#else
  float err = ((a_fp64.x * a_fp64.x - prod) + 2.0 * a_fp64.x * a_fp64.y) + a_fp64.y * a_fp64.y;
#endif
  return vec2(prod, err);
}

vec2 twoProd(float a, float b) {
  float prod = a * b;
  vec2 a_fp64 = split(a);
  vec2 b_fp64 = split(b);
  float err = ((a_fp64.x * b_fp64.x - prod) + a_fp64.x * b_fp64.y +
    a_fp64.y * b_fp64.x) + a_fp64.y * b_fp64.y;
  return vec2(prod, err);
}

vec2 sum_fp64(vec2 a, vec2 b) {
  vec2 s, t;
  s = twoSum(a.x, b.x);
  t = twoSum(a.y, b.y);
  s.y += t.x;
  s = quickTwoSum(s.x, s.y);
  s.y += t.y;
  s = quickTwoSum(s.x, s.y);
  return s;
}

vec2 sub_fp64(vec2 a, vec2 b) {
  vec2 s, t;
  s = twoSub(a.x, b.x);
  t = twoSub(a.y, b.y);
  s.y += t.x;
  s = quickTwoSum(s.x, s.y);
  s.y += t.y;
  s = quickTwoSum(s.x, s.y);
  return s;
}

vec2 mul_fp64(vec2 a, vec2 b) {
  vec2 prod = twoProd(a.x, b.x);
  // y component is for the error
  prod.y += a.x * b.y;
#if defined(LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND)
  prod = split2(prod);
#endif
  prod = quickTwoSum(prod.x, prod.y);
  prod.y += a.y * b.x;
#if defined(LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND)
  prod = split2(prod);
#endif
  prod = quickTwoSum(prod.x, prod.y);
  return prod;
}

vec2 div_fp64(vec2 a, vec2 b) {
  float xn = 1.0 / b.x;
#if defined(LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND)
  vec2 yn = mul_fp64(a, vec2(xn, 0));
#else
  vec2 yn = a * xn;
#endif
  float diff = (sub_fp64(a, mul_fp64(b, yn))).x;
  vec2 prod = twoProd(xn, diff);
  return sum_fp64(yn, prod);
}

vec2 sqrt_fp64(vec2 a) {
  if (a.x == 0.0 && a.y == 0.0) return vec2(0.0, 0.0);
  if (a.x < 0.0) return vec2(0.0 / 0.0, 0.0 / 0.0);

  float x = 1.0 / sqrt(a.x);
  float yn = a.x * x;
#if defined(LUMA_FP64_CODE_ELIMINATION_WORKAROUND)
  vec2 yn_sqr = twoSqr(yn) * fp64.ONE;
#else
  vec2 yn_sqr = twoSqr(yn);
#endif
  float diff = sub_fp64(a, yn_sqr).x;
  vec2 prod = twoProd(x * 0.5, diff);
#if defined(LUMA_FP64_HIGH_BITS_OVERFLOW_WORKAROUND)
  return sum_fp64(split(yn), prod);
#else
  return sum_fp64(vec2(yn, 0.0), prod);
#endif
}
`
);

// dist/modules/math/fp64/fp64-functions-glsl.js
var fp64functionShader = (
  /* glsl */
  `const vec2 E_FP64 = vec2(2.7182817459106445e+00, 8.254840366817007e-08);
const vec2 LOG2_FP64 = vec2(0.6931471824645996e+00, -1.9046542121259336e-09);
const vec2 PI_FP64 = vec2(3.1415927410125732, -8.742278012618954e-8);
const vec2 TWO_PI_FP64 = vec2(6.2831854820251465, -1.7484556025237907e-7);
const vec2 PI_2_FP64 = vec2(1.5707963705062866, -4.371139006309477e-8);
const vec2 PI_4_FP64 = vec2(0.7853981852531433, -2.1855695031547384e-8);
const vec2 PI_16_FP64 = vec2(0.19634954631328583, -5.463923757886846e-9);
const vec2 PI_16_2_FP64 = vec2(0.39269909262657166, -1.0927847515773692e-8);
const vec2 PI_16_3_FP64 = vec2(0.5890486240386963, -1.4906100798128818e-9);
const vec2 PI_180_FP64 = vec2(0.01745329238474369, 1.3519960498364902e-10);

const vec2 SIN_TABLE_0_FP64 = vec2(0.19509032368659973, -1.6704714833615242e-9);
const vec2 SIN_TABLE_1_FP64 = vec2(0.3826834261417389, 6.22335089017767e-9);
const vec2 SIN_TABLE_2_FP64 = vec2(0.5555702447891235, -1.1769521357507529e-8);
const vec2 SIN_TABLE_3_FP64 = vec2(0.7071067690849304, 1.2101617041793133e-8);

const vec2 COS_TABLE_0_FP64 = vec2(0.9807852506637573, 2.9739473106360492e-8);
const vec2 COS_TABLE_1_FP64 = vec2(0.9238795042037964, 2.8307490351764386e-8);
const vec2 COS_TABLE_2_FP64 = vec2(0.8314695954322815, 1.6870263741530778e-8);
const vec2 COS_TABLE_3_FP64 = vec2(0.7071067690849304, 1.2101617152815436e-8);

const vec2 INVERSE_FACTORIAL_3_FP64 = vec2(1.666666716337204e-01, -4.967053879312289e-09); // 1/3!
const vec2 INVERSE_FACTORIAL_4_FP64 = vec2(4.16666679084301e-02, -1.2417634698280722e-09); // 1/4!
const vec2 INVERSE_FACTORIAL_5_FP64 = vec2(8.333333767950535e-03, -4.34617203337595e-10); // 1/5!
const vec2 INVERSE_FACTORIAL_6_FP64 = vec2(1.3888889225199819e-03, -3.3631094437103215e-11); // 1/6!
const vec2 INVERSE_FACTORIAL_7_FP64 = vec2(1.9841270113829523e-04,  -2.725596874933456e-12); // 1/7!
const vec2 INVERSE_FACTORIAL_8_FP64 = vec2(2.4801587642286904e-05, -3.406996025904184e-13); // 1/8!
const vec2 INVERSE_FACTORIAL_9_FP64 = vec2(2.75573188446287533e-06, 3.7935713937038186e-14); // 1/9!
const vec2 INVERSE_FACTORIAL_10_FP64 = vec2(2.755731998149713e-07, -7.575112367869873e-15); // 1/10!

float nint(float d) {
    if (d == floor(d)) return d;
    return floor(d + 0.5);
}

vec2 nint_fp64(vec2 a) {
    float hi = nint(a.x);
    float lo;
    vec2 tmp;
    if (hi == a.x) {
        lo = nint(a.y);
        tmp = quickTwoSum(hi, lo);
    } else {
        lo = 0.0;
        if (abs(hi - a.x) == 0.5 && a.y < 0.0) {
            hi -= 1.0;
        }
        tmp = vec2(hi, lo);
    }
    return tmp;
}

/* k_power controls how much range reduction we would like to have
Range reduction uses the following method:
assume a = k_power * r + m * log(2), k and m being integers.
Set k_power = 4 (we can choose other k to trade accuracy with performance.
we only need to calculate exp(r) and using exp(a) = 2^m * exp(r)^k_power;
*/

vec2 exp_fp64(vec2 a) {
  // We need to make sure these two numbers match
  // as bit-wise shift is not available in GLSL 1.0
  const int k_power = 4;
  const float k = 16.0;

  const float inv_k = 1.0 / k;

  if (a.x <= -88.0) return vec2(0.0, 0.0);
  if (a.x >= 88.0) return vec2(1.0 / 0.0, 1.0 / 0.0);
  if (a.x == 0.0 && a.y == 0.0) return vec2(1.0, 0.0);
  if (a.x == 1.0 && a.y == 0.0) return E_FP64;

  float m = floor(a.x / LOG2_FP64.x + 0.5);
  vec2 r = sub_fp64(a, mul_fp64(LOG2_FP64, vec2(m, 0.0))) * inv_k;
  vec2 s, t, p;

  p = mul_fp64(r, r);
  s = sum_fp64(r, p * 0.5);
  p = mul_fp64(p, r);
  t = mul_fp64(p, INVERSE_FACTORIAL_3_FP64);

  s = sum_fp64(s, t);
  p = mul_fp64(p, r);
  t = mul_fp64(p, INVERSE_FACTORIAL_4_FP64);

  s = sum_fp64(s, t);
  p = mul_fp64(p, r);
  t = mul_fp64(p, INVERSE_FACTORIAL_5_FP64);

  // s = sum_fp64(s, t);
  // p = mul_fp64(p, r);
  // t = mul_fp64(p, INVERSE_FACTORIAL_6_FP64);

  // s = sum_fp64(s, t);
  // p = mul_fp64(p, r);
  // t = mul_fp64(p, INVERSE_FACTORIAL_7_FP64);

  s = sum_fp64(s, t);


  // At this point, s = exp(r) - 1; but after following 4 recursions, we will get exp(r) ^ 512 - 1.
  for (int i = 0; i < k_power; i++) {
    s = sum_fp64(s * 2.0, mul_fp64(s, s));
  }

#if defined(NVIDIA_FP64_WORKAROUND) || defined(INTEL_FP64_WORKAROUND)
  s = sum_fp64(s, vec2(fp64.ONE, 0.0));
#else
  s = sum_fp64(s, vec2(1.0, 0.0));
#endif

  return s * pow(2.0, m);
//   return r;
}

vec2 log_fp64(vec2 a)
{
  if (a.x == 1.0 && a.y == 0.0) return vec2(0.0, 0.0);
  if (a.x <= 0.0) return vec2(0.0 / 0.0, 0.0 / 0.0);
  vec2 x = vec2(log(a.x), 0.0);
  vec2 s;
#if defined(NVIDIA_FP64_WORKAROUND) || defined(INTEL_FP64_WORKAROUND)
  s = vec2(fp64.ONE, 0.0);
#else
  s = vec2(1.0, 0.0);
#endif

  x = sub_fp64(sum_fp64(x, mul_fp64(a, exp_fp64(-x))), s);
  return x;
}

vec2 sin_taylor_fp64(vec2 a) {
  vec2 r, s, t, x;

  if (a.x == 0.0 && a.y == 0.0) {
    return vec2(0.0, 0.0);
  }

  x = -mul_fp64(a, a);
  s = a;
  r = a;

  r = mul_fp64(r, x);
  t = mul_fp64(r, INVERSE_FACTORIAL_3_FP64);
  s = sum_fp64(s, t);

  r = mul_fp64(r, x);
  t = mul_fp64(r, INVERSE_FACTORIAL_5_FP64);
  s = sum_fp64(s, t);

  /* keep the following commented code in case we need them
  for extra accuracy from the Taylor expansion*/

  // r = mul_fp64(r, x);
  // t = mul_fp64(r, INVERSE_FACTORIAL_7_FP64);
  // s = sum_fp64(s, t);

  // r = mul_fp64(r, x);
  // t = mul_fp64(r, INVERSE_FACTORIAL_9_FP64);
  // s = sum_fp64(s, t);

  return s;
}

vec2 cos_taylor_fp64(vec2 a) {
  vec2 r, s, t, x;

  if (a.x == 0.0 && a.y == 0.0) {
    return vec2(1.0, 0.0);
  }

  x = -mul_fp64(a, a);
  r = x;
  s = sum_fp64(vec2(1.0, 0.0), r * 0.5);

  r = mul_fp64(r, x);
  t = mul_fp64(r, INVERSE_FACTORIAL_4_FP64);
  s = sum_fp64(s, t);

  r = mul_fp64(r, x);
  t = mul_fp64(r, INVERSE_FACTORIAL_6_FP64);
  s = sum_fp64(s, t);

  /* keep the following commented code in case we need them
  for extra accuracy from the Taylor expansion*/

  // r = mul_fp64(r, x);
  // t = mul_fp64(r, INVERSE_FACTORIAL_8_FP64);
  // s = sum_fp64(s, t);

  // r = mul_fp64(r, x);
  // t = mul_fp64(r, INVERSE_FACTORIAL_10_FP64);
  // s = sum_fp64(s, t);

  return s;
}

void sincos_taylor_fp64(vec2 a, out vec2 sin_t, out vec2 cos_t) {
  if (a.x == 0.0 && a.y == 0.0) {
    sin_t = vec2(0.0, 0.0);
    cos_t = vec2(1.0, 0.0);
  }

  sin_t = sin_taylor_fp64(a);
  cos_t = sqrt_fp64(sub_fp64(vec2(1.0, 0.0), mul_fp64(sin_t, sin_t)));
}

vec2 sin_fp64(vec2 a) {
    if (a.x == 0.0 && a.y == 0.0) {
        return vec2(0.0, 0.0);
    }

    // 2pi range reduction
    vec2 z = nint_fp64(div_fp64(a, TWO_PI_FP64));
    vec2 r = sub_fp64(a, mul_fp64(TWO_PI_FP64, z));

    vec2 t;
    float q = floor(r.x / PI_2_FP64.x + 0.5);
    int j = int(q);

    if (j < -2 || j > 2) {
        return vec2(0.0 / 0.0, 0.0 / 0.0);
    }

    t = sub_fp64(r, mul_fp64(PI_2_FP64, vec2(q, 0.0)));

    q = floor(t.x / PI_16_FP64.x + 0.5);
    int k = int(q);

    if (k == 0) {
        if (j == 0) {
            return sin_taylor_fp64(t);
        } else if (j == 1) {
            return cos_taylor_fp64(t);
        } else if (j == -1) {
            return -cos_taylor_fp64(t);
        } else {
            return -sin_taylor_fp64(t);
        }
    }

    int abs_k = int(abs(float(k)));

    if (abs_k > 4) {
        return vec2(0.0 / 0.0, 0.0 / 0.0);
    } else {
        t = sub_fp64(t, mul_fp64(PI_16_FP64, vec2(q, 0.0)));
    }

    vec2 u = vec2(0.0, 0.0);
    vec2 v = vec2(0.0, 0.0);

#if defined(NVIDIA_FP64_WORKAROUND) || defined(INTEL_FP64_WORKAROUND)
    if (abs(float(abs_k) - 1.0) < 0.5) {
        u = COS_TABLE_0_FP64;
        v = SIN_TABLE_0_FP64;
    } else if (abs(float(abs_k) - 2.0) < 0.5) {
        u = COS_TABLE_1_FP64;
        v = SIN_TABLE_1_FP64;
    } else if (abs(float(abs_k) - 3.0) < 0.5) {
        u = COS_TABLE_2_FP64;
        v = SIN_TABLE_2_FP64;
    } else if (abs(float(abs_k) - 4.0) < 0.5) {
        u = COS_TABLE_3_FP64;
        v = SIN_TABLE_3_FP64;
    }
#else
    if (abs_k == 1) {
        u = COS_TABLE_0_FP64;
        v = SIN_TABLE_0_FP64;
    } else if (abs_k == 2) {
        u = COS_TABLE_1_FP64;
        v = SIN_TABLE_1_FP64;
    } else if (abs_k == 3) {
        u = COS_TABLE_2_FP64;
        v = SIN_TABLE_2_FP64;
    } else if (abs_k == 4) {
        u = COS_TABLE_3_FP64;
        v = SIN_TABLE_3_FP64;
    }
#endif

    vec2 sin_t, cos_t;
    sincos_taylor_fp64(t, sin_t, cos_t);



    vec2 result = vec2(0.0, 0.0);
    if (j == 0) {
        if (k > 0) {
            result = sum_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
        } else {
            result = sub_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
        }
    } else if (j == 1) {
        if (k > 0) {
            result = sub_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        } else {
            result = sum_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        }
    } else if (j == -1) {
        if (k > 0) {
            result = sub_fp64(mul_fp64(v, sin_t), mul_fp64(u, cos_t));
        } else {
            result = -sum_fp64(mul_fp64(v, sin_t), mul_fp64(u, cos_t));
        }
    } else {
        if (k > 0) {
            result = -sum_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
        } else {
            result = sub_fp64(mul_fp64(v, cos_t), mul_fp64(u, sin_t));
        }
    }

    return result;
}

vec2 cos_fp64(vec2 a) {
    if (a.x == 0.0 && a.y == 0.0) {
        return vec2(1.0, 0.0);
    }

    // 2pi range reduction
    vec2 z = nint_fp64(div_fp64(a, TWO_PI_FP64));
    vec2 r = sub_fp64(a, mul_fp64(TWO_PI_FP64, z));

    vec2 t;
    float q = floor(r.x / PI_2_FP64.x + 0.5);
    int j = int(q);

    if (j < -2 || j > 2) {
        return vec2(0.0 / 0.0, 0.0 / 0.0);
    }

    t = sub_fp64(r, mul_fp64(PI_2_FP64, vec2(q, 0.0)));

    q = floor(t.x / PI_16_FP64.x + 0.5);
    int k = int(q);

    if (k == 0) {
        if (j == 0) {
            return cos_taylor_fp64(t);
        } else if (j == 1) {
            return -sin_taylor_fp64(t);
        } else if (j == -1) {
            return sin_taylor_fp64(t);
        } else {
            return -cos_taylor_fp64(t);
        }
    }

    int abs_k = int(abs(float(k)));

    if (abs_k > 4) {
        return vec2(0.0 / 0.0, 0.0 / 0.0);
    } else {
        t = sub_fp64(t, mul_fp64(PI_16_FP64, vec2(q, 0.0)));
    }

    vec2 u = vec2(0.0, 0.0);
    vec2 v = vec2(0.0, 0.0);

#if defined(NVIDIA_FP64_WORKAROUND) || defined(INTEL_FP64_WORKAROUND)
    if (abs(float(abs_k) - 1.0) < 0.5) {
        u = COS_TABLE_0_FP64;
        v = SIN_TABLE_0_FP64;
    } else if (abs(float(abs_k) - 2.0) < 0.5) {
        u = COS_TABLE_1_FP64;
        v = SIN_TABLE_1_FP64;
    } else if (abs(float(abs_k) - 3.0) < 0.5) {
        u = COS_TABLE_2_FP64;
        v = SIN_TABLE_2_FP64;
    } else if (abs(float(abs_k) - 4.0) < 0.5) {
        u = COS_TABLE_3_FP64;
        v = SIN_TABLE_3_FP64;
    }
#else
    if (abs_k == 1) {
        u = COS_TABLE_0_FP64;
        v = SIN_TABLE_0_FP64;
    } else if (abs_k == 2) {
        u = COS_TABLE_1_FP64;
        v = SIN_TABLE_1_FP64;
    } else if (abs_k == 3) {
        u = COS_TABLE_2_FP64;
        v = SIN_TABLE_2_FP64;
    } else if (abs_k == 4) {
        u = COS_TABLE_3_FP64;
        v = SIN_TABLE_3_FP64;
    }
#endif

    vec2 sin_t, cos_t;
    sincos_taylor_fp64(t, sin_t, cos_t);

    vec2 result = vec2(0.0, 0.0);
    if (j == 0) {
        if (k > 0) {
            result = sub_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        } else {
            result = sum_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        }
    } else if (j == 1) {
        if (k > 0) {
            result = -sum_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
        } else {
            result = sub_fp64(mul_fp64(v, cos_t), mul_fp64(u, sin_t));
        }
    } else if (j == -1) {
        if (k > 0) {
            result = sum_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
        } else {
            result = sub_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
        }
    } else {
        if (k > 0) {
            result = sub_fp64(mul_fp64(v, sin_t), mul_fp64(u, cos_t));
        } else {
            result = -sum_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        }
    }

    return result;
}

vec2 tan_fp64(vec2 a) {
    vec2 sin_a;
    vec2 cos_a;

    if (a.x == 0.0 && a.y == 0.0) {
        return vec2(0.0, 0.0);
    }

    // 2pi range reduction
    vec2 z = nint_fp64(div_fp64(a, TWO_PI_FP64));
    vec2 r = sub_fp64(a, mul_fp64(TWO_PI_FP64, z));

    vec2 t;
    float q = floor(r.x / PI_2_FP64.x + 0.5);
    int j = int(q);


    if (j < -2 || j > 2) {
        return vec2(0.0 / 0.0, 0.0 / 0.0);
    }

    t = sub_fp64(r, mul_fp64(PI_2_FP64, vec2(q, 0.0)));

    q = floor(t.x / PI_16_FP64.x + 0.5);
    int k = int(q);
    int abs_k = int(abs(float(k)));

    // We just can't get PI/16 * 3.0 very accurately.
    // so let's just store it
    if (abs_k > 4) {
        return vec2(0.0 / 0.0, 0.0 / 0.0);
    } else {
        t = sub_fp64(t, mul_fp64(PI_16_FP64, vec2(q, 0.0)));
    }


    vec2 u = vec2(0.0, 0.0);
    vec2 v = vec2(0.0, 0.0);

    vec2 sin_t, cos_t;
    vec2 s, c;
    sincos_taylor_fp64(t, sin_t, cos_t);

    if (k == 0) {
        s = sin_t;
        c = cos_t;
    } else {
#if defined(NVIDIA_FP64_WORKAROUND) || defined(INTEL_FP64_WORKAROUND)
        if (abs(float(abs_k) - 1.0) < 0.5) {
            u = COS_TABLE_0_FP64;
            v = SIN_TABLE_0_FP64;
        } else if (abs(float(abs_k) - 2.0) < 0.5) {
            u = COS_TABLE_1_FP64;
            v = SIN_TABLE_1_FP64;
        } else if (abs(float(abs_k) - 3.0) < 0.5) {
            u = COS_TABLE_2_FP64;
            v = SIN_TABLE_2_FP64;
        } else if (abs(float(abs_k) - 4.0) < 0.5) {
            u = COS_TABLE_3_FP64;
            v = SIN_TABLE_3_FP64;
        }
#else
        if (abs_k == 1) {
            u = COS_TABLE_0_FP64;
            v = SIN_TABLE_0_FP64;
        } else if (abs_k == 2) {
            u = COS_TABLE_1_FP64;
            v = SIN_TABLE_1_FP64;
        } else if (abs_k == 3) {
            u = COS_TABLE_2_FP64;
            v = SIN_TABLE_2_FP64;
        } else if (abs_k == 4) {
            u = COS_TABLE_3_FP64;
            v = SIN_TABLE_3_FP64;
        }
#endif
        if (k > 0) {
            s = sum_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
            c = sub_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        } else {
            s = sub_fp64(mul_fp64(u, sin_t), mul_fp64(v, cos_t));
            c = sum_fp64(mul_fp64(u, cos_t), mul_fp64(v, sin_t));
        }
    }

    if (j == 0) {
        sin_a = s;
        cos_a = c;
    } else if (j == 1) {
        sin_a = c;
        cos_a = -s;
    } else if (j == -1) {
        sin_a = -c;
        cos_a = s;
    } else {
        sin_a = -s;
        cos_a = -c;
    }
    return div_fp64(sin_a, cos_a);
}

vec2 radians_fp64(vec2 degree) {
  return mul_fp64(degree, PI_180_FP64);
}

vec2 mix_fp64(vec2 a, vec2 b, float x) {
  vec2 range = sub_fp64(b, a);
  return sum_fp64(a, mul_fp64(range, vec2(x, 0.0)));
}

// Vector functions
// vec2 functions
void vec2_sum_fp64(vec2 a[2], vec2 b[2], out vec2 out_val[2]) {
    out_val[0] = sum_fp64(a[0], b[0]);
    out_val[1] = sum_fp64(a[1], b[1]);
}

void vec2_sub_fp64(vec2 a[2], vec2 b[2], out vec2 out_val[2]) {
    out_val[0] = sub_fp64(a[0], b[0]);
    out_val[1] = sub_fp64(a[1], b[1]);
}

void vec2_mul_fp64(vec2 a[2], vec2 b[2], out vec2 out_val[2]) {
    out_val[0] = mul_fp64(a[0], b[0]);
    out_val[1] = mul_fp64(a[1], b[1]);
}

void vec2_div_fp64(vec2 a[2], vec2 b[2], out vec2 out_val[2]) {
    out_val[0] = div_fp64(a[0], b[0]);
    out_val[1] = div_fp64(a[1], b[1]);
}

void vec2_mix_fp64(vec2 x[2], vec2 y[2], float a, out vec2 out_val[2]) {
  vec2 range[2];
  vec2_sub_fp64(y, x, range);
  vec2 portion[2];
  portion[0] = range[0] * a;
  portion[1] = range[1] * a;
  vec2_sum_fp64(x, portion, out_val);
}

vec2 vec2_length_fp64(vec2 x[2]) {
  return sqrt_fp64(sum_fp64(mul_fp64(x[0], x[0]), mul_fp64(x[1], x[1])));
}

void vec2_normalize_fp64(vec2 x[2], out vec2 out_val[2]) {
  vec2 length = vec2_length_fp64(x);
  vec2 length_vec2[2];
  length_vec2[0] = length;
  length_vec2[1] = length;

  vec2_div_fp64(x, length_vec2, out_val);
}

vec2 vec2_distance_fp64(vec2 x[2], vec2 y[2]) {
  vec2 diff[2];
  vec2_sub_fp64(x, y, diff);
  return vec2_length_fp64(diff);
}

vec2 vec2_dot_fp64(vec2 a[2], vec2 b[2]) {
  vec2 v[2];

  v[0] = mul_fp64(a[0], b[0]);
  v[1] = mul_fp64(a[1], b[1]);

  return sum_fp64(v[0], v[1]);
}

// vec3 functions
void vec3_sub_fp64(vec2 a[3], vec2 b[3], out vec2 out_val[3]) {
  for (int i = 0; i < 3; i++) {
    out_val[i] = sum_fp64(a[i], b[i]);
  }
}

void vec3_sum_fp64(vec2 a[3], vec2 b[3], out vec2 out_val[3]) {
  for (int i = 0; i < 3; i++) {
    out_val[i] = sum_fp64(a[i], b[i]);
  }
}

vec2 vec3_length_fp64(vec2 x[3]) {
  return sqrt_fp64(sum_fp64(sum_fp64(mul_fp64(x[0], x[0]), mul_fp64(x[1], x[1])),
    mul_fp64(x[2], x[2])));
}

vec2 vec3_distance_fp64(vec2 x[3], vec2 y[3]) {
  vec2 diff[3];
  vec3_sub_fp64(x, y, diff);
  return vec3_length_fp64(diff);
}

// vec4 functions
void vec4_fp64(vec4 a, out vec2 out_val[4]) {
  out_val[0].x = a[0];
  out_val[0].y = 0.0;

  out_val[1].x = a[1];
  out_val[1].y = 0.0;

  out_val[2].x = a[2];
  out_val[2].y = 0.0;

  out_val[3].x = a[3];
  out_val[3].y = 0.0;
}

void vec4_scalar_mul_fp64(vec2 a[4], vec2 b, out vec2 out_val[4]) {
  out_val[0] = mul_fp64(a[0], b);
  out_val[1] = mul_fp64(a[1], b);
  out_val[2] = mul_fp64(a[2], b);
  out_val[3] = mul_fp64(a[3], b);
}

void vec4_sum_fp64(vec2 a[4], vec2 b[4], out vec2 out_val[4]) {
  for (int i = 0; i < 4; i++) {
    out_val[i] = sum_fp64(a[i], b[i]);
  }
}

void vec4_dot_fp64(vec2 a[4], vec2 b[4], out vec2 out_val) {
  vec2 v[4];

  v[0] = mul_fp64(a[0], b[0]);
  v[1] = mul_fp64(a[1], b[1]);
  v[2] = mul_fp64(a[2], b[2]);
  v[3] = mul_fp64(a[3], b[3]);

  out_val = sum_fp64(sum_fp64(v[0], v[1]), sum_fp64(v[2], v[3]));
}

void mat4_vec4_mul_fp64(vec2 b[16], vec2 a[4], out vec2 out_val[4]) {
  vec2 tmp[4];

  for (int i = 0; i < 4; i++)
  {
    for (int j = 0; j < 4; j++)
    {
      tmp[j] = b[j + i * 4];
    }
    vec4_dot_fp64(a, tmp, out_val[i]);
  }
}
`
);

// dist/modules/math/fp64/fp64.js
var defaultUniforms = {
  // Used in LUMA_FP64_CODE_ELIMINATION_WORKAROUND
  ONE: 1
};
var fp64arithmetic = {
  name: "fp64arithmetic",
  vs: fp64arithmeticShader,
  defaultUniforms,
  uniformTypes: { ONE: "f32" },
  // Additional Functions
  fp64ify,
  fp64LowPart,
  fp64ifyMatrix4
};
var fp64 = {
  name: "fp64",
  vs: fp64functionShader,
  dependencies: [fp64arithmetic],
  // Additional Functions
  fp64ify,
  fp64LowPart,
  fp64ifyMatrix4
};

// dist/modules/engine/picking/picking.js
var DEFAULT_HIGHLIGHT_COLOR = [0, 1, 1, 1];
var vs = (
  /* glsl */
  `uniform pickingUniforms {
  float isActive;
  float isAttribute;
  float isHighlightActive;
  float useFloatColors;
  vec3 highlightedObjectColor;
  vec4 highlightColor;
} picking;

out vec4 picking_vRGBcolor_Avalid;

// Normalize unsigned byte color to 0-1 range
vec3 picking_normalizeColor(vec3 color) {
  return picking.useFloatColors > 0.5 ? color : color / 255.0;
}

// Normalize unsigned byte color to 0-1 range
vec4 picking_normalizeColor(vec4 color) {
  return picking.useFloatColors > 0.5 ? color : color / 255.0;
}

bool picking_isColorZero(vec3 color) {
  return dot(color, vec3(1.0)) < 0.00001;
}

bool picking_isColorValid(vec3 color) {
  return dot(color, vec3(1.0)) > 0.00001;
}

// Check if this vertex is highlighted 
bool isVertexHighlighted(vec3 vertexColor) {
  vec3 highlightedObjectColor = picking_normalizeColor(picking.highlightedObjectColor);
  return
    bool(picking.isHighlightActive) && picking_isColorZero(abs(vertexColor - highlightedObjectColor));
}

// Set the current picking color
void picking_setPickingColor(vec3 pickingColor) {
  pickingColor = picking_normalizeColor(pickingColor);

  if (bool(picking.isActive)) {
    // Use alpha as the validity flag. If pickingColor is [0, 0, 0] fragment is non-pickable
    picking_vRGBcolor_Avalid.a = float(picking_isColorValid(pickingColor));

    if (!bool(picking.isAttribute)) {
      // Stores the picking color so that the fragment shader can render it during picking
      picking_vRGBcolor_Avalid.rgb = pickingColor;
    }
  } else {
    // Do the comparison with selected item color in vertex shader as it should mean fewer compares
    picking_vRGBcolor_Avalid.a = float(isVertexHighlighted(pickingColor));
  }
}

void picking_setPickingAttribute(float value) {
  if (bool(picking.isAttribute)) {
    picking_vRGBcolor_Avalid.r = value;
  }
}

void picking_setPickingAttribute(vec2 value) {
  if (bool(picking.isAttribute)) {
    picking_vRGBcolor_Avalid.rg = value;
  }
}

void picking_setPickingAttribute(vec3 value) {
  if (bool(picking.isAttribute)) {
    picking_vRGBcolor_Avalid.rgb = value;
  }
}
`
);
var fs2 = (
  /* glsl */
  `uniform pickingUniforms {
  float isActive;
  float isAttribute;
  float isHighlightActive;
  float useFloatColors;
  vec3 highlightedObjectColor;
  vec4 highlightColor;
} picking;

in vec4 picking_vRGBcolor_Avalid;

/*
 * Returns highlight color if this item is selected.
 */
vec4 picking_filterHighlightColor(vec4 color) {
  // If we are still picking, we don't highlight
  if (picking.isActive > 0.5) {
    return color;
  }

  bool selected = bool(picking_vRGBcolor_Avalid.a);

  if (selected) {
    // Blend in highlight color based on its alpha value
    float highLightAlpha = picking.highlightColor.a;
    float blendedAlpha = highLightAlpha + color.a * (1.0 - highLightAlpha);
    float highLightRatio = highLightAlpha / blendedAlpha;

    vec3 blendedRGB = mix(color.rgb, picking.highlightColor.rgb, highLightRatio);
    return vec4(blendedRGB, blendedAlpha);
  } else {
    return color;
  }
}

/*
 * Returns picking color if picking enabled else unmodified argument.
 */
vec4 picking_filterPickingColor(vec4 color) {
  if (bool(picking.isActive)) {
    if (picking_vRGBcolor_Avalid.a == 0.0) {
      discard;
    }
    return picking_vRGBcolor_Avalid;
  }
  return color;
}

/*
 * Returns picking color if picking is enabled if not
 * highlight color if this item is selected, otherwise unmodified argument.
 */
vec4 picking_filterColor(vec4 color) {
  vec4 highlightColor = picking_filterHighlightColor(color);
  return picking_filterPickingColor(highlightColor);
}
`
);
var picking = {
  props: {},
  uniforms: {},
  name: "picking",
  uniformTypes: {
    isActive: "f32",
    isAttribute: "f32",
    isHighlightActive: "f32",
    useFloatColors: "f32",
    highlightedObjectColor: "vec3<f32>",
    highlightColor: "vec4<f32>"
  },
  defaultUniforms: {
    isActive: false,
    isAttribute: false,
    isHighlightActive: false,
    useFloatColors: true,
    highlightedObjectColor: [0, 0, 0],
    highlightColor: DEFAULT_HIGHLIGHT_COLOR
  },
  vs,
  fs: fs2,
  getUniforms
};
function getUniforms(opts = {}, prevUniforms) {
  const uniforms = {};
  if (opts.highlightedObjectColor === void 0) {
  } else if (opts.highlightedObjectColor === null) {
    uniforms.isHighlightActive = false;
  } else {
    uniforms.isHighlightActive = true;
    const highlightedObjectColor = opts.highlightedObjectColor.slice(0, 3);
    uniforms.highlightedObjectColor = highlightedObjectColor;
  }
  if (opts.highlightColor) {
    const color = Array.from(opts.highlightColor, (x) => x / 255);
    if (!Number.isFinite(color[3])) {
      color[3] = 1;
    }
    uniforms.highlightColor = color;
  }
  if (opts.isActive !== void 0) {
    uniforms.isActive = Boolean(opts.isActive);
    uniforms.isAttribute = Boolean(opts.isAttribute);
  }
  if (opts.useFloatColors !== void 0) {
    uniforms.useFloatColors = Boolean(opts.useFloatColors);
  }
  return uniforms;
}

// dist/modules/lighting/lights/lighting.js
var import_core3 = require("@luma.gl/core");

// dist/modules/lighting/lights/lighting-glsl.js
var lightingUniformsGLSL = (
  /* glsl */
  `precision highp int;

// #if (defined(SHADER_TYPE_FRAGMENT) && defined(LIGHTING_FRAGMENT)) || (defined(SHADER_TYPE_VERTEX) && defined(LIGHTING_VERTEX))
struct AmbientLight {
  vec3 color;
};

struct PointLight {
  vec3 color;
  vec3 position;
  vec3 attenuation; // 2nd order x:Constant-y:Linear-z:Exponential
};

struct DirectionalLight {
  vec3 color;
  vec3 direction;
};

uniform lightingUniforms {
  int enabled;
  int lightType;

  int directionalLightCount;
  int pointLightCount;

  vec3 ambientColor;

  vec3 lightColor0;
  vec3 lightPosition0;
  vec3 lightDirection0;
  vec3 lightAttenuation0;

  vec3 lightColor1;
  vec3 lightPosition1;
  vec3 lightDirection1;
  vec3 lightAttenuation1;

  vec3 lightColor2;
  vec3 lightPosition2;
  vec3 lightDirection2;
  vec3 lightAttenuation2;
} lighting;

PointLight lighting_getPointLight(int index) {
  switch (index) {
    case 0:
      return PointLight(lighting.lightColor0, lighting.lightPosition0, lighting.lightAttenuation0);
    case 1:
      return PointLight(lighting.lightColor1, lighting.lightPosition1, lighting.lightAttenuation1);
    case 2:
    default:  
      return PointLight(lighting.lightColor2, lighting.lightPosition2, lighting.lightAttenuation2);
  }
}

DirectionalLight lighting_getDirectionalLight(int index) {
  switch (index) {
    case 0:
      return DirectionalLight(lighting.lightColor0, lighting.lightDirection0);
    case 1:
      return DirectionalLight(lighting.lightColor1, lighting.lightDirection1);
    case 2:
    default:   
      return DirectionalLight(lighting.lightColor2, lighting.lightDirection2);
  }
} 

float getPointLightAttenuation(PointLight pointLight, float distance) {
  return pointLight.attenuation.x
       + pointLight.attenuation.y * distance
       + pointLight.attenuation.z * distance * distance;
}

// #endif
`
);

// dist/modules/lighting/lights/lighting-wgsl.js
var lightingUniformsWGSL = (
  /* wgsl */
  `// #if (defined(SHADER_TYPE_FRAGMENT) && defined(LIGHTING_FRAGMENT)) || (defined(SHADER_TYPE_VERTEX) && defined(LIGHTING_VERTEX))
struct AmbientLight {
  color: vec3<f32>,
};

struct PointLight {
  color: vec3<f32>,
  position: vec3<f32>,
  attenuation: vec3<f32>, // 2nd order x:Constant-y:Linear-z:Exponential
};

struct DirectionalLight {
  color: vec3<f32>,
  direction: vec3<f32>,
};

struct lightingUniforms {
  enabled: i32,
  pointLightCount: i32,
  directionalLightCount: i32,

  ambientColor: vec3<f32>,

  // TODO - support multiple lights by uncommenting arrays below
  lightType: i32,
  lightColor: vec3<f32>,
  lightDirection: vec3<f32>,
  lightPosition: vec3<f32>,
  lightAttenuation: vec3<f32>,

  // AmbientLight ambientLight;
  // PointLight pointLight[MAX_LIGHTS];
  // DirectionalLight directionalLight[MAX_LIGHTS];
};

// Binding 0:1 is reserved for lighting (Note: could go into separate bind group as it is stable across draw calls)
@binding(1) @group(0) var<uniform> lighting : lightingUniforms;

fn lighting_getPointLight(index: i32) -> PointLight {
  return PointLight(lighting.lightColor, lighting.lightPosition, lighting.lightAttenuation);
}

fn lighting_getDirectionalLight(index: i32) -> DirectionalLight {
  return DirectionalLight(lighting.lightColor, lighting.lightDirection);
} 

fn getPointLightAttenuation(pointLight: PointLight, distance: f32) -> f32 {
  return pointLight.attenuation.x
       + pointLight.attenuation.y * distance
       + pointLight.attenuation.z * distance * distance;
}
`
);

// dist/modules/lighting/lights/lighting.js
var MAX_LIGHTS = 5;
var COLOR_FACTOR = 255;
var LIGHT_TYPE;
(function(LIGHT_TYPE2) {
  LIGHT_TYPE2[LIGHT_TYPE2["POINT"] = 0] = "POINT";
  LIGHT_TYPE2[LIGHT_TYPE2["DIRECTIONAL"] = 1] = "DIRECTIONAL";
})(LIGHT_TYPE || (LIGHT_TYPE = {}));
var lighting = {
  props: {},
  uniforms: {},
  name: "lighting",
  defines: {
    // MAX_LIGHTS
  },
  uniformTypes: {
    enabled: "i32",
    lightType: "i32",
    directionalLightCount: "i32",
    pointLightCount: "i32",
    ambientColor: "vec3<f32>",
    // TODO define as arrays once we have appropriate uniformTypes
    lightColor0: "vec3<f32>",
    lightPosition0: "vec3<f32>",
    // TODO - could combine direction and attenuation
    lightDirection0: "vec3<f32>",
    lightAttenuation0: "vec3<f32>",
    lightColor1: "vec3<f32>",
    lightPosition1: "vec3<f32>",
    lightDirection1: "vec3<f32>",
    lightAttenuation1: "vec3<f32>",
    lightColor2: "vec3<f32>",
    lightPosition2: "vec3<f32>",
    lightDirection2: "vec3<f32>",
    lightAttenuation2: "vec3<f32>"
  },
  defaultUniforms: {
    enabled: 1,
    lightType: LIGHT_TYPE.POINT,
    directionalLightCount: 0,
    pointLightCount: 0,
    ambientColor: [0.1, 0.1, 0.1],
    lightColor0: [1, 1, 1],
    lightPosition0: [1, 1, 2],
    // TODO - could combine direction and attenuation
    lightDirection0: [1, 1, 1],
    lightAttenuation0: [1, 0, 0],
    lightColor1: [1, 1, 1],
    lightPosition1: [1, 1, 2],
    lightDirection1: [1, 1, 1],
    lightAttenuation1: [1, 0, 0],
    lightColor2: [1, 1, 1],
    lightPosition2: [1, 1, 2],
    lightDirection2: [1, 1, 1],
    lightAttenuation2: [1, 0, 0]
  },
  source: lightingUniformsWGSL,
  vs: lightingUniformsGLSL,
  fs: lightingUniformsGLSL,
  getUniforms: getUniforms2
};
function getUniforms2(props, prevUniforms = {}) {
  props = props ? { ...props } : props;
  if (!props) {
    return { ...lighting.defaultUniforms };
  }
  if (props.lights) {
    props = { ...props, ...extractLightTypes(props.lights), lights: void 0 };
  }
  const { ambientLight, pointLights, directionalLights } = props || {};
  const hasLights = ambientLight || pointLights && pointLights.length > 0 || directionalLights && directionalLights.length > 0;
  if (!hasLights) {
    return { ...lighting.defaultUniforms, enabled: 0 };
  }
  const uniforms = {
    ...lighting.defaultUniforms,
    ...prevUniforms,
    ...getLightSourceUniforms({ ambientLight, pointLights, directionalLights })
  };
  if (props.enabled !== void 0) {
    uniforms.enabled = props.enabled ? 1 : 0;
  }
  return uniforms;
}
function getLightSourceUniforms({ ambientLight, pointLights = [], directionalLights = [] }) {
  const lightSourceUniforms = {};
  lightSourceUniforms.ambientColor = convertColor(ambientLight);
  let currentLight = 0;
  for (const pointLight of pointLights) {
    lightSourceUniforms.lightType = LIGHT_TYPE.POINT;
    const i = currentLight;
    lightSourceUniforms[`lightColor${i}`] = convertColor(pointLight);
    lightSourceUniforms[`lightPosition${i}`] = pointLight.position;
    lightSourceUniforms[`lightAttenuation${i}`] = pointLight.attenuation || [1, 0, 0];
    currentLight++;
  }
  for (const directionalLight of directionalLights) {
    lightSourceUniforms.lightType = LIGHT_TYPE.DIRECTIONAL;
    const i = currentLight;
    lightSourceUniforms[`lightColor${i}`] = convertColor(directionalLight);
    lightSourceUniforms[`lightDirection${i}`] = directionalLight.direction;
    currentLight++;
  }
  if (currentLight > MAX_LIGHTS) {
    import_core3.log.warn("MAX_LIGHTS exceeded")();
  }
  lightSourceUniforms.directionalLightCount = directionalLights.length;
  lightSourceUniforms.pointLightCount = pointLights.length;
  return lightSourceUniforms;
}
function extractLightTypes(lights) {
  var _a, _b;
  const lightSources = { pointLights: [], directionalLights: [] };
  for (const light of lights || []) {
    switch (light.type) {
      case "ambient":
        lightSources.ambientLight = light;
        break;
      case "directional":
        (_a = lightSources.directionalLights) == null ? void 0 : _a.push(light);
        break;
      case "point":
        (_b = lightSources.pointLights) == null ? void 0 : _b.push(light);
        break;
      default:
    }
  }
  return lightSources;
}
function convertColor(colorDef = {}) {
  const { color = [0, 0, 0], intensity = 1 } = colorDef;
  return color.map((component) => component * intensity / COLOR_FACTOR);
}

// dist/modules/lighting/no-material/dirlight.js
var SOURCE_WGSL = (
  /* WGSL */
  `  
struct dirlightUniforms {
  lightDirection: vec3<f32>,
};

alias DirlightNormal = vec3<f32>;

struct DirlightInputs {
  normal: DirlightNormal,
};

@binding(1) @group(0) var<uniform> dirlight : dirlightUniforms;

// For vertex
fn dirlight_setNormal(normal: vec3<f32>) -> DirlightNormal {
  return normalize(normal);
}

// Returns color attenuated by angle from light source
fn dirlight_filterColor(color: vec4<f32>, inputs: DirlightInputs) -> vec4<f32> {
  // TODO - fix default light direction
  // let lightDirection = dirlight.lightDirection;
  let lightDirection = vec3<f32>(1, 1, 1);
  let d: f32 = abs(dot(inputs.normal, normalize(lightDirection)));
  return vec4<f32>(color.rgb * d, color.a);
}
`
);
var VS_GLSL = (
  /* glsl */
  `out vec3 dirlight_vNormal;

void dirlight_setNormal(vec3 normal) {
  dirlight_vNormal = normalize(normal);
}
`
);
var FS_GLSL = (
  /* glsl */
  `uniform dirlightUniforms {
  vec3 lightDirection;
} dirlight;

in vec3 dirlight_vNormal;

// Returns color attenuated by angle from light source
vec4 dirlight_filterColor(vec4 color) {
  float d = abs(dot(dirlight_vNormal, normalize(dirlight.lightDirection)));
  return vec4(color.rgb * d, color.a);
}
`
);
var dirlight = {
  props: {},
  uniforms: {},
  name: "dirlight",
  dependencies: [],
  source: SOURCE_WGSL,
  vs: VS_GLSL,
  fs: FS_GLSL,
  // fragmentInputs: [
  //   {
  //     name: 'dirlight_vNormal',
  //     type: 'vec3<f32>'
  //   }
  // ],
  uniformTypes: {
    lightDirection: "vec3<f32>"
  },
  defaultUniforms: {
    lightDirection: [1, 1, 2]
  },
  getUniforms: getUniforms3
};
function getUniforms3(opts = dirlight.defaultUniforms) {
  const uniforms = {};
  if (opts.lightDirection) {
    uniforms.lightDirection = opts.lightDirection;
  }
  return uniforms;
}

// dist/modules/lighting/phong-material/phong-shaders-glsl.js
var PHONG_VS = (
  /* glsl */
  `uniform phongMaterialUniforms {
  uniform float ambient;
  uniform float diffuse;
  uniform float shininess;
  uniform vec3  specularColor;
} material;
`
);
var PHONG_FS = (
  /* glsl */
  `#define MAX_LIGHTS 3

uniform phongMaterialUniforms {
  uniform float ambient;
  uniform float diffuse;
  uniform float shininess;
  uniform vec3  specularColor;
} material;

vec3 lighting_getLightColor(vec3 surfaceColor, vec3 light_direction, vec3 view_direction, vec3 normal_worldspace, vec3 color) {
  vec3 halfway_direction = normalize(light_direction + view_direction);
  float lambertian = dot(light_direction, normal_worldspace);
  float specular = 0.0;
  if (lambertian > 0.0) {
    float specular_angle = max(dot(normal_worldspace, halfway_direction), 0.0);
    specular = pow(specular_angle, material.shininess);
  }
  lambertian = max(lambertian, 0.0);
  return (lambertian * material.diffuse * surfaceColor + specular * material.specularColor) * color;
}

vec3 lighting_getLightColor(vec3 surfaceColor, vec3 cameraPosition, vec3 position_worldspace, vec3 normal_worldspace) {
  vec3 lightColor = surfaceColor;

  if (lighting.enabled == 0) {
    return lightColor;
  }

  vec3 view_direction = normalize(cameraPosition - position_worldspace);
  lightColor = material.ambient * surfaceColor * lighting.ambientColor;

  for (int i = 0; i < lighting.pointLightCount; i++) {
    PointLight pointLight = lighting_getPointLight(i);
    vec3 light_position_worldspace = pointLight.position;
    vec3 light_direction = normalize(light_position_worldspace - position_worldspace);
    float light_attenuation = getPointLightAttenuation(pointLight, distance(light_position_worldspace, position_worldspace));
    lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color / light_attenuation);
  }

  int totalLights = min(MAX_LIGHTS, lighting.pointLightCount + lighting.directionalLightCount);
  for (int i = lighting.pointLightCount; i < totalLights; i++) {
    DirectionalLight directionalLight = lighting_getDirectionalLight(i);
    lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);
  }
  
  return lightColor;
}
`
);

// dist/modules/lighting/phong-material/phong-shaders-wgsl.js
var PHONG_WGSL = (
  /* wgsl */
  `struct phongMaterialUniforms {
  ambient: f32,
  diffuse: f32,
  shininess: f32,
  specularColor: vec3<f32>,
};

@binding(2) @group(0) var<uniform> phongMaterial : phongMaterialUniforms;

fn lighting_getLightColor(surfaceColor: vec3<f32>, light_direction: vec3<f32>, view_direction: vec3<f32>, normal_worldspace: vec3<f32>, color: vec3<f32>) -> vec3<f32> {
  let halfway_direction: vec3<f32> = normalize(light_direction + view_direction);
  var lambertian: f32 = dot(light_direction, normal_worldspace);
  var specular: f32 = 0.0;
  if (lambertian > 0.0) {
    let specular_angle = max(dot(normal_worldspace, halfway_direction), 0.0);
    specular = pow(specular_angle, phongMaterial.shininess);
  }
  lambertian = max(lambertian, 0.0);
  return (lambertian * phongMaterial.diffuse * surfaceColor + specular * phongMaterial.specularColor) * color;
}

fn lighting_getLightColor2(surfaceColor: vec3<f32>, cameraPosition: vec3<f32>, position_worldspace: vec3<f32>, normal_worldspace: vec3<f32>) -> vec3<f32> {
  var lightColor: vec3<f32> = surfaceColor;

  if (lighting.enabled == 0) {
    return lightColor;
  }

  let view_direction: vec3<f32> = normalize(cameraPosition - position_worldspace);
  lightColor = phongMaterial.ambient * surfaceColor * lighting.ambientColor;

  if (lighting.lightType == 0) {
    let pointLight: PointLight  = lighting_getPointLight(0);
    let light_position_worldspace: vec3<f32> = pointLight.position;
    let light_direction: vec3<f32> = normalize(light_position_worldspace - position_worldspace);
    lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color);
  } else if (lighting.lightType == 1) {
    var directionalLight: DirectionalLight = lighting_getDirectionalLight(0);
    lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);
  }
  
  return lightColor;
  /*
  for (int i = 0; i < MAX_LIGHTS; i++) {
    if (i >= lighting.pointLightCount) {
      break;
    }
    PointLight pointLight = lighting.pointLight[i];
    vec3 light_position_worldspace = pointLight.position;
    vec3 light_direction = normalize(light_position_worldspace - position_worldspace);
    lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color);
  }

  for (int i = 0; i < MAX_LIGHTS; i++) {
    if (i >= lighting.directionalLightCount) {
      break;
    }
    DirectionalLight directionalLight = lighting.directionalLight[i];
    lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);
  }
  */
}

fn lighting_getSpecularLightColor(cameraPosition: vec3<f32>, position_worldspace: vec3<f32>, normal_worldspace: vec3<f32>) -> vec3<f32>{
  var lightColor = vec3<f32>(0, 0, 0);
  let surfaceColor = vec3<f32>(0, 0, 0);

  if (lighting.enabled == 0) {
    let view_direction = normalize(cameraPosition - position_worldspace);

    switch (lighting.lightType) {
      case 0, default: {
        let pointLight: PointLight = lighting_getPointLight(0);
        let light_position_worldspace: vec3<f32> = pointLight.position;
        let light_direction: vec3<f32> = normalize(light_position_worldspace - position_worldspace);
        lightColor += lighting_getLightColor(surfaceColor, light_direction, view_direction, normal_worldspace, pointLight.color);
      }
      case 1: {
        let directionalLight: DirectionalLight = lighting_getDirectionalLight(0);
        lightColor += lighting_getLightColor(surfaceColor, -directionalLight.direction, view_direction, normal_worldspace, directionalLight.color);
      }
    }
  }
  return lightColor;
}
`
);

// dist/modules/lighting/gouraud-material/gouraud-material.js
var gouraudMaterial = {
  props: {},
  name: "gouraudMaterial",
  // Note these are switched between phong and gouraud
  vs: PHONG_FS.replace("phongMaterial", "gouraudMaterial"),
  fs: PHONG_VS.replace("phongMaterial", "gouraudMaterial"),
  source: PHONG_WGSL.replaceAll("phongMaterial", "gouraudMaterial"),
  defines: {
    LIGHTING_VERTEX: true
  },
  dependencies: [lighting],
  uniformTypes: {
    ambient: "f32",
    diffuse: "f32",
    shininess: "f32",
    specularColor: "vec3<f32>"
  },
  defaultUniforms: {
    ambient: 0.35,
    diffuse: 0.6,
    shininess: 32,
    specularColor: [0.15, 0.15, 0.15]
  },
  getUniforms(props) {
    const uniforms = { ...props };
    if (uniforms.specularColor) {
      uniforms.specularColor = uniforms.specularColor.map((x) => x / 255);
    }
    return { ...gouraudMaterial.defaultUniforms, ...uniforms };
  }
};

// dist/modules/lighting/phong-material/phong-material.js
var phongMaterial = {
  name: "phongMaterial",
  dependencies: [lighting],
  // Note these are switched between phong and gouraud
  source: PHONG_WGSL,
  vs: PHONG_VS,
  fs: PHONG_FS,
  defines: {
    LIGHTING_FRAGMENT: true
  },
  uniformTypes: {
    ambient: "f32",
    diffuse: "f32",
    shininess: "f32",
    specularColor: "vec3<f32>"
  },
  defaultUniforms: {
    ambient: 0.35,
    diffuse: 0.6,
    shininess: 32,
    specularColor: [0.15, 0.15, 0.15]
  },
  getUniforms(props) {
    const uniforms = { ...props };
    if (uniforms.specularColor) {
      uniforms.specularColor = uniforms.specularColor.map((x) => x / 255);
    }
    return { ...phongMaterial.defaultUniforms, ...uniforms };
  }
};

// dist/modules/lighting/pbr-material/pbr-material-glsl.js
var vs2 = (
  /* glsl */
  `out vec3 pbr_vPosition;
out vec2 pbr_vUV;

#ifdef HAS_NORMALS
# ifdef HAS_TANGENTS
out mat3 pbr_vTBN;
# else
out vec3 pbr_vNormal;
# endif
#endif

void pbr_setPositionNormalTangentUV(vec4 position, vec4 normal, vec4 tangent, vec2 uv)
{
  vec4 pos = pbrProjection.modelMatrix * position;
  pbr_vPosition = vec3(pos.xyz) / pos.w;

#ifdef HAS_NORMALS
#ifdef HAS_TANGENTS
  vec3 normalW = normalize(vec3(pbrProjection.normalMatrix * vec4(normal.xyz, 0.0)));
  vec3 tangentW = normalize(vec3(pbrProjection.modelMatrix * vec4(tangent.xyz, 0.0)));
  vec3 bitangentW = cross(normalW, tangentW) * tangent.w;
  pbr_vTBN = mat3(tangentW, bitangentW, normalW);
#else // HAS_TANGENTS != 1
  pbr_vNormal = normalize(vec3(pbrProjection.modelMatrix * vec4(normal.xyz, 0.0)));
#endif
#endif

#ifdef HAS_UV
  pbr_vUV = uv;
#else
  pbr_vUV = vec2(0.,0.);
#endif
}
`
);
var fs3 = (
  /* glsl */
  `precision highp float;

uniform pbrMaterialUniforms {
  // Material is unlit
  bool unlit;

  // Base color map
  bool baseColorMapEnabled;
  vec4 baseColorFactor;

  bool normalMapEnabled;  
  float normalScale; // #ifdef HAS_NORMALMAP

  bool emissiveMapEnabled;
  vec3 emissiveFactor; // #ifdef HAS_EMISSIVEMAP

  vec2 metallicRoughnessValues;
  bool metallicRoughnessMapEnabled;

  bool occlusionMapEnabled;
  float occlusionStrength; // #ifdef HAS_OCCLUSIONMAP
  
  bool alphaCutoffEnabled;
  float alphaCutoff; // #ifdef ALPHA_CUTOFF
  
  // IBL
  bool IBLenabled;
  vec2 scaleIBLAmbient; // #ifdef USE_IBL
  
  // debugging flags used for shader output of intermediate PBR variables
  // #ifdef PBR_DEBUG
  vec4 scaleDiffBaseMR;
  vec4 scaleFGDSpec;
  // #endif
} pbrMaterial;

// Samplers
#ifdef HAS_BASECOLORMAP
uniform sampler2D pbr_baseColorSampler;
#endif
#ifdef HAS_NORMALMAP
uniform sampler2D pbr_normalSampler;
#endif
#ifdef HAS_EMISSIVEMAP
uniform sampler2D pbr_emissiveSampler;
#endif
#ifdef HAS_METALROUGHNESSMAP
uniform sampler2D pbr_metallicRoughnessSampler;
#endif
#ifdef HAS_OCCLUSIONMAP
uniform sampler2D pbr_occlusionSampler;
#endif
#ifdef USE_IBL
uniform samplerCube pbr_diffuseEnvSampler;
uniform samplerCube pbr_specularEnvSampler;
uniform sampler2D pbr_brdfLUT;
#endif

// Inputs from vertex shader

in vec3 pbr_vPosition;
in vec2 pbr_vUV;

#ifdef HAS_NORMALS
#ifdef HAS_TANGENTS
in mat3 pbr_vTBN;
#else
in vec3 pbr_vNormal;
#endif
#endif

// Encapsulate the various inputs used by the various functions in the shading equation
// We store values in this struct to simplify the integration of alternative implementations
// of the shading terms, outlined in the Readme.MD Appendix.
struct PBRInfo {
  float NdotL;                  // cos angle between normal and light direction
  float NdotV;                  // cos angle between normal and view direction
  float NdotH;                  // cos angle between normal and half vector
  float LdotH;                  // cos angle between light direction and half vector
  float VdotH;                  // cos angle between view direction and half vector
  float perceptualRoughness;    // roughness value, as authored by the model creator (input to shader)
  float metalness;              // metallic value at the surface
  vec3 reflectance0;            // full reflectance color (normal incidence angle)
  vec3 reflectance90;           // reflectance color at grazing angle
  float alphaRoughness;         // roughness mapped to a more linear change in the roughness (proposed by [2])
  vec3 diffuseColor;            // color contribution from diffuse lighting
  vec3 specularColor;           // color contribution from specular lighting
  vec3 n;                       // normal at surface point
  vec3 v;                       // vector from surface point to camera
};

const float M_PI = 3.141592653589793;
const float c_MinRoughness = 0.04;

vec4 SRGBtoLINEAR(vec4 srgbIn)
{
#ifdef MANUAL_SRGB
#ifdef SRGB_FAST_APPROXIMATION
  vec3 linOut = pow(srgbIn.xyz,vec3(2.2));
#else // SRGB_FAST_APPROXIMATION
  vec3 bLess = step(vec3(0.04045),srgbIn.xyz);
  vec3 linOut = mix( srgbIn.xyz/vec3(12.92), pow((srgbIn.xyz+vec3(0.055))/vec3(1.055),vec3(2.4)), bLess );
#endif //SRGB_FAST_APPROXIMATION
  return vec4(linOut,srgbIn.w);;
#else //MANUAL_SRGB
  return srgbIn;
#endif //MANUAL_SRGB
}

// Find the normal for this fragment, pulling either from a predefined normal map
// or from the interpolated mesh normal and tangent attributes.
vec3 getNormal()
{
  // Retrieve the tangent space matrix
#ifndef HAS_TANGENTS
  vec3 pos_dx = dFdx(pbr_vPosition);
  vec3 pos_dy = dFdy(pbr_vPosition);
  vec3 tex_dx = dFdx(vec3(pbr_vUV, 0.0));
  vec3 tex_dy = dFdy(vec3(pbr_vUV, 0.0));
  vec3 t = (tex_dy.t * pos_dx - tex_dx.t * pos_dy) / (tex_dx.s * tex_dy.t - tex_dy.s * tex_dx.t);

#ifdef HAS_NORMALS
  vec3 ng = normalize(pbr_vNormal);
#else
  vec3 ng = cross(pos_dx, pos_dy);
#endif

  t = normalize(t - ng * dot(ng, t));
  vec3 b = normalize(cross(ng, t));
  mat3 tbn = mat3(t, b, ng);
#else // HAS_TANGENTS
  mat3 tbn = pbr_vTBN;
#endif

#ifdef HAS_NORMALMAP
  vec3 n = texture(pbr_normalSampler, pbr_vUV).rgb;
  n = normalize(tbn * ((2.0 * n - 1.0) * vec3(pbrMaterial.normalScale, pbrMaterial.normalScale, 1.0)));
#else
  // The tbn matrix is linearly interpolated, so we need to re-normalize
  vec3 n = normalize(tbn[2].xyz);
#endif

  return n;
}

// Calculation of the lighting contribution from an optional Image Based Light source.
// Precomputed Environment Maps are required uniform inputs and are computed as outlined in [1].
// See our README.md on Environment Maps [3] for additional discussion.
#ifdef USE_IBL
vec3 getIBLContribution(PBRInfo pbrInfo, vec3 n, vec3 reflection)
{
  float mipCount = 9.0; // resolution of 512x512
  float lod = (pbrInfo.perceptualRoughness * mipCount);
  // retrieve a scale and bias to F0. See [1], Figure 3
  vec3 brdf = SRGBtoLINEAR(texture(pbr_brdfLUT,
    vec2(pbrInfo.NdotV, 1.0 - pbrInfo.perceptualRoughness))).rgb;
  vec3 diffuseLight = SRGBtoLINEAR(texture(pbr_diffuseEnvSampler, n)).rgb;

#ifdef USE_TEX_LOD
  vec3 specularLight = SRGBtoLINEAR(texture(pbr_specularEnvSampler, reflection, lod)).rgb;
#else
  vec3 specularLight = SRGBtoLINEAR(texture(pbr_specularEnvSampler, reflection)).rgb;
#endif

  vec3 diffuse = diffuseLight * pbrInfo.diffuseColor;
  vec3 specular = specularLight * (pbrInfo.specularColor * brdf.x + brdf.y);

  // For presentation, this allows us to disable IBL terms
  diffuse *= pbrMaterial.scaleIBLAmbient.x;
  specular *= pbrMaterial.scaleIBLAmbient.y;

  return diffuse + specular;
}
#endif

// Basic Lambertian diffuse
// Implementation from Lambert's Photometria https://archive.org/details/lambertsphotome00lambgoog
// See also [1], Equation 1
vec3 diffuse(PBRInfo pbrInfo)
{
  return pbrInfo.diffuseColor / M_PI;
}

// The following equation models the Fresnel reflectance term of the spec equation (aka F())
// Implementation of fresnel from [4], Equation 15
vec3 specularReflection(PBRInfo pbrInfo)
{
  return pbrInfo.reflectance0 +
    (pbrInfo.reflectance90 - pbrInfo.reflectance0) *
    pow(clamp(1.0 - pbrInfo.VdotH, 0.0, 1.0), 5.0);
}

// This calculates the specular geometric attenuation (aka G()),
// where rougher material will reflect less light back to the viewer.
// This implementation is based on [1] Equation 4, and we adopt their modifications to
// alphaRoughness as input as originally proposed in [2].
float geometricOcclusion(PBRInfo pbrInfo)
{
  float NdotL = pbrInfo.NdotL;
  float NdotV = pbrInfo.NdotV;
  float r = pbrInfo.alphaRoughness;

  float attenuationL = 2.0 * NdotL / (NdotL + sqrt(r * r + (1.0 - r * r) * (NdotL * NdotL)));
  float attenuationV = 2.0 * NdotV / (NdotV + sqrt(r * r + (1.0 - r * r) * (NdotV * NdotV)));
  return attenuationL * attenuationV;
}

// The following equation(s) model the distribution of microfacet normals across
// the area being drawn (aka D())
// Implementation from "Average Irregularity Representation of a Roughened Surface
// for Ray Reflection" by T. S. Trowbridge, and K. P. Reitz
// Follows the distribution function recommended in the SIGGRAPH 2013 course notes
// from EPIC Games [1], Equation 3.
float microfacetDistribution(PBRInfo pbrInfo)
{
  float roughnessSq = pbrInfo.alphaRoughness * pbrInfo.alphaRoughness;
  float f = (pbrInfo.NdotH * roughnessSq - pbrInfo.NdotH) * pbrInfo.NdotH + 1.0;
  return roughnessSq / (M_PI * f * f);
}

void PBRInfo_setAmbientLight(inout PBRInfo pbrInfo) {
  pbrInfo.NdotL = 1.0;
  pbrInfo.NdotH = 0.0;
  pbrInfo.LdotH = 0.0;
  pbrInfo.VdotH = 1.0;
}

void PBRInfo_setDirectionalLight(inout PBRInfo pbrInfo, vec3 lightDirection) {
  vec3 n = pbrInfo.n;
  vec3 v = pbrInfo.v;
  vec3 l = normalize(lightDirection);             // Vector from surface point to light
  vec3 h = normalize(l+v);                        // Half vector between both l and v

  pbrInfo.NdotL = clamp(dot(n, l), 0.001, 1.0);
  pbrInfo.NdotH = clamp(dot(n, h), 0.0, 1.0);
  pbrInfo.LdotH = clamp(dot(l, h), 0.0, 1.0);
  pbrInfo.VdotH = clamp(dot(v, h), 0.0, 1.0);
}

void PBRInfo_setPointLight(inout PBRInfo pbrInfo, PointLight pointLight) {
  vec3 light_direction = normalize(pointLight.position - pbr_vPosition);
  PBRInfo_setDirectionalLight(pbrInfo, light_direction);
}

vec3 calculateFinalColor(PBRInfo pbrInfo, vec3 lightColor) {
  // Calculate the shading terms for the microfacet specular shading model
  vec3 F = specularReflection(pbrInfo);
  float G = geometricOcclusion(pbrInfo);
  float D = microfacetDistribution(pbrInfo);

  // Calculation of analytical lighting contribution
  vec3 diffuseContrib = (1.0 - F) * diffuse(pbrInfo);
  vec3 specContrib = F * G * D / (4.0 * pbrInfo.NdotL * pbrInfo.NdotV);
  // Obtain final intensity as reflectance (BRDF) scaled by the energy of the light (cosine law)
  return pbrInfo.NdotL * lightColor * (diffuseContrib + specContrib);
}

vec4 pbr_filterColor(vec4 colorUnused)
{
  // The albedo may be defined from a base texture or a flat color
#ifdef HAS_BASECOLORMAP
  vec4 baseColor = SRGBtoLINEAR(texture(pbr_baseColorSampler, pbr_vUV)) * pbrMaterial.baseColorFactor;
#else
  vec4 baseColor = pbrMaterial.baseColorFactor;
#endif

#ifdef ALPHA_CUTOFF
  if (baseColor.a < pbrMaterial.alphaCutoff) {
    discard;
  }
#endif

  vec3 color = vec3(0, 0, 0);

  if(pbrMaterial.unlit){
    color.rgb = baseColor.rgb;
  }
  else{
    // Metallic and Roughness material properties are packed together
    // In glTF, these factors can be specified by fixed scalar values
    // or from a metallic-roughness map
    float perceptualRoughness = pbrMaterial.metallicRoughnessValues.y;
    float metallic = pbrMaterial.metallicRoughnessValues.x;
#ifdef HAS_METALROUGHNESSMAP
    // Roughness is stored in the 'g' channel, metallic is stored in the 'b' channel.
    // This layout intentionally reserves the 'r' channel for (optional) occlusion map data
    vec4 mrSample = texture(pbr_metallicRoughnessSampler, pbr_vUV);
    perceptualRoughness = mrSample.g * perceptualRoughness;
    metallic = mrSample.b * metallic;
#endif
    perceptualRoughness = clamp(perceptualRoughness, c_MinRoughness, 1.0);
    metallic = clamp(metallic, 0.0, 1.0);
    // Roughness is authored as perceptual roughness; as is convention,
    // convert to material roughness by squaring the perceptual roughness [2].
    float alphaRoughness = perceptualRoughness * perceptualRoughness;

    vec3 f0 = vec3(0.04);
    vec3 diffuseColor = baseColor.rgb * (vec3(1.0) - f0);
    diffuseColor *= 1.0 - metallic;
    vec3 specularColor = mix(f0, baseColor.rgb, metallic);

    // Compute reflectance.
    float reflectance = max(max(specularColor.r, specularColor.g), specularColor.b);

    // For typical incident reflectance range (between 4% to 100%) set the grazing
    // reflectance to 100% for typical fresnel effect.
    // For very low reflectance range on highly diffuse objects (below 4%),
    // incrementally reduce grazing reflecance to 0%.
    float reflectance90 = clamp(reflectance * 25.0, 0.0, 1.0);
    vec3 specularEnvironmentR0 = specularColor.rgb;
    vec3 specularEnvironmentR90 = vec3(1.0, 1.0, 1.0) * reflectance90;

    vec3 n = getNormal();                          // normal at surface point
    vec3 v = normalize(pbrProjection.camera - pbr_vPosition);  // Vector from surface point to camera

    float NdotV = clamp(abs(dot(n, v)), 0.001, 1.0);
    vec3 reflection = -normalize(reflect(v, n));

    PBRInfo pbrInfo = PBRInfo(
      0.0, // NdotL
      NdotV,
      0.0, // NdotH
      0.0, // LdotH
      0.0, // VdotH
      perceptualRoughness,
      metallic,
      specularEnvironmentR0,
      specularEnvironmentR90,
      alphaRoughness,
      diffuseColor,
      specularColor,
      n,
      v
    );


#ifdef USE_LIGHTS
    // Apply ambient light
    PBRInfo_setAmbientLight(pbrInfo);
    color += calculateFinalColor(pbrInfo, lighting.ambientColor);

    // Apply directional light
    for(int i = 0; i < lighting.directionalLightCount; i++) {
      if (i < lighting.directionalLightCount) {
        PBRInfo_setDirectionalLight(pbrInfo, lighting_getDirectionalLight(i).direction);
        color += calculateFinalColor(pbrInfo, lighting_getDirectionalLight(i).color);
      }
    }

    // Apply point light
    for(int i = 0; i < lighting.pointLightCount; i++) {
      if (i < lighting.pointLightCount) {
        PBRInfo_setPointLight(pbrInfo, lighting_getPointLight(i));
        float attenuation = getPointLightAttenuation(lighting_getPointLight(i), distance(lighting_getPointLight(i).position, pbr_vPosition));
        color += calculateFinalColor(pbrInfo, lighting_getPointLight(i).color / attenuation);
      }
    }
#endif

    // Calculate lighting contribution from image based lighting source (IBL)
#ifdef USE_IBL
    if (pbrMaterial.IBLenabled) {
      color += getIBLContribution(pbrInfo, n, reflection);
    }
#endif

 // Apply optional PBR terms for additional (optional) shading
#ifdef HAS_OCCLUSIONMAP
    if (pbrMaterial.occlusionMapEnabled) {
      float ao = texture(pbr_occlusionSampler, pbr_vUV).r;
      color = mix(color, color * ao, pbrMaterial.occlusionStrength);
    }
#endif

#ifdef HAS_EMISSIVEMAP
    if (pbrMaterial.emissiveMapEnabled) {
      vec3 emissive = SRGBtoLINEAR(texture(pbr_emissiveSampler, pbr_vUV)).rgb * pbrMaterial.emissiveFactor;
      color += emissive;
    }
#endif

    // This section uses mix to override final color for reference app visualization
    // of various parameters in the lighting equation.
#ifdef PBR_DEBUG
    // TODO: Figure out how to debug multiple lights

    // color = mix(color, F, pbr_scaleFGDSpec.x);
    // color = mix(color, vec3(G), pbr_scaleFGDSpec.y);
    // color = mix(color, vec3(D), pbr_scaleFGDSpec.z);
    // color = mix(color, specContrib, pbr_scaleFGDSpec.w);

    // color = mix(color, diffuseContrib, pbr_scaleDiffBaseMR.x);
    color = mix(color, baseColor.rgb, pbrMaterial.scaleDiffBaseMR.y);
    color = mix(color, vec3(metallic), pbrMaterial.scaleDiffBaseMR.z);
    color = mix(color, vec3(perceptualRoughness), pbrMaterial.scaleDiffBaseMR.w);
#endif

  }

  return vec4(pow(color,vec3(1.0/2.2)), baseColor.a);
}
`
);

// dist/modules/lighting/pbr-material/pbr-material-wgsl.js
var source2 = (
  /* wgsl */
  `struct PBRFragmentInputs {
  pbr_vPosition: vec3f,
  pbr_vUV: vec2f,
  pbr_vTBN: mat3f,
  pbr_vNormal: vec3f
};

var fragmentInputs: PBRFragmentInputs;

fn pbr_setPositionNormalTangentUV(position: vec4f, normal: vec4f, tangent: vec4f, uv: vec2f)
{
  var pos: vec4f = pbrProjection.modelMatrix * position;
  pbr_vPosition = vec3(pos.xyz) / pos.w;

#ifdef HAS_NORMALS
#ifdef HAS_TANGENTS
  let normalW: vec3f = normalize(vec3(pbrProjection.normalMatrix * vec4(normal.xyz, 0.0)));
  let tangentW: vec3f = normalize(vec3(pbrProjection.modelMatrix * vec4(tangent.xyz, 0.0)));
  let bitangentW: vec3f = cross(normalW, tangentW) * tangent.w;
  fragmentInputs,pbr_vTBN = mat3(tangentW, bitangentW, normalW);
#else // HAS_TANGENTS != 1
  fragmentInputs.pbr_vNormal = normalize(vec3(pbrProjection.modelMatrix * vec4(normal.xyz, 0.0)));
#endif
#endif

#ifdef HAS_UV
  pbr_vUV = uv;
#else
  pbr_vUV = vec2(0.,0.);
#endif
}

struct pbrMaterialUniforms {
  // Material is unlit
  unlit: uint32,

  // Base color map
  baseColorMapEnabled: uint32,
  baseColorFactor: vec4f,

  normalMapEnabled : uint32,
  normalScale: f32,  // #ifdef HAS_NORMALMAP

  emissiveMapEnabled: uint32,
  emissiveFactor: vec3f, // #ifdef HAS_EMISSIVEMAP

  metallicRoughnessValues: vec2f,
  metallicRoughnessMapEnabled: uint32,

  occlusionMapEnabled: i32,
  occlusionStrength: f32, // #ifdef HAS_OCCLUSIONMAP
  
  alphaCutoffEnabled: i32,
  alphaCutoff: f32, // #ifdef ALPHA_CUTOFF
  
  // IBL
  IBLenabled: i32,
  scaleIBLAmbient: vec2f, // #ifdef USE_IBL
  
  // debugging flags used for shader output of intermediate PBR variables
  // #ifdef PBR_DEBUG
  scaleDiffBaseMR: vec4f,
  scaleFGDSpec: vec4f
  // #endif
} 
  
@binding(2) @group(0) var<uniform> material : pbrMaterialUniforms;

// Samplers
#ifdef HAS_BASECOLORMAP
uniform sampler2D pbr_baseColorSampler;
#endif
#ifdef HAS_NORMALMAP
uniform sampler2D pbr_normalSampler;
#endif
#ifdef HAS_EMISSIVEMAP
uniform sampler2D pbr_emissiveSampler;
#endif
#ifdef HAS_METALROUGHNESSMAP
uniform sampler2D pbr_metallicRoughnessSampler;
#endif
#ifdef HAS_OCCLUSIONMAP
uniform sampler2D pbr_occlusionSampler;
#endif
#ifdef USE_IBL
uniform samplerCube pbr_diffuseEnvSampler;
uniform samplerCube pbr_specularEnvSampler;
uniform sampler2D pbr_brdfLUT;
#endif

// Encapsulate the various inputs used by the various functions in the shading equation
// We store values in this struct to simplify the integration of alternative implementations
// of the shading terms, outlined in the Readme.MD Appendix.
struct PBRInfo {
  NdotL: f32,                  // cos angle between normal and light direction
  NdotV: f32,                  // cos angle between normal and view direction
  NdotH: f32,                  // cos angle between normal and half vector
  LdotH: f32,                  // cos angle between light direction and half vector
  VdotH: f32,                  // cos angle between view direction and half vector
  perceptualRoughness: f32,    // roughness value, as authored by the model creator (input to shader)
  metalness: f32,              // metallic value at the surface
  reflectance0: vec3f,            // full reflectance color (normal incidence angle)
  reflectance90: vec3f,           // reflectance color at grazing angle
  alphaRoughness: f32,         // roughness mapped to a more linear change in the roughness (proposed by [2])
  diffuseColor: vec3f,            // color contribution from diffuse lighting
  specularColor: vec3f,           // color contribution from specular lighting
  n: vec3f,                       // normal at surface point
  v: vec3f,                       // vector from surface point to camera
};

const M_PI = 3.141592653589793;
const c_MinRoughness = 0.04;

fn SRGBtoLINEAR(srgbIn: vec4f ) -> vec4f
{
#ifdef MANUAL_SRGB
#ifdef SRGB_FAST_APPROXIMATION
  var linOut: vec3f = pow(srgbIn.xyz,vec3(2.2));
#else // SRGB_FAST_APPROXIMATION
  var bLess: vec3f = step(vec3(0.04045),srgbIn.xyz);
  var linOut: vec3f = mix( srgbIn.xyz/vec3(12.92), pow((srgbIn.xyz+vec3(0.055))/vec3(1.055),vec3(2.4)), bLess );
#endif //SRGB_FAST_APPROXIMATION
  return vec4f(linOut,srgbIn.w);;
#else //MANUAL_SRGB
  return srgbIn;
#endif //MANUAL_SRGB
}

// Find the normal for this fragment, pulling either from a predefined normal map
// or from the interpolated mesh normal and tangent attributes.
fn getNormal() -> vec3f
{
  // Retrieve the tangent space matrix
#ifndef HAS_TANGENTS
  var pos_dx: vec3f = dFdx(pbr_vPosition);
  var pos_dy: vec3f = dFdy(pbr_vPosition);
  var tex_dx: vec3f = dFdx(vec3(pbr_vUV, 0.0));
  var tex_dy: vec3f = dFdy(vec3(pbr_vUV, 0.0));
  var t: vec3f = (tex_dy.t * pos_dx - tex_dx.t * pos_dy) / (tex_dx.s * tex_dy.t - tex_dy.s * tex_dx.t);

#ifdef HAS_NORMALS
  var ng: vec3f = normalize(pbr_vNormal);
#else
  var ng: vec3f = cross(pos_dx, pos_dy);
#endif

  t = normalize(t - ng * dot(ng, t));
  var b: vec3f = normalize(cross(ng, t));
  var tbn: mat3f = mat3f(t, b, ng);
#else // HAS_TANGENTS
  var tbn: mat3f = pbr_vTBN;
#endif

#ifdef HAS_NORMALMAP
  vec3 n = texture(pbr_normalSampler, pbr_vUV).rgb;
  n = normalize(tbn * ((2.0 * n - 1.0) * vec3(pbrMaterial.normalScale, pbrMaterial.normalScale, 1.0)));
#else
  // The tbn matrix is linearly interpolated, so we need to re-normalize
  vec3 n = normalize(tbn[2].xyz);
#endif

  return n;
}

// Calculation of the lighting contribution from an optional Image Based Light source.
// Precomputed Environment Maps are required uniform inputs and are computed as outlined in [1].
// See our README.md on Environment Maps [3] for additional discussion.
#ifdef USE_IBL
fn getIBLContribution(PBRInfo pbrInfo, vec3 n, vec3 reflection) -> vec3f
{
  float mipCount = 9.0; // resolution of 512x512
  float lod = (pbrInfo.perceptualRoughness * mipCount);
  // retrieve a scale and bias to F0. See [1], Figure 3
  vec3 brdf = SRGBtoLINEAR(texture(pbr_brdfLUT,
    vec2(pbrInfo.NdotV, 1.0 - pbrInfo.perceptualRoughness))).rgb;
  vec3 diffuseLight = SRGBtoLINEAR(texture(pbr_diffuseEnvSampler, n)).rgb;

#ifdef USE_TEX_LOD
  vec3 specularLight = SRGBtoLINEAR(texture(pbr_specularEnvSampler, reflection, lod)).rgb;
#else
  vec3 specularLight = SRGBtoLINEAR(texture(pbr_specularEnvSampler, reflection)).rgb;
#endif

  vec3 diffuse = diffuseLight * pbrInfo.diffuseColor;
  vec3 specular = specularLight * (pbrInfo.specularColor * brdf.x + brdf.y);

  // For presentation, this allows us to disable IBL terms
  diffuse *= pbrMaterial.scaleIBLAmbient.x;
  specular *= pbrMaterial.scaleIBLAmbient.y;

  return diffuse + specular;
}
#endif

// Basic Lambertian diffuse
// Implementation from Lambert's Photometria https://archive.org/details/lambertsphotome00lambgoog
// See also [1], Equation 1
fn diffuse(pbrInfo: PBRInfo) -> vec3<f32> {
  return pbrInfo.diffuseColor / PI;
}

// The following equation models the Fresnel reflectance term of the spec equation (aka F())
// Implementation of fresnel from [4], Equation 15
fn specularReflection(pbrInfo: PBRInfo) -> vec3<f32> {
  return pbrInfo.reflectance0 +
    (pbrInfo.reflectance90 - pbrInfo.reflectance0) *
    pow(clamp(1.0 - pbrInfo.VdotH, 0.0, 1.0), 5.0);
}

// This calculates the specular geometric attenuation (aka G()),
// where rougher material will reflect less light back to the viewer.
// This implementation is based on [1] Equation 4, and we adopt their modifications to
// alphaRoughness as input as originally proposed in [2].
fn geometricOcclusion(pbrInfo: PBRInfo) -> f32 {
  let NdotL: f32 = pbrInfo.NdotL;
  let NdotV: f32 = pbrInfo.NdotV;
  let r: f32 = pbrInfo.alphaRoughness;

  let attenuationL = 2.0 * NdotL / (NdotL + sqrt(r * r + (1.0 - r * r) * (NdotL * NdotL)));
  let attenuationV = 2.0 * NdotV / (NdotV + sqrt(r * r + (1.0 - r * r) * (NdotV * NdotV)));
  return attenuationL * attenuationV;
}

// The following equation(s) model the distribution of microfacet normals across
// the area being drawn (aka D())
// Implementation from "Average Irregularity Representation of a Roughened Surface
// for Ray Reflection" by T. S. Trowbridge, and K. P. Reitz
// Follows the distribution function recommended in the SIGGRAPH 2013 course notes
// from EPIC Games [1], Equation 3.
fn microfacetDistribution(pbrInfo: PBRInfo) -> f32 {
  let roughnessSq = pbrInfo.alphaRoughness * pbrInfo.alphaRoughness;
  let f = (pbrInfo.NdotH * roughnessSq - pbrInfo.NdotH) * pbrInfo.NdotH + 1.0;
  return roughnessSq / (PI * f * f);
}

fn PBRInfo_setAmbientLight(pbrInfo: ptr<function, PBRInfo>) {
  (*pbrInfo).NdotL = 1.0;
  (*pbrInfo).NdotH = 0.0;
  (*pbrInfo).LdotH = 0.0;
  (*pbrInfo).VdotH = 1.0;
}

fn PBRInfo_setDirectionalLight(pbrInfo: ptr<function, PBRInfo>, lightDirection: vec3<f32>) {
  let n = (*pbrInfo).n;
  let v = (*pbrInfo).v;
  let l = normalize(lightDirection);             // Vector from surface point to light
  let h = normalize(l + v);                      // Half vector between both l and v

  (*pbrInfo).NdotL = clamp(dot(n, l), 0.001, 1.0);
  (*pbrInfo).NdotH = clamp(dot(n, h), 0.0, 1.0);
  (*pbrInfo).LdotH = clamp(dot(l, h), 0.0, 1.0);
  (*pbrInfo).VdotH = clamp(dot(v, h), 0.0, 1.0);
}

fn PBRInfo_setPointLight(pbrInfo: ptr<function, PBRInfo>, pointLight: PointLight) {
  let light_direction = normalize(pointLight.position - pbr_vPosition);
  PBRInfo_setDirectionalLight(pbrInfo, light_direction);
}

fn calculateFinalColor(pbrInfo: PBRInfo, lightColor: vec3<f32>) -> vec3<f32> {
  // Calculate the shading terms for the microfacet specular shading model
  let F = specularReflection(pbrInfo);
  let G = geometricOcclusion(pbrInfo);
  let D = microfacetDistribution(pbrInfo);

  // Calculation of analytical lighting contribution
  let diffuseContrib = (1.0 - F) * diffuse(pbrInfo);
  let specContrib = F * G * D / (4.0 * pbrInfo.NdotL * pbrInfo.NdotV);
  // Obtain final intensity as reflectance (BRDF) scaled by the energy of the light (cosine law)
  return pbrInfo.NdotL * lightColor * (diffuseContrib + specContrib);
}

fn pbr_filterColor(colorUnused: vec4<f32>) -> vec4<f32> {
  // The albedo may be defined from a base texture or a flat color
  var baseColor: vec4<f32>;
  #ifdef HAS_BASECOLORMAP
  baseColor = SRGBtoLINEAR(textureSample(pbr_baseColorSampler, pbr_baseColorSampler, pbr_vUV)) * pbrMaterial.baseColorFactor;
  #else
  baseColor = pbrMaterial.baseColorFactor;
  #endif

  #ifdef ALPHA_CUTOFF
  if (baseColor.a < pbrMaterial.alphaCutoff) {
    discard;
  }
  #endif

  var color = vec3<f32>(0.0, 0.0, 0.0);

  if (pbrMaterial.unlit) {
    color = baseColor.rgb;
  } else {
    // Metallic and Roughness material properties are packed together
    // In glTF, these factors can be specified by fixed scalar values
    // or from a metallic-roughness map
    var perceptualRoughness = pbrMaterial.metallicRoughnessValues.y;
    var metallic = pbrMaterial.metallicRoughnessValues.x;
    #ifdef HAS_METALROUGHNESSMAP
    // Roughness is stored in the 'g' channel, metallic is stored in the 'b' channel.
    // This layout intentionally reserves the 'r' channel for (optional) occlusion map data
    let mrSample = textureSample(pbr_metallicRoughnessSampler, pbr_metallicRoughnessSampler, pbr_vUV);
    perceptualRoughness = mrSample.g * perceptualRoughness;
    metallic = mrSample.b * metallic;
    #endif
    perceptualRoughness = clamp(perceptualRoughness, c_MinRoughness, 1.0);
    metallic = clamp(metallic, 0.0, 1.0);
    // Roughness is authored as perceptual roughness; as is convention,
    // convert to material roughness by squaring the perceptual roughness [2].
    let alphaRoughness = perceptualRoughness * perceptualRoughness;

    let f0 = vec3<f32>(0.04);
    var diffuseColor = baseColor.rgb * (vec3<f32>(1.0) - f0);
    diffuseColor *= 1.0 - metallic;
    let specularColor = mix(f0, baseColor.rgb, metallic);

    // Compute reflectance.
    let reflectance = max(max(specularColor.r, specularColor.g), specularColor.b);

    // For typical incident reflectance range (between 4% to 100%) set the grazing
    // reflectance to 100% for typical fresnel effect.
    // For very low reflectance range on highly diffuse objects (below 4%),
    // incrementally reduce grazing reflectance to 0%.
    let reflectance90 = clamp(reflectance * 25.0, 0.0, 1.0);
    let specularEnvironmentR0 = specularColor;
    let specularEnvironmentR90 = vec3<f32>(1.0, 1.0, 1.0) * reflectance90;

    let n = getNormal();                          // normal at surface point
    let v = normalize(pbrProjection.camera - pbr_vPosition);  // Vector from surface point to camera

    let NdotV = clamp(abs(dot(n, v)), 0.001, 1.0);
    let reflection = -normalize(reflect(v, n));

    var pbrInfo = PBRInfo(
      0.0, // NdotL
      NdotV,
      0.0, // NdotH
      0.0, // LdotH
      0.0, // VdotH
      perceptualRoughness,
      metallic,
      specularEnvironmentR0,
      specularEnvironmentR90,
      alphaRoughness,
      diffuseColor,
      specularColor,
      n,
      v
    );

    #ifdef USE_LIGHTS
    // Apply ambient light
    PBRInfo_setAmbientLight(&pbrInfo);
    color += calculateFinalColor(pbrInfo, lighting.ambientColor);

    // Apply directional light
    for (var i = 0; i < lighting.directionalLightCount; i++) {
      if (i < lighting.directionalLightCount) {
        PBRInfo_setDirectionalLight(&pbrInfo, lighting_getDirectionalLight(i).direction);
        color += calculateFinalColor(pbrInfo, lighting_getDirectionalLight(i).color);
      }
    }

    // Apply point light
    for (var i = 0; i < lighting.pointLightCount; i++) {
      if (i < lighting.pointLightCount) {
        PBRInfo_setPointLight(&pbrInfo, lighting_getPointLight(i));
        let attenuation = getPointLightAttenuation(lighting_getPointLight(i), distance(lighting_getPointLight(i).position, pbr_vPosition));
        color += calculateFinalColor(pbrInfo, lighting_getPointLight(i).color / attenuation);
      }
    }
    #endif

    // Calculate lighting contribution from image based lighting source (IBL)
    #ifdef USE_IBL
    if (pbrMaterial.IBLenabled) {
      color += getIBLContribution(pbrInfo, n, reflection);
    }
    #endif

    // Apply optional PBR terms for additional (optional) shading
    #ifdef HAS_OCCLUSIONMAP
    if (pbrMaterial.occlusionMapEnabled) {
      let ao = textureSample(pbr_occlusionSampler, pbr_occlusionSampler, pbr_vUV).r;
      color = mix(color, color * ao, pbrMaterial.occlusionStrength);
    }
    #endif

    #ifdef HAS_EMISSIVEMAP
    if (pbrMaterial.emissiveMapEnabled) {
      let emissive = SRGBtoLINEAR(textureSample(pbr_emissiveSampler, pbr_emissiveSampler, pbr_vUV)).rgb * pbrMaterial.emissiveFactor;
      color += emissive;
    }
    #endif

    // This section uses mix to override final color for reference app visualization
    // of various parameters in the lighting equation.
    #ifdef PBR_DEBUG
    // TODO: Figure out how to debug multiple lights

    // color = mix(color, F, pbr_scaleFGDSpec.x);
    // color = mix(color, vec3(G), pbr_scaleFGDSpec.y);
    // color = mix(color, vec3(D), pbr_scaleFGDSpec.z);
    // color = mix(color, specContrib, pbr_scaleFGDSpec.w);

    // color = mix(color, diffuseContrib, pbr_scaleDiffBaseMR.x);
    color = mix(color, baseColor.rgb, pbrMaterial.scaleDiffBaseMR.y);
    color = mix(color, vec3<f32>(metallic), pbrMaterial.scaleDiffBaseMR.z);
    color = mix(color, vec3<f32>(perceptualRoughness), pbrMaterial.scaleDiffBaseMR.w);
    #endif
  }

  return vec4<f32>(pow(color, vec3<f32>(1.0 / 2.2)), baseColor.a);
}
`
);

// dist/modules/lighting/pbr-material/pbr-projection.js
var uniformBlock = (
  /* glsl */
  `uniform pbrProjectionUniforms {
  mat4 modelViewProjectionMatrix;
  mat4 modelMatrix;
  mat4 normalMatrix;
  vec3 camera;
} pbrProjection;
`
);
var pbrProjection = {
  name: "pbrProjection",
  vs: uniformBlock,
  fs: uniformBlock,
  // TODO why is this needed?
  getUniforms: (props) => props,
  uniformTypes: {
    modelViewProjectionMatrix: "mat4x4<f32>",
    modelMatrix: "mat4x4<f32>",
    normalMatrix: "mat4x4<f32>",
    camera: "vec3<i32>"
  }
};

// dist/modules/lighting/pbr-material/pbr-material.js
var pbrMaterial = {
  props: {},
  uniforms: {},
  name: "pbrMaterial",
  dependencies: [lighting, pbrProjection],
  source: source2,
  vs: vs2,
  fs: fs3,
  defines: {
    LIGHTING_FRAGMENT: true,
    HAS_NORMALMAP: false,
    HAS_EMISSIVEMAP: false,
    HAS_OCCLUSIONMAP: false,
    HAS_BASECOLORMAP: false,
    HAS_METALROUGHNESSMAP: false,
    ALPHA_CUTOFF: false,
    USE_IBL: false,
    PBR_DEBUG: false
  },
  getUniforms: (props) => props,
  uniformTypes: {
    // Material is unlit
    unlit: "i32",
    // Base color map
    baseColorMapEnabled: "i32",
    baseColorFactor: "vec4<f32>",
    normalMapEnabled: "i32",
    normalScale: "f32",
    // #ifdef HAS_NORMALMAP
    emissiveMapEnabled: "i32",
    emissiveFactor: "vec3<f32>",
    // #ifdef HAS_EMISSIVEMAP
    metallicRoughnessValues: "vec2<f32>",
    metallicRoughnessMapEnabled: "i32",
    occlusionMapEnabled: "i32",
    occlusionStrength: "f32",
    // #ifdef HAS_OCCLUSIONMAP
    alphaCutoffEnabled: "i32",
    alphaCutoff: "f32",
    // #ifdef ALPHA_CUTOFF
    // IBL
    IBLenabled: "i32",
    scaleIBLAmbient: "vec2<f32>",
    // #ifdef USE_IBL
    // debugging flags used for shader output of intermediate PBR variables
    // #ifdef PBR_DEBUG
    scaleDiffBaseMR: "vec4<f32>",
    scaleFGDSpec: "vec4<f32>"
  }
};
//# sourceMappingURL=index.cjs.map
