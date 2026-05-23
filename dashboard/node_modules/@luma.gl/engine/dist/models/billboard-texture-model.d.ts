import { Device, Texture } from '@luma.gl/core';
import { AsyncTexture } from "../async-texture/async-texture.js";
import { ClipSpace } from "./clip-space.js";
/**
 * Props for a Model that renders a bitmap into the "background", i.e covering the screen
 */
export type BackgroundTextureModelProps = {
    /** id of this model */
    id?: string;
    /** The texture to render */
    backgroundTexture: Texture | AsyncTexture;
    /** If true, the texture is rendered into transparent areas of the screen only, i.e blended in where background alpha is small */
    blend?: boolean;
};
/**
 * Model that renders a bitmap into the "background", i.e covering the screen
 */
export declare class BackgroundTextureModel extends ClipSpace {
    constructor(device: Device, props: BackgroundTextureModelProps);
    setTexture(backgroundTexture: Texture | AsyncTexture): void;
    predraw(): void;
}
//# sourceMappingURL=billboard-texture-model.d.ts.map