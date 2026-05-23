// WebGL2 Query (also handles disjoint timer extensions)
import { QuerySet } from '@luma.gl/core';
import { GL } from '@luma.gl/constants';
/**
 * Asynchronous queries for different kinds of information
 */
export class WEBGLQuerySet extends QuerySet {
    device;
    handle;
    target = null;
    _queryPending = false;
    _pollingPromise = null;
    get [Symbol.toStringTag]() {
        return 'Query';
    }
    // Create a query class
    constructor(device, props) {
        super(device, props);
        this.device = device;
        if (props.count > 1) {
            throw new Error('WebGL QuerySet can only have one value');
        }
        const handle = this.device.gl.createQuery();
        if (!handle) {
            throw new Error('WebGL query not supported');
        }
        this.handle = handle;
        Object.seal(this);
    }
    destroy() {
        this.device.gl.deleteQuery(this.handle);
    }
    // FOR RENDER PASS AND COMMAND ENCODER
    /**
     * Shortcut for timer query (dependent on extension in both WebGL1 and 2)
     * Measures GPU time delta between this call and a matching `end` call in the
     * GPU instruction stream.
     */
    beginTimestampQuery() {
        return this._begin(35007);
    }
    endTimestampQuery() {
        this._end();
    }
    // Shortcut for occlusion queries
    beginOcclusionQuery(options) {
        return this._begin(options?.conservative ? 36202 : 35887);
    }
    endOcclusionQuery() {
        this._end();
    }
    // Shortcut for transformFeedbackQuery
    beginTransformFeedbackQuery() {
        return this._begin(35976);
    }
    endTransformFeedbackQuery() {
        this._end();
    }
    async resolveQuery() {
        const value = await this.pollQuery();
        return [value];
    }
    // PRIVATE METHODS
    /**
     * Due to OpenGL API limitations, after calling `begin()` on one Query
     * instance, `end()` must be called on that same instance before
     * calling `begin()` on another query. While there can be multiple
     * outstanding queries representing disjoint `begin()`/`end()` intervals.
     * It is not possible to interleave or overlap `begin` and `end` calls.
     */
    _begin(target) {
        // Don't start a new query if one is already active.
        if (this._queryPending) {
            return;
        }
        this.target = target;
        this.device.gl.beginQuery(this.target, this.handle);
        return;
    }
    // ends the current query
    _end() {
        // Can't end a new query if the last one hasn't been resolved.
        if (this._queryPending) {
            return;
        }
        if (this.target) {
            this.device.gl.endQuery(this.target);
            this.target = null;
            this._queryPending = true;
        }
        return;
    }
    // Returns true if the query result is available
    isResultAvailable() {
        if (!this._queryPending) {
            return false;
        }
        const resultAvailable = this.device.gl.getQueryParameter(this.handle, 34919);
        if (resultAvailable) {
            this._queryPending = false;
        }
        return resultAvailable;
    }
    // Timing query is disjoint, i.e. results are invalid
    isTimerDisjoint() {
        return this.device.gl.getParameter(36795);
    }
    // Returns query result.
    getResult() {
        return this.device.gl.getQueryParameter(this.handle, 34918);
    }
    // Returns the query result, converted to milliseconds to match JavaScript conventions.
    getTimerMilliseconds() {
        return this.getResult() / 1e6;
    }
    // Polls the query
    pollQuery(limit = Number.POSITIVE_INFINITY) {
        if (this._pollingPromise) {
            return this._pollingPromise;
        }
        let counter = 0;
        this._pollingPromise = new Promise((resolve, reject) => {
            const poll = () => {
                if (this.isResultAvailable()) {
                    resolve(this.getResult());
                    this._pollingPromise = null;
                }
                else if (counter++ > limit) {
                    reject('Timed out');
                    this._pollingPromise = null;
                }
                else {
                    requestAnimationFrame(poll);
                }
            };
            requestAnimationFrame(poll);
        });
        return this._pollingPromise;
    }
}
//# sourceMappingURL=webgl-query-set.js.map