// probe.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
/* eslint-disable no-console,prefer-rest-params */
import { VERSION, isBrowser } from '@probe.gl/env';
import { BaseLog, noop } from "./base-log.js";
import { LocalStorage } from "../utils/local-storage.js";
import { formatTime, leftPad } from "../utils/formatters.js";
import { addColor } from "../utils/color.js";
import { autobind } from "../utils/autobind.js";
import assert from "../utils/assert.js";
import { getHiResTimestamp } from "../utils/hi-res-timestamp.js";
// Instrumentation in other packages may override console methods, so preserve them here
const originalConsole = {
    debug: isBrowser() ? console.debug || console.log : console.log,
    log: console.log,
    info: console.info,
    warn: console.warn,
    error: console.error
};
const DEFAULT_LOG_CONFIGURATION = {
    enabled: true,
    level: 0
};
/** A console wrapper */
export class ProbeLog extends BaseLog {
    constructor({ id } = { id: '' }) {
        super({ level: 0 });
        this.VERSION = VERSION;
        this._startTs = getHiResTimestamp();
        this._deltaTs = getHiResTimestamp();
        this.userData = {};
        // TODO - fix support from throttling groups
        this.LOG_THROTTLE_TIMEOUT = 0; // Time before throttled messages are logged again
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
        }
        else {
            console.log(this._storage.config);
        }
    }
    // Unconditional logging
    assert(condition, message) {
        if (!condition) {
            throw new Error(message || 'Assertion failed');
        }
    }
    warn(message, ...args) {
        return this._log('warn', 0, message, args, {
            method: originalConsole.warn,
            once: true
        });
    }
    error(message, ...args) {
        return this._log('error', 0, message, args, {
            method: originalConsole.error
        });
    }
    /** Print a deprecation warning */
    deprecated(oldUsage, newUsage) {
        return this.warn(`\`${oldUsage}\` is deprecated and will be removed \
in a later version. Use \`${newUsage}\` instead`);
    }
    /** Print a removal warning */
    removed(oldUsage, newUsage) {
        return this.error(`\`${oldUsage}\` has been removed. Use \`${newUsage}\` instead`);
    }
    probe(logLevel, message, ...args) {
        return this._log('log', logLevel, message, args, {
            method: originalConsole.log,
            time: true,
            once: true
        });
    }
    log(logLevel, message, ...args) {
        return this._log('log', logLevel, message, args, {
            method: originalConsole.debug
        });
    }
    info(logLevel, message, ...args) {
        return this._log('info', logLevel, message, args, { method: console.info });
    }
    once(logLevel, message, ...args) {
        return this._log('once', logLevel, message, args, {
            method: originalConsole.debug || originalConsole.info,
            once: true
        });
    }
    /** Logs an object as a table */
    table(logLevel, table, columns) {
        if (table) {
            return this._log('table', logLevel, table, (columns && [columns]) || [], {
                method: console.table || noop,
                tag: getTableHeader(table)
            });
        }
        return noop;
    }
    time(logLevel, message) {
        return this._log('time', logLevel, message, [], {
            method: console.time ? console.time : console.info
        });
    }
    timeEnd(logLevel, message) {
        return this._log('time', logLevel, message, [], {
            method: console.timeEnd ? console.timeEnd : console.info
        });
    }
    timeStamp(logLevel, message) {
        return this._log('time', logLevel, message, [], {
            method: console.timeStamp || noop
        });
    }
    group(logLevel, message, opts = { collapsed: false }) {
        const method = (opts.collapsed ? console.groupCollapsed : console.group) || console.info;
        return this._log('group', logLevel, message, [], { method });
    }
    groupCollapsed(logLevel, message, opts = {}) {
        return this.group(logLevel, message, Object.assign({}, opts, { collapsed: true }));
    }
    groupEnd(logLevel) {
        return this._log('groupEnd', logLevel, '', [], {
            method: console.groupEnd || noop
        });
    }
    // EXPERIMENTAL
    withGroup(logLevel, message, func) {
        this.group(logLevel, message)();
        try {
            func();
        }
        finally {
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
        // reset delta timer
        this._deltaTs = getHiResTimestamp();
        const message = decorateMessage(this.id, normalized.message, normalized);
        // Bind console function so that it can be called after being returned
        return method.bind(console, message, ...normalized.args);
    }
    _getConfiguration() {
        if (!this._storage.config[this.id]) {
            this._updateConfiguration(DEFAULT_LOG_CONFIGURATION);
        }
        // @ts-expect-error guaranteed to be defined
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
}
ProbeLog.VERSION = VERSION;
function decorateMessage(id, message, opts) {
    if (typeof message === 'string') {
        const time = opts.time ? leftPad(formatTime(opts.total)) : '';
        message = opts.time ? `${id}: ${time}  ${message}` : `${id}: ${message}`;
        message = addColor(message, opts.color, opts.background);
    }
    return message;
}
function getTableHeader(table) {
    for (const key in table) {
        for (const title in table[key]) {
            return title || 'untitled';
        }
    }
    return 'empty';
}
export { normalizeArguments } from "./log-utils.js";
export { normalizeLogLevel } from "./log-utils.js";
//# sourceMappingURL=probe-log.js.map