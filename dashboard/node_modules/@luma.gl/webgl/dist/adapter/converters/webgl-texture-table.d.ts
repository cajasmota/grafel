import type { DeviceFeature, TextureFormat, TextureFormatCapabilities, DeviceTextureFormatCapabilities } from '@luma.gl/core';
import { GL, GLPixelType, GLExtensions, GLTexelDataFormat } from '@luma.gl/constants';
export declare const TEXTURE_FEATURES: Partial<Record<DeviceFeature, string[]>>;
export declare function isTextureFeature(feature: DeviceFeature): boolean;
/** Checks a texture feature (for Device.features). Mainly compressed texture support */
export declare function checkTextureFeature(gl: WebGL2RenderingContext, feature: DeviceFeature, extensions: GLExtensions): boolean;
/** Map a format to webgl and constants */
type WebGLFormatInfo = {
    gl?: GL;
    /** compressed */
    x?: string;
    types?: GLPixelType[];
    dataFormat?: GLTexelDataFormat;
    /** if depthTexture is set this is a depth/stencil format that can be set to a texture  */
    depthTexture?: boolean;
    /** @deprecated can this format be used with renderbuffers */
    rb?: boolean;
};
/**
 * Texture format data -
 * Exported but can change without notice
 */
export declare const WEBGL_TEXTURE_FORMATS: Record<TextureFormat, WebGLFormatInfo>;
/** Checks if a texture format is supported */
export declare function isWebGLTextureFormatCapabilitiesed(gl: WebGL2RenderingContext, format: TextureFormat, extensions: GLExtensions): boolean;
/** Checks if a texture format is supported, renderable, filterable etc */
export declare function getTextureFormatCapabilitiesWebGL(gl: WebGL2RenderingContext, formatSupport: TextureFormatCapabilities, extensions: GLExtensions): DeviceTextureFormatCapabilities;
/** Get parameters necessary to work with format in WebGL: internalFormat, dataFormat, type, compressed, */
export declare function getTextureFormatWebGL(format: TextureFormat): {
    internalFormat: GL;
    format: GLTexelDataFormat;
    type: GLPixelType;
    compressed: boolean;
};
export declare function getDepthStencilAttachmentWebGL(format: TextureFormat): GL.DEPTH_ATTACHMENT | GL.STENCIL_ATTACHMENT | GL.DEPTH_STENCIL_ATTACHMENT;
/** TODO - VERY roundabout legacy way of calculating bytes per pixel */
export declare function getTextureFormatBytesPerPixel(format: TextureFormat): number;
export declare function getWebGLPixelDataFormat(channels: 'r' | 'rg' | 'rgb' | 'rgba' | 'bgra', integer: boolean, normalized: boolean, format: GL): GLTexelDataFormat;
export {};
//# sourceMappingURL=webgl-texture-table.d.ts.map