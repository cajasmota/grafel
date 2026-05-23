"use strict";
var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __defNormalProp = (obj, key, value) => key in obj ? __defProp(obj, key, { enumerable: true, configurable: true, writable: true, value }) : obj[key] = value;
var __export = (target, all) => {
  for (var name2 in all)
    __defProp(target, name2, { get: all[name2], enumerable: true });
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
  Adapter: () => Adapter,
  Buffer: () => Buffer2,
  CanvasContext: () => CanvasContext,
  CommandBuffer: () => CommandBuffer,
  CommandEncoder: () => CommandEncoder,
  ComputePass: () => ComputePass,
  ComputePipeline: () => ComputePipeline,
  Device: () => Device,
  DeviceFeatures: () => DeviceFeatures,
  DeviceLimits: () => DeviceLimits,
  ExternalTexture: () => ExternalTexture,
  Framebuffer: () => Framebuffer,
  PipelineLayout: () => PipelineLayout,
  QuerySet: () => QuerySet,
  RenderPass: () => RenderPass,
  RenderPipeline: () => RenderPipeline,
  Resource: () => Resource,
  Sampler: () => Sampler,
  Shader: () => Shader,
  Texture: () => Texture,
  TextureFormatDecoder: () => TextureFormatDecoder,
  TextureView: () => TextureView,
  TransformFeedback: () => TransformFeedback,
  UniformBlock: () => UniformBlock,
  UniformBufferLayout: () => UniformBufferLayout,
  UniformStore: () => UniformStore,
  VertexArray: () => VertexArray,
  _getTextureFormatDefinition: () => getTextureFormatDefinition,
  _getTextureFormatTable: () => getTextureFormatTable,
  getAttributeInfosFromLayouts: () => getAttributeInfosFromLayouts,
  getAttributeShaderTypeInfo: () => getAttributeShaderTypeInfo,
  getDataType: () => getDataType,
  getDataTypeInfo: () => getDataTypeInfo,
  getNormalizedDataType: () => getNormalizedDataType,
  getScratchArray: () => getScratchArray,
  getTypedArrayConstructor: () => getTypedArrayConstructor,
  getVariableShaderTypeInfo: () => getVariableShaderTypeInfo,
  getVertexFormatFromAttribute: () => getVertexFormatFromAttribute,
  getVertexFormatInfo: () => getVertexFormatInfo,
  log: () => log,
  luma: () => luma,
  makeVertexFormat: () => makeVertexFormat,
  readPixel: () => readPixel,
  textureFormatDecoder: () => textureFormatDecoder,
  writePixel: () => writePixel
});
module.exports = __toCommonJS(dist_exports);

// dist/utils/stats-manager.js
var import_stats = require("@probe.gl/stats");
var StatsManager = class {
  stats = /* @__PURE__ */ new Map();
  getStats(name2) {
    return this.get(name2);
  }
  get(name2) {
    if (!this.stats.has(name2)) {
      this.stats.set(name2, new import_stats.Stats({ id: name2 }));
    }
    return this.stats.get(name2);
  }
};
var lumaStats = new StatsManager();

// dist/utils/log.js
var import_log = require("@probe.gl/log");
var log = new import_log.Log({ id: "luma.gl" });

// dist/utils/uid.js
var uidCounters = {};
function uid(id = "id") {
  uidCounters[id] = uidCounters[id] || 1;
  const count = uidCounters[id]++;
  return `${id}-${count}`;
}

// dist/adapter/resources/resource.js
var Resource = class {
  toString() {
    return `${this[Symbol.toStringTag] || this.constructor.name}:"${this.id}"`;
  }
  /** props.id, for debugging. */
  id;
  props;
  userData = {};
  _device;
  /** Whether this resource has been destroyed */
  destroyed = false;
  /** For resources that allocate GPU memory */
  allocatedBytes = 0;
  /** Attached resources will be destroyed when this resource is destroyed. Tracks auto-created "sub" resources. */
  _attachedResources = /* @__PURE__ */ new Set();
  /**
   * Create a new Resource. Called from Subclass
   */
  constructor(device, props, defaultProps) {
    if (!device) {
      throw new Error("no device");
    }
    this._device = device;
    this.props = selectivelyMerge(props, defaultProps);
    const id = this.props.id !== "undefined" ? this.props.id : uid(this[Symbol.toStringTag]);
    this.props.id = id;
    this.id = id;
    this.userData = this.props.userData || {};
    this.addStats();
  }
  /**
   * destroy can be called on any resource to release it before it is garbage collected.
   */
  destroy() {
    this.destroyResource();
  }
  /** @deprecated Use destroy() */
  delete() {
    this.destroy();
    return this;
  }
  /**
   * Combines a map of user props and default props, only including props from defaultProps
   * @returns returns a map of overridden default props
   */
  getProps() {
    return this.props;
  }
  // ATTACHED RESOURCES
  /**
   * Attaches a resource. Attached resources are auto destroyed when this resource is destroyed
   * Called automatically when sub resources are auto created but can be called by application
   */
  attachResource(resource) {
    this._attachedResources.add(resource);
  }
  /**
   * Detach an attached resource. The resource will no longer be auto-destroyed when this resource is destroyed.
   */
  detachResource(resource) {
    this._attachedResources.delete(resource);
  }
  /**
   * Destroys a resource (only if owned), and removes from the owned (auto-destroy) list for this resource.
   */
  destroyAttachedResource(resource) {
    if (this._attachedResources.delete(resource)) {
      resource.destroy();
    }
  }
  /** Destroy all owned resources. Make sure the resources are no longer needed before calling. */
  destroyAttachedResources() {
    for (const resource of Object.values(this._attachedResources)) {
      resource.destroy();
    }
    this._attachedResources = /* @__PURE__ */ new Set();
  }
  // PROTECTED METHODS
  /** Perform all destroy steps. Can be called by derived resources when overriding destroy() */
  destroyResource() {
    this.destroyAttachedResources();
    this.removeStats();
    this.destroyed = true;
  }
  /** Called by .destroy() to track object destruction. Subclass must call if overriding destroy() */
  removeStats() {
    const stats = this._device.statsManager.getStats("Resource Counts");
    const name2 = this[Symbol.toStringTag];
    stats.get(`${name2}s Active`).decrementCount();
  }
  /** Called by subclass to track memory allocations */
  trackAllocatedMemory(bytes, name2 = this[Symbol.toStringTag]) {
    const stats = this._device.statsManager.getStats("Resource Counts");
    stats.get("GPU Memory").addCount(bytes);
    stats.get(`${name2} Memory`).addCount(bytes);
    this.allocatedBytes = bytes;
  }
  /** Called by subclass to track memory deallocations */
  trackDeallocatedMemory(name2 = this[Symbol.toStringTag]) {
    const stats = this._device.statsManager.getStats("Resource Counts");
    stats.get("GPU Memory").subtractCount(this.allocatedBytes);
    stats.get(`${name2} Memory`).subtractCount(this.allocatedBytes);
    this.allocatedBytes = 0;
  }
  /** Called by resource constructor to track object creation */
  addStats() {
    const stats = this._device.statsManager.getStats("Resource Counts");
    const name2 = this[Symbol.toStringTag];
    stats.get("Resources Created").incrementCount();
    stats.get(`${name2}s Created`).incrementCount();
    stats.get(`${name2}s Active`).incrementCount();
  }
};
/** Default properties for resource */
__publicField(Resource, "defaultProps", {
  id: "undefined",
  handle: void 0,
  userData: void 0
});
function selectivelyMerge(props, defaultProps) {
  const mergedProps = { ...defaultProps };
  for (const key in props) {
    if (props[key] !== void 0) {
      mergedProps[key] = props[key];
    }
  }
  return mergedProps;
}

// dist/adapter/resources/buffer.js
var _Buffer = class extends Resource {
  get [Symbol.toStringTag]() {
    return "Buffer";
  }
  /** The usage with which this buffer was created */
  usage;
  /** For index buffers, whether indices are 8, 16 or 32 bit. Note: uint8 indices are automatically converted to uint16 for WebGPU compatibility */
  indexType;
  /** "Time" of last update, can be used to check if redraw is needed */
  updateTimestamp;
  constructor(device, props) {
    const deducedProps = { ...props };
    if ((props.usage || 0) & _Buffer.INDEX && !props.indexType) {
      if (props.data instanceof Uint32Array) {
        deducedProps.indexType = "uint32";
      } else if (props.data instanceof Uint16Array) {
        deducedProps.indexType = "uint16";
      } else if (props.data instanceof Uint8Array) {
        deducedProps.indexType = "uint8";
      }
    }
    delete deducedProps.data;
    super(device, deducedProps, _Buffer.defaultProps);
    this.usage = deducedProps.usage || 0;
    this.indexType = deducedProps.indexType;
    this.updateTimestamp = device.incrementTimestamp();
  }
  /**
   * Create a copy of this Buffer with new byteLength, with same props but of the specified size.
   * @note Does not copy contents of the cloned Buffer.
   */
  clone(props) {
    return this.device.createBuffer({ ...this.props, ...props });
  }
  /** A partial CPU-side copy of the data in this buffer, for debugging purposes */
  debugData = new ArrayBuffer(0);
  /** This doesn't handle partial non-zero offset updates correctly */
  _setDebugData(data, byteOffset, byteLength) {
    const arrayBuffer2 = ArrayBuffer.isView(data) ? data.buffer : data;
    const debugDataLength = Math.min(data ? data.byteLength : byteLength, _Buffer.DEBUG_DATA_MAX_LENGTH);
    if (arrayBuffer2 === null) {
      this.debugData = new ArrayBuffer(debugDataLength);
    } else if (byteOffset === 0 && byteLength === arrayBuffer2.byteLength) {
      this.debugData = arrayBuffer2.slice(0, debugDataLength);
    } else {
      this.debugData = arrayBuffer2.slice(byteOffset, byteOffset + debugDataLength);
    }
  }
};
var Buffer2 = _Buffer;
/** Index buffer */
__publicField(Buffer2, "INDEX", 16);
/** Vertex buffer */
__publicField(Buffer2, "VERTEX", 32);
/** Uniform buffer */
__publicField(Buffer2, "UNIFORM", 64);
/** Storage buffer */
__publicField(Buffer2, "STORAGE", 128);
__publicField(Buffer2, "INDIRECT", 256);
__publicField(Buffer2, "QUERY_RESOLVE", 512);
// Usage Flags
__publicField(Buffer2, "MAP_READ", 1);
__publicField(Buffer2, "MAP_WRITE", 2);
__publicField(Buffer2, "COPY_SRC", 4);
__publicField(Buffer2, "COPY_DST", 8);
// PROTECTED METHODS (INTENDED FOR USE BY OTHER FRAMEWORK CODE ONLY)
/** Max amount of debug data saved. Two vec4's */
__publicField(Buffer2, "DEBUG_DATA_MAX_LENGTH", 32);
__publicField(Buffer2, "defaultProps", {
  ...Resource.defaultProps,
  usage: 0,
  // Buffer.COPY_DST | Buffer.COPY_SRC
  byteLength: 0,
  byteOffset: 0,
  data: null,
  indexType: "uint16",
  onMapped: void 0
});

// dist/shadertypes/data-types/decode-data-types.js
function getDataTypeInfo(type) {
  const [signedType, primitiveType, byteLength] = NORMALIZED_TYPE_MAP[type];
  const normalized = type.includes("norm");
  const integer = !normalized && !type.startsWith("float");
  const signed = type.startsWith("s");
  return {
    signedType,
    primitiveType,
    byteLength,
    normalized,
    integer,
    signed
  };
}
function getNormalizedDataType(signedDataType) {
  const dataType = signedDataType;
  switch (dataType) {
    case "uint8":
      return "unorm8";
    case "sint8":
      return "snorm8";
    case "uint16":
      return "unorm16";
    case "sint16":
      return "snorm16";
    default:
      return dataType;
  }
}
function alignTo(size, count) {
  switch (count) {
    case 1:
      return size;
    case 2:
      return size + size % 2;
    default:
      return size + (4 - size % 4) % 4;
  }
}
function getDataType(arrayOrType) {
  const Constructor = ArrayBuffer.isView(arrayOrType) ? arrayOrType.constructor : arrayOrType;
  if (Constructor === Uint8ClampedArray) {
    return "uint8";
  }
  const info = Object.values(NORMALIZED_TYPE_MAP).find((entry) => Constructor === entry[4]);
  if (!info) {
    throw new Error(Constructor.name);
  }
  return info[0];
}
function getTypedArrayConstructor(type) {
  const [, , , , Constructor] = NORMALIZED_TYPE_MAP[type];
  return Constructor;
}
var NORMALIZED_TYPE_MAP = {
  uint8: ["uint8", "u32", 1, false, Uint8Array],
  sint8: ["sint8", "i32", 1, false, Int8Array],
  unorm8: ["uint8", "f32", 1, true, Uint8Array],
  snorm8: ["sint8", "f32", 1, true, Int8Array],
  uint16: ["uint16", "u32", 2, false, Uint16Array],
  sint16: ["sint16", "i32", 2, false, Int16Array],
  unorm16: ["uint16", "u32", 2, true, Uint16Array],
  snorm16: ["sint16", "i32", 2, true, Int16Array],
  float16: ["float16", "f16", 2, false, Uint16Array],
  float32: ["float32", "f32", 4, false, Float32Array],
  uint32: ["uint32", "u32", 4, false, Uint32Array],
  sint32: ["sint32", "i32", 4, false, Int32Array]
};

// dist/shadertypes/vertex-arrays/decode-vertex-format.js
function getVertexFormatInfo(format) {
  let webglOnly;
  if (format.endsWith("-webgl")) {
    format.replace("-webgl", "");
    webglOnly = true;
  }
  const [type_, count] = format.split("x");
  const type = type_;
  const components = count ? parseInt(count) : 1;
  const decodedType = getDataTypeInfo(type);
  const result = {
    type,
    components,
    byteLength: decodedType.byteLength * components,
    integer: decodedType.integer,
    signed: decodedType.signed,
    normalized: decodedType.normalized
  };
  if (webglOnly) {
    result.webglOnly = true;
  }
  return result;
}
function makeVertexFormat(signedDataType, components, normalized) {
  const dataType = normalized ? getNormalizedDataType(signedDataType) : signedDataType;
  switch (dataType) {
    case "unorm8":
      if (components === 1) {
        return "unorm8";
      }
      if (components === 3) {
        return "unorm8x3-webgl";
      }
      return `${dataType}x${components}`;
    case "snorm8":
    case "uint8":
    case "sint8":
    case "uint16":
    case "sint16":
    case "unorm16":
    case "snorm16":
    case "float16":
      if (components === 1 || components === 3) {
        throw new Error(`size: ${components}`);
      }
      return `${dataType}x${components}`;
    default:
      return components === 1 ? dataType : `${dataType}x${components}`;
  }
}
function getVertexFormatFromAttribute(typedArray, size, normalized) {
  if (!size || size > 4) {
    throw new Error(`size ${size}`);
  }
  const components = size;
  const signedDataType = getDataType(typedArray);
  return makeVertexFormat(signedDataType, components, normalized);
}
function getCompatibleVertexFormat(opts) {
  let vertexType;
  switch (opts.primitiveType) {
    case "f32":
      vertexType = "float32";
      break;
    case "i32":
      vertexType = "sint32";
      break;
    case "u32":
      vertexType = "uint32";
      break;
    case "f16":
      return opts.components <= 2 ? "float16x2" : "float16x4";
  }
  if (opts.components === 1) {
    return vertexType;
  }
  return `${vertexType}x${opts.components}`;
}

