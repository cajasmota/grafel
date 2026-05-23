import type { TypedArray } from '@math.gl/core';
import type { PrimitiveTopology } from '@luma.gl/core';
export type GeometryProps = {
    id?: string;
    /** Determines how vertices are read from the 'vertex' attributes */
    topology: 'point-list' | 'line-list' | 'line-strip' | 'triangle-list' | 'triangle-strip';
    /** Auto calculated from attributes if not provided */
    vertexCount?: number;
    attributes: Record<string, GeometryAttribute | TypedArray>;
    indices?: GeometryAttribute | TypedArray;
};
export type GeometryAttributes = {
    POSITION: GeometryAttribute;
    NORMAL: GeometryAttribute;
    TEXCOORD_0: GeometryAttribute;
    COLOR_0?: GeometryAttribute;
    indices?: GeometryAttribute & {
        size: 1;
        value: Uint32Array | Uint16Array;
    };
};
export type GeometryAttribute = {
    size?: number;
    value: TypedArray;
    [key: string]: any;
};
export declare class Geometry {
    readonly id: string;
    /** Determines how vertices are read from the 'vertex' attributes */
    readonly topology?: PrimitiveTopology;
    readonly vertexCount: number;
    readonly indices?: GeometryAttribute;
    readonly attributes: {
        POSITION: GeometryAttribute;
        NORMAL: GeometryAttribute;
        TEXCOORD_0: GeometryAttribute;
        COLOR_0?: GeometryAttribute;
        [key: string]: GeometryAttribute | undefined;
    };
    userData: Record<string, unknown>;
    constructor(props: GeometryProps);
    getVertexCount(): number;
    /**
     * Return an object with all attributes plus indices added as a field.
     * TODO Geometry types are a mess
     */
    getAttributes(): GeometryAttributes;
    _print(attributeName: string): string;
    /**
     * GeometryAttribute
     * value: typed array
     * type: indices, vertices, uvs
     * size: elements per vertex
     * target: WebGL buffer type (string or constant)
     *
     * @param attributes
     * @param indices
     * @returns
     */
    _setAttributes(attributes: Record<string, GeometryAttribute>, indices: any): this;
    _calculateVertexCount(attributes: GeometryAttributes, indices?: GeometryAttribute): number;
}
//# sourceMappingURL=geometry.d.ts.map