import { DeviceLimits } from '@luma.gl/core';
import { GL } from '@luma.gl/constants';
export declare class WebGLDeviceLimits extends DeviceLimits {
    get maxTextureDimension1D(): number;
    get maxTextureDimension2D(): number;
    get maxTextureDimension3D(): number;
    get maxTextureArrayLayers(): number;
    get maxBindGroups(): number;
    get maxDynamicUniformBuffersPerPipelineLayout(): number;
    get maxDynamicStorageBuffersPerPipelineLayout(): number;
    get maxSampledTexturesPerShaderStage(): number;
    get maxSamplersPerShaderStage(): number;
    get maxStorageBuffersPerShaderStage(): number;
    get maxStorageTexturesPerShaderStage(): number;
    get maxUniformBuffersPerShaderStage(): number;
    get maxUniformBufferBindingSize(): number;
    get maxStorageBufferBindingSize(): number;
    get minUniformBufferOffsetAlignment(): number;
    get minStorageBufferOffsetAlignment(): number;
    get maxVertexBuffers(): number;
    get maxVertexAttributes(): number;
    get maxVertexBufferArrayStride(): number;
    get maxInterStageShaderVariables(): number;
    get maxComputeWorkgroupStorageSize(): number;
    get maxComputeInvocationsPerWorkgroup(): number;
    get maxComputeWorkgroupSizeX(): number;
    get maxComputeWorkgroupSizeY(): number;
    get maxComputeWorkgroupSizeZ(): number;
    get maxComputeWorkgroupsPerDimension(): number;
    protected gl: WebGL2RenderingContext;
    protected limits: Partial<Record<GL, number>>;
    constructor(gl: WebGL2RenderingContext);
    protected getParameter(parameter: GL): number;
}
//# sourceMappingURL=webgl-device-limits.d.ts.map