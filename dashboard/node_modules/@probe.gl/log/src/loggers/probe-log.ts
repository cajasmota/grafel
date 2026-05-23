// probe.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors

/* eslint-disable no-console,prefer-rest-params */
import {VERSION, isBrowser} from '@probe.gl/env';
import {LogFunction} from './logger';
import {BaseLog, NormalizedLogArguments, noop} from './base-log';
import {LocalStorage} from '../utils/local-storage';
import {formatTime, leftPad} from '../utils/formatters';
import {addColor} from '../utils/color';
import {autobind} from '../utils/autobind';
import assert from '../utils/assert';
import {getHiResTimestamp} from '../utils/hi-res-timestamp';

/** "Global" log configuration settings */
type ProbeLogConfiguration = {
  enabled?: boolean;
  level?: number;
  [key: string]: unknown;
};

type Table = Record<string, any>;

// Instrumentation in other packages may override console methods, so preserve them here
const originalConsole = {
  debug: isBrowser() ? console.debug || console.log : console.log,
  log: console.log,
  info: console.info,
  warn: console.warn,
  error: console.error
};

const DEFAULT_LOG_CONFIGURATION: Required<ProbeLogConfiguration> = {
  enabled: true,
  level: 0
};

/** A console wrapper */

export class ProbeLog extends BaseLog {
  static VERSION = VERSION;

  id: string;
  VERSION: string = VERSION;
  _startTs: number = getHiResTimestamp();
  _deltaTs: number = getHiResTimestamp();
  _storage: LocalStorage<Record<string, ProbeLogConfiguration>>;
  override userData = {};

  // TODO - fix support from throttling groups
  LOG_THROTTLE_TIMEOUT: number = 0; // Time before throttled messages are logged again

  constructor({id} = {id: ''}) {
    super({level: 0});
    this.id = id;
    this.userData = {};
    this._storage = new LocalStorage<Record<string, ProbeLogConfiguration>>(
      `__probe-${this.id}__`,
      {[this.id]: DEFAULT_LOG_CONFIGURATION}
    );

    this.timeStamp(`${this.id} started`);

    autobind(this);
    Object.seal(this);
  }

  isEnabled(): boolean {
    return this._getConfiguration().enabled;
  }

  override getLevel(): number {
    return this._getConfiguration().level;
  }

  /** @return milliseconds, with fractions */
  getTotal(): number {
    return Number((getHiResTimestamp() - this._startTs).toPrecision(10));
  }

  /** @return milliseconds, with fractions */
  getDelta(): number {
    return Number((getHiResTimestamp() - this._deltaTs).toPrecision(10));
  }

  /** @deprecated use logLevel */
  set priority(newPriority: number) {
    this.level = newPriority;
  }

  /** @deprecated use logLevel */
  get priority(): number {
    return this.level;
  }

  /** @deprecated use logLevel */
  getPriority(): number {
    return this.level;
  }

  // Configure

  enable(enabled: boolean = true): this {
    this._updateConfiguration({enabled});
    return this;
  }

  override setLevel(level: number): this {
    this._updateConfiguration({level});
    return this;
  }

  /** return the current status of the setting */
  get(setting: string): any {
    return this._getConfiguration()[setting];
  }

  // update the status of the setting
  set(setting: string, value: any): void {
    this._updateConfiguration({[setting]: value});
  }

  /** Logs the current settings as a table */
  settings(): void {
    if (console.table) {
      console.table(this._storage.config);
    } else {
      console.log(this._storage.config);
    }
  }

  // Unconditional logging

  assert(condition: unknown, message?: string): asserts condition {
    if (!condition) {
      throw new Error(message || 'Assertion failed');
    }
  }

  /** Warn, but only once, no console flooding */
  override warn(message: string, ...args: unknown[]): LogFunction;
  override warn(message: string, ...args: unknown[]): LogFunction {
    return this._log('warn', 0, message, args, {
      method: originalConsole.warn,
      once: true
    });
  }

  /** Print an error */
  override error(message: string, ...args: unknown[]): LogFunction;
  override error(message: string, ...args: unknown[]): LogFunction {
    return this._log('error', 0, message, args, {
      method: originalConsole.error
    });
  }

