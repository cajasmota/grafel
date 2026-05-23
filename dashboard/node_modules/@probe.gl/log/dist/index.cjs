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
  BaseLog: () => BaseLog,
  COLOR: () => COLOR,
  ConsoleLog: () => ConsoleLog,
  LocalStorage: () => LocalStorage,
  Log: () => ProbeLog,
  MemoryLog: () => MemoryLog,
  ProbeLog: () => ProbeLog,
  addColor: () => addColor,
  autobind: () => autobind,
  default: () => dist_default,
  getHiResTimestamp: () => getHiResTimestamp,
  leftPad: () => leftPad,
  rightPad: () => rightPad
});
module.exports = __toCommonJS(dist_exports);

// dist/loggers/probe-log.js
var import_env3 = require("@probe.gl/env");

// dist/utils/assert.js
function assert(condition, message) {
  if (!condition) {
    throw new Error(message || "Assertion failed");
  }
}

// dist/loggers/log-utils.js
function normalizeLogLevel(logLevel) {
  if (!logLevel) {
    return 0;
  }
  let resolvedLevel;
  switch (typeof logLevel) {
    case "number":
      resolvedLevel = logLevel;
      break;
    case "object":
      resolvedLevel = logLevel.logLevel || logLevel.priority || 0;
      break;
    default:
      return 0;
  }
  assert(Number.isFinite(resolvedLevel) && resolvedLevel >= 0);
  return resolvedLevel;
}
function normalizeArguments(opts) {
  const { logLevel, message } = opts;
  opts.logLevel = normalizeLogLevel(logLevel);
  const args = opts.args ? Array.from(opts.args) : [];
  while (args.length && args.shift() !== message) {
  }
  switch (typeof logLevel) {
    case "string":
    case "function":
      if (message !== void 0) {
        args.unshift(message);
      }
      opts.message = logLevel;
      break;
    case "object":
      Object.assign(opts, logLevel);
      break;
    default:
  }
  if (typeof opts.message === "function") {
    opts.message = opts.message();
  }
  const messageType = typeof opts.message;
  assert(messageType === "string" || messageType === "object");
  return Object.assign(opts, { args }, opts.opts);
}

// dist/loggers/base-log.js
var noop = () => {
};
var BaseLog = class {
  constructor({ level = 0 } = {}) {
    this.userData = {};
    this._onceCache = /* @__PURE__ */ new Set();
    this._level = level;
  }
  set level(newLevel) {
    this.setLevel(newLevel);
  }
  get level() {
    return this.getLevel();
  }
  setLevel(level) {
    this._level = level;
    return this;
  }
  getLevel() {
    return this._level;
  }
  // Unconditional logging
  warn(message, ...args) {
    return this._log("warn", 0, message, args, { once: true });
  }
  error(message, ...args) {
    return this._log("error", 0, message, args);
  }
  // Conditional logging
  log(logLevel, message, ...args) {
    return this._log("log", logLevel, message, args);
  }
  info(logLevel, message, ...args) {
    return this._log("info", logLevel, message, args);
  }
  once(logLevel, message, ...args) {
    return this._log("once", logLevel, message, args, { once: true });
  }
  _log(type, logLevel, message, args, options = {}) {
    const normalized = normalizeArguments({
      logLevel,
      message,
      args: this._buildArgs(logLevel, message, args),
      opts: options
    });
    return this._createLogFunction(type, normalized, options);
  }
  _buildArgs(logLevel, message, args) {
    return [logLevel, message, ...args];
  }
  _createLogFunction(type, normalized, options) {
    if (!this._shouldLog(normalized.logLevel)) {
      return noop;
    }
    const tag = this._getOnceTag(options.tag ?? normalized.tag ?? normalized.message);
    if ((options.once || normalized.once) && tag !== void 0) {
      if (this._onceCache.has(tag)) {
        return noop;
      }
      this._onceCache.add(tag);
    }
    return this._emit(type, normalized);
  }
  _shouldLog(logLevel) {
    return this.getLevel() >= normalizeLogLevel(logLevel);
  }
  _getOnceTag(tag) {
    if (tag === void 0) {
      return void 0;
    }
    try {
      return typeof tag === "string" ? tag : String(tag);
    } catch {
      return void 0;
    }
  }
};

