import { RenderPass } from '@luma.gl/core';
import { ScenegraphNode, ScenegraphNodeProps } from "./scenegraph-node.js";
import { Model } from "../model/model.js";
export type ModelNodeProps = ScenegraphNodeProps & {
    model: Model;
    managedResources?: any[];
    bounds?: [number[], number[]];
};
export declare class ModelNode extends ScenegraphNode {
    readonly model: Model;
    bounds: [number[], number[]] | null;
    managedResources: any[];
    constructor(props: ModelNodeProps);
    destroy(): void;
    getBounds(): [number[], number[]] | null;
    draw(renderPass: RenderPass): boolean;
}
//# sourceMappingURL=model-node.d.ts.map