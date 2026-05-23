/**
 * Timeline channel properties
 * @param delay = 0;
 * @param duration = Number.POSITIVE_INFINITY;
 * @param rate = 1
 * @param repeat = 1
 */
export type ChannelOptions = {
    delay?: number;
    duration?: number;
    rate?: number;
    repeat?: number;
};
export type AnimationOptions = {
    setTime: (time: number) => void;
};
type Channel = {
    time: number;
    delay: number;
    duration: number;
    rate: number;
    repeat: number;
};
type Animation = {
    channel?: number;
    animation: {
        setTime: (time: number) => void;
    };
};
export declare class Timeline {
    time: number;
    channels: Map<number, Channel>;
    animations: Map<number, Animation>;
    playing: boolean;
    lastEngineTime: number;
    constructor();
    addChannel(props: ChannelOptions): number;
    removeChannel(channelId: number): void;
    isFinished(channelId: number): boolean;
    getTime(channelId?: number): number;
    setTime(time: number): void;
    play(): void;
    pause(): void;
    reset(): void;
    attachAnimation(animation: AnimationOptions, channelHandle?: number): number;
    detachAnimation(channelId: number): void;
    update(engineTime: number): void;
    _setChannelTime(channel: Channel, time: number): void;
}
export {};
//# sourceMappingURL=timeline.d.ts.map