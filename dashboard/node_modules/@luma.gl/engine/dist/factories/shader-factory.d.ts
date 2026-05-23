import { Device, Shader, ShaderProps } from '@luma.gl/core';
/** Manages a cached pool of Shaders for reuse. */
export declare class ShaderFactory {
    static readonly defaultProps: Required<ShaderProps>;
    /** Returns the default ShaderFactory for the given {@link Device}, creating one if necessary. */
    static getDefaultShaderFactory(device: Device): ShaderFactory;
    readonly device: Device;
    readonly cachingEnabled: boolean;
    readonly destroyPolicy: 'unused' | 'never';
    readonly debug: boolean;
    private readonly _cache;
    get [Symbol.toStringTag](): string;
    toString(): string;
    /** @internal */
    constructor(device: Device);
    /** Requests a {@link Shader} from the cache, creating a new Shader only if necessary. */
    createShader(props: ShaderProps): Shader;
    /** Releases a previously-requested {@link Shader}, destroying it if no users remain. */
    release(shader: Shader): void;
    protected _hashShader(value: Shader | ShaderProps): string;
}
//# sourceMappingURL=shader-factory.d.ts.map