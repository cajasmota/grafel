// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { GL } from '@luma.gl/constants';
import { convertCompareFunction } from "./device-parameters.js";
/**
 * Convert WebGPU-style sampler props to WebGL
 * @param props
 * @returns
 */
export function convertSamplerParametersToWebGL(props) {
    const params = {};
    if (props.addressModeU) {
        params[10242] = convertAddressMode(props.addressModeU);
    }
    if (props.addressModeV) {
        params[10243] = convertAddressMode(props.addressModeV);
    }
    if (props.addressModeW) {
        params[32882] = convertAddressMode(props.addressModeW);
    }
    if (props.magFilter) {
        params[10240] = convertMaxFilterMode(props.magFilter);
    }
    if (props.minFilter || props.mipmapFilter) {
        // TODO - arbitrary choice of linear?
        params[10241] = convertMinFilterMode(props.minFilter || 'linear', props.mipmapFilter);
    }
    if (props.lodMinClamp !== undefined) {
        params[33082] = props.lodMinClamp;
    }
    if (props.lodMaxClamp !== undefined) {
        params[33083] = props.lodMaxClamp;
    }
    if (props.type === 'comparison-sampler') {
        // Setting prop.compare turns this into a comparison sampler
        params[34892] = 34894;
    }
    if (props.compare) {
        params[34893] = convertCompareFunction('compare', props.compare);
    }
    // Note depends on WebGL extension
    if (props.maxAnisotropy) {
        params[34046] = props.maxAnisotropy;
    }
    return params;
}
// HELPERS
/** Convert address more */
function convertAddressMode(addressMode) {
    switch (addressMode) {
        case 'clamp-to-edge':
            return 33071;
        case 'repeat':
            return 10497;
        case 'mirror-repeat':
            return 33648;
    }
}
function convertMaxFilterMode(maxFilter) {
    switch (maxFilter) {
        case 'nearest':
            return 9728;
        case 'linear':
            return 9729;
    }
}
/**
 * WebGPU has separate min filter and mipmap filter,
 * WebGL is combined and effectively offers 6 options
 */
function convertMinFilterMode(minFilter, mipmapFilter = 'none') {
    if (!mipmapFilter) {
        return convertMaxFilterMode(minFilter);
    }
    switch (mipmapFilter) {
        case 'none':
            return convertMaxFilterMode(minFilter);
        case 'nearest':
            switch (minFilter) {
                case 'nearest':
                    return 9984;
                case 'linear':
                    return 9985;
            }
            break;
        case 'linear':
            switch (minFilter) {
                case 'nearest':
                    return 9986;
                case 'linear':
                    return 9987;
            }
    }
}
//# sourceMappingURL=sampler-parameters.js.map