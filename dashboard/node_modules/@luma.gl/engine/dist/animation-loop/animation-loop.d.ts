import { Device } from '@luma.gl/core';
import { Timeline } from "../animation/timeline.js";
import { AnimationProps } from "./animation-props.js";
import { Stats, Stat } from '@probe.gl/stats';
/** AnimationLoop properties */
export type AnimationLoopProps = {
    device: Device | Promise<Device>;
    onAddHTML?: (div: HTMLDivElement) => string;
    onInitialize?: (animationProps: AnimationProps) => Promise<unknown>;
    onRender?: (animationProps: AnimationProps) => unknown;
    onFinalize?: (animationProps: AnimationProps) => void;
    onError?: (reason: Error) => void;
    stats?: Stats;
    autoResizeViewport?: boolean;
};
export type MutableAnimationLoopProps = {
    autoResizeViewport?: boolean;
};
/** Convenient animation loop */
export declare class AnimationLoop {
    static defaultAnimationLoopProps: {
        readonly device: never;
        readonly onAddHTML: () => string;
        readonly onInitialize: () => Promise<null>;
        readonly onRender: () => void;
        readonly onFinalize: () => void;
        readonly onError: (error: Error) => void;
        readonly stats: Stats;
        readonly autoResizeViewport: false;
    };
    device: Device | null;
    canvas: HTMLCanvasElement | OffscreenCanvas | null;
    props: Required<AnimationLoopProps>;
    animationProps: AnimationProps | null;
    timeline: Timeline | null;
    stats: Stats;
    cpuTime: Stat;
    gpuTime: Stat;
    frameRate: Stat;
    display: any;
    needsRedraw: string | false;
    _initialized: boolean;
    _running: boolean;
    _animationFrameId: any;
    _nextFramePromise: Promise<AnimationLoop> | null;
    _resolveNextFrame: ((animationLoop: AnimationLoop) => void) | null;
    _cpuStartTime: number;
    _error: Error | null;
    constructor(props: AnimationLoopProps);
    destroy(): void;
    /** @deprecated Use .destroy() */
    delete(): void;
    reportError(error: Error): void;
    /** Flags this animation loop as needing redraw */
    setNeedsRedraw(reason: string): this;
    setProps(props: MutableAnimationLoopProps): this;
    /** Starts a render loop if not already running */
    start(): Promise<this | null>;
    /** Stops a render loop if already running, finalizing */
    stop(): this;
    /** Explicitly draw a frame */
    redraw(): this;
    /** Add a timeline, it will be automatically updated by the animation loop. */
    attachTimeline(timeline: Timeline): Timeline;
    /** Remove a timeline */
    detachTimeline(): void;
    /** Wait until a render completes */
    waitForRender(): Promise<AnimationLoop>;
    /** TODO - should use device.deviceContext */
    toDataURL(): Promise<string>;
    _initialize(): void;
    _setDisplay(display: any): void;
    _requestAnimationFrame(): void;
    _cancelAnimationFrame(): void;
    _animationFrame(): void;
    _renderFrame(animationProps: AnimationProps): void;
    _clearNeedsRedraw(): void;
    _setupFrame(): void;
    _initializeAnimationProps(): void;
    _getAnimationProps(): AnimationProps;
    _updateAnimationProps(): void;
    /** Wait for supplied device */
    _initDevice(): Promise<void>;
    _createInfoDiv(): void;
    _getSizeAndAspect(): {
        width: number;
        height: number;
        aspect: number;
    };
    /** @deprecated Default viewport setup */
    _resizeViewport(): void;
    _beginFrameTimers(): void;
    _endFrameTimers(): void;
    _startEventHandling(): void;
    _onMousemove(event: Event): void;
    _onMouseleave(event: Event): void;
}
//# sourceMappingURL=animation-loop.d.ts.map