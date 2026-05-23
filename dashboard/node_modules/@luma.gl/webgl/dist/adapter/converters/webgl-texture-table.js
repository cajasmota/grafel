// luma.gl
// SPDX-License-Identifier: MIT
// Copyright (c) vis.gl contributors
import { textureFormatDecoder } from '@luma.gl/core';
import { GL } from '@luma.gl/constants';
import { getWebGLExtension } from "../../context/helpers/webgl-extensions.js";
import { getGLFromVertexType } from "./webgl-vertex-formats.js";
/* eslint-disable camelcase */
// TEXTURE FEATURES
// Define local webgl extension strings to optimize minification
const X_S3TC = 'WEBGL_compressed_texture_s3tc'; // BC1, BC2, BC3
const X_S3TC_SRGB = 'WEBGL_compressed_texture_s3tc_srgb'; // BC1, BC2, BC3
const X_RGTC = 'EXT_texture_compression_rgtc'; // BC4, BC5
const X_BPTC = 'EXT_texture_compression_bptc'; // BC6, BC7
const X_ETC2 = 'WEBGL_compressed_texture_etc'; // Renamed from 'WEBGL_compressed_texture_es3'
const X_ASTC = 'WEBGL_compressed_texture_astc';
const X_ETC1 = 'WEBGL_compressed_texture_etc1';
const X_PVRTC = 'WEBGL_compressed_texture_pvrtc';
const X_ATC = 'WEBGL_compressed_texture_atc';
// Define local webgl extension strings to optimize minification
const EXT_texture_norm16 = 'EXT_texture_norm16';
const EXT_render_snorm = 'EXT_render_snorm';
const EXT_color_buffer_float = 'EXT_color_buffer_float';
// prettier-ignore
export const TEXTURE_FEATURES = {
    'float32-renderable-webgl': ['EXT_color_buffer_float'],
    'float16-renderable-webgl': ['EXT_color_buffer_half_float'],
    'rgb9e5ufloat-renderable-webgl': ['WEBGL_render_shared_exponent'],
    'snorm8-renderable-webgl': [EXT_render_snorm],
    'norm16-renderable-webgl': [EXT_texture_norm16],
    'snorm16-renderable-webgl': [EXT_texture_norm16, EXT_render_snorm],
    'float32-filterable': ['OES_texture_float_linear'],
    'float16-filterable-webgl': ['OES_texture_half_float_linear'],
    'texture-filterable-anisotropic-webgl': ['EXT_texture_filter_anisotropic'],
    'texture-blend-float-webgl': ['EXT_float_blend'],
    'texture-compression-bc': [X_S3TC, X_S3TC_SRGB, X_RGTC, X_BPTC],
    // 'texture-compression-bc3-srgb-webgl': [X_S3TC_SRGB],
    // 'texture-compression-bc3-webgl': [X_S3TC],
    'texture-compression-bc5-webgl': [X_RGTC],
    'texture-compression-bc7-webgl': [X_BPTC],
    'texture-compression-etc2': [X_ETC2],
    'texture-compression-astc': [X_ASTC],
    'texture-compression-etc1-webgl': [X_ETC1],
    'texture-compression-pvrtc-webgl': [X_PVRTC],
    'texture-compression-atc-webgl': [X_ATC]
};
export function isTextureFeature(feature) {
    return feature in TEXTURE_FEATURES;
}
/** Checks a texture feature (for Device.features). Mainly compressed texture support */
export function checkTextureFeature(gl, feature, extensions) {
    const textureExtensions = TEXTURE_FEATURES[feature] || [];
    return textureExtensions.every(extension => getWebGLExtension(gl, extension, extensions));
}
// TABLES
/**
 * Texture format data -
 * Exported but can change without notice
 */
