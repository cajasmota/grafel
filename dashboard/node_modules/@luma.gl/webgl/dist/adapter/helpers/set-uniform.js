// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { GL } from '@luma.gl/constants';
/** Set a raw uniform (without type conversion and caching) */
/* eslint-disable max-len */
export function setUniform(gl, location, type, value) {
    const gl2 = gl;
    // Prepare the value for WebGL setters
    let uniformValue = value;
    if (uniformValue === true) {
        uniformValue = 1;
    }
    if (uniformValue === false) {
        uniformValue = 0;
    }
    const arrayValue = typeof uniformValue === 'number' ? [uniformValue] : uniformValue;
    // prettier-ignore
    switch (type) {
        case 35678:
        case 35680:
        case 35679:
        case 35682:
        case 36289:
        case 36292:
        case 36293:
        case 36298:
        case 36299:
        case 36300:
        case 36303:
        case 36306:
        case 36307:
        case 36308:
        case 36311:
            if (typeof value !== 'number') {
                throw new Error('samplers must be set to integers');
            }
            return gl.uniform1i(location, value);
        case 5126: return gl.uniform1fv(location, arrayValue);
        case 35664: return gl.uniform2fv(location, arrayValue);
        case 35665: return gl.uniform3fv(location, arrayValue);
        case 35666: return gl.uniform4fv(location, arrayValue);
        case 5124: return gl.uniform1iv(location, arrayValue);
        case 35667: return gl.uniform2iv(location, arrayValue);
        case 35668: return gl.uniform3iv(location, arrayValue);
        case 35669: return gl.uniform4iv(location, arrayValue);
        case 35670: return gl.uniform1iv(location, arrayValue);
        case 35671: return gl.uniform2iv(location, arrayValue);
        case 35672: return gl.uniform3iv(location, arrayValue);
        case 35673: return gl.uniform4iv(location, arrayValue);
        // WEBGL2 - unsigned integers
        case 5125: return gl2.uniform1uiv(location, arrayValue, 1);
        case 36294: return gl2.uniform2uiv(location, arrayValue, 2);
        case 36295: return gl2.uniform3uiv(location, arrayValue, 3);
        case 36296: return gl2.uniform4uiv(location, arrayValue, 4);
        // WebGL2 - quadratic matrices
        // false: don't transpose the matrix
        case 35674: return gl.uniformMatrix2fv(location, false, arrayValue);
        case 35675: return gl.uniformMatrix3fv(location, false, arrayValue);
        case 35676: return gl.uniformMatrix4fv(location, false, arrayValue);
        // WebGL2 - rectangular matrices
        case 35685: return gl2.uniformMatrix2x3fv(location, false, arrayValue);
        case 35686: return gl2.uniformMatrix2x4fv(location, false, arrayValue);
        case 35687: return gl2.uniformMatrix3x2fv(location, false, arrayValue);
        case 35688: return gl2.uniformMatrix3x4fv(location, false, arrayValue);
        case 35689: return gl2.uniformMatrix4x2fv(location, false, arrayValue);
        case 35690: return gl2.uniformMatrix4x3fv(location, false, arrayValue);
    }
    throw new Error('Illegal uniform');
}
//# sourceMappingURL=set-uniform.js.map