// dist/utils/local-storage.js
function getStorage(type) {
  try {
    const storage = window[type];
    const x = "__storage_test__";
    storage.setItem(x, x);
    storage.removeItem(x);
    return storage;
  } catch (e) {
    return null;
  }
}
var LocalStorage = class {
  constructor(id, defaultConfig, type = "sessionStorage") {
    this.storage = getStorage(type);
    this.id = id;
    this.config = defaultConfig;
    this._loadConfiguration();
  }
  getConfiguration() {
    return this.config;
  }
  setConfiguration(configuration) {
    Object.assign(this.config, configuration);
    if (this.storage) {
      const serialized = JSON.stringify(this.config);
      this.storage.setItem(this.id, serialized);
    }
  }
  // Get config from persistent store, if available
  _loadConfiguration() {
    let configuration = {};
    if (this.storage) {
      const serializedConfiguration = this.storage.getItem(this.id);
      configuration = serializedConfiguration ? JSON.parse(serializedConfiguration) : {};
    }
    Object.assign(this.config, configuration);
    return this;
  }
};

// dist/utils/formatters.js
function formatTime(ms) {
  let formatted;
  if (ms < 10) {
    formatted = `${ms.toFixed(2)}ms`;
  } else if (ms < 100) {
    formatted = `${ms.toFixed(1)}ms`;
  } else if (ms < 1e3) {
    formatted = `${ms.toFixed(0)}ms`;
  } else {
    formatted = `${(ms / 1e3).toFixed(2)}s`;
  }
  return formatted;
}
function leftPad(string, length = 8) {
  const padLength = Math.max(length - string.length, 0);
  return `${" ".repeat(padLength)}${string}`;
}
function rightPad(string, length = 8) {
  const padLength = Math.max(length - string.length, 0);
  return `${string}${" ".repeat(padLength)}`;
}

// dist/utils/color.js
var import_env = require("@probe.gl/env");
var COLOR;
(function(COLOR2) {
  COLOR2[COLOR2["BLACK"] = 30] = "BLACK";
  COLOR2[COLOR2["RED"] = 31] = "RED";
  COLOR2[COLOR2["GREEN"] = 32] = "GREEN";
  COLOR2[COLOR2["YELLOW"] = 33] = "YELLOW";
  COLOR2[COLOR2["BLUE"] = 34] = "BLUE";
  COLOR2[COLOR2["MAGENTA"] = 35] = "MAGENTA";
  COLOR2[COLOR2["CYAN"] = 36] = "CYAN";
  COLOR2[COLOR2["WHITE"] = 37] = "WHITE";
  COLOR2[COLOR2["BRIGHT_BLACK"] = 90] = "BRIGHT_BLACK";
  COLOR2[COLOR2["BRIGHT_RED"] = 91] = "BRIGHT_RED";
  COLOR2[COLOR2["BRIGHT_GREEN"] = 92] = "BRIGHT_GREEN";
  COLOR2[COLOR2["BRIGHT_YELLOW"] = 93] = "BRIGHT_YELLOW";
  COLOR2[COLOR2["BRIGHT_BLUE"] = 94] = "BRIGHT_BLUE";
  COLOR2[COLOR2["BRIGHT_MAGENTA"] = 95] = "BRIGHT_MAGENTA";
  COLOR2[COLOR2["BRIGHT_CYAN"] = 96] = "BRIGHT_CYAN";
  COLOR2[COLOR2["BRIGHT_WHITE"] = 97] = "BRIGHT_WHITE";
})(COLOR || (COLOR = {}));
var BACKGROUND_INCREMENT = 10;
function getColor(color) {
  if (typeof color !== "string") {
    return color;
  }
  color = color.toUpperCase();
  return COLOR[color] || COLOR.WHITE;
}
function addColor(string, color, background) {
  if (!import_env.isBrowser && typeof string === "string") {
    if (color) {
      const colorCode = getColor(color);
      string = `\x1B[${colorCode}m${string}\x1B[39m`;
    }
    if (background) {
      const colorCode = getColor(background);
      string = `\x1B[${colorCode + BACKGROUND_INCREMENT}m${string}\x1B[49m`;
    }
  }
  return string;
}

// dist/utils/autobind.js
function autobind(obj, predefined = ["constructor"]) {
  const proto = Object.getPrototypeOf(obj);
  const propNames = Object.getOwnPropertyNames(proto);
  const object = obj;
  for (const key of propNames) {
    const value = object[key];
    if (typeof value === "function") {
      if (!predefined.find((name) => key === name)) {
        object[key] = value.bind(obj);
      }
    }
  }
}

