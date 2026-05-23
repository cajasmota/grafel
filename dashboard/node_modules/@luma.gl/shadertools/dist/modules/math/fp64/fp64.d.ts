import { ShaderModule } from "../../../lib/shader-module/shader-module.js";
import { fp64ify, fp64LowPart, fp64ifyMatrix4 } from "../../../modules/math/fp64/fp64-utils.js";
type FP64Props = {};
type FP64Uniforms = {
    ONE: number;
};
type FP64Bindings = {};
type FP64Utilities = {
    fp64ify: typeof fp64ify;
    fp64LowPart: typeof fp64LowPart;
    fp64ifyMatrix4: typeof fp64ifyMatrix4;
};
/**
 * 64bit arithmetic: add, sub, mul, div (small subset of fp64 module)
 */
export declare const fp64arithmetic: ShaderModule<FP64Props, FP64Uniforms, FP64Bindings> & FP64Utilities;
/**
 * Full 64 bit math library
 */
export declare const fp64: ShaderModule<{}> & FP64Utilities;
export { fp64ify, fp64LowPart, fp64ifyMatrix4 };
//# sourceMappingURL=fp64.d.ts.map