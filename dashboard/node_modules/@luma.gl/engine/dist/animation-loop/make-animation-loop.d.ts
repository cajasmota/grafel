import { Adapter } from '@luma.gl/core';
import { AnimationLoopTemplate } from "./animation-loop-template.js";
import { AnimationLoop, AnimationLoopProps } from "./animation-loop.js";
export type MakeAnimationLoopProps = Omit<AnimationLoopProps, 'onCreateDevice' | 'onInitialize' | 'onRedraw' | 'onFinalize'> & {
    /** List of adapters to use when creating the device */
    adapters?: Adapter[];
};
/**
 * Instantiates an animation loop and initializes it with the template.
 * @note The application needs to call `start()` on the returned animation loop to start the rendering loop.
 */
export declare function makeAnimationLoop(AnimationLoopTemplateCtor: typeof AnimationLoopTemplate, props?: MakeAnimationLoopProps): AnimationLoop;
//# sourceMappingURL=make-animation-loop.d.ts.map