// dist/shadertypes/textures/texture-format-table.js
var texture_compression_bc = "texture-compression-bc";
var texture_compression_astc = "texture-compression-astc";
var texture_compression_etc2 = "texture-compression-etc2";
var texture_compression_etc1_webgl = "texture-compression-etc1-webgl";
var texture_compression_pvrtc_webgl = "texture-compression-pvrtc-webgl";
var texture_compression_atc_webgl = "texture-compression-atc-webgl";
var float32_renderable = "float32-renderable-webgl";
var float16_renderable = "float16-renderable-webgl";
var rgb9e5ufloat_renderable = "rgb9e5ufloat-renderable-webgl";
var snorm8_renderable = "snorm8-renderable-webgl";
var norm16_renderable = "norm16-renderable-webgl";
var snorm16_renderable = "snorm16-renderable-webgl";
var float32_filterable = "float32-filterable";
var float16_filterable = "float16-filterable-webgl";
function getTextureFormatDefinition(format) {
  const info = TEXTURE_FORMAT_TABLE[format];
  if (!info) {
    throw new Error(`Unsupported texture format ${format}`);
  }
  return info;
}
function getTextureFormatTable() {
  return TEXTURE_FORMAT_TABLE;
}
var TEXTURE_FORMAT_COLOR_DEPTH_TABLE = {
  // 8-bit formats
  "r8unorm": {},
  "rg8unorm": {},
  "rgb8unorm-webgl": {},
  "rgba8unorm": {},
  "rgba8unorm-srgb": {},
  "r8snorm": { render: snorm8_renderable },
  "rg8snorm": { render: snorm8_renderable },
  "rgb8snorm-webgl": {},
  "rgba8snorm": { render: snorm8_renderable },
  "r8uint": {},
  "rg8uint": {},
  "rgba8uint": {},
  "r8sint": {},
  "rg8sint": {},
  "rgba8sint": {},
  "bgra8unorm": {},
  "bgra8unorm-srgb": {},
  "r16unorm": { f: norm16_renderable },
  "rg16unorm": { render: norm16_renderable },
  "rgb16unorm-webgl": { f: norm16_renderable },
  // rgb not renderable
  "rgba16unorm": { render: norm16_renderable },
  "r16snorm": { f: snorm16_renderable },
  "rg16snorm": { render: snorm16_renderable },
  "rgb16snorm-webgl": { f: norm16_renderable },
  // rgb not renderable
  "rgba16snorm": { render: snorm16_renderable },
  "r16uint": {},
  "rg16uint": {},
  "rgba16uint": {},
  "r16sint": {},
  "rg16sint": {},
  "rgba16sint": {},
  "r16float": { render: float16_renderable, filter: "float16-filterable-webgl" },
  "rg16float": { render: float16_renderable, filter: float16_filterable },
  "rgba16float": { render: float16_renderable, filter: float16_filterable },
  "r32uint": {},
  "rg32uint": {},
  "rgba32uint": {},
  "r32sint": {},
  "rg32sint": {},
  "rgba32sint": {},
  "r32float": { render: float32_renderable, filter: float32_filterable },
  "rg32float": { render: false, filter: float32_filterable },
  "rgb32float-webgl": { render: float32_renderable, filter: float32_filterable },
  "rgba32float": { render: float32_renderable, filter: float32_filterable },
  // Packed 16-bit formats
  "rgba4unorm-webgl": { channels: "rgba", bitsPerChannel: [4, 4, 4, 4], packed: true },
  "rgb565unorm-webgl": { channels: "rgb", bitsPerChannel: [5, 6, 5, 0], packed: true },
  "rgb5a1unorm-webgl": { channels: "rgba", bitsPerChannel: [5, 5, 5, 1], packed: true },
  // Packed 32 bit formats
  "rgb9e5ufloat": { channels: "rgb", packed: true, render: rgb9e5ufloat_renderable },
  // , filter: true},
  "rg11b10ufloat": { channels: "rgb", bitsPerChannel: [11, 11, 10, 0], packed: true, p: 1, render: float32_renderable },
  "rgb10a2unorm": { channels: "rgba", bitsPerChannel: [10, 10, 10, 2], packed: true, p: 1 },
  "rgb10a2uint": { channels: "rgba", bitsPerChannel: [10, 10, 10, 2], packed: true, p: 1 },
  // Depth/stencil Formats
  // Depth and stencil formats
  stencil8: { attachment: "stencil", bitsPerChannel: [8, 0, 0, 0], dataType: "uint8" },
  "depth16unorm": { attachment: "depth", bitsPerChannel: [16, 0, 0, 0], dataType: "uint16" },
  "depth24plus": { attachment: "depth", bitsPerChannel: [24, 0, 0, 0], dataType: "uint32" },
  "depth32float": { attachment: "depth", bitsPerChannel: [32, 0, 0, 0], dataType: "float32" },
  // The depth component of the "depth24plus" and "depth24plus-stencil8" formats may be implemented as either a 24-bit depth value or a "depth32float" value.
  "depth24plus-stencil8": { attachment: "depth-stencil", bitsPerChannel: [24, 8, 0, 0], packed: true },
  // "depth32float-stencil8" feature
  "depth32float-stencil8": { attachment: "depth-stencil", bitsPerChannel: [32, 8, 0, 0], packed: true }
};
var TEXTURE_FORMAT_COMPRESSED_TABLE = {
  // BC compressed formats: check device.features.has("texture-compression-bc");
  "bc1-rgb-unorm-webgl": { f: texture_compression_bc },
  "bc1-rgb-unorm-srgb-webgl": { f: texture_compression_bc },
  "bc1-rgba-unorm": { f: texture_compression_bc },
  "bc1-rgba-unorm-srgb": { f: texture_compression_bc },
  "bc2-rgba-unorm": { f: texture_compression_bc },
  "bc2-rgba-unorm-srgb": { f: texture_compression_bc },
  "bc3-rgba-unorm": { f: texture_compression_bc },
  "bc3-rgba-unorm-srgb": { f: texture_compression_bc },
  "bc4-r-unorm": { f: texture_compression_bc },
  "bc4-r-snorm": { f: texture_compression_bc },
  "bc5-rg-unorm": { f: texture_compression_bc },
  "bc5-rg-snorm": { f: texture_compression_bc },
  "bc6h-rgb-ufloat": { f: texture_compression_bc },
  "bc6h-rgb-float": { f: texture_compression_bc },
  "bc7-rgba-unorm": { f: texture_compression_bc },
  "bc7-rgba-unorm-srgb": { f: texture_compression_bc },
  // WEBGL_compressed_texture_etc: device.features.has("texture-compression-etc2")
  // Note: Supposedly guaranteed availability compressed formats in WebGL2, but through CPU decompression
  "etc2-rgb8unorm": { f: texture_compression_etc2 },
  "etc2-rgb8unorm-srgb": { f: texture_compression_etc2 },
  "etc2-rgb8a1unorm": { f: texture_compression_etc2 },
  "etc2-rgb8a1unorm-srgb": { f: texture_compression_etc2 },
  "etc2-rgba8unorm": { f: texture_compression_etc2 },
  "etc2-rgba8unorm-srgb": { f: texture_compression_etc2 },
  "eac-r11unorm": { f: texture_compression_etc2 },
  "eac-r11snorm": { f: texture_compression_etc2 },
  "eac-rg11unorm": { f: texture_compression_etc2 },
  "eac-rg11snorm": { f: texture_compression_etc2 },
  // X_ASTC compressed formats: device.features.has("texture-compression-astc")
  "astc-4x4-unorm": { f: texture_compression_astc },
  "astc-4x4-unorm-srgb": { f: texture_compression_astc },
  "astc-5x4-unorm": { f: texture_compression_astc },
  "astc-5x4-unorm-srgb": { f: texture_compression_astc },
  "astc-5x5-unorm": { f: texture_compression_astc },
  "astc-5x5-unorm-srgb": { f: texture_compression_astc },
  "astc-6x5-unorm": { f: texture_compression_astc },
  "astc-6x5-unorm-srgb": { f: texture_compression_astc },
  "astc-6x6-unorm": { f: texture_compression_astc },
  "astc-6x6-unorm-srgb": { f: texture_compression_astc },
  "astc-8x5-unorm": { f: texture_compression_astc },
  "astc-8x5-unorm-srgb": { f: texture_compression_astc },
  "astc-8x6-unorm": { f: texture_compression_astc },
  "astc-8x6-unorm-srgb": { f: texture_compression_astc },
  "astc-8x8-unorm": { f: texture_compression_astc },
  "astc-8x8-unorm-srgb": { f: texture_compression_astc },
  "astc-10x5-unorm": { f: texture_compression_astc },
  "astc-10x5-unorm-srgb": { f: texture_compression_astc },
  "astc-10x6-unorm": { f: texture_compression_astc },
  "astc-10x6-unorm-srgb": { f: texture_compression_astc },
  "astc-10x8-unorm": { f: texture_compression_astc },
  "astc-10x8-unorm-srgb": { f: texture_compression_astc },
  "astc-10x10-unorm": { f: texture_compression_astc },
  "astc-10x10-unorm-srgb": { f: texture_compression_astc },
  "astc-12x10-unorm": { f: texture_compression_astc },
  "astc-12x10-unorm-srgb": { f: texture_compression_astc },
  "astc-12x12-unorm": { f: texture_compression_astc },
  "astc-12x12-unorm-srgb": { f: texture_compression_astc },
  // WEBGL_compressed_texture_pvrtc
  "pvrtc-rgb4unorm-webgl": { f: texture_compression_pvrtc_webgl },
  "pvrtc-rgba4unorm-webgl": { f: texture_compression_pvrtc_webgl },
  "pvrtc-rbg2unorm-webgl": { f: texture_compression_pvrtc_webgl },
  "pvrtc-rgba2unorm-webgl": { f: texture_compression_pvrtc_webgl },
  // WEBGL_compressed_texture_etc1
  "etc1-rbg-unorm-webgl": { f: texture_compression_etc1_webgl },
  // WEBGL_compressed_texture_atc
  "atc-rgb-unorm-webgl": { f: texture_compression_atc_webgl },
  "atc-rgba-unorm-webgl": { f: texture_compression_atc_webgl },
  "atc-rgbai-unorm-webgl": { f: texture_compression_atc_webgl }
};
var TEXTURE_FORMAT_TABLE = {
  ...TEXTURE_FORMAT_COLOR_DEPTH_TABLE,
  ...TEXTURE_FORMAT_COMPRESSED_TABLE
};

// dist/shadertypes/textures/texture-format-decoder.js
var COMPRESSED_TEXTURE_FORMAT_PREFIXES = [
  "bc1",
  "bc2",
  "bc3",
  "bc4",
  "bc5",
  "bc6",
  "bc7",
  "etc1",
  "etc2",
  "eac",
  "atc",
  "astc",
  "pvrtc"
];
var RGB_FORMAT_REGEX = /^(r|rg|rgb|rgba|bgra)([0-9]*)([a-z]*)(-srgb)?(-webgl)?$/;
var TextureFormatDecoder = class {
  /** Returns information about a texture format, e.g. attatchment type, components, byte length and flags (integer, signed, normalized) */
  getInfo(format) {
    return getTextureFormatInfo(format);
  }
  /** Checks if a texture format is color */
  isColor(format) {
    return format.startsWith("rgba") || format.startsWith("bgra") || format.startsWith("rgb");
  }
  /** Checks if a texture format is depth or stencil */
  isDepthStencil(format) {
    return format.startsWith("depth") || format.startsWith("stencil");
  }
  /** Checks if a texture format is compressed */
  isCompressed(format) {
    return COMPRESSED_TEXTURE_FORMAT_PREFIXES.some((prefix) => format.startsWith(prefix));
  }
  /**
   * Returns the "static" capabilities of a texture format.
   * @note Needs to be checked against current device
   */
  getCapabilities(format) {
    const info = getTextureFormatDefinition(format);
    const formatCapabilities = {
      format,
      create: info.f ?? true,
      render: info.render ?? true,
      filter: info.filter ?? true,
      blend: info.blend ?? true,
      store: info.store ?? true
    };
    const formatInfo = getTextureFormatInfo(format);
    const isDepthStencil = format.startsWith("depth") || format.startsWith("stencil");
    const isSigned = formatInfo == null ? void 0 : formatInfo.signed;
    const isInteger = formatInfo == null ? void 0 : formatInfo.integer;
    const isWebGLSpecific = formatInfo == null ? void 0 : formatInfo.webgl;
    formatCapabilities.render &&= !isSigned;
    formatCapabilities.filter &&= !isDepthStencil && !isSigned && !isInteger && !isWebGLSpecific;
    return formatCapabilities;
  }
};
var textureFormatDecoder = new TextureFormatDecoder();
function getTextureFormatInfo(format) {
  let formatInfo = getTextureFormatInfoUsingTable(format);
  if (textureFormatDecoder.isCompressed(format)) {
    formatInfo.channels = "rgb";
    formatInfo.components = 3;
    formatInfo.bytesPerPixel = 1;
    formatInfo.srgb = false;
    formatInfo.compressed = true;
    const blockSize = getCompressedTextureBlockSize(format);
    if (blockSize) {
      formatInfo.blockWidth = blockSize.blockWidth;
      formatInfo.blockHeight = blockSize.blockHeight;
    }
  }
  const matches = RGB_FORMAT_REGEX.exec(format);
  if (matches) {
    const [, channels, length, type, srgb, suffix] = matches;
    const dataType = `${type}${length}`;
    const decodedType = getDataTypeInfo(dataType);
    const bits = decodedType.byteLength * 8;
    const components = channels.length;
    const bitsPerChannel = [
      bits,
      components >= 2 ? bits : 0,
      components >= 3 ? bits : 0,
      components >= 4 ? bits : 0
    ];
    formatInfo = {
      format,
      attachment: formatInfo.attachment,
      dataType: decodedType.signedType,
      components,
      channels,
      integer: decodedType.integer,
      signed: decodedType.signed,
      normalized: decodedType.normalized,
      bitsPerChannel,
      bytesPerPixel: decodedType.byteLength * channels.length,
      packed: formatInfo.packed,
      srgb: formatInfo.srgb
    };
    if (suffix === "-webgl") {
      formatInfo.webgl = true;
    }
    if (srgb === "-srgb") {
      formatInfo.srgb = true;
    }
  }
  if (format.endsWith("-webgl")) {
    formatInfo.webgl = true;
  }
  if (format.endsWith("-srgb")) {
    formatInfo.srgb = true;
  }
  return formatInfo;
}
function getTextureFormatInfoUsingTable(format) {
  var _a;
  const info = getTextureFormatDefinition(format);
  const bytesPerPixel = info.bytesPerPixel || 1;
  const bitsPerChannel = info.bitsPerChannel || [8, 8, 8, 8];
  delete info.bitsPerChannel;
  delete info.bytesPerPixel;
  delete info.f;
  delete info.render;
  delete info.filter;
  delete info.blend;
  delete info.store;
  const formatInfo = {
    ...info,
    format,
    attachment: info.attachment || "color",
    channels: info.channels || "r",
    components: info.components || ((_a = info.channels) == null ? void 0 : _a.length) || 1,
    bytesPerPixel,
    bitsPerChannel,
    dataType: info.dataType || "uint8",
    srgb: info.srgb ?? false,
    packed: info.packed ?? false,
    webgl: info.webgl ?? false,
    integer: info.integer ?? false,
    signed: info.signed ?? false,
    normalized: info.normalized ?? false,
    compressed: info.compressed ?? false
  };
  return formatInfo;
}
function getCompressedTextureBlockSize(format) {
  const REGEX = /.*-(\d+)x(\d+)-.*/;
  const matches = REGEX.exec(format);
  if (matches) {
    const [, blockWidth, blockHeight] = matches;
    return { blockWidth: Number(blockWidth), blockHeight: Number(blockHeight) };
  }
  return null;
}

