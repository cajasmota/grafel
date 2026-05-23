// probe.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import assert from "../utils/assert.js";
/**
 * Get logLevel from first argument:
 * - log(logLevel, message, args) => logLevel
 * - log(message, args) => 0
 * - log({logLevel, ...}, message, args) => logLevel
 * - log({logLevel, message, args}) => logLevel
 */
export function normalizeLogLevel(logLevel) {
    if (!logLevel) {
        return 0;
    }
    let resolvedLevel;
    switch (typeof logLevel) {
        case 'number':
            resolvedLevel = logLevel;
            break;
        case 'object':
            // Backward compatibility
            // TODO - deprecate `priority`
            // @ts-expect-error
            resolvedLevel = logLevel.logLevel || logLevel.priority || 0;
            break;
        default:
            return 0;
    }
    // 'log level must be a number'
    assert(Number.isFinite(resolvedLevel) && resolvedLevel >= 0);
    return resolvedLevel;
}
/**
 * "Normalizes" the various argument patterns into an object with known types
 * - log(logLevel, message, args) => {logLevel, message, args}
 * - log(message, args) => {logLevel: 0, message, args}
 * - log({logLevel, ...}, message, args) => {logLevel, message, args}
 * - log({logLevel, message, args}) => {logLevel, message, args}
 */
export function normalizeArguments(opts) {
    const { logLevel, message } = opts;
    opts.logLevel = normalizeLogLevel(logLevel);
    // We use `arguments` instead of rest parameters (...args) because IE
    // does not support the syntax. Rest parameters is transpiled to code with
    // perf impact. Doing it here instead avoids constructing args when logging is
    // disabled.
    // TODO - remove when/if IE support is dropped
    const args = opts.args ? Array.from(opts.args) : [];
    // args should only contain arguments that appear after `message`
    // eslint-disable-next-line no-empty
    while (args.length && args.shift() !== message) { }
    switch (typeof logLevel) {
        case 'string':
        case 'function':
            if (message !== undefined) {
                args.unshift(message);
            }
            opts.message = logLevel;
            break;
        case 'object':
            Object.assign(opts, logLevel);
            break;
        default:
    }
    // Resolve functions into strings by calling them
    if (typeof opts.message === 'function') {
        opts.message = opts.message();
    }
    const messageType = typeof opts.message;
    // 'log message must be a string' or object
    assert(messageType === 'string' || messageType === 'object');
    // original opts + normalized opts + opts arg + fixed up message
    return Object.assign(opts, { args }, opts.opts);
}
//# sourceMappingURL=log-utils.js.map