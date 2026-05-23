// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { GL } from '@luma.gl/constants';
// Returns number of components in a specific readPixels WebGL format
export function glFormatToComponents(format) {
    switch (format) {
        case 6406:
        case 33326:
        case 6403:
        case 36244:
            return 1;
        case 33339:
        case 33340:
        case 33328:
        case 33320:
        case 33319:
            return 2;
        case 6407:
        case 36248:
        case 34837:
            return 3;
        case 6408:
        case 36249:
        case 34836:
            return 4;
        // TODO: Add support for additional WebGL2 formats
        default:
            return 0;
    }
}
// Return byte count for given readPixels WebGL type
export function glTypeToBytes(type) {
    switch (type) {
        case 5121:
            return 1;
        case 33635:
        case 32819:
        case 32820:
            return 2;
        case 5126:
            return 4;
        // TODO: Add support for additional WebGL2 types
        default:
            return 0;
    }
}
//# sourceMappingURL=format-utils.js.map