import type { ShaderLayout } from '@luma.gl/core';
/**
 * Extracts a table suitable for `console.table()` from a shader layout to assist in debugging.
 * @param layout shader layout
 * @param name app should provide the most meaningful name, usually the model or pipeline name / id.
 * @returns
 */
export declare function getDebugTableForShaderLayout(layout: ShaderLayout, name: string): Record<string, Record<string, string>>;
//# sourceMappingURL=debug-shader-layout.d.ts.map