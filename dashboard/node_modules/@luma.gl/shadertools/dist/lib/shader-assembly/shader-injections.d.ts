export declare const DECLARATION_INJECT_MARKER = "__LUMA_INJECT_DECLARATIONS__";
/**
 *
 */
export type ShaderInjection = {
    injection: string;
    order: number;
};
/**
 *  ShaderInjections, parsed and split per shader
 */
export type ShaderInjections = {
    vertex: Record<string, ShaderInjection>;
    fragment: Record<string, ShaderInjection>;
};
/**
 *
 */
export declare function normalizeInjections(injections: Record<string, string | ShaderInjection>): ShaderInjections;
/**
// A minimal shader injection/templating system.
// RFC: https://github.com/visgl/luma.gl/blob/7.0-release/dev-docs/RFCs/v6.0/shader-injection-rfc.md
 * @param source
 * @param type
 * @param inject
 * @param injectStandardStubs
 * @returns
 */
export declare function injectShader(source: string, stage: 'vertex' | 'fragment', inject: Record<string, ShaderInjection[]>, injectStandardStubs?: boolean): string;
export declare function combineInjects(injects: any[]): Record<string, string>;
//# sourceMappingURL=shader-injections.d.ts.map