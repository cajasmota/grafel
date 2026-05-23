import { Sampler, SamplerProps } from '@luma.gl/core';
import { GLSamplerParameters } from '@luma.gl/constants';
import type { WebGLDevice } from "../webgl-device.js";
/**
 * Sampler object -
 * so that they can be set directly on the texture
 * https://github.com/WebGLSamples/WebGL2Samples/blob/master/samples/sampler_object.html
 */
export declare class WEBGLSampler extends Sampler {
    readonly device: WebGLDevice;
    readonly handle: WebGLSampler;
    readonly parameters: GLSamplerParameters;
    constructor(device: WebGLDevice, props: SamplerProps);
    destroy(): void;
    toString(): string;
    /** Set sampler parameters on the sampler */
    private _setSamplerParameters;
}
//# sourceMappingURL=webgl-sampler.d.ts.map