// dist/image-utils/image-types.js
function isExternalImage(data) {
  return typeof ImageData !== "undefined" && data instanceof ImageData || typeof ImageBitmap !== "undefined" && data instanceof ImageBitmap || typeof HTMLImageElement !== "undefined" && data instanceof HTMLImageElement || typeof HTMLVideoElement !== "undefined" && data instanceof HTMLVideoElement || typeof VideoFrame !== "undefined" && data instanceof VideoFrame || typeof HTMLCanvasElement !== "undefined" && data instanceof HTMLCanvasElement || typeof OffscreenCanvas !== "undefined" && data instanceof OffscreenCanvas;
}
function getExternalImageSize(data) {
  if (typeof ImageData !== "undefined" && data instanceof ImageData || typeof ImageBitmap !== "undefined" && data instanceof ImageBitmap || typeof HTMLCanvasElement !== "undefined" && data instanceof HTMLCanvasElement || typeof OffscreenCanvas !== "undefined" && data instanceof OffscreenCanvas) {
    return { width: data.width, height: data.height };
  }
  if (typeof HTMLImageElement !== "undefined" && data instanceof HTMLImageElement) {
    return { width: data.naturalWidth, height: data.naturalHeight };
  }
  if (typeof HTMLVideoElement !== "undefined" && data instanceof HTMLVideoElement) {
    return { width: data.videoWidth, height: data.videoHeight };
  }
  if (typeof VideoFrame !== "undefined" && data instanceof VideoFrame) {
    return { width: data.displayWidth, height: data.displayHeight };
  }
  throw new Error("Unknown image type");
}

// dist/adapter/device.js
var DeviceLimits = class {
};
var DeviceFeatures = class {
  features;
  disabledFeatures;
  constructor(features = [], disabledFeatures) {
    this.features = new Set(features);
    this.disabledFeatures = disabledFeatures || {};
  }
  *[Symbol.iterator]() {
    yield* this.features;
  }
  has(feature) {
    var _a;
    return !((_a = this.disabledFeatures) == null ? void 0 : _a[feature]) && this.features.has(feature);
  }
};
var _Device = class {
  get [Symbol.toStringTag]() {
    return "Device";
  }
  toString() {
    return `Device(${this.id})`;
  }
  /** id of this device, primarily for debugging */
  id;
  /** A copy of the device props  */
  props;
  /** Available for the application to store data on the device */
  userData = {};
  /** stats */
  statsManager = lumaStats;
  /** An abstract timestamp used for change tracking */
  timestamp = 0;
  /** True if this device has been reused during device creation (app has multiple references) */
  _reused = false;
  /** Used by other luma.gl modules to store data on the device */
  _lumaData = {};
  _textureCaps = {};
  constructor(props) {
    this.props = { ..._Device.defaultProps, ...props };
    this.id = this.props.id || uid(this[Symbol.toStringTag].toLowerCase());
  }
  getVertexFormatInfo(format) {
    return getVertexFormatInfo(format);
  }
  isVertexFormatSupported(format) {
    return true;
  }
  /** Returns information about a texture format, such as data type, channels, bits per channel, compression etc */
  getTextureFormatInfo(format) {
    return textureFormatDecoder.getInfo(format);
  }
  /** Determines what operations are supported on a texture format on this particular device (checks against supported device features) */
  getTextureFormatCapabilities(format) {
    let textureCaps = this._textureCaps[format];
    if (!textureCaps) {
      const capabilities = this._getDeviceTextureFormatCapabilities(format);
      textureCaps = this._getDeviceSpecificTextureFormatCapabilities(capabilities);
      this._textureCaps[format] = textureCaps;
    }
    return textureCaps;
  }
  /** Calculates the number of mip levels for a texture of width, height and in case of 3d textures only, depth */
  getMipLevelCount(width, height, depth3d = 1) {
    const maxSize = Math.max(width, height, depth3d);
    return 1 + Math.floor(Math.log2(maxSize));
  }
  /** Check if data is an external image */
  isExternalImage(data) {
    return isExternalImage(data);
  }
  /** Get the size of an external image */
  getExternalImageSize(data) {
    return getExternalImageSize(data);
  }
  /** Check if device supports a specific texture format (creation and `nearest` sampling) */
  isTextureFormatSupported(format) {
    return this.getTextureFormatCapabilities(format).create;
  }
  /** Check if linear filtering (sampler interpolation) is supported for a specific texture format */
  isTextureFormatFilterable(format) {
    return this.getTextureFormatCapabilities(format).filter;
  }
  /** Check if device supports rendering to a framebuffer color attachment of a specific texture format */
  isTextureFormatRenderable(format) {
    return this.getTextureFormatCapabilities(format).render;
  }
  /** Check if a specific texture format is GPU compressed */
  isTextureFormatCompressed(format) {
    return textureFormatDecoder.isCompressed(format);
  }
  // DEBUG METHODS
  pushDebugGroup(groupLabel) {
    this.commandEncoder.pushDebugGroup(groupLabel);
  }
  popDebugGroup() {
    var _a;
    (_a = this.commandEncoder) == null ? void 0 : _a.popDebugGroup();
  }
  insertDebugMarker(markerLabel) {
    var _a;
    (_a = this.commandEncoder) == null ? void 0 : _a.insertDebugMarker(markerLabel);
  }
  /**
   * Trigger device loss.
   * @returns `true` if context loss could actually be triggered.
   * @note primarily intended for testing how application reacts to device loss
   */
  loseDevice() {
    return false;
  }
  /** A monotonic counter for tracking buffer and texture updates */
  incrementTimestamp() {
    return this.timestamp++;
  }
  /**
   * Reports Device errors in a way that optimizes for developer experience / debugging.
   * - Logs so that the console error links directly to the source code that generated the error.
   * - Includes the object that reported the error in the log message, even if the error is asynchronous.
   *
   * Conventions when calling reportError():
   * - Always call the returned function - to ensure error is logged, at the error site
   * - Follow with a call to device.debug() - to ensure that the debugger breaks at the error site
   *
   * @param error - the error to report. If needed, just create a new Error object with the appropriate message.
   * @param context - pass `this` as context, otherwise it may not be available in the debugger for async errors.
   * @returns the logger function returned by device.props.onError() so that it can be called from the error site.
   *
   * @example
   *   device.reportError(new Error(...), this)();
   *   device.debug();
   */
  reportError(error, context, ...args) {
    const isHandled = this.props.onError(error, context);
    if (!isHandled) {
      return log.error(error.message, context, ...args);
    }
    return () => {
    };
  }
  /** Break in the debugger - if device.props.debug is true */
  debug() {
    if (this.props.debug) {
      debugger;
    } else {
      const message = `'Type luma.log.set({debug: true}) in console to enable debug breakpoints',
or create a device with the 'debug: true' prop.`;
      log.once(0, message)();
    }
  }
  /** Returns the default / primary canvas context. Throws an error if no canvas context is available (a WebGPU compute device) */
  getDefaultCanvasContext() {
    if (!this.canvasContext) {
      throw new Error("Device has no default CanvasContext. See props.createCanvasContext");
    }
    return this.canvasContext;
  }
  /** Create a RenderPass using the default CommandEncoder */
  beginRenderPass(props) {
    return this.commandEncoder.beginRenderPass(props);
  }
  /** Create a ComputePass using the default CommandEncoder*/
  beginComputePass(props) {
    return this.commandEncoder.beginComputePass(props);
  }
  // DEPRECATED METHODS
  /** @deprecated Use getDefaultCanvasContext() */
  getCanvasContext() {
    return this.getDefaultCanvasContext();
  }
  // WebGL specific HACKS - enables app to remove webgl import
  // Use until we have a better way to handle these
  /** @deprecated - will be removed - should use command encoder */
  readPixelsToArrayWebGL(source, options) {
    throw new Error("not implemented");
  }
  /** @deprecated - will be removed - should use command encoder */
  readPixelsToBufferWebGL(source, options) {
    throw new Error("not implemented");
  }
  /** @deprecated - will be removed - should use WebGPU parameters (pipeline) */
  setParametersWebGL(parameters) {
    throw new Error("not implemented");
  }
  /** @deprecated - will be removed - should use WebGPU parameters (pipeline) */
  getParametersWebGL(parameters) {
    throw new Error("not implemented");
  }
  /** @deprecated - will be removed - should use WebGPU parameters (pipeline) */
  withParametersWebGL(parameters, func) {
    throw new Error("not implemented");
  }
  /** @deprecated - will be removed - should use clear arguments in RenderPass */
  clearWebGL(options) {
    throw new Error("not implemented");
  }
  /** @deprecated - will be removed - should use for debugging only */
  resetWebGL() {
    throw new Error("not implemented");
  }
  // IMPLEMENTATION
  /** Helper to get the canvas context props */
  static _getCanvasContextProps(props) {
    return props.createCanvasContext === true ? {} : props.createCanvasContext;
  }
  _getDeviceTextureFormatCapabilities(format) {
    const genericCapabilities = textureFormatDecoder.getCapabilities(format);
    const checkFeature = (feature) => (typeof feature === "string" ? this.features.has(feature) : feature) ?? true;
    const supported = checkFeature(genericCapabilities.create);
    return {
      format,
      create: supported,
      render: supported && checkFeature(genericCapabilities.render),
      filter: supported && checkFeature(genericCapabilities.filter),
      blend: supported && checkFeature(genericCapabilities.blend),
      store: supported && checkFeature(genericCapabilities.store)
    };
  }
  /** Subclasses use this to support .createBuffer() overloads */
  _normalizeBufferProps(props) {
    if (props instanceof ArrayBuffer || ArrayBuffer.isView(props)) {
      props = { data: props };
    }
    const newProps = { ...props };
    const usage = props.usage || 0;
    if (usage & Buffer2.INDEX) {
      if (!props.indexType) {
        if (props.data instanceof Uint32Array) {
          newProps.indexType = "uint32";
        } else if (props.data instanceof Uint16Array) {
          newProps.indexType = "uint16";
        } else if (props.data instanceof Uint8Array) {
          newProps.data = new Uint16Array(props.data);
          newProps.indexType = "uint16";
        }
      }
      if (!newProps.indexType) {
        throw new Error("indices buffer content must be of type uint16 or uint32");
      }
    }
    return newProps;
  }
};
var Device = _Device;
__publicField(Device, "defaultProps", {
  id: null,
  powerPreference: "high-performance",
  failIfMajorPerformanceCaveat: false,
  createCanvasContext: void 0,
  // WebGL specific
  webgl: {},
  // Callbacks
  // eslint-disable-next-line handle-callback-err
  onError: (error, context) => {
  },
  onResize: (context, info) => {
    const [width, height] = context.getDevicePixelSize();
    log.log(1, `${context} resized => ${width}x${height}px`)();
  },
  onPositionChange: (context, info) => {
    const [left, top] = context.getPosition();
    log.log(1, `${context} repositioned => ${left},${top}`)();
  },
  onVisibilityChange: (context) => log.log(1, `${context} Visibility changed ${context.isVisible}`)(),
  onDevicePixelRatioChange: (context, info) => log.log(1, `${context} DPR changed ${info.oldRatio} => ${context.devicePixelRatio}`)(),
  // Debug flags
  debug: log.get("debug") || void 0,
  debugShaders: log.get("debug-shaders") || void 0,
  debugFramebuffers: Boolean(log.get("debug-framebuffers")),
  debugFactories: Boolean(log.get("debug-factories")),
  debugWebGL: Boolean(log.get("debug-webgl")),
  debugSpectorJS: void 0,
  // Note: log setting is queried by the spector.js code
  debugSpectorJSUrl: void 0,
  // Experimental
  _reuseDevices: false,
  _requestMaxLimits: true,
  _cacheShaders: false,
  _cachePipelines: false,
  _cacheDestroyPolicy: "unused",
  // TODO - Change these after confirming things work as expected
  _initializeFeatures: true,
  _disabledFeatures: {
    "compilation-status-async-webgl": true
  },
  // INTERNAL
  _handle: void 0
});

