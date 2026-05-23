import type { BufferProps, FramebufferProps } from '@luma.gl/core';
import { Device, Resource, Buffer, Framebuffer } from '@luma.gl/core';
/**
 * Helper class for working with repeated transformations / computations
 * Primarily intended for GPU buffers `Swap<Buffer>` or textures `Swap<Texture>`)
 * @note the two resources are expected to be structurally identical (same size, length, format, etc)
 * @note the two resources can be destroyed by calling `destroy()`
 */
export declare class Swap<T extends Resource<any>> {
    /** The current resource - usually the source for renders or computations */
    current: T;
    /** The next resource - usually the target/destination for transforms / computations */
    next: T;
    constructor(props: {
        current: T;
        next: T;
    });
    /** Destroys the two managed resources */
    destroy(): void;
    /** Make the next resource into the current resource, and reuse the current resource as the next resource */
    swap(): void;
}
/** Helper for managing double-buffered framebuffers */
export declare class SwapFramebuffers extends Swap<Framebuffer> {
    constructor(device: Device, props: FramebufferProps);
    /**
     * Resizes the Framebuffers.
     * @returns true if the size changed, otherwise exiting framebuffers were preserved
     * @note any contents are not preserved!
     */
    resize(size: {
        width: number;
        height: number;
    }): boolean;
}
/** Helper for managing double-buffered GPU buffers */
export declare class SwapBuffers extends Swap<Buffer> {
    constructor(device: Device, props: BufferProps);
    /**
     * Resizes the Buffers.
     * @returns true if the size changed, otherwise exiting buffers were preserved.
     * @note any contents are not preserved!
     */
    resize(props: {
        byteLength: number;
    }): boolean;
}
//# sourceMappingURL=swap.d.ts.map