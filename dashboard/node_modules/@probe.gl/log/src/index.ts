import {ProbeLog} from './loggers/probe-log';

// DEFAULT EXPORT IS A LOG INSTANCE
export default new ProbeLog({id: '@probe.gl/log'});

// LOGGING
export type {Logger} from './loggers/logger';
export {ProbeLog, ProbeLog as Log} from './loggers/probe-log';
export {ConsoleLog} from './loggers/console-log';
export {BaseLog} from './loggers/base-log';
export type {MemoryLogMessage} from './loggers/memory-log';
export {MemoryLog} from './loggers/memory-log';

// UTILITIES
export {COLOR} from './utils/color';
export {addColor} from './utils/color';
export {leftPad, rightPad} from './utils/formatters';
export {autobind} from './utils/autobind';
export {LocalStorage} from './utils/local-storage';
export {getHiResTimestamp} from './utils/hi-res-timestamp';

import './init';
