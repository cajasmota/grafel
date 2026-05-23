import type { TextureFormatColor, TextureFormatDepthStencil, TextureFormat } from "../../shadertypes/textures/texture-formats.js";
import type { Device } from "../device.js";
import { Resource, ResourceProps } from "./resource.js";
import { Texture } from "./texture.js";
import { TextureView } from "./texture-view.js";
export type FramebufferProps = ResourceProps & {
    width?: number;
    height?: number;
    colorAttachments?: (TextureView | Texture | TextureFormatColor)[];
    depthStencilAttachment?: (TextureView | Texture | TextureFormatDepthStencil) | null;
};
/**
 * Create new textures with correct size for all attachments.
 * @note resize() destroys existing textures (if size has changed).
 */
export declare abstract class Framebuffer extends Resource<FramebufferProps> {
    get [Symbol.toStringTag](): string;
    /** Width of all attachments in this framebuffer */
    width: number;
    /** Height of all attachments in this framebuffer */
    height: number;
    /** Color attachments */
    abstract colorAttachments: TextureView[];
    /** Depth-stencil attachment, if provided */
    abstract depthStencilAttachment: TextureView | null;
    constructor(device: Device, props?: FramebufferProps);
    /**
     * Create a copy of this framebuffer with new attached textures, with same props but of the specified size.
     * @note Does not copy contents of the attached textures.
     */
    clone(size?: {
        width: number;
        height: number;
    }): Framebuffer;
    /**
     * Resizes all attachments
     * @note resize() destroys existing textures (if size has changed).
     * @deprecated Use framebuffer.clone()
     */
    resize(size: {
        width: number;
        height: number;
    }): void;
    resize(size: [width: number, height: number]): void;
    resize(): void;
    /** Auto creates any textures */
    protected autoCreateAttachmentTextures(): void;
    /** Create a color texture */
    protected createColorTexture(format: TextureFormat, index: number): Texture;
    /** Create depth stencil texture */
    protected createDepthStencilTexture(format: TextureFormat): Texture;
    /**
     * Default implementation of resize
     * Creates new textures with correct size for all attachments.
     * and destroys existing textures if owned
     */
    protected resizeAttachments(width: number, height: number): void;
    /** Implementation is expected to update any underlying binding (WebGL framebuffer attachment) */
    protected abstract updateAttachments(): void;
    static defaultProps: Required<FramebufferProps>;
}
//# sourceMappingURL=framebuffer.d.ts.map