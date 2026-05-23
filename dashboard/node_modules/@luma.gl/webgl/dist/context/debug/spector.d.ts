import type { Spector } from "./spector-types.js";
/** Spector debug initialization options */
type SpectorProps = {
    /** Whether spector.js is enabled */
    debugSpectorJS?: boolean;
    /** URL to load spector script from. Typically a CDN URL */
    debugSpectorJSUrl?: string;
    /** Canvas to monitor */
    gl?: WebGL2RenderingContext;
};
declare global {
    var SPECTOR: Spector;
}
export declare const DEFAULT_SPECTOR_PROPS: Required<SpectorProps>;
/** Loads spector from CDN if not already installed */
export declare function loadSpectorJS(props: {
    debugSpectorJSUrl?: string;
}): Promise<void>;
export declare function initializeSpectorJS(props: SpectorProps): Spector | null;
export {};
//# sourceMappingURL=spector.d.ts.map