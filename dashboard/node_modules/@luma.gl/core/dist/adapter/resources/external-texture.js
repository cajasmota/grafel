// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { Resource } from "./resource.js";
export class ExternalTexture extends Resource {
    get [Symbol.toStringTag]() {
        return 'ExternalTexture';
    }
    constructor(device, props) {
        super(device, props, ExternalTexture.defaultProps);
    }
    static defaultProps = {
        ...Resource.defaultProps,
        source: undefined,
        colorSpace: 'srgb'
    };
}
//# sourceMappingURL=external-texture.js.map