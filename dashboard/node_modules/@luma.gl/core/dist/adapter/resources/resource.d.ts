import type { Device } from "../device.js";
export type ResourceProps = {
    /** Name of resource, mainly for debugging purposes. A unique name will be assigned if not provided */
    id?: string;
    /** Handle for the underlying resources (WebGL object or WebGPU handle) */
    handle?: any;
    /** User provided data stored on this resource  */
    userData?: {
        [key: string]: any;
    };
};
/**
 * Base class for GPU (WebGPU/WebGL) Resources
 */
export declare abstract class Resource<Props extends ResourceProps> {
    /** Default properties for resource */
    static defaultProps: Required<ResourceProps>;
    abstract get [Symbol.toStringTag](): string;
    toString(): string;
    /** props.id, for debugging. */
    id: string;
    readonly props: Required<Props>;
    readonly userData: Record<string, unknown>;
    abstract readonly device: Device;
    abstract readonly handle: unknown;
    private _device;
    /** Whether this resource has been destroyed */
    destroyed: boolean;
    /** For resources that allocate GPU memory */
    private allocatedBytes;
    /** Attached resources will be destroyed when this resource is destroyed. Tracks auto-created "sub" resources. */
    private _attachedResources;
    /**
     * Create a new Resource. Called from Subclass
     */
    constructor(device: Device, props: Props, defaultProps: Required<Props>);
    /**
     * destroy can be called on any resource to release it before it is garbage collected.
     */
    destroy(): void;
    /** @deprecated Use destroy() */
    delete(): this;
    /**
     * Combines a map of user props and default props, only including props from defaultProps
     * @returns returns a map of overridden default props
     */
    getProps(): object;
    /**
     * Attaches a resource. Attached resources are auto destroyed when this resource is destroyed
     * Called automatically when sub resources are auto created but can be called by application
     */
    attachResource(resource: Resource<ResourceProps>): void;
    /**
     * Detach an attached resource. The resource will no longer be auto-destroyed when this resource is destroyed.
     */
    detachResource(resource: Resource<ResourceProps>): void;
    /**
     * Destroys a resource (only if owned), and removes from the owned (auto-destroy) list for this resource.
     */
    destroyAttachedResource(resource: Resource<ResourceProps>): void;
    /** Destroy all owned resources. Make sure the resources are no longer needed before calling. */
    destroyAttachedResources(): void;
    /** Perform all destroy steps. Can be called by derived resources when overriding destroy() */
    protected destroyResource(): void;
    /** Called by .destroy() to track object destruction. Subclass must call if overriding destroy() */
    protected removeStats(): void;
    /** Called by subclass to track memory allocations */
    protected trackAllocatedMemory(bytes: number, name?: string): void;
    /** Called by subclass to track memory deallocations */
    protected trackDeallocatedMemory(name?: string): void;
    /** Called by resource constructor to track object creation */
    private addStats;
}
//# sourceMappingURL=resource.d.ts.map