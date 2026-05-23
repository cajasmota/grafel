// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { lighting } from "../lights/lighting.js";
import { vs, fs } from "./pbr-material-glsl.js";
import { source } from "./pbr-material-wgsl.js";
import { pbrProjection } from "./pbr-projection.js";
/**
 * An implementation of PBR (Physically-Based Rendering).
 * Physically Based Shading of a microfacet surface defined by a glTF material.
 */
export const pbrMaterial = {
    props: {},
    uniforms: {},
    name: 'pbrMaterial',
    dependencies: [lighting, pbrProjection],
    source,
    vs,
    fs,
    defines: {
        LIGHTING_FRAGMENT: true,
        HAS_NORMALMAP: false,
        HAS_EMISSIVEMAP: false,
        HAS_OCCLUSIONMAP: false,
        HAS_BASECOLORMAP: false,
        HAS_METALROUGHNESSMAP: false,
        ALPHA_CUTOFF: false,
        USE_IBL: false,
        PBR_DEBUG: false
    },
    getUniforms: props => props,
    uniformTypes: {
        // Material is unlit
        unlit: 'i32',
        // Base color map
        baseColorMapEnabled: 'i32',
        baseColorFactor: 'vec4<f32>',
        normalMapEnabled: 'i32',
        normalScale: 'f32', // #ifdef HAS_NORMALMAP
        emissiveMapEnabled: 'i32',
        emissiveFactor: 'vec3<f32>', // #ifdef HAS_EMISSIVEMAP
        metallicRoughnessValues: 'vec2<f32>',
        metallicRoughnessMapEnabled: 'i32',
        occlusionMapEnabled: 'i32',
        occlusionStrength: 'f32', // #ifdef HAS_OCCLUSIONMAP
        alphaCutoffEnabled: 'i32',
        alphaCutoff: 'f32', // #ifdef ALPHA_CUTOFF
        // IBL
        IBLenabled: 'i32',
        scaleIBLAmbient: 'vec2<f32>', // #ifdef USE_IBL
        // debugging flags used for shader output of intermediate PBR variables
        // #ifdef PBR_DEBUG
        scaleDiffBaseMR: 'vec4<f32>',
        scaleFGDSpec: 'vec4<f32>'
    }
};
//# sourceMappingURL=pbr-material.js.map