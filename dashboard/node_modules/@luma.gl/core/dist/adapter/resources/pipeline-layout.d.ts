import { Device } from "../device.js";
import { ShaderLayout } from "../types/shader-layout.js";
import { Resource, ResourceProps } from "./resource.js";
export type PipelineLayoutProps = ResourceProps & {
    shaderLayout: ShaderLayout;
};
/** Immutable PipelineLayout object */
export declare abstract class PipelineLayout extends Resource<PipelineLayoutProps> {
    get [Symbol.toStringTag](): string;
    constructor(device: Device, props: PipelineLayoutProps);
    static defaultProps: Required<PipelineLayoutProps>;
}
//# sourceMappingURL=pipeline-layout.d.ts.map