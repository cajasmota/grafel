// probe.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors

/* eslint-disable no-console */

import {LogFunction} from './logger';
import {BaseLog, NormalizedLogArguments, noop} from './base-log';

/**
 * Minimal console-backed logger. Does not persist configuration or implement log levels.
 * Intended as a lightweight Logger implementation.
 */
export class ConsoleLog extends BaseLog {
  override warn(message: string, ...args: unknown[]): LogFunction {
    return this._log('warn', 0, message, args, {
      method: console.warn || console.log,
      once: true
    });
  }

  override error(message: string, ...args: unknown[]): LogFunction {
    return this._log('error', 0, message, args, {method: console.error});
  }

  override log(logLevel, message?, ...args: unknown[]): LogFunction {
    return this._log('log', logLevel, message, args, {
      method: console.debug || console.log
    });
  }

  override info(logLevel, message?, ...args: unknown[]): LogFunction {
    return this._log('info', logLevel, message, args, {
      method: console.info || console.log
    });
  }

  override once(logLevel, message?, ...args: unknown[]): LogFunction {
    return this._log('once', logLevel, message, args, {
      method: console.debug || console.info || console.log,
      once: true
    });
  }

  protected override _emit(_type: string, normalized: NormalizedLogArguments): LogFunction {
    const method = normalized.method;
    if (!method) {
      return noop;
    }
    return method.bind(console, normalized.message, ...normalized.args);
  }
}