// dist/adapter/luma.js
var STARTUP_MESSAGE = "set luma.log.level=1 (or higher) to trace rendering";
var ERROR_MESSAGE = "No matching device found. Ensure `@luma.gl/webgl` and/or `@luma.gl/webgpu` modules are imported.";
var _Luma = class {
  /** Global stats for all devices */
  stats = lumaStats;
  /**
   * Global log
   *
   * Assign luma.log.level in console to control logging: \
   * 0: none, 1: minimal, 2: verbose, 3: attribute/uniforms, 4: gl logs
   * luma.log.break[], set to gl funcs, luma.log.profile[] set to model names`;
   */
  log = log;
  /** Version of luma.gl */
  VERSION = (
    // Version detection using build plugin
    // @ts-expect-error no-undef
    true ? "9.2.6" : "running from source"
  );
  spector;
  preregisteredAdapters = /* @__PURE__ */ new Map();
  constructor() {
    if (globalThis.luma) {
      if (globalThis.luma.VERSION !== this.VERSION) {
        log.error(`Found luma.gl ${globalThis.luma.VERSION} while initialzing ${this.VERSION}`)();
        log.error(`'yarn why @luma.gl/core' can help identify the source of the conflict`)();
        throw new Error(`luma.gl - multiple versions detected: see console log`);
      }
      log.error("This version of luma.gl has already been initialized")();
    }
    log.log(1, `${this.VERSION} - ${STARTUP_MESSAGE}`)();
    globalThis.luma = this;
  }
  /** Creates a device. Asynchronously. */
  async createDevice(props_ = {}) {
    const props = { ..._Luma.defaultProps, ...props_ };
    const adapter = this.selectAdapter(props.type, props.adapters);
    if (!adapter) {
      throw new Error(ERROR_MESSAGE);
    }
    if (props.waitForPageLoad) {
      await adapter.pageLoaded;
    }
    return await adapter.create(props);
  }
  /**
   * Attach to an existing GPU API handle (WebGL2RenderingContext or GPUDevice).
   * @param handle Externally created WebGL context or WebGPU device
   */
  async attachDevice(handle, props) {
    var _a;
    const type = this._getTypeFromHandle(handle, props.adapters);
    const adapter = type && this.selectAdapter(type, props.adapters);
    if (!adapter) {
      throw new Error(ERROR_MESSAGE);
    }
    return await ((_a = adapter == null ? void 0 : adapter.attach) == null ? void 0 : _a.call(adapter, handle, props));
  }
  /**
   * Global adapter registration.
   * @deprecated Use props.adapters instead
   */
  registerAdapters(adapters) {
    for (const deviceClass of adapters) {
      this.preregisteredAdapters.set(deviceClass.type, deviceClass);
    }
  }
  /** Get type strings for supported Devices */
  getSupportedAdapters(adapters = []) {
    const adapterMap = this._getAdapterMap(adapters);
    return Array.from(adapterMap).map(([, adapter]) => adapter).filter((adapter) => {
      var _a;
      return (_a = adapter.isSupported) == null ? void 0 : _a.call(adapter);
    }).map((adapter) => adapter.type);
  }
  /** Get type strings for best available Device */
  getBestAvailableAdapterType(adapters = []) {
    var _a, _b;
    const KNOWN_ADAPTERS = ["webgpu", "webgl", "null"];
    const adapterMap = this._getAdapterMap(adapters);
    for (const type of KNOWN_ADAPTERS) {
      if ((_b = (_a = adapterMap.get(type)) == null ? void 0 : _a.isSupported) == null ? void 0 : _b.call(_a)) {
        return type;
      }
    }
    return null;
  }
  /** Select adapter of type from registered adapters */
  selectAdapter(type, adapters = []) {
    let selectedType = type;
    if (type === "best-available") {
      selectedType = this.getBestAvailableAdapterType(adapters);
    }
    const adapterMap = this._getAdapterMap(adapters);
    return selectedType && adapterMap.get(selectedType) || null;
  }
  /**
   * Override `HTMLCanvasContext.getCanvas()` to always create WebGL2 contexts with additional WebGL1 compatibility.
   * Useful when attaching luma to a context from an external library does not support creating WebGL2 contexts.
   */
  enforceWebGL2(enforce = true, adapters = []) {
    var _a;
    const adapterMap = this._getAdapterMap(adapters);
    const webgl2Adapter = adapterMap.get("webgl");
    if (!webgl2Adapter) {
      log.warn("enforceWebGL2: webgl adapter not found")();
    }
    (_a = webgl2Adapter == null ? void 0 : webgl2Adapter.enforceWebGL2) == null ? void 0 : _a.call(webgl2Adapter, enforce);
  }
  // DEPRECATED
  /** @deprecated */
  setDefaultDeviceProps(props) {
    Object.assign(_Luma.defaultProps, props);
  }
  // HELPERS
  /** Convert a list of adapters to a map */
  _getAdapterMap(adapters = []) {
    const map = new Map(this.preregisteredAdapters);
    for (const adapter of adapters) {
      map.set(adapter.type, adapter);
    }
    return map;
  }
  /** Get type of a handle (for attachDevice) */
  _getTypeFromHandle(handle, adapters = []) {
    if (handle instanceof WebGL2RenderingContext) {
      return "webgl";
    }
    if (typeof GPUDevice !== "undefined" && handle instanceof GPUDevice) {
      return "webgpu";
    }
    if (handle == null ? void 0 : handle.queue) {
      return "webgpu";
    }
    if (handle === null) {
      return "null";
    }
    if (handle instanceof WebGLRenderingContext) {
      log.warn("WebGL1 is not supported", handle)();
    } else {
      log.warn("Unknown handle type", handle)();
    }
    return null;
  }
};
var Luma = _Luma;
__publicField(Luma, "defaultProps", {
  ...Device.defaultProps,
  type: "best-available",
  adapters: void 0,
  waitForPageLoad: true
});
var luma = new Luma();

// dist/adapter/adapter.js
var import_env = require("@probe.gl/env");
var Adapter = class {
  /**
   * Page load promise
   * Resolves when the DOM is loaded.
   * @note Since are be limitations on number of `load` event listeners,
   * it is recommended avoid calling this accessor until actually needed.
   * I.e. we don't call it unless you know that you will be looking up a string in the DOM.
   */
  get pageLoaded() {
    return getPageLoadPromise();
  }
};
var isPage = (0, import_env.isBrowser)() && typeof document !== "undefined";
var isPageLoaded = () => isPage && document.readyState === "complete";
var pageLoadPromise = null;
function getPageLoadPromise() {
  if (!pageLoadPromise) {
    if (isPageLoaded() || typeof window === "undefined") {
      pageLoadPromise = Promise.resolve();
    } else {
      pageLoadPromise = new Promise((resolve) => window.addEventListener("load", () => resolve()));
    }
  }
  return pageLoadPromise;
}

// dist/adapter/canvas-context.js
var import_env2 = require("@probe.gl/env");

// dist/utils/promise-utils.js
function withResolvers() {
  let resolve;
  let reject;
  const promise = new Promise((_resolve, _reject) => {
    resolve = _resolve;
    reject = _reject;
  });
  return { promise, resolve, reject };
}

// dist/adapter/canvas-context.js
var _CanvasContext = class {
  static isHTMLCanvas(canvas) {
    return typeof HTMLCanvasElement !== "undefined" && canvas instanceof HTMLCanvasElement;
  }
  static isOffscreenCanvas(canvas) {
    return typeof OffscreenCanvas !== "undefined" && canvas instanceof OffscreenCanvas;
  }
  id;
  props;
  canvas;
  /** Handle to HTML canvas */
  htmlCanvas;
  /** Handle to wrapped OffScreenCanvas */
  offscreenCanvas;
  type;
  /** Promise that resolved once the resize observer has updated the pixel size */
  initialized;
  isInitialized = false;
  /** Visibility is automatically updated (via an IntersectionObserver) */
  isVisible = true;
  /** Width of canvas in CSS units (tracked by a ResizeObserver) */
  cssWidth;
  /** Height of canvas in CSS units (tracked by a ResizeObserver) */
  cssHeight;
  /** Device pixel ratio. Automatically updated via media queries */
  devicePixelRatio;
  /** Exact width of canvas in physical pixels (tracked by a ResizeObserver) */
  devicePixelWidth;
  /** Exact height of canvas in physical pixels (tracked by a ResizeObserver) */
  devicePixelHeight;
  /** Width of drawing buffer: automatically tracks this.pixelWidth if props.autoResize is true */
  drawingBufferWidth;
  /** Height of drawing buffer: automatically tracks this.pixelHeight if props.autoResize is true */
  drawingBufferHeight;
  _initializedResolvers = withResolvers();
  _resizeObserver;
  _intersectionObserver;
  _position;
  destroyed = false;
  toString() {
    return `${this[Symbol.toStringTag]}(${this.id})`;
  }
  constructor(props) {
    var _a, _b;
    this.props = { ..._CanvasContext.defaultProps, ...props };
    props = this.props;
    this.initialized = this._initializedResolvers.promise;
    if (!(0, import_env2.isBrowser)()) {
      this.canvas = { width: props.width || 1, height: props.height || 1 };
    } else if (!props.canvas) {
      this.canvas = createCanvasElement(props);
    } else if (typeof props.canvas === "string") {
      this.canvas = getCanvasFromDOM(props.canvas);
    } else {
      this.canvas = props.canvas;
    }
    if (_CanvasContext.isHTMLCanvas(this.canvas)) {
      this.id = props.id || this.canvas.id;
      this.type = "html-canvas";
      this.htmlCanvas = this.canvas;
    } else if (_CanvasContext.isOffscreenCanvas(this.canvas)) {
      this.id = props.id || "offscreen-canvas";
      this.type = "offscreen-canvas";
      this.offscreenCanvas = this.canvas;
    } else {
      this.id = props.id || "node-canvas-context";
      this.type = "node";
    }
    this.cssWidth = ((_a = this.htmlCanvas) == null ? void 0 : _a.clientWidth) || this.canvas.width;
    this.cssHeight = ((_b = this.htmlCanvas) == null ? void 0 : _b.clientHeight) || this.canvas.height;
    this.devicePixelWidth = this.canvas.width;
    this.devicePixelHeight = this.canvas.height;
    this.drawingBufferWidth = this.canvas.width;
    this.drawingBufferHeight = this.canvas.height;
    this.devicePixelRatio = globalThis.devicePixelRatio || 1;
    this._position = [0, 0];
    if (_CanvasContext.isHTMLCanvas(this.canvas)) {
      this._intersectionObserver = new IntersectionObserver((entries) => this._handleIntersection(entries));
      this._intersectionObserver.observe(this.canvas);
      this._resizeObserver = new ResizeObserver((entries) => this._handleResize(entries));
      try {
        this._resizeObserver.observe(this.canvas, { box: "device-pixel-content-box" });
      } catch {
        this._resizeObserver.observe(this.canvas, { box: "content-box" });
      }
      setTimeout(() => this._observeDevicePixelRatio(), 0);
      if (this.props.trackPosition) {
        this._trackPosition();
      }
    }
  }
  destroy() {
    this.destroyed = true;
  }
  setProps(props) {
    if ("useDevicePixels" in props) {
      this.props.useDevicePixels = props.useDevicePixels || false;
      this._updateDrawingBufferSize();
    }
    return this;
  }
  // SIZE METHODS
  /**
   * Returns the size covered by the canvas in CSS pixels
   * @note This can be different from the actual device pixel size of a canvas due to DPR scaling, and rounding to integer pixels
   * @note This is independent of the canvas' internal drawing buffer size (.width, .height).
   */
  getCSSSize() {
    return [this.cssWidth, this.cssHeight];
  }
  getPosition() {
    return this._position;
  }
  /**
   * Returns the size covered by the canvas in actual device pixels.
   * @note This can be different from the 'CSS' size of a canvas due to DPR scaling, and rounding to integer pixels
   * @note This is independent of the canvas' internal drawing buffer size (.width, .height).
   */
  getDevicePixelSize() {
    return [this.devicePixelWidth, this.devicePixelHeight];
  }
  /** Get the drawing buffer size (number of pixels GPU is rendering into, can be different from CSS size) */
  getDrawingBufferSize() {
    return [this.drawingBufferWidth, this.drawingBufferHeight];
  }
  /** Returns the biggest allowed framebuffer size. @todo Allow the application to limit this? */
  getMaxDrawingBufferSize() {
    const maxTextureDimension = this.device.limits.maxTextureDimension2D;
    return [maxTextureDimension, maxTextureDimension];
  }
  /** Update the canvas drawing buffer size. Called automatically if props.autoResize is true. */
  setDrawingBufferSize(width, height) {
    this.canvas.width = width;
    this.canvas.height = height;
    this.drawingBufferWidth = width;
    this.drawingBufferHeight = height;
  }
  /**
   * Returns the current DPR (number of physical pixels per CSS pixel), if props.useDevicePixels is true
   * @note This can be a fractional (non-integer) number, e.g. when the user zooms in the browser.
   * @note This function handles the non-HTML canvas cases
   */
  getDevicePixelRatio() {
    const dpr = typeof window !== "undefined" && window.devicePixelRatio;
    return dpr || 1;
  }
  // DEPRECATED METHODS
  /**
   * Maps CSS pixel position to device pixel position
   */
  cssToDevicePixels(cssPixel, yInvert = true) {
    const ratio = this.cssToDeviceRatio();
    const [width, height] = this.getDrawingBufferSize();
    return scalePixels(cssPixel, ratio, width, height, yInvert);
  }
  /** @deprecated - use .getDevicePixelSize() */
  getPixelSize() {
    return this.getDevicePixelSize();
  }
  /** @deprecated - TODO which values should we use for aspect */
  getAspect() {
    const [width, height] = this.getDevicePixelSize();
    return width / height;
  }
  /** @deprecated Returns multiplier need to convert CSS size to Device size */
  cssToDeviceRatio() {
    try {
      const [drawingBufferWidth] = this.getDrawingBufferSize();
      const [cssWidth] = this.getCSSSize();
      return cssWidth ? drawingBufferWidth / cssWidth : 1;
    } catch {
      return 1;
    }
  }
  /** @deprecated Use canvasContext.setDrawingBufferSize() */
  resize(size) {
    this.setDrawingBufferSize(size.width, size.height);
  }
  // IMPLEMENTATION
  /**
   * Allows subclass constructor to override the canvas id for auto created canvases.
   * This can really help when debugging DOM in apps that create multiple devices
   */
  _setAutoCreatedCanvasId(id) {
    var _a;
    if (((_a = this.htmlCanvas) == null ? void 0 : _a.id) === "lumagl-auto-created-canvas") {
      this.htmlCanvas.id = id;
    }
  }
  /** reacts to an observed intersection */
  _handleIntersection(entries) {
    const entry = entries.find((entry_) => entry_.target === this.canvas);
    if (!entry) {
      return;
    }
    const isVisible = entry.isIntersecting;
    if (this.isVisible !== isVisible) {
      this.isVisible = isVisible;
      this.device.props.onVisibilityChange(this);
    }
  }
  /**
   * Reacts to an observed resize by using the most accurate pixel size information the browser can provide
   * @see https://web.dev/articles/device-pixel-content-box
   * @see https://webgpufundamentals.org/webgpu/lessons/webgpu-resizing-the-canvas.html
   */
  _handleResize(entries) {
    var _a, _b;
    const entry = entries.find((entry_) => entry_.target === this.canvas);
    if (!entry) {
      return;
    }
    this.cssWidth = entry.contentBoxSize[0].inlineSize;
    this.cssHeight = entry.contentBoxSize[0].blockSize;
    const oldPixelSize = this.getDevicePixelSize();
    const devicePixelWidth = ((_a = entry.devicePixelContentBoxSize) == null ? void 0 : _a[0].inlineSize) || entry.contentBoxSize[0].inlineSize * devicePixelRatio;
    const devicePixelHeight = ((_b = entry.devicePixelContentBoxSize) == null ? void 0 : _b[0].blockSize) || entry.contentBoxSize[0].blockSize * devicePixelRatio;
    const [maxDevicePixelWidth, maxDevicePixelHeight] = this.getMaxDrawingBufferSize();
    this.devicePixelWidth = Math.max(1, Math.min(devicePixelWidth, maxDevicePixelWidth));
    this.devicePixelHeight = Math.max(1, Math.min(devicePixelHeight, maxDevicePixelHeight));
    this._updateDrawingBufferSize();
    this.device.props.onResize(this, { oldPixelSize });
  }
  _updateDrawingBufferSize() {
    if (this.props.autoResize) {
      if (typeof this.props.useDevicePixels === "number") {
        const dpr = this.props.useDevicePixels;
        this.setDrawingBufferSize(this.cssWidth * dpr, this.cssHeight * dpr);
      } else if (this.props.useDevicePixels) {
        this.setDrawingBufferSize(this.devicePixelWidth, this.devicePixelHeight);
      } else {
        this.setDrawingBufferSize(this.cssWidth, this.cssHeight);
      }
      this._updateDevice();
    }
    this._initializedResolvers.resolve();
    this.isInitialized = true;
    this.updatePosition();
  }
  /** Monitor DPR changes */
  _observeDevicePixelRatio() {
    const oldRatio = this.devicePixelRatio;
    this.devicePixelRatio = window.devicePixelRatio;
    this.updatePosition();
    this.device.props.onDevicePixelRatioChange(this, { oldRatio });
    matchMedia(`(resolution: ${this.devicePixelRatio}dppx)`).addEventListener("change", () => this._observeDevicePixelRatio(), { once: true });
  }
  /** Start tracking positions with a timer */
  _trackPosition(intervalMs = 100) {
    const intervalId = setInterval(() => {
      if (this.destroyed) {
        clearInterval(intervalId);
      } else {
        this.updatePosition();
      }
    }, intervalMs);
  }
  /**
   * Calculated the absolute position of the canvas
   * @note - getBoundingClientRect() is normally cheap but can be expensive
   * if called before browser has finished a reflow. Should not be the case here.
   */
  updatePosition() {
    var _a, _b, _c;
    const newRect = (_a = this.htmlCanvas) == null ? void 0 : _a.getBoundingClientRect();
    if (newRect) {
      const position = [newRect.left, newRect.top];
      this._position ??= position;
      const positionChanged = position[0] !== this._position[0] || position[1] !== this._position[1];
      if (positionChanged) {
        const oldPosition = this._position;
        this._position = position;
        (_c = (_b = this.device.props).onPositionChange) == null ? void 0 : _c.call(_b, this, { oldPosition });
      }
    }
  }
};
var CanvasContext = _CanvasContext;
__publicField(CanvasContext, "defaultProps", {
  id: void 0,
  canvas: null,
  width: 800,
  height: 600,
  useDevicePixels: true,
  autoResize: true,
  container: null,
  visible: true,
  alphaMode: "opaque",
  colorSpace: "srgb",
  trackPosition: false
});
function getContainer(container) {
  if (typeof container === "string") {
    const element = document.getElementById(container);
    if (!element) {
      throw new Error(`${container} is not an HTML element`);
    }
    return element;
  }
  if (container) {
    return container;
  }
  return document.body;
}
function getCanvasFromDOM(canvasId) {
  const canvas = document.getElementById(canvasId);
  if (!CanvasContext.isHTMLCanvas(canvas)) {
    throw new Error("Object is not a canvas element");
  }
  return canvas;
}
function createCanvasElement(props) {
  const { width, height } = props;
  const newCanvas = document.createElement("canvas");
  newCanvas.id = uid("lumagl-auto-created-canvas");
  newCanvas.width = width || 1;
  newCanvas.height = height || 1;
  newCanvas.style.width = Number.isFinite(width) ? `${width}px` : "100%";
  newCanvas.style.height = Number.isFinite(height) ? `${height}px` : "100%";
  if (!(props == null ? void 0 : props.visible)) {
    newCanvas.style.visibility = "hidden";
  }
  const container = getContainer((props == null ? void 0 : props.container) || null);
  container.insertBefore(newCanvas, container.firstChild);
  return newCanvas;
}
function scalePixels(pixel, ratio, width, height, yInvert) {
  const point = pixel;
  const x = scaleX(point[0], ratio, width);
  let y = scaleY(point[1], ratio, height, yInvert);
  let t = scaleX(point[0] + 1, ratio, width);
  const xHigh = t === width - 1 ? t : t - 1;
  t = scaleY(point[1] + 1, ratio, height, yInvert);
  let yHigh;
  if (yInvert) {
    t = t === 0 ? t : t + 1;
    yHigh = y;
    y = t;
  } else {
    yHigh = t === height - 1 ? t : t - 1;
  }
  return {
    x,
    y,
    // when ratio < 1, current css pixel and next css pixel may point to same device pixel, set width/height to 1 in those cases.
    width: Math.max(xHigh - x + 1, 1),
    height: Math.max(yHigh - y + 1, 1)
  };
}
function scaleX(x, ratio, width) {
  const r = Math.min(Math.round(x * ratio), width - 1);
  return r;
}
function scaleY(y, ratio, height, yInvert) {
  return yInvert ? Math.max(0, height - 1 - Math.round(y * ratio)) : Math.min(Math.round(y * ratio), height - 1);
}

