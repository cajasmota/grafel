import { Resource, ResourceProps } from "./resource.js";
import type { ComputeShaderLayout, Binding } from "../types/shader-layout.js";
import type { Device } from "../device.js";
import type { Shader } from "./shader.js";
/**
 * Properties for a compute pipeline
 */
export type ComputePipelineProps = ResourceProps & {
    handle?: unknown;
    /** Compiled shader object */
    shader: Shader;
    /** The entry point, defaults to main */
    entryPoint?: string;
    /** These are WGSL constant values - different from GLSL defines in that shader does not need to be recompiled */
    constants?: Record<string, number>;
    /** Describes the attributes and bindings exposed by the pipeline shader(s). */
    shaderLayout?: ComputeShaderLayout | null;
};
/**
 * A compiled and linked shader program for compute
 */
export declare abstract class ComputePipeline extends Resource<ComputePipelineProps> {
    get [Symbol.toStringTag](): string;
    hash: string;
    /** The merged shader layout */
    shaderLayout: ComputeShaderLayout;
    constructor(device: Device, props: ComputePipelineProps);
    /**
     * @todo Use renderpass.setBindings() ?
     * @todo Do we want to expose BindGroups in the API and remove this?
     */
    abstract setBindings(bindings: Record<string, Binding>): void;
    static defaultProps: Required<ComputePipelineProps>;
}
//# sourceMappingURL=compute-pipeline.d.ts.map