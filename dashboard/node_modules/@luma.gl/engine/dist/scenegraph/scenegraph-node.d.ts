import { Vector3, Matrix4, NumericArray } from '@math.gl/core';
/** Properties for creating a new Scenegraph */
export type ScenegraphNodeProps = {
    id?: string;
    /** whether to display the object at all */
    display?: boolean;
    matrix?: NumericArray;
    position?: NumericArray;
    rotation?: NumericArray;
    scale?: NumericArray;
    update?: boolean;
};
export declare class ScenegraphNode {
    readonly id: string;
    matrix: Matrix4;
    display: boolean;
    position: Vector3;
    rotation: Vector3;
    scale: Vector3;
    userData: Record<string, unknown>;
    props: ScenegraphNodeProps;
    constructor(props?: ScenegraphNodeProps);
    getBounds(): [number[], number[]] | null;
    destroy(): void;
    /** @deprecated use .destroy() */
    delete(): void;
    setProps(props: ScenegraphNodeProps): this;
    toString(): string;
    setPosition(position: any): this;
    setRotation(rotation: any): this;
    setScale(scale: any): this;
    setMatrix(matrix: any, copyMatrix?: boolean): void;
    setMatrixComponents(components: {
        position?: any;
        rotation?: any;
        scale?: any;
        update?: boolean;
    }): this;
    updateMatrix(): this;
    update(options?: {
        position?: any;
        rotation?: any;
        scale?: any;
    }): this;
    getCoordinateUniforms(viewMatrix: any, modelMatrix?: any): {
        viewMatrix: any;
        modelMatrix: any;
        objectMatrix: any;
        worldMatrix: any;
        worldInverseMatrix: any;
        worldInverseTransposeMatrix: any;
    };
    _setScenegraphNodeProps(props: ScenegraphNodeProps): void;
}
//# sourceMappingURL=scenegraph-node.d.ts.map