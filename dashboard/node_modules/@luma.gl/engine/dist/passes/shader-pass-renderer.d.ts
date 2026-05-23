import { Device, RenderPass, Texture } from '@luma.gl/core';
import type { ShaderPass } from '@luma.gl/shadertools';
import { ShaderInputs } from "../shader-inputs.js";
import { AsyncTexture } from "../async-texture/async-texture.js";
import { ClipSpace } from "../models/clip-space.js";
import { SwapFramebuffers } from "../compute/swap.js";
import { BackgroundTextureModel } from "../models/billboard-texture-model.js";
type ShaderSubPass = NonNullable<ShaderPass['passes']>[0];
/** Props for ShaderPassRenderer */
export type ShaderPassRendererProps = {
    /** List of ShaderPasses to apply to the sourceTexture */
    shaderPasses: ShaderPass[];
    /** Optional typed ShaderInputs object for setting uniforms */
    shaderInputs: ShaderInputs;
};
/** A pass that renders a given texture into screen space */
export declare class ShaderPassRenderer {
    device: Device;
    shaderInputs: ShaderInputs;
    passRenderers: PassRenderer[];
    swapFramebuffers: SwapFramebuffers;
    /** For rendering to the screen */
    clipSpace: ClipSpace;
    textureModel: BackgroundTextureModel;
    constructor(device: Device, props: ShaderPassRendererProps);
    /** Destroys resources created by this ShaderPassRenderer */
    destroy(): void;
    resize(width: number, height: number): void;
    renderToScreen(options: {
        sourceTexture: AsyncTexture;
        uniforms?: any;
        bindings?: any;
    }): boolean;
    /** Runs the shaderPasses in sequence on the sourceTexture and returns a texture with the results.
     * @returns null if the the sourceTexture has not yet been loaded
     */
    renderToTexture(options: {
        sourceTexture: AsyncTexture;
        uniforms?: any;
        bindings?: any;
    }): Texture | null;
}
/** renders one ShaderPass */
declare class PassRenderer {
    shaderPass: ShaderPass;
    subPassRenderers: SubPassRenderer[];
    constructor(device: Device, shaderPass: ShaderPass, props?: {});
    destroy(): void;
}
/** Renders one subpass of a ShaderPass */
declare class SubPassRenderer {
    model: ClipSpace;
    shaderPass: ShaderPass;
    subPass: ShaderSubPass;
    constructor(device: Device, shaderPass: ShaderPass, subPass: ShaderSubPass);
    destroy(): void;
    render(options: {
        renderPass: RenderPass;
        bindings: any;
    }): void;
}
export {};
//# sourceMappingURL=shader-pass-renderer.d.ts.map