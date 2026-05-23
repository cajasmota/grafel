// Do not name these variables the same as the global objects - will break bundling
const global_ = globalThis;
const window_ = globalThis;
const document_ = globalThis.document || {};
const process_ = globalThis.process || {};
const console_ = globalThis.console;
const navigator_ = globalThis.navigator || {};
export { global_ as global, global_ as self, window_ as window, document_ as document, process_ as process, console_ as console, navigator_ as navigator };
//# sourceMappingURL=globals.js.map