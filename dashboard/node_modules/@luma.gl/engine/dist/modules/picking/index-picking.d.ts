import type { PickingProps, PickingUniforms } from "./picking-uniforms.js";
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
export declare const picking: {
    readonly name: "picking";
    readonly source: "struct pickingUniforms {\n  isActive: int32;\n  indexMode: int32;\n  batchIndex: int32;\n\n  isHighlightActive: int32;\n  highlightedBatchIndex: int32;\n  highlightedObjectIndex: int32;\n  highlightColor: vec4<f32>;\n} picking;\n\n\nconst INDEX_PICKING_MODE_INSTANCE = 0;\nconst INDEX_PICKING_MODE_CUSTOM = 1;\nconst INDEX_PICKING_INVALID_INDEX = -1; // 2^32 - 1\n\nstruct indexPickingFragmentInputs = {\n  objectIndex: int32;\n};\n\nlet indexPickingFragmentInputs: indexPickingFragmentInputs;\n\n/**\n * Vertex shaders should call this function to set the object index.\n * If using instance or vertex mode, argument will be ignored, 0 can be supplied.\n */\nfn picking_setObjectIndex(objectIndex: int32) {\n  switch (picking.indexMode) {\n    case INDEX_PICKING_MODE_INSTANCE, default: {\n      picking_objectIndex = instance_index;\n    };\n    case INDEX_PICKING_MODE_CUSTOM: {\n      picking_objectIndex = objectIndex;\n    };\n  }\n}\n\n";
    readonly vs: "precision highp float;\nprecision highp int;\n\nuniform pickingUniforms {\n  int isActive;\n  int indexMode;\n  int batchIndex;\n\n  int isHighlightActive;\n  int highlightedBatchIndex;\n  int highlightedObjectIndex;\n  vec4 highlightColor;\n} picking;\n\n\nconst int INDEX_PICKING_MODE_INSTANCE = 0;\nconst int INDEX_PICKING_MODE_CUSTOM = 1;\n\nconst int INDEX_PICKING_INVALID_INDEX = -1; // 2^32 - 1\n\nflat out int picking_objectIndex;\n\n/**\n * Vertex shaders should call this function to set the object index.\n * If using instance or vertex mode, argument will be ignored, 0 can be supplied.\n */\nvoid picking_setObjectIndex(int objectIndex) {\n  switch (picking.indexMode) {\n    case INDEX_PICKING_MODE_INSTANCE:\n      picking_objectIndex = gl_InstanceID;\n      break;\n    case INDEX_PICKING_MODE_CUSTOM:\n      picking_objectIndex = objectIndex;\n      break;\n  }\n}\n";
    readonly fs: "precision highp float;\nprecision highp int;\n\nuniform pickingUniforms {\n  int isActive;\n  int indexMode;\n  int batchIndex;\n\n  int isHighlightActive;\n  int highlightedBatchIndex;\n  int highlightedObjectIndex;\n  vec4 highlightColor;\n} picking;\n\n\nconst int INDEX_PICKING_INVALID_INDEX = -1; // 2^32 - 1\n\nflat in int picking_objectIndex;\n\n/**\n * Check if this vertex is highlighted (part of the selected batch and object)\n */ \nbool picking_isFragmentHighlighted() {\n  return \n    bool(picking.isHighlightActive) &&\n    picking.highlightedBatchIndex == picking.batchIndex &&\n    picking.highlightedObjectIndex == picking_objectIndex\n    ;\n}\n\n/**\n * Returns highlight color if this item is selected.\n */\nvec4 picking_filterHighlightColor(vec4 color) {\n  // If we are still picking, we don't highlight\n  if (bool(picking.isActive)) {\n    return color;\n  }\n\n  // If we are not highlighted, return color as is\n  if (!picking_isFragmentHighlighted()) {\n    return color;\n  }\n   \n  // Blend in highlight color based on its alpha value\n  float highLightAlpha = picking.highlightColor.a;\n  float blendedAlpha = highLightAlpha + color.a * (1.0 - highLightAlpha);\n  float highLightRatio = highLightAlpha / blendedAlpha;\n\n  vec3 blendedRGB = mix(color.rgb, picking.highlightColor.rgb, highLightRatio);\n  return vec4(blendedRGB, blendedAlpha);\n}\n\n/*\n * Returns picking color if picking enabled else unmodified argument.\n */\nivec4 picking_getPickingColor() {\n  // Assumes that colorAttachment0 is rg32int\n  // TODO? - we could render indices into a second color attachment and not mess with fragColor\n  return ivec4(picking_objectIndex, picking.batchIndex, 0u, 0u);  \n}\n\nvec4 picking_filterPickingColor(vec4 color) {\n  if (bool(picking.isActive)) {\n    if (picking_objectIndex == INDEX_PICKING_INVALID_INDEX) {\n      discard;\n    }\n  }\n  return color;\n}\n\n/*\n * Returns picking color if picking is enabled if not\n * highlight color if this item is selected, otherwise unmodified argument.\n */\nvec4 picking_filterColor(vec4 color) {\n  vec4 outColor = color;\n  outColor = picking_filterHighlightColor(outColor);\n  outColor = picking_filterPickingColor(outColor);\n  return outColor;\n}\n";
    readonly props: PickingProps;
    readonly uniforms: PickingUniforms;
    readonly uniformTypes: Required<import("@luma.gl/shadertools").UniformTypes<PickingUniforms>>;
    readonly defaultUniforms: {
        readonly isActive: false;
        readonly indexMode: 0;
        readonly batchIndex: 0;
        readonly isHighlightActive: true;
        readonly highlightedBatchIndex: -1;
        readonly highlightedObjectIndex: -1;
        readonly highlightColor: import("@math.gl/types").NumberArray4;
    };
    readonly getUniforms: (props?: PickingProps, prevUniforms?: PickingUniforms) => PickingUniforms;
};
//# sourceMappingURL=index-picking.d.ts.map