// probe.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { normalizeArguments, normalizeLogLevel } from "./log-utils.js";
const noop = () => { };
/**
 * Base logger that implements log level handling and once de-duplication.
 * Concrete loggers implement `_emit` to perform actual output.
 */
export class BaseLog {
    constructor({ level = 0 } = {}) {
        this.userData = {};
        this._onceCache = new Set();
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
        return this._log('warn', 0, message, args, { once: true });
    }
    error(message, ...args) {
        return this._log('error', 0, message, args);
    }
    // Conditional logging
    log(logLevel, message, ...args) {
        return this._log('log', logLevel, message, args);
    }
    info(logLevel, message, ...args) {
        return this._log('info', logLevel, message, args);
    }
    once(logLevel, message, ...args) {
        return this._log('once', logLevel, message, args, { once: true });
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
        if ((options.once || normalized.once) && tag !== undefined) {
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
        if (tag === undefined) {
            return undefined;
        }
        try {
            return typeof tag === 'string' ? tag : String(tag);
        }
        catch {
            return undefined;
        }
    }
}
export { noop };
//# sourceMappingURL=base-log.js.map