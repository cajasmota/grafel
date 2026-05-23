import type { Framebuffer, Texture } from '@luma.gl/core';
/** Debug utility to draw FBO contents onto screen */
export declare function debugFramebuffer(fbo: Framebuffer | Texture, { id, minimap, opaque, top, left, rgbaScale }: {
    id: string;
    minimap?: boolean;
    opaque?: boolean;
    top?: string;
    left?: string;
    rgbaScale?: number;
}): void;
//# sourceMappingURL=debug-framebuffer.d.ts.map