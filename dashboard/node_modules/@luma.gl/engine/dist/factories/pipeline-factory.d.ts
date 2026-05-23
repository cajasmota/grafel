import type { RenderPipelineProps, ComputePipelineProps } from '@luma.gl/core';
import { Device, RenderPipeline, ComputePipeline } from '@luma.gl/core';
export type PipelineFactoryProps = RenderPipelineProps;
/**
 * Efficiently creates / caches pipelines
 */
export declare class PipelineFactory {
    static defaultProps: Required<PipelineFactoryProps>;
    /** Get the singleton default pipeline factory for the specified device */
    static getDefaultPipelineFactory(device: Device): PipelineFactory;
    readonly device: Device;
    readonly cachingEnabled: boolean;
    readonly destroyPolicy: 'unused' | 'never';
    readonly debug: boolean;
    private _hashCounter;
    private readonly _hashes;
    private readonly _renderPipelineCache;
    private readonly _computePipelineCache;
    get [Symbol.toStringTag](): string;
    toString(): string;
    constructor(device: Device);
    /** Return a RenderPipeline matching supplied props. Reuses an equivalent pipeline if already created. */
    createRenderPipeline(props: RenderPipelineProps): RenderPipeline;
    /** Return a ComputePipeline matching supplied props. Reuses an equivalent pipeline if already created. */
    createComputePipeline(props: ComputePipelineProps): ComputePipeline;
    release(pipeline: RenderPipeline | ComputePipeline): void;
    /** Destroy a cached pipeline, removing it from the cache (depending on destroy policy) */
    private _destroyPipeline;
    /** Get the appropriate cache for the type of pipeline */
    private _getCache;
    /** Calculate a hash based on all the inputs for a compute pipeline */
    private _hashComputePipeline;
    /** Calculate a hash based on all the inputs for a render pipeline */
    private _hashRenderPipeline;
    private _getHash;
}
//# sourceMappingURL=pipeline-factory.d.ts.map