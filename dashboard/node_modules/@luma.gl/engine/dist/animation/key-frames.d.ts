export type KeyFrame<T> = [number, T];
/** Holds a list of key frames (timestamped values) */
export declare class KeyFrames<T = number> {
    startIndex: number;
    endIndex: number;
    factor: number;
    times: number[];
    values: T[];
    private _lastTime;
    constructor(keyFrames: KeyFrame<T>[]);
    setKeyFrames(keyFrames: KeyFrame<T>[]): void;
    setTime(time: number): void;
    getStartTime(): number;
    getEndTime(): number;
    getStartData(): T;
    getEndData(): T;
    _calculateKeys(time: number): void;
}
//# sourceMappingURL=key-frames.d.ts.map