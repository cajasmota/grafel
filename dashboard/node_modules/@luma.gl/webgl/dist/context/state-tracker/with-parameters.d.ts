import { GLParameters } from "../parameters/unified-parameter-api.js";
/**
 * Execute a function with a set of temporary WebGL parameter overrides
 * - Saves current "global" WebGL context settings
 * - Sets the supplies WebGL context parameters,
 * - Executes supplied function
 * - Restores parameters
 * - Returns the return value of the supplied function
 */
export declare function withGLParameters(gl: WebGL2RenderingContext, parameters: GLParameters & {
    nocatch?: boolean;
}, func: any): any;
//# sourceMappingURL=with-parameters.d.ts.map