// prettier-ignore
export const WEBGL_TEXTURE_FORMATS = {
    // 8-bit formats
    'r8unorm': { gl: 33321, rb: true },
    'r8snorm': { gl: 36756 },
    'r8uint': { gl: 33330, rb: true },
    'r8sint': { gl: 33329, rb: true },
    // 16-bit formats
    'rg8unorm': { gl: 33323, rb: true },
    'rg8snorm': { gl: 36757 },
    'rg8uint': { gl: 33336, rb: true },
    'rg8sint': { gl: 33335, rb: true },
    'r16uint': { gl: 33332, rb: true },
    'r16sint': { gl: 33331, rb: true },
    'r16float': { gl: 33325, rb: true },
    'r16unorm': { gl: 33322, rb: true },
    'r16snorm': { gl: 36760 },
    // Packed 16-bit formats
    'rgba4unorm-webgl': { gl: 32854, rb: true },
    'rgb565unorm-webgl': { gl: 36194, rb: true },
    'rgb5a1unorm-webgl': { gl: 32855, rb: true },
    // 24-bit formats
    'rgb8unorm-webgl': { gl: 32849 },
    'rgb8snorm-webgl': { gl: 36758 },
    // 32-bit formats
    'rgba8unorm': { gl: 32856 },
    'rgba8unorm-srgb': { gl: 35907 },
    'rgba8snorm': { gl: 36759 },
    'rgba8uint': { gl: 36220 },
    'rgba8sint': { gl: 36238 },
    // reverse colors, webgpu only
    'bgra8unorm': {},
    'bgra8unorm-srgb': {},
    'rg16uint': { gl: 33338 },
    'rg16sint': { gl: 33337 },
    'rg16float': { gl: 33327, rb: true },
    'rg16unorm': { gl: 33324 },
    'rg16snorm': { gl: 36761 },
    'r32uint': { gl: 33334, rb: true },
    'r32sint': { gl: 33333, rb: true },
    'r32float': { gl: 33326 },
    // Packed 32-bit formats
    'rgb9e5ufloat': { gl: 35901 }, // , filter: true},
    'rg11b10ufloat': { gl: 35898, rb: true },
    'rgb10a2unorm': { gl: 32857, rb: true },
    'rgb10a2uint': { gl: 36975, rb: true },
    // 48-bit formats
    'rgb16unorm-webgl': { gl: 32852 }, // rgb not renderable
    'rgb16snorm-webgl': { gl: 36762 }, // rgb not renderable
    // 64-bit formats
    'rg32uint': { gl: 33340, rb: true },
    'rg32sint': { gl: 33339, rb: true },
    'rg32float': { gl: 33328, rb: true },
    'rgba16uint': { gl: 36214, rb: true },
    'rgba16sint': { gl: 36232, rb: true },
    'rgba16float': { gl: 34842 },
    'rgba16unorm': { gl: 32859, rb: true },
    'rgba16snorm': { gl: 36763 },
    // 96-bit formats (deprecated!)
    'rgb32float-webgl': { gl: 34837, x: EXT_color_buffer_float, dataFormat: 6407, types: [5126] },
    // 128-bit formats
    'rgba32uint': { gl: 36208, rb: true },
    'rgba32sint': { gl: 36226, rb: true },
    'rgba32float': { gl: 34836, rb: true },
    // Depth and stencil formats
    'stencil8': { gl: 36168, rb: true }, // 8 stencil bits
    'depth16unorm': { gl: 33189, dataFormat: 6402, types: [5123], rb: true }, // 16 depth bits
    'depth24plus': { gl: 33190, dataFormat: 6402, types: [5125] },
    'depth32float': { gl: 36012, dataFormat: 6402, types: [5126], rb: true },
    // The depth component of the "depth24plus" and "depth24plus-stencil8" formats may be implemented as either a 24-bit depth value or a "depth32float" value.
    'depth24plus-stencil8': { gl: 35056, rb: true, depthTexture: true, dataFormat: 34041, types: [34042] },
    // "depth32float-stencil8" feature - TODO below is render buffer only?
    'depth32float-stencil8': { gl: 36013, dataFormat: 34041, types: [36269], rb: true },
    // BC compressed formats: check device.features.has("texture-compression-bc");
    'bc1-rgb-unorm-webgl': { gl: 33776, x: X_S3TC },
    'bc1-rgb-unorm-srgb-webgl': { gl: 35916, x: X_S3TC_SRGB },
    'bc1-rgba-unorm': { gl: 33777, x: X_S3TC },
    'bc1-rgba-unorm-srgb': { gl: 35916, x: X_S3TC_SRGB },
    'bc2-rgba-unorm': { gl: 33778, x: X_S3TC },
    'bc2-rgba-unorm-srgb': { gl: 35918, x: X_S3TC_SRGB },
    'bc3-rgba-unorm': { gl: 33779, x: X_S3TC },
    'bc3-rgba-unorm-srgb': { gl: 35919, x: X_S3TC_SRGB },
    'bc4-r-unorm': { gl: 36283, x: X_RGTC },
    'bc4-r-snorm': { gl: 36284, x: X_RGTC },
    'bc5-rg-unorm': { gl: 36285, x: X_RGTC },
    'bc5-rg-snorm': { gl: 36286, x: X_RGTC },
    'bc6h-rgb-ufloat': { gl: 36495, x: X_BPTC },
    'bc6h-rgb-float': { gl: 36494, x: X_BPTC },
    'bc7-rgba-unorm': { gl: 36492, x: X_BPTC },
    'bc7-rgba-unorm-srgb': { gl: 36493, x: X_BPTC },
    // WEBGL_compressed_texture_etc: device.features.has("texture-compression-etc2")
    // Note: Supposedly guaranteed availability compressed formats in WebGL2, but through CPU decompression
    'etc2-rgb8unorm': { gl: 37492 },
    'etc2-rgb8unorm-srgb': { gl: 37494 },
    'etc2-rgb8a1unorm': { gl: 37496 },
    'etc2-rgb8a1unorm-srgb': { gl: 37497 },
    'etc2-rgba8unorm': { gl: 37493 },
    'etc2-rgba8unorm-srgb': { gl: 37495 },
    'eac-r11unorm': { gl: 37488 },
    'eac-r11snorm': { gl: 37489 },
    'eac-rg11unorm': { gl: 37490 },
    'eac-rg11snorm': { gl: 37491 },
    // X_ASTC compressed formats: device.features.has("texture-compression-astc")
    'astc-4x4-unorm': { gl: 37808 },
    'astc-4x4-unorm-srgb': { gl: 37840 },
    'astc-5x4-unorm': { gl: 37809 },
    'astc-5x4-unorm-srgb': { gl: 37841 },
    'astc-5x5-unorm': { gl: 37810 },
    'astc-5x5-unorm-srgb': { gl: 37842 },
    'astc-6x5-unorm': { gl: 37811 },
    'astc-6x5-unorm-srgb': { gl: 37843 },
    'astc-6x6-unorm': { gl: 37812 },
    'astc-6x6-unorm-srgb': { gl: 37844 },
    'astc-8x5-unorm': { gl: 37813 },
    'astc-8x5-unorm-srgb': { gl: 37845 },
    'astc-8x6-unorm': { gl: 37814 },
    'astc-8x6-unorm-srgb': { gl: 37846 },
    'astc-8x8-unorm': { gl: 37815 },
    'astc-8x8-unorm-srgb': { gl: 37847 },
    'astc-10x5-unorm': { gl: 37819 },
    'astc-10x5-unorm-srgb': { gl: 37851 },
    'astc-10x6-unorm': { gl: 37817 },
    'astc-10x6-unorm-srgb': { gl: 37849 },
    'astc-10x8-unorm': { gl: 37818 },
    'astc-10x8-unorm-srgb': { gl: 37850 },
    'astc-10x10-unorm': { gl: 37819 },
    'astc-10x10-unorm-srgb': { gl: 37851 },
    'astc-12x10-unorm': { gl: 37820 },
    'astc-12x10-unorm-srgb': { gl: 37852 },
    'astc-12x12-unorm': { gl: 37821 },
    'astc-12x12-unorm-srgb': { gl: 37853 },
    // WEBGL_compressed_texture_pvrtc
    'pvrtc-rgb4unorm-webgl': { gl: 35840 },
    'pvrtc-rgba4unorm-webgl': { gl: 35842 },
    'pvrtc-rbg2unorm-webgl': { gl: 35841 },
    'pvrtc-rgba2unorm-webgl': { gl: 35843 },
    // WEBGL_compressed_texture_etc1
    'etc1-rbg-unorm-webgl': { gl: 36196 },
    // WEBGL_compressed_texture_atc
    'atc-rgb-unorm-webgl': { gl: 35986 },
    'atc-rgba-unorm-webgl': { gl: 35986 },
    'atc-rgbai-unorm-webgl': { gl: 34798 }
};
// FUNCTIONS
/** Checks if a texture format is supported */
export function isWebGLTextureFormatCapabilitiesed(gl, format, extensions) {
    const webglTextureInfo = WEBGL_TEXTURE_FORMATS[format];
    // Check that we have a GL constant
    if (!webglTextureInfo?.gl) {
        return false;
    }
    // Check extensions
    const extension = webglTextureInfo.x;
    if (extension) {
        return Boolean(getWebGLExtension(gl, extension, extensions));
    }
    return true;
}
/** Checks if a texture format is supported, renderable, filterable etc */
export function getTextureFormatCapabilitiesWebGL(gl, formatSupport, extensions) {
    let supported = formatSupport.create;
    const webglFormatInfo = WEBGL_TEXTURE_FORMATS[formatSupport.format];
    // Support Check that we have a GL constant
    if (webglFormatInfo?.gl === undefined) {
        supported = false;
    }
    if (webglFormatInfo?.x) {
        supported = supported && Boolean(getWebGLExtension(gl, webglFormatInfo.x, extensions));
    }
    return {
        format: formatSupport.format,
        // @ts-ignore
        create: supported && formatSupport.create,
        // @ts-ignore
        render: supported && formatSupport.render,
        // @ts-ignore
        filter: supported && formatSupport.filter,
        // @ts-ignore
        blend: supported && formatSupport.blend,
        // @ts-ignore
        store: supported && formatSupport.store
    };
}
/** Get parameters necessary to work with format in WebGL: internalFormat, dataFormat, type, compressed, */
export function getTextureFormatWebGL(format) {
    const formatData = WEBGL_TEXTURE_FORMATS[format];
    const webglFormat = convertTextureFormatToGL(format);
    const decoded = textureFormatDecoder.getInfo(format);
    if (decoded.compressed) {
        // TODO: Unclear whether this is always valid, this may be why ETC2 RGBA8 fails.
        formatData.dataFormat = webglFormat;
    }
    return {
        internalFormat: webglFormat,
        format: formatData?.dataFormat ||
            getWebGLPixelDataFormat(decoded.channels, decoded.integer, decoded.normalized, webglFormat),
        // depth formats don't have a type
        type: decoded.dataType
            ? getGLFromVertexType(decoded.dataType)
            : formatData?.types?.[0] || 5121,
        compressed: decoded.compressed || false
    };
}
export function getDepthStencilAttachmentWebGL(format) {
    const formatInfo = textureFormatDecoder.getInfo(format);
    switch (formatInfo.attachment) {
        case 'depth':
            return 36096;
        case 'stencil':
            return 36128;
        case 'depth-stencil':
            return 33306;
        default:
            throw new Error(`Not a depth stencil format: ${format}`);
    }
}
/** TODO - VERY roundabout legacy way of calculating bytes per pixel */
export function getTextureFormatBytesPerPixel(format) {
    const formatInfo = textureFormatDecoder.getInfo(format);
    return formatInfo.bytesPerPixel;
}
// DATA TYPE HELPERS
export function getWebGLPixelDataFormat(channels, integer, normalized, format) {
    // WebGL1 formats use same internalFormat
    if (format === 6408 || format === 6407) {
        return format;
    }
    // prettier-ignore
    switch (channels) {
        case 'r': return integer && !normalized ? 36244 : 6403;
        case 'rg': return integer && !normalized ? 33320 : 33319;
        case 'rgb': return integer && !normalized ? 36248 : 6407;
        case 'rgba': return integer && !normalized ? 36249 : 6408;
        case 'bgra': throw new Error('bgra pixels not supported by WebGL');
        default: return 6408;
    }
}
/**
 * Map WebGPU style texture format strings to GL constants
 */
function convertTextureFormatToGL(format) {
    const formatInfo = WEBGL_TEXTURE_FORMATS[format];
    const webglFormat = formatInfo?.gl;
    if (webglFormat === undefined) {
        throw new Error(`Unsupported texture format ${format}`);
    }
    return webglFormat;
}
//# sourceMappingURL=webgl-texture-table.js.map