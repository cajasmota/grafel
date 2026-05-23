// probe.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors

import {Logger, LogFunction} from './logger';
import {NormalizedArguments, normalizeArguments, normalizeLogLevel} from './log-utils';

const noop = () => {};

export type NormalizedLogArguments = NormalizedArguments;
type LogType = 'log' | 'info' | 'once' | 'warn' | 'error' | 'table' | 'group' | 'groupEnd' | 'time';

type LogOptions = Partial<NormalizedLogArguments>;

/**
 * Base logger that implements log level handling and once de-duplication.
 * Concrete loggers implement `_emit` to perform actual output.
 */
export abstract class BaseLog implements Logger {
  userData: Record<string, unknown> = {};

  protected _level: number;
  protected _onceCache = new Set<unknown>();

  constructor({level = 0}: {level?: number} = {}) {
    this._level = level;
  }

  set level(newLevel: number) {
    this.setLevel(newLevel);
  }

  get level(): number {
    return this.getLevel();
  }

  setLevel(level: number): this {
    this._level = level;
    return this;
  }

  getLevel(): number {
    return this._level;
  }

  // Unconditional logging

  warn(message: string, ...args: unknown[]): LogFunction {
    return this._log('warn', 0, message, args, {once: true});
  }

  error(message: string, ...args: unknown[]): LogFunction {
    return this._log('error', 0, message, args);
  }

  // Conditional logging

  log(logLevel, message?, ...args: unknown[]): LogFunction {
    return this._log('log', logLevel, message, args);
  }

  info(logLevel, message?, ...args: unknown[]): LogFunction {
    return this._log('info', logLevel, message, args);
  }

  once(logLevel, message?, ...args: unknown[]): LogFunction {
    return this._log('once', logLevel, message, args, {once: true});
  }

  protected _log(
    type: LogType,
    logLevel: unknown,
    message: unknown,
    args: unknown[],
    options: LogOptions = {}
  ): LogFunction {
    const normalized = normalizeArguments({
      logLevel,
      message,
      args: this._buildArgs(logLevel, message, args),
      opts: options
    });

    return this._createLogFunction(type, normalized, options);
  }

  protected _buildArgs(logLevel: unknown, message: unknown, args: unknown[]): unknown[] {
    return [logLevel, message, ...args];
  }

  protected _createLogFunction(
    type: LogType,
    normalized: NormalizedLogArguments,
    options: LogOptions
  ): LogFunction {
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

  protected _shouldLog(logLevel: unknown): boolean {
    return this.getLevel() >= normalizeLogLevel(logLevel);
  }

  protected _getOnceTag(tag: unknown): unknown {
    if (tag === undefined) {
      return undefined;
    }
    try {
      return typeof tag === 'string' ? tag : String(tag);
    } catch {
      return undefined;
    }
  }

  /** Create the actual log function for this logger implementation. */
  protected abstract _emit(type: LogType, normalized: NormalizedLogArguments): LogFunction;
}

export {noop};
