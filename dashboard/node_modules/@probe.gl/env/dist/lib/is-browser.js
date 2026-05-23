// This function is needed in initialization stages,
// make sure it can be imported in isolation
import { isElectron } from "./is-electron.js";
/** Check if in browser by duck-typing Node context */
export function isBrowser() {
    const isNode = 
    // @ts-expect-error
    typeof process === 'object' && String(process) === '[object process]' && !process?.browser;
    return !isNode || isElectron();
}
//# sourceMappingURL=is-browser.js.map