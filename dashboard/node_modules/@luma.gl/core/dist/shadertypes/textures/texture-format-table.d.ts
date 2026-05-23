import { TextureFormat, TextureFeature, TextureFormatInfo } from "./texture-formats.js";
/** https://www.w3.org/TR/webgpu/#texture-format-caps */
/** Internal type representing texture capabilities */
type TextureFormatDefinition = Partial<TextureFormatInfo> & {
    /** for compressed texture formats */
    f?: TextureFeature;
    /** renderable if feature is present. false means the spec does not support this format */
    render?: TextureFeature | false;
    /** filterable if feature is present. false means the spec does not support this format */
    filter?: TextureFeature | false;
    blend?: TextureFeature | false;
    store?: TextureFeature | false;
    /** (bytes per pixel), for memory usage calculations. */
    b?: number;
    /** channels */
    c?: number;
    bpp?: number;
    /** packed */
    p?: number;
    /** If not supported on WebGPU */
    wgpu?: false;
};
export declare function getTextureFormatDefinition(format: TextureFormat): TextureFormatDefinition;
export declare function getTextureFormatTable(): Readonly<Record<TextureFormat, TextureFormatDefinition>>;
export declare const TEXTURE_FORMAT_TABLE: Readonly<Record<TextureFormat, TextureFormatDefinition>>;
export {};
//# sourceMappingURL=texture-format-table.d.ts.map