// dist/adapter/resources/sampler.js
var _Sampler = class extends Resource {
  get [Symbol.toStringTag]() {
    return "Sampler";
  }
  constructor(device, props) {
    props = _Sampler.normalizeProps(device, props);
    super(device, props, _Sampler.defaultProps);
  }
  static normalizeProps(device, props) {
    return props;
  }
};
var Sampler = _Sampler;
__publicField(Sampler, "defaultProps", {
  ...Resource.defaultProps,
  type: "color-sampler",
  addressModeU: "clamp-to-edge",
  addressModeV: "clamp-to-edge",
  addressModeW: "clamp-to-edge",
  magFilter: "nearest",
  minFilter: "nearest",
  mipmapFilter: "none",
  lodMinClamp: 0,
  lodMaxClamp: 32,
  // Per WebGPU spec
  compare: "less-equal",
  maxAnisotropy: 1
});

// dist/adapter/resources/texture.js
var BASE_DIMENSIONS = {
  "1d": "1d",
  "2d": "2d",
  "2d-array": "2d",
  cube: "2d",
  "cube-array": "2d",
  "3d": "3d"
};
var _Texture = class extends Resource {
  /** dimension of this texture */
  dimension;
  /** base dimension of this texture */
  baseDimension;
  /** format of this texture */
  format;
  /** width in pixels of this texture */
  width;
  /** height in pixels of this texture */
  height;
  /** depth of this texture */
  depth;
  /** mip levels in this texture */
  mipLevels;
  /** "Time" of last update. Monotonically increasing timestamp. TODO move to AsyncTexture? */
  updateTimestamp;
  get [Symbol.toStringTag]() {
    return "Texture";
  }
  toString() {
    return `Texture(${this.id},${this.format},${this.width}x${this.height})`;
  }
  /** Do not use directly. Create with device.createTexture() */
  constructor(device, props) {
    props = _Texture.normalizeProps(device, props);
    super(device, props, _Texture.defaultProps);
    this.dimension = this.props.dimension;
    this.baseDimension = BASE_DIMENSIONS[this.dimension];
    this.format = this.props.format;
    this.width = this.props.width;
    this.height = this.props.height;
    this.depth = this.props.depth;
    this.mipLevels = this.props.mipLevels;
    if (this.props.width === void 0 || this.props.height === void 0) {
      if (device.isExternalImage(props.data)) {
        const size = device.getExternalImageSize(props.data);
        this.width = (size == null ? void 0 : size.width) || 1;
        this.height = (size == null ? void 0 : size.height) || 1;
      } else {
        this.width = 1;
        this.height = 1;
        if (this.props.width === void 0 || this.props.height === void 0) {
          log.warn(`${this} created with undefined width or height. This is deprecated. Use AsyncTexture instead.`)();
        }
      }
    }
    this.updateTimestamp = device.incrementTimestamp();
  }
  /** Set sampler props associated with this texture */
  setSampler(sampler) {
    this.sampler = sampler instanceof Sampler ? sampler : this.device.createSampler(sampler);
  }
  /**
   * Create a new texture with the same parameters and optionally a different size
   * @note Textures are immutable and cannot be resized after creation, but we can create a similar texture with the same parameters but a new size.
   * @note Does not copy contents of the texture
   */
  clone(size) {
    return this.device.createTexture({ ...this.props, ...size });
  }
  /** Ensure we have integer coordinates */
  static normalizeProps(device, props) {
    const newProps = { ...props };
    const { width, height } = newProps;
    if (typeof width === "number") {
      newProps.width = Math.max(1, Math.ceil(width));
    }
    if (typeof height === "number") {
      newProps.height = Math.max(1, Math.ceil(height));
    }
    return newProps;
  }
  // HELPERS
  /** Initialize texture with supplied props */
  // eslint-disable-next-line max-statements
  _initializeData(data) {
    if (this.device.isExternalImage(data)) {
      this.copyExternalImage({
        image: data,
        width: this.width,
        height: this.height,
        depth: this.depth,
        mipLevel: 0,
        x: 0,
        y: 0,
        z: 0,
        aspect: "all",
        colorSpace: "srgb",
        premultipliedAlpha: false,
        flipY: false
      });
    } else if (data) {
      this.copyImageData({
        data,
        // width: this.width,
        // height: this.height,
        // depth: this.depth,
        mipLevel: 0,
        x: 0,
        y: 0,
        z: 0,
        aspect: "all"
      });
    }
  }
  _normalizeCopyImageDataOptions(options_) {
    const { width, height, depth } = this;
    const options = { ..._Texture.defaultCopyDataOptions, width, height, depth, ...options_ };
    const info = this.device.getTextureFormatInfo(this.format);
    if (!options_.bytesPerRow && !info.bytesPerPixel) {
      throw new Error(`bytesPerRow must be provided for texture format ${this.format}`);
    }
    options.bytesPerRow = options_.bytesPerRow || width * (info.bytesPerPixel || 4);
    options.rowsPerImage = options_.rowsPerImage || height;
    return options;
  }
  _normalizeCopyExternalImageOptions(options_) {
    const size = this.device.getExternalImageSize(options_.image);
    const options = { ..._Texture.defaultCopyExternalImageOptions, ...size, ...options_ };
    options.width = Math.min(options.width, this.width - options.x);
    options.height = Math.min(options.height, this.height - options.y);
    return options;
  }
};
var Texture = _Texture;
/** The texture can be bound for use as a sampled texture in a shader */
__publicField(Texture, "SAMPLE", 4);
/** The texture can be bound for use as a storage texture in a shader */
__publicField(Texture, "STORAGE", 8);
/** The texture can be used as a color or depth/stencil attachment in a render pass */
__publicField(Texture, "RENDER", 16);
/** The texture can be used as the source of a copy operation */
__publicField(Texture, "COPY_SRC", 1);
/** he texture can be used as the destination of a copy or write operation */
__publicField(Texture, "COPY_DST", 2);
/** @deprecated Use Texture.SAMPLE */
__publicField(Texture, "TEXTURE", 4);
/** @deprecated Use Texture.RENDER */
__publicField(Texture, "RENDER_ATTACHMENT", 16);
/** Default options */
__publicField(Texture, "defaultProps", {
  ...Resource.defaultProps,
  data: null,
  dimension: "2d",
  format: "rgba8unorm",
  usage: _Texture.TEXTURE | _Texture.RENDER_ATTACHMENT | _Texture.COPY_DST,
  width: void 0,
  height: void 0,
  depth: 1,
  mipLevels: 1,
  samples: void 0,
  sampler: {},
  view: void 0
});
__publicField(Texture, "defaultCopyDataOptions", {
  data: void 0,
  byteOffset: 0,
  bytesPerRow: void 0,
  rowsPerImage: void 0,
  mipLevel: 0,
  x: 0,
  y: 0,
  z: 0,
  aspect: "all"
});
/** Default options */
__publicField(Texture, "defaultCopyExternalImageOptions", {
  image: void 0,
  sourceX: 0,
  sourceY: 0,
  width: void 0,
  height: void 0,
  depth: 1,
  mipLevel: 0,
  x: 0,
  y: 0,
  z: 0,
  aspect: "all",
  colorSpace: "srgb",
  premultipliedAlpha: false,
  flipY: false
});

// dist/adapter/resources/texture-view.js
var _TextureView = class extends Resource {
  get [Symbol.toStringTag]() {
    return "TextureView";
  }
  /** Should not be constructed directly. Use `texture.createView(props)` */
  constructor(device, props) {
    super(device, props, _TextureView.defaultProps);
  }
};
var TextureView = _TextureView;
__publicField(TextureView, "defaultProps", {
  ...Resource.defaultProps,
  format: void 0,
  dimension: void 0,
  aspect: "all",
  baseMipLevel: 0,
  mipLevelCount: void 0,
  baseArrayLayer: 0,
  arrayLayerCount: void 0
});

// dist/adapter/resources/external-texture.js
var _ExternalTexture = class extends Resource {
  get [Symbol.toStringTag]() {
    return "ExternalTexture";
  }
  constructor(device, props) {
    super(device, props, _ExternalTexture.defaultProps);
  }
};
var ExternalTexture = _ExternalTexture;
__publicField(ExternalTexture, "defaultProps", {
  ...Resource.defaultProps,
  source: void 0,
  colorSpace: "srgb"
});

