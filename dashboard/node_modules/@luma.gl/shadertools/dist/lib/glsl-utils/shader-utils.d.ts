type QualifierInfo = {
    qualifier: string;
    type: string;
    name: string;
};
export declare function getQualifierDetails(line: string, qualifiers: string | string[]): QualifierInfo | null;
/**
 * Given the shader input and output variable names,
 * builds and return a pass through fragment shader.
 */
export declare function getPassthroughFS(options?: {
    input?: string;
    inputChannels?: 1 | 2 | 3 | 4;
    output?: string;
}): string;
/** convert glsl type to suffix */
export declare function typeToChannelSuffix(type: string): 'x' | 'xy' | 'xyz' | 'xyzw';
/** convert glsl type to channel count */
export declare function typeToChannelCount(type: string): 1 | 2 | 3 | 4;
/** Returns glsl instruction for converting to vec4 */
export declare function convertToVec4(variable: string, channels: 1 | 2 | 3 | 4): string;
export {};
//# sourceMappingURL=shader-utils.d.ts.map