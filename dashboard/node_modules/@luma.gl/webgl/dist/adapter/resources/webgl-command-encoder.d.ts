import { CommandEncoder, CommandEncoderProps } from '@luma.gl/core';
import type { RenderPassProps, ComputePass, ComputePassProps, QuerySet, Buffer, CopyBufferToBufferOptions, CopyBufferToTextureOptions, CopyTextureToBufferOptions, CopyTextureToTextureOptions } from '@luma.gl/core';
import { WEBGLCommandBuffer } from "./webgl-command-buffer.js";
import { WEBGLRenderPass } from "./webgl-render-pass.js";
import { WebGLDevice } from "../webgl-device.js";
export declare class WEBGLCommandEncoder extends CommandEncoder {
    readonly device: WebGLDevice;
    readonly handle: null;
    readonly commandBuffer: WEBGLCommandBuffer;
    constructor(device: WebGLDevice, props: CommandEncoderProps);
    destroy(): void;
    finish(): WEBGLCommandBuffer;
    beginRenderPass(props: RenderPassProps): WEBGLRenderPass;
    beginComputePass(props: ComputePassProps): ComputePass;
    copyBufferToBuffer(options: CopyBufferToBufferOptions): void;
    copyBufferToTexture(options: CopyBufferToTextureOptions): void;
    copyTextureToBuffer(options: CopyTextureToBufferOptions): void;
    copyTextureToTexture(options: CopyTextureToTextureOptions): void;
    pushDebugGroup(groupLabel: string): void;
    popDebugGroup(): void;
    insertDebugMarker(markerLabel: string): void;
    resolveQuerySet(querySet: QuerySet, destination: Buffer, options?: {
        firstQuery?: number;
        queryCount?: number;
        destinationOffset?: number;
    }): void;
}
//# sourceMappingURL=webgl-command-encoder.d.ts.map