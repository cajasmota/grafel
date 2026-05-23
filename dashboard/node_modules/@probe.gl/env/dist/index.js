// Extract injected version from package.json (injected by babel plugin)
// @ts-expect-error
export const VERSION = typeof "4.1.1" !== 'undefined' ? "4.1.1" : 'untranspiled source';
// ENVIRONMENT
export { self, window, global, document, process, console } from "./lib/globals.js";
export { isBrowser } from "./lib/is-browser.js";
export { getBrowser, isMobile } from "./lib/get-browser.js";
export { isElectron } from "./lib/is-electron.js";
// ENVIRONMENT'S ASSERT IS 5-15KB, SO WE PROVIDE OUR OWN
export { assert } from "./utils/assert.js";
// TODO - wish we could just export a constant
// export const isBrowser = checkIfBrowser();
//# sourceMappingURL=index.js.map