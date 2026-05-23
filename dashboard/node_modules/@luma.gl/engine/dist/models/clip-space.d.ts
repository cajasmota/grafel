import { Device } from '@luma.gl/core';
import { Model, ModelProps } from "../model/model.js";
/** Props for ClipSpace */
export type ClipSpaceProps = Omit<ModelProps, 'vs' | 'vertexCount' | 'geometry'>;
/**
 * A flat geometry that covers the "visible area" that the GPU renders.
 */
export declare class ClipSpace extends Model {
    constructor(device: Device, props: ClipSpaceProps);
}
//# sourceMappingURL=clip-space.d.ts.map