  /** Print a deprecation warning */
  deprecated(oldUsage: string, newUsage: string): LogFunction {
    return this.warn(`\`${oldUsage}\` is deprecated and will be removed \
in a later version. Use \`${newUsage}\` instead`);
  }

  /** Print a removal warning */
  removed(oldUsage: string, newUsage: string): LogFunction {
    return this.error(`\`${oldUsage}\` has been removed. Use \`${newUsage}\` instead`);
  }

  // Conditional logging

  /** Log to a group */
  probe(logLevel, message?, ...args: unknown[]): LogFunction;
  probe(logLevel, message?, ...args: unknown[]): LogFunction {
    return this._log('log', logLevel, message, args, {
      method: originalConsole.log,
      time: true,
      once: true
    });
  }

  /** Log a debug message */
  override log(logLevel, message?, ...args: unknown[]): LogFunction;
  override log(logLevel, message?, ...args: unknown[]): LogFunction {
    return this._log('log', logLevel, message, args, {
      method: originalConsole.debug
    });
  }

  /** Log a normal message */
  override info(logLevel, message?, ...args: unknown[]): LogFunction;
  override info(logLevel, message?, ...args: unknown[]): LogFunction {
    return this._log('info', logLevel, message, args, {method: console.info});
  }

  /** Log a normal message, but only once, no console flooding */
  override once(logLevel, message?, ...args: unknown[]): LogFunction;
  override once(logLevel, message?, ...args: unknown[]): LogFunction {
    return this._log('once', logLevel, message, args, {
      method: originalConsole.debug || originalConsole.info,
      once: true
    });
  }

  /** Logs an object as a table */
  table(logLevel, table?, columns?): LogFunction {
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

  timeStamp(logLevel, message?) {
    return this._log('time', logLevel, message, [], {
      method: console.timeStamp || noop
    });
  }

  group(logLevel, message, opts = {collapsed: false}) {
    const method = (opts.collapsed ? console.groupCollapsed : console.group) || console.info;
    return this._log('group', logLevel, message, [], {method});
  }

  groupCollapsed(logLevel, message, opts = {}) {
    return this.group(logLevel, message, Object.assign({}, opts, {collapsed: true}));
  }

  groupEnd(logLevel) {
    return this._log('groupEnd', logLevel, '', [], {
      method: console.groupEnd || noop
    });
  }

  // EXPERIMENTAL

  withGroup(logLevel: number, message: string, func: Function): void {
    this.group(logLevel, message)();

    try {
      func();
    } finally {
      this.groupEnd(logLevel)();
    }
  }

  trace(): void {
    if (console.trace) {
      console.trace();
    }
  }

  protected override _shouldLog(logLevel: unknown): boolean {
    return this.isEnabled() && super._shouldLog(logLevel);
  }

  protected override _emit(_type: string, normalized: NormalizedLogArguments): LogFunction {
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

  _getConfiguration(): Required<ProbeLogConfiguration> {
    if (!this._storage.config[this.id]) {
      this._updateConfiguration(DEFAULT_LOG_CONFIGURATION);
    }

    // @ts-expect-error guaranteed to be defined
    return this._storage.config[this.id];
  }

  _updateConfiguration(configuration: ProbeLogConfiguration): void {
    const currentConfiguration = this._storage.config[this.id] || {
      ...DEFAULT_LOG_CONFIGURATION
    };
    this._storage.setConfiguration({
      [this.id]: {...currentConfiguration, ...configuration}
    });
  }
}

function decorateMessage(id, message, opts) {
  if (typeof message === 'string') {
    const time = opts.time ? leftPad(formatTime(opts.total)) : '';
    message = opts.time ? `${id}: ${time}  ${message}` : `${id}: ${message}`;
    message = addColor(message, opts.color, opts.background);
  }
  return message;
}

function getTableHeader(table: Table): string {
  for (const key in table) {
    for (const title in table[key]) {
      return title || 'untitled';
    }
  }
  return 'empty';
}

export {normalizeArguments} from './log-utils';
export {normalizeLogLevel} from './log-utils';
