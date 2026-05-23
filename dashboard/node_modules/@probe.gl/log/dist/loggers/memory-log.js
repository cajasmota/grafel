// probe.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { BaseLog } from "./base-log.js";
export class MemoryLog extends BaseLog {
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
//# sourceMappingURL=memory-log.js.map