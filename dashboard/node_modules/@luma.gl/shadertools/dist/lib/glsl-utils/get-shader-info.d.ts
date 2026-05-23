/** Information extracted from shader source code */
export type ShaderInfo = {
    name: string;
    language: 'glsl' | 'wgsl';
    version: number;
};
/** Extracts information from shader source code */
export declare function getShaderInfo(source: string, defaultName?: string): ShaderInfo;
//# sourceMappingURL=get-shader-info.d.ts.map