// dist/utils/hi-res-timestamp.js
var import_env2 = require("@probe.gl/env");
function getHiResTimestamp() {
  var _a, _b, _c, _d, _e;
  let timestamp;
  if ((0, import_env2.isBrowser)() && import_env2.window.performance) {
    timestamp = (_c = (_b = (_a = import_env2.window) == null ? void 0 : _a.performance) == null ? void 0 : _b.now) == null ? void 0 : _c.call(_b);
  } else if ("hrtime" in import_env2.process) {
    const timeParts = (_e = (_d = import_env2.process) == null ? void 0 : _d.hrtime) == null ? void 0 : _e.call(_d);
    timestamp = timeParts[0] * 1e3 + timeParts[1] / 1e6;
  } else {
    timestamp = Date.now();
  }
  return timestamp;
}

// dist/loggers/probe-log.js
var originalConsole = {
  debug: (0, import_env3.isBrowser)() ? console.debug || console.log : console.log,
  log: console.log,
  info: console.info,
  warn: console.warn,
  error: console.error
};
var DEFAULT_LOG_CONFIGURATION = {
  enabled: true,
  level: 0
};
var ProbeLog = class extends BaseLog {
  constructor({ id } = { id: "" }) {
    super({ level: 0 });
    this.VERSION = import_env3.VERSION;
    this._startTs = getHiResTimestamp();
    this._deltaTs = getHiResTimestamp();
    this.userData = {};
    this.LOG_THROTTLE_TIMEOUT = 0;
    this.id = id;
    this.userData = {};
    this._storage = new LocalStorage(`__probe-${this.id}__`, { [this.id]: DEFAULT_LOG_CONFIGURATION });
    this.timeStamp(`${this.id} started`);
    autobind(this);
    Object.seal(this);
  }
  isEnabled() {
    return this._getConfiguration().enabled;
  }
  getLevel() {
    return this._getConfiguration().level;
  }
  /** @return milliseconds, with fractions */
  getTotal() {
    return Number((getHiResTimestamp() - this._startTs).toPrecision(10));
  }
  /** @return milliseconds, with fractions */
  getDelta() {
    return Number((getHiResTimestamp() - this._deltaTs).toPrecision(10));
  }
  /** @deprecated use logLevel */
  set priority(newPriority) {
    this.level = newPriority;
  }
  /** @deprecated use logLevel */
  get priority() {
    return this.level;
  }
  /** @deprecated use logLevel */
  getPriority() {
    return this.level;
  }
  // Configure
  enable(enabled = true) {
    this._updateConfiguration({ enabled });
    return this;
  }
  setLevel(level) {
    this._updateConfiguration({ level });
    return this;
  }
  /** return the current status of the setting */
  get(setting) {
    return this._getConfiguration()[setting];
  }
  // update the status of the setting
  set(setting, value) {
    this._updateConfiguration({ [setting]: value });
  }
  /** Logs the current settings as a table */
  settings() {
    if (console.table) {
      console.table(this._storage.config);
    } else {
      console.log(this._storage.config);
    }
  }
  // Unconditional logging
  assert(condition, message) {
    if (!condition) {
      throw new Error(message || "Assertion failed");
    }
  }
  warn(message, ...args) {
    return this._log("warn", 0, message, args, {
      method: originalConsole.warn,
      once: true
    });
  }
  error(message, ...args) {
    return this._log("error", 0, message, args, {
      method: originalConsole.error
    });
  }
  /** Print a deprecation warning */
  deprecated(oldUsage, newUsage) {
    return this.warn(`\`${oldUsage}\` is deprecated and will be removed in a later version. Use \`${newUsage}\` instead`);
  }
  /** Print a removal warning */
  removed(oldUsage, newUsage) {
    return this.error(`\`${oldUsage}\` has been removed. Use \`${newUsage}\` instead`);
  }
  probe(logLevel, message, ...args) {
    return this._log("log", logLevel, message, args, {
      method: originalConsole.log,
      time: true,
      once: true
    });
  }
  log(logLevel, message, ...args) {
    return this._log("log", logLevel, message, args, {
      method: originalConsole.debug
    });
  }
  info(logLevel, message, ...args) {
    return this._log("info", logLevel, message, args, { method: console.info });
  }
  once(logLevel, message, ...args) {
    return this._log("once", logLevel, message, args, {
      method: originalConsole.debug || originalConsole.info,
      once: true
    });
  }
  /** Logs an object as a table */
  table(logLevel, table, columns) {
    if (table) {
      return this._log("table", logLevel, table, columns && [columns] || [], {
        method: console.table || noop,
        tag: getTableHeader(table)
      });
    }
    return noop;
  }
  time(logLevel, message) {
    return this._log("time", logLevel, message, [], {
      method: console.time ? console.time : console.info
    });
  }
  timeEnd(logLevel, message) {
    return this._log("time", logLevel, message, [], {
      method: console.timeEnd ? console.timeEnd : console.info
    });
  }
  timeStamp(logLevel, message) {
    return this._log("time", logLevel, message, [], {
      method: console.timeStamp || noop
    });
  }
  group(logLevel, message, opts = { collapsed: false }) {
    const method = (opts.collapsed ? console.groupCollapsed : console.group) || console.info;
    return this._log("group", logLevel, message, [], { method });
  }
  groupCollapsed(logLevel, message, opts = {}) {
    return this.group(logLevel, message, Object.assign({}, opts, { collapsed: true }));
  }
  groupEnd(logLevel) {
    return this._log("groupEnd", logLevel, "", [], {
      method: console.groupEnd || noop
    });
  }
  // EXPERIMENTAL
  withGroup(logLevel, message, func) {
    this.group(logLevel, message)();
    try {
      func();
    } finally {
      this.groupEnd(logLevel)();
    }
  }
  trace() {
    if (console.trace) {
      console.trace();
    }
  }
  _shouldLog(logLevel) {
    return this.isEnabled() && super._shouldLog(logLevel);
  }
  _emit(_type, normalized) {
    const method = normalized.method;
    assert(method);
    normalized.total = this.getTotal();
    normalized.delta = this.getDelta();
    this._deltaTs = getHiResTimestamp();
    const message = decorateMessage(this.id, normalized.message, normalized);
    return method.bind(console, message, ...normalized.args);
  }
  _getConfiguration() {
    if (!this._storage.config[this.id]) {
      this._updateConfiguration(DEFAULT_LOG_CONFIGURATION);
    }
    return this._storage.config[this.id];
  }
  _updateConfiguration(configuration) {
    const currentConfiguration = this._storage.config[this.id] || {
      ...DEFAULT_LOG_CONFIGURATION
    };
    this._storage.setConfiguration({
      [this.id]: { ...currentConfiguration, ...configuration }
    });
  }
};
ProbeLog.VERSION = import_env3.VERSION;
function decorateMessage(id, message, opts) {
  if (typeof message === "string") {
    const time = opts.time ? leftPad(formatTime(opts.total)) : "";
    message = opts.time ? `${id}: ${time}  ${message}` : `${id}: ${message}`;
    message = addColor(message, opts.color, opts.background);
  }
  return message;
}
function getTableHeader(table) {
  for (const key in table) {
    for (const title in table[key]) {
      return title || "untitled";
    }
  }
  return "empty";
}