// dist/adapter-utils/format-compiler-log.js
function formatCompilerLog(shaderLog, source, options) {
  let formattedLog = "";
  const lines = source.split(/\r?\n/);
  const log2 = shaderLog.slice().sort((a, b) => a.lineNum - b.lineNum);
  switch ((options == null ? void 0 : options.showSourceCode) || "no") {
    case "all":
      let currentMessage = 0;
      for (let lineNum = 1; lineNum <= lines.length; lineNum++) {
        formattedLog += getNumberedLine(lines[lineNum - 1], lineNum, options);
        while (log2.length > currentMessage && log2[currentMessage].lineNum === lineNum) {
          const message = log2[currentMessage++];
          formattedLog += formatCompilerMessage(message, lines, message.lineNum, {
            ...options,
            inlineSource: false
          });
        }
      }
      while (log2.length > currentMessage) {
        const message = log2[currentMessage++];
        formattedLog += formatCompilerMessage(message, [], 0, {
          ...options,
          inlineSource: false
        });
      }
      return formattedLog;
    case "issues":
    case "no":
      for (const message of shaderLog) {
        formattedLog += formatCompilerMessage(message, lines, message.lineNum, {
          inlineSource: (options == null ? void 0 : options.showSourceCode) !== "no"
        });
      }
      return formattedLog;
  }
}
function formatCompilerMessage(message, lines, lineNum, options) {
  if (options == null ? void 0 : options.inlineSource) {
    const numberedLines = getNumberedLines(lines, lineNum);
    const positionIndicator = message.linePos > 0 ? `${" ".repeat(message.linePos + 5)}^^^
` : "";
    return `
${numberedLines}${positionIndicator}${message.type.toUpperCase()}: ${message.message}

`;
  }
  const color = message.type === "error" ? "red" : "#8B4000";
  return (options == null ? void 0 : options.html) ? `<div class='luma-compiler-log-error' style="color:${color};"><b> ${message.type.toUpperCase()}: ${message.message}</b></div>` : `${message.type.toUpperCase()}: ${message.message}`;
}
function getNumberedLines(lines, lineNum, options) {
  let numberedLines = "";
  for (let lineIndex = lineNum - 2; lineIndex <= lineNum; lineIndex++) {
    const sourceLine = lines[lineIndex - 1];
    if (sourceLine !== void 0) {
      numberedLines += getNumberedLine(sourceLine, lineNum, options);
    }
  }
  return numberedLines;
}
function getNumberedLine(line, lineNum, options) {
  const escapedLine = (options == null ? void 0 : options.html) ? escapeHTML(line) : line;
  return `${padLeft(String(lineNum), 4)}: ${escapedLine}${(options == null ? void 0 : options.html) ? "<br/>" : "\n"}`;
}
function padLeft(string, paddedLength) {
  let result = "";
  for (let i = string.length; i < paddedLength; ++i) {
    result += " ";
  }
  return result + string;
}
function escapeHTML(unsafe) {
  return unsafe.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#039;");
}

// dist/adapter/resources/shader.js
var _Shader = class extends Resource {
  get [Symbol.toStringTag]() {
    return "Shader";
  }
  /** The stage of this shader */
  stage;
  /** The source code of this shader */
  source;
  /** The compilation status of the shader. 'pending' if compilation is asynchronous, and on production */
  compilationStatus = "pending";
  /** Create a new Shader instance */
  constructor(device, props) {
    props = { ...props, debugShaders: props.debugShaders || device.props.debugShaders || "errors" };
    super(device, { id: getShaderIdFromProps(props), ...props }, _Shader.defaultProps);
    this.stage = this.props.stage;
    this.source = this.props.source;
  }
  /** Get compiler log synchronously (WebGL only) */
  getCompilationInfoSync() {
    return null;
  }
  /** Get translated shader source in host platform's native language (HLSL, GLSL, and even GLSL ES), if available */
  getTranslatedSource() {
    return null;
  }
  // PORTABLE HELPERS
  /** In browser logging of errors */
  async debugShader() {
    const trigger = this.props.debugShaders;
    switch (trigger) {
      case "never":
        return;
      case "errors":
        if (this.compilationStatus === "success") {
          return;
        }
        break;
      case "warnings":
      case "always":
        break;
    }
    const messages = await this.getCompilationInfo();
    if (trigger === "warnings" && (messages == null ? void 0 : messages.length) === 0) {
      return;
    }
    this._displayShaderLog(messages, this.id);
  }
  // PRIVATE
  /**
   * In-browser UI logging of errors
   * TODO - this HTML formatting code should not be in Device, should be pluggable
   */
  _displayShaderLog(messages, shaderId) {
    var _a;
    if (typeof document === "undefined" || !(document == null ? void 0 : document.createElement)) {
      return;
    }
    const shaderName = shaderId;
    const shaderTitle = `${this.stage} shader "${shaderName}"`;
    let htmlLog = formatCompilerLog(messages, this.source, { showSourceCode: "all", html: true });
    const translatedSource = this.getTranslatedSource();
    if (translatedSource) {
      htmlLog += `<br /><br /><h1>Translated Source</h1><br /><br /><code style="user-select:text;"><pre>${translatedSource}</pre></code>`;
    }
    const button = document.createElement("Button");
    button.innerHTML = `
<h1>Compilation error in ${shaderTitle}</h1><br /><br />
<code style="user-select:text;"><pre>
${htmlLog}
</pre></code>`;
    button.style.top = "10px";
    button.style.left = "10px";
    button.style.position = "absolute";
    button.style.zIndex = "9999";
    button.style.width = "100%";
    button.style.textAlign = "left";
    document.body.appendChild(button);
    const errors = document.getElementsByClassName("luma-compiler-log-error");
    (_a = errors[0]) == null ? void 0 : _a.scrollIntoView();
    button.onclick = () => {
      const dataURI = `data:text/plain,${encodeURIComponent(this.source)}`;
      navigator.clipboard.writeText(dataURI);
    };
  }
};
var Shader = _Shader;
__publicField(Shader, "defaultProps", {
  ...Resource.defaultProps,
  language: "auto",
  stage: void 0,
  source: "",
  sourceMap: null,
  entryPoint: "main",
  debugShaders: void 0
});
function getShaderIdFromProps(props) {
  return getShaderName(props.source) || props.id || uid(`unnamed ${props.stage}-shader`);
}
function getShaderName(shader, defaultName = "unnamed") {
  const SHADER_NAME_REGEXP = /#define[\s*]SHADER_NAME[\s*]([A-Za-z0-9_-]+)[\s*]/;
  const match = SHADER_NAME_REGEXP.exec(shader);
  return match ? match[1] : defaultName;
}

// dist/adapter/resources/framebuffer.js
var _Framebuffer = class extends Resource {
  get [Symbol.toStringTag]() {
    return "Framebuffer";
  }
  /** Width of all attachments in this framebuffer */
  width;
  /** Height of all attachments in this framebuffer */
  height;
  constructor(device, props = {}) {
    super(device, props, _Framebuffer.defaultProps);
    this.width = this.props.width;
    this.height = this.props.height;
  }
  /**
   * Create a copy of this framebuffer with new attached textures, with same props but of the specified size.
   * @note Does not copy contents of the attached textures.
   */
  clone(size) {
    const colorAttachments = this.colorAttachments.map((colorAttachment) => colorAttachment.texture.clone(size));
    const depthStencilAttachment = this.depthStencilAttachment && this.depthStencilAttachment.texture.clone(size);
    return this.device.createFramebuffer({ ...this.props, colorAttachments, depthStencilAttachment });
  }
  resize(size) {
    let updateSize = !size;
    if (size) {
      const [width, height] = Array.isArray(size) ? size : [size.width, size.height];
      updateSize = updateSize || height !== this.height || width !== this.width;
      this.width = width;
      this.height = height;
    }
    if (updateSize) {
      log.log(2, `Resizing framebuffer ${this.id} to ${this.width}x${this.height}`)();
      this.resizeAttachments(this.width, this.height);
    }
  }
  /** Auto creates any textures */
  autoCreateAttachmentTextures() {
    if (this.props.colorAttachments.length === 0 && !this.props.depthStencilAttachment) {
      throw new Error("Framebuffer has noattachments");
    }
    this.colorAttachments = this.props.colorAttachments.map((attachment2, index) => {
      if (typeof attachment2 === "string") {
        const texture = this.createColorTexture(attachment2, index);
        this.attachResource(texture);
        return texture.view;
      }
      if (attachment2 instanceof Texture) {
        return attachment2.view;
      }
      return attachment2;
    });
    const attachment = this.props.depthStencilAttachment;
    if (attachment) {
      if (typeof attachment === "string") {
        const texture = this.createDepthStencilTexture(attachment);
        this.attachResource(texture);
        this.depthStencilAttachment = texture.view;
      } else if (attachment instanceof Texture) {
        this.depthStencilAttachment = attachment.view;
      } else {
        this.depthStencilAttachment = attachment;
      }
    }
  }
  /** Create a color texture */
  createColorTexture(format, index) {
    return this.device.createTexture({
      id: `${this.id}-color-attachment-${index}`,
      usage: Texture.RENDER_ATTACHMENT,
      format,
      width: this.width,
      height: this.height,
      // TODO deprecated? - luma.gl v8 compatibility
      sampler: {
        magFilter: "linear",
        minFilter: "linear"
      }
    });
  }
  /** Create depth stencil texture */
  createDepthStencilTexture(format) {
    return this.device.createTexture({
      id: `${this.id}-depth-stencil-attachment`,
      usage: Texture.RENDER_ATTACHMENT,
      format,
      width: this.width,
      height: this.height
    });
  }
  /**
   * Default implementation of resize
   * Creates new textures with correct size for all attachments.
   * and destroys existing textures if owned
   */
  resizeAttachments(width, height) {
    for (let i = 0; i < this.colorAttachments.length; ++i) {
      if (this.colorAttachments[i]) {
        const resizedTexture = this.colorAttachments[i].texture.clone({
          width,
          height
        });
        this.destroyAttachedResource(this.colorAttachments[i]);
        this.colorAttachments[i] = resizedTexture.view;
        this.attachResource(resizedTexture.view);
      }
    }
    if (this.depthStencilAttachment) {
      const resizedTexture = this.depthStencilAttachment.texture.clone({
        width,
        height
      });
      this.destroyAttachedResource(this.depthStencilAttachment);
      this.depthStencilAttachment = resizedTexture.view;
      this.attachResource(resizedTexture);
    }
    this.updateAttachments();
  }
};
var Framebuffer = _Framebuffer;
__publicField(Framebuffer, "defaultProps", {
  ...Resource.defaultProps,
  width: 1,
  height: 1,
  colorAttachments: [],
  // ['rgba8unorm'],
  depthStencilAttachment: null
  // 'depth24plus-stencil8'
});

// dist/adapter/resources/render-pipeline.js
var _RenderPipeline = class extends Resource {
  get [Symbol.toStringTag]() {
    return "RenderPipeline";
  }
  /** The merged layout */
  shaderLayout;
  /** Buffer map describing buffer interleaving etc */
  bufferLayout;
  /** The linking status of the pipeline. 'pending' if linking is asynchronous, and on production */
  linkStatus = "pending";
  /** The hash of the pipeline */
  hash = "";
  constructor(device, props) {
    super(device, props, _RenderPipeline.defaultProps);
    this.shaderLayout = this.props.shaderLayout;
    this.bufferLayout = this.props.bufferLayout || [];
  }
};
var RenderPipeline = _RenderPipeline;
__publicField(RenderPipeline, "defaultProps", {
  ...Resource.defaultProps,
  vs: null,
  vertexEntryPoint: "vertexMain",
  vsConstants: {},
  fs: null,
  fragmentEntryPoint: "fragmentMain",
  fsConstants: {},
  shaderLayout: null,
  bufferLayout: [],
  topology: "triangle-list",
  colorAttachmentFormats: void 0,
  depthStencilAttachmentFormat: void 0,
  parameters: {},
  bindings: {},
  uniforms: {}
});

// dist/adapter/resources/render-pass.js
var _RenderPass = class extends Resource {
  get [Symbol.toStringTag]() {
    return "RenderPass";
  }
  constructor(device, props) {
    props = _RenderPass.normalizeProps(device, props);
    super(device, props, _RenderPass.defaultProps);
  }
  static normalizeProps(device, props) {
    return props;
  }
};
var RenderPass = _RenderPass;
/** TODO - should be [0, 0, 0, 0], update once deck.gl tests run clean */
__publicField(RenderPass, "defaultClearColor", [0, 0, 0, 1]);
/** Depth 1.0 represents the far plance */
__publicField(RenderPass, "defaultClearDepth", 1);
/** Clears all stencil bits */
__publicField(RenderPass, "defaultClearStencil", 0);
/** Default properties for RenderPass */
__publicField(RenderPass, "defaultProps", {
  ...Resource.defaultProps,
  framebuffer: null,
  parameters: void 0,
  clearColor: _RenderPass.defaultClearColor,
  clearColors: void 0,
  clearDepth: _RenderPass.defaultClearDepth,
  clearStencil: _RenderPass.defaultClearStencil,
  depthReadOnly: false,
  stencilReadOnly: false,
  discard: false,
  occlusionQuerySet: void 0,
  timestampQuerySet: void 0,
  beginTimestampIndex: void 0,
  endTimestampIndex: void 0
});

// dist/adapter/resources/compute-pipeline.js
var _ComputePipeline = class extends Resource {
  get [Symbol.toStringTag]() {
    return "ComputePipeline";
  }
  hash = "";
  /** The merged shader layout */
  shaderLayout;
  constructor(device, props) {
    super(device, props, _ComputePipeline.defaultProps);
    this.shaderLayout = props.shaderLayout;
  }
};
var ComputePipeline = _ComputePipeline;
__publicField(ComputePipeline, "defaultProps", {
  ...Resource.defaultProps,
  shader: void 0,
  entryPoint: void 0,
  constants: {},
  shaderLayout: void 0
});

// dist/adapter/resources/compute-pass.js
var _ComputePass = class extends Resource {
  constructor(device, props) {
    super(device, props, _ComputePass.defaultProps);
  }
  get [Symbol.toStringTag]() {
    return "ComputePass";
  }
};
var ComputePass = _ComputePass;
__publicField(ComputePass, "defaultProps", {
  ...Resource.defaultProps,
  timestampQuerySet: void 0,
  beginTimestampIndex: void 0,
  endTimestampIndex: void 0
});

// dist/adapter/resources/command-encoder.js
var _CommandEncoder = class extends Resource {
  get [Symbol.toStringTag]() {
    return "CommandEncoder";
  }
  constructor(device, props) {
    super(device, props, _CommandEncoder.defaultProps);
  }
};
var CommandEncoder = _CommandEncoder;
// TODO - luma.gl has these on the device, should we align with WebGPU API?
// beginRenderPass(GPURenderPassDescriptor descriptor): GPURenderPassEncoder;
// beginComputePass(optional GPUComputePassDescriptor descriptor = {}): GPUComputePassEncoder;
__publicField(CommandEncoder, "defaultProps", {
  ...Resource.defaultProps,
  measureExecutionTime: void 0
});

