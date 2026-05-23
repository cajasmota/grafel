import type { PrimitiveTopology, BufferLayout } from '@luma.gl/core';
import { Device, Buffer } from '@luma.gl/core';
import type { Geometry } from "../geometry/geometry.js";
export type GPUGeometryProps = {
    id?: string;
    /** Determines how vertices are read from the 'vertex' attributes */
    topology: 'point-list' | 'line-list' | 'line-strip' | 'triangle-list' | 'triangle-strip';
    /** Auto calculated from attributes if not provided */
    vertexCount: number;
    bufferLayout: BufferLayout[];
    indices?: Buffer | null;
    attributes: Record<string, Buffer>;
};
export declare class GPUGeometry {
    readonly id: string;
    userData: Record<string, unknown>;
    /** Determines how vertices are read from the 'vertex' attributes */
    readonly topology?: PrimitiveTopology;
    readonly bufferLayout: BufferLayout[];
    readonly vertexCount: number;
    readonly indices?: Buffer | null;
    readonly attributes: Record<string, Buffer>;
    constructor(props: GPUGeometryProps);
    destroy(): void;
    getVertexCount(): number;
    getAttributes(): Record<string, Buffer>;
    getIndexes(): Buffer | null;
    _calculateVertexCount(positions: Buffer): number;
}
export declare function makeGPUGeometry(device: Device, geometry: Geometry | GPUGeometry): GPUGeometry;
export declare function getIndexBufferFromGeometry(device: Device, geometry: Geometry): Buffer | undefined;
export declare function getAttributeBuffersFromGeometry(device: Device, geometry: Geometry): {
    attributes: Record<string, Buffer>;
    bufferLayout: BufferLayout[];
    vertexCount: number;
};
//# sourceMappingURL=gpu-geometry.d.ts.map