// dist/loggers/console-log.js
var ConsoleLog = class extends BaseLog {
  warn(message, ...args) {
    return this._log("warn", 0, message, args, {
      method: console.warn || console.log,
      once: true
    });
  }
  error(message, ...args) {
    return this._log("error", 0, message, args, { method: console.error });
  }
  log(logLevel, message, ...args) {
    return this._log("log", logLevel, message, args, {
      method: console.debug || console.log
    });
  }
  info(logLevel, message, ...args) {
    return this._log("info", logLevel, message, args, {
      method: console.info || console.log
    });
  }
  once(logLevel, message, ...args) {
    return this._log("once", logLevel, message, args, {
      method: console.debug || console.info || console.log,
      once: true
    });
  }
  _emit(_type, normalized) {
    const method = normalized.method;
    if (!method) {
      return noop;
    }
    return method.bind(console, normalized.message, ...normalized.args);
  }
};

// dist/loggers/memory-log.js
var MemoryLog = class extends BaseLog {
  constructor(options = {}) {
    super({ level: options.level ?? 0 });
    this.userData = {};
    this.messages = [];
    this.onMessage = options.onMessage;
  }
  _emit(type, normalized) {
    const messageText = String(normalized.message);
    const entry = {
      type: this._normalizeType(type),
      level: normalized.logLevel,
      message: messageText,
      args: normalized.args
    };
    return () => {
      this.messages.push(entry);
      if (this.onMessage) {
        this.onMessage(entry);
      }
    };
  }
  _normalizeType(type) {
    switch (type) {
      case "warn":
        return "warning";
      case "error":
        return "error";
      case "info":
        return "info";
      case "once":
        return "once";
      case "table":
        return "table";
      default:
        return "log";
    }
  }
};

// dist/init.js
globalThis.probe = {};

// dist/index.js
var dist_default = new ProbeLog({ id: "@probe.gl/log" });
//# sourceMappingURL=index.cjs.map