// dist/adapter/resources/command-buffer.js
var _CommandBuffer = class extends Resource {
  get [Symbol.toStringTag]() {
    return "CommandBuffer";
  }
  constructor(device, props) {
    super(device, props, _CommandBuffer.defaultProps);
  }
};
var CommandBuffer = _CommandBuffer;
__publicField(CommandBuffer, "defaultProps", {
  ...Resource.defaultProps
});

// dist/shadertypes/data-types/decode-shader-types.js
function getVariableShaderTypeInfo(format) {
  const decoded = UNIFORM_FORMATS[format];
  return decoded;
}
function getAttributeShaderTypeInfo(attributeType) {
  const [primitiveType, components] = TYPE_INFO[attributeType];
  const integer = primitiveType === "i32" || primitiveType === "u32";
  const signed = primitiveType !== "u32";
  const byteLength = PRIMITIVE_TYPE_SIZES[primitiveType] * components;
  return {
    primitiveType,
    components,
    byteLength,
    integer,
    signed
  };
}
var PRIMITIVE_TYPE_SIZES = {
  f32: 4,
  f16: 2,
  i32: 4,
  u32: 4
  // 'bool-webgl': 4,
};
var TYPE_INFO = {
  f32: ["f32", 1],
  "vec2<f32>": ["f32", 2],
  "vec3<f32>": ["f32", 3],
  "vec4<f32>": ["f32", 4],
  f16: ["f16", 1],
  "vec2<f16>": ["f16", 2],
  "vec3<f16>": ["f16", 3],
  "vec4<f16>": ["f16", 4],
  i32: ["i32", 1],
  "vec2<i32>": ["i32", 2],
  "vec3<i32>": ["i32", 3],
  "vec4<i32>": ["i32", 4],
  u32: ["u32", 1],
  "vec2<u32>": ["u32", 2],
  "vec3<u32>": ["u32", 3],
  "vec4<u32>": ["u32", 4]
};
var UNIFORM_FORMATS = {
  f32: { type: "f32", components: 1 },
  f16: { type: "f16", components: 1 },
  i32: { type: "i32", components: 1 },
  u32: { type: "u32", components: 1 },
  // 'bool-webgl': {type: 'bool-webgl', components: 1},
  "vec2<f32>": { type: "f32", components: 2 },
  "vec3<f32>": { type: "f32", components: 3 },
  "vec4<f32>": { type: "f32", components: 4 },
  "vec2<f16>": { type: "f16", components: 2 },
  "vec3<f16>": { type: "f16", components: 3 },
  "vec4<f16>": { type: "f16", components: 4 },
  "vec2<i32>": { type: "i32", components: 2 },
  "vec3<i32>": { type: "i32", components: 3 },
  "vec4<i32>": { type: "i32", components: 4 },
  "vec2<u32>": { type: "u32", components: 2 },
  "vec3<u32>": { type: "u32", components: 3 },
  "vec4<u32>": { type: "u32", components: 4 },
  "mat2x2<f32>": { type: "f32", components: 4 },
  "mat2x3<f32>": { type: "f32", components: 6 },
  "mat2x4<f32>": { type: "f32", components: 8 },
  "mat3x2<f32>": { type: "f32", components: 6 },
  "mat3x3<f32>": { type: "f32", components: 9 },
  "mat3x4<f32>": { type: "f32", components: 12 },
  "mat4x2<f32>": { type: "f32", components: 8 },
  "mat4x3<f32>": { type: "f32", components: 12 },
  "mat4x4<f32>": { type: "f32", components: 16 },
  "mat2x2<f16>": { type: "f16", components: 4 },
  "mat2x3<f16>": { type: "f16", components: 6 },
  "mat2x4<f16>": { type: "f16", components: 8 },
  "mat3x2<f16>": { type: "f16", components: 6 },
  "mat3x3<f16>": { type: "f16", components: 9 },
  "mat3x4<f16>": { type: "f16", components: 12 },
  "mat4x2<f16>": { type: "f16", components: 8 },
  "mat4x3<f16>": { type: "f16", components: 12 },
  "mat4x4<f16>": { type: "f16", components: 16 },
  "mat2x2<i32>": { type: "i32", components: 4 },
  "mat2x3<i32>": { type: "i32", components: 6 },
  "mat2x4<i32>": { type: "i32", components: 8 },
  "mat3x2<i32>": { type: "i32", components: 6 },
  "mat3x3<i32>": { type: "i32", components: 9 },
  "mat3x4<i32>": { type: "i32", components: 12 },
  "mat4x2<i32>": { type: "i32", components: 8 },
  "mat4x3<i32>": { type: "i32", components: 12 },
  "mat4x4<i32>": { type: "i32", components: 16 },
  "mat2x2<u32>": { type: "u32", components: 4 },
  "mat2x3<u32>": { type: "u32", components: 6 },
  "mat2x4<u32>": { type: "u32", components: 8 },
  "mat3x2<u32>": { type: "u32", components: 6 },
  "mat3x3<u32>": { type: "u32", components: 9 },
  "mat3x4<u32>": { type: "u32", components: 12 },
  "mat4x2<u32>": { type: "u32", components: 8 },
  "mat4x3<u32>": { type: "u32", components: 12 },
  "mat4x4<u32>": { type: "u32", components: 16 }
};
var WGSL_ATTRIBUTE_TYPE_ALIAS_MAP = {
  vec2i: "vec2<i32>",
  vec3i: "vec3<i32>",
  vec4i: "vec4<i32>",
  vec2u: "vec2<u32>",
  vec3u: "vec3<u32>",
  vec4u: "vec4<u32>",
  vec2f: "vec2<f32>",
  vec3f: "vec3<f32>",
  vec4f: "vec4<f32>",
  // Requires the f16 extension.
  vec2h: "vec2<f16>",
  vec3h: "vec3<f16>",
  vec4h: "vec4<f16>"
};
var WGSL_VARIABLE_TYPE_ALIAS_MAP = {
  ...WGSL_ATTRIBUTE_TYPE_ALIAS_MAP,
  mat2x2f: "mat2x2<f32>",
  mat2x3f: "mat2x3<f32>",
  mat2x4f: "mat2x4<f32>",
  mat3x2f: "mat3x2<f32>",
  mat3x3f: "mat3x3<f32>",
  mat3x4f: "mat3x4<f32>",
  mat4x2f: "mat4x2<f32>",
  mat4x3f: "mat4x3<f32>",
  mat4x4f: "mat4x4<f32>",
  mat2x2i: "mat2x2<i32>",
  mat2x3i: "mat2x3<i32>",
  mat2x4i: "mat2x4<i32>",
  mat3x2i: "mat3x2<i32>",
  mat3x3i: "mat3x3<i32>",
  mat3x4i: "mat3x4<i32>",
  mat4x2i: "mat4x2<i32>",
  mat4x3i: "mat4x3<i32>",
  mat4x4i: "mat4x4<i32>",
  mat2x2u: "mat2x2<u32>",
  mat2x3u: "mat2x3<u32>",
  mat2x4u: "mat2x4<u32>",
  mat3x2u: "mat3x2<u32>",
  mat3x3u: "mat3x3<u32>",
  mat3x4u: "mat3x4<u32>",
  mat4x2u: "mat4x2<u32>",
  mat4x3u: "mat4x3<u32>",
  mat4x4u: "mat4x4<u32>",
  mat2x2h: "mat2x2<f16>",
  mat2x3h: "mat2x3<f16>",
  mat2x4h: "mat2x4<f16>",
  mat3x2h: "mat3x2<f16>",
  mat3x3h: "mat3x3<f16>",
  mat3x4h: "mat3x4<f16>",
  mat4x2h: "mat4x2<f16>",
  mat4x3h: "mat4x3<f16>",
  mat4x4h: "mat4x4<f16>"
};

// dist/adapter-utils/get-attribute-from-layouts.js
function getAttributeInfosFromLayouts(shaderLayout, bufferLayout) {
  const attributeInfos = {};
  for (const attribute of shaderLayout.attributes) {
    const attributeInfo = getAttributeInfoFromLayouts(shaderLayout, bufferLayout, attribute.name);
    if (attributeInfo) {
      attributeInfos[attribute.name] = attributeInfo;
    }
  }
  return attributeInfos;
}
function getAttributeInfosByLocation(shaderLayout, bufferLayout, maxVertexAttributes = 16) {
  const attributeInfos = getAttributeInfosFromLayouts(shaderLayout, bufferLayout);
  const locationInfos = new Array(maxVertexAttributes).fill(null);
  for (const attributeInfo of Object.values(attributeInfos)) {
    locationInfos[attributeInfo.location] = attributeInfo;
  }
  return locationInfos;
}
function getAttributeInfoFromLayouts(shaderLayout, bufferLayout, name2) {
  const shaderDeclaration = getAttributeFromShaderLayout(shaderLayout, name2);
  const bufferMapping = getAttributeFromBufferLayout(bufferLayout, name2);
  if (!shaderDeclaration) {
    return null;
  }
  const attributeTypeInfo = getAttributeShaderTypeInfo(shaderDeclaration.type);
  const defaultVertexFormat = getCompatibleVertexFormat(attributeTypeInfo);
  const vertexFormat = (bufferMapping == null ? void 0 : bufferMapping.vertexFormat) || defaultVertexFormat;
  const vertexFormatInfo = getVertexFormatInfo(vertexFormat);
  return {
    attributeName: (bufferMapping == null ? void 0 : bufferMapping.attributeName) || shaderDeclaration.name,
    bufferName: (bufferMapping == null ? void 0 : bufferMapping.bufferName) || shaderDeclaration.name,
    location: shaderDeclaration.location,
    shaderType: shaderDeclaration.type,
    primitiveType: attributeTypeInfo.primitiveType,
    shaderComponents: attributeTypeInfo.components,
    vertexFormat,
    bufferDataType: vertexFormatInfo.type,
    bufferComponents: vertexFormatInfo.components,
    // normalized is a property of the buffer's vertex format
    normalized: vertexFormatInfo.normalized,
    // integer is a property of the shader declaration
    integer: attributeTypeInfo.integer,
    stepMode: (bufferMapping == null ? void 0 : bufferMapping.stepMode) || shaderDeclaration.stepMode || "vertex",
    byteOffset: (bufferMapping == null ? void 0 : bufferMapping.byteOffset) || 0,
    byteStride: (bufferMapping == null ? void 0 : bufferMapping.byteStride) || 0
  };
}
function getAttributeFromShaderLayout(shaderLayout, name2) {
  const attribute = shaderLayout.attributes.find((attr) => attr.name === name2);
  if (!attribute) {
    log.warn(`shader layout attribute "${name2}" not present in shader`);
  }
  return attribute || null;
}
function getAttributeFromBufferLayout(bufferLayouts, name2) {
  checkBufferLayouts(bufferLayouts);
  let bufferLayoutInfo = getAttributeFromShortHand(bufferLayouts, name2);
  if (bufferLayoutInfo) {
    return bufferLayoutInfo;
  }
  bufferLayoutInfo = getAttributeFromAttributesList(bufferLayouts, name2);
  if (bufferLayoutInfo) {
    return bufferLayoutInfo;
  }
  log.warn(`layout for attribute "${name2}" not present in buffer layout`);
  return null;
}
function checkBufferLayouts(bufferLayouts) {
  for (const bufferLayout of bufferLayouts) {
    if (bufferLayout.attributes && bufferLayout.format || !bufferLayout.attributes && !bufferLayout.format) {
      log.warn(`BufferLayout ${name} must have either 'attributes' or 'format' field`);
    }
  }
}
function getAttributeFromShortHand(bufferLayouts, name2) {
  for (const bufferLayout of bufferLayouts) {
    if (bufferLayout.format && bufferLayout.name === name2) {
      return {
        attributeName: bufferLayout.name,
        bufferName: name2,
        stepMode: bufferLayout.stepMode,
        vertexFormat: bufferLayout.format,
        // If offset is needed, use `attributes` field.
        byteOffset: 0,
        byteStride: bufferLayout.byteStride || 0
      };
    }
  }
  return null;
}
function getAttributeFromAttributesList(bufferLayouts, name2) {
  var _a;
  for (const bufferLayout of bufferLayouts) {
    let byteStride = bufferLayout.byteStride;
    if (typeof bufferLayout.byteStride !== "number") {
      for (const attributeMapping2 of bufferLayout.attributes || []) {
        const info = getVertexFormatInfo(attributeMapping2.format);
        byteStride += info.byteLength;
      }
    }
    const attributeMapping = (_a = bufferLayout.attributes) == null ? void 0 : _a.find((mapping) => mapping.attribute === name2);
    if (attributeMapping) {
      return {
        attributeName: attributeMapping.attribute,
        bufferName: bufferLayout.name,
        stepMode: bufferLayout.stepMode,
        vertexFormat: attributeMapping.format,
        byteOffset: attributeMapping.byteOffset,
        // @ts-ignore
        byteStride
      };
    }
  }
  return null;
}

// dist/adapter/resources/vertex-array.js
var _VertexArray = class extends Resource {
  get [Symbol.toStringTag]() {
    return "VertexArray";
  }
  /** Max number of vertex attributes */
  maxVertexAttributes;
  /** Attribute infos indexed by location - TODO only needed by webgl module? */
  attributeInfos;
  /** Index buffer */
  indexBuffer = null;
  /** Attributes indexed by buffer slot */
  attributes;
  constructor(device, props) {
    super(device, props, _VertexArray.defaultProps);
    this.maxVertexAttributes = device.limits.maxVertexAttributes;
    this.attributes = new Array(this.maxVertexAttributes).fill(null);
    this.attributeInfos = getAttributeInfosByLocation(props.shaderLayout, props.bufferLayout, this.maxVertexAttributes);
  }
  // DEPRECATED METHODS
  /** @deprecated Set constant attributes (WebGL only) */
  setConstantWebGL(location, value) {
    this.device.reportError(new Error("constant attributes not supported"), this)();
  }
};
var VertexArray = _VertexArray;
__publicField(VertexArray, "defaultProps", {
  ...Resource.defaultProps,
  shaderLayout: void 0,
  bufferLayout: []
});

// dist/adapter/resources/transform-feedback.js
var _TransformFeedback = class extends Resource {
  get [Symbol.toStringTag]() {
    return "TransformFeedback";
  }
  constructor(device, props) {
    super(device, props, _TransformFeedback.defaultProps);
  }
};
var TransformFeedback = _TransformFeedback;
__publicField(TransformFeedback, "defaultProps", {
  ...Resource.defaultProps,
  layout: void 0,
  buffers: {}
});

