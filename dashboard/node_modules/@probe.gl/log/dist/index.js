import { ProbeLog } from "./loggers/probe-log.js";
// DEFAULT EXPORT IS A LOG INSTANCE
export default new ProbeLog({ id: '@probe.gl/log' });
export { ProbeLog, ProbeLog as Log } from "./loggers/probe-log.js";
export { ConsoleLog } from "./loggers/console-log.js";
export { BaseLog } from "./loggers/base-log.js";
export { MemoryLog } from "./loggers/memory-log.js";
// UTILITIES
export { COLOR } from "./utils/color.js";
export { addColor } from "./utils/color.js";
export { leftPad, rightPad } from "./utils/formatters.js";
export { autobind } from "./utils/autobind.js";
export { LocalStorage } from "./utils/local-storage.js";
export { getHiResTimestamp } from "./utils/hi-res-timestamp.js";
import "./init.js";
//# sourceMappingURL=index.js.map