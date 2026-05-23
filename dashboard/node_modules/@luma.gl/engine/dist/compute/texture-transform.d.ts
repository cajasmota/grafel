import { Buffer, Device, Framebuffer, RenderPassProps, Sampler, Texture } from '@luma.gl/core';
import { Model, ModelProps } from "../model/model.js";
/**
 * Properties for creating a {@link TextureTransform}
 */
export type TextureTransformProps = Omit<ModelProps, 'fs'> & {
    fs?: ModelProps['fs'];
    /** @deprecated TODO(donmccurdy): Needed? */
    inject?: Record<string, string>;
    /** @deprecated TODO(donmccurdy): Needed? */
    framebuffer?: Framebuffer;
    /** @deprecated TODO(donmccurdy): Model already handles this? */
    sourceBuffers?: Record<string, Buffer>;
    /** @deprecated TODO(donmccurdy): Model already handles this? */
    sourceTextures?: Record<string, Texture>;
    targetTexture: Texture;
    targetTextureChannels: 1 | 2 | 3 | 4;
    targetTextureVarying: string;
};
type TextureBinding = {
    sourceBuffers: Record<string, Buffer>;
    sourceTextures: Record<string, Texture>;
    targetTexture: Texture;
    framebuffer?: Framebuffer;
};
/**
 * Creates a pipeline for texture→texture transforms.
 * @deprecated
 */
export declare class TextureTransform {
    readonly device: Device;
    readonly model: Model;
    readonly sampler: Sampler;
    currentIndex: number;
    samplerTextureMap: Record<string, any> | null;
    bindings: TextureBinding[];
    resources: Record<string, any>;
    constructor(device: Device, props: TextureTransformProps);
    destroy(): void;
    /** @deprecated Use {@link destroy}. */
    delete(): void;
    run(options?: RenderPassProps): void;
    getTargetTexture(): Texture;
    getFramebuffer(): Framebuffer | undefined;
    _initialize(props: TextureTransformProps): void;
    _updateBindings(props: TextureTransformProps): void;
    _updateBinding(binding: TextureBinding, { sourceBuffers, sourceTextures, targetTexture }: TextureTransformProps): TextureBinding;
    _setSourceTextureParameters(): void;
}
export {};
//# sourceMappingURL=texture-transform.d.ts.map