// dist/adapter/resources/query-set.js
var _QuerySet = class extends Resource {
  get [Symbol.toStringTag]() {
    return "QuerySet";
  }
  constructor(device, props) {
    super(device, props, _QuerySet.defaultProps);
  }
};
var QuerySet = _QuerySet;
__publicField(QuerySet, "defaultProps", {
  ...Resource.defaultProps,
  type: void 0,
  count: void 0
});

// dist/adapter/resources/pipeline-layout.js
var _PipelineLayout = class extends Resource {
  get [Symbol.toStringTag]() {
    return "PipelineLayout";
  }
  constructor(device, props) {
    super(device, props, _PipelineLayout.defaultProps);
  }
};
var PipelineLayout = _PipelineLayout;
__publicField(PipelineLayout, "defaultProps", {
  ...Resource.defaultProps,
  shaderLayout: {
    attributes: [],
    bindings: []
  }
});

// dist/utils/array-utils-flat.js
var arrayBuffer;
function getScratchArrayBuffer(byteLength) {
  if (!arrayBuffer || arrayBuffer.byteLength < byteLength) {
    arrayBuffer = new ArrayBuffer(byteLength);
  }
  return arrayBuffer;
}
function getScratchArray(Type, length) {
  const scratchArrayBuffer = getScratchArrayBuffer(Type.BYTES_PER_ELEMENT * length);
  return new Type(scratchArrayBuffer, 0, length);
}

// dist/utils/is-array.js
function isTypedArray(value) {
  return ArrayBuffer.isView(value) && !(value instanceof DataView);
}
function isNumberArray(value) {
  if (Array.isArray(value)) {
    return value.length === 0 || typeof value[0] === "number";
  }
  return isTypedArray(value);
}

// dist/portable/uniform-buffer-layout.js
var minBufferSize = 1024;
var UniformBufferLayout = class {
  layout = {};
  /** number of bytes needed for buffer allocation */
  byteLength;
  /** Create a new UniformBufferLayout given a map of attributes. */
  constructor(uniformTypes, uniformSizes = {}) {
    let size = 0;
    for (const [key, uniformType] of Object.entries(uniformTypes)) {
      const typeAndComponents = getVariableShaderTypeInfo(uniformType);
      const { type, components } = typeAndComponents;
      const count = components * ((uniformSizes == null ? void 0 : uniformSizes[key]) ?? 1);
      size = alignTo(size, count);
      const offset = size;
      size += count;
      this.layout[key] = { type, size: count, offset };
    }
    size += (4 - size % 4) % 4;
    const actualByteLength = size * 4;
    this.byteLength = Math.max(actualByteLength, minBufferSize);
  }
  /** Get the data for the complete buffer */
  getData(uniformValues) {
    const arrayBuffer2 = getScratchArrayBuffer(this.byteLength);
    const typedArrays = {
      i32: new Int32Array(arrayBuffer2),
      u32: new Uint32Array(arrayBuffer2),
      f32: new Float32Array(arrayBuffer2),
      // TODO not implemented
      f16: new Uint16Array(arrayBuffer2)
    };
    for (const [name2, value] of Object.entries(uniformValues)) {
      const uniformLayout = this.layout[name2];
      if (!uniformLayout) {
        log.warn(`Supplied uniform value ${name2} not present in uniform block layout`)();
        continue;
      }
      const { type, size, offset } = uniformLayout;
      const typedArray = typedArrays[type];
      if (size === 1) {
        if (typeof value !== "number" && typeof value !== "boolean") {
          log.warn(`Supplied value for single component uniform ${name2} is not a number: ${value}`)();
          continue;
        }
        typedArray[offset] = Number(value);
      } else {
        if (!isNumberArray(value)) {
          log.warn(`Supplied value for multi component / array uniform ${name2} is not a numeric array: ${value}`)();
          continue;
        }
        typedArray.set(value, offset);
      }
    }
    return new Uint8Array(arrayBuffer2, 0, this.byteLength);
  }
  /** Does this layout have a field with specified name */
  has(name2) {
    return Boolean(this.layout[name2]);
  }
  /** Get offset and size for a field with specified name */
  get(name2) {
    const layout = this.layout[name2];
    return layout;
  }
};

// dist/utils/array-equal.js
function arrayEqual(a, b, limit = 16) {
  if (a !== b) {
    return false;
  }
  const arrayA = a;
  const arrayB = b;
  if (!isNumberArray(arrayA)) {
    return false;
  }
  if (isNumberArray(arrayB) && arrayA.length === arrayB.length) {
    for (let i = 0; i < arrayA.length; ++i) {
      if (arrayB[i] !== arrayA[i]) {
        return false;
      }
    }
  }
  return true;
}
function arrayCopy(a) {
  if (isNumberArray(a)) {
    return a.slice();
  }
  return a;
}

// dist/portable/uniform-block.js
var UniformBlock = class {
  name;
  uniforms = {};
  modifiedUniforms = {};
  modified = true;
  bindingLayout = {};
  needsRedraw = "initialized";
  constructor(props) {
    var _a;
    this.name = (props == null ? void 0 : props.name) || "unnamed";
    if ((props == null ? void 0 : props.name) && (props == null ? void 0 : props.shaderLayout)) {
      const binding = (_a = props == null ? void 0 : props.shaderLayout.bindings) == null ? void 0 : _a.find((binding_) => binding_.type === "uniform" && binding_.name === (props == null ? void 0 : props.name));
      if (!binding) {
        throw new Error(props == null ? void 0 : props.name);
      }
      const uniformBlock = binding;
      for (const uniform of uniformBlock.uniforms || []) {
        this.bindingLayout[uniform.name] = uniform;
      }
    }
  }
  /** Set a map of uniforms */
  setUniforms(uniforms) {
    for (const [key, value] of Object.entries(uniforms)) {
      this._setUniform(key, value);
      if (!this.needsRedraw) {
        this.setNeedsRedraw(`${this.name}.${key}=${value}`);
      }
    }
  }
  setNeedsRedraw(reason) {
    this.needsRedraw = this.needsRedraw || reason;
  }
  /** Returns all uniforms */
  getAllUniforms() {
    this.modifiedUniforms = {};
    this.needsRedraw = false;
    return this.uniforms || {};
  }
  /** Set a single uniform */
  _setUniform(key, value) {
    if (arrayEqual(this.uniforms[key], value)) {
      return;
    }
    this.uniforms[key] = arrayCopy(value);
    this.modifiedUniforms[key] = true;
    this.modified = true;
  }
};

// dist/portable/uniform-store.js
var UniformStore = class {
  /** Stores the uniform values for each uniform block */
  uniformBlocks = /* @__PURE__ */ new Map();
  /** Can generate data for a uniform buffer for each block from data */
  uniformBufferLayouts = /* @__PURE__ */ new Map();
  /** Actual buffer for the blocks */
  uniformBuffers = /* @__PURE__ */ new Map();
  /**
   * Create a new UniformStore instance
   * @param blocks
   */
  constructor(blocks) {
    for (const [bufferName, block] of Object.entries(blocks)) {
      const uniformBufferName = bufferName;
      const uniformBufferLayout = new UniformBufferLayout(block.uniformTypes ?? {}, block.uniformSizes ?? {});
      this.uniformBufferLayouts.set(uniformBufferName, uniformBufferLayout);
      const uniformBlock = new UniformBlock({ name: bufferName });
      uniformBlock.setUniforms(block.defaultUniforms || {});
      this.uniformBlocks.set(uniformBufferName, uniformBlock);
    }
  }
  /** Destroy any managed uniform buffers */
  destroy() {
    for (const uniformBuffer of this.uniformBuffers.values()) {
      uniformBuffer.destroy();
    }
  }
  /**
   * Set uniforms
   * Makes all properties partial
   */
  setUniforms(uniforms) {
    var _a;
    for (const [blockName, uniformValues] of Object.entries(uniforms)) {
      (_a = this.uniformBlocks.get(blockName)) == null ? void 0 : _a.setUniforms(uniformValues);
    }
    this.updateUniformBuffers();
  }
  /** Get the required minimum length of the uniform buffer */
  getUniformBufferByteLength(uniformBufferName) {
    var _a;
    return ((_a = this.uniformBufferLayouts.get(uniformBufferName)) == null ? void 0 : _a.byteLength) || 0;
  }
  /** Get formatted binary memory that can be uploaded to a buffer */
  getUniformBufferData(uniformBufferName) {
    var _a, _b;
    const uniformValues = ((_a = this.uniformBlocks.get(uniformBufferName)) == null ? void 0 : _a.getAllUniforms()) || {};
    return (_b = this.uniformBufferLayouts.get(uniformBufferName)) == null ? void 0 : _b.getData(uniformValues);
  }
  /**
   * Creates an unmanaged uniform buffer (umnanaged means that application is responsible for destroying it)
   * The new buffer is initialized with current / supplied values
   */
  createUniformBuffer(device, uniformBufferName, uniforms) {
    if (uniforms) {
      this.setUniforms(uniforms);
    }
    const byteLength = this.getUniformBufferByteLength(uniformBufferName);
    const uniformBuffer = device.createBuffer({
      usage: Buffer2.UNIFORM | Buffer2.COPY_DST,
      byteLength
    });
    const uniformBufferData = this.getUniformBufferData(uniformBufferName);
    uniformBuffer.write(uniformBufferData);
    return uniformBuffer;
  }
  /** Get the managed uniform buffer. "managed" resources are destroyed when the uniformStore is destroyed. */
  getManagedUniformBuffer(device, uniformBufferName) {
    if (!this.uniformBuffers.get(uniformBufferName)) {
      const byteLength = this.getUniformBufferByteLength(uniformBufferName);
      const uniformBuffer = device.createBuffer({
        usage: Buffer2.UNIFORM | Buffer2.COPY_DST,
        byteLength
      });
      this.uniformBuffers.set(uniformBufferName, uniformBuffer);
    }
    return this.uniformBuffers.get(uniformBufferName);
  }
  /** Updates all uniform buffers where values have changed */
  updateUniformBuffers() {
    let reason = false;
    for (const uniformBufferName of this.uniformBlocks.keys()) {
      const bufferReason = this.updateUniformBuffer(uniformBufferName);
      reason ||= bufferReason;
    }
    if (reason) {
      log.log(3, `UniformStore.updateUniformBuffers(): ${reason}`)();
    }
    return reason;
  }
  /** Update one uniform buffer. Only updates if values have changed */
  updateUniformBuffer(uniformBufferName) {
    var _a;
    const uniformBlock = this.uniformBlocks.get(uniformBufferName);
    let uniformBuffer = this.uniformBuffers.get(uniformBufferName);
    let reason = false;
    if (uniformBuffer && (uniformBlock == null ? void 0 : uniformBlock.needsRedraw)) {
      reason ||= uniformBlock.needsRedraw;
      const uniformBufferData = this.getUniformBufferData(uniformBufferName);
      uniformBuffer = this.uniformBuffers.get(uniformBufferName);
      uniformBuffer == null ? void 0 : uniformBuffer.write(uniformBufferData);
      const uniformValues = (_a = this.uniformBlocks.get(uniformBufferName)) == null ? void 0 : _a.getAllUniforms();
      log.log(4, `Writing to uniform buffer ${String(uniformBufferName)}`, uniformBufferData, uniformValues)();
    }
    return reason;
  }
};

// dist/shadertypes/textures/pixel-utils.js
function readPixel(pixelData, x, y, bitsPerChannel) {
  if (x < 0 || x >= pixelData.width || y < 0 || y >= pixelData.height) {
    throw new Error("Coordinates out of bounds.");
  }
  const byteOffset = y * pixelData.bytesPerRow + x * pixelData.bytesPerPixel;
  const pixelDataView = new DataView(pixelData.arrayBuffer, byteOffset, pixelData.bytesPerPixel);
  let bitOffsetWithinPixel = 0;
  const channels = [];
  for (let i = 0; i < 4; i++) {
    const bits = bitsPerChannel[i];
    if (bits <= 0) {
      channels.push(0);
    } else {
      const channelValue = readBitsFromDataView(pixelDataView, bitOffsetWithinPixel, bits);
      channels.push(channelValue);
      bitOffsetWithinPixel += bits;
    }
  }
  return [channels[0], channels[1], channels[2], channels[3]];
}
function writePixel(dataView, bitOffset, bitsPerChannel, pixel) {
  let currentBitOffset = bitOffset;
  for (let channel = 0; channel < 4; channel++) {
    const bits = bitsPerChannel[channel];
    const maxValue = (1 << bits) - 1;
    const channelValue = pixel[channel] & maxValue;
    writeBitsToDataView(dataView, currentBitOffset, bits, channelValue);
    currentBitOffset += bits;
  }
}
function readBitsFromDataView(dataView, bitOffset, bitCount) {
  if (bitOffset % 8 === 0) {
    const byteOffset = bitOffset / 8;
    if (bitCount === 8 && byteOffset + 1 <= dataView.byteLength) {
      return dataView.getUint8(byteOffset);
    } else if (bitCount === 16 && byteOffset + 2 <= dataView.byteLength) {
      return dataView.getUint16(byteOffset, false);
    } else if (bitCount === 32 && byteOffset + 4 <= dataView.byteLength) {
      return dataView.getUint32(byteOffset, false);
    }
  }
  let value = 0;
  for (let i = 0; i < bitCount; i++) {
    const overallBitIndex = bitOffset + i;
    const byteIndex = Math.floor(overallBitIndex / 8);
    const bitIndex = overallBitIndex % 8;
    const byteValue = dataView.getUint8(byteIndex);
    const bit = byteValue >> 7 - bitIndex & 1;
    value = value << 1 | bit;
  }
  return value;
}
function writeBitsToDataView(dataView, bitOffset, bitCount, value) {
  if (bitOffset % 8 === 0) {
    const byteOffset = bitOffset / 8;
    if (bitCount === 8 && byteOffset + 1 <= dataView.byteLength) {
      dataView.setUint8(byteOffset, value & 255);
      return;
    } else if (bitCount === 16 && byteOffset + 2 <= dataView.byteLength) {
      dataView.setUint16(byteOffset, value & 65535, false);
      return;
    } else if (bitCount === 32 && byteOffset + 4 <= dataView.byteLength) {
      dataView.setUint32(byteOffset, value, false);
      return;
    }
  }
  for (let i = 0; i < bitCount; i++) {
    const overallBitIndex = bitOffset + i;
    const byteIndex = Math.floor(overallBitIndex / 8);
    const bitIndex = overallBitIndex % 8;
    const mask = 1 << 7 - bitIndex;
    const bitValue = value >> bitCount - 1 - i & 1;
    let currentByte = dataView.getUint8(byteIndex);
    currentByte &= ~mask;
    if (bitValue) {
      currentByte |= mask;
    }
    dataView.setUint8(byteIndex, currentByte);
  }
}
//# sourceMappingURL=index.cjs.map
