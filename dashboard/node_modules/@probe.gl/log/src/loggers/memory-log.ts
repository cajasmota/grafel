// probe.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors

import {LogFunction} from './logger';
import {BaseLog, NormalizedLogArguments} from './base-log';

export type MemoryLogMessage = {
  level: number;
  type: 'warning' | 'error' | 'log' | 'info' | 'once' | 'table';
  message: string;
  args: unknown[];
};

export class MemoryLog extends BaseLog {
  override userData: Record<string, unknown> = {};

  messages: MemoryLogMessage[] = [];
  onMessage?: ((message: MemoryLogMessage) => void) | undefined;

  constructor(
    options: {
      onMessage?: (message: MemoryLogMessage) => void;
      level?: number;
    } = {}
  ) {
    super({level: options.level ?? 0});
    this.onMessage = options.onMessage;
  }

  protected override _emit(type: string, normalized: NormalizedLogArguments): LogFunction {
    const messageText = String(normalized.message);
    const entry: MemoryLogMessage = {
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

  private _normalizeType(type: string): MemoryLogMessage['type'] {
    switch (type) {
      case 'warn':
        return 'warning';
      case 'error':
        return 'error';
      case 'info':
        return 'info';
      case 'once':
        return 'once';
      case 'table':
        return 'table';
      default:
        return 'log';
    }
  }
}
