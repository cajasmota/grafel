import { QuerySet, QuerySetProps } from '@luma.gl/core';
import { WebGLDevice } from "../webgl-device.js";
/**
 * Asynchronous queries for different kinds of information
 */
export declare class WEBGLQuerySet extends QuerySet {
    readonly device: WebGLDevice;
    readonly handle: WebGLQuery;
    target: number | null;
    _queryPending: boolean;
    _pollingPromise: Promise<any> | null;
    get [Symbol.toStringTag](): string;
    constructor(device: WebGLDevice, props: QuerySetProps);
    destroy(): void;
    /**
     * Shortcut for timer query (dependent on extension in both WebGL1 and 2)
     * Measures GPU time delta between this call and a matching `end` call in the
     * GPU instruction stream.
     */
    beginTimestampQuery(): void;
    endTimestampQuery(): void;
    beginOcclusionQuery(options?: {
        conservative?: boolean;
    }): void;
    endOcclusionQuery(): void;
    beginTransformFeedbackQuery(): void;
    endTransformFeedbackQuery(): void;
    resolveQuery(): Promise<bigint[]>;
    /**
     * Due to OpenGL API limitations, after calling `begin()` on one Query
     * instance, `end()` must be called on that same instance before
     * calling `begin()` on another query. While there can be multiple
     * outstanding queries representing disjoint `begin()`/`end()` intervals.
     * It is not possible to interleave or overlap `begin` and `end` calls.
     */
    protected _begin(target: number): void;
    protected _end(): void;
    isResultAvailable(): boolean;
    isTimerDisjoint(): boolean;
    getResult(): any;
    getTimerMilliseconds(): number;
    pollQuery(limit?: number): Promise<any>;
}
//# sourceMappingURL=webgl-query-set.d.ts.map