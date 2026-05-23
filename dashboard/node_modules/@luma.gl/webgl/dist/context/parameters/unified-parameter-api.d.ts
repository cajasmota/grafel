import type { GLParameters } from '@luma.gl/constants';
export type { GLParameters };
/**
 * Sets any GL parameter regardless of function (gl.blendMode, ...)
 *
 * @note requires a `cache` object to be set on the context (gl.state.cache)
 * This object is used to fill in any missing values for composite setter functions
 */
export declare function setGLParameters(gl: WebGL2RenderingContext, parameters: GLParameters): void;
/**
 * Reads the entire WebGL state from a context

  // default to querying all parameters

  * @returns - a newly created map, with values keyed by GL parameters
 *
 * @note Copies the state from a context (gl.getParameter should not be overriden)
 * Reads the entire WebGL state from a context
 *
 * @note This can generates a huge amount of synchronous driver roundtrips and should be
 * considered a very slow operation, to be used only if/when a context already manipulated
 * by external code needs to be synchronized for the first time
 */
export declare function getGLParameters(gl: WebGL2RenderingContext, parameters?: keyof GLParameters | (keyof GLParameters)[] | GLParameters): GLParameters;
/**
 * Reset all parameters to a (almost) pure context state
 * @note viewport and scissor will be set to the values in GL_PARAMETER_DEFAULTS,
 * NOT the canvas size dimensions, so they will have to be properly set after
 * calling this function.
 */
export declare function resetGLParameters(gl: WebGL2RenderingContext): void;
//# sourceMappingURL=unified-parameter-api.d.ts.map