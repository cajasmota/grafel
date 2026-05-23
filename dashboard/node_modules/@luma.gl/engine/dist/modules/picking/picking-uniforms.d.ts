import type { NumberArray4 } from '@math.gl/types';
export declare const INVALID_INDEX = -1;
/**
 * Props for the picking module, which depending on mode renders picking colors or highlighted item.
 * When active, renders picking colors, assumed to be rendered to off-screen "picking" buffer.
 * When inactive, renders normal colors, with the exception of selected object which is rendered with highlight
 * can distinguish between 2^32 different objects in each of 2^32 different batches.
 */
export type PickingProps = {
    /** Are we picking? I.e. rendering picking colors? */
    isActive?: boolean;
    /** Whether to use instance_index (built-in) or a custom application supplied index (usually from an attribute) */
    indexMode?: 'instance' | 'custom';
    /** Batch index (used when rendering multiple models to identify which model was picked), defaults to 0 */
    batchIndex?: number;
    /** Index of the highlighted batch, defaults to 0 */
    highlightedBatchIndex?: number | null;
    /** Set an index to highlight that item, or `null` to explicitly clear **/
    highlightedObjectIndex?: number | null;
    /** Color of visual highlight of "selected" item () */
    highlightColor?: NumberArray4;
};
/**
 * Uniforms for the picking module, which renders picking colors and highlighted item.
 * When active, renders picking colors, assumed to be rendered to off-screen "picking" buffer.
 * When inactive, renders normal colors, with the exception of selected object which is rendered with highlight
 */
export type PickingUniforms = {
    /**
     * When true, renders picking colors. Set when rendering to off-screen "picking" buffer.
     * When false, renders normal colors, with the exception of selected object which is rendered with highlight
     */
    isActive: boolean;
    /** Set to true when picking an attribute value instead of object index */
    indexMode: 0 | 1;
    /** Index of batch currently being rendered */
    batchIndex: number;
    /** Do we have a highlighted item? */
    isHighlightActive: boolean;
    /** Color of visual highlight of "selected" item. Note: RGBA components must in the range 0-1 */
    highlightColor: NumberArray4;
    /** Indicates which batch to visually highlight an item in (defaults to 0) */
    highlightedBatchIndex: number;
    /** Indicates which index in the batch to highlight an item in */
    highlightedObjectIndex: number;
};
export type PickingBindings = {};
export declare const GLSL_UNIFORMS = "precision highp float;\nprecision highp int;\n\nuniform pickingUniforms {\n  int isActive;\n  int indexMode;\n  int batchIndex;\n\n  int isHighlightActive;\n  int highlightedBatchIndex;\n  int highlightedObjectIndex;\n  vec4 highlightColor;\n} picking;\n";
export declare const WGSL_UNIFORMS = "struct pickingUniforms {\n  isActive: int32;\n  indexMode: int32;\n  batchIndex: int32;\n\n  isHighlightActive: int32;\n  highlightedBatchIndex: int32;\n  highlightedObjectIndex: int32;\n  highlightColor: vec4<f32>;\n} picking;\n";
declare function getUniforms(props?: PickingProps, prevUniforms?: PickingUniforms): PickingUniforms;
/**
 * Provides support for color-based picking and highlighting.
 *
 * In particular, supports picking a specific instance in an instanced
 * draw call and highlighting an instance based on its picking color,
 * and correspondingly, supports picking and highlighting groups of
 * primitives with the same picking color in non-instanced draw-calls
 *
 * @note Color based picking has the significant advantage in that it can be added to any
 * existing shader without requiring any additional picking logic.
 */
export declare const pickingUniforms: {
    readonly props: PickingProps;
    readonly uniforms: PickingUniforms;
    readonly name: "picking";
    readonly uniformTypes: Required<import("@luma.gl/shadertools").UniformTypes<PickingUniforms>>;
    readonly defaultUniforms: {
        readonly isActive: false;
        readonly indexMode: 0;
        readonly batchIndex: 0;
        readonly isHighlightActive: true;
        readonly highlightedBatchIndex: -1;
        readonly highlightedObjectIndex: -1;
        readonly highlightColor: NumberArray4;
    };
    readonly getUniforms: typeof getUniforms;
};
export {};
//# sourceMappingURL=picking-uniforms.d.ts.map