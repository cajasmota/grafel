var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __hasOwnProp = Object.prototype.hasOwnProperty;
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

// dist/index.js
var dist_exports = {};
__export(dist_exports, {
  VERSION: () => VERSION,
  assert: () => assert,
  console: () => console_,
  document: () => document_,
  getBrowser: () => getBrowser,
  global: () => global_,
  isBrowser: () => isBrowser,
  isElectron: () => isElectron,
  isMobile: () => isMobile,
  process: () => process_,
  self: () => global_,
  window: () => window_
});
module.exports = __toCommonJS(dist_exports);

// dist/lib/globals.js
var global_ = globalThis;
var window_ = globalThis;
var document_ = globalThis.document || {};
var process_ = globalThis.process || {};
var console_ = globalThis.console;
var navigator_ = globalThis.navigator || {};

// dist/lib/is-electron.js
function isElectron(mockUserAgent) {
  var _a, _b;
  if (typeof window !== "undefined" && ((_a = window.process) == null ? void 0 : _a.type) === "renderer") {
    return true;
  }
  if (typeof process !== "undefined" && Boolean((_b = process.versions) == null ? void 0 : _b["electron"])) {
    return true;
  }
  const realUserAgent = typeof navigator !== "undefined" && navigator.userAgent;
  const userAgent = mockUserAgent || realUserAgent;
  return Boolean(userAgent && userAgent.indexOf("Electron") >= 0);
}

// dist/lib/is-browser.js
function isBrowser() {
  const isNode = (
    // @ts-expect-error
    typeof process === "object" && String(process) === "[object process]" && !(process == null ? void 0 : process.browser)
  );
  return !isNode || isElectron();
}

// dist/lib/get-browser.js
function isMobile() {
  return typeof globalThis.orientation !== "undefined";
}
function getBrowser(mockUserAgent) {
  if (!mockUserAgent && !isBrowser()) {
    return "Node";
  }
  if (isElectron(mockUserAgent)) {
    return "Electron";
  }
  const userAgent = mockUserAgent || navigator_.userAgent || "";
  if (userAgent.indexOf("Edge") > -1) {
    return "Edge";
  }
  if (globalThis.chrome) {
    return "Chrome";
  }
  if (globalThis.safari) {
    return "Safari";
  }
  if (globalThis.mozInnerScreenX) {
    return "Firefox";
  }
  return "Unknown";
}

// dist/utils/assert.js
function assert(condition, message) {
  if (!condition) {
    throw new Error(message || "Assertion failed");
  }
}

// dist/index.js
var VERSION = true ? "4.1.1" : "untranspiled source";
//# sourceMappingURL=index.cjs.map
