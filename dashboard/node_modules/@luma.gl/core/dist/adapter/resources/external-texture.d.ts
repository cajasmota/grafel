import type { Device } from "../device.js";
import { Resource, ResourceProps } from "./resource.js";
export type ExternalTextureProps = ResourceProps & {
    source: HTMLVideoElement;
    colorSpace?: 'srgb';
};
export declare abstract class ExternalTexture extends Resource<ExternalTextureProps> {
    get [Symbol.toStringTag](): string;
    constructor(device: Device, props: ExternalTextureProps);
    static defaultProps: Required<ExternalTextureProps>;
}
//# sourceMappingURL=external-texture.d.ts.map