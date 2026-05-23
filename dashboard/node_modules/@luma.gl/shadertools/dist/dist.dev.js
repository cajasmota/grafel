(function webpackUniversalModuleDefinition(root, factory) {
  if (typeof exports === 'object' && typeof module === 'object')
    module.exports = factory();
  else if (typeof define === 'function' && define.amd) define([], factory);
        else if (typeof exports === 'object') exports['luma'] = factory();
  else root['luma'] = factory();})(globalThis, function () {
"use strict";
var __exports__ = (() => {
  var __create = Object.create;
  var __defProp = Object.defineProperty;
  var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
  var __getOwnPropNames = Object.getOwnPropertyNames;
  var __getProtoOf = Object.getPrototypeOf;
  var __hasOwnProp = Object.prototype.hasOwnProperty;
  var __defNormalProp = (obj, key, value) => key in obj ? __defProp(obj, key, { enumerable: true, configurable: true, writable: true, value }) : obj[key] = value;
  var __commonJS = (cb, mod) => function __require() {
    return mod || (0, cb[__getOwnPropNames(cb)[0]])((mod = { exports: {} }).exports, mod), mod.exports;
  };
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
  var __reExport = (target, mod, secondTarget) => (__copyProps(target, mod, "default"), secondTarget && __copyProps(secondTarget, mod, "default"));
  var __toESM = (mod, isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(
    // If the importer is in node compatibility mode or this is not an ESM
    // file that has been converted to a CommonJS file using a Babel-
    // compatible transform (i.e. "__esModule" has not been set), then set
    // "default" to the CommonJS "module.exports" for node compatibility.
    isNodeMode || !mod || !mod.__esModule ? __defProp(target, "default", { value: mod, enumerable: true }) : target,
    mod
  ));
  var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);
  var __publicField = (obj, key, value) => {
    __defNormalProp(obj, typeof key !== "symbol" ? key + "" : key, value);
    return value;
  };

  // external-global-plugin:@luma.gl/core
  var require_core = __commonJS({
    "external-global-plugin:@luma.gl/core"(exports, module) {
      module.exports = globalThis.luma;
    }
  });

  // bundle.ts
  var bundle_exports = {};
  __export(bundle_exports, {
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
  __reExport(bundle_exports, __toESM(require_core(), 1));

  // src/lib/utils/assert.ts
  function assert(condition, message) {
    if (!condition) {
      throw new Error(message || "shadertools: assertion failed.");
    }
  }

  // src/lib/filters/prop-types.ts
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
          assert(
            propsValidator.validate(properties[key], propsValidator),
            `${errorMessage}: invalid ${key}`
          );
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

  // src/module-injectors.ts
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

  // src/lib/shader-assembly/shader-injections.ts
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
      fragmentData.sort((a2, b2) => a2.order - b2.order);
      fragments.length = fragmentData.length;
      for (let i2 = 0, len = fragmentData.length; i2 < len; ++i2) {
        fragments[i2] = fragmentData[i2].injection;
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

  // src/lib/shader-module/shader-module.ts
  function initializeShaderModules(modules) {
    modules.map((module) => initializeShaderModule(module));
  }
  function initializeShaderModule(module) {
    if (module.instance) {
      return;
    }
    initializeShaderModules(module.dependencies || []);
    const {
      propTypes = {},
      deprecations = [],
      // defines = {},
      inject = {}
    } = module;
    const instance = {
      normalizedInjections: normalizeInjections(inject),
      parsedDeprecations: parseDeprecationDefinitions(deprecations)
    };
    if (propTypes) {
      instance.propValidators = makePropValidators(propTypes);
    }
    module.instance = instance;
    let defaultProps = {};
    if (propTypes) {
      defaultProps = Object.entries(propTypes).reduce(
        (obj, [key, propType]) => {
          const value = propType?.value;
          if (value) {
            obj[key] = value;
          }
          return obj;
        },
        {}
      );
    }
    module.defaultUniforms = { ...module.defaultUniforms, ...defaultProps };
  }
  function getShaderModuleUniforms(module, props, oldUniforms) {
    initializeShaderModule(module);
    const uniforms = oldUniforms || { ...module.defaultUniforms };
    if (props && module.getUniforms) {
      return module.getUniforms(props, uniforms);
    }
    return getValidatedProperties(props, module.instance?.propValidators, module.name);
  }
  function checkShaderModuleDeprecations(shaderModule, shaderSource, log3) {
    shaderModule.deprecations?.forEach((def) => {
      if (def.regex?.test(shaderSource)) {
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

  // src/lib/shader-module/shader-module-dependencies.ts
  function getShaderModuleDependencies(modules) {
    initializeShaderModules(modules);
    const moduleMap = {};
    const moduleDepth = {};
    getDependencyGraph({ modules, level: 0, moduleMap, moduleDepth });
    const dependencies = Object.keys(moduleDepth).sort((a2, b2) => moduleDepth[b2] - moduleDepth[a2]).map((name) => moduleMap[name]);
    initializeShaderModules(dependencies);
    return dependencies;
  }
  function getDependencyGraph(options) {
    const { modules, level, moduleMap, moduleDepth } = options;
    if (level >= 5) {
      throw new Error("Possible loop in shader dependency graph");
    }
    for (const module of modules) {
      moduleMap[module.name] = module;
      if (moduleDepth[module.name] === void 0 || moduleDepth[module.name] < level) {
        moduleDepth[module.name] = level;
      }
    }
    for (const module of modules) {
      if (module.dependencies) {
        getDependencyGraph({ modules: module.dependencies, level: level + 1, moduleMap, moduleDepth });
      }
    }
  }
  function getShaderDependencies(modules) {
    initializeShaderModules(modules);
    const moduleMap = {};
    const moduleDepth = {};
    getDependencyGraph({ modules, level: 0, moduleMap, moduleDepth });
    modules = Object.keys(moduleDepth).sort((a2, b2) => moduleDepth[b2] - moduleDepth[a2]).map((name) => moduleMap[name]);
    initializeShaderModules(modules);
    return modules;
  }
  function resolveModules(modules) {
    return getShaderDependencies(modules);
  }

  // src/lib/shader-assembly/platform-defines.ts
  function getPlatformShaderDefines(platformInfo) {
    switch (platformInfo?.gpu.toLowerCase()) {
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

  // src/lib/shader-transpiler/transpile-glsl-shader.ts
  function transpileGLSLShader(source3, stage) {
    const sourceGLSLVersion = Number(source3.match(/^#version[ \t]+(\d+)/m)?.[1] || 100);
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

  // src/lib/shader-assembly/shader-hooks.ts
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
        injections.sort((a2, b2) => a2.order - b2.order);
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

  // src/lib/glsl-utils/get-shader-info.ts
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

  // src/lib/shader-assembly/assemble-shaders.ts
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
    for (const module of modulesToInject) {
      if (log3) {
        checkShaderModuleDeprecations(module, coreSource, log3);
      }
      const moduleSource = getShaderModuleSource(module, "wgsl");
      assembledSource += moduleSource;
      const injections = module.injections?.[stage] || {};
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
    const {
      source: source3,
      stage,
      language = "glsl",
      modules,
      defines = {},
      hookFunctions = [],
      inject = {},
      prologue = true,
      log: log3
    } = options;
    assert(typeof source3 === "string", "shader source must be a string");
    const sourceVersion = language === "glsl" ? getShaderInfo(source3).version : -1;
    const targetVersion = platformInfo.shaderLanguageVersion;
    const sourceVersionDirective = sourceVersion === 100 ? "#version 100" : "#version 300 es";
    const sourceLines = source3.split("\n");
    const coreSource = sourceLines.slice(1).join("\n");
    const allDefines = {};
    modules.forEach((module) => {
      Object.assign(allDefines, module.defines);
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
    for (const module of modules) {
      if (log3) {
        checkShaderModuleDeprecations(module, coreSource, log3);
      }
      const moduleSource = getShaderModuleSource(module, stage);
      assembledSource += moduleSource;
      const injections = module.instance?.normalizedInjections[stage] || {};
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
      const uniforms = {};
      for (const module of modules) {
        const moduleUniforms = module.getUniforms?.(opts, uniforms);
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
  function getShaderModuleSource(module, stage) {
    let moduleSource;
    switch (stage) {
      case "vertex":
        moduleSource = module.vs || "";
        break;
      case "fragment":
        moduleSource = module.fs || "";
        break;
      case "wgsl":
        moduleSource = module.source || "";
        break;
      default:
        assert(false);
    }
    if (!module.name) {
      throw new Error("Shader module must have a name");
    }
    const moduleName = module.name.toUpperCase().replace(/[^0-9a-z]/gi, "_");
    let source3 = `// ----- MODULE ${module.name} ---------------

`;
    if (stage !== "wgsl") {
      source3 += `#define MODULE_${moduleName}
`;
    }
    source3 += `${moduleSource}
`;
    return source3;
  }

  // src/lib/preprocessor/preprocessor.ts
  var IFDEF_REGEXP = /^\s*\#\s*ifdef\s*([a-zA-Z_]+)\s*$/;
  var ENDIF_REGEXP = /^\s*\#\s*endif\s*$/;
  function preprocess(source3, options) {
    const lines = source3.split("\n");
    const output = [];
    let conditional = true;
    let currentDefine = null;
    for (const line of lines) {
      const matchIf = line.match(IFDEF_REGEXP);
      const matchEnd = line.match(ENDIF_REGEXP);
      if (matchIf) {
        currentDefine = matchIf[1];
        conditional = Boolean(options?.defines?.[currentDefine]);
      } else if (matchEnd) {
        conditional = true;
      } else if (conditional) {
        output.push(line);
      }
    }
    return output.join("\n");
  }

  // src/lib/shader-assembler.ts
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
    addDefaultModule(module) {
      if (!this._defaultModules.find(
        (m2) => m2.name === (typeof module === "string" ? module : module.name)
      )) {
        this._defaultModules.push(module);
      }
    }
    /**
     * Remove a default module
     */
    removeDefaultModule(module) {
      const moduleName = typeof module === "string" ? module : module.name;
      this._defaultModules = this._defaultModules.filter((m2) => m2.name !== moduleName);
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
      for (let i2 = 0, len = this._defaultModules.length; i2 < len; ++i2) {
        const module = this._defaultModules[i2];
        const name = module.name;
        modules[count++] = module;
        seen[name] = true;
      }
      for (let i2 = 0, len = appModules.length; i2 < len; ++i2) {
        const module = appModules[i2];
        const name = module.name;
        if (!seen[name]) {
          modules[count++] = module;
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

  // src/lib/glsl-utils/shader-utils.ts
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

  // src/lib/shader-generator/utils/capitalize.ts
  function capitalize(str) {
    return typeof str === "string" ? str.charAt(0).toUpperCase() + str.slice(1) : str;
  }

  // src/lib/shader-generator/glsl/generate-glsl.ts
  function generateGLSLForModule(module, options) {
    return generateGLSLUniformDeclarations(module, options);
  }
  function generateGLSLUniformDeclarations(module, options) {
    const glsl = [];
    switch (options.uniforms) {
      case "scoped-interface-blocks":
      case "unscoped-interface-blocks":
        glsl.push(`uniform ${capitalize(module.name)} {`);
        break;
      case "uniforms":
    }
    for (const [uniformName, uniformFormat] of Object.entries(module.uniformTypes || {})) {
      const glslUniformType = getGLSLUniformType(uniformFormat);
      switch (options.uniforms) {
        case "scoped-interface-blocks":
          glsl.push(`  ${glslUniformType} ${uniformName};`);
          break;
        case "unscoped-interface-blocks":
          glsl.push(`  ${glslUniformType} ${module.name}_${uniformName};`);
          break;
        case "uniforms":
          glsl.push(`uniform ${glslUniformType} ${module.name}_${uniformName};`);
      }
    }
    switch (options.uniforms) {
      case "scoped-interface-blocks":
        glsl.push(`} ${module.name};`);
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

  // src/lib/shader-generator/wgsl/generate-wgsl.ts
  function generateWGSLForModule(module, options) {
    return generateWGSLUniformDeclarations(module, options);
  }
  function generateWGSLUniformDeclarations(module, options) {
    const wgsl = [];
    wgsl.push(`struct ${capitalize(module.name)} {`);
    for (const [uniformName, uniformFormat] of Object.entries(module?.uniformTypes || {})) {
      const wgslUniformType = uniformFormat;
      wgsl.push(`  ${uniformName} : ${wgslUniformType};`);
    }
    wgsl.push("};");
    wgsl.push(`var<uniform> ${module.name} : ${capitalize(module.name)};`);
    return wgsl.join("\n");
  }

  // src/lib/shader-generator/generate-shader.ts
  function generateShaderForModule(module, options) {
    switch (options.shaderLanguage) {
      case "glsl":
        return generateGLSLForModule(module, options);
      case "wgsl":
        return generateWGSLForModule(module, options);
    }
  }

  // src/lib/wgsl/get-shader-layout-wgsl.ts
  var import_core = __toESM(require_core(), 1);

  // ../../node_modules/wgsl_reflect/wgsl_reflect.module.js
  var e = class {
    constructor(e2, t2) {
      this.name = e2, this.attributes = t2, this.size = 0;
    }
    get isArray() {
      return false;
    }
    get isStruct() {
      return false;
    }
    get isTemplate() {
      return false;
    }
    getTypeName() {
      return this.name;
    }
  };
  var t = class {
    constructor(e2, t2, n2) {
      this.name = e2, this.type = t2, this.attributes = n2, this.offset = 0, this.size = 0;
    }
    get isArray() {
      return this.type.isArray;
    }
    get isStruct() {
      return this.type.isStruct;
    }
    get isTemplate() {
      return this.type.isTemplate;
    }
    get align() {
      return this.type.isStruct ? this.type.align : 0;
    }
    get members() {
      return this.type.isStruct ? this.type.members : null;
    }
    get format() {
      return this.type.isArray || this.type.isTemplate ? this.type.format : null;
    }
    get count() {
      return this.type.isArray ? this.type.count : 0;
    }
    get stride() {
      return this.type.isArray ? this.type.stride : this.size;
    }
  };
  var n = class extends e {
    constructor(e2, t2) {
      super(e2, t2), this.members = [], this.align = 0, this.startLine = -1, this.endLine = -1, this.inUse = false;
    }
    get isStruct() {
      return true;
    }
  };
  var s = class extends e {
    constructor(e2, t2) {
      super(e2, t2), this.count = 0, this.stride = 0;
    }
    get isArray() {
      return true;
    }
  };
  var r = class extends e {
    constructor(e2, t2, n2, s2) {
      super(e2, n2), this.format = t2, this.access = s2;
    }
    get isTemplate() {
      return true;
    }
    getTypeName() {
      let e2 = this.name;
      if (null !== this.format) {
        if ("vec2" === e2 || "vec3" === e2 || "vec4" === e2 || "mat2x2" === e2 || "mat2x3" === e2 || "mat2x4" === e2 || "mat3x2" === e2 || "mat3x3" === e2 || "mat3x4" === e2 || "mat4x2" === e2 || "mat4x3" === e2 || "mat4x4" === e2) {
          if ("f32" === this.format.name)
            return e2 += "f", e2;
          if ("i32" === this.format.name)
            return e2 += "i", e2;
          if ("u32" === this.format.name)
            return e2 += "u", e2;
          if ("bool" === this.format.name)
            return e2 += "b", e2;
          if ("f16" === this.format.name)
            return e2 += "h", e2;
        }
        e2 += `<${this.format.name}>`;
      } else if ("vec2" === e2 || "vec3" === e2 || "vec4" === e2)
        return e2;
      return e2;
    }
  };
  var a;
  ((e2) => {
    e2[e2.Uniform = 0] = "Uniform", e2[e2.Storage = 1] = "Storage", e2[e2.Texture = 2] = "Texture", e2[e2.Sampler = 3] = "Sampler", e2[e2.StorageTexture = 4] = "StorageTexture";
  })(a || (a = {}));
  var i = class {
    constructor(e2, t2, n2, s2, r2, a2, i2) {
      this.name = e2, this.type = t2, this.group = n2, this.binding = s2, this.attributes = r2, this.resourceType = a2, this.access = i2;
    }
    get isArray() {
      return this.type.isArray;
    }
    get isStruct() {
      return this.type.isStruct;
    }
    get isTemplate() {
      return this.type.isTemplate;
    }
    get size() {
      return this.type.size;
    }
    get align() {
      return this.type.isStruct ? this.type.align : 0;
    }
    get members() {
      return this.type.isStruct ? this.type.members : null;
    }
    get format() {
      return this.type.isArray || this.type.isTemplate ? this.type.format : null;
    }
    get count() {
      return this.type.isArray ? this.type.count : 0;
    }
    get stride() {
      return this.type.isArray ? this.type.stride : this.size;
    }
  };
  var o = class {
    constructor(e2, t2) {
      this.name = e2, this.type = t2;
    }
  };
  var l = class {
    constructor(e2, t2, n2, s2) {
      this.name = e2, this.type = t2, this.locationType = n2, this.location = s2, this.interpolation = null;
    }
  };
  var c = class {
    constructor(e2, t2, n2, s2) {
      this.name = e2, this.type = t2, this.locationType = n2, this.location = s2;
    }
  };
  var u = class {
    constructor(e2, t2, n2, s2) {
      this.name = e2, this.type = t2, this.attributes = n2, this.id = s2;
    }
  };
  var h = class {
    constructor(e2, t2, n2) {
      this.name = e2, this.type = t2, this.attributes = n2;
    }
  };
  var f = class {
    constructor(e2, t2 = null, n2) {
      this.stage = null, this.inputs = [], this.outputs = [], this.arguments = [], this.returnType = null, this.resources = [], this.overrides = [], this.startLine = -1, this.endLine = -1, this.inUse = false, this.calls = /* @__PURE__ */ new Set(), this.name = e2, this.stage = t2, this.attributes = n2;
    }
  };
  var p = class {
    constructor() {
      this.vertex = [], this.fragment = [], this.compute = [];
    }
  };
  var d = new Float32Array(1);
  var m = new Int32Array(d.buffer);
  var _ = new Uint16Array(1);
  function g(e2) {
    d[0] = e2;
    const t2 = m[0], n2 = t2 >> 31 & 1;
    let s2 = t2 >> 23 & 255, r2 = 8388607 & t2;
    if (255 === s2)
      return _[0] = n2 << 15 | 31744 | (0 !== r2 ? 512 : 0), _[0];
    if (0 === s2) {
      if (0 === r2)
        return _[0] = n2 << 15, _[0];
      r2 |= 8388608;
      let e3 = 113;
      for (; !(8388608 & r2); )
        r2 <<= 1, e3--;
      return s2 = 127 - e3, r2 &= 8388607, s2 > 0 ? (r2 = (r2 >> 126 - s2) + (r2 >> 127 - s2 & 1), _[0] = n2 << 15 | s2 << 10 | r2 >> 13, _[0]) : (_[0] = n2 << 15, _[0]);
    }
    return s2 = s2 - 127 + 15, s2 >= 31 ? (_[0] = n2 << 15 | 31744, _[0]) : s2 <= 0 ? s2 < -10 ? (_[0] = n2 << 15, _[0]) : (r2 = (8388608 | r2) >> 1 - s2, _[0] = n2 << 15 | r2 >> 13, _[0]) : (r2 >>= 13, _[0] = n2 << 15 | s2 << 10 | r2, _[0]);
  }
  var x = new Uint32Array(1);
  var y = new Float32Array(x.buffer, 0, 1);
  function b(e2) {
    const t2 = 112 + (e2 >> 6 & 31) << 23 | (63 & e2) << 17;
    return x[0] = t2, y[0];
  }
  function v(e2, t2, n2, s2, r2, a2, i2, o2, l2) {
    const c2 = s2 * (i2 >>= r2) * (a2 >>= r2) + n2 * i2 + t2 * o2;
    switch (l2) {
      case "r8unorm":
        return [w(e2, c2, "8unorm", 1)[0]];
      case "r8snorm":
        return [w(e2, c2, "8snorm", 1)[0]];
      case "r8uint":
        return [w(e2, c2, "8uint", 1)[0]];
      case "r8sint":
        return [w(e2, c2, "8sint", 1)[0]];
      case "rg8unorm": {
        const t3 = w(e2, c2, "8unorm", 2);
        return [t3[0], t3[1]];
      }
      case "rg8snorm": {
        const t3 = w(e2, c2, "8snorm", 2);
        return [t3[0], t3[1]];
      }
      case "rg8uint": {
        const t3 = w(e2, c2, "8uint", 2);
        return [t3[0], t3[1]];
      }
      case "rg8sint": {
        const t3 = w(e2, c2, "8sint", 2);
        return [t3[0], t3[1]];
      }
      case "rgba8unorm-srgb":
      case "rgba8unorm": {
        const t3 = w(e2, c2, "8unorm", 4);
        return [t3[0], t3[1], t3[2], t3[3]];
      }
      case "rgba8snorm": {
        const t3 = w(e2, c2, "8snorm", 4);
        return [t3[0], t3[1], t3[2], t3[3]];
      }
      case "rgba8uint": {
        const t3 = w(e2, c2, "8uint", 4);
        return [t3[0], t3[1], t3[2], t3[3]];
      }
      case "rgba8sint": {
        const t3 = w(e2, c2, "8sint", 4);
        return [t3[0], t3[1], t3[2], t3[3]];
      }
      case "bgra8unorm-srgb":
      case "bgra8unorm": {
        const t3 = w(e2, c2, "8unorm", 4);
        return [t3[2], t3[1], t3[0], t3[3]];
      }
      case "r16uint":
        return [w(e2, c2, "16uint", 1)[0]];
      case "r16sint":
        return [w(e2, c2, "16sint", 1)[0]];
      case "r16float":
        return [w(e2, c2, "16float", 1)[0]];
      case "rg16uint": {
        const t3 = w(e2, c2, "16uint", 2);
        return [t3[0], t3[1]];
      }
      case "rg16sint": {
        const t3 = w(e2, c2, "16sint", 2);
        return [t3[0], t3[1]];
      }
      case "rg16float": {
        const t3 = w(e2, c2, "16float", 2);
        return [t3[0], t3[1]];
      }
      case "rgba16uint": {
        const t3 = w(e2, c2, "16uint", 4);
        return [t3[0], t3[1], t3[2], t3[3]];
      }
      case "rgba16sint": {
        const t3 = w(e2, c2, "16sint", 4);
        return [t3[0], t3[1], t3[2], t3[3]];
      }
      case "rgba16float": {
        const t3 = w(e2, c2, "16float", 4);
        return [t3[0], t3[1], t3[2], t3[3]];
      }
      case "r32uint":
        return [w(e2, c2, "32uint", 1)[0]];
      case "r32sint":
        return [w(e2, c2, "32sint", 1)[0]];
      case "depth16unorm":
      case "depth24plus":
      case "depth24plus-stencil8":
      case "depth32float":
      case "depth32float-stencil8":
      case "r32float":
        return [w(e2, c2, "32float", 1)[0]];
      case "rg32uint": {
        const t3 = w(e2, c2, "32uint", 2);
        return [t3[0], t3[1]];
      }
      case "rg32sint": {
        const t3 = w(e2, c2, "32sint", 2);
        return [t3[0], t3[1]];
      }
      case "rg32float": {
        const t3 = w(e2, c2, "32float", 2);
        return [t3[0], t3[1]];
      }
      case "rgba32uint": {
        const t3 = w(e2, c2, "32uint", 4);
        return [t3[0], t3[1], t3[2], t3[3]];
      }
      case "rgba32sint": {
        const t3 = w(e2, c2, "32sint", 4);
        return [t3[0], t3[1], t3[2], t3[3]];
      }
      case "rgba32float": {
        const t3 = w(e2, c2, "32float", 4);
        return [t3[0], t3[1], t3[2], t3[3]];
      }
      case "rg11b10ufloat": {
        const t3 = new Uint32Array(e2.buffer, c2, 1)[0], n3 = (4192256 & t3) >> 11, s3 = (4290772992 & t3) >> 22;
        return [b(2047 & t3), b(n3), function(e3) {
          const t4 = 112 + (e3 >> 5 & 31) << 23 | (31 & e3) << 18;
          return x[0] = t4, y[0];
        }(s3), 1];
      }
    }
    return null;
  }
  function w(e2, t2, n2, s2) {
    const r2 = [0, 0, 0, 0];
    for (let c2 = 0; c2 < s2; ++c2)
      switch (n2) {
        case "8unorm":
          r2[c2] = e2[t2] / 255, t2++;
          break;
        case "8snorm":
          r2[c2] = e2[t2] / 255 * 2 - 1, t2++;
          break;
        case "8uint":
          r2[c2] = e2[t2], t2++;
          break;
        case "8sint":
          r2[c2] = e2[t2] - 127, t2++;
          break;
        case "16uint":
          r2[c2] = e2[t2] | e2[t2 + 1] << 8, t2 += 2;
          break;
        case "16sint":
          r2[c2] = (e2[t2] | e2[t2 + 1] << 8) - 32768, t2 += 2;
          break;
        case "16float":
          r2[c2] = (a2 = e2[t2] | e2[t2 + 1] << 8, i2 = void 0, o2 = void 0, l2 = void 0, i2 = (32768 & a2) >> 15, l2 = 1023 & a2, 0 == (o2 = (31744 & a2) >> 10) ? (i2 ? -1 : 1) * Math.pow(2, -14) * (l2 / Math.pow(2, 10)) : 31 == o2 ? l2 ? NaN : 1 / 0 * (i2 ? -1 : 1) : (i2 ? -1 : 1) * Math.pow(2, o2 - 15) * (1 + l2 / Math.pow(2, 10))), t2 += 2;
          break;
        case "32uint":
        case "32sint":
          r2[c2] = e2[t2] | e2[t2 + 1] << 8 | e2[t2 + 2] << 16 | e2[t2 + 3] << 24, t2 += 4;
          break;
        case "32float":
          r2[c2] = new Float32Array(e2.buffer, t2, 1)[0], t2 += 4;
      }
    var a2, i2, o2, l2;
    return r2;
  }
  function k(e2, t2, n2, s2, r2) {
    for (let a2 = 0; a2 < s2; ++a2)
      switch (n2) {
        case "8unorm":
          e2[t2] = 255 * r2[a2], t2++;
          break;
        case "8snorm":
          e2[t2] = 0.5 * (r2[a2] + 1) * 255, t2++;
          break;
        case "8uint":
          e2[t2] = r2[a2], t2++;
          break;
        case "8sint":
          e2[t2] = r2[a2] + 127, t2++;
          break;
        case "16uint":
          new Uint16Array(e2.buffer, t2, 1)[0] = r2[a2], t2 += 2;
          break;
        case "16sint":
          new Int16Array(e2.buffer, t2, 1)[0] = r2[a2], t2 += 2;
          break;
        case "16float": {
          const n3 = g(r2[a2]);
          new Uint16Array(e2.buffer, t2, 1)[0] = n3, t2 += 2;
          break;
        }
        case "32uint":
          new Uint32Array(e2.buffer, t2, 1)[0] = r2[a2], t2 += 4;
          break;
        case "32sint":
          new Int32Array(e2.buffer, t2, 1)[0] = r2[a2], t2 += 4;
          break;
        case "32float":
          new Float32Array(e2.buffer, t2, 1)[0] = r2[a2], t2 += 4;
      }
    return r2;
  }
  var I = { r8unorm: { bytesPerBlock: 1, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 1 }, r8snorm: { bytesPerBlock: 1, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 1 }, r8uint: { bytesPerBlock: 1, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 1 }, r8sint: { bytesPerBlock: 1, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 1 }, rg8unorm: { bytesPerBlock: 2, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 2 }, rg8snorm: { bytesPerBlock: 2, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 2 }, rg8uint: { bytesPerBlock: 2, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 2 }, rg8sint: { bytesPerBlock: 2, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 2 }, rgba8unorm: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, "rgba8unorm-srgb": { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, rgba8snorm: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, rgba8uint: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, rgba8sint: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, bgra8unorm: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, "bgra8unorm-srgb": { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, r16uint: { bytesPerBlock: 2, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 1 }, r16sint: { bytesPerBlock: 2, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 1 }, r16float: { bytesPerBlock: 2, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 1 }, rg16uint: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 2 }, rg16sint: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 2 }, rg16float: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 2 }, rgba16uint: { bytesPerBlock: 8, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, rgba16sint: { bytesPerBlock: 8, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, rgba16float: { bytesPerBlock: 8, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, r32uint: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 1 }, r32sint: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 1 }, r32float: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 1 }, rg32uint: { bytesPerBlock: 8, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 2 }, rg32sint: { bytesPerBlock: 8, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 2 }, rg32float: { bytesPerBlock: 8, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 2 }, rgba32uint: { bytesPerBlock: 16, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, rgba32sint: { bytesPerBlock: 16, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, rgba32float: { bytesPerBlock: 16, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, rgb10a2uint: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, rgb10a2unorm: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, rg11b10ufloat: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, stencil8: { bytesPerBlock: 1, blockWidth: 1, blockHeight: 1, isCompressed: false, isDepthStencil: true, hasDepth: false, hasStencil: true, channels: 1 }, depth16unorm: { bytesPerBlock: 2, blockWidth: 1, blockHeight: 1, isCompressed: false, isDepthStencil: true, hasDepth: true, hasStencil: false, channels: 1 }, depth24plus: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, isDepthStencil: true, hasDepth: true, hasStencil: false, depthOnlyFormat: "depth32float", channels: 1 }, "depth24plus-stencil8": { bytesPerBlock: 8, blockWidth: 1, blockHeight: 1, isCompressed: false, isDepthStencil: true, hasDepth: true, hasStencil: true, depthOnlyFormat: "depth32float", channels: 1 }, depth32float: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, isDepthStencil: true, hasDepth: true, hasStencil: false, channels: 1 }, "depth32float-stencil8": { bytesPerBlock: 8, blockWidth: 1, blockHeight: 1, isCompressed: false, isDepthStencil: true, hasDepth: true, hasStencil: true, stencilOnlyFormat: "depth32float", channels: 1 }, rgb9e5ufloat: { bytesPerBlock: 4, blockWidth: 1, blockHeight: 1, isCompressed: false, channels: 4 }, "bc1-rgba-unorm": { bytesPerBlock: 8, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "bc1-rgba-unorm-srgb": { bytesPerBlock: 8, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "bc2-rgba-unorm": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "bc2-rgba-unorm-srgb": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "bc3-rgba-unorm": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "bc3-rgba-unorm-srgb": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "bc4-r-unorm": { bytesPerBlock: 8, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 1 }, "bc4-r-snorm": { bytesPerBlock: 8, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 1 }, "bc5-rg-unorm": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 2 }, "bc5-rg-snorm": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 2 }, "bc6h-rgb-ufloat": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "bc6h-rgb-float": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "bc7-rgba-unorm": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "bc7-rgba-unorm-srgb": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "etc2-rgb8unorm": { bytesPerBlock: 8, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "etc2-rgb8unorm-srgb": { bytesPerBlock: 8, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "etc2-rgb8a1unorm": { bytesPerBlock: 8, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "etc2-rgb8a1unorm-srgb": { bytesPerBlock: 8, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "etc2-rgba8unorm": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "etc2-rgba8unorm-srgb": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "eac-r11unorm": { bytesPerBlock: 8, blockWidth: 1, blockHeight: 1, isCompressed: true, channels: 1 }, "eac-r11snorm": { bytesPerBlock: 8, blockWidth: 1, blockHeight: 1, isCompressed: true, channels: 1 }, "eac-rg11unorm": { bytesPerBlock: 16, blockWidth: 1, blockHeight: 1, isCompressed: true, channels: 2 }, "eac-rg11snorm": { bytesPerBlock: 16, blockWidth: 1, blockHeight: 1, isCompressed: true, channels: 2 }, "astc-4x4-unorm": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "astc-4x4-unorm-srgb": { bytesPerBlock: 16, blockWidth: 4, blockHeight: 4, isCompressed: true, channels: 4 }, "astc-5x4-unorm": { bytesPerBlock: 16, blockWidth: 5, blockHeight: 4, isCompressed: true, channels: 4 }, "astc-5x4-unorm-srgb": { bytesPerBlock: 16, blockWidth: 5, blockHeight: 4, isCompressed: true, channels: 4 }, "astc-5x5-unorm": { bytesPerBlock: 16, blockWidth: 5, blockHeight: 5, isCompressed: true, channels: 4 }, "astc-5x5-unorm-srgb": { bytesPerBlock: 16, blockWidth: 5, blockHeight: 5, isCompressed: true, channels: 4 }, "astc-6x5-unorm": { bytesPerBlock: 16, blockWidth: 6, blockHeight: 5, isCompressed: true, channels: 4 }, "astc-6x5-unorm-srgb": { bytesPerBlock: 16, blockWidth: 6, blockHeight: 5, isCompressed: true, channels: 4 }, "astc-6x6-unorm": { bytesPerBlock: 16, blockWidth: 6, blockHeight: 6, isCompressed: true, channels: 4 }, "astc-6x6-unorm-srgb": { bytesPerBlock: 16, blockWidth: 6, blockHeight: 6, isCompressed: true, channels: 4 }, "astc-8x5-unorm": { bytesPerBlock: 16, blockWidth: 8, blockHeight: 5, isCompressed: true, channels: 4 }, "astc-8x5-unorm-srgb": { bytesPerBlock: 16, blockWidth: 8, blockHeight: 5, isCompressed: true, channels: 4 }, "astc-8x6-unorm": { bytesPerBlock: 16, blockWidth: 8, blockHeight: 6, isCompressed: true, channels: 4 }, "astc-8x6-unorm-srgb": { bytesPerBlock: 16, blockWidth: 8, blockHeight: 6, isCompressed: true, channels: 4 }, "astc-8x8-unorm": { bytesPerBlock: 16, blockWidth: 8, blockHeight: 8, isCompressed: true, channels: 4 }, "astc-8x8-unorm-srgb": { bytesPerBlock: 16, blockWidth: 8, blockHeight: 8, isCompressed: true, channels: 4 }, "astc-10x5-unorm": { bytesPerBlock: 16, blockWidth: 10, blockHeight: 5, isCompressed: true, channels: 4 }, "astc-10x5-unorm-srgb": { bytesPerBlock: 16, blockWidth: 10, blockHeight: 5, isCompressed: true, channels: 4 }, "astc-10x6-unorm": { bytesPerBlock: 16, blockWidth: 10, blockHeight: 6, isCompressed: true, channels: 4 }, "astc-10x6-unorm-srgb": { bytesPerBlock: 16, blockWidth: 10, blockHeight: 6, isCompressed: true, channels: 4 }, "astc-10x8-unorm": { bytesPerBlock: 16, blockWidth: 10, blockHeight: 8, isCompressed: true, channels: 4 }, "astc-10x8-unorm-srgb": { bytesPerBlock: 16, blockWidth: 10, blockHeight: 8, isCompressed: true, channels: 4 }, "astc-10x10-unorm": { bytesPerBlock: 16, blockWidth: 10, blockHeight: 10, isCompressed: true, channels: 4 }, "astc-10x10-unorm-srgb": { bytesPerBlock: 16, blockWidth: 10, blockHeight: 10, isCompressed: true, channels: 4 }, "astc-12x10-unorm": { bytesPerBlock: 16, blockWidth: 12, blockHeight: 10, isCompressed: true, channels: 4 }, "astc-12x10-unorm-srgb": { bytesPerBlock: 16, blockWidth: 12, blockHeight: 10, isCompressed: true, channels: 4 }, "astc-12x12-unorm": { bytesPerBlock: 16, blockWidth: 12, blockHeight: 12, isCompressed: true, channels: 4 }, "astc-12x12-unorm-srgb": { bytesPerBlock: 16, blockWidth: 12, blockHeight: 12, isCompressed: true, channels: 4 } };
  var T = class {
    constructor() {
      this.id = T._id++, this.line = 0;
    }
    get isAstNode() {
      return true;
    }
    get astNodeType() {
      return "";
    }
    search(e2) {
      e2(this);
    }
    searchBlock(e2, t2) {
      if (e2) {
        t2(S.instance);
        for (const n2 of e2)
          n2 instanceof Array ? this.searchBlock(n2, t2) : n2.search(t2);
        t2(A.instance);
      }
    }
    constEvaluate(e2, t2) {
      throw new Error("Cannot evaluate node");
    }
    constEvaluateString(e2) {
      return this.constEvaluate(e2).toString();
    }
  };
  T._id = 0;
  var S = class extends T {
  };
  S.instance = new S();
  var A = class extends T {
  };
  A.instance = new A();
  var E = /* @__PURE__ */ new Set(["all", "all", "any", "select", "arrayLength", "abs", "acos", "acosh", "asin", "asinh", "atan", "atanh", "atan2", "ceil", "clamp", "cos", "cosh", "countLeadingZeros", "countOneBits", "countTrailingZeros", "cross", "degrees", "determinant", "distance", "dot", "dot4U8Packed", "dot4I8Packed", "exp", "exp2", "extractBits", "faceForward", "firstLeadingBit", "firstTrailingBit", "floor", "fma", "fract", "frexp", "insertBits", "inverseSqrt", "ldexp", "length", "log", "log2", "max", "min", "mix", "modf", "normalize", "pow", "quantizeToF16", "radians", "reflect", "refract", "reverseBits", "round", "saturate", "sign", "sin", "sinh", "smoothStep", "sqrt", "step", "tan", "tanh", "transpose", "trunc", "dpdx", "dpdxCoarse", "dpdxFine", "dpdy", "dpdyCoarse", "dpdyFine", "fwidth", "fwidthCoarse", "fwidthFine", "textureDimensions", "textureGather", "textureGatherCompare", "textureLoad", "textureNumLayers", "textureNumLevels", "textureNumSamples", "textureSample", "textureSampleBias", "textureSampleCompare", "textureSampleCompareLevel", "textureSampleGrad", "textureSampleLevel", "textureSampleBaseClampToEdge", "textureStore", "atomicLoad", "atomicStore", "atomicAdd", "atomicSub", "atomicMax", "atomicMin", "atomicAnd", "atomicOr", "atomicXor", "atomicExchange", "atomicCompareExchangeWeak", "pack4x8snorm", "pack4x8unorm", "pack4xI8", "pack4xU8", "pack4x8Clamp", "pack4xU8Clamp", "pack2x16snorm", "pack2x16unorm", "pack2x16float", "unpack4x8snorm", "unpack4x8unorm", "unpack4xI8", "unpack4xU8", "unpack2x16snorm", "unpack2x16unorm", "unpack2x16float", "storageBarrier", "textureBarrier", "workgroupBarrier", "workgroupUniformLoad", "subgroupAdd", "subgroupExclusiveAdd", "subgroupInclusiveAdd", "subgroupAll", "subgroupAnd", "subgroupAny", "subgroupBallot", "subgroupBroadcast", "subgroupBroadcastFirst", "subgroupElect", "subgroupMax", "subgroupMin", "subgroupMul", "subgroupExclusiveMul", "subgroupInclusiveMul", "subgroupOr", "subgroupShuffle", "subgroupShuffleDown", "subgroupShuffleUp", "subgroupShuffleXor", "subgroupXor", "quadBroadcast", "quadSwapDiagonal", "quadSwapX", "quadSwapY"]);
  var $ = class extends T {
    constructor() {
      super();
    }
  };
  var L = class extends $ {
    constructor(e2, t2, n2, s2, r2, a2) {
      super(), this.calls = /* @__PURE__ */ new Set(), this.name = e2, this.args = t2, this.returnType = n2, this.body = s2, this.startLine = r2, this.endLine = a2;
    }
    get astNodeType() {
      return "function";
    }
    search(e2) {
      if (this.attributes)
        for (const t2 of this.attributes)
          e2(t2);
      e2(this);
      for (const t2 of this.args)
        e2(t2);
      this.searchBlock(this.body, e2);
    }
  };
  var C = class extends $ {
    constructor(e2) {
      super(), this.expression = e2;
    }
    get astNodeType() {
      return "staticAssert";
    }
    search(e2) {
      this.expression.search(e2);
    }
  };
  var D = class extends $ {
    constructor(e2, t2) {
      super(), this.condition = e2, this.body = t2;
    }
    get astNodeType() {
      return "while";
    }
    search(e2) {
      this.condition.search(e2), this.searchBlock(this.body, e2);
    }
  };
  var N = class extends $ {
    constructor(e2, t2) {
      super(), this.body = e2, this.loopId = t2;
    }
    get astNodeType() {
      return "continuing";
    }
    search(e2) {
      this.searchBlock(this.body, e2);
    }
  };
  var V = class extends $ {
    constructor(e2, t2, n2, s2) {
      super(), this.init = e2, this.condition = t2, this.increment = n2, this.body = s2;
    }
    get astNodeType() {
      return "for";
    }
    search(e2) {
      var t2, n2, s2;
      null === (t2 = this.init) || void 0 === t2 || t2.search(e2), null === (n2 = this.condition) || void 0 === n2 || n2.search(e2), null === (s2 = this.increment) || void 0 === s2 || s2.search(e2), this.searchBlock(this.body, e2);
    }
  };
  var O = class extends $ {
    constructor(e2, t2, n2, s2, r2) {
      super(), this.attributes = null, this.name = e2, this.type = t2, this.storage = n2, this.access = s2, this.value = r2;
    }
    get astNodeType() {
      return "var";
    }
    search(e2) {
      var t2;
      e2(this), null === (t2 = this.value) || void 0 === t2 || t2.search(e2);
    }
  };
  var B = class extends $ {
    constructor(e2, t2, n2) {
      super(), this.attributes = null, this.name = e2, this.type = t2, this.value = n2;
    }
    get astNodeType() {
      return "override";
    }
    search(e2) {
      var t2;
      null === (t2 = this.value) || void 0 === t2 || t2.search(e2);
    }
  };
  var F = class extends $ {
    constructor(e2, t2, n2, s2, r2) {
      super(), this.attributes = null, this.name = e2, this.type = t2, this.storage = n2, this.access = s2, this.value = r2;
    }
    get astNodeType() {
      return "let";
    }
    search(e2) {
      var t2;
      e2(this), null === (t2 = this.value) || void 0 === t2 || t2.search(e2);
    }
  };
  var M = class extends $ {
    constructor(e2, t2, n2, s2, r2) {
      super(), this.attributes = null, this.name = e2, this.type = t2, this.storage = n2, this.access = s2, this.value = r2;
    }
    get astNodeType() {
      return "const";
    }
    constEvaluate(e2, t2) {
      return this.value.constEvaluate(e2, t2);
    }
    search(e2) {
      var t2;
      e2(this), null === (t2 = this.value) || void 0 === t2 || t2.search(e2);
    }
  };
  var U;
  var P;
  var W;
  var q;
  ((e2) => {
    e2.increment = "++", e2.decrement = "--";
  })(U || (U = {})), ((e2) => {
    e2.parse = function(t2) {
      const n2 = t2;
      if ("parse" == n2)
        throw new Error("Invalid value for IncrementOperator");
      return e2[n2];
    };
  })(U || (U = {}));
  var H = class extends $ {
    constructor(e2, t2) {
      super(), this.operator = e2, this.variable = t2;
    }
    get astNodeType() {
      return "increment";
    }
    search(e2) {
      this.variable.search(e2);
    }
  };
  ((e2) => {
    e2.assign = "=", e2.addAssign = "+=", e2.subtractAssin = "-=", e2.multiplyAssign = "*=", e2.divideAssign = "/=", e2.moduloAssign = "%=", e2.andAssign = "&=", e2.orAssign = "|=", e2.xorAssign = "^=", e2.shiftLeftAssign = "<<=", e2.shiftRightAssign = ">>=";
  })(P || (P = {})), ((e2) => {
    e2.parse = function(e3) {
      const t2 = e3;
      if ("parse" == t2)
        throw new Error("Invalid value for AssignOperator");
      return t2;
    };
  })(P || (P = {}));
  var z = class extends $ {
    constructor(e2, t2, n2) {
      super(), this.operator = e2, this.variable = t2, this.value = n2;
    }
    get astNodeType() {
      return "assign";
    }
    search(e2) {
      this.variable.search(e2), this.value.search(e2);
    }
  };
  var R = class extends $ {
    constructor(e2, t2) {
      super(), this.name = e2, this.args = t2;
    }
    get astNodeType() {
      return "call";
    }
    isBuiltin() {
      return E.has(this.name);
    }
    search(e2) {
      for (const t2 of this.args)
        t2.search(e2);
      e2(this);
    }
  };
  var G = class extends $ {
    constructor(e2, t2) {
      super(), this.body = e2, this.continuing = t2;
    }
    get astNodeType() {
      return "loop";
    }
  };
  var X = class extends $ {
    constructor(e2, t2) {
      super(), this.condition = e2, this.cases = t2;
    }
    get astNodeType() {
      return "switch";
    }
  };
  var j = class extends $ {
    constructor(e2, t2, n2, s2) {
      super(), this.condition = e2, this.body = t2, this.elseif = n2, this.else = s2;
    }
    get astNodeType() {
      return "if";
    }
    search(e2) {
      this.condition.search(e2), this.searchBlock(this.body, e2), this.searchBlock(this.elseif, e2), this.searchBlock(this.else, e2);
    }
  };
  var Z = class extends $ {
    constructor(e2) {
      super(), this.value = e2;
    }
    get astNodeType() {
      return "return";
    }
    search(e2) {
      var t2;
      null === (t2 = this.value) || void 0 === t2 || t2.search(e2);
    }
  };
  var Q = class extends $ {
    constructor(e2) {
      super(), this.name = e2;
    }
    get astNodeType() {
      return "enable";
    }
  };
  var Y = class extends $ {
    constructor(e2) {
      super(), this.extensions = e2;
    }
    get astNodeType() {
      return "requires";
    }
  };
  var K = class extends $ {
    constructor(e2, t2) {
      super(), this.severity = e2, this.rule = t2;
    }
    get astNodeType() {
      return "diagnostic";
    }
  };
  var J = class extends $ {
    constructor(e2, t2) {
      super(), this.name = e2, this.type = t2;
    }
    get astNodeType() {
      return "alias";
    }
  };
  var ee = class extends $ {
    constructor() {
      super();
    }
    get astNodeType() {
      return "discard";
    }
  };
  var te = class extends $ {
    constructor() {
      super(), this.condition = null, this.loopId = -1;
    }
    get astNodeType() {
      return "break";
    }
  };
  var ne = class extends $ {
    constructor() {
      super(), this.loopId = -1;
    }
    get astNodeType() {
      return "continue";
    }
  };
  var se = class extends $ {
    constructor(e2) {
      super(), this.attributes = null, this.name = e2;
    }
    get astNodeType() {
      return "type";
    }
    get isStruct() {
      return false;
    }
    get isArray() {
      return false;
    }
    static maxFormatType(e2) {
      let t2 = e2[0];
      if ("f32" === t2.name)
        return t2;
      for (let n2 = 1; n2 < e2.length; ++n2) {
        const s2 = se._priority.get(t2.name);
        se._priority.get(e2[n2].name) < s2 && (t2 = e2[n2]);
      }
      return "x32" === t2.name ? se.i32 : t2;
    }
    getTypeName() {
      return this.name;
    }
  };
  se.x32 = new se("x32"), se.f32 = new se("f32"), se.i32 = new se("i32"), se.u32 = new se("u32"), se.f16 = new se("f16"), se.bool = new se("bool"), se.void = new se("void"), se._priority = /* @__PURE__ */ new Map([["f32", 0], ["f16", 1], ["u32", 2], ["i32", 3], ["x32", 3]]);
  var re = class extends se {
    constructor(e2) {
      super(e2);
    }
  };
  var ae = class extends se {
    constructor(e2, t2, n2, s2) {
      super(e2), this.members = t2, this.startLine = n2, this.endLine = s2;
    }
    get astNodeType() {
      return "struct";
    }
    get isStruct() {
      return true;
    }
    getMemberIndex(e2) {
      for (let t2 = 0; t2 < this.members.length; t2++)
        if (this.members[t2].name == e2)
          return t2;
      return -1;
    }
    search(e2) {
      for (const t2 of this.members)
        e2(t2);
    }
  };
  var ie = class extends se {
    constructor(e2, t2, n2) {
      super(e2), this.format = t2, this.access = n2;
    }
    get astNodeType() {
      return "template";
    }
    getTypeName() {
      let e2 = this.name;
      if (null !== this.format) {
        if ("vec2" === e2 || "vec3" === e2 || "vec4" === e2 || "mat2x2" === e2 || "mat2x3" === e2 || "mat2x4" === e2 || "mat3x2" === e2 || "mat3x3" === e2 || "mat3x4" === e2 || "mat4x2" === e2 || "mat4x3" === e2 || "mat4x4" === e2) {
          if ("f32" === this.format.name)
            return e2 += "f", e2;
          if ("i32" === this.format.name)
            return e2 += "i", e2;
          if ("u32" === this.format.name)
            return e2 += "u", e2;
          if ("bool" === this.format.name)
            return e2 += "b", e2;
          if ("f16" === this.format.name)
            return e2 += "h", e2;
        }
        e2 += `<${this.format.name}>`;
      } else if ("vec2" === e2 || "vec3" === e2 || "vec4" === e2)
        return e2;
      return e2;
    }
  };
  ie.vec2f = new ie("vec2", se.f32, null), ie.vec3f = new ie("vec3", se.f32, null), ie.vec4f = new ie("vec4", se.f32, null), ie.vec2i = new ie("vec2", se.i32, null), ie.vec3i = new ie("vec3", se.i32, null), ie.vec4i = new ie("vec4", se.i32, null), ie.vec2u = new ie("vec2", se.u32, null), ie.vec3u = new ie("vec3", se.u32, null), ie.vec4u = new ie("vec4", se.u32, null), ie.vec2h = new ie("vec2", se.f16, null), ie.vec3h = new ie("vec3", se.f16, null), ie.vec4h = new ie("vec4", se.f16, null), ie.vec2b = new ie("vec2", se.bool, null), ie.vec3b = new ie("vec3", se.bool, null), ie.vec4b = new ie("vec4", se.bool, null), ie.mat2x2f = new ie("mat2x2", se.f32, null), ie.mat2x3f = new ie("mat2x3", se.f32, null), ie.mat2x4f = new ie("mat2x4", se.f32, null), ie.mat3x2f = new ie("mat3x2", se.f32, null), ie.mat3x3f = new ie("mat3x3", se.f32, null), ie.mat3x4f = new ie("mat3x4", se.f32, null), ie.mat4x2f = new ie("mat4x2", se.f32, null), ie.mat4x3f = new ie("mat4x3", se.f32, null), ie.mat4x4f = new ie("mat4x4", se.f32, null), ie.mat2x2h = new ie("mat2x2", se.f16, null), ie.mat2x3h = new ie("mat2x3", se.f16, null), ie.mat2x4h = new ie("mat2x4", se.f16, null), ie.mat3x2h = new ie("mat3x2", se.f16, null), ie.mat3x3h = new ie("mat3x3", se.f16, null), ie.mat3x4h = new ie("mat3x4", se.f16, null), ie.mat4x2h = new ie("mat4x2", se.f16, null), ie.mat4x3h = new ie("mat4x3", se.f16, null), ie.mat4x4h = new ie("mat4x4", se.f16, null), ie.mat2x2i = new ie("mat2x2", se.i32, null), ie.mat2x3i = new ie("mat2x3", se.i32, null), ie.mat2x4i = new ie("mat2x4", se.i32, null), ie.mat3x2i = new ie("mat3x2", se.i32, null), ie.mat3x3i = new ie("mat3x3", se.i32, null), ie.mat3x4i = new ie("mat3x4", se.i32, null), ie.mat4x2i = new ie("mat4x2", se.i32, null), ie.mat4x3i = new ie("mat4x3", se.i32, null), ie.mat4x4i = new ie("mat4x4", se.i32, null), ie.mat2x2u = new ie("mat2x2", se.u32, null), ie.mat2x3u = new ie("mat2x3", se.u32, null), ie.mat2x4u = new ie("mat2x4", se.u32, null), ie.mat3x2u = new ie("mat3x2", se.u32, null), ie.mat3x3u = new ie("mat3x3", se.u32, null), ie.mat3x4u = new ie("mat3x4", se.u32, null), ie.mat4x2u = new ie("mat4x2", se.u32, null), ie.mat4x3u = new ie("mat4x3", se.u32, null), ie.mat4x4u = new ie("mat4x4", se.u32, null);
  var oe = class extends se {
    constructor(e2, t2, n2, s2) {
      super(e2), this.storage = t2, this.type = n2, this.access = s2;
    }
    get astNodeType() {
      return "pointer";
    }
  };
  var le = class extends se {
    constructor(e2, t2, n2, s2) {
      super(e2), this.attributes = t2, this.format = n2, this.count = s2;
    }
    get astNodeType() {
      return "array";
    }
    get isArray() {
      return true;
    }
  };
  var ce = class extends se {
    constructor(e2, t2, n2) {
      super(e2), this.format = t2, this.access = n2;
    }
    get astNodeType() {
      return "sampler";
    }
  };
  var ue = class extends T {
    constructor() {
      super(), this.postfix = null;
    }
  };
  var he = class extends ue {
    constructor(e2) {
      super(), this.value = e2;
    }
    get astNodeType() {
      return "stringExpr";
    }
    toString() {
      return this.value;
    }
    constEvaluateString() {
      return this.value;
    }
  };
  var fe = class extends ue {
    constructor(e2, t2) {
      super(), this.type = e2, this.args = t2;
    }
    get astNodeType() {
      return "createExpr";
    }
    search(e2) {
      if (e2(this), this.args)
        for (const t2 of this.args)
          t2.search(e2);
    }
    constEvaluate(e2, t2) {
      return t2 && (t2[0] = this.type), e2.evalExpression(this, e2.context);
    }
  };
  var pe = class extends ue {
    constructor(e2, t2) {
      super(), this.cachedReturnValue = null, this.name = e2, this.args = t2;
    }
    get astNodeType() {
      return "callExpr";
    }
    setCachedReturnValue(e2) {
      this.cachedReturnValue = e2;
    }
    get isBuiltin() {
      return E.has(this.name);
    }
    constEvaluate(e2, t2) {
      return e2.evalExpression(this, e2.context);
    }
    search(e2) {
      for (const t2 of this.args)
        t2.search(e2);
      e2(this);
    }
  };
  var de = class extends ue {
    constructor(e2) {
      super(), this.name = e2;
    }
    get astNodeType() {
      return "varExpr";
    }
    search(e2) {
      e2(this), this.postfix && this.postfix.search(e2);
    }
    constEvaluate(e2, t2) {
      return e2.evalExpression(this, e2.context);
    }
  };
  var me = class extends ue {
    constructor(e2, t2) {
      super(), this.name = e2, this.initializer = t2;
    }
    get astNodeType() {
      return "constExpr";
    }
    constEvaluate(e2, t2) {
      if (this.initializer) {
        const t3 = e2.evalExpression(this.initializer, e2.context);
        return null !== t3 && this.postfix ? t3.getSubData(e2, this.postfix, e2.context) : t3;
      }
      return null;
    }
    search(e2) {
      this.initializer.search(e2);
    }
  };
  var _e = class extends ue {
    constructor(e2, t2) {
      super(), this.value = e2, this.type = t2;
    }
    get astNodeType() {
      return "literalExpr";
    }
    constEvaluate(e2, t2) {
      return void 0 !== t2 && (t2[0] = this.type), this.value;
    }
    get isScalar() {
      return this.value instanceof Ve;
    }
    get isVector() {
      return this.value instanceof Be || this.value instanceof Fe;
    }
    get scalarValue() {
      return this.value instanceof Ve ? this.value.value : (console.error("Value is not scalar."), 0);
    }
    get vectorValue() {
      return this.value instanceof Be || this.value instanceof Fe ? this.value.data : (console.error("Value is not a vector or matrix."), new Float32Array(0));
    }
  };
  var ge = class extends ue {
    constructor(e2, t2) {
      super(), this.type = e2, this.value = t2;
    }
    get astNodeType() {
      return "bitcastExpr";
    }
    search(e2) {
      this.value.search(e2);
    }
  };
  var ye = class extends ue {
    constructor(e2) {
      super(), this.index = e2;
    }
    search(e2) {
      this.index.search(e2);
    }
  };
  var be = class extends ue {
    constructor() {
      super();
    }
  };
  var ve = class extends be {
    constructor(e2, t2) {
      super(), this.operator = e2, this.right = t2;
    }
    get astNodeType() {
      return "unaryOp";
    }
    constEvaluate(e2, t2) {
      return e2.evalExpression(this, e2.context);
    }
    search(e2) {
      this.right.search(e2);
    }
  };
  var we = class extends be {
    constructor(e2, t2, n2) {
      super(), this.operator = e2, this.left = t2, this.right = n2;
    }
    get astNodeType() {
      return "binaryOp";
    }
    _getPromotedType(e2, t2) {
      return e2.name === t2.name ? e2 : "f32" === e2.name || "f32" === t2.name ? se.f32 : "u32" === e2.name || "u32" === t2.name ? se.u32 : se.i32;
    }
    constEvaluate(e2, t2) {
      return e2.evalExpression(this, e2.context);
    }
    search(e2) {
      this.left.search(e2), this.right.search(e2);
    }
  };
  var ke = class extends T {
    constructor(e2) {
      super(), this.body = e2;
    }
  };
  var Ie = class extends ue {
    constructor() {
      super();
    }
    get astNodeType() {
      return "default";
    }
  };
  var Te = class extends ke {
    constructor(e2, t2) {
      super(t2), this.selectors = e2;
    }
    get astNodeType() {
      return "case";
    }
    search(e2) {
      this.searchBlock(this.body, e2);
    }
  };
  var Se = class extends ke {
    constructor(e2) {
      super(e2);
    }
    get astNodeType() {
      return "default";
    }
    search(e2) {
      this.searchBlock(this.body, e2);
    }
  };
  var Ae = class extends T {
    constructor(e2, t2, n2) {
      super(), this.name = e2, this.type = t2, this.attributes = n2;
    }
    get astNodeType() {
      return "argument";
    }
  };
  var Ee = class extends T {
    constructor(e2, t2) {
      super(), this.condition = e2, this.body = t2;
    }
    get astNodeType() {
      return "elseif";
    }
    search(e2) {
      this.condition.search(e2), this.searchBlock(this.body, e2);
    }
  };
  var $e = class extends T {
    constructor(e2, t2, n2) {
      super(), this.name = e2, this.type = t2, this.attributes = n2;
    }
    get astNodeType() {
      return "member";
    }
  };
  var Le = class extends T {
    constructor(e2, t2) {
      super(), this.name = e2, this.value = t2;
    }
    get astNodeType() {
      return "attribute";
    }
  };
  var Ce = class {
    constructor(e2, t2) {
      this.parent = null, this.typeInfo = e2, this.parent = t2, this.id = Ce._id++;
    }
    clone() {
      throw `Clone: Not implemented for ${this.constructor.name}`;
    }
    setDataValue(e2, t2, n2, s2) {
      console.error(`SetDataValue: Not implemented for ${this.constructor.name}`);
    }
    getSubData(e2, t2, n2) {
      return console.error(`GetDataValue: Not implemented for ${this.constructor.name}`), null;
    }
    toString() {
      return `<${this.typeInfo.name}>`;
    }
  };
  Ce._id = 0;
  var De = class extends Ce {
    constructor() {
      super(new e("void", null), null);
    }
    toString() {
      return "void";
    }
  };
  De.void = new De();
  var Ne = class extends Ce {
    constructor(t2) {
      super(new e("pointer", null), null), this.reference = t2;
    }
    clone() {
      return this;
    }
    setDataValue(e2, t2, n2, s2) {
      this.reference.setDataValue(e2, t2, n2, s2);
    }
    getSubData(e2, t2, n2) {
      return t2 ? this.reference.getSubData(e2, t2, n2) : this;
    }
  };
  var Ve = class extends Ce {
    constructor(e2, t2, n2 = null) {
      super(t2, n2), e2 instanceof Int32Array || e2 instanceof Uint32Array || e2 instanceof Float32Array ? this.data = e2 : "x32" === this.typeInfo.name ? e2 - Math.floor(e2) != 0 ? this.data = new Float32Array([e2]) : this.data = e2 >= 0 ? new Uint32Array([e2]) : new Int32Array([e2]) : "i32" === this.typeInfo.name || "bool" === this.typeInfo.name ? this.data = new Int32Array([e2]) : "u32" === this.typeInfo.name ? this.data = new Uint32Array([e2]) : "f32" === this.typeInfo.name || "f16" === this.typeInfo.name ? this.data = new Float32Array([e2]) : console.error("ScalarData2: Invalid type", t2);
    }
    clone() {
      if (this.data instanceof Float32Array)
        return new Ve(new Float32Array(this.data), this.typeInfo, null);
      if (this.data instanceof Int32Array)
        return new Ve(new Int32Array(this.data), this.typeInfo, null);
      if (this.data instanceof Uint32Array)
        return new Ve(new Uint32Array(this.data), this.typeInfo, null);
      throw "ScalarData: Invalid data type";
    }
    get value() {
      return this.data[0];
    }
    set value(e2) {
      this.data[0] = e2;
    }
    setDataValue(e2, t2, n2, s2) {
      if (n2)
        return void console.error("SetDataValue: Scalar data does not support postfix", n2);
      if (!(t2 instanceof Ve))
        return void console.error("SetDataValue: Invalid value", t2);
      let r2 = t2.data[0];
      "i32" === this.typeInfo.name || "u32" === this.typeInfo.name ? r2 = Math.floor(r2) : "bool" === this.typeInfo.name && (r2 = r2 ? 1 : 0), this.data[0] = r2;
    }
    getSubData(e2, t2, n2) {
      return t2 ? (console.error("getSubData: Scalar data does not support postfix", t2), null) : this;
    }
    toString() {
      return `${this.value}`;
    }
  };
  function Oe(e2, t2, n2) {
    const s2 = t2.length;
    return 2 === s2 ? "f32" === n2 ? new Be(new Float32Array(t2), e2.getTypeInfo("vec2f")) : "i32" === n2 || "bool" === n2 ? new Be(new Int32Array(t2), e2.getTypeInfo("vec2i")) : "u32" === n2 ? new Be(new Uint32Array(t2), e2.getTypeInfo("vec2u")) : "f16" === n2 ? new Be(new Float32Array(t2), e2.getTypeInfo("vec2h")) : (console.error(`getSubData: Unknown format ${n2}`), null) : 3 === s2 ? "f32" === n2 ? new Be(new Float32Array(t2), e2.getTypeInfo("vec3f")) : "i32" === n2 || "bool" === n2 ? new Be(new Int32Array(t2), e2.getTypeInfo("vec3i")) : "u32" === n2 ? new Be(new Uint32Array(t2), e2.getTypeInfo("vec3u")) : "f16" === n2 ? new Be(new Float32Array(t2), e2.getTypeInfo("vec3h")) : (console.error(`getSubData: Unknown format ${n2}`), null) : 4 === s2 ? "f32" === n2 ? new Be(new Float32Array(t2), e2.getTypeInfo("vec4f")) : "i32" === n2 || "bool" === n2 ? new Be(new Int32Array(t2), e2.getTypeInfo("vec4i")) : "u32" === n2 ? new Be(new Uint32Array(t2), e2.getTypeInfo("vec4u")) : "f16" === n2 ? new Be(new Float32Array(t2), e2.getTypeInfo("vec4h")) : (console.error(`getSubData: Unknown format ${n2}`), null) : (console.error(`getSubData: Invalid vector size ${t2.length}`), null);
  }
  var Be = class extends Ce {
    constructor(e2, t2, n2 = null) {
      if (super(t2, n2), e2 instanceof Float32Array || e2 instanceof Uint32Array || e2 instanceof Int32Array)
        this.data = e2;
      else {
        const t3 = this.typeInfo.name;
        "vec2f" === t3 || "vec3f" === t3 || "vec4f" === t3 ? this.data = new Float32Array(e2) : "vec2i" === t3 || "vec3i" === t3 || "vec4i" === t3 ? this.data = new Int32Array(e2) : "vec2u" === t3 || "vec3u" === t3 || "vec4u" === t3 ? this.data = new Uint32Array(e2) : "vec2h" === t3 || "vec3h" === t3 || "vec4h" === t3 ? this.data = new Float32Array(e2) : "vec2b" === t3 || "vec3b" === t3 || "vec4b" === t3 ? this.data = new Int32Array(e2) : "vec2" === t3 || "vec3" === t3 || "vec4" === t3 ? this.data = new Float32Array(e2) : console.error(`VectorData: Invalid type ${t3}`);
      }
    }
    clone() {
      if (this.data instanceof Float32Array)
        return new Be(new Float32Array(this.data), this.typeInfo, null);
      if (this.data instanceof Int32Array)
        return new Be(new Int32Array(this.data), this.typeInfo, null);
      if (this.data instanceof Uint32Array)
        return new Be(new Uint32Array(this.data), this.typeInfo, null);
      throw "VectorData: Invalid data type";
    }
    setDataValue(e2, t2, n2, s2) {
      n2 instanceof he ? console.error("TODO: Set vector postfix") : t2 instanceof Be ? this.data = t2.data : console.error("SetDataValue: Invalid value", t2);
    }
    getSubData(e2, t2, n2) {
      if (null === t2)
        return this;
      let s2 = e2.getTypeInfo("f32");
      if (this.typeInfo instanceof r)
        s2 = this.typeInfo.format || s2;
      else {
        const t3 = this.typeInfo.name;
        "vec2f" === t3 || "vec3f" === t3 || "vec4f" === t3 ? s2 = e2.getTypeInfo("f32") : "vec2i" === t3 || "vec3i" === t3 || "vec4i" === t3 ? s2 = e2.getTypeInfo("i32") : "vec2b" === t3 || "vec3b" === t3 || "vec4b" === t3 ? s2 = e2.getTypeInfo("bool") : "vec2u" === t3 || "vec3u" === t3 || "vec4u" === t3 ? s2 = e2.getTypeInfo("u32") : "vec2h" === t3 || "vec3h" === t3 || "vec4h" === t3 ? s2 = e2.getTypeInfo("f16") : console.error(`GetSubData: Unknown type ${t3}`);
      }
      let a2 = this;
      for (; null !== t2 && null !== a2; ) {
        if (t2 instanceof ye) {
          const r2 = t2.index;
          let i2 = -1;
          if (r2 instanceof _e) {
            if (!(r2.value instanceof Ve))
              return console.error(`GetSubData: Invalid array index ${r2.value}`), null;
            i2 = r2.value.value;
          } else {
            const t3 = e2.evalExpression(r2, n2);
            if (!(t3 instanceof Ve))
              return console.error("GetSubData: Unknown index type", r2), null;
            i2 = t3.value;
          }
          if (i2 < 0 || i2 >= a2.data.length)
            return console.error("GetSubData: Index out of range", i2), null;
          if (a2.data instanceof Float32Array) {
            const e3 = new Float32Array(a2.data.buffer, a2.data.byteOffset + 4 * i2, 1);
            return new Ve(e3, s2);
          }
          if (a2.data instanceof Int32Array) {
            const e3 = new Int32Array(a2.data.buffer, a2.data.byteOffset + 4 * i2, 1);
            return new Ve(e3, s2);
          }
          if (a2.data instanceof Uint32Array) {
            const e3 = new Uint32Array(a2.data.buffer, a2.data.byteOffset + 4 * i2, 1);
            return new Ve(e3, s2);
          }
          throw "GetSubData: Invalid data type";
        }
        if (!(t2 instanceof he))
          return console.error("GetSubData: Unknown postfix", t2), null;
        {
          const n3 = t2.value.toLowerCase();
          if (1 === n3.length) {
            let e3 = 0;
            if ("x" === n3 || "r" === n3)
              e3 = 0;
            else if ("y" === n3 || "g" === n3)
              e3 = 1;
            else if ("z" === n3 || "b" === n3)
              e3 = 2;
            else {
              if ("w" !== n3 && "a" !== n3)
                return console.error(`GetSubData: Unknown member ${n3}`), null;
              e3 = 3;
            }
            if (this.data instanceof Float32Array) {
              let t3 = new Float32Array(this.data.buffer, this.data.byteOffset + 4 * e3, 1);
              return new Ve(t3, s2, this);
            }
            if (this.data instanceof Int32Array) {
              let t3 = new Int32Array(this.data.buffer, this.data.byteOffset + 4 * e3, 1);
              return new Ve(t3, s2, this);
            }
            if (this.data instanceof Uint32Array) {
              let t3 = new Uint32Array(this.data.buffer, this.data.byteOffset + 4 * e3, 1);
              return new Ve(t3, s2, this);
            }
          }
          const r2 = [];
          for (const e3 of n3)
            "x" === e3 || "r" === e3 ? r2.push(this.data[0]) : "y" === e3 || "g" === e3 ? r2.push(this.data[1]) : "z" === e3 || "b" === e3 ? r2.push(this.data[2]) : "w" === e3 || "a" === e3 ? r2.push(this.data[3]) : console.error(`GetDataValue: Unknown member ${e3}`);
          a2 = Oe(e2, r2, s2.name);
        }
        t2 = t2.postfix;
      }
      return a2;
    }
    toString() {
      let e2 = `${this.data[0]}`;
      for (let t2 = 1; t2 < this.data.length; ++t2)
        e2 += `, ${this.data[t2]}`;
      return e2;
    }
  };
  var Fe = class extends Ce {
    constructor(e2, t2, n2 = null) {
      super(t2, n2), e2 instanceof Float32Array ? this.data = e2 : this.data = new Float32Array(e2);
    }
    clone() {
      return new Fe(new Float32Array(this.data), this.typeInfo, null);
    }
    setDataValue(e2, t2, n2, s2) {
      n2 instanceof he ? console.error("TODO: Set matrix postfix") : t2 instanceof Fe ? this.data = t2.data : console.error("SetDataValue: Invalid value", t2);
    }
    getSubData(e2, t2, n2) {
      if (null === t2)
        return this;
      const s2 = this.typeInfo.name;
      if (e2.getTypeInfo("f32"), this.typeInfo instanceof r)
        this.typeInfo.format;
      else if (s2.endsWith("f"))
        e2.getTypeInfo("f32");
      else if (s2.endsWith("i"))
        e2.getTypeInfo("i32");
      else if (s2.endsWith("u"))
        e2.getTypeInfo("u32");
      else {
        if (!s2.endsWith("h"))
          return console.error(`GetDataValue: Unknown type ${s2}`), null;
        e2.getTypeInfo("f16");
      }
      if (t2 instanceof ye) {
        const r2 = t2.index;
        let a2 = -1;
        if (r2 instanceof _e) {
          if (!(r2.value instanceof Ve))
            return console.error(`GetDataValue: Invalid array index ${r2.value}`), null;
          a2 = r2.value.value;
        } else {
          const t3 = e2.evalExpression(r2, n2);
          if (!(t3 instanceof Ve))
            return console.error("GetDataValue: Unknown index type", r2), null;
          a2 = t3.value;
        }
        if (a2 < 0 || a2 >= this.data.length)
          return console.error("GetDataValue: Index out of range", a2), null;
        const i2 = s2.endsWith("h") ? "h" : "f";
        let o2;
        if ("mat2x2" === s2 || "mat2x2f" === s2 || "mat2x2h" === s2 || "mat3x2" === s2 || "mat3x2f" === s2 || "mat3x2h" === s2 || "mat4x2" === s2 || "mat4x2f" === s2 || "mat4x2h" === s2)
          o2 = new Be(new Float32Array(this.data.buffer, this.data.byteOffset + 2 * a2 * 4, 2), e2.getTypeInfo(`vec2${i2}`));
        else if ("mat2x3" === s2 || "mat2x3f" === s2 || "mat2x3h" === s2 || "mat3x3" === s2 || "mat3x3f" === s2 || "mat3x3h" === s2 || "mat4x3" === s2 || "mat4x3f" === s2 || "mat4x3h" === s2)
          o2 = new Be(new Float32Array(this.data.buffer, this.data.byteOffset + 3 * a2 * 4, 3), e2.getTypeInfo(`vec3${i2}`));
        else {
          if ("mat2x4" !== s2 && "mat2x4f" !== s2 && "mat2x4h" !== s2 && "mat3x4" !== s2 && "mat3x4f" !== s2 && "mat3x4h" !== s2 && "mat4x4" !== s2 && "mat4x4f" !== s2 && "mat4x4h" !== s2)
            return console.error(`GetDataValue: Unknown type ${s2}`), null;
          o2 = new Be(new Float32Array(this.data.buffer, this.data.byteOffset + 4 * a2 * 4, 4), e2.getTypeInfo(`vec4${i2}`));
        }
        return t2.postfix ? o2.getSubData(e2, t2.postfix, n2) : o2;
      }
      return console.error("GetDataValue: Invalid postfix", t2), null;
    }
    toString() {
      let e2 = `${this.data[0]}`;
      for (let t2 = 1; t2 < this.data.length; ++t2)
        e2 += `, ${this.data[t2]}`;
      return e2;
    }
  };
  var Me = class extends Ce {
    constructor(e2, t2, n2 = 0, s2 = null) {
      super(t2, s2), this.buffer = e2 instanceof ArrayBuffer ? e2 : e2.buffer, this.offset = n2;
    }
    clone() {
      const e2 = new Uint8Array(new Uint8Array(this.buffer, this.offset, this.typeInfo.size));
      return new Me(e2.buffer, this.typeInfo, 0, null);
    }
    setDataValue(t2, r2, a2, i2) {
      if (null === r2)
        return void console.log("setDataValue: NULL data.");
      let o2 = this.offset, l2 = this.typeInfo;
      for (; a2; ) {
        if (a2 instanceof ye)
          if (l2 instanceof s) {
            const e2 = a2.index;
            if (e2 instanceof _e) {
              if (!(e2.value instanceof Ve))
                return void console.error(`SetDataValue: Invalid index type ${e2.value}`);
              o2 += e2.value.value * l2.stride;
            } else {
              const n2 = t2.evalExpression(e2, i2);
              if (!(n2 instanceof Ve))
                return void console.error("SetDataValue: Unknown index type", e2);
              o2 += n2.value * l2.stride;
            }
            l2 = l2.format;
          } else
            console.error(`SetDataValue: Type ${l2.getTypeName()} is not an array`);
        else {
          if (!(a2 instanceof he))
            return void console.error("SetDataValue: Unknown postfix type", a2);
          {
            const t3 = a2.value;
            if (l2 instanceof n) {
              let e2 = false;
              for (const n2 of l2.members)
                if (n2.name === t3) {
                  o2 += n2.offset, l2 = n2.type, e2 = true;
                  break;
                }
              if (!e2)
                return void console.error(`SetDataValue: Member ${t3} not found`);
            } else if (l2 instanceof e) {
              const e2 = l2.getTypeName();
              let n2 = 0;
              if ("x" === t3 || "r" === t3)
                n2 = 0;
              else if ("y" === t3 || "g" === t3)
                n2 = 1;
              else if ("z" === t3 || "b" === t3)
                n2 = 2;
              else {
                if ("w" !== t3 && "a" !== t3)
                  return void console.error(`SetDataValue: Unknown member ${t3}`);
                n2 = 3;
              }
              if (!(r2 instanceof Ve))
                return void console.error("SetDataValue: Invalid value", r2);
              const s2 = r2.value;
              return "vec2f" === e2 ? void (new Float32Array(this.buffer, o2, 2)[n2] = s2) : "vec3f" === e2 ? void (new Float32Array(this.buffer, o2, 3)[n2] = s2) : "vec4f" === e2 ? void (new Float32Array(this.buffer, o2, 4)[n2] = s2) : "vec2i" === e2 ? void (new Int32Array(this.buffer, o2, 2)[n2] = s2) : "vec3i" === e2 ? void (new Int32Array(this.buffer, o2, 3)[n2] = s2) : "vec4i" === e2 ? void (new Int32Array(this.buffer, o2, 4)[n2] = s2) : "vec2u" === e2 ? void (new Uint32Array(this.buffer, o2, 2)[n2] = s2) : "vec3u" === e2 ? void (new Uint32Array(this.buffer, o2, 3)[n2] = s2) : "vec4u" === e2 ? void (new Uint32Array(this.buffer, o2, 4)[n2] = s2) : void console.error(`SetDataValue: Type ${e2} is not a struct`);
            }
          }
        }
        a2 = a2.postfix;
      }
      this.setData(t2, r2, l2, o2, i2);
    }
    setData(e2, t2, n2, s2, r2) {
      const a2 = n2.getTypeName();
      if ("f32" !== a2 && "f16" !== a2)
        if ("i32" !== a2 && "atomic<i32>" !== a2 && "x32" !== a2)
          if ("u32" !== a2 && "atomic<u32>" !== a2)
            if ("bool" !== a2)
              if ("vec2f" !== a2 && "vec2h" !== a2)
                if ("vec3f" !== a2 && "vec3h" !== a2)
                  if ("vec4f" !== a2 && "vec4h" !== a2)
                    if ("vec2i" !== a2)
                      if ("vec3i" !== a2)
                        if ("vec4i" !== a2)
                          if ("vec2u" !== a2)
                            if ("vec3u" !== a2)
                              if ("vec4u" !== a2)
                                if ("vec2b" !== a2)
                                  if ("vec3b" !== a2)
                                    if ("vec4b" !== a2)
                                      if ("mat2x2f" !== a2 && "mat2x2h" !== a2)
                                        if ("mat2x3f" !== a2 && "mat2x3h" !== a2)
                                          if ("mat2x4f" !== a2 && "mat2x4h" !== a2)
                                            if ("mat3x2f" !== a2 && "mat3x2h" !== a2)
                                              if ("mat3x3f" !== a2 && "mat3x3h" !== a2)
                                                if ("mat3x4f" !== a2 && "mat3x4h" !== a2)
                                                  if ("mat4x2f" !== a2 && "mat4x2h" !== a2)
                                                    if ("mat4x3f" !== a2 && "mat4x3h" !== a2)
                                                      if ("mat4x4f" !== a2 && "mat4x4h" !== a2)
                                                        if (t2 instanceof Me) {
                                                          if (n2 === t2.typeInfo) {
                                                            return void new Uint8Array(this.buffer, s2, t2.buffer.byteLength).set(new Uint8Array(t2.buffer));
                                                          }
                                                          console.error("SetDataValue: Type mismatch", a2, t2.typeInfo.getTypeName());
                                                        } else
                                                          console.error(`SetData: Unknown type ${a2}`);
                                                      else {
                                                        const e3 = new Float32Array(this.buffer, s2, 16);
                                                        t2 instanceof Fe ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3], e3[4] = t2.data[4], e3[5] = t2.data[5], e3[6] = t2.data[6], e3[7] = t2.data[7], e3[8] = t2.data[8], e3[9] = t2.data[9], e3[10] = t2.data[10], e3[11] = t2.data[11], e3[12] = t2.data[12], e3[13] = t2.data[13], e3[14] = t2.data[14], e3[15] = t2.data[15]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3], e3[4] = t2[4], e3[5] = t2[5], e3[6] = t2[6], e3[7] = t2[7], e3[8] = t2[8], e3[9] = t2[9], e3[10] = t2[10], e3[11] = t2[11], e3[12] = t2[12], e3[13] = t2[13], e3[14] = t2[14], e3[15] = t2[15]);
                                                      }
                                                    else {
                                                      const e3 = new Float32Array(this.buffer, s2, 12);
                                                      t2 instanceof Fe ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3], e3[4] = t2.data[4], e3[5] = t2.data[5], e3[6] = t2.data[6], e3[7] = t2.data[7], e3[8] = t2.data[8], e3[9] = t2.data[9], e3[10] = t2.data[10], e3[11] = t2.data[11]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3], e3[4] = t2[4], e3[5] = t2[5], e3[6] = t2[6], e3[7] = t2[7], e3[8] = t2[8], e3[9] = t2[9], e3[10] = t2[10], e3[11] = t2[11]);
                                                    }
                                                  else {
                                                    const e3 = new Float32Array(this.buffer, s2, 8);
                                                    t2 instanceof Fe ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3], e3[4] = t2.data[4], e3[5] = t2.data[5], e3[6] = t2.data[6], e3[7] = t2.data[7]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3], e3[4] = t2[4], e3[5] = t2[5], e3[6] = t2[6], e3[7] = t2[7]);
                                                  }
                                                else {
                                                  const e3 = new Float32Array(this.buffer, s2, 12);
                                                  t2 instanceof Fe ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3], e3[4] = t2.data[4], e3[5] = t2.data[5], e3[6] = t2.data[6], e3[7] = t2.data[7], e3[8] = t2.data[8], e3[9] = t2.data[9], e3[10] = t2.data[10], e3[11] = t2.data[11]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3], e3[4] = t2[4], e3[5] = t2[5], e3[6] = t2[6], e3[7] = t2[7], e3[8] = t2[8], e3[9] = t2[9], e3[10] = t2[10], e3[11] = t2[11]);
                                                }
                                              else {
                                                const e3 = new Float32Array(this.buffer, s2, 9);
                                                t2 instanceof Fe ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3], e3[4] = t2.data[4], e3[5] = t2.data[5], e3[6] = t2.data[6], e3[7] = t2.data[7], e3[8] = t2.data[8]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3], e3[4] = t2[4], e3[5] = t2[5], e3[6] = t2[6], e3[7] = t2[7], e3[8] = t2[8]);
                                              }
                                            else {
                                              const e3 = new Float32Array(this.buffer, s2, 6);
                                              t2 instanceof Fe ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3], e3[4] = t2.data[4], e3[5] = t2.data[5]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3], e3[4] = t2[4], e3[5] = t2[5]);
                                            }
                                          else {
                                            const e3 = new Float32Array(this.buffer, s2, 8);
                                            t2 instanceof Fe ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3], e3[4] = t2.data[4], e3[5] = t2.data[5], e3[6] = t2.data[6], e3[7] = t2.data[7]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3], e3[4] = t2[4], e3[5] = t2[5], e3[6] = t2[6], e3[7] = t2[7]);
                                          }
                                        else {
                                          const e3 = new Float32Array(this.buffer, s2, 6);
                                          t2 instanceof Fe ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3], e3[4] = t2.data[4], e3[5] = t2.data[5]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3], e3[4] = t2[4], e3[5] = t2[5]);
                                        }
                                      else {
                                        const e3 = new Float32Array(this.buffer, s2, 4);
                                        t2 instanceof Fe ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3]);
                                      }
                                    else {
                                      const e3 = new Uint32Array(this.buffer, s2, 4);
                                      t2 instanceof Be ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3]);
                                    }
                                  else {
                                    const e3 = new Uint32Array(this.buffer, s2, 3);
                                    t2 instanceof Be ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2]);
                                  }
                                else {
                                  const e3 = new Uint32Array(this.buffer, s2, 2);
                                  t2 instanceof Be ? (e3[0] = t2.data[0], e3[1] = t2.data[1]) : (e3[0] = t2[0], e3[1] = t2[1]);
                                }
                              else {
                                const e3 = new Uint32Array(this.buffer, s2, 4);
                                t2 instanceof Be ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3]);
                              }
                            else {
                              const e3 = new Uint32Array(this.buffer, s2, 3);
                              t2 instanceof Be ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2]);
                            }
                          else {
                            const e3 = new Uint32Array(this.buffer, s2, 2);
                            t2 instanceof Be ? (e3[0] = t2.data[0], e3[1] = t2.data[1]) : (e3[0] = t2[0], e3[1] = t2[1]);
                          }
                        else {
                          const e3 = new Int32Array(this.buffer, s2, 4);
                          t2 instanceof Be ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3]);
                        }
                      else {
                        const e3 = new Int32Array(this.buffer, s2, 3);
                        t2 instanceof Be ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2]);
                      }
                    else {
                      const e3 = new Int32Array(this.buffer, s2, 2);
                      t2 instanceof Be ? (e3[0] = t2.data[0], e3[1] = t2.data[1]) : (e3[0] = t2[0], e3[1] = t2[1]);
                    }
                  else {
                    const e3 = new Float32Array(this.buffer, s2, 4);
                    t2 instanceof Be ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2], e3[3] = t2.data[3]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2], e3[3] = t2[3]);
                  }
                else {
                  const e3 = new Float32Array(this.buffer, s2, 3);
                  t2 instanceof Be ? (e3[0] = t2.data[0], e3[1] = t2.data[1], e3[2] = t2.data[2]) : (e3[0] = t2[0], e3[1] = t2[1], e3[2] = t2[2]);
                }
              else {
                const e3 = new Float32Array(this.buffer, s2, 2);
                t2 instanceof Be ? (e3[0] = t2.data[0], e3[1] = t2.data[1]) : (e3[0] = t2[0], e3[1] = t2[1]);
              }
            else
              t2 instanceof Ve && (new Int32Array(this.buffer, s2, 1)[0] = t2.value);
          else
            t2 instanceof Ve && (new Uint32Array(this.buffer, s2, 1)[0] = t2.value);
        else
          t2 instanceof Ve && (new Int32Array(this.buffer, s2, 1)[0] = t2.value);
      else
        t2 instanceof Ve && (new Float32Array(this.buffer, s2, 1)[0] = t2.value);
    }
    getSubData(t2, a2, i2) {
      var o2, l2, c2;
      if (null === a2)
        return this;
      let u2 = this.offset, h2 = this.typeInfo;
      for (; a2; ) {
        if (a2 instanceof ye) {
          const e2 = a2.index, n2 = t2.evalExpression(e2, i2);
          let r2 = 0;
          if (n2 instanceof Ve ? r2 = n2.value : console.error("GetDataValue: Invalid index type", e2), h2 instanceof s)
            u2 += r2 * h2.stride, h2 = h2.format;
          else {
            const e3 = h2.getTypeName();
            "mat4x4" === e3 || "mat4x4f" === e3 || "mat4x4h" === e3 ? (u2 += 16 * r2, h2 = t2.getTypeInfo("vec4f")) : console.error(`getDataValue: Type ${h2.getTypeName()} is not an array`);
          }
        } else {
          if (!(a2 instanceof he))
            return console.error("GetDataValue: Unknown postfix type", a2), null;
          {
            const s2 = a2.value;
            if (h2 instanceof n) {
              let e2 = false;
              for (const t3 of h2.members)
                if (t3.name === s2) {
                  u2 += t3.offset, h2 = t3.type, e2 = true;
                  break;
                }
              if (!e2)
                return console.error(`GetDataValue: Member ${s2} not found`), null;
            } else if (h2 instanceof e) {
              const e2 = h2.getTypeName();
              if ("vec2f" === e2 || "vec3f" === e2 || "vec4f" === e2 || "vec2i" === e2 || "vec3i" === e2 || "vec4i" === e2 || "vec2u" === e2 || "vec3u" === e2 || "vec4u" === e2 || "vec2b" === e2 || "vec3b" === e2 || "vec4b" === e2 || "vec2h" === e2 || "vec3h" === e2 || "vec4h" === e2 || "vec2" === e2 || "vec3" === e2 || "vec4" === e2) {
                if (s2.length > 0 && s2.length < 5) {
                  let n2 = "f";
                  const r2 = [];
                  for (let a3 = 0; a3 < s2.length; ++a3) {
                    const i3 = s2[a3].toLowerCase();
                    let o3 = 0;
                    if ("x" === i3 || "r" === i3)
                      o3 = 0;
                    else if ("y" === i3 || "g" === i3)
                      o3 = 1;
                    else if ("z" === i3 || "b" === i3)
                      o3 = 2;
                    else {
                      if ("w" !== i3 && "a" !== i3)
                        return console.error(`Unknown member ${s2}`), null;
                      o3 = 3;
                    }
                    if (1 === s2.length) {
                      if (e2.endsWith("f"))
                        return this.buffer.byteLength < u2 + 4 * o3 + 4 ? (console.log("Insufficient buffer data"), null) : new Ve(new Float32Array(this.buffer, u2 + 4 * o3, 1), t2.getTypeInfo("f32"), this);
                      if (e2.endsWith("h"))
                        return new Ve(new Float32Array(this.buffer, u2 + 4 * o3, 1), t2.getTypeInfo("f16"), this);
                      if (e2.endsWith("i"))
                        return new Ve(new Int32Array(this.buffer, u2 + 4 * o3, 1), t2.getTypeInfo("i32"), this);
                      if (e2.endsWith("b"))
                        return new Ve(new Int32Array(this.buffer, u2 + 4 * o3, 1), t2.getTypeInfo("bool"), this);
                      if (e2.endsWith("u"))
                        return new Ve(new Uint32Array(this.buffer, u2 + 4 * o3, 1), t2.getTypeInfo("i32"), this);
                    }
                    if ("vec2f" === e2)
                      r2.push(new Float32Array(this.buffer, u2, 2)[o3]);
                    else if ("vec3f" === e2) {
                      if (u2 + 12 >= this.buffer.byteLength)
                        return console.log("Insufficient buffer data"), null;
                      const e3 = new Float32Array(this.buffer, u2, 3);
                      r2.push(e3[o3]);
                    } else if ("vec4f" === e2)
                      r2.push(new Float32Array(this.buffer, u2, 4)[o3]);
                    else if ("vec2i" === e2)
                      n2 = "i", r2.push(new Int32Array(this.buffer, u2, 2)[o3]);
                    else if ("vec3i" === e2)
                      n2 = "i", r2.push(new Int32Array(this.buffer, u2, 3)[o3]);
                    else if ("vec4i" === e2)
                      n2 = "i", r2.push(new Int32Array(this.buffer, u2, 4)[o3]);
                    else if ("vec2u" === e2) {
                      n2 = "u";
                      const e3 = new Uint32Array(this.buffer, u2, 2);
                      r2.push(e3[o3]);
                    } else
                      "vec3u" === e2 ? (n2 = "u", r2.push(new Uint32Array(this.buffer, u2, 3)[o3])) : "vec4u" === e2 && (n2 = "u", r2.push(new Uint32Array(this.buffer, u2, 4)[o3]));
                  }
                  return 2 === r2.length ? h2 = t2.getTypeInfo(`vec2${n2}`) : 3 === r2.length ? h2 = t2.getTypeInfo(`vec3${n2}`) : 4 === r2.length ? h2 = t2.getTypeInfo(`vec4${n2}`) : console.error(`GetDataValue: Invalid vector length ${r2.length}`), new Be(r2, h2, null);
                }
                return console.error(`GetDataValue: Unknown member ${s2}`), null;
              }
              return console.error(`GetDataValue: Type ${e2} is not a struct`), null;
            }
          }
        }
        a2 = a2.postfix;
      }
      const f2 = h2.getTypeName();
      return "f32" === f2 ? new Ve(new Float32Array(this.buffer, u2, 1), h2, this) : "i32" === f2 ? new Ve(new Int32Array(this.buffer, u2, 1), h2, this) : "u32" === f2 ? new Ve(new Uint32Array(this.buffer, u2, 1), h2, this) : "vec2f" === f2 ? new Be(new Float32Array(this.buffer, u2, 2), h2, this) : "vec3f" === f2 ? new Be(new Float32Array(this.buffer, u2, 3), h2, this) : "vec4f" === f2 ? new Be(new Float32Array(this.buffer, u2, 4), h2, this) : "vec2i" === f2 ? new Be(new Int32Array(this.buffer, u2, 2), h2, this) : "vec3i" === f2 ? new Be(new Int32Array(this.buffer, u2, 3), h2, this) : "vec4i" === f2 ? new Be(new Int32Array(this.buffer, u2, 4), h2, this) : "vec2u" === f2 ? new Be(new Uint32Array(this.buffer, u2, 2), h2, this) : "vec3u" === f2 ? new Be(new Uint32Array(this.buffer, u2, 3), h2, this) : "vec4u" === f2 ? new Be(new Uint32Array(this.buffer, u2, 4), h2, this) : h2 instanceof r && "atomic" === h2.name ? "u32" === (null === (o2 = h2.format) || void 0 === o2 ? void 0 : o2.name) ? new Ve(new Uint32Array(this.buffer, u2, 1)[0], h2.format, this) : "i32" === (null === (l2 = h2.format) || void 0 === l2 ? void 0 : l2.name) ? new Ve(new Int32Array(this.buffer, u2, 1)[0], h2.format, this) : (console.error(`GetDataValue: Invalid atomic format ${null === (c2 = h2.format) || void 0 === c2 ? void 0 : c2.name}`), null) : new Me(this.buffer, h2, u2, this);
    }
    toString() {
      let e2 = "";
      if (this.typeInfo instanceof s)
        if ("f32" === this.typeInfo.format.name) {
          const t2 = new Float32Array(this.buffer, this.offset);
          e2 = `[${t2[0]}`;
          for (let n2 = 1; n2 < t2.length; ++n2)
            e2 += `, ${t2[n2]}`;
        } else if ("i32" === this.typeInfo.format.name) {
          const t2 = new Int32Array(this.buffer, this.offset);
          e2 = `[${t2[0]}`;
          for (let n2 = 1; n2 < t2.length; ++n2)
            e2 += `, ${t2[n2]}`;
        } else if ("u32" === this.typeInfo.format.name) {
          const t2 = new Uint32Array(this.buffer, this.offset);
          e2 = `[${t2[0]}`;
          for (let n2 = 1; n2 < t2.length; ++n2)
            e2 += `, ${t2[n2]}`;
        } else if ("vec2f" === this.typeInfo.format.name) {
          const t2 = new Float32Array(this.buffer, this.offset);
          e2 = `[${t2[0]}, ${t2[1]}]`;
          for (let n2 = 1; n2 < t2.length / 2; ++n2)
            e2 += `, [${t2[2 * n2]}, ${t2[2 * n2 + 1]}]`;
        } else if ("vec3f" === this.typeInfo.format.name) {
          const t2 = new Float32Array(this.buffer, this.offset);
          e2 = `[${t2[0]}, ${t2[1]}, ${t2[2]}]`;
          for (let n2 = 4; n2 < t2.length; n2 += 4)
            e2 += `, [${t2[n2]}, ${t2[n2 + 1]}, ${t2[n2 + 2]}]`;
        } else if ("vec4f" === this.typeInfo.format.name) {
          const t2 = new Float32Array(this.buffer, this.offset);
          e2 = `[${t2[0]}, ${t2[1]}, ${t2[2]}, ${t2[3]}]`;
          for (let n2 = 4; n2 < t2.length; n2 += 4)
            e2 += `, [${t2[n2]}, ${t2[n2 + 1]}, ${t2[n2 + 2]}, ${t2[n2 + 3]}]`;
        } else
          e2 = "[...]";
      else
        this.typeInfo instanceof n ? e2 += "{...}" : e2 = "[...]";
      return e2;
    }
  };
  var Ue = class extends Ce {
    constructor(e2, t2, n2, s2) {
      super(t2, null), this.data = e2, this.descriptor = n2, this.view = s2;
    }
    clone() {
      return new Ue(this.data, this.typeInfo, this.descriptor, this.view);
    }
    get width() {
      var e2, t2;
      const n2 = this.descriptor.size;
      return n2 instanceof Array && n2.length > 0 ? null !== (e2 = n2[0]) && void 0 !== e2 ? e2 : 0 : n2 instanceof Object && null !== (t2 = n2.width) && void 0 !== t2 ? t2 : 0;
    }
    get height() {
      var e2, t2;
      const n2 = this.descriptor.size;
      return n2 instanceof Array && n2.length > 1 ? null !== (e2 = n2[1]) && void 0 !== e2 ? e2 : 0 : n2 instanceof Object && null !== (t2 = n2.height) && void 0 !== t2 ? t2 : 0;
    }
    get depthOrArrayLayers() {
      var e2, t2;
      const n2 = this.descriptor.size;
      return n2 instanceof Array && n2.length > 2 ? null !== (e2 = n2[2]) && void 0 !== e2 ? e2 : 0 : n2 instanceof Object && null !== (t2 = n2.depthOrArrayLayers) && void 0 !== t2 ? t2 : 0;
    }
    get format() {
      var e2;
      return this.descriptor && null !== (e2 = this.descriptor.format) && void 0 !== e2 ? e2 : "rgba8unorm";
    }
    get sampleCount() {
      var e2;
      return this.descriptor && null !== (e2 = this.descriptor.sampleCount) && void 0 !== e2 ? e2 : 1;
    }
    get mipLevelCount() {
      var e2;
      return this.descriptor && null !== (e2 = this.descriptor.mipLevelCount) && void 0 !== e2 ? e2 : 1;
    }
    get dimension() {
      var e2;
      return this.descriptor && null !== (e2 = this.descriptor.dimension) && void 0 !== e2 ? e2 : "2d";
    }
    getMipLevelSize(e2) {
      if (e2 >= this.mipLevelCount)
        return [0, 0, 0];
      const t2 = [this.width, this.height, this.depthOrArrayLayers];
      for (let n2 = 0; n2 < t2.length; ++n2)
        t2[n2] = Math.max(1, t2[n2] >> e2);
      return t2;
    }
    get texelByteSize() {
      const e2 = this.format, t2 = I[e2];
      return t2 ? t2.isDepthStencil ? 4 : t2.bytesPerBlock : 0;
    }
    get bytesPerRow() {
      return this.width * this.texelByteSize;
    }
    get isDepthStencil() {
      const e2 = this.format, t2 = I[e2];
      return !!t2 && t2.isDepthStencil;
    }
    getGpuSize() {
      const e2 = this.format, t2 = I[e2], n2 = this.width;
      if (!e2 || n2 <= 0 || !t2)
        return -1;
      const s2 = this.height, r2 = this.depthOrArrayLayers, a2 = this.dimension;
      return n2 / t2.blockWidth * ("1d" === a2 ? 1 : s2 / t2.blockHeight) * t2.bytesPerBlock * r2;
    }
    getPixel(e2, t2, n2 = 0, s2 = 0) {
      const r2 = this.texelByteSize, a2 = this.bytesPerRow, i2 = this.height, o2 = this.data[s2];
      return v(new Uint8Array(o2), e2, t2, n2, s2, i2, a2, r2, this.format);
    }
    setPixel(e2, t2, n2, s2, r2) {
      const a2 = this.texelByteSize, i2 = this.bytesPerRow, o2 = this.height, l2 = this.data[s2];
      !function(e3, t3, n3, s3, r3, a3, i3, o3, l3, c2) {
        const u2 = s3 * (i3 >>= r3) * (a3 >>= r3) + n3 * i3 + t3 * o3;
        switch (l3) {
          case "r8unorm":
            return void k(e3, u2, "8unorm", 1, c2);
          case "r8snorm":
            return void k(e3, u2, "8snorm", 1, c2);
          case "r8uint":
            return void k(e3, u2, "8uint", 1, c2);
          case "r8sint":
            return void k(e3, u2, "8sint", 1, c2);
          case "rg8unorm":
            return void k(e3, u2, "8unorm", 2, c2);
          case "rg8snorm":
            return void k(e3, u2, "8snorm", 2, c2);
          case "rg8uint":
            return void k(e3, u2, "8uint", 2, c2);
          case "rg8sint":
            return void k(e3, u2, "8sint", 2, c2);
          case "rgba8unorm-srgb":
          case "rgba8unorm":
          case "bgra8unorm-srgb":
          case "bgra8unorm":
            return void k(e3, u2, "8unorm", 4, c2);
          case "rgba8snorm":
            return void k(e3, u2, "8snorm", 4, c2);
          case "rgba8uint":
            return void k(e3, u2, "8uint", 4, c2);
          case "rgba8sint":
            return void k(e3, u2, "8sint", 4, c2);
          case "r16uint":
            return void k(e3, u2, "16uint", 1, c2);
          case "r16sint":
            return void k(e3, u2, "16sint", 1, c2);
          case "r16float":
            return void k(e3, u2, "16float", 1, c2);
          case "rg16uint":
            return void k(e3, u2, "16uint", 2, c2);
          case "rg16sint":
            return void k(e3, u2, "16sint", 2, c2);
          case "rg16float":
            return void k(e3, u2, "16float", 2, c2);
          case "rgba16uint":
            return void k(e3, u2, "16uint", 4, c2);
          case "rgba16sint":
            return void k(e3, u2, "16sint", 4, c2);
          case "rgba16float":
            return void k(e3, u2, "16float", 4, c2);
          case "r32uint":
            return void k(e3, u2, "32uint", 1, c2);
          case "r32sint":
            return void k(e3, u2, "32sint", 1, c2);
          case "depth16unorm":
          case "depth24plus":
          case "depth24plus-stencil8":
          case "depth32float":
          case "depth32float-stencil8":
          case "r32float":
            return void k(e3, u2, "32float", 1, c2);
          case "rg32uint":
            return void k(e3, u2, "32uint", 2, c2);
          case "rg32sint":
            return void k(e3, u2, "32sint", 2, c2);
          case "rg32float":
            return void k(e3, u2, "32float", 2, c2);
          case "rgba32uint":
            return void k(e3, u2, "32uint", 4, c2);
          case "rgba32sint":
            return void k(e3, u2, "32sint", 4, c2);
          case "rgba32float":
            return void k(e3, u2, "32float", 4, c2);
          case "rg11b10ufloat":
            console.error("TODO: rg11b10ufloat not supported for writing");
        }
      }(new Uint8Array(l2), e2, t2, n2, s2, o2, i2, a2, this.format, r2);
    }
  };
  ((e2) => {
    e2[e2.token = 0] = "token", e2[e2.keyword = 1] = "keyword", e2[e2.reserved = 2] = "reserved";
  })(q || (q = {}));
  var Pe = class {
    constructor(e2, t2, n2) {
      this.name = e2, this.type = t2, this.rule = n2;
    }
    toString() {
      return this.name;
    }
  };
  var We = class {
  };
  W = We, We.none = new Pe("", q.reserved, ""), We.eof = new Pe("EOF", q.token, ""), We.reserved = { asm: new Pe("asm", q.reserved, "asm"), bf16: new Pe("bf16", q.reserved, "bf16"), do: new Pe("do", q.reserved, "do"), enum: new Pe("enum", q.reserved, "enum"), f16: new Pe("f16", q.reserved, "f16"), f64: new Pe("f64", q.reserved, "f64"), handle: new Pe("handle", q.reserved, "handle"), i8: new Pe("i8", q.reserved, "i8"), i16: new Pe("i16", q.reserved, "i16"), i64: new Pe("i64", q.reserved, "i64"), mat: new Pe("mat", q.reserved, "mat"), premerge: new Pe("premerge", q.reserved, "premerge"), regardless: new Pe("regardless", q.reserved, "regardless"), typedef: new Pe("typedef", q.reserved, "typedef"), u8: new Pe("u8", q.reserved, "u8"), u16: new Pe("u16", q.reserved, "u16"), u64: new Pe("u64", q.reserved, "u64"), unless: new Pe("unless", q.reserved, "unless"), using: new Pe("using", q.reserved, "using"), vec: new Pe("vec", q.reserved, "vec"), void: new Pe("void", q.reserved, "void") }, We.keywords = { array: new Pe("array", q.keyword, "array"), atomic: new Pe("atomic", q.keyword, "atomic"), bool: new Pe("bool", q.keyword, "bool"), f32: new Pe("f32", q.keyword, "f32"), i32: new Pe("i32", q.keyword, "i32"), mat2x2: new Pe("mat2x2", q.keyword, "mat2x2"), mat2x3: new Pe("mat2x3", q.keyword, "mat2x3"), mat2x4: new Pe("mat2x4", q.keyword, "mat2x4"), mat3x2: new Pe("mat3x2", q.keyword, "mat3x2"), mat3x3: new Pe("mat3x3", q.keyword, "mat3x3"), mat3x4: new Pe("mat3x4", q.keyword, "mat3x4"), mat4x2: new Pe("mat4x2", q.keyword, "mat4x2"), mat4x3: new Pe("mat4x3", q.keyword, "mat4x3"), mat4x4: new Pe("mat4x4", q.keyword, "mat4x4"), ptr: new Pe("ptr", q.keyword, "ptr"), sampler: new Pe("sampler", q.keyword, "sampler"), sampler_comparison: new Pe("sampler_comparison", q.keyword, "sampler_comparison"), struct: new Pe("struct", q.keyword, "struct"), texture_1d: new Pe("texture_1d", q.keyword, "texture_1d"), texture_2d: new Pe("texture_2d", q.keyword, "texture_2d"), texture_2d_array: new Pe("texture_2d_array", q.keyword, "texture_2d_array"), texture_3d: new Pe("texture_3d", q.keyword, "texture_3d"), texture_cube: new Pe("texture_cube", q.keyword, "texture_cube"), texture_cube_array: new Pe("texture_cube_array", q.keyword, "texture_cube_array"), texture_multisampled_2d: new Pe("texture_multisampled_2d", q.keyword, "texture_multisampled_2d"), texture_storage_1d: new Pe("texture_storage_1d", q.keyword, "texture_storage_1d"), texture_storage_2d: new Pe("texture_storage_2d", q.keyword, "texture_storage_2d"), texture_storage_2d_array: new Pe("texture_storage_2d_array", q.keyword, "texture_storage_2d_array"), texture_storage_3d: new Pe("texture_storage_3d", q.keyword, "texture_storage_3d"), texture_depth_2d: new Pe("texture_depth_2d", q.keyword, "texture_depth_2d"), texture_depth_2d_array: new Pe("texture_depth_2d_array", q.keyword, "texture_depth_2d_array"), texture_depth_cube: new Pe("texture_depth_cube", q.keyword, "texture_depth_cube"), texture_depth_cube_array: new Pe("texture_depth_cube_array", q.keyword, "texture_depth_cube_array"), texture_depth_multisampled_2d: new Pe("texture_depth_multisampled_2d", q.keyword, "texture_depth_multisampled_2d"), texture_external: new Pe("texture_external", q.keyword, "texture_external"), u32: new Pe("u32", q.keyword, "u32"), vec2: new Pe("vec2", q.keyword, "vec2"), vec3: new Pe("vec3", q.keyword, "vec3"), vec4: new Pe("vec4", q.keyword, "vec4"), bitcast: new Pe("bitcast", q.keyword, "bitcast"), block: new Pe("block", q.keyword, "block"), break: new Pe("break", q.keyword, "break"), case: new Pe("case", q.keyword, "case"), continue: new Pe("continue", q.keyword, "continue"), continuing: new Pe("continuing", q.keyword, "continuing"), default: new Pe("default", q.keyword, "default"), diagnostic: new Pe("diagnostic", q.keyword, "diagnostic"), discard: new Pe("discard", q.keyword, "discard"), else: new Pe("else", q.keyword, "else"), enable: new Pe("enable", q.keyword, "enable"), fallthrough: new Pe("fallthrough", q.keyword, "fallthrough"), false: new Pe("false", q.keyword, "false"), fn: new Pe("fn", q.keyword, "fn"), for: new Pe("for", q.keyword, "for"), function: new Pe("function", q.keyword, "function"), if: new Pe("if", q.keyword, "if"), let: new Pe("let", q.keyword, "let"), const: new Pe("const", q.keyword, "const"), loop: new Pe("loop", q.keyword, "loop"), while: new Pe("while", q.keyword, "while"), private: new Pe("private", q.keyword, "private"), read: new Pe("read", q.keyword, "read"), read_write: new Pe("read_write", q.keyword, "read_write"), return: new Pe("return", q.keyword, "return"), requires: new Pe("requires", q.keyword, "requires"), storage: new Pe("storage", q.keyword, "storage"), switch: new Pe("switch", q.keyword, "switch"), true: new Pe("true", q.keyword, "true"), alias: new Pe("alias", q.keyword, "alias"), type: new Pe("type", q.keyword, "type"), uniform: new Pe("uniform", q.keyword, "uniform"), var: new Pe("var", q.keyword, "var"), override: new Pe("override", q.keyword, "override"), workgroup: new Pe("workgroup", q.keyword, "workgroup"), write: new Pe("write", q.keyword, "write"), r8unorm: new Pe("r8unorm", q.keyword, "r8unorm"), r8snorm: new Pe("r8snorm", q.keyword, "r8snorm"), r8uint: new Pe("r8uint", q.keyword, "r8uint"), r8sint: new Pe("r8sint", q.keyword, "r8sint"), r16uint: new Pe("r16uint", q.keyword, "r16uint"), r16sint: new Pe("r16sint", q.keyword, "r16sint"), r16float: new Pe("r16float", q.keyword, "r16float"), rg8unorm: new Pe("rg8unorm", q.keyword, "rg8unorm"), rg8snorm: new Pe("rg8snorm", q.keyword, "rg8snorm"), rg8uint: new Pe("rg8uint", q.keyword, "rg8uint"), rg8sint: new Pe("rg8sint", q.keyword, "rg8sint"), r32uint: new Pe("r32uint", q.keyword, "r32uint"), r32sint: new Pe("r32sint", q.keyword, "r32sint"), r32float: new Pe("r32float", q.keyword, "r32float"), rg16uint: new Pe("rg16uint", q.keyword, "rg16uint"), rg16sint: new Pe("rg16sint", q.keyword, "rg16sint"), rg16float: new Pe("rg16float", q.keyword, "rg16float"), rgba8unorm: new Pe("rgba8unorm", q.keyword, "rgba8unorm"), rgba8unorm_srgb: new Pe("rgba8unorm_srgb", q.keyword, "rgba8unorm_srgb"), rgba8snorm: new Pe("rgba8snorm", q.keyword, "rgba8snorm"), rgba8uint: new Pe("rgba8uint", q.keyword, "rgba8uint"), rgba8sint: new Pe("rgba8sint", q.keyword, "rgba8sint"), bgra8unorm: new Pe("bgra8unorm", q.keyword, "bgra8unorm"), bgra8unorm_srgb: new Pe("bgra8unorm_srgb", q.keyword, "bgra8unorm_srgb"), rgb10a2unorm: new Pe("rgb10a2unorm", q.keyword, "rgb10a2unorm"), rg11b10float: new Pe("rg11b10float", q.keyword, "rg11b10float"), rg32uint: new Pe("rg32uint", q.keyword, "rg32uint"), rg32sint: new Pe("rg32sint", q.keyword, "rg32sint"), rg32float: new Pe("rg32float", q.keyword, "rg32float"), rgba16uint: new Pe("rgba16uint", q.keyword, "rgba16uint"), rgba16sint: new Pe("rgba16sint", q.keyword, "rgba16sint"), rgba16float: new Pe("rgba16float", q.keyword, "rgba16float"), rgba32uint: new Pe("rgba32uint", q.keyword, "rgba32uint"), rgba32sint: new Pe("rgba32sint", q.keyword, "rgba32sint"), rgba32float: new Pe("rgba32float", q.keyword, "rgba32float"), static_assert: new Pe("static_assert", q.keyword, "static_assert") }, We.tokens = { decimal_float_literal: new Pe("decimal_float_literal", q.token, /((-?[0-9]*\.[0-9]+|-?[0-9]+\.[0-9]*)((e|E)(\+|-)?[0-9]+)?[fh]?)|(-?[0-9]+(e|E)(\+|-)?[0-9]+[fh]?)|(-?[0-9]+[fh])/), hex_float_literal: new Pe("hex_float_literal", q.token, /-?0x((([0-9a-fA-F]*\.[0-9a-fA-F]+|[0-9a-fA-F]+\.[0-9a-fA-F]*)((p|P)(\+|-)?[0-9]+[fh]?)?)|([0-9a-fA-F]+(p|P)(\+|-)?[0-9]+[fh]?))/), int_literal: new Pe("int_literal", q.token, /-?0x[0-9a-fA-F]+|0i?|-?[1-9][0-9]*i?/), uint_literal: new Pe("uint_literal", q.token, /0x[0-9a-fA-F]+u|0u|[1-9][0-9]*u/), name: new Pe("name", q.token, /([_\p{XID_Start}][\p{XID_Continue}]+)|([\p{XID_Start}])/u), ident: new Pe("ident", q.token, /[_a-zA-Z][0-9a-zA-Z_]*/), and: new Pe("and", q.token, "&"), and_and: new Pe("and_and", q.token, "&&"), arrow: new Pe("arrow ", q.token, "->"), attr: new Pe("attr", q.token, "@"), forward_slash: new Pe("forward_slash", q.token, "/"), bang: new Pe("bang", q.token, "!"), bracket_left: new Pe("bracket_left", q.token, "["), bracket_right: new Pe("bracket_right", q.token, "]"), brace_left: new Pe("brace_left", q.token, "{"), brace_right: new Pe("brace_right", q.token, "}"), colon: new Pe("colon", q.token, ":"), comma: new Pe("comma", q.token, ","), equal: new Pe("equal", q.token, "="), equal_equal: new Pe("equal_equal", q.token, "=="), not_equal: new Pe("not_equal", q.token, "!="), greater_than: new Pe("greater_than", q.token, ">"), greater_than_equal: new Pe("greater_than_equal", q.token, ">="), shift_right: new Pe("shift_right", q.token, ">>"), less_than: new Pe("less_than", q.token, "<"), less_than_equal: new Pe("less_than_equal", q.token, "<="), shift_left: new Pe("shift_left", q.token, "<<"), modulo: new Pe("modulo", q.token, "%"), minus: new Pe("minus", q.token, "-"), minus_minus: new Pe("minus_minus", q.token, "--"), period: new Pe("period", q.token, "."), plus: new Pe("plus", q.token, "+"), plus_plus: new Pe("plus_plus", q.token, "++"), or: new Pe("or", q.token, "|"), or_or: new Pe("or_or", q.token, "||"), paren_left: new Pe("paren_left", q.token, "("), paren_right: new Pe("paren_right", q.token, ")"), semicolon: new Pe("semicolon", q.token, ";"), star: new Pe("star", q.token, "*"), tilde: new Pe("tilde", q.token, "~"), underscore: new Pe("underscore", q.token, "_"), xor: new Pe("xor", q.token, "^"), plus_equal: new Pe("plus_equal", q.token, "+="), minus_equal: new Pe("minus_equal", q.token, "-="), times_equal: new Pe("times_equal", q.token, "*="), division_equal: new Pe("division_equal", q.token, "/="), modulo_equal: new Pe("modulo_equal", q.token, "%="), and_equal: new Pe("and_equal", q.token, "&="), or_equal: new Pe("or_equal", q.token, "|="), xor_equal: new Pe("xor_equal", q.token, "^="), shift_right_equal: new Pe("shift_right_equal", q.token, ">>="), shift_left_equal: new Pe("shift_left_equal", q.token, "<<=") }, We.simpleTokens = { "@": W.tokens.attr, "{": W.tokens.brace_left, "}": W.tokens.brace_right, ":": W.tokens.colon, ",": W.tokens.comma, "(": W.tokens.paren_left, ")": W.tokens.paren_right, ";": W.tokens.semicolon }, We.literalTokens = { "&": W.tokens.and, "&&": W.tokens.and_and, "->": W.tokens.arrow, "/": W.tokens.forward_slash, "!": W.tokens.bang, "[": W.tokens.bracket_left, "]": W.tokens.bracket_right, "=": W.tokens.equal, "==": W.tokens.equal_equal, "!=": W.tokens.not_equal, ">": W.tokens.greater_than, ">=": W.tokens.greater_than_equal, ">>": W.tokens.shift_right, "<": W.tokens.less_than, "<=": W.tokens.less_than_equal, "<<": W.tokens.shift_left, "%": W.tokens.modulo, "-": W.tokens.minus, "--": W.tokens.minus_minus, ".": W.tokens.period, "+": W.tokens.plus, "++": W.tokens.plus_plus, "|": W.tokens.or, "||": W.tokens.or_or, "*": W.tokens.star, "~": W.tokens.tilde, _: W.tokens.underscore, "^": W.tokens.xor, "+=": W.tokens.plus_equal, "-=": W.tokens.minus_equal, "*=": W.tokens.times_equal, "/=": W.tokens.division_equal, "%=": W.tokens.modulo_equal, "&=": W.tokens.and_equal, "|=": W.tokens.or_equal, "^=": W.tokens.xor_equal, ">>=": W.tokens.shift_right_equal, "<<=": W.tokens.shift_left_equal }, We.regexTokens = { decimal_float_literal: W.tokens.decimal_float_literal, hex_float_literal: W.tokens.hex_float_literal, int_literal: W.tokens.int_literal, uint_literal: W.tokens.uint_literal, ident: W.tokens.ident }, We.storage_class = [W.keywords.function, W.keywords.private, W.keywords.workgroup, W.keywords.uniform, W.keywords.storage], We.access_mode = [W.keywords.read, W.keywords.write, W.keywords.read_write], We.sampler_type = [W.keywords.sampler, W.keywords.sampler_comparison], We.sampled_texture_type = [W.keywords.texture_1d, W.keywords.texture_2d, W.keywords.texture_2d_array, W.keywords.texture_3d, W.keywords.texture_cube, W.keywords.texture_cube_array], We.multisampled_texture_type = [W.keywords.texture_multisampled_2d], We.storage_texture_type = [W.keywords.texture_storage_1d, W.keywords.texture_storage_2d, W.keywords.texture_storage_2d_array, W.keywords.texture_storage_3d], We.depth_texture_type = [W.keywords.texture_depth_2d, W.keywords.texture_depth_2d_array, W.keywords.texture_depth_cube, W.keywords.texture_depth_cube_array, W.keywords.texture_depth_multisampled_2d], We.texture_external_type = [W.keywords.texture_external], We.any_texture_type = [...W.sampled_texture_type, ...W.multisampled_texture_type, ...W.storage_texture_type, ...W.depth_texture_type, ...W.texture_external_type], We.texel_format = [W.keywords.r8unorm, W.keywords.r8snorm, W.keywords.r8uint, W.keywords.r8sint, W.keywords.r16uint, W.keywords.r16sint, W.keywords.r16float, W.keywords.rg8unorm, W.keywords.rg8snorm, W.keywords.rg8uint, W.keywords.rg8sint, W.keywords.r32uint, W.keywords.r32sint, W.keywords.r32float, W.keywords.rg16uint, W.keywords.rg16sint, W.keywords.rg16float, W.keywords.rgba8unorm, W.keywords.rgba8unorm_srgb, W.keywords.rgba8snorm, W.keywords.rgba8uint, W.keywords.rgba8sint, W.keywords.bgra8unorm, W.keywords.bgra8unorm_srgb, W.keywords.rgb10a2unorm, W.keywords.rg11b10float, W.keywords.rg32uint, W.keywords.rg32sint, W.keywords.rg32float, W.keywords.rgba16uint, W.keywords.rgba16sint, W.keywords.rgba16float, W.keywords.rgba32uint, W.keywords.rgba32sint, W.keywords.rgba32float], We.const_literal = [W.tokens.int_literal, W.tokens.uint_literal, W.tokens.decimal_float_literal, W.tokens.hex_float_literal, W.keywords.true, W.keywords.false], We.literal_or_ident = [W.tokens.ident, W.tokens.int_literal, W.tokens.uint_literal, W.tokens.decimal_float_literal, W.tokens.hex_float_literal, W.tokens.name], We.element_count_expression = [W.tokens.int_literal, W.tokens.uint_literal, W.tokens.ident], We.template_types = [W.keywords.vec2, W.keywords.vec3, W.keywords.vec4, W.keywords.mat2x2, W.keywords.mat2x3, W.keywords.mat2x4, W.keywords.mat3x2, W.keywords.mat3x3, W.keywords.mat3x4, W.keywords.mat4x2, W.keywords.mat4x3, W.keywords.mat4x4, W.keywords.atomic, W.keywords.bitcast, ...W.any_texture_type], We.attribute_name = [W.tokens.ident, W.keywords.block, W.keywords.diagnostic], We.assignment_operators = [W.tokens.equal, W.tokens.plus_equal, W.tokens.minus_equal, W.tokens.times_equal, W.tokens.division_equal, W.tokens.modulo_equal, W.tokens.and_equal, W.tokens.or_equal, W.tokens.xor_equal, W.tokens.shift_right_equal, W.tokens.shift_left_equal], We.increment_operators = [W.tokens.plus_plus, W.tokens.minus_minus];
  var qe = class {
    constructor(e2, t2, n2, s2, r2) {
      this.type = e2, this.lexeme = t2, this.line = n2, this.start = s2, this.end = r2;
    }
    toString() {
      return this.lexeme;
    }
    isTemplateType() {
      return -1 != We.template_types.indexOf(this.type);
    }
    isArrayType() {
      return this.type == We.keywords.array;
    }
    isArrayOrTemplateType() {
      return this.isArrayType() || this.isTemplateType();
    }
  };
  var He = class {
    constructor(e2) {
      this._tokens = [], this._start = 0, this._current = 0, this._line = 1, this._source = null != e2 ? e2 : "";
    }
    scanTokens() {
      for (; !this._isAtEnd(); )
        if (this._start = this._current, !this.scanToken())
          throw `Invalid syntax at line ${this._line}`;
      return this._tokens.push(new qe(We.eof, "", this._line, this._current, this._current)), this._tokens;
    }
    scanToken() {
      let e2 = this._advance();
      if ("\n" == e2)
        return this._line++, true;
      if (this._isWhitespace(e2))
        return true;
      if ("/" == e2) {
        if ("/" == this._peekAhead()) {
          for (; "\n" != e2; ) {
            if (this._isAtEnd())
              return true;
            e2 = this._advance();
          }
          return this._line++, true;
        }
        if ("*" == this._peekAhead()) {
          this._advance();
          let t3 = 1;
          for (; t3 > 0; ) {
            if (this._isAtEnd())
              return true;
            if (e2 = this._advance(), "\n" == e2)
              this._line++;
            else if ("*" == e2) {
              if ("/" == this._peekAhead() && (this._advance(), t3--, 0 == t3))
                return true;
            } else
              "/" == e2 && "*" == this._peekAhead() && (this._advance(), t3++);
          }
          return true;
        }
      }
      const t2 = We.simpleTokens[e2];
      if (t2)
        return this._addToken(t2), true;
      let n2 = We.none;
      const s2 = this._isAlpha(e2), r2 = "_" === e2;
      if (this._isAlphaNumeric(e2)) {
        let t3 = this._peekAhead();
        for (; this._isAlphaNumeric(t3); )
          e2 += this._advance(), t3 = this._peekAhead();
      }
      if (s2) {
        const t3 = We.keywords[e2];
        if (t3)
          return this._addToken(t3), true;
      }
      if (s2 || r2)
        return this._addToken(We.tokens.ident), true;
      for (; ; ) {
        let t3 = this._findType(e2);
        const s3 = this._peekAhead();
        if ("-" == e2 && this._tokens.length > 0) {
          if ("=" == s3)
            return this._current++, e2 += s3, this._addToken(We.tokens.minus_equal), true;
          if ("-" == s3)
            return this._current++, e2 += s3, this._addToken(We.tokens.minus_minus), true;
          const n3 = this._tokens.length - 1;
          if ((-1 != We.literal_or_ident.indexOf(this._tokens[n3].type) || this._tokens[n3].type == We.tokens.paren_right) && ">" != s3)
            return this._addToken(t3), true;
        }
        if (">" == e2 && (">" == s3 || "=" == s3)) {
          let e3 = false, n3 = this._tokens.length - 1;
          for (let t4 = 0; t4 < 5 && n3 >= 0 && -1 === We.assignment_operators.indexOf(this._tokens[n3].type); ++t4, --n3)
            if (this._tokens[n3].type === We.tokens.less_than) {
              n3 > 0 && this._tokens[n3 - 1].isArrayOrTemplateType() && (e3 = true);
              break;
            }
          if (e3)
            return this._addToken(t3), true;
        }
        if (t3 === We.none) {
          let s4 = e2, r3 = 0;
          const a2 = 2;
          for (let e3 = 0; e3 < a2; ++e3)
            if (s4 += this._peekAhead(e3), t3 = this._findType(s4), t3 !== We.none) {
              r3 = e3;
              break;
            }
          if (t3 === We.none)
            return n2 !== We.none && (this._current--, this._addToken(n2), true);
          e2 = s4, this._current += r3 + 1;
        }
        if (n2 = t3, this._isAtEnd())
          break;
        e2 += this._advance();
      }
      return n2 !== We.none && (this._addToken(n2), true);
    }
    _findType(e2) {
      for (const t3 in We.regexTokens) {
        const n2 = We.regexTokens[t3];
        if (this._match(e2, n2.rule))
          return n2;
      }
      const t2 = We.literalTokens[e2];
      return t2 || We.none;
    }
    _match(e2, t2) {
      const n2 = t2.exec(e2);
      return n2 && 0 == n2.index && n2[0] == e2;
    }
    _isAtEnd() {
      return this._current >= this._source.length;
    }
    _isAlpha(e2) {
      return !this._isNumeric(e2) && !this._isWhitespace(e2) && "_" !== e2 && "." !== e2 && "(" !== e2 && ")" !== e2 && "[" !== e2 && "]" !== e2 && "{" !== e2 && "}" !== e2 && "," !== e2 && ";" !== e2 && ":" !== e2 && "=" !== e2 && "!" !== e2 && "<" !== e2 && ">" !== e2 && "+" !== e2 && "-" !== e2 && "*" !== e2 && "/" !== e2 && "%" !== e2 && "&" !== e2 && "|" !== e2 && "^" !== e2 && "~" !== e2 && "@" !== e2 && "#" !== e2 && "?" !== e2 && "'" !== e2 && "`" !== e2 && '"' !== e2 && "\\" !== e2 && "\n" !== e2 && "\r" !== e2 && "	" !== e2 && "\0" !== e2;
    }
    _isNumeric(e2) {
      return e2 >= "0" && e2 <= "9";
    }
    _isAlphaNumeric(e2) {
      return this._isAlpha(e2) || this._isNumeric(e2) || "_" === e2;
    }
    _isWhitespace(e2) {
      return " " == e2 || "	" == e2 || "\r" == e2;
    }
    _advance(e2 = 0) {
      let t2 = this._source[this._current];
      return e2 = e2 || 0, e2++, this._current += e2, t2;
    }
    _peekAhead(e2 = 0) {
      return e2 = e2 || 0, this._current + e2 >= this._source.length ? "\0" : this._source[this._current + e2];
    }
    _addToken(e2) {
      const t2 = this._source.substring(this._start, this._current);
      this._tokens.push(new qe(e2, t2, this._line, this._start, this._current));
    }
  };
  function ze(e2) {
    return Array.isArray(e2) || (null == e2 ? void 0 : e2.buffer) instanceof ArrayBuffer;
  }
  var Re = new Float32Array(1);
  var Ge = new Uint32Array(Re.buffer);
  var Xe = new Uint32Array(Re.buffer);
  var je = new Int32Array(1);
  var Ze = new Float32Array(je.buffer);
  var Qe = new Uint32Array(je.buffer);
  var Ye = new Uint32Array(1);
  var Ke = new Float32Array(Ye.buffer);
  var Je = new Int32Array(Ye.buffer);
  function et(e2, t2, n2) {
    if (t2 === n2)
      return e2;
    if ("f32" === t2) {
      if ("i32" === n2 || "x32" === n2)
        return Re[0] = e2, Ge[0];
      if ("u32" === n2)
        return Re[0] = e2, Xe[0];
    } else if ("i32" === t2 || "x32" === t2) {
      if ("f32" === n2)
        return je[0] = e2, Ze[0];
      if ("u32" === n2)
        return je[0] = e2, Qe[0];
    } else if ("u32" === t2) {
      if ("f32" === n2)
        return Ye[0] = e2, Ke[0];
      if ("i32" === n2 || "x32" === n2)
        return Ye[0] = e2, Je[0];
    }
    return console.error(`Unsupported cast from ${t2} to ${n2}`), e2;
  }
  var tt = class {
    constructor(e2) {
      this.resources = null, this.inUse = false, this.info = null, this.node = e2;
    }
  };
  var nt = class {
    constructor(e2, t2) {
      this.align = e2, this.size = t2;
    }
  };
  var st = class {
    constructor() {
      this.uniforms = [], this.storage = [], this.textures = [], this.samplers = [], this.aliases = [], this.overrides = [], this.structs = [], this.entry = new p(), this.functions = [], this._types = /* @__PURE__ */ new Map(), this._functions = /* @__PURE__ */ new Map();
    }
    _isStorageTexture(e2) {
      return "texture_storage_1d" == e2.name || "texture_storage_2d" == e2.name || "texture_storage_2d_array" == e2.name || "texture_storage_3d" == e2.name;
    }
    updateAST(e2) {
      for (const t2 of e2)
        t2 instanceof L && this._functions.set(t2.name, new tt(t2));
      for (const t2 of e2)
        if (t2 instanceof ae) {
          const e3 = this.getTypeInfo(t2, null);
          e3 instanceof n && this.structs.push(e3);
        }
      for (const t2 of e2)
        if (t2 instanceof J)
          this.aliases.push(this._getAliasInfo(t2));
        else if (t2 instanceof B) {
          const e3 = t2, n2 = this._getAttributeNum(e3.attributes, "id", 0), s2 = null != e3.type ? this.getTypeInfo(e3.type, e3.attributes) : null;
          this.overrides.push(new u(e3.name, s2, e3.attributes, n2));
        } else if (this._isUniformVar(t2)) {
          const e3 = t2, n2 = this._getAttributeNum(e3.attributes, "group", 0), s2 = this._getAttributeNum(e3.attributes, "binding", 0), r2 = this.getTypeInfo(e3.type, e3.attributes), o2 = new i(e3.name, r2, n2, s2, e3.attributes, a.Uniform, e3.access);
          o2.access || (o2.access = "read"), this.uniforms.push(o2);
        } else if (this._isStorageVar(t2)) {
          const e3 = t2, n2 = this._getAttributeNum(e3.attributes, "group", 0), s2 = this._getAttributeNum(e3.attributes, "binding", 0), r2 = this.getTypeInfo(e3.type, e3.attributes), o2 = this._isStorageTexture(r2), l2 = new i(e3.name, r2, n2, s2, e3.attributes, o2 ? a.StorageTexture : a.Storage, e3.access);
          l2.access || (l2.access = "read"), this.storage.push(l2);
        } else if (this._isTextureVar(t2)) {
          const e3 = t2, n2 = this._getAttributeNum(e3.attributes, "group", 0), s2 = this._getAttributeNum(e3.attributes, "binding", 0), r2 = this.getTypeInfo(e3.type, e3.attributes), o2 = this._isStorageTexture(r2), l2 = new i(e3.name, r2, n2, s2, e3.attributes, o2 ? a.StorageTexture : a.Texture, e3.access);
          l2.access || (l2.access = "read"), o2 ? this.storage.push(l2) : this.textures.push(l2);
        } else if (this._isSamplerVar(t2)) {
          const e3 = t2, n2 = this._getAttributeNum(e3.attributes, "group", 0), s2 = this._getAttributeNum(e3.attributes, "binding", 0), r2 = this.getTypeInfo(e3.type, e3.attributes), o2 = new i(e3.name, r2, n2, s2, e3.attributes, a.Sampler, e3.access);
          this.samplers.push(o2);
        } else if (t2 instanceof L) {
          const e3 = this._getAttribute(t2, "vertex"), n2 = this._getAttribute(t2, "fragment"), s2 = this._getAttribute(t2, "compute"), r2 = e3 || n2 || s2, a2 = new f(t2.name, null == r2 ? void 0 : r2.name, t2.attributes);
          a2.attributes = t2.attributes, a2.startLine = t2.startLine, a2.endLine = t2.endLine, this.functions.push(a2), this._functions.get(t2.name).info = a2, r2 && (this._functions.get(t2.name).inUse = true, a2.inUse = true, a2.resources = this._findResources(t2, !!r2), a2.inputs = this._getInputs(t2.args), a2.outputs = this._getOutputs(t2.returnType), this.entry[r2.name].push(a2)), a2.arguments = t2.args.map((e4) => new h(e4.name, this.getTypeInfo(e4.type, e4.attributes), e4.attributes)), a2.returnType = t2.returnType ? this.getTypeInfo(t2.returnType, t2.attributes) : null;
        } else
          ;
      for (const e3 of this._functions.values())
        e3.info && (e3.info.inUse = e3.inUse, this._addCalls(e3.node, e3.info.calls));
      for (const e3 of this._functions.values())
        e3.node.search((t2) => {
          var n2, s2, r2;
          if (t2 instanceof Le) {
            if (t2.value)
              if (ze(t2.value))
                for (const s3 of t2.value)
                  for (const t3 of this.overrides)
                    s3 === t3.name && (null === (n2 = e3.info) || void 0 === n2 || n2.overrides.push(t3));
              else
                for (const n3 of this.overrides)
                  t2.value === n3.name && (null === (s2 = e3.info) || void 0 === s2 || s2.overrides.push(n3));
          } else if (t2 instanceof de)
            for (const n3 of this.overrides)
              t2.name === n3.name && (null === (r2 = e3.info) || void 0 === r2 || r2.overrides.push(n3));
        });
      for (const e3 of this.uniforms)
        this._markStructsInUse(e3.type);
      for (const e3 of this.storage)
        this._markStructsInUse(e3.type);
    }
    getStructInfo(e2) {
      for (const t2 of this.structs)
        if (t2.name == e2)
          return t2;
      return null;
    }
    getOverrideInfo(e2) {
      for (const t2 of this.overrides)
        if (t2.name == e2)
          return t2;
      return null;
    }
    _markStructsInUse(e2) {
      if (e2)
        if (e2.isStruct) {
          if (e2.inUse = true, e2.members)
            for (const t2 of e2.members)
              this._markStructsInUse(t2.type);
        } else if (e2.isArray)
          this._markStructsInUse(e2.format);
        else if (e2.isTemplate)
          e2.format && this._markStructsInUse(e2.format);
        else {
          const t2 = this._getAlias(e2.name);
          t2 && this._markStructsInUse(t2);
        }
    }
    _addCalls(e2, t2) {
      var n2;
      for (const s2 of e2.calls) {
        const e3 = null === (n2 = this._functions.get(s2.name)) || void 0 === n2 ? void 0 : n2.info;
        e3 && t2.add(e3);
      }
    }
    findResource(e2, t2, n2) {
      if (n2) {
        for (const s2 of this.entry.compute)
          if (s2.name === n2) {
            for (const n3 of s2.resources)
              if (n3.group == e2 && n3.binding == t2)
                return n3;
          }
        for (const s2 of this.entry.vertex)
          if (s2.name === n2) {
            for (const n3 of s2.resources)
              if (n3.group == e2 && n3.binding == t2)
                return n3;
          }
        for (const s2 of this.entry.fragment)
          if (s2.name === n2) {
            for (const n3 of s2.resources)
              if (n3.group == e2 && n3.binding == t2)
                return n3;
          }
      }
      for (const n3 of this.uniforms)
        if (n3.group == e2 && n3.binding == t2)
          return n3;
      for (const n3 of this.storage)
        if (n3.group == e2 && n3.binding == t2)
          return n3;
      for (const n3 of this.textures)
        if (n3.group == e2 && n3.binding == t2)
          return n3;
      for (const n3 of this.samplers)
        if (n3.group == e2 && n3.binding == t2)
          return n3;
      return null;
    }
    _findResource(e2) {
      for (const t2 of this.uniforms)
        if (t2.name == e2)
          return t2;
      for (const t2 of this.storage)
        if (t2.name == e2)
          return t2;
      for (const t2 of this.textures)
        if (t2.name == e2)
          return t2;
      for (const t2 of this.samplers)
        if (t2.name == e2)
          return t2;
      return null;
    }
    _markStructsFromAST(e2) {
      const t2 = this.getTypeInfo(e2, null);
      this._markStructsInUse(t2);
    }
    _findResources(e2, t2) {
      const n2 = [], s2 = this, r2 = [];
      return e2.search((a2) => {
        if (a2 instanceof S)
          r2.push({});
        else if (a2 instanceof A)
          r2.pop();
        else if (a2 instanceof O) {
          const e3 = a2;
          t2 && null !== e3.type && this._markStructsFromAST(e3.type), r2.length > 0 && (r2[r2.length - 1][e3.name] = e3);
        } else if (a2 instanceof fe) {
          const e3 = a2;
          t2 && null !== e3.type && this._markStructsFromAST(e3.type);
        } else if (a2 instanceof F) {
          const e3 = a2;
          t2 && null !== e3.type && this._markStructsFromAST(e3.type), r2.length > 0 && (r2[r2.length - 1][e3.name] = e3);
        } else if (a2 instanceof de) {
          const e3 = a2;
          if (r2.length > 0) {
            if (r2[r2.length - 1][e3.name])
              return;
          }
          const t3 = s2._findResource(e3.name);
          t3 && n2.push(t3);
        } else if (a2 instanceof pe) {
          const r3 = a2, i2 = s2._functions.get(r3.name);
          i2 && (t2 && (i2.inUse = true), e2.calls.add(i2.node), null === i2.resources && (i2.resources = s2._findResources(i2.node, t2)), n2.push(...i2.resources));
        } else if (a2 instanceof R) {
          const r3 = a2, i2 = s2._functions.get(r3.name);
          i2 && (t2 && (i2.inUse = true), e2.calls.add(i2.node), null === i2.resources && (i2.resources = s2._findResources(i2.node, t2)), n2.push(...i2.resources));
        }
      }), [...new Map(n2.map((e3) => [e3.name, e3])).values()];
    }
    getBindGroups() {
      const e2 = [];
      function t2(t3, n2) {
        t3 >= e2.length && (e2.length = t3 + 1), void 0 === e2[t3] && (e2[t3] = []), n2 >= e2[t3].length && (e2[t3].length = n2 + 1);
      }
      for (const n2 of this.uniforms) {
        t2(n2.group, n2.binding);
        e2[n2.group][n2.binding] = n2;
      }
      for (const n2 of this.storage) {
        t2(n2.group, n2.binding);
        e2[n2.group][n2.binding] = n2;
      }
      for (const n2 of this.textures) {
        t2(n2.group, n2.binding);
        e2[n2.group][n2.binding] = n2;
      }
      for (const n2 of this.samplers) {
        t2(n2.group, n2.binding);
        e2[n2.group][n2.binding] = n2;
      }
      return e2;
    }
    _getOutputs(e2, t2 = void 0) {
      if (void 0 === t2 && (t2 = []), e2 instanceof ae)
        this._getStructOutputs(e2, t2);
      else {
        const n2 = this._getOutputInfo(e2);
        null !== n2 && t2.push(n2);
      }
      return t2;
    }
    _getStructOutputs(e2, t2) {
      for (const n2 of e2.members)
        if (n2.type instanceof ae)
          this._getStructOutputs(n2.type, t2);
        else {
          const e3 = this._getAttribute(n2, "location") || this._getAttribute(n2, "builtin");
          if (null !== e3) {
            const s2 = this.getTypeInfo(n2.type, n2.type.attributes), r2 = this._parseInt(e3.value), a2 = new c(n2.name, s2, e3.name, r2);
            t2.push(a2);
          }
        }
    }
    _getOutputInfo(e2) {
      const t2 = this._getAttribute(e2, "location") || this._getAttribute(e2, "builtin");
      if (null !== t2) {
        const n2 = this.getTypeInfo(e2, e2.attributes), s2 = this._parseInt(t2.value);
        return new c("", n2, t2.name, s2);
      }
      return null;
    }
    _getInputs(e2, t2 = void 0) {
      void 0 === t2 && (t2 = []);
      for (const n2 of e2)
        if (n2.type instanceof ae)
          this._getStructInputs(n2.type, t2);
        else {
          const e3 = this._getInputInfo(n2);
          null !== e3 && t2.push(e3);
        }
      return t2;
    }
    _getStructInputs(e2, t2) {
      for (const n2 of e2.members)
        if (n2.type instanceof ae)
          this._getStructInputs(n2.type, t2);
        else {
          const e3 = this._getInputInfo(n2);
          null !== e3 && t2.push(e3);
        }
    }
    _getInputInfo(e2) {
      const t2 = this._getAttribute(e2, "location") || this._getAttribute(e2, "builtin");
      if (null !== t2) {
        const n2 = this._getAttribute(e2, "interpolation"), s2 = this.getTypeInfo(e2.type, e2.attributes), r2 = this._parseInt(t2.value), a2 = new l(e2.name, s2, t2.name, r2);
        return null !== n2 && (a2.interpolation = this._parseString(n2.value)), a2;
      }
      return null;
    }
    _parseString(e2) {
      return e2 instanceof Array && (e2 = e2[0]), e2;
    }
    _parseInt(e2) {
      e2 instanceof Array && (e2 = e2[0]);
      const t2 = parseInt(e2);
      return isNaN(t2) ? e2 : t2;
    }
    _getAlias(e2) {
      for (const t2 of this.aliases)
        if (t2.name == e2)
          return t2.type;
      return null;
    }
    _getAliasInfo(e2) {
      return new o(e2.name, this.getTypeInfo(e2.type, null));
    }
    getTypeInfoByName(e2) {
      for (const t2 of this.structs)
        if (t2.name == e2)
          return t2;
      for (const t2 of this.aliases)
        if (t2.name == e2)
          return t2.type;
      return null;
    }
    getTypeInfo(a2, i2 = null) {
      if (this._types.has(a2))
        return this._types.get(a2);
      if (a2 instanceof le) {
        const e2 = a2, t2 = e2.format ? this.getTypeInfo(e2.format, e2.attributes) : null, n2 = new s(e2.name, i2);
        return n2.format = t2, n2.count = e2.count, this._types.set(a2, n2), this._updateTypeInfo(n2), n2;
      }
      if (a2 instanceof ae) {
        const e2 = a2, s2 = new n(e2.name, i2);
        s2.startLine = e2.startLine, s2.endLine = e2.endLine;
        for (const n2 of e2.members) {
          const e3 = this.getTypeInfo(n2.type, n2.attributes);
          s2.members.push(new t(n2.name, e3, n2.attributes));
        }
        return this._types.set(a2, s2), this._updateTypeInfo(s2), s2;
      }
      if (a2 instanceof ce) {
        const t2 = a2, n2 = t2.format instanceof se, s2 = t2.format ? n2 ? this.getTypeInfo(t2.format, null) : new e(t2.format, null) : null, o3 = new r(t2.name, s2, i2, t2.access);
        return this._types.set(a2, o3), this._updateTypeInfo(o3), o3;
      }
      if (a2 instanceof ie) {
        const e2 = a2, t2 = e2.format ? this.getTypeInfo(e2.format, null) : null, n2 = new r(e2.name, t2, i2, e2.access);
        return this._types.set(a2, n2), this._updateTypeInfo(n2), n2;
      }
      const o2 = new e(a2.name, i2);
      return this._types.set(a2, o2), this._updateTypeInfo(o2), o2;
    }
    _updateTypeInfo(e2) {
      var t2, r2, a2;
      const i2 = this._getTypeSize(e2);
      if (e2.size = null !== (t2 = null == i2 ? void 0 : i2.size) && void 0 !== t2 ? t2 : 0, e2 instanceof s && e2.format) {
        const t3 = this._getTypeSize(e2.format);
        e2.stride = Math.max(null !== (r2 = null == t3 ? void 0 : t3.size) && void 0 !== r2 ? r2 : 0, null !== (a2 = null == t3 ? void 0 : t3.align) && void 0 !== a2 ? a2 : 0), this._updateTypeInfo(e2.format);
      }
      e2 instanceof n && this._updateStructInfo(e2);
    }
    _updateStructInfo(e2) {
      var t2;
      let n2 = 0, s2 = 0, r2 = 0, a2 = 0;
      for (let i2 = 0, o2 = e2.members.length; i2 < o2; ++i2) {
        const o3 = e2.members[i2], l2 = this._getTypeSize(o3);
        if (!l2)
          continue;
        null !== (t2 = this._getAlias(o3.type.name)) && void 0 !== t2 || o3.type;
        const c2 = l2.align, u2 = l2.size;
        n2 = this._roundUp(c2, n2 + s2), s2 = u2, r2 = n2, a2 = Math.max(a2, c2), o3.offset = n2, o3.size = u2, this._updateTypeInfo(o3.type);
      }
      e2.size = this._roundUp(a2, r2 + s2), e2.align = a2;
    }
    _getTypeSize(r2) {
      var a2, i2;
      if (null == r2)
        return null;
      const o2 = this._getAttributeNum(r2.attributes, "size", 0), l2 = this._getAttributeNum(r2.attributes, "align", 0);
      if (r2 instanceof t && (r2 = r2.type), r2 instanceof e) {
        const e2 = this._getAlias(r2.name);
        null !== e2 && (r2 = e2);
      }
      {
        const e2 = st._typeInfo[r2.name];
        if (void 0 !== e2) {
          const t2 = "f16" === (null === (a2 = r2.format) || void 0 === a2 ? void 0 : a2.name) ? 2 : 1;
          return new nt(Math.max(l2, e2.align / t2), Math.max(o2, e2.size / t2));
        }
      }
      {
        const e2 = st._typeInfo[r2.name.substring(0, r2.name.length - 1)];
        if (e2) {
          const t2 = "h" === r2.name[r2.name.length - 1] ? 2 : 1;
          return new nt(Math.max(l2, e2.align / t2), Math.max(o2, e2.size / t2));
        }
      }
      if (r2 instanceof s) {
        let e2 = r2, t2 = 8, n2 = 8;
        const s2 = this._getTypeSize(e2.format);
        null !== s2 && (n2 = s2.size, t2 = s2.align);
        return n2 = e2.count * this._getAttributeNum(null !== (i2 = null == r2 ? void 0 : r2.attributes) && void 0 !== i2 ? i2 : null, "stride", this._roundUp(t2, n2)), o2 && (n2 = o2), new nt(Math.max(l2, t2), Math.max(o2, n2));
      }
      if (r2 instanceof n) {
        let e2 = 0, t2 = 0, n2 = 0, s2 = 0, a3 = 0;
        for (const t3 of r2.members) {
          const r3 = this._getTypeSize(t3.type);
          null !== r3 && (e2 = Math.max(r3.align, e2), n2 = this._roundUp(r3.align, n2 + s2), s2 = r3.size, a3 = n2);
        }
        return t2 = this._roundUp(e2, a3 + s2), new nt(Math.max(l2, e2), Math.max(o2, t2));
      }
      return null;
    }
    _isUniformVar(e2) {
      return e2 instanceof O && "uniform" == e2.storage;
    }
    _isStorageVar(e2) {
      return e2 instanceof O && "storage" == e2.storage;
    }
    _isTextureVar(e2) {
      return e2 instanceof O && null !== e2.type && -1 != st._textureTypes.indexOf(e2.type.name);
    }
    _isSamplerVar(e2) {
      return e2 instanceof O && null !== e2.type && -1 != st._samplerTypes.indexOf(e2.type.name);
    }
    _getAttribute(e2, t2) {
      const n2 = e2;
      if (!n2 || !n2.attributes)
        return null;
      const s2 = n2.attributes;
      for (let e3 of s2)
        if (e3.name == t2)
          return e3;
      return null;
    }
    _getAttributeNum(e2, t2, n2) {
      if (null === e2)
        return n2;
      for (let s2 of e2)
        if (s2.name == t2) {
          let e3 = null !== s2 && null !== s2.value ? s2.value : n2;
          return e3 instanceof Array && (e3 = e3[0]), "number" == typeof e3 ? e3 : "string" == typeof e3 ? parseInt(e3) : n2;
        }
      return n2;
    }
    _roundUp(e2, t2) {
      return Math.ceil(t2 / e2) * e2;
    }
  };
  st._typeInfo = { f16: { align: 2, size: 2 }, i32: { align: 4, size: 4 }, u32: { align: 4, size: 4 }, f32: { align: 4, size: 4 }, atomic: { align: 4, size: 4 }, vec2: { align: 8, size: 8 }, vec3: { align: 16, size: 12 }, vec4: { align: 16, size: 16 }, mat2x2: { align: 8, size: 16 }, mat3x2: { align: 8, size: 24 }, mat4x2: { align: 8, size: 32 }, mat2x3: { align: 16, size: 32 }, mat3x3: { align: 16, size: 48 }, mat4x3: { align: 16, size: 64 }, mat2x4: { align: 16, size: 32 }, mat3x4: { align: 16, size: 48 }, mat4x4: { align: 16, size: 64 } }, st._textureTypes = We.any_texture_type.map((e2) => e2.name), st._samplerTypes = We.sampler_type.map((e2) => e2.name);
  var rt = class {
    constructor(e2, t2, n2) {
      this.name = e2, this.value = t2, this.node = n2;
    }
    clone() {
      return new rt(this.name, this.value, this.node);
    }
  };
  var at = class {
    constructor(e2) {
      this.name = e2.name, this.node = e2;
    }
    clone() {
      return new at(this.node);
    }
  };
  var it = class {
    constructor(e2) {
      this.parent = null, this.variables = /* @__PURE__ */ new Map(), this.functions = /* @__PURE__ */ new Map(), this.currentFunctionName = "", e2 && (this.parent = e2, this.currentFunctionName = e2.currentFunctionName);
    }
    getVariable(e2) {
      var t2;
      return this.variables.has(e2) ? null !== (t2 = this.variables.get(e2)) && void 0 !== t2 ? t2 : null : this.parent ? this.parent.getVariable(e2) : null;
    }
    getFunction(e2) {
      var t2;
      return this.functions.has(e2) ? null !== (t2 = this.functions.get(e2)) && void 0 !== t2 ? t2 : null : this.parent ? this.parent.getFunction(e2) : null;
    }
    createVariable(e2, t2, n2) {
      this.variables.set(e2, new rt(e2, t2, null != n2 ? n2 : null));
    }
    setVariable(e2, t2, n2) {
      const s2 = this.getVariable(e2);
      null !== s2 ? s2.value = t2 : this.createVariable(e2, t2, n2);
    }
    getVariableValue(e2) {
      var t2;
      const n2 = this.getVariable(e2);
      return null !== (t2 = null == n2 ? void 0 : n2.value) && void 0 !== t2 ? t2 : null;
    }
    clone() {
      return new it(this);
    }
  };
  var ot = class {
    evalExpression(e2, t2) {
      return null;
    }
    getTypeInfo(e2) {
      return null;
    }
    getVariableName(e2, t2) {
      return "";
    }
  };
  var lt = class {
    constructor(e2) {
      this.exec = e2;
    }
    getTypeInfo(e2) {
      return this.exec.getTypeInfo(e2);
    }
    All(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      let s2 = true;
      if (n2 instanceof Be)
        return n2.data.forEach((e3) => {
          e3 || (s2 = false);
        }), new Ve(s2 ? 1 : 0, this.getTypeInfo("bool"));
      throw new Error(`All() expects a vector argument. Line ${e2.line}`);
    }
    Any(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be) {
        const e3 = n2.data.some((e4) => e4);
        return new Ve(e3 ? 1 : 0, this.getTypeInfo("bool"));
      }
      throw new Error(`Any() expects a vector argument. Line ${e2.line}`);
    }
    Select(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[2], t2);
      if (!(n2 instanceof Ve))
        throw new Error(`Select() expects a bool condition. Line ${e2.line}`);
      return n2.value ? this.exec.evalExpression(e2.args[1], t2) : this.exec.evalExpression(e2.args[0], t2);
    }
    ArrayLength(e2, t2) {
      let n2 = e2.args[0];
      n2 instanceof ve && (n2 = n2.right);
      const s2 = this.exec.evalExpression(n2, t2);
      if (s2 instanceof Me && 0 === s2.typeInfo.size) {
        const e3 = s2.typeInfo, t3 = s2.buffer.byteLength / e3.stride;
        return new Ve(t3, this.getTypeInfo("u32"));
      }
      return new Ve(s2.typeInfo.size, this.getTypeInfo("u32"));
    }
    Abs(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.abs(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.abs(s2.value), s2.typeInfo);
    }
    Acos(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.acos(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.acos(s2.value), n2.typeInfo);
    }
    Acosh(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.acosh(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.acosh(s2.value), n2.typeInfo);
    }
    Asin(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.asin(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.asin(s2.value), n2.typeInfo);
    }
    Asinh(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.asinh(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.asinh(s2.value), n2.typeInfo);
    }
    Atan(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.atan(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.atan(s2.value), n2.typeInfo);
    }
    Atanh(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.atanh(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.atanh(s2.value), n2.typeInfo);
    }
    Atan2(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2);
      if (n2 instanceof Be && s2 instanceof Be)
        return new Be(n2.data.map((e3, t3) => Math.atan2(e3, s2.data[t3])), n2.typeInfo);
      const r2 = n2, a2 = s2;
      return new Ve(Math.atan2(r2.value, a2.value), n2.typeInfo);
    }
    Ceil(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.ceil(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.ceil(s2.value), n2.typeInfo);
    }
    _clamp(e2, t2, n2) {
      return Math.min(Math.max(e2, t2), n2);
    }
    Clamp(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2), r2 = this.exec.evalExpression(e2.args[2], t2);
      if (n2 instanceof Be && s2 instanceof Be && r2 instanceof Be)
        return new Be(n2.data.map((e3, t3) => this._clamp(e3, s2.data[t3], r2.data[t3])), n2.typeInfo);
      const a2 = n2, i2 = s2, o2 = r2;
      return new Ve(this._clamp(a2.value, i2.value, o2.value), n2.typeInfo);
    }
    Cos(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.cos(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.cos(s2.value), n2.typeInfo);
    }
    Cosh(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.cosh(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.cos(s2.value), n2.typeInfo);
    }
    CountLeadingZeros(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.clz32(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.clz32(s2.value), n2.typeInfo);
    }
    _countOneBits(e2) {
      let t2 = 0;
      for (; 0 !== e2; )
        1 & e2 && t2++, e2 >>= 1;
      return t2;
    }
    CountOneBits(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => this._countOneBits(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(this._countOneBits(s2.value), n2.typeInfo);
    }
    _countTrailingZeros(e2) {
      if (0 === e2)
        return 32;
      let t2 = 0;
      for (; !(1 & e2); )
        e2 >>= 1, t2++;
      return t2;
    }
    CountTrailingZeros(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => this._countTrailingZeros(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(this._countTrailingZeros(s2.value), n2.typeInfo);
    }
    Cross(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2);
      if (n2 instanceof Be && s2 instanceof Be) {
        if (3 !== n2.data.length || 3 !== s2.data.length)
          return console.error(`Cross() expects 3D vectors. Line ${e2.line}`), null;
        const t3 = n2.data, r2 = s2.data;
        return new Be([t3[1] * r2[2] - r2[1] * t3[2], t3[2] * r2[0] - r2[2] * t3[0], t3[0] * r2[1] - r2[0] * t3[1]], n2.typeInfo);
      }
      return console.error(`Cross() expects vector arguments. Line ${e2.line}`), null;
    }
    Degrees(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = 180 / Math.PI;
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => e3 * s2), n2.typeInfo);
      return new Ve(n2.value * s2, this.getTypeInfo("f32"));
    }
    Determinant(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Fe) {
        const e3 = n2.data, t3 = n2.typeInfo.getTypeName(), s2 = t3.endsWith("h") ? this.getTypeInfo("f16") : this.getTypeInfo("f32");
        if ("mat2x2" === t3 || "mat2x2f" === t3 || "mat2x2h" === t3)
          return new Ve(e3[0] * e3[3] - e3[1] * e3[2], s2);
        if ("mat2x3" === t3 || "mat2x3f" === t3 || "mat2x3h" === t3)
          return new Ve(e3[0] * (e3[4] * e3[8] - e3[5] * e3[7]) - e3[1] * (e3[3] * e3[8] - e3[5] * e3[6]) + e3[2] * (e3[3] * e3[7] - e3[4] * e3[6]), s2);
        if ("mat2x4" === t3 || "mat2x4f" === t3 || "mat2x4h" === t3)
          console.error(`TODO: Determinant for ${t3}`);
        else if ("mat3x2" === t3 || "mat3x2f" === t3 || "mat3x2h" === t3)
          console.error(`TODO: Determinant for ${t3}`);
        else {
          if ("mat3x3" === t3 || "mat3x3f" === t3 || "mat3x3h" === t3)
            return new Ve(e3[0] * (e3[4] * e3[8] - e3[5] * e3[7]) - e3[1] * (e3[3] * e3[8] - e3[5] * e3[6]) + e3[2] * (e3[3] * e3[7] - e3[4] * e3[6]), s2);
          "mat3x4" === t3 || "mat3x4f" === t3 || "mat3x4h" === t3 || "mat4x2" === t3 || "mat4x2f" === t3 || "mat4x2h" === t3 || "mat4x3" === t3 || "mat4x3f" === t3 || "mat4x3h" === t3 ? console.error(`TODO: Determinant for ${t3}`) : "mat4x4" !== t3 && "mat4x4f" !== t3 && "mat4x4h" !== t3 || console.error(`TODO: Determinant for ${t3}`);
        }
      }
      return console.error(`Determinant expects a matrix argument. Line ${e2.line}`), null;
    }
    Distance(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2);
      if (n2 instanceof Be && s2 instanceof Be) {
        let e3 = 0;
        for (let t3 = 0; t3 < n2.data.length; ++t3)
          e3 += (n2.data[t3] - s2.data[t3]) * (n2.data[t3] - s2.data[t3]);
        return new Ve(Math.sqrt(e3), this.getTypeInfo("f32"));
      }
      const r2 = n2, a2 = s2;
      return new Ve(Math.abs(r2.value - a2.value), n2.typeInfo);
    }
    _dot(e2, t2) {
      let n2 = 0;
      for (let s2 = 0; s2 < e2.length; ++s2)
        n2 += t2[s2] * e2[s2];
      return n2;
    }
    Dot(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2);
      return n2 instanceof Be && s2 instanceof Be ? new Ve(this._dot(n2.data, s2.data), this.getTypeInfo("f32")) : (console.error(`Dot() expects vector arguments. Line ${e2.line}`), null);
    }
    Dot4U8Packed(e2, t2) {
      return console.error(`TODO: dot4U8Packed. Line ${e2.line}`), null;
    }
    Dot4I8Packed(e2, t2) {
      return console.error(`TODO: dot4I8Packed. Line ${e2.line}`), null;
    }
    Exp(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.exp(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.exp(s2.value), n2.typeInfo);
    }
    Exp2(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.pow(2, e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.pow(2, s2.value), n2.typeInfo);
    }
    ExtractBits(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2), r2 = this.exec.evalExpression(e2.args[2], t2);
      if ("u32" !== s2.typeInfo.name && "x32" !== s2.typeInfo.name)
        return console.error(`ExtractBits() expects an i32 offset argument. Line ${e2.line}`), null;
      if ("u32" !== r2.typeInfo.name && "x32" !== r2.typeInfo.name)
        return console.error(`ExtractBits() expects an i32 count argument. Line ${e2.line}`), null;
      const a2 = s2.value, i2 = r2.value;
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => e3 >> a2 & (1 << i2) - 1), n2.typeInfo);
      if ("i32" !== n2.typeInfo.name && "x32" !== n2.typeInfo.name)
        return console.error(`ExtractBits() expects an i32 argument. Line ${e2.line}`), null;
      const o2 = n2.value;
      return new Ve(o2 >> a2 & (1 << i2) - 1, this.getTypeInfo("i32"));
    }
    FaceForward(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2), r2 = this.exec.evalExpression(e2.args[2], t2);
      if (n2 instanceof Be && s2 instanceof Be && r2 instanceof Be) {
        const e3 = this._dot(s2.data, r2.data);
        return new Be(e3 < 0 ? Array.from(n2.data) : n2.data.map((e4) => -e4), n2.typeInfo);
      }
      return console.error(`FaceForward() expects vector arguments. Line ${e2.line}`), null;
    }
    _firstLeadingBit(e2) {
      return 0 === e2 ? -1 : 31 - Math.clz32(e2);
    }
    FirstLeadingBit(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => this._firstLeadingBit(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(this._firstLeadingBit(s2.value), n2.typeInfo);
    }
    _firstTrailingBit(e2) {
      return 0 === e2 ? -1 : Math.log2(e2 & -e2);
    }
    FirstTrailingBit(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => this._firstTrailingBit(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(this._firstTrailingBit(s2.value), n2.typeInfo);
    }
    Floor(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.floor(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.floor(s2.value), n2.typeInfo);
    }
    Fma(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2), r2 = this.exec.evalExpression(e2.args[2], t2);
      if (n2 instanceof Be && s2 instanceof Be && r2 instanceof Be)
        return n2.data.length !== s2.data.length || n2.data.length !== r2.data.length ? (console.error(`Fma() expects vectors of the same length. Line ${e2.line}`), null) : new Be(n2.data.map((e3, t3) => e3 * s2.data[t3] + r2.data[t3]), n2.typeInfo);
      const a2 = n2, i2 = s2, o2 = r2;
      return new Ve(a2.value * i2.value + o2.value, a2.typeInfo);
    }
    Fract(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => e3 - Math.floor(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(s2.value - Math.floor(s2.value), n2.typeInfo);
    }
    Frexp(e2, t2) {
      return console.error(`TODO: frexp. Line ${e2.line}`), null;
    }
    InsertBits(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2), r2 = this.exec.evalExpression(e2.args[2], t2), a2 = this.exec.evalExpression(e2.args[3], t2);
      if ("u32" !== r2.typeInfo.name && "x32" !== r2.typeInfo.name)
        return console.error(`InsertBits() expects an i32 offset argument. Line ${e2.line}`), null;
      const i2 = r2.value, o2 = (1 << a2.value) - 1 << i2, l2 = ~o2;
      if (n2 instanceof Be && s2 instanceof Be)
        return new Be(n2.data.map((e3, t3) => e3 & l2 | s2.data[t3] << i2 & o2), n2.typeInfo);
      const c2 = n2.value, u2 = s2.value;
      return new Ve(c2 & l2 | u2 << i2 & o2, n2.typeInfo);
    }
    InverseSqrt(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => 1 / Math.sqrt(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(1 / Math.sqrt(s2.value), n2.typeInfo);
    }
    Ldexp(e2, t2) {
      return console.error(`TODO: ldexp. Line ${e2.line}`), null;
    }
    Length(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be) {
        let e3 = 0;
        return n2.data.forEach((t3) => {
          e3 += t3 * t3;
        }), new Ve(Math.sqrt(e3), this.getTypeInfo("f32"));
      }
      const s2 = n2;
      return new Ve(Math.abs(s2.value), n2.typeInfo);
    }
    Log(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.log(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.log(s2.value), n2.typeInfo);
    }
    Log2(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.log2(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.log2(s2.value), n2.typeInfo);
    }
    Max(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2);
      if (n2 instanceof Be && s2 instanceof Be)
        return new Be(n2.data.map((e3, t3) => Math.max(e3, s2.data[t3])), n2.typeInfo);
      const r2 = n2, a2 = s2;
      return new Ve(Math.max(r2.value, a2.value), n2.typeInfo);
    }
    Min(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2);
      if (n2 instanceof Be && s2 instanceof Be)
        return new Be(n2.data.map((e3, t3) => Math.min(e3, s2.data[t3])), n2.typeInfo);
      const r2 = n2, a2 = s2;
      return new Ve(Math.min(r2.value, a2.value), n2.typeInfo);
    }
    Mix(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2), r2 = this.exec.evalExpression(e2.args[2], t2);
      if (n2 instanceof Be && s2 instanceof Be && r2 instanceof Be)
        return new Be(n2.data.map((e3, t3) => n2.data[t3] * (1 - r2.data[t3]) + s2.data[t3] * r2.data[t3]), n2.typeInfo);
      const a2 = s2, i2 = r2;
      return new Ve(n2.value * (1 - i2.value) + a2.value * i2.value, n2.typeInfo);
    }
    Modf(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2);
      if (n2 instanceof Be && s2 instanceof Be)
        return new Be(n2.data.map((e3, t3) => e3 % s2.data[t3]), n2.typeInfo);
      const r2 = s2;
      return new Ve(n2.value % r2.value, n2.typeInfo);
    }
    Normalize(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be) {
        const s2 = this.Length(e2, t2).value;
        return new Be(n2.data.map((e3) => e3 / s2), n2.typeInfo);
      }
      return console.error(`Normalize() expects a vector argument. Line ${e2.line}`), null;
    }
    Pow(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2);
      if (n2 instanceof Be && s2 instanceof Be)
        return new Be(n2.data.map((e3, t3) => Math.pow(e3, s2.data[t3])), n2.typeInfo);
      const r2 = n2, a2 = s2;
      return new Ve(Math.pow(r2.value, a2.value), n2.typeInfo);
    }
    QuantizeToF16(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => e3), n2.typeInfo);
      return new Ve(n2.value, n2.typeInfo);
    }
    Radians(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => e3 * Math.PI / 180), n2.typeInfo);
      return new Ve(n2.value * Math.PI / 180, this.getTypeInfo("f32"));
    }
    Reflect(e2, t2) {
      let n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2);
      if (n2 instanceof Be && s2 instanceof Be) {
        const e3 = this._dot(n2.data, s2.data);
        return new Be(n2.data.map((t3, n3) => t3 - 2 * e3 * s2.data[n3]), n2.typeInfo);
      }
      return console.error(`Reflect() expects vector arguments. Line ${e2.line}`), null;
    }
    Refract(e2, t2) {
      let n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2), r2 = this.exec.evalExpression(e2.args[2], t2);
      if (n2 instanceof Be && s2 instanceof Be && r2 instanceof Ve) {
        const e3 = this._dot(s2.data, n2.data);
        return new Be(n2.data.map((t3, n3) => {
          const a2 = 1 - r2.value * r2.value * (1 - e3 * e3);
          if (a2 < 0)
            return 0;
          const i2 = Math.sqrt(a2);
          return r2.value * t3 - (r2.value * e3 + i2) * s2.data[n3];
        }), n2.typeInfo);
      }
      return console.error(`Refract() expects vector arguments and a scalar argument. Line ${e2.line}`), null;
    }
    ReverseBits(e2, t2) {
      return console.error(`TODO: reverseBits. Line ${e2.line}`), null;
    }
    Round(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.round(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.round(s2.value), n2.typeInfo);
    }
    Saturate(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.min(Math.max(e3, 0), 1)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.min(Math.max(s2.value, 0), 1), n2.typeInfo);
    }
    Sign(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.sign(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.sign(s2.value), n2.typeInfo);
    }
    Sin(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.sin(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.sin(s2.value), n2.typeInfo);
    }
    Sinh(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.sinh(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.sinh(s2.value), n2.typeInfo);
    }
    _smoothstep(e2, t2, n2) {
      const s2 = Math.min(Math.max((n2 - e2) / (t2 - e2), 0), 1);
      return s2 * s2 * (3 - 2 * s2);
    }
    SmoothStep(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2), r2 = this.exec.evalExpression(e2.args[2], t2);
      if (r2 instanceof Be && n2 instanceof Be && s2 instanceof Be)
        return new Be(r2.data.map((e3, t3) => this._smoothstep(n2.data[t3], s2.data[t3], e3)), r2.typeInfo);
      const a2 = n2, i2 = s2, o2 = r2;
      return new Ve(this._smoothstep(a2.value, i2.value, o2.value), r2.typeInfo);
    }
    Sqrt(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.sqrt(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.sqrt(s2.value), n2.typeInfo);
    }
    Step(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2), s2 = this.exec.evalExpression(e2.args[1], t2);
      if (s2 instanceof Be && n2 instanceof Be)
        return new Be(s2.data.map((e3, t3) => e3 < n2.data[t3] ? 0 : 1), s2.typeInfo);
      const r2 = n2;
      return new Ve(s2.value < r2.value ? 0 : 1, r2.typeInfo);
    }
    Tan(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.tan(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.tan(s2.value), n2.typeInfo);
    }
    Tanh(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.tanh(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.tanh(s2.value), n2.typeInfo);
    }
    _getTransposeType(e2) {
      const t2 = e2.getTypeName();
      return "mat2x2f" === t2 || "mat2x2h" === t2 ? e2 : "mat2x3f" === t2 ? this.getTypeInfo("mat3x2f") : "mat2x3h" === t2 ? this.getTypeInfo("mat3x2h") : "mat2x4f" === t2 ? this.getTypeInfo("mat4x2f") : "mat2x4h" === t2 ? this.getTypeInfo("mat4x2h") : "mat3x2f" === t2 ? this.getTypeInfo("mat2x3f") : "mat3x2h" === t2 ? this.getTypeInfo("mat2x3h") : "mat3x3f" === t2 || "mat3x3h" === t2 ? e2 : "mat3x4f" === t2 ? this.getTypeInfo("mat4x3f") : "mat3x4h" === t2 ? this.getTypeInfo("mat4x3h") : "mat4x2f" === t2 ? this.getTypeInfo("mat2x4f") : "mat4x2h" === t2 ? this.getTypeInfo("mat2x4h") : "mat4x3f" === t2 ? this.getTypeInfo("mat3x4f") : "mat4x3h" === t2 ? this.getTypeInfo("mat3x4h") : ("mat4x4f" === t2 || "mat4x4h" === t2 || console.error(`Invalid matrix type ${t2}`), e2);
    }
    Transpose(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (!(n2 instanceof Fe))
        return console.error(`Transpose() expects a matrix argument. Line ${e2.line}`), null;
      const s2 = this._getTransposeType(n2.typeInfo);
      if ("mat2x2" === n2.typeInfo.name || "mat2x2f" === n2.typeInfo.name || "mat2x2h" === n2.typeInfo.name) {
        const e3 = n2.data;
        return new Fe([e3[0], e3[2], e3[1], e3[3]], s2);
      }
      if ("mat2x3" === n2.typeInfo.name || "mat2x3f" === n2.typeInfo.name || "mat2x3h" === n2.typeInfo.name) {
        const e3 = n2.data;
        return new Fe([e3[0], e3[3], e3[6], e3[1], e3[4], e3[7]], s2);
      }
      if ("mat2x4" === n2.typeInfo.name || "mat2x4f" === n2.typeInfo.name || "mat2x4h" === n2.typeInfo.name) {
        const e3 = n2.data;
        return new Fe([e3[0], e3[4], e3[8], e3[12], e3[1], e3[5], e3[9], e3[13]], s2);
      }
      if ("mat3x2" === n2.typeInfo.name || "mat3x2f" === n2.typeInfo.name || "mat3x2h" === n2.typeInfo.name) {
        const e3 = n2.data;
        return new Fe([e3[0], e3[3], e3[1], e3[4], e3[2], e3[5]], s2);
      }
      if ("mat3x3" === n2.typeInfo.name || "mat3x3f" === n2.typeInfo.name || "mat3x3h" === n2.typeInfo.name) {
        const e3 = n2.data;
        return new Fe([e3[0], e3[3], e3[6], e3[1], e3[4], e3[7], e3[2], e3[5], e3[8]], s2);
      }
      if ("mat3x4" === n2.typeInfo.name || "mat3x4f" === n2.typeInfo.name || "mat3x4h" === n2.typeInfo.name) {
        const e3 = n2.data;
        return new Fe([e3[0], e3[4], e3[8], e3[12], e3[1], e3[5], e3[9], e3[13], e3[2], e3[6], e3[10], e3[14]], s2);
      }
      if ("mat4x2" === n2.typeInfo.name || "mat4x2f" === n2.typeInfo.name || "mat4x2h" === n2.typeInfo.name) {
        const e3 = n2.data;
        return new Fe([e3[0], e3[4], e3[1], e3[5], e3[2], e3[6]], s2);
      }
      if ("mat4x3" === n2.typeInfo.name || "mat4x3f" === n2.typeInfo.name || "mat4x3h" === n2.typeInfo.name) {
        const e3 = n2.data;
        return new Fe([e3[0], e3[4], e3[8], e3[1], e3[5], e3[9], e3[2], e3[6], e3[10]], s2);
      }
      if ("mat4x4" === n2.typeInfo.name || "mat4x4f" === n2.typeInfo.name || "mat4x4h" === n2.typeInfo.name) {
        const e3 = n2.data;
        return new Fe([e3[0], e3[4], e3[8], e3[12], e3[1], e3[5], e3[9], e3[13], e3[2], e3[6], e3[10], e3[14], e3[3], e3[7], e3[11], e3[15]], s2);
      }
      return console.error(`Invalid matrix type ${n2.typeInfo.name}`), null;
    }
    Trunc(e2, t2) {
      const n2 = this.exec.evalExpression(e2.args[0], t2);
      if (n2 instanceof Be)
        return new Be(n2.data.map((e3) => Math.trunc(e3)), n2.typeInfo);
      const s2 = n2;
      return new Ve(Math.trunc(s2.value), n2.typeInfo);
    }
    Dpdx(e2, t2) {
      return console.error(`TODO: dpdx. Line ${e2.line}`), null;
    }
    DpdxCoarse(e2, t2) {
      return console.error(`TODO: dpdxCoarse. Line ${e2.line}`), null;
    }
    DpdxFine(e2, t2) {
      return console.error("TODO: dpdxFine"), null;
    }
    Dpdy(e2, t2) {
      return console.error("TODO: dpdy"), null;
    }
    DpdyCoarse(e2, t2) {
      return console.error("TODO: dpdyCoarse"), null;
    }
    DpdyFine(e2, t2) {
      return console.error("TODO: dpdyFine"), null;
    }
    Fwidth(e2, t2) {
      return console.error("TODO: fwidth"), null;
    }
    FwidthCoarse(e2, t2) {
      return console.error("TODO: fwidthCoarse"), null;
    }
    FwidthFine(e2, t2) {
      return console.error("TODO: fwidthFine"), null;
    }
    TextureDimensions(e2, t2) {
      const n2 = e2.args[0], s2 = e2.args.length > 1 ? this.exec.evalExpression(e2.args[1], t2).value : 0;
      if (n2 instanceof de) {
        const r2 = n2.name, a2 = t2.getVariableValue(r2);
        if (a2 instanceof Ue) {
          if (s2 < 0 || s2 >= a2.mipLevelCount)
            return console.error(`Invalid mip level for textureDimensions. Line ${e2.line}`), null;
          const t3 = a2.getMipLevelSize(s2), n3 = a2.dimension;
          return "1d" === n3 ? new Ve(t3[0], this.getTypeInfo("u32")) : "3d" === n3 ? new Be(t3, this.getTypeInfo("vec3u")) : "2d" === n3 ? new Be(t3.slice(0, 2), this.getTypeInfo("vec2u")) : (console.error(`Invalid texture dimension ${n3} not found. Line ${e2.line}`), null);
        }
        return console.error(`Texture ${r2} not found. Line ${e2.line}`), null;
      }
      return console.error(`Invalid texture argument for textureDimensions. Line ${e2.line}`), null;
    }
    TextureGather(e2, t2) {
      return console.error("TODO: textureGather"), null;
    }
    TextureGatherCompare(e2, t2) {
      return console.error("TODO: textureGatherCompare"), null;
    }
    TextureLoad(e2, t2) {
      const n2 = e2.args[0], s2 = this.exec.evalExpression(e2.args[1], t2), r2 = e2.args.length > 2 ? this.exec.evalExpression(e2.args[2], t2).value : 0;
      if (!(s2 instanceof Be) || 2 !== s2.data.length)
        return console.error(`Invalid UV argument for textureLoad. Line ${e2.line}`), null;
      if (n2 instanceof de) {
        const a2 = n2.name, i2 = t2.getVariableValue(a2);
        if (i2 instanceof Ue) {
          const t3 = Math.floor(s2.data[0]), n3 = Math.floor(s2.data[1]);
          if (t3 < 0 || t3 >= i2.width || n3 < 0 || n3 >= i2.height)
            return console.error(`Texture ${a2} out of bounds. Line ${e2.line}`), null;
          const o2 = i2.getPixel(t3, n3, 0, r2);
          return null === o2 ? (console.error(`Invalid texture format for textureLoad. Line ${e2.line}`), null) : new Be(o2, this.getTypeInfo("vec4f"));
        }
        return console.error(`Texture ${a2} not found. Line ${e2.line}`), null;
      }
      return console.error(`Invalid texture argument for textureLoad. Line ${e2.line}`), null;
    }
    TextureNumLayers(e2, t2) {
      const n2 = e2.args[0];
      if (n2 instanceof de) {
        const s2 = n2.name, r2 = t2.getVariableValue(s2);
        return r2 instanceof Ue ? new Ve(r2.depthOrArrayLayers, this.getTypeInfo("u32")) : (console.error(`Texture ${s2} not found. Line ${e2.line}`), null);
      }
      return console.error(`Invalid texture argument for textureNumLayers. Line ${e2.line}`), null;
    }
    TextureNumLevels(e2, t2) {
      const n2 = e2.args[0];
      if (n2 instanceof de) {
        const s2 = n2.name, r2 = t2.getVariableValue(s2);
        return r2 instanceof Ue ? new Ve(r2.mipLevelCount, this.getTypeInfo("u32")) : (console.error(`Texture ${s2} not found. Line ${e2.line}`), null);
      }
      return console.error(`Invalid texture argument for textureNumLevels. Line ${e2.line}`), null;
    }
    TextureNumSamples(e2, t2) {
      const n2 = e2.args[0];
      if (n2 instanceof de) {
        const s2 = n2.name, r2 = t2.getVariableValue(s2);
        return r2 instanceof Ue ? new Ve(r2.sampleCount, this.getTypeInfo("u32")) : (console.error(`Texture ${s2} not found. Line ${e2.line}`), null);
      }
      return console.error(`Invalid texture argument for textureNumSamples. Line ${e2.line}`), null;
    }
    TextureSample(e2, t2) {
      return console.error("TODO: textureSample"), null;
    }
    TextureSampleBias(e2, t2) {
      return console.error("TODO: textureSampleBias"), null;
    }
    TextureSampleCompare(e2, t2) {
      return console.error("TODO: textureSampleCompare"), null;
    }
    TextureSampleCompareLevel(e2, t2) {
      return console.error("TODO: textureSampleCompareLevel"), null;
    }
    TextureSampleGrad(e2, t2) {
      return console.error("TODO: textureSampleGrad"), null;
    }
    TextureSampleLevel(e2, t2) {
      return console.error("TODO: textureSampleLevel"), null;
    }
    TextureSampleBaseClampToEdge(e2, t2) {
      return console.error("TODO: textureSampleBaseClampToEdge"), null;
    }
    TextureStore(e2, t2) {
      const n2 = e2.args[0], s2 = this.exec.evalExpression(e2.args[1], t2), r2 = 4 === e2.args.length ? this.exec.evalExpression(e2.args[2], t2).value : 0, a2 = 4 === e2.args.length ? this.exec.evalExpression(e2.args[3], t2).data : this.exec.evalExpression(e2.args[2], t2).data;
      if (4 !== a2.length)
        return console.error(`Invalid value argument for textureStore. Line ${e2.line}`), null;
      if (!(s2 instanceof Be) || 2 !== s2.data.length)
        return console.error(`Invalid UV argument for textureStore. Line ${e2.line}`), null;
      if (n2 instanceof de) {
        const i2 = n2.name, o2 = t2.getVariableValue(i2);
        if (o2 instanceof Ue) {
          const t3 = o2.getMipLevelSize(0), n3 = Math.floor(s2.data[0]), l2 = Math.floor(s2.data[1]);
          return n3 < 0 || n3 >= t3[0] || l2 < 0 || l2 >= t3[1] ? (console.error(`Texture ${i2} out of bounds. Line ${e2.line}`), null) : (o2.setPixel(n3, l2, 0, r2, Array.from(a2)), null);
        }
        return console.error(`Texture ${i2} not found. Line ${e2.line}`), null;
      }
      return console.error(`Invalid texture argument for textureStore. Line ${e2.line}`), null;
    }
    AtomicLoad(e2, t2) {
      let n2 = e2.args[0];
      n2 instanceof ve && (n2 = n2.right);
      const s2 = this.exec.getVariableName(n2, t2);
      return t2.getVariable(s2).value.getSubData(this.exec, n2.postfix, t2);
    }
    AtomicStore(e2, t2) {
      let n2 = e2.args[0];
      n2 instanceof ve && (n2 = n2.right);
      const s2 = this.exec.getVariableName(n2, t2), r2 = t2.getVariable(s2);
      let a2 = e2.args[1];
      const i2 = this.exec.evalExpression(a2, t2), o2 = r2.value.getSubData(this.exec, n2.postfix, t2);
      return o2 instanceof Ve && i2 instanceof Ve && (o2.value = i2.value), r2.value instanceof Me && r2.value.setDataValue(this.exec, o2, n2.postfix, t2), null;
    }
    AtomicAdd(e2, t2) {
      let n2 = e2.args[0];
      n2 instanceof ve && (n2 = n2.right);
      const s2 = this.exec.getVariableName(n2, t2), r2 = t2.getVariable(s2);
      let a2 = e2.args[1];
      const i2 = this.exec.evalExpression(a2, t2), o2 = r2.value.getSubData(this.exec, n2.postfix, t2), l2 = new Ve(o2.value, o2.typeInfo);
      return o2 instanceof Ve && i2 instanceof Ve && (o2.value += i2.value), r2.value instanceof Me && r2.value.setDataValue(this.exec, o2, n2.postfix, t2), l2;
    }
    AtomicSub(e2, t2) {
      let n2 = e2.args[0];
      n2 instanceof ve && (n2 = n2.right);
      const s2 = this.exec.getVariableName(n2, t2), r2 = t2.getVariable(s2);
      let a2 = e2.args[1];
      const i2 = this.exec.evalExpression(a2, t2), o2 = r2.value.getSubData(this.exec, n2.postfix, t2), l2 = new Ve(o2.value, o2.typeInfo);
      return o2 instanceof Ve && i2 instanceof Ve && (o2.value -= i2.value), r2.value instanceof Me && r2.value.setDataValue(this.exec, o2, n2.postfix, t2), l2;
    }
    AtomicMax(e2, t2) {
      let n2 = e2.args[0];
      n2 instanceof ve && (n2 = n2.right);
      const s2 = this.exec.getVariableName(n2, t2), r2 = t2.getVariable(s2);
      let a2 = e2.args[1];
      const i2 = this.exec.evalExpression(a2, t2), o2 = r2.value.getSubData(this.exec, n2.postfix, t2), l2 = new Ve(o2.value, o2.typeInfo);
      return o2 instanceof Ve && i2 instanceof Ve && (o2.value = Math.max(o2.value, i2.value)), r2.value instanceof Me && r2.value.setDataValue(this.exec, o2, n2.postfix, t2), l2;
    }
    AtomicMin(e2, t2) {
      let n2 = e2.args[0];
      n2 instanceof ve && (n2 = n2.right);
      const s2 = this.exec.getVariableName(n2, t2), r2 = t2.getVariable(s2);
      let a2 = e2.args[1];
      const i2 = this.exec.evalExpression(a2, t2), o2 = r2.value.getSubData(this.exec, n2.postfix, t2), l2 = new Ve(o2.value, o2.typeInfo);
      return o2 instanceof Ve && i2 instanceof Ve && (o2.value = Math.min(o2.value, i2.value)), r2.value instanceof Me && r2.value.setDataValue(this.exec, o2, n2.postfix, t2), l2;
    }
    AtomicAnd(e2, t2) {
      let n2 = e2.args[0];
      n2 instanceof ve && (n2 = n2.right);
      const s2 = this.exec.getVariableName(n2, t2), r2 = t2.getVariable(s2);
      let a2 = e2.args[1];
      const i2 = this.exec.evalExpression(a2, t2), o2 = r2.value.getSubData(this.exec, n2.postfix, t2), l2 = new Ve(o2.value, o2.typeInfo);
      return o2 instanceof Ve && i2 instanceof Ve && (o2.value = o2.value & i2.value), r2.value instanceof Me && r2.value.setDataValue(this.exec, o2, n2.postfix, t2), l2;
    }
    AtomicOr(e2, t2) {
      let n2 = e2.args[0];
      n2 instanceof ve && (n2 = n2.right);
      const s2 = this.exec.getVariableName(n2, t2), r2 = t2.getVariable(s2);
      let a2 = e2.args[1];
      const i2 = this.exec.evalExpression(a2, t2), o2 = r2.value.getSubData(this.exec, n2.postfix, t2), l2 = new Ve(o2.value, o2.typeInfo);
      return o2 instanceof Ve && i2 instanceof Ve && (o2.value = o2.value | i2.value), r2.value instanceof Me && r2.value.setDataValue(this.exec, o2, n2.postfix, t2), l2;
    }
    AtomicXor(e2, t2) {
      let n2 = e2.args[0];
      n2 instanceof ve && (n2 = n2.right);
      const s2 = this.exec.getVariableName(n2, t2), r2 = t2.getVariable(s2);
      let a2 = e2.args[1];
      const i2 = this.exec.evalExpression(a2, t2), o2 = r2.value.getSubData(this.exec, n2.postfix, t2), l2 = new Ve(o2.value, o2.typeInfo);
      return o2 instanceof Ve && i2 instanceof Ve && (o2.value = o2.value ^ i2.value), r2.value instanceof Me && r2.value.setDataValue(this.exec, o2, n2.postfix, t2), l2;
    }
    AtomicExchange(e2, t2) {
      let n2 = e2.args[0];
      n2 instanceof ve && (n2 = n2.right);
      const s2 = this.exec.getVariableName(n2, t2), r2 = t2.getVariable(s2);
      let a2 = e2.args[1];
      const i2 = this.exec.evalExpression(a2, t2), o2 = r2.value.getSubData(this.exec, n2.postfix, t2), l2 = new Ve(o2.value, o2.typeInfo);
      return o2 instanceof Ve && i2 instanceof Ve && (o2.value = i2.value), r2.value instanceof Me && r2.value.setDataValue(this.exec, o2, n2.postfix, t2), l2;
    }
    AtomicCompareExchangeWeak(e2, t2) {
      return console.error("TODO: atomicCompareExchangeWeak"), null;
    }
    Pack4x8snorm(e2, t2) {
      return console.error("TODO: pack4x8snorm"), null;
    }
    Pack4x8unorm(e2, t2) {
      return console.error("TODO: pack4x8unorm"), null;
    }
    Pack4xI8(e2, t2) {
      return console.error("TODO: pack4xI8"), null;
    }
    Pack4xU8(e2, t2) {
      return console.error("TODO: pack4xU8"), null;
    }
    Pack4x8Clamp(e2, t2) {
      return console.error("TODO: pack4x8Clamp"), null;
    }
    Pack4xU8Clamp(e2, t2) {
      return console.error("TODO: pack4xU8Clamp"), null;
    }
    Pack2x16snorm(e2, t2) {
      return console.error("TODO: pack2x16snorm"), null;
    }
    Pack2x16unorm(e2, t2) {
      return console.error("TODO: pack2x16unorm"), null;
    }
    Pack2x16float(e2, t2) {
      return console.error("TODO: pack2x16float"), null;
    }
    Unpack4x8snorm(e2, t2) {
      return console.error("TODO: unpack4x8snorm"), null;
    }
    Unpack4x8unorm(e2, t2) {
      return console.error("TODO: unpack4x8unorm"), null;
    }
    Unpack4xI8(e2, t2) {
      return console.error("TODO: unpack4xI8"), null;
    }
    Unpack4xU8(e2, t2) {
      return console.error("TODO: unpack4xU8"), null;
    }
    Unpack2x16snorm(e2, t2) {
      return console.error("TODO: unpack2x16snorm"), null;
    }
    Unpack2x16unorm(e2, t2) {
      return console.error("TODO: unpack2x16unorm"), null;
    }
    Unpack2x16float(e2, t2) {
      return console.error("TODO: unpack2x16float"), null;
    }
    StorageBarrier(e2, t2) {
      return null;
    }
    TextureBarrier(e2, t2) {
      return null;
    }
    WorkgroupBarrier(e2, t2) {
      return null;
    }
    WorkgroupUniformLoad(e2, t2) {
      return null;
    }
    SubgroupAdd(e2, t2) {
      return console.error("TODO: subgroupAdd"), null;
    }
    SubgroupExclusiveAdd(e2, t2) {
      return console.error("TODO: subgroupExclusiveAdd"), null;
    }
    SubgroupInclusiveAdd(e2, t2) {
      return console.error("TODO: subgroupInclusiveAdd"), null;
    }
    SubgroupAll(e2, t2) {
      return console.error("TODO: subgroupAll"), null;
    }
    SubgroupAnd(e2, t2) {
      return console.error("TODO: subgroupAnd"), null;
    }
    SubgroupAny(e2, t2) {
      return console.error("TODO: subgroupAny"), null;
    }
    SubgroupBallot(e2, t2) {
      return console.error("TODO: subgroupBallot"), null;
    }
    SubgroupBroadcast(e2, t2) {
      return console.error("TODO: subgroupBroadcast"), null;
    }
    SubgroupBroadcastFirst(e2, t2) {
      return console.error("TODO: subgroupBroadcastFirst"), null;
    }
    SubgroupElect(e2, t2) {
      return console.error("TODO: subgroupElect"), null;
    }
    SubgroupMax(e2, t2) {
      return console.error("TODO: subgroupMax"), null;
    }
    SubgroupMin(e2, t2) {
      return console.error("TODO: subgroupMin"), null;
    }
    SubgroupMul(e2, t2) {
      return console.error("TODO: subgroupMul"), null;
    }
    SubgroupExclusiveMul(e2, t2) {
      return console.error("TODO: subgroupExclusiveMul"), null;
    }
    SubgroupInclusiveMul(e2, t2) {
      return console.error("TODO: subgroupInclusiveMul"), null;
    }
    SubgroupOr(e2, t2) {
      return console.error("TODO: subgroupOr"), null;
    }
    SubgroupShuffle(e2, t2) {
      return console.error("TODO: subgroupShuffle"), null;
    }
    SubgroupShuffleDown(e2, t2) {
      return console.error("TODO: subgroupShuffleDown"), null;
    }
    SubgroupShuffleUp(e2, t2) {
      return console.error("TODO: subgroupShuffleUp"), null;
    }
    SubgroupShuffleXor(e2, t2) {
      return console.error("TODO: subgroupShuffleXor"), null;
    }
    SubgroupXor(e2, t2) {
      return console.error("TODO: subgroupXor"), null;
    }
    QuadBroadcast(e2, t2) {
      return console.error("TODO: quadBroadcast"), null;
    }
    QuadSwapDiagonal(e2, t2) {
      return console.error("TODO: quadSwapDiagonal"), null;
    }
    QuadSwapX(e2, t2) {
      return console.error("TODO: quadSwapX"), null;
    }
    QuadSwapY(e2, t2) {
      return console.error("TODO: quadSwapY"), null;
    }
  };
  var ct = { vec2: 2, vec2f: 2, vec2i: 2, vec2u: 2, vec2b: 2, vec2h: 2, vec3: 3, vec3f: 3, vec3i: 3, vec3u: 3, vec3b: 3, vec3h: 3, vec4: 4, vec4f: 4, vec4i: 4, vec4u: 4, vec4b: 4, vec4h: 4 };
  var ut = { mat2x2: [2, 2, 4], mat2x2f: [2, 2, 4], mat2x2h: [2, 2, 4], mat2x3: [2, 3, 6], mat2x3f: [2, 3, 6], mat2x3h: [2, 3, 6], mat2x4: [2, 4, 8], mat2x4f: [2, 4, 8], mat2x4h: [2, 4, 8], mat3x2: [3, 2, 6], mat3x2f: [3, 2, 6], mat3x2h: [3, 2, 6], mat3x3: [3, 3, 9], mat3x3f: [3, 3, 9], mat3x3h: [3, 3, 9], mat3x4: [3, 4, 12], mat3x4f: [3, 4, 12], mat3x4h: [3, 4, 12], mat4x2: [4, 2, 8], mat4x2f: [4, 2, 8], mat4x2h: [4, 2, 8], mat4x3: [4, 3, 12], mat4x3f: [4, 3, 12], mat4x3h: [4, 3, 12], mat4x4: [4, 4, 16], mat4x4f: [4, 4, 16], mat4x4h: [4, 4, 16] };
  var ht = class extends ot {
    constructor(e2, t2) {
      var n2;
      super(), this.ast = null != e2 ? e2 : [], this.reflection = new st(), this.reflection.updateAST(this.ast), this.context = null !== (n2 = null == t2 ? void 0 : t2.clone()) && void 0 !== n2 ? n2 : new it(), this.builtins = new lt(this), this.typeInfo = { bool: this.getTypeInfo(se.bool), i32: this.getTypeInfo(se.i32), u32: this.getTypeInfo(se.u32), f32: this.getTypeInfo(se.f32), f16: this.getTypeInfo(se.f16), vec2f: this.getTypeInfo(ie.vec2f), vec2u: this.getTypeInfo(ie.vec2u), vec2i: this.getTypeInfo(ie.vec2i), vec2h: this.getTypeInfo(ie.vec2h), vec3f: this.getTypeInfo(ie.vec3f), vec3u: this.getTypeInfo(ie.vec3u), vec3i: this.getTypeInfo(ie.vec3i), vec3h: this.getTypeInfo(ie.vec3h), vec4f: this.getTypeInfo(ie.vec4f), vec4u: this.getTypeInfo(ie.vec4u), vec4i: this.getTypeInfo(ie.vec4i), vec4h: this.getTypeInfo(ie.vec4h), mat2x2f: this.getTypeInfo(ie.mat2x2f), mat2x3f: this.getTypeInfo(ie.mat2x3f), mat2x4f: this.getTypeInfo(ie.mat2x4f), mat3x2f: this.getTypeInfo(ie.mat3x2f), mat3x3f: this.getTypeInfo(ie.mat3x3f), mat3x4f: this.getTypeInfo(ie.mat3x4f), mat4x2f: this.getTypeInfo(ie.mat4x2f), mat4x3f: this.getTypeInfo(ie.mat4x3f), mat4x4f: this.getTypeInfo(ie.mat4x4f) };
    }
    getVariableValue(e2) {
      var t2, n2;
      const r2 = null !== (n2 = null === (t2 = this.context.getVariable(e2)) || void 0 === t2 ? void 0 : t2.value) && void 0 !== n2 ? n2 : null;
      if (null === r2)
        return null;
      if (r2 instanceof Ve)
        return r2.value;
      if (r2 instanceof Be)
        return Array.from(r2.data);
      if (r2 instanceof Fe)
        return Array.from(r2.data);
      if (r2 instanceof Me && r2.typeInfo instanceof s) {
        if ("u32" === r2.typeInfo.format.name)
          return Array.from(new Uint32Array(r2.buffer, r2.offset, r2.typeInfo.count));
        if ("i32" === r2.typeInfo.format.name)
          return Array.from(new Int32Array(r2.buffer, r2.offset, r2.typeInfo.count));
        if ("f32" === r2.typeInfo.format.name)
          return Array.from(new Float32Array(r2.buffer, r2.offset, r2.typeInfo.count));
      }
      return console.error(`Unsupported return variable type ${r2.typeInfo.name}`), null;
    }
    execute(e2) {
      (e2 = null != e2 ? e2 : {}).constants && this._setOverrides(e2.constants, this.context), this._execStatements(this.ast, this.context);
    }
    dispatchWorkgroups(e2, t2, n2, s2) {
      const r2 = this.context.clone();
      (s2 = null != s2 ? s2 : {}).constants && this._setOverrides(s2.constants, r2), this._execStatements(this.ast, r2);
      const a2 = r2.getFunction(e2);
      if (!a2)
        return void console.error(`Function ${e2} not found`);
      if ("number" == typeof t2)
        t2 = [t2, 1, 1];
      else {
        if (0 === t2.length)
          return void console.error("Invalid dispatch count");
        1 === t2.length ? t2 = [t2[0], 1, 1] : 2 === t2.length ? t2 = [t2[0], t2[1], 1] : t2.length > 3 && (t2 = [t2[0], t2[1], t2[2]]);
      }
      const i2 = t2[0], o2 = t2[1], l2 = t2[2], c2 = this.getTypeInfo("vec3u");
      r2.setVariable("@num_workgroups", new Be(t2, c2));
      for (const e3 in n2)
        for (const t3 in n2[e3]) {
          const s3 = n2[e3][t3];
          r2.variables.forEach((n3) => {
            var r3;
            const a3 = n3.node;
            if (null == a3 ? void 0 : a3.attributes) {
              let i3 = null, o3 = null;
              for (const e4 of a3.attributes)
                "binding" === e4.name ? i3 = e4.value : "group" === e4.name && (o3 = e4.value);
              if (t3 == i3 && e3 == o3)
                if (void 0 !== s3.texture && void 0 !== s3.descriptor) {
                  const e4 = new Ue(s3.texture, this.getTypeInfo(a3.type), s3.descriptor, null !== (r3 = s3.texture.view) && void 0 !== r3 ? r3 : null);
                  n3.value = e4;
                } else
                  void 0 !== s3.uniform ? n3.value = new Me(s3.uniform, this.getTypeInfo(a3.type)) : n3.value = new Me(s3, this.getTypeInfo(a3.type));
            }
          });
        }
      for (let e3 = 0; e3 < l2; ++e3)
        for (let t3 = 0; t3 < o2; ++t3)
          for (let n3 = 0; n3 < i2; ++n3)
            r2.setVariable("@workgroup_id", new Be([n3, t3, e3], this.getTypeInfo("vec3u"))), this._dispatchWorkgroup(a2, [n3, t3, e3], r2);
    }
    execStatement(e2, t2) {
      if (e2 instanceof Z)
        return this.evalExpression(e2.value, t2);
      if (e2 instanceof te) {
        if (e2.condition) {
          const n2 = this.evalExpression(e2.condition, t2);
          if (!(n2 instanceof Ve))
            throw new Error("Invalid break-if condition");
          if (!n2.value)
            return null;
        }
        return ht._breakObj;
      }
      if (e2 instanceof ne)
        return ht._continueObj;
      if (e2 instanceof F)
        this._let(e2, t2);
      else if (e2 instanceof O)
        this._var(e2, t2);
      else if (e2 instanceof M)
        this._const(e2, t2);
      else if (e2 instanceof L)
        this._function(e2, t2);
      else {
        if (e2 instanceof j)
          return this._if(e2, t2);
        if (e2 instanceof X)
          return this._switch(e2, t2);
        if (e2 instanceof V)
          return this._for(e2, t2);
        if (e2 instanceof D)
          return this._while(e2, t2);
        if (e2 instanceof G)
          return this._loop(e2, t2);
        if (e2 instanceof N) {
          const n2 = t2.clone();
          return n2.currentFunctionName = t2.currentFunctionName, this._execStatements(e2.body, n2);
        }
        if (e2 instanceof z)
          this._assign(e2, t2);
        else if (e2 instanceof H)
          this._increment(e2, t2);
        else {
          if (e2 instanceof ae)
            return null;
          if (e2 instanceof B) {
            const n2 = e2.name;
            null === t2.getVariable(n2) && t2.setVariable(n2, new Ve(0, this.getTypeInfo("u32")));
          } else if (e2 instanceof R)
            this._call(e2, t2);
          else {
            if (e2 instanceof K)
              return null;
            if (e2 instanceof J)
              return null;
            console.error("Invalid statement type.", e2, `Line ${e2.line}`);
          }
        }
      }
      return null;
    }
    evalExpression(e2, t2) {
      return e2 instanceof we ? this._evalBinaryOp(e2, t2) : e2 instanceof _e ? this._evalLiteral(e2, t2) : e2 instanceof de ? this._evalVariable(e2, t2) : e2 instanceof pe ? this._evalCall(e2, t2) : e2 instanceof fe ? this._evalCreate(e2, t2) : e2 instanceof me ? this._evalConst(e2, t2) : e2 instanceof ge ? this._evalBitcast(e2, t2) : e2 instanceof ve ? this._evalUnaryOp(e2, t2) : (console.error("Invalid expression type", e2, `Line ${e2.line}`), null);
    }
    getTypeInfo(e2) {
      var t2;
      if (e2 instanceof se) {
        const t3 = this.reflection.getTypeInfo(e2);
        if (null !== t3)
          return t3;
      }
      let n2 = null !== (t2 = this.typeInfo[e2]) && void 0 !== t2 ? t2 : null;
      return null !== n2 || (n2 = this.reflection.getTypeInfoByName(e2)), n2;
    }
    _setOverrides(e2, t2) {
      for (const n2 in e2) {
        const s2 = e2[n2], r2 = this.reflection.getOverrideInfo(n2);
        null !== r2 ? (null === r2.type && (r2.type = this.getTypeInfo("u32")), "u32" === r2.type.name || "i32" === r2.type.name || "f32" === r2.type.name || "f16" === r2.type.name ? t2.setVariable(n2, new Ve(s2, r2.type)) : "bool" === r2.type.name ? t2.setVariable(n2, new Ve(s2 ? 1 : 0, r2.type)) : "vec2" === r2.type.name || "vec3" === r2.type.name || "vec4" === r2.type.name || "vec2f" === r2.type.name || "vec3f" === r2.type.name || "vec4f" === r2.type.name || "vec2i" === r2.type.name || "vec3i" === r2.type.name || "vec4i" === r2.type.name || "vec2u" === r2.type.name || "vec3u" === r2.type.name || "vec4u" === r2.type.name || "vec2h" === r2.type.name || "vec3h" === r2.type.name || "vec4h" === r2.type.name ? t2.setVariable(n2, new Be(s2, r2.type)) : console.error(`Invalid constant type for ${n2}`)) : console.error(`Override ${n2} does not exist in the shader.`);
      }
    }
    _dispatchWorkgroup(e2, t2, n2) {
      const s2 = [1, 1, 1];
      for (const t3 of e2.node.attributes)
        if ("workgroup_size" === t3.name) {
          if (t3.value.length > 0) {
            const e3 = n2.getVariableValue(t3.value[0]);
            s2[0] = e3 instanceof Ve ? e3.value : parseInt(t3.value[0]);
          }
          if (t3.value.length > 1) {
            const e3 = n2.getVariableValue(t3.value[1]);
            s2[1] = e3 instanceof Ve ? e3.value : parseInt(t3.value[1]);
          }
          if (t3.value.length > 2) {
            const e3 = n2.getVariableValue(t3.value[2]);
            s2[2] = e3 instanceof Ve ? e3.value : parseInt(t3.value[2]);
          }
        }
      const r2 = this.getTypeInfo("vec3u"), a2 = this.getTypeInfo("u32");
      n2.setVariable("@workgroup_size", new Be(s2, r2));
      const i2 = s2[0], o2 = s2[1], l2 = s2[2];
      for (let c2 = 0, u2 = 0; c2 < l2; ++c2)
        for (let l3 = 0; l3 < o2; ++l3)
          for (let o3 = 0; o3 < i2; ++o3, ++u2) {
            const i3 = [o3, l3, c2], h2 = [o3 + t2[0] * s2[0], l3 + t2[1] * s2[1], c2 + t2[2] * s2[2]];
            n2.setVariable("@local_invocation_id", new Be(i3, r2)), n2.setVariable("@global_invocation_id", new Be(h2, r2)), n2.setVariable("@local_invocation_index", new Ve(u2, a2)), this._dispatchExec(e2, n2);
          }
    }
    _dispatchExec(e2, t2) {
      for (const n2 of e2.node.args)
        for (const e3 of n2.attributes)
          if ("builtin" === e3.name) {
            const s2 = `@${e3.value}`, r2 = t2.getVariable(s2);
            void 0 !== r2 && t2.variables.set(n2.name, r2);
          }
      this._execStatements(e2.node.body, t2);
    }
    getVariableName(e2, t2) {
      for (; e2 instanceof ve; )
        e2 = e2.right;
      return e2 instanceof de ? e2.name : (console.error("Unknown variable type", e2, "Line", e2.line), null);
    }
    _execStatements(e2, t2) {
      for (const n2 of e2) {
        if (n2 instanceof Array) {
          const e4 = t2.clone(), s2 = this._execStatements(n2, e4);
          if (s2)
            return s2;
          continue;
        }
        const e3 = this.execStatement(n2, t2);
        if (e3)
          return e3;
      }
      return null;
    }
    _call(e2, t2) {
      const n2 = t2.clone();
      n2.currentFunctionName = e2.name;
      const s2 = t2.getFunction(e2.name);
      if (s2) {
        for (let t3 = 0; t3 < s2.node.args.length; ++t3) {
          const r2 = s2.node.args[t3], a2 = this.evalExpression(e2.args[t3], n2);
          n2.setVariable(r2.name, a2, r2);
        }
        this._execStatements(s2.node.body, n2);
      } else if (e2.isBuiltin)
        this._callBuiltinFunction(e2, n2);
      else {
        this.getTypeInfo(e2.name) && this._evalCreate(e2, t2);
      }
    }
    _increment(e2, t2) {
      const n2 = this.getVariableName(e2.variable, t2), s2 = t2.getVariable(n2);
      s2 ? "++" === e2.operator ? s2.value instanceof Ve ? s2.value.value++ : console.error(`Variable ${n2} is not a scalar. Line ${e2.line}`) : "--" === e2.operator ? s2.value instanceof Ve ? s2.value.value-- : console.error(`Variable ${n2} is not a scalar. Line ${e2.line}`) : console.error(`Unknown increment operator ${e2.operator}. Line ${e2.line}`) : console.error(`Variable ${n2} not found. Line ${e2.line}`);
    }
    _getVariableData(e2, t2) {
      if (e2 instanceof de) {
        const n2 = this.getVariableName(e2, t2), s2 = t2.getVariable(n2);
        return null === s2 ? (console.error(`Variable ${n2} not found. Line ${e2.line}`), null) : s2.value.getSubData(this, e2.postfix, t2);
      }
      if (e2 instanceof ve) {
        if ("*" === e2.operator) {
          const n2 = this._getVariableData(e2.right, t2);
          return n2 instanceof Ne ? n2.reference.getSubData(this, e2.postfix, t2) : (console.error(`Variable ${e2.right} is not a pointer. Line ${e2.line}`), null);
        }
        if ("&" === e2.operator) {
          const n2 = this._getVariableData(e2.right, t2);
          return new Ne(n2);
        }
      }
      return null;
    }
    _assign(e2, t2) {
      let n2 = null, s2 = "<var>", r2 = null;
      if (e2.variable instanceof ve) {
        const n3 = this._getVariableData(e2.variable, t2), s3 = this.evalExpression(e2.value, t2), r3 = e2.operator;
        if ("=" === r3) {
          if (n3 instanceof Ve || n3 instanceof Be || n3 instanceof Fe) {
            if (s3 instanceof Ve || s3 instanceof Be || s3 instanceof Fe && n3.data.length === s3.data.length)
              return void n3.data.set(s3.data);
            console.error(`Invalid assignment. Line ${e2.line}`);
          } else if (n3 instanceof Me && s3 instanceof Me && n3.buffer.byteLength - n3.offset >= s3.buffer.byteLength - s3.offset)
            return void (n3.buffer.byteLength % 4 == 0 ? new Uint32Array(n3.buffer, n3.offset, n3.typeInfo.size / 4).set(new Uint32Array(s3.buffer, s3.offset, s3.typeInfo.size / 4)) : new Uint8Array(n3.buffer, n3.offset, n3.typeInfo.size).set(new Uint8Array(s3.buffer, s3.offset, s3.typeInfo.size)));
          return console.error(`Invalid assignment. Line ${e2.line}`), null;
        }
        if ("+=" === r3)
          return n3 instanceof Ve || n3 instanceof Be || n3 instanceof Fe ? s3 instanceof Ve || s3 instanceof Be || s3 instanceof Fe ? void n3.data.set(s3.data.map((e3, t3) => n3.data[t3] + e3)) : void console.error(`Invalid assignment . Line ${e2.line}`) : void console.error(`Invalid assignment. Line ${e2.line}`);
        if ("-=" === r3)
          return (n3 instanceof Ve || n3 instanceof Be || n3 instanceof Fe) && (s3 instanceof Ve || s3 instanceof Be || s3 instanceof Fe) ? void n3.data.set(s3.data.map((e3, t3) => n3.data[t3] - e3)) : void console.error(`Invalid assignment. Line ${e2.line}`);
      }
      if (e2.variable instanceof ve) {
        if ("*" === e2.variable.operator) {
          s2 = this.getVariableName(e2.variable.right, t2);
          const r3 = t2.getVariable(s2);
          if (!(r3 && r3.value instanceof Ne))
            return void console.error(`Variable ${s2} is not a pointer. Line ${e2.line}`);
          n2 = r3.value.reference;
          let a3 = e2.variable.postfix;
          if (!a3) {
            let t3 = e2.variable.right;
            for (; t3 instanceof ve; ) {
              if (t3.postfix) {
                a3 = t3.postfix;
                break;
              }
              t3 = t3.right;
            }
          }
          a3 && (n2 = n2.getSubData(this, a3, t2));
        }
      } else {
        r2 = e2.variable.postfix, s2 = this.getVariableName(e2.variable, t2);
        const a3 = t2.getVariable(s2);
        if (null === a3)
          return void console.error(`Variable ${s2} not found. Line ${e2.line}`);
        n2 = a3.value;
      }
      if (n2 instanceof Ne && (n2 = n2.reference), null === n2)
        return void console.error(`Variable ${s2} not found. Line ${e2.line}`);
      const a2 = this.evalExpression(e2.value, t2), i2 = e2.operator;
      if ("=" === i2)
        if (n2 instanceof Me)
          n2.setDataValue(this, a2, r2, t2);
        else if (r2) {
          if (!(n2 instanceof Be || n2 instanceof Fe))
            return void console.error(`Variable ${s2} is not a vector or matrix. Line ${e2.line}`);
          if (r2 instanceof ye) {
            const i3 = this.evalExpression(r2.index, t2).value;
            if (n2 instanceof Be) {
              if (!(a2 instanceof Ve))
                return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
              n2.data[i3] = a2.value;
            } else {
              if (!(n2 instanceof Fe))
                return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
              {
                const i4 = this.evalExpression(r2.index, t2).value;
                if (i4 < 0)
                  return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
                if (!(a2 instanceof Be))
                  return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
                {
                  const t3 = n2.typeInfo.getTypeName();
                  if ("mat2x2" === t3 || "mat2x2f" === t3 || "mat2x2h" === t3) {
                    if (!(i4 < 2 && 2 === a2.data.length))
                      return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
                    n2.data[2 * i4] = a2.data[0], n2.data[2 * i4 + 1] = a2.data[1];
                  } else if ("mat2x3" === t3 || "mat2x3f" === t3 || "mat2x3h" === t3) {
                    if (!(i4 < 2 && 3 === a2.data.length))
                      return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
                    n2.data[3 * i4] = a2.data[0], n2.data[3 * i4 + 1] = a2.data[1], n2.data[3 * i4 + 2] = a2.data[2];
                  } else if ("mat2x4" === t3 || "mat2x4f" === t3 || "mat2x4h" === t3) {
                    if (!(i4 < 2 && 4 === a2.data.length))
                      return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
                    n2.data[4 * i4] = a2.data[0], n2.data[4 * i4 + 1] = a2.data[1], n2.data[4 * i4 + 2] = a2.data[2], n2.data[4 * i4 + 3] = a2.data[3];
                  } else if ("mat3x2" === t3 || "mat3x2f" === t3 || "mat3x2h" === t3) {
                    if (!(i4 < 3 && 2 === a2.data.length))
                      return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
                    n2.data[2 * i4] = a2.data[0], n2.data[2 * i4 + 1] = a2.data[1];
                  } else if ("mat3x3" === t3 || "mat3x3f" === t3 || "mat3x3h" === t3) {
                    if (!(i4 < 3 && 3 === a2.data.length))
                      return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
                    n2.data[3 * i4] = a2.data[0], n2.data[3 * i4 + 1] = a2.data[1], n2.data[3 * i4 + 2] = a2.data[2];
                  } else if ("mat3x4" === t3 || "mat3x4f" === t3 || "mat3x4h" === t3) {
                    if (!(i4 < 3 && 4 === a2.data.length))
                      return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
                    n2.data[4 * i4] = a2.data[0], n2.data[4 * i4 + 1] = a2.data[1], n2.data[4 * i4 + 2] = a2.data[2], n2.data[4 * i4 + 3] = a2.data[3];
                  } else if ("mat4x2" === t3 || "mat4x2f" === t3 || "mat4x2h" === t3) {
                    if (!(i4 < 4 && 2 === a2.data.length))
                      return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
                    n2.data[2 * i4] = a2.data[0], n2.data[2 * i4 + 1] = a2.data[1];
                  } else if ("mat4x3" === t3 || "mat4x3f" === t3 || "mat4x3h" === t3) {
                    if (!(i4 < 4 && 3 === a2.data.length))
                      return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
                    n2.data[3 * i4] = a2.data[0], n2.data[3 * i4 + 1] = a2.data[1], n2.data[3 * i4 + 2] = a2.data[2];
                  } else {
                    if ("mat4x4" !== t3 && "mat4x4f" !== t3 && "mat4x4h" !== t3)
                      return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
                    if (!(i4 < 4 && 4 === a2.data.length))
                      return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
                    n2.data[4 * i4] = a2.data[0], n2.data[4 * i4 + 1] = a2.data[1], n2.data[4 * i4 + 2] = a2.data[2], n2.data[4 * i4 + 3] = a2.data[3];
                  }
                }
              }
            }
          } else if (r2 instanceof he) {
            const t3 = r2.value;
            if (!(n2 instanceof Be))
              return void console.error(`Invalid assignment to ${t3}. Variable ${s2} is not a vector. Line ${e2.line}`);
            if (a2 instanceof Ve) {
              if (t3.length > 1)
                return void console.error(`Invalid assignment to ${t3} for variable ${s2}. Line ${e2.line}`);
              if ("x" === t3)
                n2.data[0] = a2.value;
              else if ("y" === t3) {
                if (n2.data.length < 2)
                  return void console.error(`Invalid assignment to ${t3} for variable ${s2}. Line ${e2.line}`);
                n2.data[1] = a2.value;
              } else if ("z" === t3) {
                if (n2.data.length < 3)
                  return void console.error(`Invalid assignment to ${t3} for variable ${s2}. Line ${e2.line}`);
                n2.data[2] = a2.value;
              } else if ("w" === t3) {
                if (n2.data.length < 4)
                  return void console.error(`Invalid assignment to ${t3} for variable ${s2}. Line ${e2.line}`);
                n2.data[3] = a2.value;
              }
            } else {
              if (!(a2 instanceof Be))
                return void console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
              if (t3.length !== a2.data.length)
                return void console.error(`Invalid assignment to ${t3} for variable ${s2}. Line ${e2.line}`);
              for (let r3 = 0; r3 < t3.length; ++r3) {
                const i3 = t3[r3];
                if ("x" === i3 || "r" === i3)
                  n2.data[0] = a2.data[r3];
                else if ("y" === i3 || "g" === i3) {
                  if (a2.data.length < 2)
                    return void console.error(`Invalid assignment to ${i3} for variable ${s2}. Line ${e2.line}`);
                  n2.data[1] = a2.data[r3];
                } else if ("z" === i3 || "b" === i3) {
                  if (a2.data.length < 3)
                    return void console.error(`Invalid assignment to ${i3} for variable ${s2}. Line ${e2.line}`);
                  n2.data[2] = a2.data[r3];
                } else {
                  if ("w" !== i3 && "a" !== i3)
                    return void console.error(`Invalid assignment to ${i3} for variable ${s2}. Line ${e2.line}`);
                  if (a2.data.length < 4)
                    return void console.error(`Invalid assignment to ${i3} for variable ${s2}. Line ${e2.line}`);
                  n2.data[3] = a2.data[r3];
                }
              }
            }
          }
        } else
          n2 instanceof Ve && a2 instanceof Ve ? n2.value = a2.value : n2 instanceof Be && a2 instanceof Be || n2 instanceof Fe && a2 instanceof Fe ? n2.data.set(a2.data) : console.error(`Invalid assignment to ${s2}. Line ${e2.line}`);
      else {
        const s3 = n2.getSubData(this, r2, t2);
        if (s3 instanceof Be && a2 instanceof Ve) {
          const t3 = s3.data, n3 = a2.value;
          if ("+=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] += n3;
          else if ("-=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] -= n3;
          else if ("*=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] *= n3;
          else if ("/=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] /= n3;
          else if ("%=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] %= n3;
          else if ("&=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] &= n3;
          else if ("|=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] |= n3;
          else if ("^=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] ^= n3;
          else if ("<<=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] <<= n3;
          else if (">>=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] >>= n3;
          else
            console.error(`Invalid operator ${i2}. Line ${e2.line}`);
        } else if (s3 instanceof Be && a2 instanceof Be) {
          const t3 = s3.data, n3 = a2.data;
          if (t3.length !== n3.length)
            return void console.error(`Vector length mismatch. Line ${e2.line}`);
          if ("+=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] += n3[e3];
          else if ("-=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] -= n3[e3];
          else if ("*=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] *= n3[e3];
          else if ("/=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] /= n3[e3];
          else if ("%=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] %= n3[e3];
          else if ("&=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] &= n3[e3];
          else if ("|=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] |= n3[e3];
          else if ("^=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] ^= n3[e3];
          else if ("<<=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] <<= n3[e3];
          else if (">>=" === i2)
            for (let e3 = 0; e3 < t3.length; ++e3)
              t3[e3] >>= n3[e3];
          else
            console.error(`Invalid operator ${i2}. Line ${e2.line}`);
        } else {
          if (!(s3 instanceof Ve && a2 instanceof Ve))
            return void console.error(`Invalid type for ${e2.operator} operator. Line ${e2.line}`);
          "+=" === i2 ? s3.value += a2.value : "-=" === i2 ? s3.value -= a2.value : "*=" === i2 ? s3.value *= a2.value : "/=" === i2 ? s3.value /= a2.value : "%=" === i2 ? s3.value %= a2.value : "&=" === i2 ? s3.value &= a2.value : "|=" === i2 ? s3.value |= a2.value : "^=" === i2 ? s3.value ^= a2.value : "<<=" === i2 ? s3.value <<= a2.value : ">>=" === i2 ? s3.value >>= a2.value : console.error(`Invalid operator ${i2}. Line ${e2.line}`);
        }
        n2 instanceof Me && n2.setDataValue(this, s3, r2, t2);
      }
    }
    _function(e2, t2) {
      const n2 = new at(e2);
      t2.functions.set(e2.name, n2);
    }
    _const(e2, t2) {
      let n2 = null;
      null !== e2.value && (n2 = this.evalExpression(e2.value, t2)), t2.createVariable(e2.name, n2, e2);
    }
    _let(e2, t2) {
      let n2 = null;
      if (null !== e2.value) {
        if (n2 = this.evalExpression(e2.value, t2), null === n2)
          return void console.error(`Invalid value for variable ${e2.name}. Line ${e2.line}`);
        e2.value instanceof ve || (n2 = n2.clone());
      } else {
        const s2 = e2.type.name;
        if ("f32" === s2 || "i32" === s2 || "u32" === s2 || "bool" === s2 || "f16" === s2 || "vec2" === s2 || "vec3" === s2 || "vec4" === s2 || "vec2f" === s2 || "vec3f" === s2 || "vec4f" === s2 || "vec2i" === s2 || "vec3i" === s2 || "vec4i" === s2 || "vec2u" === s2 || "vec3u" === s2 || "vec4u" === s2 || "vec2h" === s2 || "vec3h" === s2 || "vec4h" === s2 || "vec2b" === s2 || "vec3b" === s2 || "vec4b" === s2 || "mat2x2" === s2 || "mat2x3" === s2 || "mat2x4" === s2 || "mat3x2" === s2 || "mat3x3" === s2 || "mat3x4" === s2 || "mat4x2" === s2 || "mat4x3" === s2 || "mat4x4" === s2 || "mat2x2f" === s2 || "mat2x3f" === s2 || "mat2x4f" === s2 || "mat3x2f" === s2 || "mat3x3f" === s2 || "mat3x4f" === s2 || "mat4x2f" === s2 || "mat4x3f" === s2 || "mat4x4f" === s2 || "mat2x2h" === s2 || "mat2x3h" === s2 || "mat2x4h" === s2 || "mat3x2h" === s2 || "mat3x3h" === s2 || "mat3x4h" === s2 || "mat4x2h" === s2 || "mat4x3h" === s2 || "mat4x4h" === s2 || "array" === s2) {
          const s3 = new fe(e2.type, []);
          n2 = this._evalCreate(s3, t2);
        }
      }
      t2.createVariable(e2.name, n2, e2);
    }
    _var(e2, t2) {
      let n2 = null;
      if (null !== e2.value) {
        if (n2 = this.evalExpression(e2.value, t2), null === n2)
          return void console.error(`Invalid value for variable ${e2.name}. Line ${e2.line}`);
        e2.value instanceof ve || (n2 = n2.clone());
      } else {
        if (null === e2.type)
          return void console.error(`Variable ${e2.name} has no type. Line ${e2.line}`);
        const s2 = e2.type.name;
        if ("f32" === s2 || "i32" === s2 || "u32" === s2 || "bool" === s2 || "f16" === s2 || "vec2" === s2 || "vec3" === s2 || "vec4" === s2 || "vec2f" === s2 || "vec3f" === s2 || "vec4f" === s2 || "vec2i" === s2 || "vec3i" === s2 || "vec4i" === s2 || "vec2u" === s2 || "vec3u" === s2 || "vec4u" === s2 || "vec2h" === s2 || "vec3h" === s2 || "vec4h" === s2 || "vec2b" === s2 || "vec3b" === s2 || "vec4b" === s2 || "mat2x2" === s2 || "mat2x3" === s2 || "mat2x4" === s2 || "mat3x2" === s2 || "mat3x3" === s2 || "mat3x4" === s2 || "mat4x2" === s2 || "mat4x3" === s2 || "mat4x4" === s2 || "mat2x2f" === s2 || "mat2x3f" === s2 || "mat2x4f" === s2 || "mat3x2f" === s2 || "mat3x3f" === s2 || "mat3x4f" === s2 || "mat4x2f" === s2 || "mat4x3f" === s2 || "mat4x4f" === s2 || "mat2x2h" === s2 || "mat2x3h" === s2 || "mat2x4h" === s2 || "mat3x2h" === s2 || "mat3x3h" === s2 || "mat3x4h" === s2 || "mat4x2h" === s2 || "mat4x3h" === s2 || "mat4x4h" === s2 || e2.type instanceof le || e2.type instanceof ae || e2.type instanceof ie) {
          const s3 = new fe(e2.type, []);
          n2 = this._evalCreate(s3, t2);
        }
      }
      t2.createVariable(e2.name, n2, e2);
    }
    _switch(e2, t2) {
      t2 = t2.clone();
      const n2 = this.evalExpression(e2.condition, t2);
      if (!(n2 instanceof Ve))
        return console.error(`Invalid if condition. Line ${e2.line}`), null;
      let s2 = null;
      for (const r2 of e2.cases)
        if (r2 instanceof Te)
          for (const a2 of r2.selectors) {
            if (a2 instanceof Ie) {
              s2 = r2;
              continue;
            }
            const i2 = this.evalExpression(a2, t2);
            if (!(i2 instanceof Ve))
              return console.error(`Invalid case selector. Line ${e2.line}`), null;
            if (i2.value === n2.value)
              return this._execStatements(r2.body, t2);
          }
        else
          r2 instanceof Se && (s2 = r2);
      return s2 ? this._execStatements(s2.body, t2) : null;
    }
    _if(e2, t2) {
      t2 = t2.clone();
      const n2 = this.evalExpression(e2.condition, t2);
      if (!(n2 instanceof Ve))
        return console.error(`Invalid if condition. Line ${e2.line}`), null;
      if (n2.value)
        return this._execStatements(e2.body, t2);
      for (const n3 of e2.elseif) {
        const s2 = this.evalExpression(n3.condition, t2);
        if (!(s2 instanceof Ve))
          return console.error(`Invalid if condition. Line ${e2.line}`), null;
        if (s2.value)
          return this._execStatements(n3.body, t2);
      }
      return e2.else ? this._execStatements(e2.else, t2) : null;
    }
    _getScalarValue(e2) {
      return e2 instanceof Ve ? e2.value : (console.error("Expected scalar value.", e2), 0);
    }
    _for(e2, t2) {
      for (t2 = t2.clone(), this.execStatement(e2.init, t2); this._getScalarValue(this.evalExpression(e2.condition, t2)); ) {
        const n2 = this._execStatements(e2.body, t2);
        if (n2 === ht._breakObj)
          break;
        if (null !== n2 && n2 !== ht._continueObj)
          return n2;
        this.execStatement(e2.increment, t2);
      }
      return null;
    }
    _loop(e2, t2) {
      for (t2 = t2.clone(); ; ) {
        const n2 = this._execStatements(e2.body, t2);
        if (n2 === ht._breakObj)
          break;
        if (n2 === ht._continueObj) {
          if (e2.continuing) {
            if (this._execStatements(e2.continuing.body, t2) === ht._breakObj)
              break;
          }
        } else if (null !== n2)
          return n2;
      }
      return null;
    }
    _while(e2, t2) {
      for (t2 = t2.clone(); this._getScalarValue(this.evalExpression(e2.condition, t2)); ) {
        const n2 = this._execStatements(e2.body, t2);
        if (n2 === ht._breakObj)
          break;
        if (n2 !== ht._continueObj && null !== n2)
          return n2;
      }
      return null;
    }
    _evalBitcast(e2, t2) {
      const n2 = this.evalExpression(e2.value, t2), s2 = e2.type;
      if (n2 instanceof Ve) {
        const e3 = et(n2.value, n2.typeInfo.name, s2.name);
        return new Ve(e3, this.getTypeInfo(s2));
      }
      if (n2 instanceof Be) {
        const t3 = n2.typeInfo.getTypeName();
        let r2 = "";
        if (t3.endsWith("f"))
          r2 = "f32";
        else if (t3.endsWith("i"))
          r2 = "i32";
        else if (t3.endsWith("u"))
          r2 = "u32";
        else if (t3.endsWith("b"))
          r2 = "bool";
        else {
          if (!t3.endsWith("h"))
            return console.error(`Unknown vector type ${t3}. Line ${e2.line}`), null;
          r2 = "f16";
        }
        const a2 = s2.getTypeName();
        let i2 = "";
        if (a2.endsWith("f"))
          i2 = "f32";
        else if (a2.endsWith("i"))
          i2 = "i32";
        else if (a2.endsWith("u"))
          i2 = "u32";
        else if (a2.endsWith("b"))
          i2 = "bool";
        else {
          if (!a2.endsWith("h"))
            return console.error(`Unknown vector type ${i2}. Line ${e2.line}`), null;
          i2 = "f16";
        }
        const o2 = function(e3, t4, n3) {
          if (t4 === n3)
            return e3;
          const s3 = new Array(e3.length);
          for (let r3 = 0; r3 < e3.length; r3++)
            s3[r3] = et(e3[r3], t4, n3);
          return s3;
        }(Array.from(n2.data), r2, i2);
        return new Be(o2, this.getTypeInfo(s2));
      }
      return console.error(`TODO: bitcast for ${n2.typeInfo.name}. Line ${e2.line}`), null;
    }
    _evalConst(e2, t2) {
      return t2.getVariableValue(e2.name).clone().getSubData(this, e2.postfix, t2);
    }
    _evalCreate(e2, t2) {
      var r2;
      if (e2 instanceof fe) {
        if (null === e2.type)
          return De.void;
        switch (e2.type.getTypeName()) {
          case "bool":
          case "i32":
          case "u32":
          case "f32":
          case "f16":
            return this._callConstructorValue(e2, t2);
          case "vec2":
          case "vec3":
          case "vec4":
          case "vec2f":
          case "vec3f":
          case "vec4f":
          case "vec2h":
          case "vec3h":
          case "vec4h":
          case "vec2i":
          case "vec3i":
          case "vec4i":
          case "vec2u":
          case "vec3u":
          case "vec4u":
          case "vec2b":
          case "vec3b":
          case "vec4b":
            return this._callConstructorVec(e2, t2);
          case "mat2x2":
          case "mat2x2f":
          case "mat2x2h":
          case "mat2x3":
          case "mat2x3f":
          case "mat2x3h":
          case "mat2x4":
          case "mat2x4f":
          case "mat2x4h":
          case "mat3x2":
          case "mat3x2f":
          case "mat3x2h":
          case "mat3x3":
          case "mat3x3f":
          case "mat3x3h":
          case "mat3x4":
          case "mat3x4f":
          case "mat3x4h":
          case "mat4x2":
          case "mat4x2f":
          case "mat4x2h":
          case "mat4x3":
          case "mat4x3f":
          case "mat4x3h":
          case "mat4x4":
          case "mat4x4f":
          case "mat4x4h":
            return this._callConstructorMatrix(e2, t2);
        }
      }
      const a2 = e2 instanceof fe ? e2.type.name : e2.name, i2 = e2 instanceof fe ? this.getTypeInfo(e2.type) : this.getTypeInfo(e2.name);
      if (null === i2)
        return console.error(`Unknown type ${a2}. Line ${e2.line}`), null;
      if (0 === i2.size)
        return null;
      const o2 = new Me(new ArrayBuffer(i2.size), i2, 0);
      if (i2 instanceof n) {
        if (e2.args)
          for (let n2 = 0; n2 < e2.args.length; ++n2) {
            const s2 = i2.members[n2], r3 = e2.args[n2], a3 = this.evalExpression(r3, t2);
            o2.setData(this, a3, s2.type, s2.offset, t2);
          }
      } else if (i2 instanceof s) {
        let n2 = 0;
        if (e2.args)
          for (let s2 = 0; s2 < e2.args.length; ++s2) {
            const a3 = e2.args[s2], l2 = this.evalExpression(a3, t2);
            null === i2.format && ("x32" === (null === (r2 = l2.typeInfo) || void 0 === r2 ? void 0 : r2.name) ? i2.format = this.getTypeInfo("i32") : i2.format = l2.typeInfo), o2.setData(this, l2, i2.format, n2, t2), n2 += i2.stride;
          }
      } else
        console.error(`Unknown type "${a2}". Line ${e2.line}`);
      return e2 instanceof fe ? o2.getSubData(this, e2.postfix, t2) : o2;
    }
    _evalLiteral(e2, t2) {
      const n2 = this.getTypeInfo(e2.type), s2 = n2.name;
      if ("x32" === s2 || "u32" === s2 || "f32" === s2 || "f16" === s2 || "i32" === s2 || "bool" === s2) {
        return new Ve(e2.scalarValue, n2);
      }
      return "vec2" === s2 || "vec3" === s2 || "vec4" === s2 || "vec2f" === s2 || "vec3f" === s2 || "vec4f" === s2 || "vec2h" === s2 || "vec3h" === s2 || "vec4h" === s2 || "vec2i" === s2 || "vec3i" === s2 || "vec4i" === s2 || "vec2u" === s2 || "vec3u" === s2 || "vec4u" === s2 ? this._callConstructorVec(e2, t2) : "mat2x2" === s2 || "mat2x3" === s2 || "mat2x4" === s2 || "mat3x2" === s2 || "mat3x3" === s2 || "mat3x4" === s2 || "mat4x2" === s2 || "mat4x3" === s2 || "mat4x4" === s2 || "mat2x2f" === s2 || "mat2x3f" === s2 || "mat2x4f" === s2 || "mat3x2f" === s2 || "mat3x3f" === s2 || "mat3x4f" === s2 || "mat4x2f" === s2 || "mat4x3f" === s2 || "mat4x4f" === s2 || "mat2x2h" === s2 || "mat2x3h" === s2 || "mat2x4h" === s2 || "mat3x2h" === s2 || "mat3x3h" === s2 || "mat3x4h" === s2 || "mat4x2h" === s2 || "mat4x3h" === s2 || "mat4x4h" === s2 ? this._callConstructorMatrix(e2, t2) : e2.value;
    }
    _evalVariable(e2, t2) {
      const n2 = t2.getVariableValue(e2.name);
      return null === n2 ? n2 : n2.getSubData(this, e2.postfix, t2);
    }
    _maxFormatTypeInfo(e2) {
      let t2 = e2[0];
      if ("f32" === t2.name)
        return t2;
      for (let n2 = 1; n2 < e2.length; ++n2) {
        const s2 = ht._priority.get(t2.name);
        ht._priority.get(e2[n2].name) < s2 && (t2 = e2[n2]);
      }
      return "x32" === t2.name ? this.getTypeInfo("i32") : t2;
    }
    _evalUnaryOp(e2, t2) {
      const n2 = this.evalExpression(e2.right, t2);
      if ("&" === e2.operator)
        return new Ne(n2);
      if ("*" === e2.operator)
        return n2 instanceof Ne ? n2.reference.getSubData(this, e2.postfix, t2) : (console.error(`Invalid dereference. Line ${e2.line}`), null);
      const s2 = n2 instanceof Ve ? n2.value : n2 instanceof Be ? Array.from(n2.data) : null;
      switch (e2.operator) {
        case "+": {
          if (ze(s2)) {
            const e4 = s2.map((e5, t4) => +e5);
            return new Be(e4, n2.typeInfo);
          }
          const e3 = s2, t3 = this._maxFormatTypeInfo([n2.typeInfo, n2.typeInfo]);
          return new Ve(+e3, t3);
        }
        case "-": {
          if (ze(s2)) {
            const e4 = s2.map((e5, t4) => -e5);
            return new Be(e4, n2.typeInfo);
          }
          const e3 = s2, t3 = this._maxFormatTypeInfo([n2.typeInfo, n2.typeInfo]);
          return new Ve(-e3, t3);
        }
        case "!": {
          if (ze(s2)) {
            const e4 = s2.map((e5, t4) => e5 ? 0 : 1);
            return new Be(e4, n2.typeInfo);
          }
          const e3 = s2, t3 = this._maxFormatTypeInfo([n2.typeInfo, n2.typeInfo]);
          return new Ve(e3 ? 0 : 1, t3);
        }
        case "~": {
          if (ze(s2)) {
            const e4 = s2.map((e5, t4) => ~e5);
            return new Be(e4, n2.typeInfo);
          }
          const e3 = s2, t3 = this._maxFormatTypeInfo([n2.typeInfo, n2.typeInfo]);
          return new Ve(~e3, t3);
        }
      }
      return console.error(`Invalid unary operator ${e2.operator}. Line ${e2.line}`), null;
    }
    _evalBinaryOp(e2, t2) {
      const n2 = this.evalExpression(e2.left, t2), s2 = this.evalExpression(e2.right, t2), r2 = n2 instanceof Ve ? n2.value : n2 instanceof Be || n2 instanceof Fe ? Array.from(n2.data) : null, a2 = s2 instanceof Ve ? s2.value : s2 instanceof Be || s2 instanceof Fe ? Array.from(s2.data) : null;
      switch (e2.operator) {
        case "+": {
          if (ze(r2) && ze(a2)) {
            const t4 = r2, s3 = a2;
            if (t4.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i3 = t4.map((e3, t5) => e3 + s3[t5]);
            return new Be(i3, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t4 = r2.map((t5, n3) => t5 + e3);
            return new Be(t4, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t4 = a2.map((t5, n3) => e3 + t5);
            return new Be(t4, s2.typeInfo);
          }
          const t3 = r2, i2 = a2, o2 = this._maxFormatTypeInfo([n2.typeInfo, s2.typeInfo]);
          return new Ve(t3 + i2, o2);
        }
        case "-": {
          if (ze(r2) && ze(a2)) {
            const t4 = r2, s3 = a2;
            if (t4.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i3 = t4.map((e3, t5) => e3 - s3[t5]);
            return new Be(i3, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t4 = r2.map((t5, n3) => t5 - e3);
            return new Be(t4, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t4 = a2.map((t5, n3) => e3 - t5);
            return new Be(t4, s2.typeInfo);
          }
          const t3 = r2, i2 = a2, o2 = this._maxFormatTypeInfo([n2.typeInfo, s2.typeInfo]);
          return new Ve(t3 - i2, o2);
        }
        case "*": {
          if (ze(r2) && ze(a2)) {
            const t4 = r2, i3 = a2;
            if (n2 instanceof Fe && s2 instanceof Fe) {
              const r3 = function(e3, t5, n3, s3) {
                if (void 0 === ut[t5.name] || void 0 === ut[s3.name])
                  return null;
                const r4 = ut[t5.name][0], a4 = ut[t5.name][1], i4 = ut[s3.name][0];
                if (r4 !== ut[s3.name][1])
                  return null;
                const o4 = new Array(i4 * a4);
                for (let t6 = 0; t6 < a4; t6++)
                  for (let s4 = 0; s4 < i4; s4++) {
                    let l3 = 0;
                    for (let i5 = 0; i5 < r4; i5++)
                      l3 += e3[i5 * a4 + t6] * n3[s4 * r4 + i5];
                    o4[t6 * i4 + s4] = l3;
                  }
                return o4;
              }(t4, n2.typeInfo, i3, s2.typeInfo);
              if (null === r3)
                return console.error(`Matrix multiplication failed. Line ${e2.line}.`), null;
              const a3 = ut[s2.typeInfo.name][0], o3 = ut[n2.typeInfo.name][1], l2 = this.getTypeInfo(`mat${a3}x${o3}f`);
              return new Fe(r3, l2);
            }
            if (n2 instanceof Fe && s2 instanceof Be) {
              const r3 = function(e3, t5, n3, s3) {
                if (void 0 === ut[t5.name] || void 0 === ct[s3.name])
                  return null;
                const r4 = ut[t5.name][0], a3 = ut[t5.name][1];
                if (r4 !== n3.length)
                  return null;
                const i4 = new Array(a3);
                for (let t6 = 0; t6 < a3; t6++) {
                  let s4 = 0;
                  for (let i5 = 0; i5 < r4; i5++)
                    s4 += e3[i5 * a3 + t6] * n3[i5];
                  i4[t6] = s4;
                }
                return i4;
              }(t4, n2.typeInfo, i3, s2.typeInfo);
              return null === r3 ? (console.error(`Matrix vector multiplication failed. Line ${e2.line}.`), null) : new Be(r3, s2.typeInfo);
            }
            if (n2 instanceof Be && s2 instanceof Fe) {
              const r3 = function(e3, t5, n3, s3) {
                if (void 0 === ct[t5.name] || void 0 === ut[s3.name])
                  return null;
                const r4 = ut[s3.name][0], a3 = ut[s3.name][1];
                if (a3 !== e3.length)
                  return null;
                const i4 = [];
                for (let t6 = 0; t6 < r4; t6++) {
                  let s4 = 0;
                  for (let i5 = 0; i5 < a3; i5++)
                    s4 += e3[i5] * n3[i5 * r4 + t6];
                  i4[t6] = s4;
                }
                return i4;
              }(t4, n2.typeInfo, i3, s2.typeInfo);
              return null === r3 ? (console.error(`Matrix vector multiplication failed. Line ${e2.line}.`), null) : new Be(r3, n2.typeInfo);
            }
            {
              if (t4.length !== i3.length)
                return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
              const s3 = t4.map((e3, t5) => e3 * i3[t5]);
              return new Be(s3, n2.typeInfo);
            }
          }
          if (ze(r2)) {
            const e3 = a2, t4 = r2.map((t5, n3) => t5 * e3);
            return n2 instanceof Fe ? new Fe(t4, n2.typeInfo) : new Be(t4, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t4 = a2.map((t5, n3) => e3 * t5);
            return s2 instanceof Fe ? new Fe(t4, s2.typeInfo) : new Be(t4, s2.typeInfo);
          }
          const t3 = r2, i2 = a2, o2 = this._maxFormatTypeInfo([n2.typeInfo, s2.typeInfo]);
          return new Ve(t3 * i2, o2);
        }
        case "%": {
          if (ze(r2) && ze(a2)) {
            const t4 = r2, s3 = a2;
            if (t4.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i3 = t4.map((e3, t5) => e3 % s3[t5]);
            return new Be(i3, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t4 = r2.map((t5, n3) => t5 % e3);
            return new Be(t4, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t4 = a2.map((t5, n3) => e3 % t5);
            return new Be(t4, s2.typeInfo);
          }
          const t3 = r2, i2 = a2, o2 = this._maxFormatTypeInfo([n2.typeInfo, s2.typeInfo]);
          return new Ve(t3 % i2, o2);
        }
        case "/": {
          if (ze(r2) && ze(a2)) {
            const t4 = r2, s3 = a2;
            if (t4.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i3 = t4.map((e3, t5) => e3 / s3[t5]);
            return new Be(i3, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t4 = r2.map((t5, n3) => t5 / e3);
            return new Be(t4, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t4 = a2.map((t5, n3) => e3 / t5);
            return new Be(t4, s2.typeInfo);
          }
          const t3 = r2, i2 = a2, o2 = this._maxFormatTypeInfo([n2.typeInfo, s2.typeInfo]);
          return new Ve(t3 / i2, o2);
        }
        case "&": {
          if (ze(r2) && ze(a2)) {
            const t4 = r2, s3 = a2;
            if (t4.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i3 = t4.map((e3, t5) => e3 & s3[t5]);
            return new Be(i3, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t4 = r2.map((t5, n3) => t5 & e3);
            return new Be(t4, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t4 = a2.map((t5, n3) => e3 & t5);
            return new Be(t4, s2.typeInfo);
          }
          const t3 = r2, i2 = a2, o2 = this._maxFormatTypeInfo([n2.typeInfo, s2.typeInfo]);
          return new Ve(t3 & i2, o2);
        }
        case "|": {
          if (ze(r2) && ze(a2)) {
            const t4 = r2, s3 = a2;
            if (t4.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i3 = t4.map((e3, t5) => e3 | s3[t5]);
            return new Be(i3, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t4 = r2.map((t5, n3) => t5 | e3);
            return new Be(t4, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t4 = a2.map((t5, n3) => e3 | t5);
            return new Be(t4, s2.typeInfo);
          }
          const t3 = r2, i2 = a2, o2 = this._maxFormatTypeInfo([n2.typeInfo, s2.typeInfo]);
          return new Ve(t3 | i2, o2);
        }
        case "^": {
          if (ze(r2) && ze(a2)) {
            const t4 = r2, s3 = a2;
            if (t4.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i3 = t4.map((e3, t5) => e3 ^ s3[t5]);
            return new Be(i3, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t4 = r2.map((t5, n3) => t5 ^ e3);
            return new Be(t4, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t4 = a2.map((t5, n3) => e3 ^ t5);
            return new Be(t4, s2.typeInfo);
          }
          const t3 = r2, i2 = a2, o2 = this._maxFormatTypeInfo([n2.typeInfo, s2.typeInfo]);
          return new Ve(t3 ^ i2, o2);
        }
        case "<<": {
          if (ze(r2) && ze(a2)) {
            const t4 = r2, s3 = a2;
            if (t4.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i3 = t4.map((e3, t5) => e3 << s3[t5]);
            return new Be(i3, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t4 = r2.map((t5, n3) => t5 << e3);
            return new Be(t4, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t4 = a2.map((t5, n3) => e3 << t5);
            return new Be(t4, s2.typeInfo);
          }
          const t3 = r2, i2 = a2, o2 = this._maxFormatTypeInfo([n2.typeInfo, s2.typeInfo]);
          return new Ve(t3 << i2, o2);
        }
        case ">>": {
          if (ze(r2) && ze(a2)) {
            const t4 = r2, s3 = a2;
            if (t4.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i3 = t4.map((e3, t5) => e3 >> s3[t5]);
            return new Be(i3, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t4 = r2.map((t5, n3) => t5 >> e3);
            return new Be(t4, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t4 = a2.map((t5, n3) => e3 >> t5);
            return new Be(t4, s2.typeInfo);
          }
          const t3 = r2, i2 = a2, o2 = this._maxFormatTypeInfo([n2.typeInfo, s2.typeInfo]);
          return new Ve(t3 >> i2, o2);
        }
        case ">":
          if (ze(r2) && ze(a2)) {
            const t3 = r2, s3 = a2;
            if (t3.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i2 = t3.map((e3, t4) => e3 > s3[t4] ? 1 : 0);
            return new Be(i2, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t3 = r2.map((t4, n3) => t4 > e3 ? 1 : 0);
            return new Be(t3, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t3 = a2.map((t4, n3) => e3 > t4 ? 1 : 0);
            return new Be(t3, s2.typeInfo);
          }
          return new Ve(r2 > a2 ? 1 : 0, this.getTypeInfo("bool"));
        case "<":
          if (ze(r2) && ze(a2)) {
            const t3 = r2, s3 = a2;
            if (t3.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i2 = t3.map((e3, t4) => e3 < s3[t4] ? 1 : 0);
            return new Be(i2, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t3 = r2.map((t4, n3) => t4 < e3 ? 1 : 0);
            return new Be(t3, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t3 = a2.map((t4, n3) => e3 < t4 ? 1 : 0);
            return new Be(t3, s2.typeInfo);
          }
          return new Ve(r2 < a2 ? 1 : 0, this.getTypeInfo("bool"));
        case "==":
          if (ze(r2) && ze(a2)) {
            const t3 = r2, s3 = a2;
            if (t3.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i2 = t3.map((e3, t4) => e3 === s3[t4] ? 1 : 0);
            return new Be(i2, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t3 = r2.map((t4, n3) => t4 == e3 ? 1 : 0);
            return new Be(t3, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t3 = a2.map((t4, n3) => e3 == t4 ? 1 : 0);
            return new Be(t3, s2.typeInfo);
          }
          return new Ve(r2 === a2 ? 1 : 0, this.getTypeInfo("bool"));
        case "!=":
          if (ze(r2) && ze(a2)) {
            const t3 = r2, s3 = a2;
            if (t3.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i2 = t3.map((e3, t4) => e3 !== s3[t4] ? 1 : 0);
            return new Be(i2, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t3 = r2.map((t4, n3) => t4 !== e3 ? 1 : 0);
            return new Be(t3, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t3 = a2.map((t4, n3) => e3 !== t4 ? 1 : 0);
            return new Be(t3, s2.typeInfo);
          }
          return new Ve(r2 !== a2 ? 1 : 0, this.getTypeInfo("bool"));
        case ">=":
          if (ze(r2) && ze(a2)) {
            const t3 = r2, s3 = a2;
            if (t3.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i2 = t3.map((e3, t4) => e3 >= s3[t4] ? 1 : 0);
            return new Be(i2, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t3 = r2.map((t4, n3) => t4 >= e3 ? 1 : 0);
            return new Be(t3, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t3 = a2.map((t4, n3) => e3 >= t4 ? 1 : 0);
            return new Be(t3, s2.typeInfo);
          }
          return new Ve(r2 >= a2 ? 1 : 0, this.getTypeInfo("bool"));
        case "<=":
          if (ze(r2) && ze(a2)) {
            const t3 = r2, s3 = a2;
            if (t3.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i2 = t3.map((e3, t4) => e3 <= s3[t4] ? 1 : 0);
            return new Be(i2, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t3 = r2.map((t4, n3) => t4 <= e3 ? 1 : 0);
            return new Be(t3, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t3 = a2.map((t4, n3) => e3 <= t4 ? 1 : 0);
            return new Be(t3, s2.typeInfo);
          }
          return new Ve(r2 <= a2 ? 1 : 0, this.getTypeInfo("bool"));
        case "&&":
          if (ze(r2) && ze(a2)) {
            const t3 = r2, s3 = a2;
            if (t3.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i2 = t3.map((e3, t4) => e3 && s3[t4] ? 1 : 0);
            return new Be(i2, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t3 = r2.map((t4, n3) => t4 && e3 ? 1 : 0);
            return new Be(t3, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t3 = a2.map((t4, n3) => e3 && t4 ? 1 : 0);
            return new Be(t3, s2.typeInfo);
          }
          return new Ve(r2 && a2 ? 1 : 0, this.getTypeInfo("bool"));
        case "||":
          if (ze(r2) && ze(a2)) {
            const t3 = r2, s3 = a2;
            if (t3.length !== s3.length)
              return console.error(`Vector length mismatch. Line ${e2.line}.`), null;
            const i2 = t3.map((e3, t4) => e3 || s3[t4] ? 1 : 0);
            return new Be(i2, n2.typeInfo);
          }
          if (ze(r2)) {
            const e3 = a2, t3 = r2.map((t4, n3) => t4 || e3 ? 1 : 0);
            return new Be(t3, n2.typeInfo);
          }
          if (ze(a2)) {
            const e3 = r2, t3 = a2.map((t4, n3) => e3 || t4 ? 1 : 0);
            return new Be(t3, s2.typeInfo);
          }
          return new Ve(r2 || a2 ? 1 : 0, this.getTypeInfo("bool"));
      }
      return console.error(`Unknown operator ${e2.operator}. Line ${e2.line}`), null;
    }
    _evalCall(e2, t2) {
      if (null !== e2.cachedReturnValue)
        return e2.cachedReturnValue;
      const n2 = t2.clone();
      n2.currentFunctionName = e2.name;
      const s2 = t2.getFunction(e2.name);
      if (!s2) {
        if (e2.isBuiltin)
          return this._callBuiltinFunction(e2, n2);
        return this.getTypeInfo(e2.name) ? this._evalCreate(e2, t2) : (console.error(`Unknown function "${e2.name}". Line ${e2.line}`), null);
      }
      for (let t3 = 0; t3 < s2.node.args.length; ++t3) {
        const r2 = s2.node.args[t3], a2 = this.evalExpression(e2.args[t3], n2);
        n2.createVariable(r2.name, a2, r2);
      }
      return this._execStatements(s2.node.body, n2);
    }
    _callBuiltinFunction(e2, t2) {
      switch (e2.name) {
        case "all":
          return this.builtins.All(e2, t2);
        case "any":
          return this.builtins.Any(e2, t2);
        case "select":
          return this.builtins.Select(e2, t2);
        case "arrayLength":
          return this.builtins.ArrayLength(e2, t2);
        case "abs":
          return this.builtins.Abs(e2, t2);
        case "acos":
          return this.builtins.Acos(e2, t2);
        case "acosh":
          return this.builtins.Acosh(e2, t2);
        case "asin":
          return this.builtins.Asin(e2, t2);
        case "asinh":
          return this.builtins.Asinh(e2, t2);
        case "atan":
          return this.builtins.Atan(e2, t2);
        case "atanh":
          return this.builtins.Atanh(e2, t2);
        case "atan2":
          return this.builtins.Atan2(e2, t2);
        case "ceil":
          return this.builtins.Ceil(e2, t2);
        case "clamp":
          return this.builtins.Clamp(e2, t2);
        case "cos":
          return this.builtins.Cos(e2, t2);
        case "cosh":
          return this.builtins.Cosh(e2, t2);
        case "countLeadingZeros":
          return this.builtins.CountLeadingZeros(e2, t2);
        case "countOneBits":
          return this.builtins.CountOneBits(e2, t2);
        case "countTrailingZeros":
          return this.builtins.CountTrailingZeros(e2, t2);
        case "cross":
          return this.builtins.Cross(e2, t2);
        case "degrees":
          return this.builtins.Degrees(e2, t2);
        case "determinant":
          return this.builtins.Determinant(e2, t2);
        case "distance":
          return this.builtins.Distance(e2, t2);
        case "dot":
          return this.builtins.Dot(e2, t2);
        case "dot4U8Packed":
          return this.builtins.Dot4U8Packed(e2, t2);
        case "dot4I8Packed":
          return this.builtins.Dot4I8Packed(e2, t2);
        case "exp":
          return this.builtins.Exp(e2, t2);
        case "exp2":
          return this.builtins.Exp2(e2, t2);
        case "extractBits":
          return this.builtins.ExtractBits(e2, t2);
        case "faceForward":
          return this.builtins.FaceForward(e2, t2);
        case "firstLeadingBit":
          return this.builtins.FirstLeadingBit(e2, t2);
        case "firstTrailingBit":
          return this.builtins.FirstTrailingBit(e2, t2);
        case "floor":
          return this.builtins.Floor(e2, t2);
        case "fma":
          return this.builtins.Fma(e2, t2);
        case "fract":
          return this.builtins.Fract(e2, t2);
        case "frexp":
          return this.builtins.Frexp(e2, t2);
        case "insertBits":
          return this.builtins.InsertBits(e2, t2);
        case "inverseSqrt":
          return this.builtins.InverseSqrt(e2, t2);
        case "ldexp":
          return this.builtins.Ldexp(e2, t2);
        case "length":
          return this.builtins.Length(e2, t2);
        case "log":
          return this.builtins.Log(e2, t2);
        case "log2":
          return this.builtins.Log2(e2, t2);
        case "max":
          return this.builtins.Max(e2, t2);
        case "min":
          return this.builtins.Min(e2, t2);
        case "mix":
          return this.builtins.Mix(e2, t2);
        case "modf":
          return this.builtins.Modf(e2, t2);
        case "normalize":
          return this.builtins.Normalize(e2, t2);
        case "pow":
          return this.builtins.Pow(e2, t2);
        case "quantizeToF16":
          return this.builtins.QuantizeToF16(e2, t2);
        case "radians":
          return this.builtins.Radians(e2, t2);
        case "reflect":
          return this.builtins.Reflect(e2, t2);
        case "refract":
          return this.builtins.Refract(e2, t2);
        case "reverseBits":
          return this.builtins.ReverseBits(e2, t2);
        case "round":
          return this.builtins.Round(e2, t2);
        case "saturate":
          return this.builtins.Saturate(e2, t2);
        case "sign":
          return this.builtins.Sign(e2, t2);
        case "sin":
          return this.builtins.Sin(e2, t2);
        case "sinh":
          return this.builtins.Sinh(e2, t2);
        case "smoothStep":
          return this.builtins.SmoothStep(e2, t2);
        case "sqrt":
          return this.builtins.Sqrt(e2, t2);
        case "step":
          return this.builtins.Step(e2, t2);
        case "tan":
          return this.builtins.Tan(e2, t2);
        case "tanh":
          return this.builtins.Tanh(e2, t2);
        case "transpose":
          return this.builtins.Transpose(e2, t2);
        case "trunc":
          return this.builtins.Trunc(e2, t2);
        case "dpdx":
          return this.builtins.Dpdx(e2, t2);
        case "dpdxCoarse":
          return this.builtins.DpdxCoarse(e2, t2);
        case "dpdxFine":
          return this.builtins.DpdxFine(e2, t2);
        case "dpdy":
          return this.builtins.Dpdy(e2, t2);
        case "dpdyCoarse":
          return this.builtins.DpdyCoarse(e2, t2);
        case "dpdyFine":
          return this.builtins.DpdyFine(e2, t2);
        case "fwidth":
          return this.builtins.Fwidth(e2, t2);
        case "fwidthCoarse":
          return this.builtins.FwidthCoarse(e2, t2);
        case "fwidthFine":
          return this.builtins.FwidthFine(e2, t2);
        case "textureDimensions":
          return this.builtins.TextureDimensions(e2, t2);
        case "textureGather":
          return this.builtins.TextureGather(e2, t2);
        case "textureGatherCompare":
          return this.builtins.TextureGatherCompare(e2, t2);
        case "textureLoad":
          return this.builtins.TextureLoad(e2, t2);
        case "textureNumLayers":
          return this.builtins.TextureNumLayers(e2, t2);
        case "textureNumLevels":
          return this.builtins.TextureNumLevels(e2, t2);
        case "textureNumSamples":
          return this.builtins.TextureNumSamples(e2, t2);
        case "textureSample":
          return this.builtins.TextureSample(e2, t2);
        case "textureSampleBias":
          return this.builtins.TextureSampleBias(e2, t2);
        case "textureSampleCompare":
          return this.builtins.TextureSampleCompare(e2, t2);
        case "textureSampleCompareLevel":
          return this.builtins.TextureSampleCompareLevel(e2, t2);
        case "textureSampleGrad":
          return this.builtins.TextureSampleGrad(e2, t2);
        case "textureSampleLevel":
          return this.builtins.TextureSampleLevel(e2, t2);
        case "textureSampleBaseClampToEdge":
          return this.builtins.TextureSampleBaseClampToEdge(e2, t2);
        case "textureStore":
          return this.builtins.TextureStore(e2, t2);
        case "atomicLoad":
          return this.builtins.AtomicLoad(e2, t2);
        case "atomicStore":
          return this.builtins.AtomicStore(e2, t2);
        case "atomicAdd":
          return this.builtins.AtomicAdd(e2, t2);
        case "atomicSub":
          return this.builtins.AtomicSub(e2, t2);
        case "atomicMax":
          return this.builtins.AtomicMax(e2, t2);
        case "atomicMin":
          return this.builtins.AtomicMin(e2, t2);
        case "atomicAnd":
          return this.builtins.AtomicAnd(e2, t2);
        case "atomicOr":
          return this.builtins.AtomicOr(e2, t2);
        case "atomicXor":
          return this.builtins.AtomicXor(e2, t2);
        case "atomicExchange":
          return this.builtins.AtomicExchange(e2, t2);
        case "atomicCompareExchangeWeak":
          return this.builtins.AtomicCompareExchangeWeak(e2, t2);
        case "pack4x8snorm":
          return this.builtins.Pack4x8snorm(e2, t2);
        case "pack4x8unorm":
          return this.builtins.Pack4x8unorm(e2, t2);
        case "pack4xI8":
          return this.builtins.Pack4xI8(e2, t2);
        case "pack4xU8":
          return this.builtins.Pack4xU8(e2, t2);
        case "pack4x8Clamp":
          return this.builtins.Pack4x8Clamp(e2, t2);
        case "pack4xU8Clamp":
          return this.builtins.Pack4xU8Clamp(e2, t2);
        case "pack2x16snorm":
          return this.builtins.Pack2x16snorm(e2, t2);
        case "pack2x16unorm":
          return this.builtins.Pack2x16unorm(e2, t2);
        case "pack2x16float":
          return this.builtins.Pack2x16float(e2, t2);
        case "unpack4x8snorm":
          return this.builtins.Unpack4x8snorm(e2, t2);
        case "unpack4x8unorm":
          return this.builtins.Unpack4x8unorm(e2, t2);
        case "unpack4xI8":
          return this.builtins.Unpack4xI8(e2, t2);
        case "unpack4xU8":
          return this.builtins.Unpack4xU8(e2, t2);
        case "unpack2x16snorm":
          return this.builtins.Unpack2x16snorm(e2, t2);
        case "unpack2x16unorm":
          return this.builtins.Unpack2x16unorm(e2, t2);
        case "unpack2x16float":
          return this.builtins.Unpack2x16float(e2, t2);
        case "storageBarrier":
          return this.builtins.StorageBarrier(e2, t2);
        case "textureBarrier":
          return this.builtins.TextureBarrier(e2, t2);
        case "workgroupBarrier":
          return this.builtins.WorkgroupBarrier(e2, t2);
        case "workgroupUniformLoad":
          return this.builtins.WorkgroupUniformLoad(e2, t2);
        case "subgroupAdd":
          return this.builtins.SubgroupAdd(e2, t2);
        case "subgroupExclusiveAdd":
          return this.builtins.SubgroupExclusiveAdd(e2, t2);
        case "subgroupInclusiveAdd":
          return this.builtins.SubgroupInclusiveAdd(e2, t2);
        case "subgroupAll":
          return this.builtins.SubgroupAll(e2, t2);
        case "subgroupAnd":
          return this.builtins.SubgroupAnd(e2, t2);
        case "subgroupAny":
          return this.builtins.SubgroupAny(e2, t2);
        case "subgroupBallot":
          return this.builtins.SubgroupBallot(e2, t2);
        case "subgroupBroadcast":
          return this.builtins.SubgroupBroadcast(e2, t2);
        case "subgroupBroadcastFirst":
          return this.builtins.SubgroupBroadcastFirst(e2, t2);
        case "subgroupElect":
          return this.builtins.SubgroupElect(e2, t2);
        case "subgroupMax":
          return this.builtins.SubgroupMax(e2, t2);
        case "subgroupMin":
          return this.builtins.SubgroupMin(e2, t2);
        case "subgroupMul":
          return this.builtins.SubgroupMul(e2, t2);
        case "subgroupExclusiveMul":
          return this.builtins.SubgroupExclusiveMul(e2, t2);
        case "subgroupInclusiveMul":
          return this.builtins.SubgroupInclusiveMul(e2, t2);
        case "subgroupOr":
          return this.builtins.SubgroupOr(e2, t2);
        case "subgroupShuffle":
          return this.builtins.SubgroupShuffle(e2, t2);
        case "subgroupShuffleDown":
          return this.builtins.SubgroupShuffleDown(e2, t2);
        case "subgroupShuffleUp":
          return this.builtins.SubgroupShuffleUp(e2, t2);
        case "subgroupShuffleXor":
          return this.builtins.SubgroupShuffleXor(e2, t2);
        case "subgroupXor":
          return this.builtins.SubgroupXor(e2, t2);
        case "quadBroadcast":
          return this.builtins.QuadBroadcast(e2, t2);
        case "quadSwapDiagonal":
          return this.builtins.QuadSwapDiagonal(e2, t2);
        case "quadSwapX":
          return this.builtins.QuadSwapX(e2, t2);
        case "quadSwapY":
          return this.builtins.QuadSwapY(e2, t2);
      }
      const n2 = t2.getFunction(e2.name);
      if (n2) {
        const s2 = t2.clone();
        for (let t3 = 0; t3 < n2.node.args.length; ++t3) {
          const r2 = n2.node.args[t3], a2 = this.evalExpression(e2.args[t3], s2);
          s2.setVariable(r2.name, a2, r2);
        }
        return this._execStatements(n2.node.body, s2);
      }
      return null;
    }
    _callConstructorValue(e2, t2) {
      if (!e2.args || 0 === e2.args.length)
        return new Ve(0, this.getTypeInfo(e2.type));
      const n2 = this.evalExpression(e2.args[0], t2);
      return n2.typeInfo = this.getTypeInfo(e2.type), n2.getSubData(this, e2.postfix, t2).clone();
    }
    _callConstructorVec(e2, t2) {
      const n2 = this.getTypeInfo(e2.type), s2 = e2.type.getTypeName(), r2 = ct[s2];
      if (void 0 === r2)
        return console.error(`Invalid vec constructor ${s2}. Line ${e2.line}`), null;
      const a2 = [];
      if (e2 instanceof _e)
        if (e2.isVector) {
          const t3 = e2.vectorValue;
          for (const e3 of t3)
            a2.push(e3);
        } else
          a2.push(e2.scalarValue);
      else if (e2.args)
        for (const n3 of e2.args) {
          const e3 = this.evalExpression(n3, t2);
          if (e3 instanceof Be) {
            const t3 = e3.data;
            for (let e4 = 0; e4 < t3.length; ++e4) {
              let n4 = t3[e4];
              a2.push(n4);
            }
          } else if (e3 instanceof Ve) {
            let t3 = e3.value;
            a2.push(t3);
          }
        }
      if (e2.type instanceof ie && null === e2.type.format && (e2.type.format = ie.f32), 0 === a2.length) {
        const s3 = new Array(r2).fill(0);
        return new Be(s3, n2).getSubData(this, e2.postfix, t2);
      }
      if (1 === a2.length)
        for (; a2.length < r2; )
          a2.push(a2[0]);
      if (a2.length < r2)
        return console.error(`Invalid vec constructor. Line ${e2.line}`), null;
      return new Be(a2.length > r2 ? a2.slice(0, r2) : a2, n2).getSubData(this, e2.postfix, t2);
    }
    _callConstructorMatrix(e2, t2) {
      const n2 = this.getTypeInfo(e2.type), s2 = e2.type.getTypeName(), a2 = ut[s2];
      if (void 0 === a2)
        return console.error(`Invalid matrix constructor ${s2}. Line ${e2.line}`), null;
      const i2 = [];
      if (e2 instanceof _e)
        if (e2.isVector) {
          const t3 = e2.vectorValue;
          for (const e3 of t3)
            i2.push(e3);
        } else
          i2.push(e2.scalarValue);
      else if (e2.args)
        for (const n3 of e2.args) {
          const e3 = this.evalExpression(n3, t2);
          e3 instanceof Be ? i2.push(...e3.data) : e3 instanceof Ve ? i2.push(e3.value) : e3 instanceof Fe && i2.push(...e3.data);
        }
      if (n2 instanceof r && null === n2.format && (n2.format = this.getTypeInfo("f32")), 0 === i2.length) {
        const s3 = new Array(a2[2]).fill(0);
        return new Fe(s3, n2).getSubData(this, e2.postfix, t2);
      }
      return i2.length !== a2[2] ? (console.error(`Invalid matrix constructor. Line ${e2.line}`), null) : new Fe(i2, n2).getSubData(this, e2.postfix, t2);
    }
  };
  ht._breakObj = new Ce(new e("BREAK", null), null), ht._continueObj = new Ce(new e("CONTINUE", null), null), ht._priority = /* @__PURE__ */ new Map([["f32", 0], ["f16", 1], ["u32", 2], ["i32", 3], ["x32", 3]]);
  var ft = class {
    constructor() {
      this.constants = /* @__PURE__ */ new Map(), this.aliases = /* @__PURE__ */ new Map(), this.structs = /* @__PURE__ */ new Map();
    }
  };
  var pt = class {
    constructor() {
      this._tokens = [], this._current = 0, this._currentLine = 1, this._deferArrayCountEval = [], this._currentLoop = [], this._context = new ft(), this._exec = new ht(), this._forwardTypeCount = 0;
    }
    parse(e2) {
      this._initialize(e2), this._deferArrayCountEval.length = 0;
      const t2 = [];
      for (; !this._isAtEnd(); ) {
        const e3 = this._global_decl_or_directive();
        if (!e3)
          break;
        t2.push(e3);
      }
      if (this._deferArrayCountEval.length > 0) {
        for (const e3 of this._deferArrayCountEval) {
          const t3 = e3.arrayType, n2 = e3.countNode;
          if (n2 instanceof de) {
            const e4 = n2.name, s2 = this._context.constants.get(e4);
            if (s2)
              try {
                const e5 = s2.constEvaluate(this._exec);
                t3.count = e5;
              } catch (e5) {
              }
          }
        }
        this._deferArrayCountEval.length = 0;
      }
      if (this._forwardTypeCount > 0)
        for (const e3 of t2)
          e3.search((e4) => {
            e4 instanceof $e || e4 instanceof oe ? e4.type = this._forwardType(e4.type) : e4 instanceof le ? e4.format = this._forwardType(e4.format) : e4 instanceof O || e4 instanceof F || e4 instanceof M ? e4.type = this._forwardType(e4.type) : e4 instanceof L ? e4.returnType = this._forwardType(e4.returnType) : e4 instanceof Ae && (e4.type = this._forwardType(e4.type));
          });
      return t2;
    }
    _forwardType(e2) {
      if (e2 instanceof re) {
        const t2 = this._getType(e2.name);
        if (t2)
          return t2;
      } else
        e2 instanceof oe ? e2.type = this._forwardType(e2.type) : e2 instanceof le && (e2.format = this._forwardType(e2.format));
      return e2;
    }
    _initialize(e2) {
      if (e2)
        if ("string" == typeof e2) {
          const t2 = new He(e2);
          this._tokens = t2.scanTokens();
        } else
          this._tokens = e2;
      else
        this._tokens = [];
      this._current = 0;
    }
    _updateNode(e2, t2) {
      return e2.line = null != t2 ? t2 : this._currentLine, e2;
    }
    _error(e2, t2) {
      return { token: e2, message: t2, toString: () => `${t2}` };
    }
    _isAtEnd() {
      return this._current >= this._tokens.length || this._peek().type == We.eof;
    }
    _match(e2) {
      if (e2 instanceof Pe)
        return !!this._check(e2) && (this._advance(), true);
      for (let t2 = 0, n2 = e2.length; t2 < n2; ++t2) {
        const n3 = e2[t2];
        if (this._check(n3))
          return this._advance(), true;
      }
      return false;
    }
    _consume(e2, t2) {
      if (this._check(e2))
        return this._advance();
      throw this._error(this._peek(), `${t2}. Line:${this._currentLine}`);
    }
    _check(e2) {
      if (this._isAtEnd())
        return false;
      const t2 = this._peek();
      if (e2 instanceof Array) {
        const n2 = t2.type;
        let s2 = false;
        for (const t3 of e2) {
          if (n2 === t3)
            return true;
          t3 === We.tokens.name && (s2 = true);
        }
        if (s2) {
          const e3 = We.tokens.name.rule.exec(t2.lexeme);
          if (e3 && 0 == e3.index && e3[0] == t2.lexeme)
            return true;
        }
        return false;
      }
      if (t2.type === e2)
        return true;
      if (e2 === We.tokens.name) {
        const e3 = We.tokens.name.rule.exec(t2.lexeme);
        return e3 && 0 == e3.index && e3[0] == t2.lexeme;
      }
      return false;
    }
    _advance() {
      var e2, t2;
      return this._currentLine = null !== (t2 = null === (e2 = this._peek()) || void 0 === e2 ? void 0 : e2.line) && void 0 !== t2 ? t2 : -1, this._isAtEnd() || this._current++, this._previous();
    }
    _peek() {
      return this._tokens[this._current];
    }
    _previous() {
      return this._tokens[this._current - 1];
    }
    _global_decl_or_directive() {
      for (; this._match(We.tokens.semicolon) && !this._isAtEnd(); )
        ;
      if (this._match(We.keywords.alias)) {
        const e3 = this._type_alias();
        return this._consume(We.tokens.semicolon, "Expected ';'"), this._exec.reflection.updateAST([e3]), e3;
      }
      if (this._match(We.keywords.diagnostic)) {
        const e3 = this._diagnostic();
        return this._consume(We.tokens.semicolon, "Expected ';'"), this._exec.reflection.updateAST([e3]), e3;
      }
      if (this._match(We.keywords.requires)) {
        const e3 = this._requires_directive();
        return this._consume(We.tokens.semicolon, "Expected ';'"), this._exec.reflection.updateAST([e3]), e3;
      }
      if (this._match(We.keywords.enable)) {
        const e3 = this._enable_directive();
        return this._consume(We.tokens.semicolon, "Expected ';'"), this._exec.reflection.updateAST([e3]), e3;
      }
      const e2 = this._attribute();
      if (this._check(We.keywords.var)) {
        const t2 = this._global_variable_decl();
        return null != t2 && (t2.attributes = e2), this._consume(We.tokens.semicolon, "Expected ';'."), this._exec.reflection.updateAST([t2]), t2;
      }
      if (this._check(We.keywords.override)) {
        const t2 = this._override_variable_decl();
        return null != t2 && (t2.attributes = e2), this._consume(We.tokens.semicolon, "Expected ';'."), this._exec.reflection.updateAST([t2]), t2;
      }
      if (this._check(We.keywords.let)) {
        const t2 = this._global_let_decl();
        return null != t2 && (t2.attributes = e2), this._consume(We.tokens.semicolon, "Expected ';'."), this._exec.reflection.updateAST([t2]), t2;
      }
      if (this._check(We.keywords.const)) {
        const t2 = this._global_const_decl();
        return null != t2 && (t2.attributes = e2), this._consume(We.tokens.semicolon, "Expected ';'."), this._exec.reflection.updateAST([t2]), t2;
      }
      if (this._check(We.keywords.struct)) {
        const t2 = this._struct_decl();
        return null != t2 && (t2.attributes = e2), this._exec.reflection.updateAST([t2]), t2;
      }
      if (this._check(We.keywords.fn)) {
        const t2 = this._function_decl();
        return null != t2 && (t2.attributes = e2), this._exec.reflection.updateAST([t2]), t2;
      }
      return null;
    }
    _function_decl() {
      if (!this._match(We.keywords.fn))
        return null;
      const e2 = this._currentLine, t2 = this._consume(We.tokens.ident, "Expected function name.").toString();
      this._consume(We.tokens.paren_left, "Expected '(' for function arguments.");
      const n2 = [];
      if (!this._check(We.tokens.paren_right))
        do {
          if (this._check(We.tokens.paren_right))
            break;
          const e3 = this._attribute(), t3 = this._consume(We.tokens.name, "Expected argument name.").toString();
          this._consume(We.tokens.colon, "Expected ':' for argument type.");
          const s3 = this._attribute(), r3 = this._type_decl();
          null != r3 && (r3.attributes = s3, n2.push(this._updateNode(new Ae(t3, r3, e3))));
        } while (this._match(We.tokens.comma));
      this._consume(We.tokens.paren_right, "Expected ')' after function arguments.");
      let s2 = null;
      if (this._match(We.tokens.arrow)) {
        const e3 = this._attribute();
        s2 = this._type_decl(), null != s2 && (s2.attributes = e3);
      }
      const r2 = this._compound_statement(), a2 = this._currentLine;
      return this._updateNode(new L(t2, n2, s2, r2, e2, a2), e2);
    }
    _compound_statement() {
      const e2 = [];
      for (this._consume(We.tokens.brace_left, "Expected '{' for block."); !this._check(We.tokens.brace_right); ) {
        const t2 = this._statement();
        null !== t2 && e2.push(t2);
      }
      return this._consume(We.tokens.brace_right, "Expected '}' for block."), e2;
    }
    _statement() {
      for (; this._match(We.tokens.semicolon) && !this._isAtEnd(); )
        ;
      if (this._check(We.tokens.attr) && this._attribute(), this._check(We.keywords.if))
        return this._if_statement();
      if (this._check(We.keywords.switch))
        return this._switch_statement();
      if (this._check(We.keywords.loop))
        return this._loop_statement();
      if (this._check(We.keywords.for))
        return this._for_statement();
      if (this._check(We.keywords.while))
        return this._while_statement();
      if (this._check(We.keywords.continuing))
        return this._continuing_statement();
      if (this._check(We.keywords.static_assert))
        return this._static_assert_statement();
      if (this._check(We.tokens.brace_left))
        return this._compound_statement();
      let e2 = null;
      if (this._check(We.keywords.return))
        e2 = this._return_statement();
      else if (this._check([We.keywords.var, We.keywords.let, We.keywords.const]))
        e2 = this._variable_statement();
      else if (this._match(We.keywords.discard))
        e2 = this._updateNode(new ee());
      else if (this._match(We.keywords.break)) {
        const t2 = this._updateNode(new te());
        if (this._currentLoop.length > 0) {
          const e3 = this._currentLoop[this._currentLoop.length - 1];
          t2.loopId = e3.id;
        }
        e2 = t2, this._check(We.keywords.if) && (this._advance(), t2.condition = this._optional_paren_expression());
      } else if (this._match(We.keywords.continue)) {
        const t2 = this._updateNode(new ne());
        if (!(this._currentLoop.length > 0))
          throw this._error(this._peek(), `Continue statement must be inside a loop. Line: ${t2.line}`);
        {
          const e3 = this._currentLoop[this._currentLoop.length - 1];
          t2.loopId = e3.id;
        }
        e2 = t2;
      } else
        e2 = this._increment_decrement_statement() || this._func_call_statement() || this._assignment_statement();
      return null != e2 && this._consume(We.tokens.semicolon, "Expected ';' after statement."), e2;
    }
    _static_assert_statement() {
      if (!this._match(We.keywords.static_assert))
        return null;
      const e2 = this._currentLine, t2 = this._optional_paren_expression();
      return this._updateNode(new C(t2), e2);
    }
    _while_statement() {
      if (!this._match(We.keywords.while))
        return null;
      const e2 = this._updateNode(new D(null, null));
      return this._currentLoop.push(e2), e2.condition = this._optional_paren_expression(), this._check(We.tokens.attr) && this._attribute(), e2.body = this._compound_statement(), this._currentLoop.pop(), e2;
    }
    _continuing_statement() {
      const e2 = this._currentLoop.length > 0 ? this._currentLoop[this._currentLoop.length - 1].id : -1;
      if (!this._match(We.keywords.continuing))
        return null;
      const t2 = this._currentLine, n2 = this._compound_statement();
      return this._updateNode(new N(n2, e2), t2);
    }
    _for_statement() {
      if (!this._match(We.keywords.for))
        return null;
      this._consume(We.tokens.paren_left, "Expected '('.");
      const e2 = this._updateNode(new V(null, null, null, null));
      return this._currentLoop.push(e2), e2.init = this._check(We.tokens.semicolon) ? null : this._for_init(), this._consume(We.tokens.semicolon, "Expected ';'."), e2.condition = this._check(We.tokens.semicolon) ? null : this._short_circuit_or_expression(), this._consume(We.tokens.semicolon, "Expected ';'."), e2.increment = this._check(We.tokens.paren_right) ? null : this._for_increment(), this._consume(We.tokens.paren_right, "Expected ')'."), this._check(We.tokens.attr) && this._attribute(), e2.body = this._compound_statement(), this._currentLoop.pop(), e2;
    }
    _for_init() {
      return this._variable_statement() || this._func_call_statement() || this._assignment_statement();
    }
    _for_increment() {
      return this._func_call_statement() || this._increment_decrement_statement() || this._assignment_statement();
    }
    _variable_statement() {
      if (this._check(We.keywords.var)) {
        const e2 = this._variable_decl();
        if (null === e2)
          throw this._error(this._peek(), "Variable declaration expected.");
        let t2 = null;
        return this._match(We.tokens.equal) && (t2 = this._short_circuit_or_expression()), this._updateNode(new O(e2.name, e2.type, e2.storage, e2.access, t2), e2.line);
      }
      if (this._match(We.keywords.let)) {
        const e2 = this._currentLine, t2 = this._consume(We.tokens.name, "Expected name for let.").toString();
        let n2 = null;
        if (this._match(We.tokens.colon)) {
          const e3 = this._attribute();
          n2 = this._type_decl(), null != n2 && (n2.attributes = e3);
        }
        this._consume(We.tokens.equal, "Expected '=' for let.");
        const s2 = this._short_circuit_or_expression();
        return this._updateNode(new F(t2, n2, null, null, s2), e2);
      }
      if (this._match(We.keywords.const)) {
        const e2 = this._currentLine, t2 = this._consume(We.tokens.name, "Expected name for const.").toString();
        let n2 = null;
        if (this._match(We.tokens.colon)) {
          const e3 = this._attribute();
          n2 = this._type_decl(), null != n2 && (n2.attributes = e3);
        }
        this._consume(We.tokens.equal, "Expected '=' for const.");
        const s2 = this._short_circuit_or_expression();
        return null === n2 && s2 instanceof _e && (n2 = s2.type), this._updateNode(new M(t2, n2, null, null, s2), e2);
      }
      return null;
    }
    _increment_decrement_statement() {
      const e2 = this._current, t2 = this._unary_expression();
      if (null == t2)
        return null;
      if (!this._check(We.increment_operators))
        return this._current = e2, null;
      const n2 = this._consume(We.increment_operators, "Expected increment operator");
      return this._updateNode(new H(n2.type === We.tokens.plus_plus ? U.increment : U.decrement, t2));
    }
    _assignment_statement() {
      let e2 = null;
      const t2 = this._currentLine;
      if (this._check(We.tokens.brace_right))
        return null;
      let n2 = this._match(We.tokens.underscore);
      if (n2 || (e2 = this._unary_expression()), !n2 && null == e2)
        return null;
      const s2 = this._consume(We.assignment_operators, "Expected assignment operator."), r2 = this._short_circuit_or_expression();
      return this._updateNode(new z(P.parse(s2.lexeme), e2, r2), t2);
    }
    _func_call_statement() {
      if (!this._check(We.tokens.ident))
        return null;
      const e2 = this._currentLine, t2 = this._current, n2 = this._consume(We.tokens.ident, "Expected function name."), s2 = this._argument_expression_list();
      return null === s2 ? (this._current = t2, null) : this._updateNode(new R(n2.lexeme, s2), e2);
    }
    _loop_statement() {
      if (!this._match(We.keywords.loop))
        return null;
      this._check(We.tokens.attr) && this._attribute(), this._consume(We.tokens.brace_left, "Expected '{' for loop.");
      const e2 = this._updateNode(new G([], null));
      this._currentLoop.push(e2);
      let t2 = this._statement();
      for (; null !== t2; ) {
        if (Array.isArray(t2))
          for (let n2 of t2)
            e2.body.push(n2);
        else
          e2.body.push(t2);
        if (t2 instanceof N) {
          e2.continuing = t2;
          break;
        }
        t2 = this._statement();
      }
      return this._currentLoop.pop(), this._consume(We.tokens.brace_right, "Expected '}' for loop."), e2;
    }
    _switch_statement() {
      if (!this._match(We.keywords.switch))
        return null;
      const e2 = this._updateNode(new X(null, []));
      if (this._currentLoop.push(e2), e2.condition = this._optional_paren_expression(), this._check(We.tokens.attr) && this._attribute(), this._consume(We.tokens.brace_left, "Expected '{' for switch."), e2.cases = this._switch_body(), null == e2.cases || 0 == e2.cases.length)
        throw this._error(this._previous(), "Expected 'case' or 'default'.");
      return this._consume(We.tokens.brace_right, "Expected '}' for switch."), this._currentLoop.pop(), e2;
    }
    _switch_body() {
      const e2 = [];
      let t2 = false;
      for (; this._check([We.keywords.default, We.keywords.case]); ) {
        if (this._match(We.keywords.case)) {
          const n2 = this._case_selectors();
          for (const e3 of n2)
            if (e3 instanceof Ie) {
              if (t2)
                throw this._error(this._previous(), "Multiple default cases in switch statement.");
              t2 = true;
              break;
            }
          this._match(We.tokens.colon), this._check(We.tokens.attr) && this._attribute(), this._consume(We.tokens.brace_left, "Exected '{' for switch case.");
          const s2 = this._case_body();
          this._consume(We.tokens.brace_right, "Exected '}' for switch case."), e2.push(this._updateNode(new Te(n2, s2)));
        }
        if (this._match(We.keywords.default)) {
          if (t2)
            throw this._error(this._previous(), "Multiple default cases in switch statement.");
          this._match(We.tokens.colon), this._check(We.tokens.attr) && this._attribute(), this._consume(We.tokens.brace_left, "Exected '{' for switch default.");
          const n2 = this._case_body();
          this._consume(We.tokens.brace_right, "Exected '}' for switch default."), e2.push(this._updateNode(new Se(n2)));
        }
      }
      return e2;
    }
    _case_selectors() {
      const e2 = [];
      for (this._match(We.keywords.default) ? e2.push(this._updateNode(new Ie())) : e2.push(this._shift_expression()); this._match(We.tokens.comma); )
        this._match(We.keywords.default) ? e2.push(this._updateNode(new Ie())) : e2.push(this._shift_expression());
      return e2;
    }
    _case_body() {
      if (this._match(We.keywords.fallthrough))
        return this._consume(We.tokens.semicolon, "Expected ';'"), [];
      let e2 = this._statement();
      if (null == e2)
        return [];
      e2 instanceof Array || (e2 = [e2]);
      const t2 = this._case_body();
      return 0 == t2.length ? e2 : [...e2, t2[0]];
    }
    _if_statement() {
      if (!this._match(We.keywords.if))
        return null;
      const e2 = this._currentLine, t2 = this._optional_paren_expression();
      this._check(We.tokens.attr) && this._attribute();
      const n2 = this._compound_statement();
      let s2 = [];
      this._match_elseif() && (this._check(We.tokens.attr) && this._attribute(), s2 = this._elseif_statement(s2));
      let r2 = null;
      return this._match(We.keywords.else) && (this._check(We.tokens.attr) && this._attribute(), r2 = this._compound_statement()), this._updateNode(new j(t2, n2, s2, r2), e2);
    }
    _match_elseif() {
      return this._tokens[this._current].type === We.keywords.else && this._tokens[this._current + 1].type === We.keywords.if && (this._advance(), this._advance(), true);
    }
    _elseif_statement(e2 = []) {
      const t2 = this._optional_paren_expression(), n2 = this._compound_statement();
      return e2.push(this._updateNode(new Ee(t2, n2))), this._match_elseif() && (this._check(We.tokens.attr) && this._attribute(), this._elseif_statement(e2)), e2;
    }
    _return_statement() {
      if (!this._match(We.keywords.return))
        return null;
      const e2 = this._short_circuit_or_expression();
      return this._updateNode(new Z(e2));
    }
    _short_circuit_or_expression() {
      let e2 = this._short_circuit_and_expr();
      for (; this._match(We.tokens.or_or); )
        e2 = this._updateNode(new we(this._previous().toString(), e2, this._short_circuit_and_expr()));
      return e2;
    }
    _short_circuit_and_expr() {
      let e2 = this._inclusive_or_expression();
      for (; this._match(We.tokens.and_and); )
        e2 = this._updateNode(new we(this._previous().toString(), e2, this._inclusive_or_expression()));
      return e2;
    }
    _inclusive_or_expression() {
      let e2 = this._exclusive_or_expression();
      for (; this._match(We.tokens.or); )
        e2 = this._updateNode(new we(this._previous().toString(), e2, this._exclusive_or_expression()));
      return e2;
    }
    _exclusive_or_expression() {
      let e2 = this._and_expression();
      for (; this._match(We.tokens.xor); )
        e2 = this._updateNode(new we(this._previous().toString(), e2, this._and_expression()));
      return e2;
    }
    _and_expression() {
      let e2 = this._equality_expression();
      for (; this._match(We.tokens.and); )
        e2 = this._updateNode(new we(this._previous().toString(), e2, this._equality_expression()));
      return e2;
    }
    _equality_expression() {
      const e2 = this._relational_expression();
      return this._match([We.tokens.equal_equal, We.tokens.not_equal]) ? this._updateNode(new we(this._previous().toString(), e2, this._relational_expression())) : e2;
    }
    _relational_expression() {
      let e2 = this._shift_expression();
      for (; this._match([We.tokens.less_than, We.tokens.greater_than, We.tokens.less_than_equal, We.tokens.greater_than_equal]); )
        e2 = this._updateNode(new we(this._previous().toString(), e2, this._shift_expression()));
      return e2;
    }
    _shift_expression() {
      let e2 = this._additive_expression();
      for (; this._match([We.tokens.shift_left, We.tokens.shift_right]); )
        e2 = this._updateNode(new we(this._previous().toString(), e2, this._additive_expression()));
      return e2;
    }
    _additive_expression() {
      let e2 = this._multiplicative_expression();
      for (; this._match([We.tokens.plus, We.tokens.minus]); )
        e2 = this._updateNode(new we(this._previous().toString(), e2, this._multiplicative_expression()));
      return e2;
    }
    _multiplicative_expression() {
      let e2 = this._unary_expression();
      for (; this._match([We.tokens.star, We.tokens.forward_slash, We.tokens.modulo]); )
        e2 = this._updateNode(new we(this._previous().toString(), e2, this._unary_expression()));
      return e2;
    }
    _unary_expression() {
      return this._match([We.tokens.minus, We.tokens.bang, We.tokens.tilde, We.tokens.star, We.tokens.and]) ? this._updateNode(new ve(this._previous().toString(), this._unary_expression())) : this._singular_expression();
    }
    _singular_expression() {
      const e2 = this._primary_expression(), t2 = this._postfix_expression();
      return t2 && (e2.postfix = t2), e2;
    }
    _postfix_expression() {
      if (this._match(We.tokens.bracket_left)) {
        const e2 = this._short_circuit_or_expression();
        this._consume(We.tokens.bracket_right, "Expected ']'.");
        const t2 = this._updateNode(new ye(e2)), n2 = this._postfix_expression();
        return n2 && (t2.postfix = n2), t2;
      }
      if (this._match(We.tokens.period)) {
        const e2 = this._consume(We.tokens.name, "Expected member name."), t2 = this._postfix_expression(), n2 = this._updateNode(new he(e2.lexeme));
        return t2 && (n2.postfix = t2), n2;
      }
      return null;
    }
    _getStruct(e2) {
      if (this._context.aliases.has(e2)) {
        return this._context.aliases.get(e2).type;
      }
      if (this._context.structs.has(e2)) {
        return this._context.structs.get(e2);
      }
      return null;
    }
    _getType(e2) {
      const t2 = this._getStruct(e2);
      if (null !== t2)
        return t2;
      switch (e2) {
        case "void":
          return se.void;
        case "bool":
          return se.bool;
        case "i32":
          return se.i32;
        case "u32":
          return se.u32;
        case "f32":
          return se.f32;
        case "f16":
          return se.f16;
        case "vec2f":
          return ie.vec2f;
        case "vec3f":
          return ie.vec3f;
        case "vec4f":
          return ie.vec4f;
        case "vec2i":
          return ie.vec2i;
        case "vec3i":
          return ie.vec3i;
        case "vec4i":
          return ie.vec4i;
        case "vec2u":
          return ie.vec2u;
        case "vec3u":
          return ie.vec3u;
        case "vec4u":
          return ie.vec4u;
        case "vec2h":
          return ie.vec2h;
        case "vec3h":
          return ie.vec3h;
        case "vec4h":
          return ie.vec4h;
        case "mat2x2f":
          return ie.mat2x2f;
        case "mat2x3f":
          return ie.mat2x3f;
        case "mat2x4f":
          return ie.mat2x4f;
        case "mat3x2f":
          return ie.mat3x2f;
        case "mat3x3f":
          return ie.mat3x3f;
        case "mat3x4f":
          return ie.mat3x4f;
        case "mat4x2f":
          return ie.mat4x2f;
        case "mat4x3f":
          return ie.mat4x3f;
        case "mat4x4f":
          return ie.mat4x4f;
        case "mat2x2h":
          return ie.mat2x2h;
        case "mat2x3h":
          return ie.mat2x3h;
        case "mat2x4h":
          return ie.mat2x4h;
        case "mat3x2h":
          return ie.mat3x2h;
        case "mat3x3h":
          return ie.mat3x3h;
        case "mat3x4h":
          return ie.mat3x4h;
        case "mat4x2h":
          return ie.mat4x2h;
        case "mat4x3h":
          return ie.mat4x3h;
        case "mat4x4h":
          return ie.mat4x4h;
        case "mat2x2i":
          return ie.mat2x2i;
        case "mat2x3i":
          return ie.mat2x3i;
        case "mat2x4i":
          return ie.mat2x4i;
        case "mat3x2i":
          return ie.mat3x2i;
        case "mat3x3i":
          return ie.mat3x3i;
        case "mat3x4i":
          return ie.mat3x4i;
        case "mat4x2i":
          return ie.mat4x2i;
        case "mat4x3i":
          return ie.mat4x3i;
        case "mat4x4i":
          return ie.mat4x4i;
        case "mat2x2u":
          return ie.mat2x2u;
        case "mat2x3u":
          return ie.mat2x3u;
        case "mat2x4u":
          return ie.mat2x4u;
        case "mat3x2u":
          return ie.mat3x2u;
        case "mat3x3u":
          return ie.mat3x3u;
        case "mat3x4u":
          return ie.mat3x4u;
        case "mat4x2u":
          return ie.mat4x2u;
        case "mat4x3u":
          return ie.mat4x3u;
        case "mat4x4u":
          return ie.mat4x4u;
      }
      return null;
    }
    _validateTypeRange(e2, t2) {
      if ("i32" === t2.name) {
        if (e2 < -2147483648 || e2 > 2147483647)
          throw this._error(this._previous(), `Value out of range for i32: ${e2}. Line: ${this._currentLine}.`);
      } else if ("u32" === t2.name && (e2 < 0 || e2 > 4294967295))
        throw this._error(this._previous(), `Value out of range for u32: ${e2}. Line: ${this._currentLine}.`);
    }
    _primary_expression() {
      if (this._match(We.tokens.ident)) {
        const e3 = this._previous().toString();
        if (this._check(We.tokens.paren_left)) {
          const t3 = this._argument_expression_list(), n2 = this._getType(e3);
          return null !== n2 ? this._updateNode(new fe(n2, t3)) : this._updateNode(new pe(e3, t3));
        }
        if (this._context.constants.has(e3)) {
          const t3 = this._context.constants.get(e3);
          return this._updateNode(new me(e3, t3.value));
        }
        return this._updateNode(new de(e3));
      }
      if (this._match(We.tokens.int_literal)) {
        const e3 = this._previous().toString();
        let t3 = e3.endsWith("i") || e3.endsWith("i") ? se.i32 : e3.endsWith("u") || e3.endsWith("U") ? se.u32 : se.x32;
        const n2 = parseInt(e3);
        return this._validateTypeRange(n2, t3), this._updateNode(new _e(new Ve(n2, this._exec.getTypeInfo(t3)), t3));
      }
      if (this._match(We.tokens.uint_literal)) {
        const e3 = parseInt(this._previous().toString());
        return this._validateTypeRange(e3, se.u32), this._updateNode(new _e(new Ve(e3, this._exec.getTypeInfo(se.u32)), se.u32));
      }
      if (this._match([We.tokens.decimal_float_literal, We.tokens.hex_float_literal])) {
        let e3 = this._previous().toString(), t3 = e3.endsWith("h");
        t3 && (e3 = e3.substring(0, e3.length - 1));
        const n2 = parseFloat(e3);
        this._validateTypeRange(n2, t3 ? se.f16 : se.f32);
        const s2 = t3 ? se.f16 : se.f32;
        return this._updateNode(new _e(new Ve(n2, this._exec.getTypeInfo(s2)), s2));
      }
      if (this._match([We.keywords.true, We.keywords.false])) {
        let e3 = this._previous().toString() === We.keywords.true.rule;
        return this._updateNode(new _e(new Ve(e3 ? 1 : 0, this._exec.getTypeInfo(se.bool)), se.bool));
      }
      if (this._check(We.tokens.paren_left))
        return this._paren_expression();
      if (this._match(We.keywords.bitcast)) {
        this._consume(We.tokens.less_than, "Expected '<'.");
        const e3 = this._type_decl();
        this._consume(We.tokens.greater_than, "Expected '>'.");
        const t3 = this._paren_expression();
        return this._updateNode(new ge(e3, t3));
      }
      const e2 = this._type_decl(), t2 = this._argument_expression_list();
      return this._updateNode(new fe(e2, t2));
    }
    _argument_expression_list() {
      if (!this._match(We.tokens.paren_left))
        return null;
      const e2 = [];
      do {
        if (this._check(We.tokens.paren_right))
          break;
        const t2 = this._short_circuit_or_expression();
        e2.push(t2);
      } while (this._match(We.tokens.comma));
      return this._consume(We.tokens.paren_right, "Expected ')' for agument list"), e2;
    }
    _optional_paren_expression() {
      this._match(We.tokens.paren_left);
      const e2 = this._short_circuit_or_expression();
      return this._match(We.tokens.paren_right), e2;
    }
    _paren_expression() {
      this._consume(We.tokens.paren_left, "Expected '('.");
      const e2 = this._short_circuit_or_expression();
      return this._consume(We.tokens.paren_right, "Expected ')'."), e2;
    }
    _struct_decl() {
      if (!this._match(We.keywords.struct))
        return null;
      const e2 = this._currentLine, t2 = this._consume(We.tokens.ident, "Expected name for struct.").toString();
      this._consume(We.tokens.brace_left, "Expected '{' for struct body.");
      const n2 = [];
      for (; !this._check(We.tokens.brace_right); ) {
        const e3 = this._attribute(), t3 = this._consume(We.tokens.name, "Expected variable name.").toString();
        this._consume(We.tokens.colon, "Expected ':' for struct member type.");
        const s3 = this._attribute(), r3 = this._type_decl();
        null != r3 && (r3.attributes = s3), this._check(We.tokens.brace_right) ? this._match(We.tokens.comma) : this._consume(We.tokens.comma, "Expected ',' for struct member."), n2.push(this._updateNode(new $e(t3, r3, e3)));
      }
      this._consume(We.tokens.brace_right, "Expected '}' after struct body.");
      const s2 = this._currentLine, r2 = this._updateNode(new ae(t2, n2, e2, s2), e2);
      return this._context.structs.set(t2, r2), r2;
    }
    _global_variable_decl() {
      const e2 = this._variable_decl();
      if (!e2)
        return null;
      if (this._match(We.tokens.equal)) {
        const t2 = this._const_expression();
        e2.value = t2;
      }
      if (null !== e2.type && e2.value instanceof _e) {
        if ("x32" !== e2.value.type.name) {
          if (e2.type.getTypeName() !== e2.value.type.getTypeName())
            throw this._error(this._peek(), `Invalid cast from ${e2.value.type.name} to ${e2.type.name}. Line:${this._currentLine}`);
        }
        e2.value.isScalar && this._validateTypeRange(e2.value.scalarValue, e2.type), e2.value.type = e2.type;
      } else
        null === e2.type && e2.value instanceof _e && (e2.type = "x32" === e2.value.type.name ? se.i32 : e2.value.type, e2.value.isScalar && this._validateTypeRange(e2.value.scalarValue, e2.type));
      return e2;
    }
    _override_variable_decl() {
      const e2 = this._override_decl();
      return e2 && this._match(We.tokens.equal) && (e2.value = this._const_expression()), e2;
    }
    _global_const_decl() {
      var e2;
      if (!this._match(We.keywords.const))
        return null;
      const t2 = this._consume(We.tokens.name, "Expected variable name"), n2 = this._currentLine;
      let s2 = null;
      if (this._match(We.tokens.colon)) {
        const e3 = this._attribute();
        s2 = this._type_decl(), null != s2 && (s2.attributes = e3);
      }
      let a2 = null;
      this._consume(We.tokens.equal, "const declarations require an assignment");
      const i2 = this._short_circuit_or_expression();
      try {
        let e3 = [se.f32], n3 = i2.constEvaluate(this._exec, e3);
        n3 instanceof Ve && this._validateTypeRange(n3.value, e3[0]), e3[0] instanceof ie && null === e3[0].format && n3.typeInfo instanceof r && null !== n3.typeInfo.format && ("f16" === n3.typeInfo.format.name ? e3[0].format = se.f16 : "f32" === n3.typeInfo.format.name ? e3[0].format = se.f32 : "i32" === n3.typeInfo.format.name ? e3[0].format = se.i32 : "u32" === n3.typeInfo.format.name ? e3[0].format = se.u32 : "bool" === n3.typeInfo.format.name ? e3[0].format = se.bool : console.error(`TODO: impelement template format type ${n3.typeInfo.format.name}`)), a2 = this._updateNode(new _e(n3, e3[0])), this._exec.context.setVariable(t2.toString(), n3);
      } catch (e3) {
        a2 = i2;
      }
      if (null !== s2 && a2 instanceof _e) {
        if ("x32" !== a2.type.name) {
          if (s2.getTypeName() !== a2.type.getTypeName())
            throw this._error(this._peek(), `Invalid cast from ${a2.type.name} to ${s2.name}. Line:${this._currentLine}`);
        }
        a2.type = s2, a2.isScalar && this._validateTypeRange(a2.scalarValue, a2.type);
      } else
        null === s2 && a2 instanceof _e && (s2 = null !== (e2 = null == a2 ? void 0 : a2.type) && void 0 !== e2 ? e2 : se.f32, s2 === se.x32 && (s2 = se.i32));
      const o2 = this._updateNode(new M(t2.toString(), s2, "", "", a2), n2);
      return this._context.constants.set(o2.name, o2), o2;
    }
    _global_let_decl() {
      if (!this._match(We.keywords.let))
        return null;
      const e2 = this._currentLine, t2 = this._consume(We.tokens.name, "Expected variable name");
      let n2 = null;
      if (this._match(We.tokens.colon)) {
        const e3 = this._attribute();
        n2 = this._type_decl(), null != n2 && (n2.attributes = e3);
      }
      let s2 = null;
      if (this._match(We.tokens.equal) && (s2 = this._const_expression()), null !== n2 && s2 instanceof _e) {
        if ("x32" !== s2.type.name) {
          if (n2.getTypeName() !== s2.type.getTypeName())
            throw this._error(this._peek(), `Invalid cast from ${s2.type.name} to ${n2.name}. Line:${this._currentLine}`);
        }
        s2.type = n2;
      } else
        null === n2 && s2 instanceof _e && (n2 = "x32" === s2.type.name ? se.i32 : s2.type);
      return s2 instanceof _e && s2.isScalar && this._validateTypeRange(s2.scalarValue, n2), this._updateNode(new F(t2.toString(), n2, "", "", s2), e2);
    }
    _const_expression() {
      return this._short_circuit_or_expression();
    }
    _variable_decl() {
      if (!this._match(We.keywords.var))
        return null;
      const e2 = this._currentLine;
      let t2 = "", n2 = "";
      this._match(We.tokens.less_than) && (t2 = this._consume(We.storage_class, "Expected storage_class.").toString(), this._match(We.tokens.comma) && (n2 = this._consume(We.access_mode, "Expected access_mode.").toString()), this._consume(We.tokens.greater_than, "Expected '>'."));
      const s2 = this._consume(We.tokens.name, "Expected variable name");
      let r2 = null;
      if (this._match(We.tokens.colon)) {
        const e3 = this._attribute();
        r2 = this._type_decl(), null != r2 && (r2.attributes = e3);
      }
      return this._updateNode(new O(s2.toString(), r2, t2, n2, null), e2);
    }
    _override_decl() {
      if (!this._match(We.keywords.override))
        return null;
      const e2 = this._consume(We.tokens.name, "Expected variable name");
      let t2 = null;
      if (this._match(We.tokens.colon)) {
        const e3 = this._attribute();
        t2 = this._type_decl(), null != t2 && (t2.attributes = e3);
      }
      return this._updateNode(new B(e2.toString(), t2, null));
    }
    _diagnostic() {
      this._consume(We.tokens.paren_left, "Expected '('");
      const e2 = this._consume(We.tokens.ident, "Expected severity control name.");
      this._consume(We.tokens.comma, "Expected ','");
      let t2 = this._consume(We.tokens.ident, "Expected diagnostic rule name.").toString();
      if (this._match(We.tokens.period)) {
        t2 += `.${this._consume(We.tokens.ident, "Expected diagnostic message.").toString()}`;
      }
      return this._consume(We.tokens.paren_right, "Expected ')'"), this._updateNode(new K(e2.toString(), t2));
    }
    _enable_directive() {
      const e2 = this._consume(We.tokens.ident, "identity expected.");
      return this._updateNode(new Q(e2.toString()));
    }
    _requires_directive() {
      const e2 = [this._consume(We.tokens.ident, "identity expected.").toString()];
      for (; this._match(We.tokens.comma); ) {
        const t2 = this._consume(We.tokens.ident, "identity expected.");
        e2.push(t2.toString());
      }
      return this._updateNode(new Y(e2));
    }
    _type_alias() {
      const e2 = this._consume(We.tokens.ident, "identity expected.");
      this._consume(We.tokens.equal, "Expected '=' for type alias.");
      let t2 = this._type_decl();
      if (null === t2)
        throw this._error(this._peek(), "Expected Type for Alias.");
      this._context.aliases.has(t2.name) && (t2 = this._context.aliases.get(t2.name).type);
      const n2 = this._updateNode(new J(e2.toString(), t2));
      return this._context.aliases.set(n2.name, n2), n2;
    }
    _type_decl() {
      if (this._check([We.tokens.ident, ...We.texel_format, We.keywords.bool, We.keywords.f32, We.keywords.i32, We.keywords.u32])) {
        const e3 = this._advance().toString();
        if (this._context.structs.has(e3))
          return this._context.structs.get(e3);
        if (this._context.aliases.has(e3))
          return this._context.aliases.get(e3).type;
        if (!this._getType(e3)) {
          const t3 = this._updateNode(new re(e3));
          return this._forwardTypeCount++, t3;
        }
        return this._updateNode(new se(e3));
      }
      let e2 = this._texture_sampler_types();
      if (e2)
        return e2;
      if (this._check(We.template_types)) {
        let e3 = this._advance().toString(), t3 = null, n2 = null;
        this._match(We.tokens.less_than) && (t3 = this._type_decl(), n2 = null, this._match(We.tokens.comma) && (n2 = this._consume(We.access_mode, "Expected access_mode for pointer").toString()), this._consume(We.tokens.greater_than, "Expected '>' for type."));
        return this._updateNode(new ie(e3, t3, n2));
      }
      if (this._match(We.keywords.ptr)) {
        let e3 = this._previous().toString();
        this._consume(We.tokens.less_than, "Expected '<' for pointer.");
        const t3 = this._consume(We.storage_class, "Expected storage_class for pointer");
        this._consume(We.tokens.comma, "Expected ',' for pointer.");
        const n2 = this._type_decl();
        let s2 = null;
        this._match(We.tokens.comma) && (s2 = this._consume(We.access_mode, "Expected access_mode for pointer").toString()), this._consume(We.tokens.greater_than, "Expected '>' for pointer.");
        return this._updateNode(new oe(e3, t3.toString(), n2, s2));
      }
      const t2 = this._attribute();
      if (this._match(We.keywords.array)) {
        let e3 = null, n2 = -1;
        const s2 = this._previous();
        let r2 = null;
        if (this._match(We.tokens.less_than)) {
          e3 = this._type_decl(), this._context.aliases.has(e3.name) && (e3 = this._context.aliases.get(e3.name).type);
          let t3 = "";
          if (this._match(We.tokens.comma)) {
            r2 = this._shift_expression();
            try {
              t3 = r2.constEvaluate(this._exec).toString(), r2 = null;
            } catch (e4) {
              t3 = "1";
            }
          }
          this._consume(We.tokens.greater_than, "Expected '>' for array."), n2 = t3 ? parseInt(t3) : 0;
        }
        const a2 = this._updateNode(new le(s2.toString(), t2, e3, n2));
        return r2 && this._deferArrayCountEval.push({ arrayType: a2, countNode: r2 }), a2;
      }
      return null;
    }
    _texture_sampler_types() {
      if (this._match(We.sampler_type))
        return this._updateNode(new ce(this._previous().toString(), null, null));
      if (this._match(We.depth_texture_type))
        return this._updateNode(new ce(this._previous().toString(), null, null));
      if (this._match(We.sampled_texture_type) || this._match(We.multisampled_texture_type)) {
        const e2 = this._previous();
        this._consume(We.tokens.less_than, "Expected '<' for sampler type.");
        const t2 = this._type_decl();
        return this._consume(We.tokens.greater_than, "Expected '>' for sampler type."), this._updateNode(new ce(e2.toString(), t2, null));
      }
      if (this._match(We.storage_texture_type)) {
        const e2 = this._previous();
        this._consume(We.tokens.less_than, "Expected '<' for sampler type.");
        const t2 = this._consume(We.texel_format, "Invalid texel format.").toString();
        this._consume(We.tokens.comma, "Expected ',' after texel format.");
        const n2 = this._consume(We.access_mode, "Expected access mode for storage texture type.").toString();
        return this._consume(We.tokens.greater_than, "Expected '>' for sampler type."), this._updateNode(new ce(e2.toString(), t2, n2));
      }
      return null;
    }
    _attribute() {
      let e2 = [];
      for (; this._match(We.tokens.attr); ) {
        const t2 = this._consume(We.attribute_name, "Expected attribute name"), n2 = this._updateNode(new Le(t2.toString(), null));
        if (this._match(We.tokens.paren_left)) {
          if (n2.value = this._consume(We.literal_or_ident, "Expected attribute value").toString(), this._check(We.tokens.comma)) {
            this._advance();
            do {
              const e3 = this._consume(We.literal_or_ident, "Expected attribute value").toString();
              n2.value instanceof Array || (n2.value = [n2.value]), n2.value.push(e3);
            } while (this._match(We.tokens.comma));
          }
          this._consume(We.tokens.paren_right, "Expected ')'");
        }
        e2.push(n2);
      }
      return 0 == e2.length ? null : e2;
    }
  };
  var dt = class extends st {
    constructor(e2) {
      super(), e2 && this.update(e2);
    }
    update(e2) {
      const t2 = new pt().parse(e2);
      this.updateAST(t2);
    }
  };

  // src/lib/wgsl/get-shader-layout-wgsl.ts
  function getShaderLayoutFromWGSL(source3) {
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
      for (const attribute of uniform.type?.members || []) {
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
    const attributeCount = vertex?.inputs.length || 0;
    for (let i2 = 0; i2 < attributeCount; i2++) {
      const wgslAttribute = vertex.inputs[i2];
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
    return type?.format ? `${type.name}<${type.format.name}>` : type.name;
  }
  function parseWGSL(source3) {
    try {
      return new dt(source3);
    } catch (error) {
      if (error instanceof Error) {
        throw error;
      }
      let message = "WGSL parse error";
      if (typeof error === "object" && error?.message) {
        message += `: ${error.message} `;
      }
      if (typeof error === "object" && error?.token) {
        message += error.token.line || "";
      }
      throw new Error(message, { cause: error });
    }
  }

  // ../../node_modules/@math.gl/core/dist/lib/common.js
  var RADIANS_TO_DEGREES = 1 / Math.PI * 180;
  var DEGREES_TO_RADIANS = 1 / 180 * Math.PI;
  var DEFAULT_CONFIG = {
    EPSILON: 1e-12,
    debug: false,
    precision: 4,
    printTypes: false,
    printDegrees: false,
    printRowMajor: true,
    _cartographicRadians: false
  };
  globalThis.mathgl = globalThis.mathgl || { config: { ...DEFAULT_CONFIG } };
  var config = globalThis.mathgl.config;
  function isArray(value) {
    return Array.isArray(value) || ArrayBuffer.isView(value) && !(value instanceof DataView);
  }
  function clamp(value, min, max) {
    return map(value, (value2) => Math.max(min, Math.min(max, value2)));
  }
  function duplicateArray(array) {
    return array.clone ? array.clone() : new Array(array.length);
  }
  function map(value, func, result) {
    if (isArray(value)) {
      const array = value;
      result = result || duplicateArray(array);
      for (let i2 = 0; i2 < result.length && i2 < array.length; ++i2) {
        const val = typeof value === "number" ? value : value[i2];
        result[i2] = func(val, i2, result);
      }
      return result;
    }
    return func(value);
  }

  // src/modules/math/fp16/fp16-utils.ts
  var float16Tables = null;
  var buffer = new ArrayBuffer(4);
  var floatView = new Float32Array(buffer);
  var uint32View = new Uint32Array(buffer);
  function toHalfFloat(val) {
    float16Tables ||= generateFloat16Tables();
    val = clamp(val, -65504, 65504);
    floatView[0] = val;
    const f2 = uint32View[0];
    const e2 = f2 >> 23 & 511;
    return float16Tables.baseTable[e2] + ((f2 & 8388607) >> float16Tables.shiftTable[e2]);
  }
  function fromHalfFloat(val) {
    float16Tables ||= generateFloat16Tables();
    const m2 = val >> 10;
    uint32View[0] = float16Tables.mantissaTable[float16Tables.offsetTable[m2] + (val & 1023)] + float16Tables.exponentTable[m2];
    return floatView[0];
  }
  function generateFloat16Tables() {
    const baseTable = new Uint32Array(512);
    const shiftTable = new Uint32Array(512);
    for (let i2 = 0; i2 < 256; ++i2) {
      const e2 = i2 - 127;
      if (e2 < -27) {
        baseTable[i2] = 0;
        baseTable[i2 | 256] = 32768;
        shiftTable[i2] = 24;
        shiftTable[i2 | 256] = 24;
      } else if (e2 < -14) {
        baseTable[i2] = 1024 >> -e2 - 14;
        baseTable[i2 | 256] = 1024 >> -e2 - 14 | 32768;
        shiftTable[i2] = -e2 - 1;
        shiftTable[i2 | 256] = -e2 - 1;
      } else if (e2 <= 15) {
        baseTable[i2] = e2 + 15 << 10;
        baseTable[i2 | 256] = e2 + 15 << 10 | 32768;
        shiftTable[i2] = 13;
        shiftTable[i2 | 256] = 13;
      } else if (e2 < 128) {
        baseTable[i2] = 31744;
        baseTable[i2 | 256] = 64512;
        shiftTable[i2] = 24;
        shiftTable[i2 | 256] = 24;
      } else {
        baseTable[i2] = 31744;
        baseTable[i2 | 256] = 64512;
        shiftTable[i2] = 13;
        shiftTable[i2 | 256] = 13;
      }
    }
    const mantissaTable = new Uint32Array(2048);
    const exponentTable = new Uint32Array(64);
    const offsetTable = new Uint32Array(64);
    for (let i2 = 1; i2 < 1024; ++i2) {
      let m2 = i2 << 13;
      let e2 = 0;
      while ((m2 & 8388608) === 0) {
        m2 <<= 1;
        e2 -= 8388608;
      }
      m2 &= ~8388608;
      e2 += 947912704;
      mantissaTable[i2] = m2 | e2;
    }
    for (let i2 = 1024; i2 < 2048; ++i2) {
      mantissaTable[i2] = 939524096 + (i2 - 1024 << 13);
    }
    for (let i2 = 1; i2 < 31; ++i2) {
      exponentTable[i2] = i2 << 23;
    }
    exponentTable[31] = 1199570944;
    exponentTable[32] = 2147483648;
    for (let i2 = 33; i2 < 63; ++i2) {
      exponentTable[i2] = 2147483648 + (i2 - 32 << 23);
    }
    exponentTable[63] = 3347054592;
    for (let i2 = 1; i2 < 64; ++i2) {
      if (i2 !== 32) {
        offsetTable[i2] = 1024;
      }
    }
    return { baseTable, shiftTable, mantissaTable, exponentTable, offsetTable };
  }

  // src/modules/math/fp64/fp64-utils.ts
  function fp64ify(a2, out = [], startIndex = 0) {
    const hiPart = Math.fround(a2);
    const loPart = a2 - hiPart;
    out[startIndex] = hiPart;
    out[startIndex + 1] = loPart;
    return out;
  }
  function fp64LowPart(a2) {
    return a2 - Math.fround(a2);
  }
  function fp64ifyMatrix4(matrix) {
    const matrixFP64 = new Float32Array(32);
    for (let i2 = 0; i2 < 4; ++i2) {
      for (let j2 = 0; j2 < 4; ++j2) {
        const index = i2 * 4 + j2;
        fp64ify(matrix[j2 * 4 + i2], matrixFP64, index * 2);
      }
    }
    return matrixFP64;
  }

  // src/modules/math/random/random.ts
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

  // src/modules/math/fp32/fp32.ts
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

  // src/modules/math/fp64/fp64-arithmetic-glsl.ts
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

  // src/modules/math/fp64/fp64-functions-glsl.ts
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

  // src/modules/math/fp64/fp64.ts
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

  // src/modules/engine/picking/picking.ts
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
      const color = Array.from(opts.highlightColor, (x2) => x2 / 255);
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

  // src/modules/lighting/lights/lighting.ts
  var import_core3 = __toESM(require_core(), 1);

  // src/modules/lighting/lights/lighting-glsl.ts
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

  // src/modules/lighting/lights/lighting-wgsl.ts
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

  // src/modules/lighting/lights/lighting.ts
  var MAX_LIGHTS = 5;
  var COLOR_FACTOR = 255;
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
      lightType: 0 /* POINT */,
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
  function getLightSourceUniforms({
    ambientLight,
    pointLights = [],
    directionalLights = []
  }) {
    const lightSourceUniforms = {};
    lightSourceUniforms.ambientColor = convertColor(ambientLight);
    let currentLight = 0;
    for (const pointLight of pointLights) {
      lightSourceUniforms.lightType = 0 /* POINT */;
      const i2 = currentLight;
      lightSourceUniforms[`lightColor${i2}`] = convertColor(pointLight);
      lightSourceUniforms[`lightPosition${i2}`] = pointLight.position;
      lightSourceUniforms[`lightAttenuation${i2}`] = pointLight.attenuation || [1, 0, 0];
      currentLight++;
    }
    for (const directionalLight of directionalLights) {
      lightSourceUniforms.lightType = 1 /* DIRECTIONAL */;
      const i2 = currentLight;
      lightSourceUniforms[`lightColor${i2}`] = convertColor(directionalLight);
      lightSourceUniforms[`lightDirection${i2}`] = directionalLight.direction;
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
    const lightSources = { pointLights: [], directionalLights: [] };
    for (const light of lights || []) {
      switch (light.type) {
        case "ambient":
          lightSources.ambientLight = light;
          break;
        case "directional":
          lightSources.directionalLights?.push(light);
          break;
        case "point":
          lightSources.pointLights?.push(light);
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

  // src/modules/lighting/no-material/dirlight.ts
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

  // src/modules/lighting/phong-material/phong-shaders-glsl.ts
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

  // src/modules/lighting/phong-material/phong-shaders-wgsl.ts
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

  // src/modules/lighting/gouraud-material/gouraud-material.ts
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
        uniforms.specularColor = uniforms.specularColor.map((x2) => x2 / 255);
      }
      return { ...gouraudMaterial.defaultUniforms, ...uniforms };
    }
  };

  // src/modules/lighting/phong-material/phong-material.ts
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
        uniforms.specularColor = uniforms.specularColor.map((x2) => x2 / 255);
      }
      return { ...phongMaterial.defaultUniforms, ...uniforms };
    }
  };

  // src/modules/lighting/pbr-material/pbr-material-glsl.ts
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

  // src/modules/lighting/pbr-material/pbr-material-wgsl.ts
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

  // src/modules/lighting/pbr-material/pbr-projection.ts
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

  // src/modules/lighting/pbr-material/pbr-material.ts
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
  return __toCommonJS(bundle_exports);
})();
      return __exports__;
      });
