import type { VariableShaderType } from "../shadertypes/data-types/shader-types.js";
import type { UniformValue } from "../adapter/types/uniforms.js";
import type { Device } from "../adapter/device.js";
import { Buffer } from "../adapter/resources/buffer.js";
import { UniformBlock } from "./uniform-block.js";
import { UniformBufferLayout } from "./uniform-buffer-layout.js";
/**
 * A uniform store holds a uniform values for one or more uniform blocks,
 * - It can generate binary data for any uniform buffer
 * - It can manage a uniform buffer for each block
 * - It can update managed uniform buffers with a single call
 * - It performs some book keeping on what has changed to minimize unnecessary writes to uniform buffers.
 */
export declare class UniformStore<TPropGroups extends Record<string, Record<string, unknown>> = Record<string, Record<string, unknown>>> {
    /** Stores the uniform values for each uniform block */
    uniformBlocks: Map<keyof TPropGroups, UniformBlock<Record<string, UniformValue>>>;
    /** Can generate data for a uniform buffer for each block from data */
    uniformBufferLayouts: Map<keyof TPropGroups, UniformBufferLayout>;
    /** Actual buffer for the blocks */
    uniformBuffers: Map<keyof TPropGroups, Buffer>;
    /**
     * Create a new UniformStore instance
     * @param blocks
     */
    constructor(blocks: Record<keyof TPropGroups, {
        uniformTypes?: Record<string, VariableShaderType>;
        uniformSizes?: Record<string, number>;
        defaultProps?: Record<string, unknown>;
        defaultUniforms?: Record<string, UniformValue>;
    }>);
    /** Destroy any managed uniform buffers */
    destroy(): void;
    /**
     * Set uniforms
     * Makes all properties partial
     */
    setUniforms(uniforms: Partial<{
        [group in keyof TPropGroups]: Partial<TPropGroups[group]>;
    }>): void;
    /** Get the required minimum length of the uniform buffer */
    getUniformBufferByteLength(uniformBufferName: keyof TPropGroups): number;
    /** Get formatted binary memory that can be uploaded to a buffer */
    getUniformBufferData(uniformBufferName: keyof TPropGroups): Uint8Array;
    /**
     * Creates an unmanaged uniform buffer (umnanaged means that application is responsible for destroying it)
     * The new buffer is initialized with current / supplied values
     */
    createUniformBuffer(device: Device, uniformBufferName: keyof TPropGroups, uniforms?: Partial<{
        [group in keyof TPropGroups]: Partial<TPropGroups[group]>;
    }>): Buffer;
    /** Get the managed uniform buffer. "managed" resources are destroyed when the uniformStore is destroyed. */
    getManagedUniformBuffer(device: Device, uniformBufferName: keyof TPropGroups): Buffer;
    /** Updates all uniform buffers where values have changed */
    updateUniformBuffers(): false | string;
    /** Update one uniform buffer. Only updates if values have changed */
    updateUniformBuffer(uniformBufferName: keyof TPropGroups): false | string;
}
//# sourceMappingURL=uniform-store.d.ts.map