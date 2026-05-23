// probe.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { BaseLog, noop } from "./base-log.js";
/**
 * Minimal console-backed logger. Does not persist configuration or implement log levels.
 * Intended as a lightweight Logger implementation.
 */
export class ConsoleLog extends BaseLog {
    warn(message, ...args) {
        return this._log('warn', 0, message, args, {
            method: console.warn || console.log,
            once: true
        });
    }
    error(message, ...args) {
        return this._log('error', 0, message, args, { method: console.error });
    }
    log(logLevel, message, ...args) {
        return this._log('log', logLevel, message, args, {
            method: console.debug || console.log
        });
    }
    info(logLevel, message, ...args) {
        return this._log('info', logLevel, message, args, {
            method: console.info || console.log
        });
    }
    once(logLevel, message, ...args) {
        return this._log('once', logLevel, message, args, {
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
}
//# sourceMappingURL=console-log.js.map