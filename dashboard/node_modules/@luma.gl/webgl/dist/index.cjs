"use strict";
var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __esm = (fn, res) => function __init() {
  return fn && (res = (0, fn[__getOwnPropNames(fn)[0]])(fn = 0)), res;
};
var __export = (target2, all) => {
  for (var name in all)
    __defProp(target2, name, { get: all[name], enumerable: true });
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

// dist/context/polyfills/polyfill-webgl1-extensions.js
function enforceWebGL2(enforce = true) {
  const prototype = HTMLCanvasElement.prototype;
  if (!enforce && prototype.originalGetContext) {
    prototype.getContext = prototype.originalGetContext;
    prototype.originalGetContext = void 0;
    return;
  }
  prototype.originalGetContext = prototype.getContext;
  prototype.getContext = function(contextId, options) {
    if (contextId === "webgl" || contextId === "experimental-webgl") {
      const context = this.originalGetContext("webgl2", options);
      if (context instanceof HTMLElement) {
        polyfillWebGL1Extensions(context);
      }
      return context;
    }
    return this.originalGetContext(contextId, options);
  };
}
function polyfillWebGL1Extensions(gl) {
  gl.getExtension("EXT_color_buffer_float");
  const boundExtensions = {
    ...WEBGL1_STATIC_EXTENSIONS,
    WEBGL_disjoint_timer_query: gl.getExtension("EXT_disjoint_timer_query_webgl2"),
    WEBGL_draw_buffers: getWEBGL_draw_buffers(gl),
    OES_vertex_array_object: getOES_vertex_array_object(gl),
    ANGLE_instanced_arrays: getANGLE_instanced_arrays(gl)
  };
  const originalGetExtension = gl.getExtension;
  gl.getExtension = function(extensionName) {
    const ext = originalGetExtension.call(gl, extensionName);
    if (ext) {
      return ext;
    }
    if (extensionName in boundExtensions) {
      return boundExtensions[extensionName];
    }
    return null;
  };
  const originalGetSupportedExtensions = gl.getSupportedExtensions;
  gl.getSupportedExtensions = function() {
    const extensions = originalGetSupportedExtensions.apply(gl) || [];
    return extensions == null ? void 0 : extensions.concat(Object.keys(boundExtensions));
  };
}
var import_constants, WEBGL1_STATIC_EXTENSIONS, getWEBGL_draw_buffers, getOES_vertex_array_object, getANGLE_instanced_arrays;
var init_polyfill_webgl1_extensions = __esm({
  "dist/context/polyfills/polyfill-webgl1-extensions.js"() {
    "use strict";
    import_constants = require("@luma.gl/constants");
    WEBGL1_STATIC_EXTENSIONS = {
      WEBGL_depth_texture: {
        UNSIGNED_INT_24_8_WEBGL: 34042
      },
      OES_element_index_uint: {},
      OES_texture_float: {},
      OES_texture_half_float: {
        // @ts-expect-error different numbers?
        HALF_FLOAT_OES: 5131
      },
      EXT_color_buffer_float: {},
      OES_standard_derivatives: {
        FRAGMENT_SHADER_DERIVATIVE_HINT_OES: 35723
      },
      EXT_frag_depth: {},
      EXT_blend_minmax: {
        MIN_EXT: 32775,
        MAX_EXT: 32776
      },
      EXT_shader_texture_lod: {}
    };
    getWEBGL_draw_buffers = (gl) => ({
      drawBuffersWEBGL(buffers) {
        return gl.drawBuffers(buffers);
      },
      COLOR_ATTACHMENT0_WEBGL: 36064,
      COLOR_ATTACHMENT1_WEBGL: 36065,
      COLOR_ATTACHMENT2_WEBGL: 36066,
      COLOR_ATTACHMENT3_WEBGL: 36067
    });
    getOES_vertex_array_object = (gl) => ({
      VERTEX_ARRAY_BINDING_OES: 34229,
      createVertexArrayOES() {
        return gl.createVertexArray();
      },
      deleteVertexArrayOES(vertexArray) {
        return gl.deleteVertexArray(vertexArray);
      },
      isVertexArrayOES(vertexArray) {
        return gl.isVertexArray(vertexArray);
      },
      bindVertexArrayOES(vertexArray) {
        return gl.bindVertexArray(vertexArray);
      }
    });
    getANGLE_instanced_arrays = (gl) => ({
      VERTEX_ATTRIB_ARRAY_DIVISOR_ANGLE: 35070,
      drawArraysInstancedANGLE(...args) {
        return gl.drawArraysInstanced(...args);
      },
      drawElementsInstancedANGLE(...args) {
        return gl.drawElementsInstanced(...args);
      },
      vertexAttribDivisorANGLE(...args) {
        return gl.vertexAttribDivisor(...args);
      }
    });
  }
});

// dist/utils/load-script.js
async function loadScript(scriptUrl, scriptId) {
  const head = document.getElementsByTagName("head")[0];
  if (!head) {
    throw new Error("loadScript");
  }
  const script = document.createElement("script");
  script.setAttribute("type", "text/javascript");
  script.setAttribute("src", scriptUrl);
  if (scriptId) {
    script.id = scriptId;
  }
  return new Promise((resolve, reject) => {
    script.onload = resolve;
    script.onerror = (error) => reject(new Error(`Unable to load script '${scriptUrl}': ${error}`));
    head.appendChild(script);
  });
}
var init_load_script = __esm({
  "dist/utils/load-script.js"() {
    "use strict";
  }
});

// dist/context/debug/spector.js
async function loadSpectorJS(props) {
  if (!globalThis.SPECTOR) {
    try {
      await loadScript(props.debugSpectorJSUrl || DEFAULT_SPECTOR_PROPS.debugSpectorJSUrl);
    } catch (error) {
      import_core.log.warn(String(error));
    }
  }
}
function initializeSpectorJS(props) {
  var _a;
  props = { ...DEFAULT_SPECTOR_PROPS, ...props };
  if (!props.debugSpectorJS) {
    return null;
  }
  if (!spector && globalThis.SPECTOR && !((_a = globalThis.luma) == null ? void 0 : _a.spector)) {
    import_core.log.probe(LOG_LEVEL, "SPECTOR found and initialized. Start with `luma.spector.displayUI()`")();
    const { Spector: SpectorJS } = globalThis.SPECTOR;
    spector = new SpectorJS();
    if (globalThis.luma) {
      globalThis.luma.spector = spector;
    }
  }
  if (!spector) {
    return null;
  }
  if (!initialized) {
    initialized = true;
    spector.spyCanvases();
    spector == null ? void 0 : spector.onCaptureStarted.add((capture) => import_core.log.info("Spector capture started:", capture)());
    spector == null ? void 0 : spector.onCapture.add((capture) => {
      import_core.log.info("Spector capture complete:", capture)();
      spector == null ? void 0 : spector.getResultUI();
      spector == null ? void 0 : spector.resultView.display();
      spector == null ? void 0 : spector.resultView.addCapture(capture);
    });
  }
  if (props.gl) {
    const gl = props.gl;
    const device = gl.device;
    spector == null ? void 0 : spector.startCapture(props.gl, 500);
    gl.device = device;
    new Promise((resolve) => setTimeout(resolve, 2e3)).then((_) => {
      import_core.log.info("Spector capture stopped after 2 seconds")();
      spector == null ? void 0 : spector.stopCapture();
    });
  }
  return spector;
}
var import_core, LOG_LEVEL, spector, initialized, DEFAULT_SPECTOR_PROPS;
var init_spector = __esm({
  "dist/context/debug/spector.js"() {
    "use strict";
    import_core = require("@luma.gl/core");
    init_load_script();
    LOG_LEVEL = 1;
    spector = null;
    initialized = false;
    DEFAULT_SPECTOR_PROPS = {
      debugSpectorJS: import_core.log.get("debug-spectorjs"),
      // https://github.com/BabylonJS/Spector.js#basic-usage
      // https://forum.babylonjs.com/t/spectorcdn-is-temporarily-off/48241
      // spectorUrl: 'https://spectorcdn.babylonjs.com/spector.bundle.js';
      debugSpectorJSUrl: "https://cdn.jsdelivr.net/npm/spectorjs@0.9.30/dist/spector.bundle.js",
      gl: void 0
    };
  }
});

// dist/context/debug/webgl-developer-tools.js
function getWebGLContextData(gl) {
  gl.luma = gl.luma || {};
  return gl.luma;
}
async function loadWebGLDeveloperTools() {
  if ((0, import_env.isBrowser)() && !globalThis.WebGLDebugUtils) {
    globalThis.global = globalThis.global || globalThis;
    globalThis.global.module = {};
    await loadScript(WEBGL_DEBUG_CDN_URL);
  }
}
function makeDebugContext(gl, props = {}) {
  return props.debugWebGL || props.traceWebGL ? getDebugContext(gl, props) : getRealContext(gl);
}
function getRealContext(gl) {
  const data = getWebGLContextData(gl);
  return data.realContext ? data.realContext : gl;
}
function getDebugContext(gl, props) {
  if (!globalThis.WebGLDebugUtils) {
    import_core2.log.warn("webgl-debug not loaded")();
    return gl;
  }
  const data = getWebGLContextData(gl);
  if (data.debugContext) {
    return data.debugContext;
  }
  globalThis.WebGLDebugUtils.init({ ...import_constants2.GL, ...gl });
  const glDebug = globalThis.WebGLDebugUtils.makeDebugContext(gl, onGLError.bind(null, props), onValidateGLFunc.bind(null, props));
  for (const key in import_constants2.GL) {
    if (!(key in glDebug) && typeof import_constants2.GL[key] === "number") {
      glDebug[key] = import_constants2.GL[key];
    }
  }
  class WebGLDebugContext {
  }
  Object.setPrototypeOf(glDebug, Object.getPrototypeOf(gl));
  Object.setPrototypeOf(WebGLDebugContext, glDebug);
  const debugContext = Object.create(WebGLDebugContext);
  data.realContext = gl;
  data.debugContext = debugContext;
  debugContext.debug = true;
  return debugContext;
}
function getFunctionString(functionName, functionArgs) {
  functionArgs = Array.from(functionArgs).map((arg) => arg === void 0 ? "undefined" : arg);
  let args = globalThis.WebGLDebugUtils.glFunctionArgsToString(functionName, functionArgs);
  args = `${args.slice(0, 100)}${args.length > 100 ? "..." : ""}`;
  return `gl.${functionName}(${args})`;
}
function onGLError(props, err, functionName, args) {
  args = Array.from(args).map((arg) => arg === void 0 ? "undefined" : arg);
  const errorMessage = globalThis.WebGLDebugUtils.glEnumToString(err);
  const functionArgs = globalThis.WebGLDebugUtils.glFunctionArgsToString(functionName, args);
  const message2 = `${errorMessage} in gl.${functionName}(${functionArgs})`;
  import_core2.log.error(message2)();
  debugger;
}
function onValidateGLFunc(props, functionName, functionArgs) {
  let functionString = "";
  if (import_core2.log.level >= 1) {
    functionString = getFunctionString(functionName, functionArgs);
    if (props.traceWebGL) {
      import_core2.log.log(1, functionString)();
    }
  }
  for (const arg of functionArgs) {
    if (arg === void 0) {
      functionString = functionString || getFunctionString(functionName, functionArgs);
      debugger;
    }
  }
}
var import_core2, import_constants2, import_env, WEBGL_DEBUG_CDN_URL;
var init_webgl_developer_tools = __esm({
  "dist/context/debug/webgl-developer-tools.js"() {
    "use strict";
    import_core2 = require("@luma.gl/core");
    import_constants2 = require("@luma.gl/constants");
    import_env = require("@probe.gl/env");
    init_load_script();
    WEBGL_DEBUG_CDN_URL = "https://unpkg.com/webgl-debug@2.0.1/index.js";
  }
});

// dist/context/parameters/webgl-parameter-tables.js
function isArray(array) {
  return Array.isArray(array) || ArrayBuffer.isView(array) && !(array instanceof DataView);
}
function getValue(glEnum, values, cache) {
  return values[glEnum] !== void 0 ? values[glEnum] : cache[glEnum];
}
var import_constants3, GL_PARAMETER_DEFAULTS, enable, hint, pixelStorei, bindFramebuffer, bindBuffer, GL_PARAMETER_SETTERS, GL_COMPOSITE_PARAMETER_SETTERS, GL_HOOKED_SETTERS, isEnabled, GL_PARAMETER_GETTERS, NON_CACHE_PARAMETERS;
var init_webgl_parameter_tables = __esm({
  "dist/context/parameters/webgl-parameter-tables.js"() {
    "use strict";
    import_constants3 = require("@luma.gl/constants");
    GL_PARAMETER_DEFAULTS = {
      [3042]: false,
      [32773]: new Float32Array([0, 0, 0, 0]),
      [32777]: 32774,
      [34877]: 32774,
      [32969]: 1,
      [32968]: 0,
      [32971]: 1,
      [32970]: 0,
      [3106]: new Float32Array([0, 0, 0, 0]),
      // TBD
      [3107]: [true, true, true, true],
      [2884]: false,
      [2885]: 1029,
      [2929]: false,
      [2931]: 1,
      [2932]: 513,
      [2928]: new Float32Array([0, 1]),
      // TBD
      [2930]: true,
      [3024]: true,
      [35725]: null,
      // FRAMEBUFFER_BINDING and DRAW_FRAMEBUFFER_BINDING(WebGL2) refer same state.
      [36006]: null,
      [36007]: null,
      [34229]: null,
      [34964]: null,
      [2886]: 2305,
      [33170]: 4352,
      [2849]: 1,
      [32823]: false,
      [32824]: 0,
      [10752]: 0,
      [32926]: false,
      [32928]: false,
      [32938]: 1,
      [32939]: false,
      [3089]: false,
      // Note: Dynamic value. If scissor test enabled we expect users to set correct scissor box
      [3088]: new Int32Array([0, 0, 1024, 1024]),
      [2960]: false,
      [2961]: 0,
      [2968]: 4294967295,
      [36005]: 4294967295,
      [2962]: 519,
      [2967]: 0,
      [2963]: 4294967295,
      [34816]: 519,
      [36003]: 0,
      [36004]: 4294967295,
      [2964]: 7680,
      [2965]: 7680,
      [2966]: 7680,
      [34817]: 7680,
      [34818]: 7680,
      [34819]: 7680,
      // Dynamic value: We use [0, 0, 1024, 1024] as default, but usually this is updated in each frame.
      [2978]: [0, 0, 1024, 1024],
      [36389]: null,
      [36662]: null,
      [36663]: null,
      [35053]: null,
      [35055]: null,
      [35723]: 4352,
      [36010]: null,
      [35977]: false,
      [3333]: 4,
      [3317]: 4,
      [37440]: false,
      [37441]: false,
      [37443]: 37444,
      [3330]: 0,
      [3332]: 0,
      [3331]: 0,
      [3314]: 0,
      [32878]: 0,
      [3316]: 0,
      [3315]: 0,
      [32877]: 0
    };
    enable = (gl, value, key) => value ? gl.enable(key) : gl.disable(key);
    hint = (gl, value, key) => gl.hint(key, value);
    pixelStorei = (gl, value, key) => gl.pixelStorei(key, value);
    bindFramebuffer = (gl, value, key) => {
      const target2 = key === 36006 ? 36009 : 36008;
      return gl.bindFramebuffer(target2, value);
    };
    bindBuffer = (gl, value, key) => {
      const bindingMap = {
        [34964]: 34962,
        [36662]: 36662,
        [36663]: 36663,
        [35053]: 35051,
        [35055]: 35052
      };
      const glTarget = bindingMap[key];
      gl.bindBuffer(glTarget, value);
    };
    GL_PARAMETER_SETTERS = {
      [3042]: enable,
      [32773]: (gl, value) => gl.blendColor(...value),
      [32777]: "blendEquation",
      [34877]: "blendEquation",
      [32969]: "blendFunc",
      [32968]: "blendFunc",
      [32971]: "blendFunc",
      [32970]: "blendFunc",
      [3106]: (gl, value) => gl.clearColor(...value),
      [3107]: (gl, value) => gl.colorMask(...value),
      [2884]: enable,
      [2885]: (gl, value) => gl.cullFace(value),
      [2929]: enable,
      [2931]: (gl, value) => gl.clearDepth(value),
      [2932]: (gl, value) => gl.depthFunc(value),
      [2928]: (gl, value) => gl.depthRange(...value),
      [2930]: (gl, value) => gl.depthMask(value),
      [3024]: enable,
      [35723]: hint,
      [35725]: (gl, value) => gl.useProgram(value),
      [36007]: (gl, value) => gl.bindRenderbuffer(36161, value),
      [36389]: (gl, value) => {
        var _a;
        return (_a = gl.bindTransformFeedback) == null ? void 0 : _a.call(gl, 36386, value);
      },
      [34229]: (gl, value) => gl.bindVertexArray(value),
      // NOTE: FRAMEBUFFER_BINDING and DRAW_FRAMEBUFFER_BINDING(WebGL2) refer same state.
      [36006]: bindFramebuffer,
      [36010]: bindFramebuffer,
      // Buffers
      [34964]: bindBuffer,
      [36662]: bindBuffer,
      [36663]: bindBuffer,
      [35053]: bindBuffer,
      [35055]: bindBuffer,
      [2886]: (gl, value) => gl.frontFace(value),
      [33170]: hint,
      [2849]: (gl, value) => gl.lineWidth(value),
      [32823]: enable,
      [32824]: "polygonOffset",
      [10752]: "polygonOffset",
      [35977]: enable,
      [32926]: enable,
      [32928]: enable,
      [32938]: "sampleCoverage",
      [32939]: "sampleCoverage",
      [3089]: enable,
      [3088]: (gl, value) => gl.scissor(...value),
      [2960]: enable,
      [2961]: (gl, value) => gl.clearStencil(value),
      [2968]: (gl, value) => gl.stencilMaskSeparate(1028, value),
      [36005]: (gl, value) => gl.stencilMaskSeparate(1029, value),
      [2962]: "stencilFuncFront",
      [2967]: "stencilFuncFront",
      [2963]: "stencilFuncFront",
      [34816]: "stencilFuncBack",
      [36003]: "stencilFuncBack",
      [36004]: "stencilFuncBack",
      [2964]: "stencilOpFront",
      [2965]: "stencilOpFront",
      [2966]: "stencilOpFront",
      [34817]: "stencilOpBack",
      [34818]: "stencilOpBack",
      [34819]: "stencilOpBack",
      [2978]: (gl, value) => gl.viewport(...value),
      // WEBGL2 EXTENSIONS
      // EXT_depth_clamp https://registry.khronos.org/webgl/extensions/EXT_depth_clamp/
      [34383]: enable,
      // WEBGL_provoking_vertex https://registry.khronos.org/webgl/extensions/WEBGL_provoking_vertex/
      // [GL.PROVOKING_VERTEX_WEBL]: TODO - extension function needed
      // WEBGL_polygon_mode https://registry.khronos.org/webgl/extensions/WEBGL_polygon_mode/
      // POLYGON_MODE_WEBGL  TODO - extension function needed
      [10754]: enable,
      // WEBGL_clip_cull_distance https://registry.khronos.org/webgl/extensions/WEBGL_clip_cull_distance/
      [12288]: enable,
      [12289]: enable,
      [12290]: enable,
      [12291]: enable,
      [12292]: enable,
      [12293]: enable,
      [12294]: enable,
      [12295]: enable,
      // PIXEL PACK/UNPACK MODES
      [3333]: pixelStorei,
      [3317]: pixelStorei,
      [37440]: pixelStorei,
      [37441]: pixelStorei,
      [37443]: pixelStorei,
      [3330]: pixelStorei,
      [3332]: pixelStorei,
      [3331]: pixelStorei,
      [3314]: pixelStorei,
      [32878]: pixelStorei,
      [3316]: pixelStorei,
      [3315]: pixelStorei,
      [32877]: pixelStorei,
      // Function-style setters
      framebuffer: (gl, framebuffer) => {
        const handle = framebuffer && "handle" in framebuffer ? framebuffer.handle : framebuffer;
        return gl.bindFramebuffer(36160, handle);
      },
      blend: (gl, value) => value ? gl.enable(3042) : gl.disable(3042),
      blendColor: (gl, value) => gl.blendColor(...value),
      blendEquation: (gl, args) => {
        const separateModes = typeof args === "number" ? [args, args] : args;
        gl.blendEquationSeparate(...separateModes);
      },
      blendFunc: (gl, args) => {
        const separateFuncs = (args == null ? void 0 : args.length) === 2 ? [...args, ...args] : args;
        gl.blendFuncSeparate(...separateFuncs);
      },
      clearColor: (gl, value) => gl.clearColor(...value),
      clearDepth: (gl, value) => gl.clearDepth(value),
      clearStencil: (gl, value) => gl.clearStencil(value),
      colorMask: (gl, value) => gl.colorMask(...value),
      cull: (gl, value) => value ? gl.enable(2884) : gl.disable(2884),
      cullFace: (gl, value) => gl.cullFace(value),
      depthTest: (gl, value) => value ? gl.enable(2929) : gl.disable(2929),
      depthFunc: (gl, value) => gl.depthFunc(value),
      depthMask: (gl, value) => gl.depthMask(value),
      depthRange: (gl, value) => gl.depthRange(...value),
      dither: (gl, value) => value ? gl.enable(3024) : gl.disable(3024),
      derivativeHint: (gl, value) => {
        gl.hint(35723, value);
      },
      frontFace: (gl, value) => gl.frontFace(value),
      mipmapHint: (gl, value) => gl.hint(33170, value),
      lineWidth: (gl, value) => gl.lineWidth(value),
      polygonOffsetFill: (gl, value) => value ? gl.enable(32823) : gl.disable(32823),
      polygonOffset: (gl, value) => gl.polygonOffset(...value),
      sampleCoverage: (gl, value) => gl.sampleCoverage(value[0], value[1] || false),
      scissorTest: (gl, value) => value ? gl.enable(3089) : gl.disable(3089),
      scissor: (gl, value) => gl.scissor(...value),
      stencilTest: (gl, value) => value ? gl.enable(2960) : gl.disable(2960),
      stencilMask: (gl, value) => {
        value = isArray(value) ? value : [value, value];
        const [mask, backMask] = value;
        gl.stencilMaskSeparate(1028, mask);
        gl.stencilMaskSeparate(1029, backMask);
      },
      stencilFunc: (gl, args) => {
        args = isArray(args) && args.length === 3 ? [...args, ...args] : args;
        const [func, ref, mask, backFunc, backRef, backMask] = args;
        gl.stencilFuncSeparate(1028, func, ref, mask);
        gl.stencilFuncSeparate(1029, backFunc, backRef, backMask);
      },
      stencilOp: (gl, args) => {
        args = isArray(args) && args.length === 3 ? [...args, ...args] : args;
        const [sfail, dpfail, dppass, backSfail, backDpfail, backDppass] = args;
        gl.stencilOpSeparate(1028, sfail, dpfail, dppass);
        gl.stencilOpSeparate(1029, backSfail, backDpfail, backDppass);
      },
      viewport: (gl, value) => gl.viewport(...value)
    };
    GL_COMPOSITE_PARAMETER_SETTERS = {
      blendEquation: (gl, values, cache) => gl.blendEquationSeparate(getValue(32777, values, cache), getValue(34877, values, cache)),
      blendFunc: (gl, values, cache) => gl.blendFuncSeparate(getValue(32969, values, cache), getValue(32968, values, cache), getValue(32971, values, cache), getValue(32970, values, cache)),
      polygonOffset: (gl, values, cache) => gl.polygonOffset(getValue(32824, values, cache), getValue(10752, values, cache)),
      sampleCoverage: (gl, values, cache) => gl.sampleCoverage(getValue(32938, values, cache), getValue(32939, values, cache)),
      stencilFuncFront: (gl, values, cache) => gl.stencilFuncSeparate(1028, getValue(2962, values, cache), getValue(2967, values, cache), getValue(2963, values, cache)),
      stencilFuncBack: (gl, values, cache) => gl.stencilFuncSeparate(1029, getValue(34816, values, cache), getValue(36003, values, cache), getValue(36004, values, cache)),
      stencilOpFront: (gl, values, cache) => gl.stencilOpSeparate(1028, getValue(2964, values, cache), getValue(2965, values, cache), getValue(2966, values, cache)),
      stencilOpBack: (gl, values, cache) => gl.stencilOpSeparate(1029, getValue(34817, values, cache), getValue(34818, values, cache), getValue(34819, values, cache))
    };
    GL_HOOKED_SETTERS = {
      // GENERIC SETTERS
      enable: (update, capability) => update({
        [capability]: true
      }),
      disable: (update, capability) => update({
        [capability]: false
      }),
      pixelStorei: (update, pname, value) => update({
        [pname]: value
      }),
      hint: (update, pname, value) => update({
        [pname]: value
      }),
      // SPECIFIC SETTERS
      useProgram: (update, value) => update({
        [35725]: value
      }),
      bindRenderbuffer: (update, target2, value) => update({
        [36007]: value
      }),
      bindTransformFeedback: (update, target2, value) => update({
        [36389]: value
      }),
      bindVertexArray: (update, value) => update({
        [34229]: value
      }),
      bindFramebuffer: (update, target2, framebuffer) => {
        switch (target2) {
          case 36160:
            return update({
              [36006]: framebuffer,
              [36010]: framebuffer
            });
          case 36009:
            return update({ [36006]: framebuffer });
          case 36008:
            return update({ [36010]: framebuffer });
          default:
            return null;
        }
      },
      bindBuffer: (update, target2, buffer) => {
        const pname = {
          [34962]: [34964],
          [36662]: [36662],
          [36663]: [36663],
          [35051]: [35053],
          [35052]: [35055]
        }[target2];
        if (pname) {
          return update({ [pname]: buffer });
        }
        return { valueChanged: true };
      },
      blendColor: (update, r, g, b, a) => update({
        [32773]: new Float32Array([r, g, b, a])
      }),
      blendEquation: (update, mode) => update({
        [32777]: mode,
        [34877]: mode
      }),
      blendEquationSeparate: (update, modeRGB, modeAlpha) => update({
        [32777]: modeRGB,
        [34877]: modeAlpha
      }),
      blendFunc: (update, src, dst) => update({
        [32969]: src,
        [32968]: dst,
        [32971]: src,
        [32970]: dst
      }),
      blendFuncSeparate: (update, srcRGB, dstRGB, srcAlpha, dstAlpha) => update({
        [32969]: srcRGB,
        [32968]: dstRGB,
        [32971]: srcAlpha,
        [32970]: dstAlpha
      }),
      clearColor: (update, r, g, b, a) => update({
        [3106]: new Float32Array([r, g, b, a])
      }),
      clearDepth: (update, depth) => update({
        [2931]: depth
      }),
      clearStencil: (update, s) => update({
        [2961]: s
      }),
      colorMask: (update, r, g, b, a) => update({
        [3107]: [r, g, b, a]
      }),
      cullFace: (update, mode) => update({
        [2885]: mode
      }),
      depthFunc: (update, func) => update({
        [2932]: func
      }),
      depthRange: (update, zNear, zFar) => update({
        [2928]: new Float32Array([zNear, zFar])
      }),
      depthMask: (update, mask) => update({
        [2930]: mask
      }),
      frontFace: (update, face) => update({
        [2886]: face
      }),
      lineWidth: (update, width) => update({
        [2849]: width
      }),
      polygonOffset: (update, factor, units) => update({
        [32824]: factor,
        [10752]: units
      }),
      sampleCoverage: (update, value, invert) => update({
        [32938]: value,
        [32939]: invert
      }),
      scissor: (update, x, y, width, height) => update({
        [3088]: new Int32Array([x, y, width, height])
      }),
      stencilMask: (update, mask) => update({
        [2968]: mask,
        [36005]: mask
      }),
      stencilMaskSeparate: (update, face, mask) => update({
        [face === 1028 ? 2968 : 36005]: mask
      }),
      stencilFunc: (update, func, ref, mask) => update({
        [2962]: func,
        [2967]: ref,
        [2963]: mask,
        [34816]: func,
        [36003]: ref,
        [36004]: mask
      }),
      stencilFuncSeparate: (update, face, func, ref, mask) => update({
        [face === 1028 ? 2962 : 34816]: func,
        [face === 1028 ? 2967 : 36003]: ref,
        [face === 1028 ? 2963 : 36004]: mask
      }),
      stencilOp: (update, fail, zfail, zpass) => update({
        [2964]: fail,
        [2965]: zfail,
        [2966]: zpass,
        [34817]: fail,
        [34818]: zfail,
        [34819]: zpass
      }),
      stencilOpSeparate: (update, face, fail, zfail, zpass) => update({
        [face === 1028 ? 2964 : 34817]: fail,
        [face === 1028 ? 2965 : 34818]: zfail,
        [face === 1028 ? 2966 : 34819]: zpass
      }),
      viewport: (update, x, y, width, height) => update({
        [2978]: [x, y, width, height]
      })
    };
    isEnabled = (gl, key) => gl.isEnabled(key);
    GL_PARAMETER_GETTERS = {
      [3042]: isEnabled,
      [2884]: isEnabled,
      [2929]: isEnabled,
      [3024]: isEnabled,
      [32823]: isEnabled,
      [32926]: isEnabled,
      [32928]: isEnabled,
      [3089]: isEnabled,
      [2960]: isEnabled,
      [35977]: isEnabled
    };
    NON_CACHE_PARAMETERS = /* @__PURE__ */ new Set([
      34016,
      36388,
      36387,
      35983,
      35368,
      34965,
      35739,
      35738,
      3074,
      34853,
      34854,
      34855,
      34856,
      34857,
      34858,
      34859,
      34860,
      34861,
      34862,
      34863,
      34864,
      34865,
      34866,
      34867,
      34868,
      35097,
      32873,
      35869,
      32874,
      34068
    ]);
  }
});

// dist/context/parameters/unified-parameter-api.js
function setGLParameters(gl, parameters) {
  if (isObjectEmpty(parameters)) {
    return;
  }
  const compositeSetters = {};
  for (const key in parameters) {
    const glConstant = Number(key);
    const setter = GL_PARAMETER_SETTERS[key];
    if (setter) {
      if (typeof setter === "string") {
        compositeSetters[setter] = true;
      } else {
        setter(gl, parameters[key], glConstant);
      }
    }
  }
  const cache = gl.state && gl.state.cache;
  if (cache) {
    for (const key in compositeSetters) {
      const compositeSetter = GL_COMPOSITE_PARAMETER_SETTERS[key];
      compositeSetter(gl, parameters, cache);
    }
  }
}
function getGLParameters(gl, parameters = GL_PARAMETER_DEFAULTS) {
  if (typeof parameters === "number") {
    const key = parameters;
    const getter = GL_PARAMETER_GETTERS[key];
    return getter ? getter(gl, key) : gl.getParameter(key);
  }
  const parameterKeys = Array.isArray(parameters) ? parameters : Object.keys(parameters);
  const state = {};
  for (const key of parameterKeys) {
    const getter = GL_PARAMETER_GETTERS[key];
    state[key] = getter ? getter(gl, Number(key)) : gl.getParameter(Number(key));
  }
  return state;
}
function resetGLParameters(gl) {
  setGLParameters(gl, GL_PARAMETER_DEFAULTS);
}
function isObjectEmpty(object) {
  for (const key in object) {
    return false;
  }
  return true;
}
var init_unified_parameter_api = __esm({
  "dist/context/parameters/unified-parameter-api.js"() {
    "use strict";
    init_webgl_parameter_tables();
  }
});

// dist/context/state-tracker/deep-array-equal.js
function deepArrayEqual(x, y) {
  if (x === y) {
    return true;
  }
  if (isArray2(x) && isArray2(y) && x.length === y.length) {
    for (let i = 0; i < x.length; ++i) {
      if (x[i] !== y[i]) {
        return false;
      }
    }
    return true;
  }
  return false;
}
function isArray2(x) {
  return Array.isArray(x) || ArrayBuffer.isView(x);
}
var init_deep_array_equal = __esm({
  "dist/context/state-tracker/deep-array-equal.js"() {
    "use strict";
  }
});

// dist/context/state-tracker/webgl-state-tracker.js
function installGetterOverride(gl, functionName) {
  const originalGetterFunc = gl[functionName].bind(gl);
  gl[functionName] = function get(pname) {
    if (pname === void 0 || NON_CACHE_PARAMETERS.has(pname)) {
      return originalGetterFunc(pname);
    }
    const glState = WebGLStateTracker.get(gl);
    if (!(pname in glState.cache)) {
      glState.cache[pname] = originalGetterFunc(pname);
    }
    return glState.enable ? (
      // Call the getter the params so that it can e.g. serve from a cache
      glState.cache[pname]
    ) : (
      // Optionally call the original function to do a "hard" query from the WebGL2RenderingContext
      originalGetterFunc(pname)
    );
  };
  Object.defineProperty(gl[functionName], "name", {
    value: `${functionName}-from-cache`,
    configurable: false
  });
}
function installSetterSpy(gl, functionName, setter) {
  if (!gl[functionName]) {
    return;
  }
  const originalSetterFunc = gl[functionName].bind(gl);
  gl[functionName] = function set(...params) {
    const glState = WebGLStateTracker.get(gl);
    const { valueChanged, oldValue } = setter(glState._updateCache, ...params);
    if (valueChanged) {
      originalSetterFunc(...params);
    }
    return oldValue;
  };
  Object.defineProperty(gl[functionName], "name", {
    value: `${functionName}-to-cache`,
    configurable: false
  });
}
function installProgramSpy(gl) {
  const originalUseProgram = gl.useProgram.bind(gl);
  gl.useProgram = function useProgramLuma(handle) {
    const glState = WebGLStateTracker.get(gl);
    if (glState.program !== handle) {
      originalUseProgram(handle);
      glState.program = handle;
    }
  };
}
var WebGLStateTracker;
var init_webgl_state_tracker = __esm({
  "dist/context/state-tracker/webgl-state-tracker.js"() {
    "use strict";
    init_unified_parameter_api();
    init_deep_array_equal();
    init_webgl_parameter_tables();
    WebGLStateTracker = class {
      static get(gl) {
        return gl.state;
      }
      gl;
      program = null;
      stateStack = [];
      enable = true;
      cache = null;
      log;
      initialized = false;
      constructor(gl, props) {
        this.gl = gl;
        this.log = (props == null ? void 0 : props.log) || (() => {
        });
        this._updateCache = this._updateCache.bind(this);
        Object.seal(this);
      }
      push(values = {}) {
        this.stateStack.push({});
      }
      pop() {
        const oldValues = this.stateStack[this.stateStack.length - 1];
        setGLParameters(this.gl, oldValues);
        this.stateStack.pop();
      }
      /**
       * Initialize WebGL state caching on a context
       * can be called multiple times to enable/disable
       *
       * @note After calling this function, context state will be cached
       * .push() and .pop() will be available for saving,
       * temporarily modifying, and then restoring state.
       */
      trackState(gl, options) {
        this.cache = (options == null ? void 0 : options.copyState) ? getGLParameters(gl) : Object.assign({}, GL_PARAMETER_DEFAULTS);
        if (this.initialized) {
          throw new Error("WebGLStateTracker");
        }
        this.initialized = true;
        this.gl.state = this;
        installProgramSpy(gl);
        for (const key in GL_HOOKED_SETTERS) {
          const setter = GL_HOOKED_SETTERS[key];
          installSetterSpy(gl, key, setter);
        }
        installGetterOverride(gl, "getParameter");
        installGetterOverride(gl, "isEnabled");
      }
      /**
      // interceptor for context set functions - update our cache and our stack
      // values (Object) - the key values for this setter
       * @param values
       * @returns
       */
      _updateCache(values) {
        let valueChanged = false;
        let oldValue;
        const oldValues = this.stateStack.length > 0 ? this.stateStack[this.stateStack.length - 1] : null;
        for (const key in values) {
          const value = values[key];
          const cached = this.cache[key];
          if (!deepArrayEqual(value, cached)) {
            valueChanged = true;
            oldValue = cached;
            if (oldValues && !(key in oldValues)) {
              oldValues[key] = cached;
            }
            this.cache[key] = value;
          }
        }
        return { valueChanged, oldValue };
      }
    };
  }
});

// dist/context/helpers/create-browser-context.js
function createBrowserContext(canvas, props, webglContextAttributes) {
  let errorMessage = "";
  const webglProps = {
    preserveDrawingBuffer: true,
    // failIfMajorPerformanceCaveat: true,
    ...webglContextAttributes
  };
  let gl = null;
  gl ||= canvas.getContext("webgl2", webglProps);
  if (webglProps.failIfMajorPerformanceCaveat) {
    errorMessage ||= "Only software GPU is available. Set `failIfMajorPerformanceCaveat: false` to allow.";
  }
  if (!gl && !webglContextAttributes.failIfMajorPerformanceCaveat) {
    webglProps.failIfMajorPerformanceCaveat = false;
    gl = canvas.getContext("webgl2", webglProps);
    gl.luma ||= {};
    gl.luma.softwareRenderer = true;
  }
  if (!gl) {
    gl = canvas.getContext("webgl", {});
    if (gl) {
      gl = null;
      errorMessage ||= "Your browser only supports WebGL1";
    }
  }
  if (!gl) {
    errorMessage ||= "Your browser does not support WebGL";
    throw new Error(`Failed to create WebGL context: ${errorMessage}`);
  }
  const { onContextLost, onContextRestored } = props;
  canvas.addEventListener("webglcontextlost", (event) => onContextLost(event), false);
  canvas.addEventListener("webglcontextrestored", (event) => onContextRestored(event), false);
  gl.luma ||= {};
  return gl;
}
var init_create_browser_context = __esm({
  "dist/context/helpers/create-browser-context.js"() {
    "use strict";
  }
});

// dist/context/helpers/webgl-extensions.js
function getWebGLExtension(gl, name, extensions) {
  if (extensions[name] === void 0) {
    extensions[name] = gl.getExtension(name) || null;
  }
  return extensions[name];
}
var init_webgl_extensions = __esm({
  "dist/context/helpers/webgl-extensions.js"() {
    "use strict";
  }
});

// dist/adapter/device-helpers/webgl-device-info.js
function getDeviceInfo(gl, extensions) {
  const vendorMasked = gl.getParameter(7936);
  const rendererMasked = gl.getParameter(7937);
  getWebGLExtension(gl, "WEBGL_debug_renderer_info", extensions);
  const ext = extensions.WEBGL_debug_renderer_info;
  const vendorUnmasked = gl.getParameter(ext ? ext.UNMASKED_VENDOR_WEBGL : 7936);
  const rendererUnmasked = gl.getParameter(ext ? ext.UNMASKED_RENDERER_WEBGL : 7937);
  const vendor = vendorUnmasked || vendorMasked;
  const renderer = rendererUnmasked || rendererMasked;
  const version = gl.getParameter(7938);
  const gpu = identifyGPUVendor(vendor, renderer);
  const gpuBackend = identifyGPUBackend(vendor, renderer);
  const gpuType = identifyGPUType(vendor, renderer);
  const shadingLanguage = "glsl";
  const shadingLanguageVersion = 300;
  return {
    type: "webgl",
    gpu,
    gpuType,
    gpuBackend,
    vendor,
    renderer,
    version,
    shadingLanguage,
    shadingLanguageVersion
  };
}
function identifyGPUVendor(vendor, renderer) {
  if (/NVIDIA/i.exec(vendor) || /NVIDIA/i.exec(renderer)) {
    return "nvidia";
  }
  if (/INTEL/i.exec(vendor) || /INTEL/i.exec(renderer)) {
    return "intel";
  }
  if (/Apple/i.exec(vendor) || /Apple/i.exec(renderer)) {
    return "apple";
  }
  if (/AMD/i.exec(vendor) || /AMD/i.exec(renderer) || /ATI/i.exec(vendor) || /ATI/i.exec(renderer)) {
    return "amd";
  }
  if (/SwiftShader/i.exec(vendor) || /SwiftShader/i.exec(renderer)) {
    return "software";
  }
  return "unknown";
}
function identifyGPUBackend(vendor, renderer) {
  if (/Metal/i.exec(vendor) || /Metal/i.exec(renderer)) {
    return "metal";
  }
  if (/ANGLE/i.exec(vendor) || /ANGLE/i.exec(renderer)) {
    return "opengl";
  }
  return "unknown";
}
function identifyGPUType(vendor, renderer) {
  if (/SwiftShader/i.exec(vendor) || /SwiftShader/i.exec(renderer)) {
    return "cpu";
  }
  const gpuVendor = identifyGPUVendor(vendor, renderer);
  switch (gpuVendor) {
    case "intel":
      return "integrated";
    case "software":
      return "cpu";
    case "unknown":
      return "unknown";
    default:
      return "discrete";
  }
}
var import_constants4;
var init_webgl_device_info = __esm({
  "dist/adapter/device-helpers/webgl-device-info.js"() {
    "use strict";
    import_constants4 = require("@luma.gl/constants");
    init_webgl_extensions();
  }
});

// dist/adapter/converters/webgl-vertex-formats.js
function getGLFromVertexType(dataType) {
  switch (dataType) {
    case "uint8":
      return 5121;
    case "sint8":
      return 5120;
    case "unorm8":
      return 5121;
    case "snorm8":
      return 5120;
    case "uint16":
      return 5123;
    case "sint16":
      return 5122;
    case "unorm16":
      return 5123;
    case "snorm16":
      return 5122;
    case "uint32":
      return 5125;
    case "sint32":
      return 5124;
    case "float16":
      return 5131;
    case "float32":
      return 5126;
  }
  throw new Error(String(dataType));
}
var import_constants5;
var init_webgl_vertex_formats = __esm({
  "dist/adapter/converters/webgl-vertex-formats.js"() {
    "use strict";
    import_constants5 = require("@luma.gl/constants");
  }
});

// dist/adapter/converters/webgl-texture-table.js
function isTextureFeature(feature) {
  return feature in TEXTURE_FEATURES;
}
function checkTextureFeature(gl, feature, extensions) {
  const textureExtensions = TEXTURE_FEATURES[feature] || [];
  return textureExtensions.every((extension) => getWebGLExtension(gl, extension, extensions));
}
function getTextureFormatCapabilitiesWebGL(gl, formatSupport, extensions) {
  let supported = formatSupport.create;
  const webglFormatInfo = WEBGL_TEXTURE_FORMATS[formatSupport.format];
  if ((webglFormatInfo == null ? void 0 : webglFormatInfo.gl) === void 0) {
    supported = false;
  }
  if (webglFormatInfo == null ? void 0 : webglFormatInfo.x) {
    supported = supported && Boolean(getWebGLExtension(gl, webglFormatInfo.x, extensions));
  }
  return {
    format: formatSupport.format,
    // @ts-ignore
    create: supported && formatSupport.create,
    // @ts-ignore
    render: supported && formatSupport.render,
    // @ts-ignore
    filter: supported && formatSupport.filter,
    // @ts-ignore
    blend: supported && formatSupport.blend,
    // @ts-ignore
    store: supported && formatSupport.store
  };
}
function getTextureFormatWebGL(format) {
  var _a;
  const formatData = WEBGL_TEXTURE_FORMATS[format];
  const webglFormat = convertTextureFormatToGL(format);
  const decoded = import_core3.textureFormatDecoder.getInfo(format);
  if (decoded.compressed) {
    formatData.dataFormat = webglFormat;
  }
  return {
    internalFormat: webglFormat,
    format: (formatData == null ? void 0 : formatData.dataFormat) || getWebGLPixelDataFormat(decoded.channels, decoded.integer, decoded.normalized, webglFormat),
    // depth formats don't have a type
    type: decoded.dataType ? getGLFromVertexType(decoded.dataType) : ((_a = formatData == null ? void 0 : formatData.types) == null ? void 0 : _a[0]) || 5121,
    compressed: decoded.compressed || false
  };
}
function getDepthStencilAttachmentWebGL(format) {
  const formatInfo = import_core3.textureFormatDecoder.getInfo(format);
  switch (formatInfo.attachment) {
    case "depth":
      return 36096;
    case "stencil":
      return 36128;
    case "depth-stencil":
      return 33306;
    default:
      throw new Error(`Not a depth stencil format: ${format}`);
  }
}
function getWebGLPixelDataFormat(channels, integer, normalized, format) {
  if (format === 6408 || format === 6407) {
    return format;
  }
  switch (channels) {
    case "r":
      return integer && !normalized ? 36244 : 6403;
    case "rg":
      return integer && !normalized ? 33320 : 33319;
    case "rgb":
      return integer && !normalized ? 36248 : 6407;
    case "rgba":
      return integer && !normalized ? 36249 : 6408;
    case "bgra":
      throw new Error("bgra pixels not supported by WebGL");
    default:
      return 6408;
  }
}
function convertTextureFormatToGL(format) {
  const formatInfo = WEBGL_TEXTURE_FORMATS[format];
  const webglFormat = formatInfo == null ? void 0 : formatInfo.gl;
  if (webglFormat === void 0) {
    throw new Error(`Unsupported texture format ${format}`);
  }
  return webglFormat;
}
var import_core3, import_constants6, X_S3TC, X_S3TC_SRGB, X_RGTC, X_BPTC, X_ETC2, X_ASTC, X_ETC1, X_PVRTC, X_ATC, EXT_texture_norm16, EXT_render_snorm, EXT_color_buffer_float, TEXTURE_FEATURES, WEBGL_TEXTURE_FORMATS;
var init_webgl_texture_table = __esm({
  "dist/adapter/converters/webgl-texture-table.js"() {
    "use strict";
    import_core3 = require("@luma.gl/core");
    import_constants6 = require("@luma.gl/constants");
    init_webgl_extensions();
    init_webgl_vertex_formats();
    X_S3TC = "WEBGL_compressed_texture_s3tc";
    X_S3TC_SRGB = "WEBGL_compressed_texture_s3tc_srgb";
    X_RGTC = "EXT_texture_compression_rgtc";
    X_BPTC = "EXT_texture_compression_bptc";
    X_ETC2 = "WEBGL_compressed_texture_etc";
    X_ASTC = "WEBGL_compressed_texture_astc";
    X_ETC1 = "WEBGL_compressed_texture_etc1";
    X_PVRTC = "WEBGL_compressed_texture_pvrtc";
    X_ATC = "WEBGL_compressed_texture_atc";
    EXT_texture_norm16 = "EXT_texture_norm16";
    EXT_render_snorm = "EXT_render_snorm";
    EXT_color_buffer_float = "EXT_color_buffer_float";
    TEXTURE_FEATURES = {
      "float32-renderable-webgl": ["EXT_color_buffer_float"],
      "float16-renderable-webgl": ["EXT_color_buffer_half_float"],
      "rgb9e5ufloat-renderable-webgl": ["WEBGL_render_shared_exponent"],
      "snorm8-renderable-webgl": [EXT_render_snorm],
      "norm16-renderable-webgl": [EXT_texture_norm16],
      "snorm16-renderable-webgl": [EXT_texture_norm16, EXT_render_snorm],
      "float32-filterable": ["OES_texture_float_linear"],
      "float16-filterable-webgl": ["OES_texture_half_float_linear"],
      "texture-filterable-anisotropic-webgl": ["EXT_texture_filter_anisotropic"],
      "texture-blend-float-webgl": ["EXT_float_blend"],
      "texture-compression-bc": [X_S3TC, X_S3TC_SRGB, X_RGTC, X_BPTC],
      // 'texture-compression-bc3-srgb-webgl': [X_S3TC_SRGB],
      // 'texture-compression-bc3-webgl': [X_S3TC],
      "texture-compression-bc5-webgl": [X_RGTC],
      "texture-compression-bc7-webgl": [X_BPTC],
      "texture-compression-etc2": [X_ETC2],
      "texture-compression-astc": [X_ASTC],
      "texture-compression-etc1-webgl": [X_ETC1],
      "texture-compression-pvrtc-webgl": [X_PVRTC],
      "texture-compression-atc-webgl": [X_ATC]
    };
    WEBGL_TEXTURE_FORMATS = {
      // 8-bit formats
      "r8unorm": { gl: 33321, rb: true },
      "r8snorm": { gl: 36756 },
      "r8uint": { gl: 33330, rb: true },
      "r8sint": { gl: 33329, rb: true },
      // 16-bit formats
      "rg8unorm": { gl: 33323, rb: true },
      "rg8snorm": { gl: 36757 },
      "rg8uint": { gl: 33336, rb: true },
      "rg8sint": { gl: 33335, rb: true },
      "r16uint": { gl: 33332, rb: true },
      "r16sint": { gl: 33331, rb: true },
      "r16float": { gl: 33325, rb: true },
      "r16unorm": { gl: 33322, rb: true },
      "r16snorm": { gl: 36760 },
      // Packed 16-bit formats
      "rgba4unorm-webgl": { gl: 32854, rb: true },
      "rgb565unorm-webgl": { gl: 36194, rb: true },
      "rgb5a1unorm-webgl": { gl: 32855, rb: true },
      // 24-bit formats
      "rgb8unorm-webgl": { gl: 32849 },
      "rgb8snorm-webgl": { gl: 36758 },
      // 32-bit formats
      "rgba8unorm": { gl: 32856 },
      "rgba8unorm-srgb": { gl: 35907 },
      "rgba8snorm": { gl: 36759 },
      "rgba8uint": { gl: 36220 },
      "rgba8sint": { gl: 36238 },
      // reverse colors, webgpu only
      "bgra8unorm": {},
      "bgra8unorm-srgb": {},
      "rg16uint": { gl: 33338 },
      "rg16sint": { gl: 33337 },
      "rg16float": { gl: 33327, rb: true },
      "rg16unorm": { gl: 33324 },
      "rg16snorm": { gl: 36761 },
      "r32uint": { gl: 33334, rb: true },
      "r32sint": { gl: 33333, rb: true },
      "r32float": { gl: 33326 },
      // Packed 32-bit formats
      "rgb9e5ufloat": { gl: 35901 },
      // , filter: true},
      "rg11b10ufloat": { gl: 35898, rb: true },
      "rgb10a2unorm": { gl: 32857, rb: true },
      "rgb10a2uint": { gl: 36975, rb: true },
      // 48-bit formats
      "rgb16unorm-webgl": { gl: 32852 },
      // rgb not renderable
      "rgb16snorm-webgl": { gl: 36762 },
      // rgb not renderable
      // 64-bit formats
      "rg32uint": { gl: 33340, rb: true },
      "rg32sint": { gl: 33339, rb: true },
      "rg32float": { gl: 33328, rb: true },
      "rgba16uint": { gl: 36214, rb: true },
      "rgba16sint": { gl: 36232, rb: true },
      "rgba16float": { gl: 34842 },
      "rgba16unorm": { gl: 32859, rb: true },
      "rgba16snorm": { gl: 36763 },
      // 96-bit formats (deprecated!)
      "rgb32float-webgl": { gl: 34837, x: EXT_color_buffer_float, dataFormat: 6407, types: [5126] },
      // 128-bit formats
      "rgba32uint": { gl: 36208, rb: true },
      "rgba32sint": { gl: 36226, rb: true },
      "rgba32float": { gl: 34836, rb: true },
      // Depth and stencil formats
      "stencil8": { gl: 36168, rb: true },
      // 8 stencil bits
      "depth16unorm": { gl: 33189, dataFormat: 6402, types: [5123], rb: true },
      // 16 depth bits
      "depth24plus": { gl: 33190, dataFormat: 6402, types: [5125] },
      "depth32float": { gl: 36012, dataFormat: 6402, types: [5126], rb: true },
      // The depth component of the "depth24plus" and "depth24plus-stencil8" formats may be implemented as either a 24-bit depth value or a "depth32float" value.
      "depth24plus-stencil8": { gl: 35056, rb: true, depthTexture: true, dataFormat: 34041, types: [34042] },
      // "depth32float-stencil8" feature - TODO below is render buffer only?
      "depth32float-stencil8": { gl: 36013, dataFormat: 34041, types: [36269], rb: true },
      // BC compressed formats: check device.features.has("texture-compression-bc");
      "bc1-rgb-unorm-webgl": { gl: 33776, x: X_S3TC },
      "bc1-rgb-unorm-srgb-webgl": { gl: 35916, x: X_S3TC_SRGB },
      "bc1-rgba-unorm": { gl: 33777, x: X_S3TC },
      "bc1-rgba-unorm-srgb": { gl: 35916, x: X_S3TC_SRGB },
      "bc2-rgba-unorm": { gl: 33778, x: X_S3TC },
      "bc2-rgba-unorm-srgb": { gl: 35918, x: X_S3TC_SRGB },
      "bc3-rgba-unorm": { gl: 33779, x: X_S3TC },
      "bc3-rgba-unorm-srgb": { gl: 35919, x: X_S3TC_SRGB },
      "bc4-r-unorm": { gl: 36283, x: X_RGTC },
      "bc4-r-snorm": { gl: 36284, x: X_RGTC },
      "bc5-rg-unorm": { gl: 36285, x: X_RGTC },
      "bc5-rg-snorm": { gl: 36286, x: X_RGTC },
      "bc6h-rgb-ufloat": { gl: 36495, x: X_BPTC },
      "bc6h-rgb-float": { gl: 36494, x: X_BPTC },
      "bc7-rgba-unorm": { gl: 36492, x: X_BPTC },
      "bc7-rgba-unorm-srgb": { gl: 36493, x: X_BPTC },
      // WEBGL_compressed_texture_etc: device.features.has("texture-compression-etc2")
      // Note: Supposedly guaranteed availability compressed formats in WebGL2, but through CPU decompression
      "etc2-rgb8unorm": { gl: 37492 },
      "etc2-rgb8unorm-srgb": { gl: 37494 },
      "etc2-rgb8a1unorm": { gl: 37496 },
      "etc2-rgb8a1unorm-srgb": { gl: 37497 },
      "etc2-rgba8unorm": { gl: 37493 },
      "etc2-rgba8unorm-srgb": { gl: 37495 },
      "eac-r11unorm": { gl: 37488 },
      "eac-r11snorm": { gl: 37489 },
      "eac-rg11unorm": { gl: 37490 },
      "eac-rg11snorm": { gl: 37491 },
      // X_ASTC compressed formats: device.features.has("texture-compression-astc")
      "astc-4x4-unorm": { gl: 37808 },
      "astc-4x4-unorm-srgb": { gl: 37840 },
      "astc-5x4-unorm": { gl: 37809 },
      "astc-5x4-unorm-srgb": { gl: 37841 },
      "astc-5x5-unorm": { gl: 37810 },
      "astc-5x5-unorm-srgb": { gl: 37842 },
      "astc-6x5-unorm": { gl: 37811 },
      "astc-6x5-unorm-srgb": { gl: 37843 },
      "astc-6x6-unorm": { gl: 37812 },
      "astc-6x6-unorm-srgb": { gl: 37844 },
      "astc-8x5-unorm": { gl: 37813 },
      "astc-8x5-unorm-srgb": { gl: 37845 },
      "astc-8x6-unorm": { gl: 37814 },
      "astc-8x6-unorm-srgb": { gl: 37846 },
      "astc-8x8-unorm": { gl: 37815 },
      "astc-8x8-unorm-srgb": { gl: 37847 },
      "astc-10x5-unorm": { gl: 37819 },
      "astc-10x5-unorm-srgb": { gl: 37851 },
      "astc-10x6-unorm": { gl: 37817 },
      "astc-10x6-unorm-srgb": { gl: 37849 },
      "astc-10x8-unorm": { gl: 37818 },
      "astc-10x8-unorm-srgb": { gl: 37850 },
      "astc-10x10-unorm": { gl: 37819 },
      "astc-10x10-unorm-srgb": { gl: 37851 },
      "astc-12x10-unorm": { gl: 37820 },
      "astc-12x10-unorm-srgb": { gl: 37852 },
      "astc-12x12-unorm": { gl: 37821 },
      "astc-12x12-unorm-srgb": { gl: 37853 },
      // WEBGL_compressed_texture_pvrtc
      "pvrtc-rgb4unorm-webgl": { gl: 35840 },
      "pvrtc-rgba4unorm-webgl": { gl: 35842 },
      "pvrtc-rbg2unorm-webgl": { gl: 35841 },
      "pvrtc-rgba2unorm-webgl": { gl: 35843 },
      // WEBGL_compressed_texture_etc1
      "etc1-rbg-unorm-webgl": { gl: 36196 },
      // WEBGL_compressed_texture_atc
      "atc-rgb-unorm-webgl": { gl: 35986 },
      "atc-rgba-unorm-webgl": { gl: 35986 },
      "atc-rgbai-unorm-webgl": { gl: 34798 }
    };
  }
});

// dist/adapter/device-helpers/webgl-device-features.js
var import_core4, WEBGL_FEATURES, WebGLDeviceFeatures;
var init_webgl_device_features = __esm({
  "dist/adapter/device-helpers/webgl-device-features.js"() {
    "use strict";
    import_core4 = require("@luma.gl/core");
    init_webgl_extensions();
    init_webgl_texture_table();
    WEBGL_FEATURES = {
      // optional WebGPU features
      "depth-clip-control": "EXT_depth_clamp",
      // TODO these seem subtly different
      // 'timestamp-query' // GPUQueryType "timestamp-query"
      // "indirect-first-instance"
      // Textures are handled by getTextureFeatures()
      // 'depth32float-stencil8' // GPUTextureFormat 'depth32float-stencil8'
      // optional WebGL features
      "timer-query-webgl": "EXT_disjoint_timer_query_webgl2",
      "compilation-status-async-webgl": "KHR_parallel_shader_compile",
      "polygon-mode-webgl": "WEBGL_polygon_mode",
      "provoking-vertex-webgl": "WEBGL_provoking_vertex",
      "shader-clip-cull-distance-webgl": "WEBGL_clip_cull_distance",
      "shader-noperspective-interpolation-webgl": "NV_shader_noperspective_interpolation",
      "shader-conservative-depth-webgl": "EXT_conservative_depth"
      // Textures are handled by getTextureFeatures()
    };
    WebGLDeviceFeatures = class extends import_core4.DeviceFeatures {
      gl;
      extensions;
      testedFeatures = /* @__PURE__ */ new Set();
      constructor(gl, extensions, disabledFeatures) {
        super([], disabledFeatures);
        this.gl = gl;
        this.extensions = extensions;
        getWebGLExtension(gl, "EXT_color_buffer_float", extensions);
      }
      *[Symbol.iterator]() {
        const features = this.getFeatures();
        for (const feature of features) {
          if (this.has(feature)) {
            yield feature;
          }
        }
        return [];
      }
      has(feature) {
        var _a;
        if ((_a = this.disabledFeatures) == null ? void 0 : _a[feature]) {
          return false;
        }
        if (!this.testedFeatures.has(feature)) {
          this.testedFeatures.add(feature);
          if (isTextureFeature(feature) && checkTextureFeature(this.gl, feature, this.extensions)) {
            this.features.add(feature);
          }
          if (this.getWebGLFeature(feature)) {
            this.features.add(feature);
          }
        }
        return this.features.has(feature);
      }
      // FOR DEVICE
      initializeFeatures() {
        const features = this.getFeatures().filter((feature) => feature !== "polygon-mode-webgl");
        for (const feature of features) {
          this.has(feature);
        }
      }
      // IMPLEMENTATION
      getFeatures() {
        return [...Object.keys(WEBGL_FEATURES), ...Object.keys(TEXTURE_FEATURES)];
      }
      /** Extract all WebGL features */
      getWebGLFeature(feature) {
        const featureInfo = WEBGL_FEATURES[feature];
        const isSupported = typeof featureInfo === "string" ? Boolean(getWebGLExtension(this.gl, featureInfo, this.extensions)) : Boolean(featureInfo);
        return isSupported;
      }
    };
  }
});

// dist/adapter/device-helpers/webgl-device-limits.js
var import_core5, import_constants7, WebGLDeviceLimits;
var init_webgl_device_limits = __esm({
  "dist/adapter/device-helpers/webgl-device-limits.js"() {
    "use strict";
    import_core5 = require("@luma.gl/core");
    import_constants7 = require("@luma.gl/constants");
    WebGLDeviceLimits = class extends import_core5.DeviceLimits {
      get maxTextureDimension1D() {
        return 0;
      }
      // WebGL does not support 1D textures
      get maxTextureDimension2D() {
        return this.getParameter(3379);
      }
      get maxTextureDimension3D() {
        return this.getParameter(32883);
      }
      get maxTextureArrayLayers() {
        return this.getParameter(35071);
      }
      get maxBindGroups() {
        return 0;
      }
      get maxDynamicUniformBuffersPerPipelineLayout() {
        return 0;
      }
      // TBD
      get maxDynamicStorageBuffersPerPipelineLayout() {
        return 0;
      }
      // TBD
      get maxSampledTexturesPerShaderStage() {
        return this.getParameter(35660);
      }
      // ) TBD
      get maxSamplersPerShaderStage() {
        return this.getParameter(35661);
      }
      get maxStorageBuffersPerShaderStage() {
        return 0;
      }
      // TBD
      get maxStorageTexturesPerShaderStage() {
        return 0;
      }
      // TBD
      get maxUniformBuffersPerShaderStage() {
        return this.getParameter(35375);
      }
      get maxUniformBufferBindingSize() {
        return this.getParameter(35376);
      }
      get maxStorageBufferBindingSize() {
        return 0;
      }
      get minUniformBufferOffsetAlignment() {
        return this.getParameter(35380);
      }
      get minStorageBufferOffsetAlignment() {
        return 0;
      }
      get maxVertexBuffers() {
        return 16;
      }
      // WebGL 2 supports 16 buffers, see https://github.com/gpuweb/gpuweb/issues/4284
      get maxVertexAttributes() {
        return this.getParameter(34921);
      }
      get maxVertexBufferArrayStride() {
        return 2048;
      }
      // TBD, this is just the default value from WebGPU
      get maxInterStageShaderVariables() {
        return this.getParameter(35659);
      }
      get maxComputeWorkgroupStorageSize() {
        return 0;
      }
      // WebGL does not support compute shaders
      get maxComputeInvocationsPerWorkgroup() {
        return 0;
      }
      // WebGL does not support compute shaders
      get maxComputeWorkgroupSizeX() {
        return 0;
      }
      // WebGL does not support compute shaders
      get maxComputeWorkgroupSizeY() {
        return 0;
      }
      // WebGL does not support compute shaders
      get maxComputeWorkgroupSizeZ() {
        return 0;
      }
      // WebGL does not support compute shaders
      get maxComputeWorkgroupsPerDimension() {
        return 0;
      }
      // WebGL does not support compute shaders
      // PRIVATE
      gl;
      limits = {};
      constructor(gl) {
        super();
        this.gl = gl;
      }
      getParameter(parameter) {
        if (this.limits[parameter] === void 0) {
          this.limits[parameter] = this.gl.getParameter(parameter);
        }
        return this.limits[parameter] || 0;
      }
    };
  }
});

// dist/adapter/resources/webgl-framebuffer.js
function mapIndexToCubeMapFace(layer) {
  return layer < 34069 ? layer + 34069 : layer;
}
function _getFrameBufferStatus(status) {
  switch (status) {
    case 36053:
      return "success";
    case 36054:
      return "Mismatched attachments";
    case 36055:
      return "No attachments";
    case 36057:
      return "Height/width mismatch";
    case 36061:
      return "Unsupported or split attachments";
    case 36182:
      return "Samples mismatch";
    default:
      return `${status}`;
  }
}
var import_core6, import_constants8, WEBGLFramebuffer;
var init_webgl_framebuffer = __esm({
  "dist/adapter/resources/webgl-framebuffer.js"() {
    "use strict";
    import_core6 = require("@luma.gl/core");
    import_constants8 = require("@luma.gl/constants");
    init_webgl_texture_table();
    WEBGLFramebuffer = class extends import_core6.Framebuffer {
      device;
      gl;
      handle;
      colorAttachments = [];
      depthStencilAttachment = null;
      constructor(device, props) {
        super(device, props);
        const isDefaultFramebuffer = props.handle === null;
        this.device = device;
        this.gl = device.gl;
        this.handle = this.props.handle || isDefaultFramebuffer ? this.props.handle : this.gl.createFramebuffer();
        if (!isDefaultFramebuffer) {
          device._setWebGLDebugMetadata(this.handle, this, { spector: this.props });
          this.autoCreateAttachmentTextures();
          this.updateAttachments();
        }
      }
      /** destroys any auto created resources etc. */
      destroy() {
        super.destroy();
        if (!this.destroyed && this.handle !== null) {
          this.gl.deleteFramebuffer(this.handle);
        }
      }
      updateAttachments() {
        const prevHandle = this.gl.bindFramebuffer(36160, this.handle);
        for (let i = 0; i < this.colorAttachments.length; ++i) {
          const attachment = this.colorAttachments[i];
          if (attachment) {
            const attachmentPoint = 36064 + i;
            this._attachTextureView(attachmentPoint, attachment);
          }
        }
        if (this.depthStencilAttachment) {
          const attachmentPoint = getDepthStencilAttachmentWebGL(this.depthStencilAttachment.props.format);
          this._attachTextureView(attachmentPoint, this.depthStencilAttachment);
        }
        if (this.device.props.debug) {
          const status = this.gl.checkFramebufferStatus(36160);
          if (status !== 36053) {
            throw new Error(`Framebuffer ${_getFrameBufferStatus(status)}`);
          }
        }
        this.gl.bindFramebuffer(36160, prevHandle);
      }
      // PRIVATE
      /** In WebGL we must use renderbuffers for depth/stencil attachments (unless we have extensions) */
      // protected override createDepthStencilTexture(format: TextureFormat): Texture {
      //   // return new WEBGLRenderbuffer(this.device, {
      //   return new WEBGLTexture(this.device, {
      //     id: `${this.id}-depth-stencil`,
      //     format,
      //     width: this.width,
      //     height: this.height,
      //     mipmaps: false
      //   });
      // }
      /**
       * @param attachment
       * @param texture
       * @param layer = 0 - index into WEBGLTextureArray and Texture3D or face for `TextureCubeMap`
       * @param level = 0 - mipmapLevel
       */
      _attachTextureView(attachment, textureView) {
        const { gl } = this.device;
        const { texture } = textureView;
        const level = textureView.props.baseMipLevel;
        const layer = textureView.props.baseArrayLayer;
        gl.bindTexture(texture.glTarget, texture.handle);
        switch (texture.glTarget) {
          case 35866:
          case 32879:
            gl.framebufferTextureLayer(36160, attachment, texture.handle, level, layer);
            break;
          case 34067:
            const face = mapIndexToCubeMapFace(layer);
            gl.framebufferTexture2D(36160, attachment, face, texture.handle, level);
            break;
          case 3553:
            gl.framebufferTexture2D(36160, attachment, 3553, texture.handle, level);
            break;
          default:
            throw new Error("Illegal texture type");
        }
        gl.bindTexture(texture.glTarget, null);
      }
    };
  }
});

// dist/adapter/webgl-canvas-context.js
var import_core7, WebGLCanvasContext;
var init_webgl_canvas_context = __esm({
  "dist/adapter/webgl-canvas-context.js"() {
    "use strict";
    import_core7 = require("@luma.gl/core");
    init_webgl_framebuffer();
    WebGLCanvasContext = class extends import_core7.CanvasContext {
      device;
      handle = null;
      _framebuffer = null;
      get [Symbol.toStringTag]() {
        return "WebGLCanvasContext";
      }
      constructor(device, props) {
        super(props);
        this.device = device;
        this._setAutoCreatedCanvasId(`${this.device.id}-canvas`);
        this._updateDevice();
      }
      getCurrentFramebuffer() {
        this._framebuffer = this._framebuffer || new WEBGLFramebuffer(this.device, { handle: null });
        return this._framebuffer;
      }
      // IMPLEMENTATION OF ABSTRACT METHODS
      _updateDevice() {
      }
    };
  }
});

// dist/utils/uid.js
function uid(id = "id") {
  uidCounters[id] = uidCounters[id] || 1;
  const count = uidCounters[id]++;
  return `${id}-${count}`;
}
var uidCounters;
var init_uid = __esm({
  "dist/utils/uid.js"() {
    "use strict";
    uidCounters = {};
  }
});

// dist/adapter/resources/webgl-buffer.js
function getWebGLTarget(usage) {
  if (usage & import_core8.Buffer.INDEX) {
    return 34963;
  }
  if (usage & import_core8.Buffer.VERTEX) {
    return 34962;
  }
  if (usage & import_core8.Buffer.UNIFORM) {
    return 35345;
  }
  return 34962;
}
function getWebGLUsage(usage) {
  if (usage & import_core8.Buffer.INDEX) {
    return 35044;
  }
  if (usage & import_core8.Buffer.VERTEX) {
    return 35044;
  }
  if (usage & import_core8.Buffer.UNIFORM) {
    return 35048;
  }
  return 35044;
}
var import_core8, import_constants9, WEBGLBuffer;
var init_webgl_buffer = __esm({
  "dist/adapter/resources/webgl-buffer.js"() {
    "use strict";
    import_core8 = require("@luma.gl/core");
    import_constants9 = require("@luma.gl/constants");
    WEBGLBuffer = class extends import_core8.Buffer {
      device;
      gl;
      handle;
      /** Target in OpenGL defines the type of buffer */
      glTarget;
      /** Usage is a hint on how frequently the buffer will be updates */
      glUsage;
      /** Index type is needed when issuing draw calls, so we pre-compute it */
      glIndexType = 5123;
      /** Number of bytes allocated on the GPU for this buffer */
      byteLength = 0;
      /** Number of bytes used */
      bytesUsed = 0;
      constructor(device, props = {}) {
        super(device, props);
        this.device = device;
        this.gl = this.device.gl;
        const handle = typeof props === "object" ? props.handle : void 0;
        this.handle = handle || this.gl.createBuffer();
        device._setWebGLDebugMetadata(this.handle, this, {
          spector: { ...this.props, data: typeof this.props.data }
        });
        this.glTarget = getWebGLTarget(this.props.usage);
        this.glUsage = getWebGLUsage(this.props.usage);
        this.glIndexType = this.props.indexType === "uint32" ? 5125 : 5123;
        if (props.data) {
          this._initWithData(props.data, props.byteOffset, props.byteLength);
        } else {
          this._initWithByteLength(props.byteLength || 0);
        }
      }
      destroy() {
        if (!this.destroyed && this.handle) {
          this.removeStats();
          this.trackDeallocatedMemory();
          this.gl.deleteBuffer(this.handle);
          this.destroyed = true;
          this.handle = null;
        }
      }
      /** Allocate a new buffer and initialize to contents of typed array */
      _initWithData(data, byteOffset = 0, byteLength = data.byteLength + byteOffset) {
        const glTarget = this.glTarget;
        this.gl.bindBuffer(glTarget, this.handle);
        this.gl.bufferData(glTarget, byteLength, this.glUsage);
        this.gl.bufferSubData(glTarget, byteOffset, data);
        this.gl.bindBuffer(glTarget, null);
        this.bytesUsed = byteLength;
        this.byteLength = byteLength;
        this._setDebugData(data, byteOffset, byteLength);
        this.trackAllocatedMemory(byteLength);
      }
      // Allocate a GPU buffer of specified size.
      _initWithByteLength(byteLength) {
        let data = byteLength;
        if (byteLength === 0) {
          data = new Float32Array(0);
        }
        const glTarget = this.glTarget;
        this.gl.bindBuffer(glTarget, this.handle);
        this.gl.bufferData(glTarget, data, this.glUsage);
        this.gl.bindBuffer(glTarget, null);
        this.bytesUsed = byteLength;
        this.byteLength = byteLength;
        this._setDebugData(null, 0, byteLength);
        this.trackAllocatedMemory(byteLength);
        return this;
      }
      write(data, byteOffset = 0) {
        const dataView = ArrayBuffer.isView(data) ? data : new Uint8Array(data);
        const srcOffset = 0;
        const byteLength = void 0;
        const glTarget = 36663;
        this.gl.bindBuffer(glTarget, this.handle);
        if (srcOffset !== 0 || byteLength !== void 0) {
          this.gl.bufferSubData(glTarget, byteOffset, dataView, srcOffset, byteLength);
        } else {
          this.gl.bufferSubData(glTarget, byteOffset, dataView);
        }
        this.gl.bindBuffer(glTarget, null);
        this._setDebugData(data, byteOffset, data.byteLength);
      }
      async mapAndWriteAsync(callback, byteOffset = 0, byteLength = this.byteLength - byteOffset) {
        const arrayBuffer = new ArrayBuffer(byteLength);
        await callback(arrayBuffer, "copied");
        this.write(arrayBuffer, byteOffset);
      }
      async readAsync(byteOffset = 0, byteLength) {
        return this.readSyncWebGL(byteOffset, byteLength);
      }
      async mapAndReadAsync(callback, byteOffset = 0, byteLength) {
        const data = await this.readAsync(byteOffset, byteLength);
        return await callback(data.buffer, "copied");
      }
      readSyncWebGL(byteOffset = 0, byteLength) {
        byteLength = byteLength ?? this.byteLength - byteOffset;
        const data = new Uint8Array(byteLength);
        const dstOffset = 0;
        this.gl.bindBuffer(36662, this.handle);
        this.gl.getBufferSubData(36662, byteOffset, data, dstOffset, byteLength);
        this.gl.bindBuffer(36662, null);
        this._setDebugData(data, byteOffset, byteLength);
        return data;
      }
    };
  }
});

// dist/adapter/helpers/parse-shader-compiler-log.js
function parseShaderCompilerLog(errLog) {
  const lines = errLog.split(/\r?\n/);
  const messages = [];
  for (const line of lines) {
    if (line.length <= 1) {
      continue;
    }
    const segments = line.split(":");
    if (segments.length === 2) {
      const [messageType2, message2] = segments;
      messages.push({
        message: message2.trim(),
        type: getMessageType(messageType2),
        lineNum: 0,
        linePos: 0
      });
      continue;
    }
    const [messageType, linePosition, lineNumber, ...rest] = segments;
    let lineNum = parseInt(lineNumber, 10);
    if (isNaN(lineNum)) {
      lineNum = 0;
    }
    let linePos = parseInt(linePosition, 10);
    if (isNaN(linePos)) {
      linePos = 0;
    }
    messages.push({
      message: rest.join(":").trim(),
      type: getMessageType(messageType),
      lineNum,
      linePos
      // TODO
    });
  }
  return messages;
}
function getMessageType(messageType) {
  const MESSAGE_TYPES = ["warning", "error", "info"];
  const lowerCaseType = messageType.toLowerCase();
  return MESSAGE_TYPES.includes(lowerCaseType) ? lowerCaseType : "info";
}
var init_parse_shader_compiler_log = __esm({
  "dist/adapter/helpers/parse-shader-compiler-log.js"() {
    "use strict";
  }
});

// dist/adapter/resources/webgl-shader.js
var import_core9, import_constants10, WEBGLShader;
var init_webgl_shader = __esm({
  "dist/adapter/resources/webgl-shader.js"() {
    "use strict";
    import_core9 = require("@luma.gl/core");
    import_constants10 = require("@luma.gl/constants");
    init_parse_shader_compiler_log();
    WEBGLShader = class extends import_core9.Shader {
      device;
      handle;
      constructor(device, props) {
        super(device, props);
        this.device = device;
        switch (this.props.stage) {
          case "vertex":
            this.handle = this.props.handle || this.device.gl.createShader(35633);
            break;
          case "fragment":
            this.handle = this.props.handle || this.device.gl.createShader(35632);
            break;
          default:
            throw new Error(this.props.stage);
        }
        device._setWebGLDebugMetadata(this.handle, this, { spector: this.props });
        this._compile(this.source);
      }
      destroy() {
        if (this.handle) {
          this.removeStats();
          this.device.gl.deleteShader(this.handle);
          this.destroyed = true;
          this.handle.destroyed = true;
        }
      }
      get asyncCompilationStatus() {
        return this._waitForCompilationComplete().then(() => {
          this._getCompilationStatus();
          return this.compilationStatus;
        });
      }
      async getCompilationInfo() {
        await this._waitForCompilationComplete();
        return this.getCompilationInfoSync();
      }
      getCompilationInfoSync() {
        const shaderLog = this.device.gl.getShaderInfoLog(this.handle);
        return shaderLog ? parseShaderCompilerLog(shaderLog) : [];
      }
      getTranslatedSource() {
        const extensions = this.device.getExtension("WEBGL_debug_shaders");
        const ext = extensions.WEBGL_debug_shaders;
        return (ext == null ? void 0 : ext.getTranslatedShaderSource(this.handle)) || null;
      }
      // PRIVATE METHODS
      /** Compile a shader and get compilation status */
      async _compile(source) {
        source = source.startsWith("#version ") ? source : `#version 300 es
${source}`;
        const { gl } = this.device;
        gl.shaderSource(this.handle, source);
        gl.compileShader(this.handle);
        if (!this.device.props.debug) {
          this.compilationStatus = "pending";
          return;
        }
        if (!this.device.features.has("compilation-status-async-webgl")) {
          this._getCompilationStatus();
          this.debugShader();
          if (this.compilationStatus === "error") {
            throw new Error(`GLSL compilation errors in ${this.props.stage} shader ${this.props.id}`);
          }
          return;
        }
        import_core9.log.once(1, "Shader compilation is asynchronous")();
        await this._waitForCompilationComplete();
        import_core9.log.info(2, `Shader ${this.id} - async compilation complete: ${this.compilationStatus}`)();
        this._getCompilationStatus();
        this.debugShader();
      }
      /** Use KHR_parallel_shader_compile extension if available */
      async _waitForCompilationComplete() {
        const waitMs = async (ms) => await new Promise((resolve) => setTimeout(resolve, ms));
        const DELAY_MS = 10;
        if (!this.device.features.has("compilation-status-async-webgl")) {
          await waitMs(DELAY_MS);
          return;
        }
        const { gl } = this.device;
        for (; ; ) {
          const complete = gl.getShaderParameter(this.handle, 37297);
          if (complete) {
            return;
          }
          await waitMs(DELAY_MS);
        }
      }
      /**
       * Get the shader compilation status
       * TODO - Load log even when no error reported, to catch warnings?
       * https://gamedev.stackexchange.com/questions/30429/how-to-detect-glsl-warnings
       */
      _getCompilationStatus() {
        this.compilationStatus = this.device.gl.getShaderParameter(this.handle, 35713) ? "success" : "error";
      }
    };
  }
});

// dist/adapter/converters/device-parameters.js
function withDeviceAndGLParameters(device, parameters, glParameters, func) {
  if (isObjectEmpty2(parameters)) {
    return func(device);
  }
  const webglDevice = device;
  webglDevice.pushState();
  try {
    setDeviceParameters(device, parameters);
    setGLParameters(webglDevice.gl, glParameters);
    return func(device);
  } finally {
    webglDevice.popState();
  }
}
function withDeviceParameters(device, parameters, func) {
  if (isObjectEmpty2(parameters)) {
    return func(device);
  }
  const webglDevice = device;
  webglDevice.pushState();
  try {
    setDeviceParameters(device, parameters);
    return func(device);
  } finally {
    webglDevice.popState();
  }
}
function setDeviceParameters(device, parameters) {
  const webglDevice = device;
  const { gl } = webglDevice;
  if (parameters.cullMode) {
    switch (parameters.cullMode) {
      case "none":
        gl.disable(2884);
        break;
      case "front":
        gl.enable(2884);
        gl.cullFace(1028);
        break;
      case "back":
        gl.enable(2884);
        gl.cullFace(1029);
        break;
    }
  }
  if (parameters.frontFace) {
    gl.frontFace(map("frontFace", parameters.frontFace, {
      ccw: 2305,
      cw: 2304
    }));
  }
  if (parameters.unclippedDepth) {
    if (device.features.has("depth-clip-control")) {
      gl.enable(34383);
    }
  }
  if (parameters.depthBias !== void 0) {
    gl.enable(32823);
    gl.polygonOffset(parameters.depthBias, parameters.depthBiasSlopeScale || 0);
  }
  if (parameters.provokingVertex) {
    if (device.features.has("provoking-vertex-webgl")) {
      const extensions = webglDevice.getExtension("WEBGL_provoking_vertex");
      const ext = extensions.WEBGL_provoking_vertex;
      const vertex = map("provokingVertex", parameters.provokingVertex, {
        first: 36429,
        last: 36430
      });
      ext == null ? void 0 : ext.provokingVertexWEBGL(vertex);
    }
  }
  if (parameters.polygonMode || parameters.polygonOffsetLine) {
    if (device.features.has("polygon-mode-webgl")) {
      if (parameters.polygonMode) {
        const extensions = webglDevice.getExtension("WEBGL_polygon_mode");
        const ext = extensions.WEBGL_polygon_mode;
        const mode = map("polygonMode", parameters.polygonMode, {
          fill: 6914,
          line: 6913
        });
        ext == null ? void 0 : ext.polygonModeWEBGL(1028, mode);
        ext == null ? void 0 : ext.polygonModeWEBGL(1029, mode);
      }
      if (parameters.polygonOffsetLine) {
        gl.enable(10754);
      }
    }
  }
  if (device.features.has("shader-clip-cull-distance-webgl")) {
    if (parameters.clipDistance0) {
      gl.enable(12288);
    }
    if (parameters.clipDistance1) {
      gl.enable(12289);
    }
    if (parameters.clipDistance2) {
      gl.enable(12290);
    }
    if (parameters.clipDistance3) {
      gl.enable(12291);
    }
    if (parameters.clipDistance4) {
      gl.enable(12292);
    }
    if (parameters.clipDistance5) {
      gl.enable(12293);
    }
    if (parameters.clipDistance6) {
      gl.enable(12294);
    }
    if (parameters.clipDistance7) {
      gl.enable(12295);
    }
  }
  if (parameters.depthWriteEnabled !== void 0) {
    gl.depthMask(mapBoolean("depthWriteEnabled", parameters.depthWriteEnabled));
  }
  if (parameters.depthCompare) {
    parameters.depthCompare !== "always" ? gl.enable(2929) : gl.disable(2929);
    gl.depthFunc(convertCompareFunction("depthCompare", parameters.depthCompare));
  }
  if (parameters.stencilWriteMask) {
    const mask = parameters.stencilWriteMask;
    gl.stencilMaskSeparate(1028, mask);
    gl.stencilMaskSeparate(1029, mask);
  }
  if (parameters.stencilReadMask) {
    import_core10.log.warn("stencilReadMask not supported under WebGL");
  }
  if (parameters.stencilCompare) {
    const mask = parameters.stencilReadMask || 4294967295;
    const glValue = convertCompareFunction("depthCompare", parameters.stencilCompare);
    parameters.stencilCompare !== "always" ? gl.enable(2960) : gl.disable(2960);
    gl.stencilFuncSeparate(1028, glValue, 0, mask);
    gl.stencilFuncSeparate(1029, glValue, 0, mask);
  }
  if (parameters.stencilPassOperation && parameters.stencilFailOperation && parameters.stencilDepthFailOperation) {
    const dppass = convertStencilOperation("stencilPassOperation", parameters.stencilPassOperation);
    const sfail = convertStencilOperation("stencilFailOperation", parameters.stencilFailOperation);
    const dpfail = convertStencilOperation("stencilDepthFailOperation", parameters.stencilDepthFailOperation);
    gl.stencilOpSeparate(1028, sfail, dpfail, dppass);
    gl.stencilOpSeparate(1029, sfail, dpfail, dppass);
  }
  switch (parameters.blend) {
    case true:
      gl.enable(3042);
      break;
    case false:
      gl.disable(3042);
      break;
    default:
  }
  if (parameters.blendColorOperation || parameters.blendAlphaOperation) {
    const colorEquation = convertBlendOperationToEquation("blendColorOperation", parameters.blendColorOperation || "add");
    const alphaEquation = convertBlendOperationToEquation("blendAlphaOperation", parameters.blendAlphaOperation || "add");
    gl.blendEquationSeparate(colorEquation, alphaEquation);
    const colorSrcFactor = convertBlendFactorToFunction("blendColorSrcFactor", parameters.blendColorSrcFactor || "one");
    const colorDstFactor = convertBlendFactorToFunction("blendColorDstFactor", parameters.blendColorDstFactor || "zero");
    const alphaSrcFactor = convertBlendFactorToFunction("blendAlphaSrcFactor", parameters.blendAlphaSrcFactor || "one");
    const alphaDstFactor = convertBlendFactorToFunction("blendAlphaDstFactor", parameters.blendAlphaDstFactor || "zero");
    gl.blendFuncSeparate(colorSrcFactor, colorDstFactor, alphaSrcFactor, alphaDstFactor);
  }
}
function convertCompareFunction(parameter, value) {
  return map(parameter, value, {
    never: 512,
    less: 513,
    equal: 514,
    "less-equal": 515,
    greater: 516,
    "not-equal": 517,
    "greater-equal": 518,
    always: 519
  });
}
function convertStencilOperation(parameter, value) {
  return map(parameter, value, {
    keep: 7680,
    zero: 0,
    replace: 7681,
    invert: 5386,
    "increment-clamp": 7682,
    "decrement-clamp": 7683,
    "increment-wrap": 34055,
    "decrement-wrap": 34056
  });
}
function convertBlendOperationToEquation(parameter, value) {
  return map(parameter, value, {
    add: 32774,
    subtract: 32778,
    "reverse-subtract": 32779,
    min: 32775,
    max: 32776
  });
}
function convertBlendFactorToFunction(parameter, value, type = "color") {
  return map(parameter, value, {
    one: 1,
    zero: 0,
    src: 768,
    "one-minus-src": 769,
    dst: 774,
    "one-minus-dst": 775,
    "src-alpha": 770,
    "one-minus-src-alpha": 771,
    "dst-alpha": 772,
    "one-minus-dst-alpha": 773,
    "src-alpha-saturated": 776,
    constant: type === "color" ? 32769 : 32771,
    "one-minus-constant": type === "color" ? 32770 : 32772,
    // 'constant-alpha': GL.CONSTANT_ALPHA,
    // 'one-minus-constant-alpha': GL.ONE_MINUS_CONSTANT_ALPHA,
    // TODO not supported in WebGL2
    src1: 768,
    "one-minus-src1": 769,
    "src1-alpha": 770,
    "one-minus-src1-alpha": 771
  });
}
function message(parameter, value) {
  return `Illegal parameter ${value} for ${parameter}`;
}
function map(parameter, value, valueMap) {
  if (!(value in valueMap)) {
    throw new Error(message(parameter, value));
  }
  return valueMap[value];
}
function mapBoolean(parameter, value) {
  return value;
}
function isObjectEmpty2(obj) {
  let isEmpty = true;
  for (const key in obj) {
    isEmpty = false;
    break;
  }
  return isEmpty;
}
var import_core10, import_constants11;
var init_device_parameters = __esm({
  "dist/adapter/converters/device-parameters.js"() {
    "use strict";
    import_core10 = require("@luma.gl/core");
    import_constants11 = require("@luma.gl/constants");
    init_unified_parameter_api();
  }
});

// dist/adapter/converters/sampler-parameters.js
function convertSamplerParametersToWebGL(props) {
  const params = {};
  if (props.addressModeU) {
    params[10242] = convertAddressMode(props.addressModeU);
  }
  if (props.addressModeV) {
    params[10243] = convertAddressMode(props.addressModeV);
  }
  if (props.addressModeW) {
    params[32882] = convertAddressMode(props.addressModeW);
  }
  if (props.magFilter) {
    params[10240] = convertMaxFilterMode(props.magFilter);
  }
  if (props.minFilter || props.mipmapFilter) {
    params[10241] = convertMinFilterMode(props.minFilter || "linear", props.mipmapFilter);
  }
  if (props.lodMinClamp !== void 0) {
    params[33082] = props.lodMinClamp;
  }
  if (props.lodMaxClamp !== void 0) {
    params[33083] = props.lodMaxClamp;
  }
  if (props.type === "comparison-sampler") {
    params[34892] = 34894;
  }
  if (props.compare) {
    params[34893] = convertCompareFunction("compare", props.compare);
  }
  if (props.maxAnisotropy) {
    params[34046] = props.maxAnisotropy;
  }
  return params;
}
function convertAddressMode(addressMode) {
  switch (addressMode) {
    case "clamp-to-edge":
      return 33071;
    case "repeat":
      return 10497;
    case "mirror-repeat":
      return 33648;
  }
}
function convertMaxFilterMode(maxFilter) {
  switch (maxFilter) {
    case "nearest":
      return 9728;
    case "linear":
      return 9729;
  }
}
function convertMinFilterMode(minFilter, mipmapFilter = "none") {
  if (!mipmapFilter) {
    return convertMaxFilterMode(minFilter);
  }
  switch (mipmapFilter) {
    case "none":
      return convertMaxFilterMode(minFilter);
    case "nearest":
      switch (minFilter) {
        case "nearest":
          return 9984;
        case "linear":
          return 9985;
      }
      break;
    case "linear":
      switch (minFilter) {
        case "nearest":
          return 9986;
        case "linear":
          return 9987;
      }
  }
}
var import_constants12;
var init_sampler_parameters = __esm({
  "dist/adapter/converters/sampler-parameters.js"() {
    "use strict";
    import_constants12 = require("@luma.gl/constants");
    init_device_parameters();
  }
});

// dist/adapter/resources/webgl-sampler.js
var import_core11, import_constants13, WEBGLSampler;
var init_webgl_sampler = __esm({
  "dist/adapter/resources/webgl-sampler.js"() {
    "use strict";
    import_core11 = require("@luma.gl/core");
    import_constants13 = require("@luma.gl/constants");
    init_sampler_parameters();
    WEBGLSampler = class extends import_core11.Sampler {
      device;
      handle;
      parameters;
      constructor(device, props) {
        super(device, props);
        this.device = device;
        this.parameters = convertSamplerParametersToWebGL(props);
        this.handle = props.handle || this.device.gl.createSampler();
        this._setSamplerParameters(this.parameters);
      }
      destroy() {
        if (this.handle) {
          this.device.gl.deleteSampler(this.handle);
          this.handle = void 0;
        }
      }
      toString() {
        return `Sampler(${this.id},${JSON.stringify(this.props)})`;
      }
      /** Set sampler parameters on the sampler */
      _setSamplerParameters(parameters) {
        for (const [pname, value] of Object.entries(parameters)) {
          const param = Number(pname);
          switch (param) {
            case 33082:
            case 33083:
              this.device.gl.samplerParameterf(this.handle, param, value);
              break;
            default:
              this.device.gl.samplerParameteri(this.handle, param, value);
              break;
          }
        }
      }
    };
  }
});

// dist/context/state-tracker/with-parameters.js
function withGLParameters(gl, parameters, func) {
  if (isObjectEmpty3(parameters)) {
    return func(gl);
  }
  const { nocatch = true } = parameters;
  const webglState = WebGLStateTracker.get(gl);
  webglState.push();
  setGLParameters(gl, parameters);
  let value;
  if (nocatch) {
    value = func(gl);
    webglState.pop();
  } else {
    try {
      value = func(gl);
    } finally {
      webglState.pop();
    }
  }
  return value;
}
function isObjectEmpty3(object) {
  for (const key in object) {
    return false;
  }
  return true;
}
var init_with_parameters = __esm({
  "dist/context/state-tracker/with-parameters.js"() {
    "use strict";
    init_unified_parameter_api();
    init_webgl_state_tracker();
  }
});

// dist/adapter/resources/webgl-texture-view.js
var import_core12, WEBGLTextureView;
var init_webgl_texture_view = __esm({
  "dist/adapter/resources/webgl-texture-view.js"() {
    "use strict";
    import_core12 = require("@luma.gl/core");
    WEBGLTextureView = class extends import_core12.TextureView {
      device;
      gl;
      handle;
      // Does not have a WebGL representation
      texture;
      constructor(device, props) {
        super(device, { ...import_core12.Texture.defaultProps, ...props });
        this.device = device;
        this.gl = this.device.gl;
        this.handle = null;
        this.texture = props.texture;
      }
    };
  }
});

// dist/adapter/resources/webgl-texture.js
function getWebGLTextureTarget(dimension) {
  switch (dimension) {
    case "1d":
      break;
    case "2d":
      return 3553;
    case "3d":
      return 32879;
    case "cube":
      return 34067;
    case "2d-array":
      return 35866;
    case "cube-array":
      break;
  }
  throw new Error(dimension);
}
function getWebGLCubeFaceTarget(glTarget, dimension, level) {
  return dimension === "cube" ? 34069 + level : glTarget;
}
var import_core13, import_constants14, WEBGLTexture;
var init_webgl_texture = __esm({
  "dist/adapter/resources/webgl-texture.js"() {
    "use strict";
    import_core13 = require("@luma.gl/core");
    import_constants14 = require("@luma.gl/constants");
    init_webgl_texture_table();
    init_sampler_parameters();
    init_with_parameters();
    init_webgl_texture_view();
    WEBGLTexture = class extends import_core13.Texture {
      // readonly MAX_ATTRIBUTES: number;
      device;
      gl;
      handle;
      // @ts-ignore TODO - currently unused in WebGL. Create dummy sampler?
      sampler = void 0;
      view;
      /**
       * The WebGL target corresponding to the texture type
       * @note `target` cannot be modified by bind:
       * textures are special because when you first bind them to a target,
       * When you first bind a texture as a GL_TEXTURE_2D, you are saying that this texture is a 2D texture.
       * And it will always be a 2D texture; this state cannot be changed ever.
       * A texture that was first bound as a GL_TEXTURE_2D, must always be bound as a GL_TEXTURE_2D;
       * attempting to bind it as GL_TEXTURE_3D will give rise to a run-time error
       */
      glTarget;
      /** The WebGL format - essentially channel structure */
      glFormat;
      /** The WebGL data format - the type of each channel */
      glType;
      /** The WebGL constant corresponding to the WebGPU style constant in format */
      glInternalFormat;
      /** Whether the internal format is compressed */
      compressed;
      // state
      /** Texture binding slot - TODO - move to texture view? */
      _textureUnit = 0;
      constructor(device, props) {
        super(device, props);
        this.device = device;
        this.gl = this.device.gl;
        const formatInfo = getTextureFormatWebGL(this.props.format);
        this.glTarget = getWebGLTextureTarget(this.props.dimension);
        this.glInternalFormat = formatInfo.internalFormat;
        this.glFormat = formatInfo.format;
        this.glType = formatInfo.type;
        this.compressed = formatInfo.compressed;
        this.handle = this.props.handle || this.gl.createTexture();
        this.device._setWebGLDebugMetadata(this.handle, this, { spector: this.props });
        this.gl.bindTexture(this.glTarget, this.handle);
        const { dimension, width, height, depth, mipLevels, glTarget, glInternalFormat } = this;
        switch (dimension) {
          case "2d":
          case "cube":
            this.gl.texStorage2D(glTarget, mipLevels, glInternalFormat, width, height);
            break;
          case "2d-array":
          case "3d":
            this.gl.texStorage3D(glTarget, mipLevels, glInternalFormat, width, height, depth);
            break;
          default:
            throw new Error(dimension);
        }
        this.gl.bindTexture(this.glTarget, null);
        this._initializeData(props.data);
        this.setSampler(this.props.sampler);
        this.view = new WEBGLTextureView(this.device, { ...this.props, texture: this });
        Object.seal(this);
      }
      destroy() {
        if (this.handle) {
          this.gl.deleteTexture(this.handle);
          this.removeStats();
          this.trackDeallocatedMemory("Texture");
          this.destroyed = true;
        }
      }
      createView(props) {
        return new WEBGLTextureView(this.device, { ...props, texture: this });
      }
      setSampler(sampler = {}) {
        super.setSampler(sampler);
        const parameters = convertSamplerParametersToWebGL(this.sampler.props);
        this._setSamplerParameters(parameters);
      }
      copyImageData(options_) {
        const options = this._normalizeCopyImageDataOptions(options_);
        const typedArray = options.data;
        const { width, height, depth } = this;
        const { mipLevel = 0, byteOffset = 0, x = 0, y = 0, z = 0 } = options;
        const { glFormat, glType, compressed } = this;
        const glTarget = getWebGLCubeFaceTarget(this.glTarget, this.dimension, z);
        let unpackRowLength;
        if (!this.compressed) {
          const { bytesPerPixel } = this.device.getTextureFormatInfo(this.format);
          if (bytesPerPixel) {
            if (options.bytesPerRow % bytesPerPixel !== 0) {
              throw new Error(`bytesPerRow (${options.bytesPerRow}) must be a multiple of bytesPerPixel (${bytesPerPixel}) for ${this.format}`);
            }
            unpackRowLength = options.bytesPerRow / bytesPerPixel;
          }
        }
        const glParameters = !this.compressed ? {
          ...unpackRowLength !== void 0 ? { [3314]: unpackRowLength } : {},
          [32878]: options.rowsPerImage
        } : {};
        this.gl.bindTexture(glTarget, this.handle);
        withGLParameters(this.gl, glParameters, () => {
          switch (this.dimension) {
            case "2d":
            case "cube":
              if (compressed) {
                this.gl.compressedTexSubImage2D(glTarget, mipLevel, x, y, width, height, glFormat, typedArray, byteOffset);
              } else {
                this.gl.texSubImage2D(glTarget, mipLevel, x, y, width, height, glFormat, glType, typedArray, byteOffset);
              }
              break;
            case "2d-array":
            case "3d":
              if (compressed) {
                this.gl.compressedTexSubImage3D(glTarget, mipLevel, x, y, z, width, height, depth, glFormat, typedArray, byteOffset);
              } else {
                this.gl.texSubImage3D(glTarget, mipLevel, x, y, z, width, height, depth, glFormat, glType, typedArray, byteOffset);
              }
              break;
            default:
          }
        });
        this.gl.bindTexture(glTarget, null);
      }
      copyExternalImage(options_) {
        const options = this._normalizeCopyExternalImageOptions(options_);
        if (options.sourceX || options.sourceY) {
          throw new Error("WebGL does not support sourceX/sourceY)");
        }
        const { glFormat, glType } = this;
        const { image, depth, mipLevel, x, y, z, width, height } = options;
        const glTarget = getWebGLCubeFaceTarget(this.glTarget, this.dimension, depth);
        const glParameters = options.flipY ? { [37440]: true } : {};
        this.gl.bindTexture(this.glTarget, this.handle);
        withGLParameters(this.gl, glParameters, () => {
          switch (this.dimension) {
            case "2d":
            case "cube":
              this.gl.texSubImage2D(glTarget, mipLevel, x, y, width, height, glFormat, glType, image);
              break;
            case "2d-array":
            case "3d":
              this.gl.texSubImage3D(glTarget, mipLevel, x, y, z, width, height, depth, glFormat, glType, image);
              break;
            default:
          }
        });
        this.gl.bindTexture(this.glTarget, null);
        return { width: options.width, height: options.height };
      }
      // WEBGL SPECIFIC
      generateMipmapsWebGL(options) {
        const isFilterableAndRenderable = this.device.isTextureFormatRenderable(this.props.format) && this.device.isTextureFormatFilterable(this.props.format);
        if (!isFilterableAndRenderable) {
          import_core13.log.warn(`${this} is not renderable or filterable, may not be able to generate mipmaps`)();
          if (!(options == null ? void 0 : options.force)) {
            return;
          }
        }
        try {
          this.gl.bindTexture(this.glTarget, this.handle);
          this.gl.generateMipmap(this.glTarget);
        } catch (error) {
          import_core13.log.warn(`Error generating mipmap for ${this}: ${error.message}`)();
        } finally {
          this.gl.bindTexture(this.glTarget, null);
        }
      }
      // INTERNAL
      /**
       * Sets sampler parameters on texture
       */
      _setSamplerParameters(parameters) {
        import_core13.log.log(2, `${this.id} sampler parameters`, this.device.getGLKeys(parameters))();
        this.gl.bindTexture(this.glTarget, this.handle);
        for (const [pname, pvalue] of Object.entries(parameters)) {
          const param = Number(pname);
          const value = pvalue;
          switch (param) {
            case 33082:
            case 33083:
              this.gl.texParameterf(this.glTarget, param, value);
              break;
            case 10240:
            case 10241:
              this.gl.texParameteri(this.glTarget, param, value);
              break;
            case 10242:
            case 10243:
            case 32882:
              this.gl.texParameteri(this.glTarget, param, value);
              break;
            case 34046:
              if (this.device.features.has("texture-filterable-anisotropic-webgl")) {
                this.gl.texParameteri(this.glTarget, param, value);
              }
              break;
            case 34892:
            case 34893:
              this.gl.texParameteri(this.glTarget, param, value);
              break;
          }
        }
        this.gl.bindTexture(this.glTarget, null);
      }
      _getActiveUnit() {
        return this.gl.getParameter(34016) - 33984;
      }
      _bind(_textureUnit) {
        const { gl } = this;
        if (_textureUnit !== void 0) {
          this._textureUnit = _textureUnit;
          gl.activeTexture(33984 + _textureUnit);
        }
        gl.bindTexture(this.glTarget, this.handle);
        return _textureUnit;
      }
      _unbind(_textureUnit) {
        const { gl } = this;
        if (_textureUnit !== void 0) {
          this._textureUnit = _textureUnit;
          gl.activeTexture(33984 + _textureUnit);
        }
        gl.bindTexture(this.glTarget, null);
        return _textureUnit;
      }
    };
  }
});

// dist/adapter/converters/webgl-shadertypes.js
function convertDataTypeToGLDataType(normalizedType) {
  return NORMALIZED_SHADER_TYPE_TO_WEBGL[normalizedType];
}
function convertGLUniformTypeToShaderVariableType(glUniformType) {
  return WEBGL_SHADER_TYPES[glUniformType];
}
function isGLSamplerType(type) {
  return Boolean(WEBGL_SAMPLER_TO_TEXTURE_BINDINGS[type]);
}
function getTextureBindingFromGLSamplerType(glSamplerType) {
  return WEBGL_SAMPLER_TO_TEXTURE_BINDINGS[glSamplerType];
}
var import_constants15, WEBGL_SHADER_TYPES, WEBGL_SAMPLER_TO_TEXTURE_BINDINGS, NORMALIZED_SHADER_TYPE_TO_WEBGL;
var init_webgl_shadertypes = __esm({
  "dist/adapter/converters/webgl-shadertypes.js"() {
    "use strict";
    import_constants15 = require("@luma.gl/constants");
    WEBGL_SHADER_TYPES = {
      [5126]: "f32",
      [35664]: "vec2<f32>",
      [35665]: "vec3<f32>",
      [35666]: "vec4<f32>",
      [5124]: "i32",
      [35667]: "vec2<i32>",
      [35668]: "vec3<i32>",
      [35669]: "vec4<i32>",
      [5125]: "u32",
      [36294]: "vec2<u32>",
      [36295]: "vec3<u32>",
      [36296]: "vec4<u32>",
      [35670]: "f32",
      [35671]: "vec2<f32>",
      [35672]: "vec3<f32>",
      [35673]: "vec4<f32>",
      // TODO - are sizes/components below correct?
      [35674]: "mat2x2<f32>",
      [35685]: "mat2x3<f32>",
      [35686]: "mat2x4<f32>",
      [35687]: "mat3x2<f32>",
      [35675]: "mat3x3<f32>",
      [35688]: "mat3x4<f32>",
      [35689]: "mat4x2<f32>",
      [35690]: "mat4x3<f32>",
      [35676]: "mat4x4<f32>"
    };
    WEBGL_SAMPLER_TO_TEXTURE_BINDINGS = {
      [35678]: { viewDimension: "2d", sampleType: "float" },
      [35680]: { viewDimension: "cube", sampleType: "float" },
      [35679]: { viewDimension: "3d", sampleType: "float" },
      [35682]: { viewDimension: "3d", sampleType: "depth" },
      [36289]: { viewDimension: "2d-array", sampleType: "float" },
      [36292]: { viewDimension: "2d-array", sampleType: "depth" },
      [36293]: { viewDimension: "cube", sampleType: "float" },
      [36298]: { viewDimension: "2d", sampleType: "sint" },
      [36299]: { viewDimension: "3d", sampleType: "sint" },
      [36300]: { viewDimension: "cube", sampleType: "sint" },
      [36303]: { viewDimension: "2d-array", sampleType: "uint" },
      [36306]: { viewDimension: "2d", sampleType: "uint" },
      [36307]: { viewDimension: "3d", sampleType: "uint" },
      [36308]: { viewDimension: "cube", sampleType: "uint" },
      [36311]: { viewDimension: "2d-array", sampleType: "uint" }
    };
    NORMALIZED_SHADER_TYPE_TO_WEBGL = {
      uint8: 5121,
      sint8: 5120,
      unorm8: 5121,
      snorm8: 5120,
      uint16: 5123,
      sint16: 5122,
      unorm16: 5123,
      snorm16: 5122,
      uint32: 5125,
      sint32: 5124,
      // WebGPU does not support normalized 32 bit integer attributes
      //  'unorm32': GL.UNSIGNED_INT,
      //  'snorm32': GL.INT,
      float16: 5131,
      float32: 5126
    };
  }
});

// dist/adapter/helpers/get-shader-layout-from-glsl.js
function getShaderLayoutFromGLSL(gl, program) {
  const shaderLayout = {
    attributes: [],
    bindings: []
  };
  shaderLayout.attributes = readAttributeDeclarations(gl, program);
  const uniformBlocks = readUniformBlocks(gl, program);
  for (const uniformBlock of uniformBlocks) {
    const uniforms2 = uniformBlock.uniforms.map((uniform) => ({
      name: uniform.name,
      format: uniform.format,
      byteOffset: uniform.byteOffset,
      byteStride: uniform.byteStride,
      arrayLength: uniform.arrayLength
    }));
    shaderLayout.bindings.push({
      type: "uniform",
      name: uniformBlock.name,
      group: 0,
      location: uniformBlock.location,
      visibility: (uniformBlock.vertex ? 1 : 0) & (uniformBlock.fragment ? 2 : 0),
      minBindingSize: uniformBlock.byteLength,
      uniforms: uniforms2
    });
  }
  const uniforms = readUniformBindings(gl, program);
  let textureUnit = 0;
  for (const uniform of uniforms) {
    if (isGLSamplerType(uniform.type)) {
      const { viewDimension, sampleType } = getTextureBindingFromGLSamplerType(uniform.type);
      shaderLayout.bindings.push({
        type: "texture",
        name: uniform.name,
        group: 0,
        location: textureUnit,
        viewDimension,
        sampleType
      });
      uniform.textureUnit = textureUnit;
      textureUnit += 1;
    }
  }
  if (uniforms.length) {
    shaderLayout.uniforms = uniforms;
  }
  const varyings = readVaryings(gl, program);
  if (varyings == null ? void 0 : varyings.length) {
    shaderLayout.varyings = varyings;
  }
  return shaderLayout;
}
function readAttributeDeclarations(gl, program) {
  const attributes = [];
  const count = gl.getProgramParameter(program, 35721);
  for (let index = 0; index < count; index++) {
    const activeInfo = gl.getActiveAttrib(program, index);
    if (!activeInfo) {
      throw new Error("activeInfo");
    }
    const {
      name,
      type: compositeType
      /* , size*/
    } = activeInfo;
    const location = gl.getAttribLocation(program, name);
    if (location >= 0) {
      const attributeType = convertGLUniformTypeToShaderVariableType(compositeType);
      const stepMode = /instance/i.test(name) ? "instance" : "vertex";
      attributes.push({
        name,
        location,
        stepMode,
        type: attributeType
        // size - for arrays, size is the number of elements in the array
      });
    }
  }
  attributes.sort((a, b) => a.location - b.location);
  return attributes;
}
function readVaryings(gl, program) {
  const varyings = [];
  const count = gl.getProgramParameter(program, 35971);
  for (let location = 0; location < count; location++) {
    const activeInfo = gl.getTransformFeedbackVarying(program, location);
    if (!activeInfo) {
      throw new Error("activeInfo");
    }
    const { name, type: glUniformType, size } = activeInfo;
    const uniformType = convertGLUniformTypeToShaderVariableType(glUniformType);
    const { type, components } = (0, import_core14.getVariableShaderTypeInfo)(uniformType);
    varyings.push({ location, name, type, size: size * components });
  }
  varyings.sort((a, b) => a.location - b.location);
  return varyings;
}
function readUniformBindings(gl, program) {
  const uniforms = [];
  const uniformCount = gl.getProgramParameter(program, 35718);
  for (let i = 0; i < uniformCount; i++) {
    const activeInfo = gl.getActiveUniform(program, i);
    if (!activeInfo) {
      throw new Error("activeInfo");
    }
    const { name: rawName, size, type } = activeInfo;
    const { name, isArray: isArray3 } = parseUniformName(rawName);
    let webglLocation = gl.getUniformLocation(program, name);
    const uniformInfo = {
      // WebGL locations are uniquely typed but just numbers
      location: webglLocation,
      name,
      size,
      type,
      isArray: isArray3
    };
    uniforms.push(uniformInfo);
    if (uniformInfo.size > 1) {
      for (let j = 0; j < uniformInfo.size; j++) {
        const elementName = `${name}[${j}]`;
        webglLocation = gl.getUniformLocation(program, elementName);
        const arrayElementUniformInfo = {
          ...uniformInfo,
          name: elementName,
          location: webglLocation
        };
        uniforms.push(arrayElementUniformInfo);
      }
    }
  }
  return uniforms;
}
function readUniformBlocks(gl, program) {
  const getBlockParameter = (blockIndex, pname) => gl.getActiveUniformBlockParameter(program, blockIndex, pname);
  const uniformBlocks = [];
  const blockCount = gl.getProgramParameter(program, 35382);
  for (let blockIndex = 0; blockIndex < blockCount; blockIndex++) {
    const blockInfo = {
      name: gl.getActiveUniformBlockName(program, blockIndex) || "",
      location: getBlockParameter(blockIndex, 35391),
      byteLength: getBlockParameter(blockIndex, 35392),
      vertex: getBlockParameter(blockIndex, 35396),
      fragment: getBlockParameter(blockIndex, 35398),
      uniformCount: getBlockParameter(blockIndex, 35394),
      uniforms: []
    };
    const uniformIndices = getBlockParameter(blockIndex, 35395) || [];
    const uniformType = gl.getActiveUniforms(program, uniformIndices, 35383);
    const uniformArrayLength = gl.getActiveUniforms(program, uniformIndices, 35384);
    const uniformOffset = gl.getActiveUniforms(program, uniformIndices, 35387);
    const uniformStride = gl.getActiveUniforms(program, uniformIndices, 35388);
    for (let i = 0; i < blockInfo.uniformCount; ++i) {
      const activeInfo = gl.getActiveUniform(program, uniformIndices[i]);
      if (!activeInfo) {
        throw new Error("activeInfo");
      }
      const format = convertGLUniformTypeToShaderVariableType(uniformType[i]);
      blockInfo.uniforms.push({
        name: activeInfo.name,
        format,
        type: uniformType[i],
        arrayLength: uniformArrayLength[i],
        byteOffset: uniformOffset[i],
        byteStride: uniformStride[i]
        // matrixStride: uniformStride[i],
        // rowMajor: uniformRowMajor[i]
      });
    }
    uniformBlocks.push(blockInfo);
  }
  uniformBlocks.sort((a, b) => a.location - b.location);
  return uniformBlocks;
}
function parseUniformName(name) {
  if (name[name.length - 1] !== "]") {
    return {
      name,
      length: 1,
      isArray: false
    };
  }
  const UNIFORM_NAME_REGEXP = /([^[]*)(\[[0-9]+\])?/;
  const matches = UNIFORM_NAME_REGEXP.exec(name);
  if (!matches || matches.length < 2) {
    throw new Error(`Failed to parse GLSL uniform name ${name}`);
  }
  return {
    name: matches[1],
    length: matches[2] ? 1 : 0,
    isArray: Boolean(matches[2])
  };
}
var import_core14, import_constants16;
var init_get_shader_layout_from_glsl = __esm({
  "dist/adapter/helpers/get-shader-layout-from-glsl.js"() {
    "use strict";
    import_core14 = require("@luma.gl/core");
    import_constants16 = require("@luma.gl/constants");
    init_webgl_shadertypes();
  }
});

// dist/adapter/helpers/set-uniform.js
function setUniform(gl, location, type, value) {
  const gl2 = gl;
  let uniformValue = value;
  if (uniformValue === true) {
    uniformValue = 1;
  }
  if (uniformValue === false) {
    uniformValue = 0;
  }
  const arrayValue = typeof uniformValue === "number" ? [uniformValue] : uniformValue;
  switch (type) {
    case 35678:
    case 35680:
    case 35679:
    case 35682:
    case 36289:
    case 36292:
    case 36293:
    case 36298:
    case 36299:
    case 36300:
    case 36303:
    case 36306:
    case 36307:
    case 36308:
    case 36311:
      if (typeof value !== "number") {
        throw new Error("samplers must be set to integers");
      }
      return gl.uniform1i(location, value);
    case 5126:
      return gl.uniform1fv(location, arrayValue);
    case 35664:
      return gl.uniform2fv(location, arrayValue);
    case 35665:
      return gl.uniform3fv(location, arrayValue);
    case 35666:
      return gl.uniform4fv(location, arrayValue);
    case 5124:
      return gl.uniform1iv(location, arrayValue);
    case 35667:
      return gl.uniform2iv(location, arrayValue);
    case 35668:
      return gl.uniform3iv(location, arrayValue);
    case 35669:
      return gl.uniform4iv(location, arrayValue);
    case 35670:
      return gl.uniform1iv(location, arrayValue);
    case 35671:
      return gl.uniform2iv(location, arrayValue);
    case 35672:
      return gl.uniform3iv(location, arrayValue);
    case 35673:
      return gl.uniform4iv(location, arrayValue);
    case 5125:
      return gl2.uniform1uiv(location, arrayValue, 1);
    case 36294:
      return gl2.uniform2uiv(location, arrayValue, 2);
    case 36295:
      return gl2.uniform3uiv(location, arrayValue, 3);
    case 36296:
      return gl2.uniform4uiv(location, arrayValue, 4);
    case 35674:
      return gl.uniformMatrix2fv(location, false, arrayValue);
    case 35675:
      return gl.uniformMatrix3fv(location, false, arrayValue);
    case 35676:
      return gl.uniformMatrix4fv(location, false, arrayValue);
    case 35685:
      return gl2.uniformMatrix2x3fv(location, false, arrayValue);
    case 35686:
      return gl2.uniformMatrix2x4fv(location, false, arrayValue);
    case 35687:
      return gl2.uniformMatrix3x2fv(location, false, arrayValue);
    case 35688:
      return gl2.uniformMatrix3x4fv(location, false, arrayValue);
    case 35689:
      return gl2.uniformMatrix4x2fv(location, false, arrayValue);
    case 35690:
      return gl2.uniformMatrix4x3fv(location, false, arrayValue);
  }
  throw new Error("Illegal uniform");
}
var import_constants17;
var init_set_uniform = __esm({
  "dist/adapter/helpers/set-uniform.js"() {
    "use strict";
    import_constants17 = require("@luma.gl/constants");
  }
});

// dist/adapter/helpers/webgl-topology-utils.js
function getGLDrawMode(topology) {
  switch (topology) {
    case "point-list":
      return 0;
    case "line-list":
      return 1;
    case "line-strip":
      return 3;
    case "triangle-list":
      return 4;
    case "triangle-strip":
      return 5;
    default:
      throw new Error(topology);
  }
}
function getGLPrimitive(topology) {
  switch (topology) {
    case "point-list":
      return 0;
    case "line-list":
      return 1;
    case "line-strip":
      return 1;
    case "triangle-list":
      return 4;
    case "triangle-strip":
      return 4;
    default:
      throw new Error(topology);
  }
}
var import_constants18;
var init_webgl_topology_utils = __esm({
  "dist/adapter/helpers/webgl-topology-utils.js"() {
    "use strict";
    import_constants18 = require("@luma.gl/constants");
  }
});

// dist/adapter/resources/webgl-render-pipeline.js
function mergeShaderLayout(baseLayout, overrideLayout) {
  const mergedLayout = {
    ...baseLayout,
    attributes: baseLayout.attributes.map((attribute) => ({ ...attribute }))
  };
  for (const attribute of (overrideLayout == null ? void 0 : overrideLayout.attributes) || []) {
    const baseAttribute = mergedLayout.attributes.find((attr) => attr.name === attribute.name);
    if (!baseAttribute) {
      import_core15.log.warn(`shader layout attribute ${attribute.name} not present in shader`);
    } else {
      baseAttribute.type = attribute.type || baseAttribute.type;
      baseAttribute.stepMode = attribute.stepMode || baseAttribute.stepMode;
    }
  }
  return mergedLayout;
}
var import_core15, import_constants19, LOG_PROGRAM_PERF_PRIORITY, WEBGLRenderPipeline;
var init_webgl_render_pipeline = __esm({
  "dist/adapter/resources/webgl-render-pipeline.js"() {
    "use strict";
    import_core15 = require("@luma.gl/core");
    import_constants19 = require("@luma.gl/constants");
    init_get_shader_layout_from_glsl();
    init_device_parameters();
    init_set_uniform();
    init_webgl_buffer();
    init_webgl_framebuffer();
    init_webgl_texture();
    init_webgl_texture_view();
    init_webgl_topology_utils();
    LOG_PROGRAM_PERF_PRIORITY = 4;
    WEBGLRenderPipeline = class extends import_core15.RenderPipeline {
      /** The WebGL device that created this render pipeline */
      device;
      /** Handle to underlying WebGL program */
      handle;
      /** vertex shader */
      vs;
      /** fragment shader */
      fs;
      /** The layout extracted from shader by WebGL introspection APIs */
      introspectedLayout;
      /** Uniforms set on this model */
      uniforms = {};
      /** Bindings set on this model */
      bindings = {};
      /** WebGL varyings */
      varyings = null;
      _uniformCount = 0;
      _uniformSetters = {};
      // TODO are these used?
      get [Symbol.toStringTag]() {
        return "WEBGLRenderPipeline";
      }
      constructor(device, props) {
        super(device, props);
        this.device = device;
        this.handle = this.props.handle || this.device.gl.createProgram();
        this.device._setWebGLDebugMetadata(this.handle, this, { spector: { id: this.props.id } });
        this.vs = props.vs;
        this.fs = props.fs;
        const { varyings, bufferMode = 35981 } = props;
        if (varyings && varyings.length > 0) {
          this.varyings = varyings;
          this.device.gl.transformFeedbackVaryings(this.handle, varyings, bufferMode);
        }
        this._linkShaders();
        import_core15.log.time(3, `RenderPipeline ${this.id} - shaderLayout introspection`)();
        this.introspectedLayout = getShaderLayoutFromGLSL(this.device.gl, this.handle);
        import_core15.log.timeEnd(3, `RenderPipeline ${this.id} - shaderLayout introspection`)();
        this.shaderLayout = props.shaderLayout ? mergeShaderLayout(this.introspectedLayout, props.shaderLayout) : this.introspectedLayout;
      }
      destroy() {
        if (this.handle) {
          this.device.gl.useProgram(null);
          this.device.gl.deleteProgram(this.handle);
          this.destroyed = true;
          this.handle.destroyed = true;
          this.handle = null;
        }
      }
      /**
       * Bindings include: textures, samplers and uniform buffers
       * @todo needed for portable model
       */
      setBindings(bindings, options) {
        for (const [name, value] of Object.entries(bindings)) {
          const binding = this.shaderLayout.bindings.find((binding_) => binding_.name === name) || this.shaderLayout.bindings.find((binding_) => binding_.name === `${name}Uniforms`);
          if (!binding) {
            const validBindings = this.shaderLayout.bindings.map((binding_) => `"${binding_.name}"`).join(", ");
            if (!(options == null ? void 0 : options.disableWarnings)) {
              import_core15.log.warn(`No binding "${name}" in render pipeline "${this.id}", expected one of ${validBindings}`, value)();
            }
            continue;
          }
          if (!value) {
            import_core15.log.warn(`Unsetting binding "${name}" in render pipeline "${this.id}"`)();
          }
          switch (binding.type) {
            case "uniform":
              if (!(value instanceof WEBGLBuffer) && !(value.buffer instanceof WEBGLBuffer)) {
                throw new Error("buffer value");
              }
              break;
            case "texture":
              if (!(value instanceof WEBGLTextureView || value instanceof WEBGLTexture || value instanceof WEBGLFramebuffer)) {
                throw new Error(`${this} Bad texture binding for ${name}`);
              }
              break;
            case "sampler":
              import_core15.log.warn(`Ignoring sampler ${name}`)();
              break;
            default:
              throw new Error(binding.type);
          }
          this.bindings[name] = value;
        }
      }
      /** @todo needed for portable model
       * @note The WebGL API is offers many ways to draw things
       * This function unifies those ways into a single call using common parameters with sane defaults
       */
      draw(options) {
        var _a;
        const {
          renderPass,
          parameters = this.props.parameters,
          topology = this.props.topology,
          vertexArray,
          vertexCount,
          // indexCount,
          instanceCount,
          isInstanced = false,
          firstVertex = 0,
          // firstIndex,
          // firstInstance,
          // baseVertex,
          transformFeedback
        } = options;
        const glDrawMode = getGLDrawMode(topology);
        const isIndexed = Boolean(vertexArray.indexBuffer);
        const glIndexType = (_a = vertexArray.indexBuffer) == null ? void 0 : _a.glIndexType;
        if (this.linkStatus !== "success") {
          import_core15.log.info(2, `RenderPipeline:${this.id}.draw() aborted - waiting for shader linking`)();
          return false;
        }
        if (!this._areTexturesRenderable()) {
          import_core15.log.info(2, `RenderPipeline:${this.id}.draw() aborted - textures not yet loaded`)();
          return false;
        }
        this.device.gl.useProgram(this.handle);
        vertexArray.bindBeforeRender(renderPass);
        if (transformFeedback) {
          transformFeedback.begin(this.props.topology);
        }
        this._applyBindings();
        this._applyUniforms();
        const webglRenderPass = renderPass;
        withDeviceAndGLParameters(this.device, parameters, webglRenderPass.glParameters, () => {
          if (isIndexed && isInstanced) {
            this.device.gl.drawElementsInstanced(
              glDrawMode,
              vertexCount || 0,
              // indexCount?
              glIndexType,
              firstVertex,
              instanceCount || 0
            );
          } else if (isIndexed) {
            this.device.gl.drawElements(glDrawMode, vertexCount || 0, glIndexType, firstVertex);
          } else if (isInstanced) {
            this.device.gl.drawArraysInstanced(glDrawMode, firstVertex, vertexCount || 0, instanceCount || 0);
          } else {
            this.device.gl.drawArrays(glDrawMode, firstVertex, vertexCount || 0);
          }
          if (transformFeedback) {
            transformFeedback.end();
          }
        });
        vertexArray.unbindAfterRender(renderPass);
        return true;
      }
      // PRIVATE METHODS
      // setAttributes(attributes: Record<string, Buffer>): void {}
      // setBindings(bindings: Record<string, Binding>): void {}
      async _linkShaders() {
        const { gl } = this.device;
        gl.attachShader(this.handle, this.vs.handle);
        gl.attachShader(this.handle, this.fs.handle);
        import_core15.log.time(LOG_PROGRAM_PERF_PRIORITY, `linkProgram for ${this.id}`)();
        gl.linkProgram(this.handle);
        import_core15.log.timeEnd(LOG_PROGRAM_PERF_PRIORITY, `linkProgram for ${this.id}`)();
        if (import_core15.log.level === 0) {
        }
        if (!this.device.features.has("compilation-status-async-webgl")) {
          const status2 = this._getLinkStatus();
          this._reportLinkStatus(status2);
          return;
        }
        import_core15.log.once(1, "RenderPipeline linking is asynchronous")();
        await this._waitForLinkComplete();
        import_core15.log.info(2, `RenderPipeline ${this.id} - async linking complete: ${this.linkStatus}`)();
        const status = this._getLinkStatus();
        this._reportLinkStatus(status);
      }
      /** Report link status. First, check for shader compilation failures if linking fails */
      async _reportLinkStatus(status) {
        var _a;
        switch (status) {
          case "success":
            return;
          default:
            const errorType = status === "link-error" ? "Link error" : "Validation error";
            switch (this.vs.compilationStatus) {
              case "error":
                this.vs.debugShader();
                throw new Error(`${this} ${errorType} during compilation of ${this.vs}`);
              case "pending":
                await this.vs.asyncCompilationStatus;
                this.vs.debugShader();
                break;
              case "success":
                break;
            }
            switch ((_a = this.fs) == null ? void 0 : _a.compilationStatus) {
              case "error":
                this.fs.debugShader();
                throw new Error(`${this} ${errorType} during compilation of ${this.fs}`);
              case "pending":
                await this.fs.asyncCompilationStatus;
                this.fs.debugShader();
                break;
              case "success":
                break;
            }
            const linkErrorLog = this.device.gl.getProgramInfoLog(this.handle);
            this.device.reportError(new Error(`${errorType} during ${status}: ${linkErrorLog}`), this)();
            this.device.debug();
        }
      }
      /**
       * Get the shader compilation status
       * TODO - Load log even when no error reported, to catch warnings?
       * https://gamedev.stackexchange.com/questions/30429/how-to-detect-glsl-warnings
       */
      _getLinkStatus() {
        const { gl } = this.device;
        const linked = gl.getProgramParameter(this.handle, 35714);
        if (!linked) {
          this.linkStatus = "error";
          return "link-error";
        }
        gl.validateProgram(this.handle);
        const validated = gl.getProgramParameter(this.handle, 35715);
        if (!validated) {
          this.linkStatus = "error";
          return "validation-error";
        }
        this.linkStatus = "success";
        return "success";
      }
      /** Use KHR_parallel_shader_compile extension if available */
      async _waitForLinkComplete() {
        const waitMs = async (ms) => await new Promise((resolve) => setTimeout(resolve, ms));
        const DELAY_MS = 10;
        if (!this.device.features.has("compilation-status-async-webgl")) {
          await waitMs(DELAY_MS);
          return;
        }
        const { gl } = this.device;
        for (; ; ) {
          const complete = gl.getProgramParameter(this.handle, 37297);
          if (complete) {
            return;
          }
          await waitMs(DELAY_MS);
        }
      }
      /**
       * Checks if all texture-values uniforms are renderable (i.e. loaded)
       * Update a texture if needed (e.g. from video)
       * Note: This is currently done before every draw call
       */
      _areTexturesRenderable() {
        let texturesRenderable = true;
        for (const bindingInfo of this.shaderLayout.bindings) {
          if (!this.bindings[bindingInfo.name] && !this.bindings[bindingInfo.name.replace(/Uniforms$/, "")]) {
            import_core15.log.warn(`Binding ${bindingInfo.name} not found in ${this.id}`)();
            texturesRenderable = false;
          }
        }
        return texturesRenderable;
      }
      /** Apply any bindings (before each draw call) */
      _applyBindings() {
        if (this.linkStatus !== "success") {
          return;
        }
        const { gl } = this.device;
        gl.useProgram(this.handle);
        let textureUnit = 0;
        let uniformBufferIndex = 0;
        for (const binding of this.shaderLayout.bindings) {
          const value = this.bindings[binding.name] || this.bindings[binding.name.replace(/Uniforms$/, "")];
          if (!value) {
            throw new Error(`No value for binding ${binding.name} in ${this.id}`);
          }
          switch (binding.type) {
            case "uniform":
              const { name } = binding;
              const location = gl.getUniformBlockIndex(this.handle, name);
              if (location === 4294967295) {
                throw new Error(`Invalid uniform block name ${name}`);
              }
              gl.uniformBlockBinding(this.handle, uniformBufferIndex, location);
              if (value instanceof WEBGLBuffer) {
                gl.bindBufferBase(35345, uniformBufferIndex, value.handle);
              } else {
                gl.bindBufferRange(
                  35345,
                  uniformBufferIndex,
                  // @ts-expect-error
                  value.buffer.handle,
                  // @ts-expect-error
                  value.offset || 0,
                  // @ts-expect-error
                  value.size || value.buffer.byteLength - value.offset
                );
              }
              uniformBufferIndex += 1;
              break;
            case "texture":
              if (!(value instanceof WEBGLTextureView || value instanceof WEBGLTexture || value instanceof WEBGLFramebuffer)) {
                throw new Error("texture");
              }
              let texture;
              if (value instanceof WEBGLTextureView) {
                texture = value.texture;
              } else if (value instanceof WEBGLTexture) {
                texture = value;
              } else if (value instanceof WEBGLFramebuffer && value.colorAttachments[0] instanceof WEBGLTextureView) {
                import_core15.log.warn("Passing framebuffer in texture binding may be deprecated. Use fbo.colorAttachments[0] instead")();
                texture = value.colorAttachments[0].texture;
              } else {
                throw new Error("No texture");
              }
              gl.activeTexture(33984 + textureUnit);
              gl.bindTexture(texture.glTarget, texture.handle);
              textureUnit += 1;
              break;
            case "sampler":
              break;
            case "storage":
            case "read-only-storage":
              throw new Error(`binding type '${binding.type}' not supported in WebGL`);
          }
        }
      }
      /**
       * Due to program sharing, uniforms need to be reset before every draw call
       * (though caching will avoid redundant WebGL calls)
       */
      _applyUniforms() {
        for (const uniformLayout of this.shaderLayout.uniforms || []) {
          const { name, location, type, textureUnit } = uniformLayout;
          const value = this.uniforms[name] ?? textureUnit;
          if (value !== void 0) {
            setUniform(this.device.gl, location, type, value);
          }
        }
      }
    };
  }
});

// dist/adapter/resources/webgl-command-buffer.js
function _copyBufferToBuffer(device, options) {
  const source = options.sourceBuffer;
  const destination = options.destinationBuffer;
  device.gl.bindBuffer(36662, source.handle);
  device.gl.bindBuffer(36663, destination.handle);
  device.gl.copyBufferSubData(36662, 36663, options.sourceOffset ?? 0, options.destinationOffset ?? 0, options.size);
  device.gl.bindBuffer(36662, null);
  device.gl.bindBuffer(36663, null);
}
function _copyBufferToTexture(device, options) {
  throw new Error("Not implemented");
}
function _copyTextureToBuffer(device, options) {
  const {
    /** Texture to copy to/from. */
    sourceTexture,
    /**  Mip-map level of the texture to copy to/from. (Default 0) */
    mipLevel = 0,
    /** Defines which aspects of the texture to copy to/from. */
    aspect = "all",
    /** Width to copy */
    width = options.sourceTexture.width,
    /** Height to copy */
    height = options.sourceTexture.height,
    depthOrArrayLayers = 0,
    /** Defines the origin of the copy - the minimum corner of the texture sub-region to copy to/from. */
    origin = [0, 0],
    /** Destination buffer */
    destinationBuffer,
    /** Offset, in bytes, from the beginning of the buffer to the start of the image data (default 0) */
    byteOffset = 0,
    /**
     * The stride, in bytes, between the beginning of each block row and the subsequent block row.
     * Required if there are multiple block rows (i.e. the copy height or depth is more than one block).
     */
    bytesPerRow,
    /**
     * Number of block rows per single image of the texture.
     * rowsPerImage &times; bytesPerRow is the stride, in bytes, between the beginning of each image of data and the subsequent image.
     * Required if there are multiple images (i.e. the copy depth is more than one).
     */
    rowsPerImage
  } = options;
  if (aspect !== "all") {
    throw new Error("aspect not supported in WebGL");
  }
  if (mipLevel !== 0 || depthOrArrayLayers !== 0 || bytesPerRow || rowsPerImage) {
    throw new Error("not implemented");
  }
  const { framebuffer, destroyFramebuffer } = getFramebuffer(sourceTexture);
  let prevHandle;
  try {
    const webglBuffer = destinationBuffer;
    const sourceWidth = width || framebuffer.width;
    const sourceHeight = height || framebuffer.height;
    const sourceParams = getTextureFormatWebGL(framebuffer.colorAttachments[0].texture.props.format);
    const sourceFormat = sourceParams.format;
    const sourceType = sourceParams.type;
    device.gl.bindBuffer(35051, webglBuffer.handle);
    prevHandle = device.gl.bindFramebuffer(36160, framebuffer.handle);
    device.gl.readPixels(origin[0], origin[1], sourceWidth, sourceHeight, sourceFormat, sourceType, byteOffset);
  } finally {
    device.gl.bindBuffer(35051, null);
    if (prevHandle !== void 0) {
      device.gl.bindFramebuffer(36160, prevHandle);
    }
    if (destroyFramebuffer) {
      framebuffer.destroy();
    }
  }
}
function _copyTextureToTexture(device, options) {
  const {
    /** Texture to copy to/from. */
    sourceTexture,
    /**  Mip-map level of the texture to copy to (Default 0) */
    destinationMipLevel = 0,
    /** Defines which aspects of the texture to copy to/from. */
    // aspect = 'all',
    /** Defines the origin of the copy - the minimum corner of the texture sub-region to copy from. */
    origin = [0, 0],
    /** Defines the origin of the copy - the minimum corner of the texture sub-region to copy to. */
    destinationOrigin = [0, 0],
    /** Texture to copy to/from. */
    destinationTexture
    /**  Mip-map level of the texture to copy to/from. (Default 0) */
    // destinationMipLevel = options.mipLevel,
    /** Defines the origin of the copy - the minimum corner of the texture sub-region to copy to/from. */
    // destinationOrigin = [0, 0],
    /** Defines which aspects of the texture to copy to/from. */
    // destinationAspect = options.aspect,
  } = options;
  let {
    width = options.destinationTexture.width,
    height = options.destinationTexture.height
    // depthOrArrayLayers = 0
  } = options;
  const { framebuffer, destroyFramebuffer } = getFramebuffer(sourceTexture);
  const [sourceX, sourceY] = origin;
  const [destinationX, destinationY, destinationZ] = destinationOrigin;
  const prevHandle = device.gl.bindFramebuffer(36160, framebuffer.handle);
  let texture;
  let textureTarget;
  if (destinationTexture instanceof WEBGLTexture) {
    texture = destinationTexture;
    width = Number.isFinite(width) ? width : texture.width;
    height = Number.isFinite(height) ? height : texture.height;
    texture._bind(0);
    textureTarget = texture.glTarget;
  } else {
    throw new Error("invalid destination");
  }
  switch (textureTarget) {
    case 3553:
    case 34067:
      device.gl.copyTexSubImage2D(textureTarget, destinationMipLevel, destinationX, destinationY, sourceX, sourceY, width, height);
      break;
    case 35866:
    case 32879:
      device.gl.copyTexSubImage3D(textureTarget, destinationMipLevel, destinationX, destinationY, destinationZ, sourceX, sourceY, width, height);
      break;
    default:
  }
  if (texture) {
    texture._unbind();
  }
  device.gl.bindFramebuffer(36160, prevHandle);
  if (destroyFramebuffer) {
    framebuffer.destroy();
  }
}
function getFramebuffer(source) {
  if (source instanceof import_core16.Texture) {
    const { width, height, id } = source;
    const framebuffer = source.device.createFramebuffer({
      id: `framebuffer-for-${id}`,
      width,
      height,
      colorAttachments: [source]
    });
    return { framebuffer, destroyFramebuffer: true };
  }
  return { framebuffer: source, destroyFramebuffer: false };
}
var import_core16, import_constants20, WEBGLCommandBuffer;
var init_webgl_command_buffer = __esm({
  "dist/adapter/resources/webgl-command-buffer.js"() {
    "use strict";
    import_core16 = require("@luma.gl/core");
    import_constants20 = require("@luma.gl/constants");
    init_webgl_texture();
    init_webgl_texture_table();
    WEBGLCommandBuffer = class extends import_core16.CommandBuffer {
      device;
      handle = null;
      commands = [];
      constructor(device) {
        super(device, {});
        this.device = device;
      }
      _executeCommands(commands = this.commands) {
        for (const command of commands) {
          switch (command.name) {
            case "copy-buffer-to-buffer":
              _copyBufferToBuffer(this.device, command.options);
              break;
            case "copy-buffer-to-texture":
              _copyBufferToTexture(this.device, command.options);
              break;
            case "copy-texture-to-buffer":
              _copyTextureToBuffer(this.device, command.options);
              break;
            case "copy-texture-to-texture":
              _copyTextureToTexture(this.device, command.options);
              break;
            default:
              throw new Error(command.name);
          }
        }
      }
    };
  }
});

// dist/adapter/resources/webgl-render-pass.js
var import_core17, import_constants21, COLOR_CHANNELS, WEBGLRenderPass;
var init_webgl_render_pass = __esm({
  "dist/adapter/resources/webgl-render-pass.js"() {
    "use strict";
    import_core17 = require("@luma.gl/core");
    import_constants21 = require("@luma.gl/constants");
    init_with_parameters();
    init_unified_parameter_api();
    COLOR_CHANNELS = [1, 2, 4, 8];
    WEBGLRenderPass = class extends import_core17.RenderPass {
      device;
      handle = null;
      /** Parameters that should be applied before each draw call */
      glParameters = {};
      constructor(device, props) {
        var _a;
        super(device, props);
        this.device = device;
        let viewport;
        if (!((_a = props == null ? void 0 : props.parameters) == null ? void 0 : _a.viewport)) {
          if (props == null ? void 0 : props.framebuffer) {
            const { width, height } = props.framebuffer;
            viewport = [0, 0, width, height];
          } else {
            const [width, height] = device.getDefaultCanvasContext().getDrawingBufferSize();
            viewport = [0, 0, width, height];
          }
        }
        this.device.pushState();
        this.setParameters({ viewport, ...this.props.parameters });
        const webglFramebuffer = this.props.framebuffer;
        if (this.props.framebuffer && (webglFramebuffer == null ? void 0 : webglFramebuffer.handle)) {
          const drawBuffers = this.props.framebuffer.colorAttachments.map((_, i) => 36064 + i);
          this.device.gl.drawBuffers(drawBuffers);
        } else {
          this.device.gl.drawBuffers([1029]);
        }
        this.clear();
      }
      end() {
        this.device.popState();
      }
      pushDebugGroup(groupLabel) {
      }
      popDebugGroup() {
      }
      insertDebugMarker(markerLabel) {
      }
      // beginOcclusionQuery(queryIndex: number): void;
      // endOcclusionQuery(): void;
      // executeBundles(bundles: Iterable<GPURenderBundle>): void;
      /**
       * Maps RenderPass parameters to GL parameters
       */
      setParameters(parameters = {}) {
        const glParameters = { ...this.glParameters };
        glParameters.framebuffer = this.props.framebuffer || null;
        if (this.props.depthReadOnly) {
          glParameters.depthMask = !this.props.depthReadOnly;
        }
        glParameters.stencilMask = this.props.stencilReadOnly ? 0 : 1;
        glParameters[35977] = this.props.discard;
        if (parameters.viewport) {
          if (parameters.viewport.length >= 6) {
            glParameters.viewport = parameters.viewport.slice(0, 4);
            glParameters.depthRange = [
              parameters.viewport[4],
              parameters.viewport[5]
            ];
          } else {
            glParameters.viewport = parameters.viewport;
          }
        }
        if (parameters.scissorRect) {
          glParameters.scissorTest = true;
          glParameters.scissor = parameters.scissorRect;
        }
        if (parameters.blendConstant) {
          glParameters.blendColor = parameters.blendConstant;
        }
        if (parameters.stencilReference) {
          console.warn("RenderPassParameters.stencilReference not yet implemented in WebGL");
          glParameters[2967] = parameters.stencilReference;
        }
        if ("colorMask" in parameters) {
          glParameters.colorMask = COLOR_CHANNELS.map((channel) => Boolean(channel & parameters.colorMask));
        }
        this.glParameters = glParameters;
        setGLParameters(this.device.gl, glParameters);
      }
      beginOcclusionQuery(queryIndex) {
        const webglQuerySet = this.props.occlusionQuerySet;
        webglQuerySet == null ? void 0 : webglQuerySet.beginOcclusionQuery();
      }
      endOcclusionQuery() {
        const webglQuerySet = this.props.occlusionQuerySet;
        webglQuerySet == null ? void 0 : webglQuerySet.endOcclusionQuery();
      }
      // PRIVATE
      /**
       * Optionally clears depth, color and stencil buffers based on parameters
       */
      clear() {
        const glParameters = { ...this.glParameters };
        let clearMask = 0;
        if (this.props.clearColors) {
          this.props.clearColors.forEach((color, drawBufferIndex) => {
            if (color) {
              this.clearColorBuffer(drawBufferIndex, color);
            }
          });
        }
        if (this.props.clearColor !== false && this.props.clearColors === void 0) {
          clearMask |= 16384;
          glParameters.clearColor = this.props.clearColor;
        }
        if (this.props.clearDepth !== false) {
          clearMask |= 256;
          glParameters.clearDepth = this.props.clearDepth;
        }
        if (this.props.clearStencil !== false) {
          clearMask |= 1024;
          glParameters.clearStencil = this.props.clearStencil;
        }
        if (clearMask !== 0) {
          withGLParameters(this.device.gl, glParameters, () => {
            this.device.gl.clear(clearMask);
          });
        }
      }
      /**
       * WebGL2 - clear a specific color buffer
       */
      clearColorBuffer(drawBuffer = 0, value = [0, 0, 0, 0]) {
        withGLParameters(this.device.gl, { framebuffer: this.props.framebuffer }, () => {
          switch (value.constructor) {
            case Int8Array:
            case Int16Array:
            case Int32Array:
              this.device.gl.clearBufferiv(6144, drawBuffer, value);
              break;
            case Uint8Array:
            case Uint8ClampedArray:
            case Uint16Array:
            case Uint32Array:
              this.device.gl.clearBufferuiv(6144, drawBuffer, value);
              break;
            case Float32Array:
              this.device.gl.clearBufferfv(6144, drawBuffer, value);
              break;
            default:
              throw new Error("clearColorBuffer: color must be typed array");
          }
        });
      }
    };
  }
});

// dist/adapter/resources/webgl-command-encoder.js
var import_core18, WEBGLCommandEncoder;
var init_webgl_command_encoder = __esm({
  "dist/adapter/resources/webgl-command-encoder.js"() {
    "use strict";
    import_core18 = require("@luma.gl/core");
    init_webgl_command_buffer();
    init_webgl_render_pass();
    WEBGLCommandEncoder = class extends import_core18.CommandEncoder {
      device;
      handle = null;
      commandBuffer;
      constructor(device, props) {
        super(device, props);
        this.device = device;
        this.commandBuffer = new WEBGLCommandBuffer(device);
      }
      destroy() {
      }
      finish() {
        return this.commandBuffer;
      }
      beginRenderPass(props) {
        return new WEBGLRenderPass(this.device, props);
      }
      beginComputePass(props) {
        throw new Error("ComputePass not supported in WebGL");
      }
      copyBufferToBuffer(options) {
        this.commandBuffer.commands.push({ name: "copy-buffer-to-buffer", options });
      }
      copyBufferToTexture(options) {
        this.commandBuffer.commands.push({ name: "copy-buffer-to-texture", options });
      }
      copyTextureToBuffer(options) {
        this.commandBuffer.commands.push({ name: "copy-texture-to-buffer", options });
      }
      copyTextureToTexture(options) {
        this.commandBuffer.commands.push({ name: "copy-texture-to-texture", options });
      }
      // clearTexture(options: ClearTextureOptions): void {
      //   this.commandBuffer.commands.push({name: 'copy-texture-to-texture', options});
      // }
      pushDebugGroup(groupLabel) {
      }
      popDebugGroup() {
      }
      insertDebugMarker(markerLabel) {
      }
      resolveQuerySet(querySet, destination, options) {
      }
    };
  }
});

// dist/utils/fill-array.js
function fillArray(options) {
  const { target: target2, source, start = 0, count = 1 } = options;
  const length = source.length;
  const total = count * length;
  let copied = 0;
  for (let i = start; copied < length; copied++) {
    target2[i++] = source[copied];
  }
  while (copied < total) {
    if (copied < total - copied) {
      target2.copyWithin(start + copied, start, start + copied);
      copied *= 2;
    } else {
      target2.copyWithin(start + copied, start, start + total - copied);
      copied = total;
    }
  }
  return options.target;
}
var init_fill_array = __esm({
  "dist/utils/fill-array.js"() {
    "use strict";
  }
});

// dist/adapter/resources/webgl-vertex-array.js
function normalizeConstantArrayValue(arrayValue) {
  if (Array.isArray(arrayValue)) {
    return new Float32Array(arrayValue);
  }
  return arrayValue;
}
function compareConstantArrayValues(v1, v2) {
  if (!v1 || !v2 || v1.length !== v2.length || v1.constructor !== v2.constructor) {
    return false;
  }
  for (let i = 0; i < v1.length; ++i) {
    if (v1[i] !== v2[i]) {
      return false;
    }
  }
  return true;
}
var import_core19, import_constants22, import_env2, WEBGLVertexArray;
var init_webgl_vertex_array = __esm({
  "dist/adapter/resources/webgl-vertex-array.js"() {
    "use strict";
    import_core19 = require("@luma.gl/core");
    import_constants22 = require("@luma.gl/constants");
    import_env2 = require("@probe.gl/env");
    init_webgl_vertex_formats();
    init_fill_array();
    WEBGLVertexArray = class extends import_core19.VertexArray {
      get [Symbol.toStringTag]() {
        return "VertexArray";
      }
      device;
      handle;
      /** Attribute 0 buffer constant */
      buffer = null;
      bufferValue = null;
      /** * Attribute 0 can not be disable on most desktop OpenGL based browsers */
      static isConstantAttributeZeroSupported(device) {
        return (0, import_env2.getBrowser)() === "Chrome";
      }
      // Create a VertexArray
      constructor(device, props) {
        super(device, props);
        this.device = device;
        this.handle = this.device.gl.createVertexArray();
      }
      destroy() {
        var _a;
        super.destroy();
        if (this.buffer) {
          (_a = this.buffer) == null ? void 0 : _a.destroy();
        }
        if (this.handle) {
          this.device.gl.deleteVertexArray(this.handle);
          this.handle = void 0;
        }
      }
      /**
      // Set (bind/unbind) an elements buffer, for indexed rendering.
      // Must be a Buffer bound to GL.ELEMENT_ARRAY_BUFFER or null. Constants not supported
       *
       * @param elementBuffer
       */
      setIndexBuffer(indexBuffer) {
        const buffer = indexBuffer;
        if (buffer && buffer.glTarget !== 34963) {
          throw new Error("Use .setBuffer()");
        }
        this.device.gl.bindVertexArray(this.handle);
        this.device.gl.bindBuffer(34963, buffer ? buffer.handle : null);
        this.indexBuffer = buffer;
        this.device.gl.bindVertexArray(null);
      }
      /** Set a location in vertex attributes array to a buffer, enables the location, sets divisor */
      setBuffer(location, attributeBuffer) {
        const buffer = attributeBuffer;
        if (buffer.glTarget === 34963) {
          throw new Error("Use .setIndexBuffer()");
        }
        const { size, type, stride, offset, normalized, integer, divisor } = this._getAccessor(location);
        this.device.gl.bindVertexArray(this.handle);
        this.device.gl.bindBuffer(34962, buffer.handle);
        if (integer) {
          this.device.gl.vertexAttribIPointer(location, size, type, stride, offset);
        } else {
          this.device.gl.vertexAttribPointer(location, size, type, normalized, stride, offset);
        }
        this.device.gl.bindBuffer(34962, null);
        this.device.gl.enableVertexAttribArray(location);
        this.device.gl.vertexAttribDivisor(location, divisor || 0);
        this.attributes[location] = buffer;
        this.device.gl.bindVertexArray(null);
      }
      /** Set a location in vertex attributes array to a constant value, disables the location */
      setConstantWebGL(location, value) {
        this._enable(location, false);
        this.attributes[location] = value;
      }
      bindBeforeRender() {
        this.device.gl.bindVertexArray(this.handle);
        this._applyConstantAttributes();
      }
      unbindAfterRender() {
        this.device.gl.bindVertexArray(null);
      }
      // Internal methods
      /**
       * Constant attributes need to be reset before every draw call
       * Any attribute that is disabled in the current vertex array object
       * is read from the context's global constant value for that attribute location.
       * @note Constant attributes are only supported in WebGL, not in WebGPU
       */
      _applyConstantAttributes() {
        for (let location = 0; location < this.maxVertexAttributes; ++location) {
          const constant = this.attributes[location];
          if (ArrayBuffer.isView(constant)) {
            this.device.setConstantAttributeWebGL(location, constant);
          }
        }
      }
      /**
       * Set a location in vertex attributes array to a buffer, enables the location, sets divisor
       * @note requires vertex array to be bound
       */
      // protected _setAttributeLayout(location: number): void {
      //   const {size, type, stride, offset, normalized, integer, divisor} = this._getAccessor(location);
      //   // WebGL2 supports *integer* data formats, i.e. GPU will see integer values
      //   if (integer) {
      //     this.device.gl.vertexAttribIPointer(location, size, type, stride, offset);
      //   } else {
      //     // Attaches ARRAY_BUFFER with specified buffer format to location
      //     this.device.gl.vertexAttribPointer(location, size, type, normalized, stride, offset);
      //   }
      //   this.device.gl.vertexAttribDivisor(location, divisor || 0);
      // }
      /** Get an accessor from the  */
      _getAccessor(location) {
        const attributeInfo = this.attributeInfos[location];
        if (!attributeInfo) {
          throw new Error(`Unknown attribute location ${location}`);
        }
        const glType = getGLFromVertexType(attributeInfo.bufferDataType);
        return {
          size: attributeInfo.bufferComponents,
          type: glType,
          stride: attributeInfo.byteStride,
          offset: attributeInfo.byteOffset,
          normalized: attributeInfo.normalized,
          // it is the shader attribute declaration, not the vertex memory format,
          // that determines if the data in the buffer will be treated as integers.
          //
          // Also note that WebGL supports assigning non-normalized integer data to floating point attributes,
          // but as far as we can tell, WebGPU does not.
          integer: attributeInfo.integer,
          divisor: attributeInfo.stepMode === "instance" ? 1 : 0
        };
      }
      /**
       * Enabling an attribute location makes it reference the currently bound buffer
       * Disabling an attribute location makes it reference the global constant value
       * TODO - handle single values for size 1 attributes?
       * TODO - convert classic arrays based on known type?
       */
      _enable(location, enable2 = true) {
        const canDisableAttributeZero = WEBGLVertexArray.isConstantAttributeZeroSupported(this.device);
        const canDisableAttribute = canDisableAttributeZero || location !== 0;
        if (enable2 || canDisableAttribute) {
          location = Number(location);
          this.device.gl.bindVertexArray(this.handle);
          if (enable2) {
            this.device.gl.enableVertexAttribArray(location);
          } else {
            this.device.gl.disableVertexAttribArray(location);
          }
          this.device.gl.bindVertexArray(null);
        }
      }
      /**
       * Provide a means to create a buffer that is equivalent to a constant.
       * NOTE: Desktop OpenGL cannot disable attribute 0.
       * https://stackoverflow.com/questions/20305231/webgl-warning-attribute-0-is-disabled-
       * this-has-significant-performance-penalty
       */
      getConstantBuffer(elementCount, value) {
        const constantValue = normalizeConstantArrayValue(value);
        const byteLength = constantValue.byteLength * elementCount;
        const length = constantValue.length * elementCount;
        if (this.buffer && byteLength !== this.buffer.byteLength) {
          throw new Error(`Buffer size is immutable, byte length ${byteLength} !== ${this.buffer.byteLength}.`);
        }
        let updateNeeded = !this.buffer;
        this.buffer = this.buffer || this.device.createBuffer({ byteLength });
        updateNeeded ||= !compareConstantArrayValues(constantValue, this.bufferValue);
        if (updateNeeded) {
          const typedArray = (0, import_core19.getScratchArray)(value.constructor, length);
          fillArray({ target: typedArray, source: constantValue, start: 0, count: length });
          this.buffer.write(typedArray);
          this.bufferValue = value;
        }
        return this.buffer;
      }
    };
  }
});

// dist/adapter/resources/webgl-transform-feedback.js
function isIndex(value) {
  if (typeof value === "number") {
    return Number.isInteger(value);
  }
  return /^\d+$/.test(value);
}
var import_core20, import_constants23, WEBGLTransformFeedback;
var init_webgl_transform_feedback = __esm({
  "dist/adapter/resources/webgl-transform-feedback.js"() {
    "use strict";
    import_core20 = require("@luma.gl/core");
    import_constants23 = require("@luma.gl/constants");
    init_dist();
    init_webgl_topology_utils();
    WEBGLTransformFeedback = class extends import_core20.TransformFeedback {
      device;
      gl;
      handle;
      /**
       * NOTE: The Model already has this information while drawing, but
       * TransformFeedback currently needs it internally, to look up
       * varying information outside of a draw() call.
       */
      layout;
      buffers = {};
      unusedBuffers = {};
      /**
       * Allows us to avoid a Chrome bug where a buffer that is already bound to a
       * different target cannot be bound to 'TRANSFORM_FEEDBACK_BUFFER' target.
       * This a major workaround, see: https://github.com/KhronosGroup/WebGL/issues/2346
       */
      bindOnUse = true;
      _bound = false;
      constructor(device, props) {
        super(device, props);
        this.device = device;
        this.gl = device.gl;
        this.handle = this.props.handle || this.gl.createTransformFeedback();
        this.layout = this.props.layout;
        if (props.buffers) {
          this.setBuffers(props.buffers);
        }
        Object.seal(this);
      }
      destroy() {
        this.gl.deleteTransformFeedback(this.handle);
        super.destroy();
      }
      begin(topology = "point-list") {
        this.gl.bindTransformFeedback(36386, this.handle);
        if (this.bindOnUse) {
          this._bindBuffers();
        }
        this.gl.beginTransformFeedback(getGLPrimitive(topology));
      }
      end() {
        this.gl.endTransformFeedback();
        if (this.bindOnUse) {
          this._unbindBuffers();
        }
        this.gl.bindTransformFeedback(36386, null);
      }
      // SUBCLASS
      setBuffers(buffers) {
        this.buffers = {};
        this.unusedBuffers = {};
        this.bind(() => {
          for (const bufferName in buffers) {
            this.setBuffer(bufferName, buffers[bufferName]);
          }
        });
      }
      setBuffer(locationOrName, bufferOrRange) {
        const location = this._getVaryingIndex(locationOrName);
        const { buffer, byteLength, byteOffset } = this._getBufferRange(bufferOrRange);
        if (location < 0) {
          this.unusedBuffers[locationOrName] = buffer;
          import_core20.log.warn(`${this.id} unusedBuffers varying buffer ${locationOrName}`)();
          return;
        }
        this.buffers[location] = { buffer, byteLength, byteOffset };
        if (!this.bindOnUse) {
          this._bindBuffer(location, buffer, byteOffset, byteLength);
        }
      }
      getBuffer(locationOrName) {
        if (isIndex(locationOrName)) {
          return this.buffers[locationOrName] || null;
        }
        const location = this._getVaryingIndex(locationOrName);
        return location >= 0 ? this.buffers[location] : null;
      }
      bind(funcOrHandle = this.handle) {
        if (typeof funcOrHandle !== "function") {
          this.gl.bindTransformFeedback(36386, funcOrHandle);
          return this;
        }
        let value;
        if (!this._bound) {
          this.gl.bindTransformFeedback(36386, this.handle);
          this._bound = true;
          value = funcOrHandle();
          this._bound = false;
          this.gl.bindTransformFeedback(36386, null);
        } else {
          value = funcOrHandle();
        }
        return value;
      }
      unbind() {
        this.bind(null);
      }
      // PRIVATE METHODS
      /** Extract offsets for bindBufferRange */
      _getBufferRange(bufferOrRange) {
        if (bufferOrRange instanceof WEBGLBuffer) {
          return { buffer: bufferOrRange, byteOffset: 0, byteLength: bufferOrRange.byteLength };
        }
        const { buffer, byteOffset = 0, byteLength = bufferOrRange.buffer.byteLength } = bufferOrRange;
        return { buffer, byteOffset, byteLength };
      }
      _getVaryingIndex(locationOrName) {
        if (isIndex(locationOrName)) {
          return Number(locationOrName);
        }
        for (const varying of this.layout.varyings || []) {
          if (locationOrName === varying.name) {
            return varying.location;
          }
        }
        return -1;
      }
      /**
       * Need to avoid chrome bug where buffer that is already bound to a different target
       * cannot be bound to 'TRANSFORM_FEEDBACK_BUFFER' target.
       */
      _bindBuffers() {
        for (const bufferIndex in this.buffers) {
          const { buffer, byteLength, byteOffset } = this._getBufferRange(this.buffers[bufferIndex]);
          this._bindBuffer(Number(bufferIndex), buffer, byteOffset, byteLength);
        }
      }
      _unbindBuffers() {
        for (const bufferIndex in this.buffers) {
          this.gl.bindBufferBase(35982, Number(bufferIndex), null);
        }
      }
      _bindBuffer(index, buffer, byteOffset = 0, byteLength) {
        const handle = buffer && buffer.handle;
        if (!handle || byteLength === void 0) {
          this.gl.bindBufferBase(35982, index, handle);
        } else {
          this.gl.bindBufferRange(35982, index, handle, byteOffset, byteLength);
        }
      }
    };
  }
});

// dist/adapter/resources/webgl-query-set.js
var import_core21, import_constants24, WEBGLQuerySet;
var init_webgl_query_set = __esm({
  "dist/adapter/resources/webgl-query-set.js"() {
    "use strict";
    import_core21 = require("@luma.gl/core");
    import_constants24 = require("@luma.gl/constants");
    WEBGLQuerySet = class extends import_core21.QuerySet {
      device;
      handle;
      target = null;
      _queryPending = false;
      _pollingPromise = null;
      get [Symbol.toStringTag]() {
        return "Query";
      }
      // Create a query class
      constructor(device, props) {
        super(device, props);
        this.device = device;
        if (props.count > 1) {
          throw new Error("WebGL QuerySet can only have one value");
        }
        const handle = this.device.gl.createQuery();
        if (!handle) {
          throw new Error("WebGL query not supported");
        }
        this.handle = handle;
        Object.seal(this);
      }
      destroy() {
        this.device.gl.deleteQuery(this.handle);
      }
      // FOR RENDER PASS AND COMMAND ENCODER
      /**
       * Shortcut for timer query (dependent on extension in both WebGL1 and 2)
       * Measures GPU time delta between this call and a matching `end` call in the
       * GPU instruction stream.
       */
      beginTimestampQuery() {
        return this._begin(35007);
      }
      endTimestampQuery() {
        this._end();
      }
      // Shortcut for occlusion queries
      beginOcclusionQuery(options) {
        return this._begin((options == null ? void 0 : options.conservative) ? 36202 : 35887);
      }
      endOcclusionQuery() {
        this._end();
      }
      // Shortcut for transformFeedbackQuery
      beginTransformFeedbackQuery() {
        return this._begin(35976);
      }
      endTransformFeedbackQuery() {
        this._end();
      }
      async resolveQuery() {
        const value = await this.pollQuery();
        return [value];
      }
      // PRIVATE METHODS
      /**
       * Due to OpenGL API limitations, after calling `begin()` on one Query
       * instance, `end()` must be called on that same instance before
       * calling `begin()` on another query. While there can be multiple
       * outstanding queries representing disjoint `begin()`/`end()` intervals.
       * It is not possible to interleave or overlap `begin` and `end` calls.
       */
      _begin(target2) {
        if (this._queryPending) {
          return;
        }
        this.target = target2;
        this.device.gl.beginQuery(this.target, this.handle);
        return;
      }
      // ends the current query
      _end() {
        if (this._queryPending) {
          return;
        }
        if (this.target) {
          this.device.gl.endQuery(this.target);
          this.target = null;
          this._queryPending = true;
        }
        return;
      }
      // Returns true if the query result is available
      isResultAvailable() {
        if (!this._queryPending) {
          return false;
        }
        const resultAvailable = this.device.gl.getQueryParameter(this.handle, 34919);
        if (resultAvailable) {
          this._queryPending = false;
        }
        return resultAvailable;
      }
      // Timing query is disjoint, i.e. results are invalid
      isTimerDisjoint() {
        return this.device.gl.getParameter(36795);
      }
      // Returns query result.
      getResult() {
        return this.device.gl.getQueryParameter(this.handle, 34918);
      }
      // Returns the query result, converted to milliseconds to match JavaScript conventions.
      getTimerMilliseconds() {
        return this.getResult() / 1e6;
      }
      // Polls the query
      pollQuery(limit = Number.POSITIVE_INFINITY) {
        if (this._pollingPromise) {
          return this._pollingPromise;
        }
        let counter = 0;
        this._pollingPromise = new Promise((resolve, reject) => {
          const poll = () => {
            if (this.isResultAvailable()) {
              resolve(this.getResult());
              this._pollingPromise = null;
            } else if (counter++ > limit) {
              reject("Timed out");
              this._pollingPromise = null;
            } else {
              requestAnimationFrame(poll);
            }
          };
          requestAnimationFrame(poll);
        });
        return this._pollingPromise;
      }
    };
  }
});

// dist/adapter/helpers/format-utils.js
function glFormatToComponents(format) {
  switch (format) {
    case 6406:
    case 33326:
    case 6403:
    case 36244:
      return 1;
    case 33339:
    case 33340:
    case 33328:
    case 33320:
    case 33319:
      return 2;
    case 6407:
    case 36248:
    case 34837:
      return 3;
    case 6408:
    case 36249:
    case 34836:
      return 4;
    default:
      return 0;
  }
}
function glTypeToBytes(type) {
  switch (type) {
    case 5121:
      return 1;
    case 33635:
    case 32819:
    case 32820:
      return 2;
    case 5126:
      return 4;
    default:
      return 0;
  }
}
var import_constants25;
var init_format_utils = __esm({
  "dist/adapter/helpers/format-utils.js"() {
    "use strict";
    import_constants25 = require("@luma.gl/constants");
  }
});

// dist/adapter/converters/shader-formats.js
function convertGLDataTypeToDataType(type) {
  return GL_DATA_TYPE_MAP[type];
}
var import_constants26, GL_DATA_TYPE_MAP;
var init_shader_formats = __esm({
  "dist/adapter/converters/shader-formats.js"() {
    "use strict";
    import_constants26 = require("@luma.gl/constants");
    GL_DATA_TYPE_MAP = {
      [5124]: "sint32",
      [5125]: "uint32",
      [5122]: "sint16",
      [5123]: "uint16",
      [5120]: "sint8",
      [5121]: "uint8",
      [5126]: "float32",
      [5131]: "float16",
      [33635]: "uint16",
      [32819]: "uint16",
      [32820]: "uint16",
      [33640]: "uint32",
      [35899]: "uint32",
      [35902]: "uint32",
      [34042]: "uint32",
      [36269]: "uint32"
    };
  }
});

// dist/adapter/helpers/webgl-texture-utils.js
function readPixelsToArray(source, options) {
  var _a;
  const {
    sourceX = 0,
    sourceY = 0,
    sourceAttachment = 0
    // TODO - support gl.readBuffer
  } = options || {};
  let {
    target: target2 = null,
    // following parameters are auto deduced if not provided
    sourceWidth,
    sourceHeight,
    sourceDepth,
    sourceFormat,
    sourceType
  } = options || {};
  const { framebuffer, deleteFramebuffer } = getFramebuffer2(source);
  const { gl, handle } = framebuffer;
  sourceWidth ||= framebuffer.width;
  sourceHeight ||= framebuffer.height;
  const texture = (_a = framebuffer.colorAttachments[sourceAttachment]) == null ? void 0 : _a.texture;
  if (!texture) {
    throw new Error(`Invalid framebuffer attachment ${sourceAttachment}`);
  }
  sourceDepth = (texture == null ? void 0 : texture.depth) || 1;
  sourceFormat ||= (texture == null ? void 0 : texture.glFormat) || 6408;
  sourceType ||= (texture == null ? void 0 : texture.glType) || 5121;
  target2 = getPixelArray(target2, sourceType, sourceFormat, sourceWidth, sourceHeight, sourceDepth);
  const signedType = (0, import_core22.getDataType)(target2);
  sourceType = sourceType || convertDataTypeToGLDataType(signedType);
  const prevHandle = gl.bindFramebuffer(36160, handle);
  gl.readBuffer(36064 + sourceAttachment);
  gl.readPixels(sourceX, sourceY, sourceWidth, sourceHeight, sourceFormat, sourceType, target2);
  gl.readBuffer(36064);
  gl.bindFramebuffer(36160, prevHandle || null);
  if (deleteFramebuffer) {
    framebuffer.destroy();
  }
  return target2;
}
function readPixelsToBuffer(source, options) {
  const { target: target2, sourceX = 0, sourceY = 0, sourceFormat = 6408, targetByteOffset = 0 } = options || {};
  let { sourceWidth, sourceHeight, sourceType } = options || {};
  const { framebuffer, deleteFramebuffer } = getFramebuffer2(source);
  sourceWidth = sourceWidth || framebuffer.width;
  sourceHeight = sourceHeight || framebuffer.height;
  const webglFramebuffer = framebuffer;
  sourceType = sourceType || 5121;
  let webglBufferTarget = target2;
  if (!webglBufferTarget) {
    const components = glFormatToComponents(sourceFormat);
    const byteCount = glTypeToBytes(sourceType);
    const byteLength = targetByteOffset + sourceWidth * sourceHeight * components * byteCount;
    webglBufferTarget = webglFramebuffer.device.createBuffer({ byteLength });
  }
  const commandEncoder = source.device.createCommandEncoder();
  commandEncoder.copyTextureToBuffer({
    sourceTexture: source,
    width: sourceWidth,
    height: sourceHeight,
    origin: [sourceX, sourceY],
    destinationBuffer: webglBufferTarget,
    byteOffset: targetByteOffset
  });
  commandEncoder.destroy();
  if (deleteFramebuffer) {
    framebuffer.destroy();
  }
  return webglBufferTarget;
}
function getFramebuffer2(source) {
  if (!(source instanceof import_core22.Framebuffer)) {
    return { framebuffer: toFramebuffer(source), deleteFramebuffer: true };
  }
  return { framebuffer: source, deleteFramebuffer: false };
}
function toFramebuffer(texture, props) {
  const { device, width, height, id } = texture;
  const framebuffer = device.createFramebuffer({
    ...props,
    id: `framebuffer-for-${id}`,
    width,
    height,
    colorAttachments: [texture]
  });
  return framebuffer;
}
function getPixelArray(pixelArray, glType, glFormat, width, height, depth) {
  if (pixelArray) {
    return pixelArray;
  }
  glType ||= 5121;
  const shaderType = convertGLDataTypeToDataType(glType);
  const ArrayType = (0, import_core22.getTypedArrayConstructor)(shaderType);
  const components = glFormatToComponents(glFormat);
  return new ArrayType(width * height * components);
}
var import_core22, import_constants27;
var init_webgl_texture_utils = __esm({
  "dist/adapter/helpers/webgl-texture-utils.js"() {
    "use strict";
    import_core22 = require("@luma.gl/core");
    import_constants27 = require("@luma.gl/constants");
    init_webgl_shadertypes();
    init_format_utils();
    init_shader_formats();
  }
});

// dist/adapter/webgl-device.js
var webgl_device_exports = {};
__export(webgl_device_exports, {
  WebGLDevice: () => WebGLDevice
});
function setConstantFloatArray(device, location, array) {
  switch (array.length) {
    case 1:
      device.gl.vertexAttrib1fv(location, array);
      break;
    case 2:
      device.gl.vertexAttrib2fv(location, array);
      break;
    case 3:
      device.gl.vertexAttrib3fv(location, array);
      break;
    case 4:
      device.gl.vertexAttrib4fv(location, array);
      break;
    default:
  }
}
function setConstantIntArray(device, location, array) {
  device.gl.vertexAttribI4iv(location, array);
}
function setConstantUintArray(device, location, array) {
  device.gl.vertexAttribI4uiv(location, array);
}
function compareConstantArrayValues2(v1, v2) {
  if (!v1 || !v2 || v1.length !== v2.length || v1.constructor !== v2.constructor) {
    return false;
  }
  for (let i = 0; i < v1.length; ++i) {
    if (v1[i] !== v2[i]) {
      return false;
    }
  }
  return true;
}
var import_core23, WebGLDevice;
var init_webgl_device = __esm({
  "dist/adapter/webgl-device.js"() {
    "use strict";
    import_core23 = require("@luma.gl/core");
    init_webgl_state_tracker();
    init_create_browser_context();
    init_webgl_device_info();
    init_webgl_device_features();
    init_webgl_device_limits();
    init_webgl_canvas_context();
    init_spector();
    init_webgl_developer_tools();
    init_webgl_texture_table();
    init_uid();
    init_webgl_buffer();
    init_webgl_shader();
    init_webgl_sampler();
    init_webgl_texture();
    init_webgl_framebuffer();
    init_webgl_render_pipeline();
    init_webgl_command_encoder();
    init_webgl_vertex_array();
    init_webgl_transform_feedback();
    init_webgl_query_set();
    init_webgl_texture_utils();
    init_unified_parameter_api();
    init_with_parameters();
    init_webgl_extensions();
    WebGLDevice = class extends import_core23.Device {
      // Public `Device` API
      /** type of this device */
      type = "webgl";
      // Use the ! assertion to handle the case where _reuseDevices causes the constructor to return early
      /** The underlying WebGL context */
      handle;
      features;
      limits;
      info;
      canvasContext;
      preferredColorFormat = "rgba8unorm";
      preferredDepthFormat = "depth24plus";
      commandEncoder;
      lost;
      _resolveContextLost;
      /** WebGL2 context. */
      gl;
      /** Store constants */
      // @ts-ignore TODO fix
      _constants;
      /** State used by luma.gl classes - TODO - not used? */
      _extensions = {};
      _polyfilled = false;
      /** Instance of Spector.js (if initialized) */
      spectorJS;
      //
      // Public API
      //
      get [Symbol.toStringTag]() {
        return "WebGLDevice";
      }
      toString() {
        return `${this[Symbol.toStringTag]}(${this.id})`;
      }
      isVertexFormatSupported(format) {
        switch (format) {
          case "unorm8x4-bgra":
            return false;
          default:
            return true;
        }
      }
      constructor(props) {
        var _a, _b;
        super({ ...props, id: props.id || uid("webgl-device") });
        const canvasContextProps = import_core23.Device._getCanvasContextProps(props);
        if (!canvasContextProps) {
          throw new Error("WebGLDevice requires props.createCanvasContext to be set");
        }
        let device = (_b = (_a = canvasContextProps.canvas) == null ? void 0 : _a.gl) == null ? void 0 : _b.device;
        if (device) {
          throw new Error(`WebGL context already attached to device ${device.id}`);
        }
        this.canvasContext = new WebGLCanvasContext(this, canvasContextProps);
        this.lost = new Promise((resolve) => {
          this._resolveContextLost = resolve;
        });
        const webglContextAttributes = { ...props.webgl };
        if (canvasContextProps.alphaMode === "premultiplied") {
          webglContextAttributes.premultipliedAlpha = true;
        }
        if (props.powerPreference !== void 0) {
          webglContextAttributes.powerPreference = props.powerPreference;
        }
        const externalGLContext = this.props._handle;
        const gl = externalGLContext || createBrowserContext(this.canvasContext.canvas, {
          onContextLost: (event) => {
            var _a2;
            return (_a2 = this._resolveContextLost) == null ? void 0 : _a2.call(this, {
              reason: "destroyed",
              message: "Entered sleep mode, or too many apps or browser tabs are using the GPU."
            });
          },
          // eslint-disable-next-line no-console
          onContextRestored: (event) => console.log("WebGL context restored")
        }, webglContextAttributes);
        if (!gl) {
          throw new Error("WebGL context creation failed");
        }
        device = gl.device;
        if (device) {
          if (props._reuseDevices) {
            import_core23.log.log(1, `Not creating a new Device, instead returning a reference to Device ${device.id} already attached to WebGL context`, device)();
            device._reused = true;
            return device;
          }
          throw new Error(`WebGL context already attached to device ${device.id}`);
        }
        this.handle = gl;
        this.gl = gl;
        this.spectorJS = initializeSpectorJS({ ...this.props, gl: this.handle });
        this.gl.device = this;
        this.gl._version = 2;
        this.info = getDeviceInfo(this.gl, this._extensions);
        this.limits = new WebGLDeviceLimits(this.gl);
        this.features = new WebGLDeviceFeatures(this.gl, this._extensions, this.props._disabledFeatures);
        if (this.props._initializeFeatures) {
          this.features.initializeFeatures();
        }
        const glState = new WebGLStateTracker(this.gl, {
          log: (...args) => import_core23.log.log(1, ...args)()
        });
        glState.trackState(this.gl, { copyState: false });
        const debugWebGL = props.debugWebGL || props.debug;
        const traceWebGL = props.debugWebGL;
        if (debugWebGL) {
          this.gl = makeDebugContext(this.gl, { debugWebGL, traceWebGL });
          import_core23.log.warn("WebGL debug mode activated. Performance reduced.")();
          if (props.debugWebGL) {
            import_core23.log.level = Math.max(import_core23.log.level, 1);
          }
        }
        this.commandEncoder = new WEBGLCommandEncoder(this, { id: `${this}-command-encoder` });
      }
      /**
       * Destroys the device
       *
       * @note "Detaches" from the WebGL context unless _reuseDevices is true.
       *
       * @note The underlying WebGL context is not immediately destroyed,
       * but may be destroyed later through normal JavaScript garbage collection.
       * This is a fundamental limitation since WebGL does not offer any
       * browser API for destroying WebGL contexts.
       */
      destroy() {
        if (!this.props._reuseDevices && !this._reused) {
          delete this.gl.device;
        }
      }
      get isLost() {
        return this.gl.isContextLost();
      }
      // IMPLEMENTATION OF ABSTRACT DEVICE
      getTextureByteAlignment() {
        return 4;
      }
      createCanvasContext(props) {
        throw new Error("WebGL only supports a single canvas");
      }
      createBuffer(props) {
        const newProps = this._normalizeBufferProps(props);
        return new WEBGLBuffer(this, newProps);
      }
      createTexture(props) {
        return new WEBGLTexture(this, props);
      }
      createExternalTexture(props) {
        throw new Error("createExternalTexture() not implemented");
      }
      createSampler(props) {
        return new WEBGLSampler(this, props);
      }
      createShader(props) {
        return new WEBGLShader(this, props);
      }
      createFramebuffer(props) {
        return new WEBGLFramebuffer(this, props);
      }
      createVertexArray(props) {
        return new WEBGLVertexArray(this, props);
      }
      createTransformFeedback(props) {
        return new WEBGLTransformFeedback(this, props);
      }
      createQuerySet(props) {
        return new WEBGLQuerySet(this, props);
      }
      createRenderPipeline(props) {
        return new WEBGLRenderPipeline(this, props);
      }
      createComputePipeline(props) {
        throw new Error("ComputePipeline not supported in WebGL");
      }
      createCommandEncoder(props = {}) {
        return new WEBGLCommandEncoder(this, props);
      }
      /**
       * Offscreen Canvas Support: Commit the frame
       * https://developer.mozilla.org/en-US/docs/Web/API/WebGL2RenderingContext/commit
       * Chrome's offscreen canvas does not require gl.commit
       */
      submit(commandBuffer) {
        if (!commandBuffer) {
          commandBuffer = this.commandEncoder.finish();
          this.commandEncoder.destroy();
          this.commandEncoder = this.createCommandEncoder({ id: `${this.id}-default-encoder` });
        }
        commandBuffer._executeCommands();
      }
      //
      // TEMPORARY HACKS - will be removed in v9.1
      //
      /** @deprecated - should use command encoder */
      readPixelsToArrayWebGL(source, options) {
        return readPixelsToArray(source, options);
      }
      /** @deprecated - should use command encoder */
      readPixelsToBufferWebGL(source, options) {
        return readPixelsToBuffer(source, options);
      }
      setParametersWebGL(parameters) {
        setGLParameters(this.gl, parameters);
      }
      getParametersWebGL(parameters) {
        return getGLParameters(this.gl, parameters);
      }
      withParametersWebGL(parameters, func) {
        return withGLParameters(this.gl, parameters, func);
      }
      resetWebGL() {
        import_core23.log.warn("WebGLDevice.resetWebGL is deprecated, use only for debugging")();
        resetGLParameters(this.gl);
      }
      _getDeviceSpecificTextureFormatCapabilities(capabilities) {
        return getTextureFormatCapabilitiesWebGL(this.gl, capabilities, this._extensions);
      }
      //
      // WebGL-only API (not part of `Device` API)
      //
      /**
       * Triggers device (or WebGL context) loss.
       * @note primarily intended for testing how application reacts to device loss
       */
      loseDevice() {
        var _a;
        let deviceLossTriggered = false;
        const extensions = this.getExtension("WEBGL_lose_context");
        const ext = extensions.WEBGL_lose_context;
        if (ext) {
          deviceLossTriggered = true;
          ext.loseContext();
        }
        (_a = this._resolveContextLost) == null ? void 0 : _a.call(this, {
          reason: "destroyed",
          message: "Application triggered context loss"
        });
        return deviceLossTriggered;
      }
      /** Save current WebGL context state onto an internal stack */
      pushState() {
        const webglState = WebGLStateTracker.get(this.gl);
        webglState.push();
      }
      /** Restores previously saved context state */
      popState() {
        const webglState = WebGLStateTracker.get(this.gl);
        webglState.pop();
      }
      /**
       * Returns the GL.<KEY> constant that corresponds to a numeric value of a GL constant
       * Be aware that there are some duplicates especially for constants that are 0,
       * so this isn't guaranteed to return the right key in all cases.
       */
      getGLKey(value, options) {
        const number = Number(value);
        for (const key in this.gl) {
          if (this.gl[key] === number) {
            return `GL.${key}`;
          }
        }
        return (options == null ? void 0 : options.emptyIfUnknown) ? "" : String(value);
      }
      /**
       * Returns a map with any GL.<KEY> constants mapped to strings, both for keys and values
       */
      getGLKeys(glParameters) {
        const opts = { emptyIfUnknown: true };
        return Object.entries(glParameters).reduce((keys, [key, value]) => {
          keys[`${key}:${this.getGLKey(key, opts)}`] = `${value}:${this.getGLKey(value, opts)}`;
          return keys;
        }, {});
      }
      /**
       * Set a constant value for a location. Disabled attributes at that location will read from this value
       * @note WebGL constants are stored globally on the WebGL context, not the VertexArray
       * so they need to be updated before every render
       * @todo - remember/cache values to avoid setting them unnecessarily?
       */
      setConstantAttributeWebGL(location, constant) {
        const maxVertexAttributes = this.limits.maxVertexAttributes;
        this._constants = this._constants || new Array(maxVertexAttributes).fill(null);
        const currentConstant = this._constants[location];
        if (currentConstant && compareConstantArrayValues2(currentConstant, constant)) {
          import_core23.log.info(1, `setConstantAttributeWebGL(${location}) could have been skipped, value unchanged`)();
        }
        this._constants[location] = constant;
        switch (constant.constructor) {
          case Float32Array:
            setConstantFloatArray(this, location, constant);
            break;
          case Int32Array:
            setConstantIntArray(this, location, constant);
            break;
          case Uint32Array:
            setConstantUintArray(this, location, constant);
            break;
          default:
            throw new Error("constant");
        }
      }
      /** Ensure extensions are only requested once */
      getExtension(name) {
        getWebGLExtension(this.gl, name, this._extensions);
        return this._extensions;
      }
      // INTERNAL SUPPORT METHODS FOR WEBGL RESOURCES
      /**
       * Storing data on a special field on WebGLObjects makes that data visible in SPECTOR chrome debug extension
       * luma.gl ids and props can be inspected
       */
      _setWebGLDebugMetadata(handle, resource, options) {
        handle.luma = resource;
        const spectorMetadata = { props: options.spector, id: options.spector["id"] };
        handle.__SPECTOR_Metadata = spectorMetadata;
      }
    };
  }
});

// dist/adapter/webgl-adapter.js
function isWebGL(gl) {
  if (typeof WebGL2RenderingContext !== "undefined" && gl instanceof WebGL2RenderingContext) {
    return true;
  }
  return Boolean(gl && Number.isFinite(gl._version));
}
var import_core24, LOG_LEVEL2, WebGLAdapter, webgl2Adapter;
var init_webgl_adapter = __esm({
  "dist/adapter/webgl-adapter.js"() {
    "use strict";
    import_core24 = require("@luma.gl/core");
    init_polyfill_webgl1_extensions();
    init_spector();
    init_webgl_developer_tools();
    LOG_LEVEL2 = 1;
    WebGLAdapter = class extends import_core24.Adapter {
      /** type of device's created by this adapter */
      type = "webgl";
      constructor() {
        super();
        import_core24.Device.defaultProps = { ...import_core24.Device.defaultProps, ...DEFAULT_SPECTOR_PROPS };
      }
      /** Force any created WebGL contexts to be WebGL2 contexts, polyfilled with WebGL1 extensions */
      enforceWebGL2(enable2) {
        enforceWebGL2(enable2);
      }
      /** Check if WebGL 2 is available */
      isSupported() {
        return typeof WebGL2RenderingContext !== "undefined";
      }
      isDeviceHandle(handle) {
        if (typeof WebGL2RenderingContext !== "undefined" && handle instanceof WebGL2RenderingContext) {
          return true;
        }
        if (typeof WebGLRenderingContext !== "undefined" && handle instanceof WebGLRenderingContext) {
          import_core24.log.warn("WebGL1 is not supported", handle)();
        }
        return false;
      }
      /**
       * Get a device instance from a GL context
       * Creates a WebGLCanvasContext against the contexts canvas
       * @note autoResize will be disabled, assuming that whoever created the external context will be handling resizes.
       * @param gl
       * @returns
       */
      async attach(gl, props = {}) {
        const { WebGLDevice: WebGLDevice2 } = await Promise.resolve().then(() => (init_webgl_device(), webgl_device_exports));
        if (gl instanceof WebGLDevice2) {
          return gl;
        }
        if ((gl == null ? void 0 : gl.device) instanceof WebGLDevice2) {
          return gl.device;
        }
        if (!isWebGL(gl)) {
          throw new Error("Invalid WebGL2RenderingContext");
        }
        const createCanvasContext = props.createCanvasContext === true ? {} : props.createCanvasContext;
        return new WebGLDevice2({
          ...props,
          _handle: gl,
          createCanvasContext: { canvas: gl.canvas, autoResize: false, ...createCanvasContext }
        });
      }
      async create(props = {}) {
        const { WebGLDevice: WebGLDevice2 } = await Promise.resolve().then(() => (init_webgl_device(), webgl_device_exports));
        import_core24.log.groupCollapsed(LOG_LEVEL2, "WebGLDevice created")();
        try {
          const promises = [];
          if (props.debugWebGL || props.debug) {
            promises.push(loadWebGLDeveloperTools());
          }
          if (props.debugSpectorJS) {
            promises.push(loadSpectorJS(props));
          }
          const results = await Promise.allSettled(promises);
          for (const result of results) {
            if (result.status === "rejected") {
              import_core24.log.error(`Failed to initialize debug libraries ${result.reason}`)();
            }
          }
          const device = new WebGLDevice2(props);
          const message2 = `${device._reused ? "Reusing" : "Created"} device with WebGL2 ${device.props.debug ? "debug " : ""}context: ${device.info.vendor}, ${device.info.renderer} for canvas: ${device.canvasContext.id}`;
          import_core24.log.probe(LOG_LEVEL2, message2)();
          import_core24.log.table(LOG_LEVEL2, device.info)();
          return device;
        } finally {
          import_core24.log.groupEnd(LOG_LEVEL2)();
        }
      }
    };
    webgl2Adapter = new WebGLAdapter();
  }
});

// dist/index.js
var dist_exports = {};
__export(dist_exports, {
  WEBGLBuffer: () => WEBGLBuffer,
  WEBGLCommandEncoder: () => WEBGLCommandEncoder,
  WEBGLFramebuffer: () => WEBGLFramebuffer,
  WEBGLRenderPass: () => WEBGLRenderPass,
  WEBGLRenderPipeline: () => WEBGLRenderPipeline,
  WEBGLSampler: () => WEBGLSampler,
  WEBGLShader: () => WEBGLShader,
  WEBGLTexture: () => WEBGLTexture,
  WEBGLTransformFeedback: () => WEBGLTransformFeedback,
  WEBGLVertexArray: () => WEBGLVertexArray,
  WebGLCanvasContext: () => WebGLCanvasContext,
  WebGLDevice: () => WebGLDevice,
  WebGLStateTracker: () => WebGLStateTracker,
  getGLParameters: () => getGLParameters,
  getShaderLayoutFromGLSL: () => getShaderLayoutFromGLSL,
  resetGLParameters: () => resetGLParameters,
  setDeviceParameters: () => setDeviceParameters,
  setGLParameters: () => setGLParameters,
  webgl2Adapter: () => webgl2Adapter,
  withDeviceParameters: () => withDeviceParameters,
  withGLParameters: () => withGLParameters
});
module.exports = __toCommonJS(dist_exports);
var init_dist = __esm({
  "dist/index.js"() {
    init_webgl_adapter();
    init_webgl_device();
    init_webgl_canvas_context();
    init_webgl_buffer();
    init_webgl_texture();
    init_webgl_shader();
    init_webgl_sampler();
    init_webgl_framebuffer();
    init_webgl_render_pipeline();
    init_webgl_command_encoder();
    init_webgl_render_pass();
    init_webgl_vertex_array();
    init_webgl_transform_feedback();
    init_device_parameters();
    init_get_shader_layout_from_glsl();
    init_webgl_state_tracker();
    init_unified_parameter_api();
    init_with_parameters();
  }
});
init_dist();
//# sourceMappingURL=index.cjs.map
