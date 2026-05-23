// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { DeviceLimits } from '@luma.gl/core';
import { GL } from '@luma.gl/constants';
// prettier-ignore
export class WebGLDeviceLimits extends DeviceLimits {
    get maxTextureDimension1D() { return 0; } // WebGL does not support 1D textures
    get maxTextureDimension2D() { return this.getParameter(3379); }
    get maxTextureDimension3D() { return this.getParameter(32883); }
    get maxTextureArrayLayers() { return this.getParameter(35071); }
    get maxBindGroups() { return 0; }
    get maxDynamicUniformBuffersPerPipelineLayout() { return 0; } // TBD
    get maxDynamicStorageBuffersPerPipelineLayout() { return 0; } // TBD
    get maxSampledTexturesPerShaderStage() { return this.getParameter(35660); } // ) TBD
    get maxSamplersPerShaderStage() { return this.getParameter(35661); }
    get maxStorageBuffersPerShaderStage() { return 0; } // TBD
    get maxStorageTexturesPerShaderStage() { return 0; } // TBD
    get maxUniformBuffersPerShaderStage() { return this.getParameter(35375); }
    get maxUniformBufferBindingSize() { return this.getParameter(35376); }
    get maxStorageBufferBindingSize() { return 0; }
    get minUniformBufferOffsetAlignment() { return this.getParameter(35380); }
    get minStorageBufferOffsetAlignment() { return 0; }
    get maxVertexBuffers() { return 16; } // WebGL 2 supports 16 buffers, see https://github.com/gpuweb/gpuweb/issues/4284
    get maxVertexAttributes() { return this.getParameter(34921); }
    get maxVertexBufferArrayStride() { return 2048; } // TBD, this is just the default value from WebGPU
    get maxInterStageShaderVariables() { return this.getParameter(35659); }
    get maxComputeWorkgroupStorageSize() { return 0; } // WebGL does not support compute shaders
    get maxComputeInvocationsPerWorkgroup() { return 0; } // WebGL does not support compute shaders
    get maxComputeWorkgroupSizeX() { return 0; } // WebGL does not support compute shaders
    get maxComputeWorkgroupSizeY() { return 0; } // WebGL does not support compute shaders
    get maxComputeWorkgroupSizeZ() { return 0; } // WebGL does not support compute shaders
    get maxComputeWorkgroupsPerDimension() { return 0; } // WebGL does not support compute shaders
    // PRIVATE
    gl;
    limits = {};
    constructor(gl) {
        super();
        this.gl = gl;
    }
    getParameter(parameter) {
        if (this.limits[parameter] === undefined) {
            this.limits[parameter] = this.gl.getParameter(parameter);
        }
        return this.limits[parameter] || 0;
    }
}
//# sourceMappingURL=webgl-device-limits.js.map