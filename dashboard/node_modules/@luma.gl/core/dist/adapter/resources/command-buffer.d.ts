import { Device } from "../device.js";
import { Resource, ResourceProps } from "./resource.js";
export type CommandBufferProps = ResourceProps & {};
/**
 * Encodes commands to queue that can be executed later
 */
export declare abstract class CommandBuffer extends Resource<CommandBufferProps> {
    get [Symbol.toStringTag](): string;
    constructor(device: Device, props: CommandBufferProps);
    static defaultProps: Required<CommandBufferProps>;
}
//# sourceMappingURL=command-buffer.d.ts.map