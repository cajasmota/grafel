import { Matrix4 } from '@math.gl/core';
import { ScenegraphNode, ScenegraphNodeProps } from "./scenegraph-node.js";
export type GroupNodeProps = ScenegraphNodeProps & {
    children?: ScenegraphNode[];
};
export declare class GroupNode extends ScenegraphNode {
    children: ScenegraphNode[];
    constructor(children: ScenegraphNode[]);
    constructor(props?: GroupNodeProps);
    getBounds(): [number[], number[]] | null;
    destroy(): void;
    add(...children: (ScenegraphNode | ScenegraphNode[])[]): this;
    remove(child: ScenegraphNode): this;
    removeAll(): this;
    traverse(visitor: (node: ScenegraphNode, context: {
        worldMatrix: Matrix4;
    }) => void, { worldMatrix }?: {
        worldMatrix?: Matrix4 | undefined;
    }): void;
}
//# sourceMappingURL=group-node.d.ts.map