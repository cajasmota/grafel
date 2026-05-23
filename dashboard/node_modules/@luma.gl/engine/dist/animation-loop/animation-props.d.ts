import { CanvasContext, Device } from '@luma.gl/core';
import { Timeline } from "../animation/timeline.js";
import type { AnimationLoop } from "./animation-loop.js";
/** Properties passed to every render frame  */
export type AnimationProps = {
    animationLoop: AnimationLoop;
    device: Device;
    canvasContext: CanvasContext;
    canvas: HTMLCanvasElement | OffscreenCanvas;
    width: number;
    height: number;
    aspect: number;
    time: number;
    startTime: number;
    engineTime: number;
    tick: number;
    tock: number;
    needsRedraw?: string | false;
    timeline: Timeline | null;
    _mousePosition?: [number, number] | null;
};
//# sourceMappingURL=animation-props.d.ts.map