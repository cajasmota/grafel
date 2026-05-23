import type { TextureFormat, TextureFormatCompressed, TextureFormatInfo, TextureFormatCapabilities, TextureFormatColor } from "./texture-formats.js";
export declare class TextureFormatDecoder {
    /** Returns information about a texture format, e.g. attatchment type, components, byte length and flags (integer, signed, normalized) */
    getInfo(format: TextureFormat): TextureFormatInfo;
    /** Checks if a texture format is color */
    isColor(format: TextureFormat): format is TextureFormatColor;
    /** Checks if a texture format is depth or stencil */
    isDepthStencil(format: TextureFormat): boolean;
    /** Checks if a texture format is compressed */
    isCompressed(format: TextureFormat): format is TextureFormatCompressed;
    /**
     * Returns the "static" capabilities of a texture format.
     * @note Needs to be checked against current device
     */
    getCapabilities(format: TextureFormat): TextureFormatCapabilities;
}
export declare const textureFormatDecoder: TextureFormatDecoder;
//# sourceMappingURL=texture-format-decoder.d.ts.map