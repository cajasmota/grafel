var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __export = (target, all) => {
  for (var name in all)
    __defProp(target, name, { get: all[name], enumerable: true });
};
var __copyProps = (to, from, except, desc) => {
  if (from && typeof from === "object" || typeof from === "function") {
    for (let key of __getOwnPropNames(from))
      if (!__hasOwnProp.call(to, key) && key !== except)
        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
  }
  return to;
};
var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);

// dist/index.js
var dist_exports = {};
__export(dist_exports, {
  Stat: () => Stat,
  Stats: () => Stats,
  _getHiResTimestamp: () => getHiResTimestamp
});
module.exports = __toCommonJS(dist_exports);

// dist/utils/hi-res-timestamp.js
function getHiResTimestamp() {
  let timestamp;
  if (typeof window !== "undefined" && window.performance) {
    timestamp = window.performance.now();
  } else if (typeof process !== "undefined" && process.hrtime) {
    const timeParts = process.hrtime();
    timestamp = timeParts[0] * 1e3 + timeParts[1] / 1e6;
  } else {
    timestamp = Date.now();
  }
  return timestamp;
}

// dist/lib/stat.js
var Stat = class {
  constructor(name, type) {
    this.sampleSize = 1;
    this.time = 0;
    this.count = 0;
    this.samples = 0;
    this.lastTiming = 0;
    this.lastSampleTime = 0;
    this.lastSampleCount = 0;
    this._count = 0;
    this._time = 0;
    this._samples = 0;
    this._startTime = 0;
    this._timerPending = false;
    this.name = name;
    this.type = type;
    this.reset();
  }
  reset() {
    this.time = 0;
    this.count = 0;
    this.samples = 0;
    this.lastTiming = 0;
    this.lastSampleTime = 0;
    this.lastSampleCount = 0;
    this._count = 0;
    this._time = 0;
    this._samples = 0;
    this._startTime = 0;
    this._timerPending = false;
    return this;
  }
  setSampleSize(samples) {
    this.sampleSize = samples;
    return this;
  }
  /** Call to increment count (+1) */
  incrementCount() {
    this.addCount(1);
    return this;
  }
  /** Call to decrement count (-1) */
  decrementCount() {
    this.subtractCount(1);
    return this;
  }
  /** Increase count */
  addCount(value) {
    this._count += value;
    this._samples++;
    this._checkSampling();
    return this;
  }
  /** Decrease count */
  subtractCount(value) {
    this._count -= value;
    this._samples++;
    this._checkSampling();
    return this;
  }
  /** Add an arbitrary timing and bump the count */
  addTime(time) {
    this._time += time;
    this.lastTiming = time;
    this._samples++;
    this._checkSampling();
    return this;
  }
  /** Start a timer */
  timeStart() {
    this._startTime = getHiResTimestamp();
    this._timerPending = true;
    return this;
  }
  /** End a timer. Adds to time and bumps the timing count. */
  timeEnd() {
    if (!this._timerPending) {
      return this;
    }
    this.addTime(getHiResTimestamp() - this._startTime);
    this._timerPending = false;
    this._checkSampling();
    return this;
  }
  getSampleAverageCount() {
    return this.sampleSize > 0 ? this.lastSampleCount / this.sampleSize : 0;
  }
  /** Calculate average time / count for the previous window */
  getSampleAverageTime() {
    return this.sampleSize > 0 ? this.lastSampleTime / this.sampleSize : 0;
  }
  /** Calculate counts per second for the previous window */
  getSampleHz() {
    return this.lastSampleTime > 0 ? this.sampleSize / (this.lastSampleTime / 1e3) : 0;
  }
  getAverageCount() {
    return this.samples > 0 ? this.count / this.samples : 0;
  }
  /** Calculate average time / count */
  getAverageTime() {
    return this.samples > 0 ? this.time / this.samples : 0;
  }
  /** Calculate counts per second */
  getHz() {
    return this.time > 0 ? this.samples / (this.time / 1e3) : 0;
  }
  _checkSampling() {
    if (this._samples === this.sampleSize) {
      this.lastSampleTime = this._time;
      this.lastSampleCount = this._count;
      this.count += this._count;
      this.time += this._time;
      this.samples += this._samples;
      this._time = 0;
      this._count = 0;
      this._samples = 0;
    }
  }
};

// dist/lib/stats.js
var Stats = class {
  constructor(options) {
    this.stats = {};
    this.id = options.id;
    this.stats = {};
    this._initializeStats(options.stats);
    Object.seal(this);
  }
  /** Acquire a stat. Create if it doesn't exist. */
  get(name, type = "count") {
    return this._getOrCreate({ name, type });
  }
  get size() {
    return Object.keys(this.stats).length;
  }
  /** Reset all stats */
  reset() {
    for (const stat of Object.values(this.stats)) {
      stat.reset();
    }
    return this;
  }
  forEach(fn) {
    for (const stat of Object.values(this.stats)) {
      fn(stat);
    }
  }
  getTable() {
    const table = {};
    this.forEach((stat) => {
      table[stat.name] = {
        time: stat.time || 0,
        count: stat.count || 0,
        average: stat.getAverageTime() || 0,
        hz: stat.getHz() || 0
      };
    });
    return table;
  }
  _initializeStats(stats = []) {
    stats.forEach((stat) => this._getOrCreate(stat));
  }
  _getOrCreate(stat) {
    const { name, type } = stat;
    let result = this.stats[name];
    if (!result) {
      if (stat instanceof Stat) {
        result = stat;
      } else {
        result = new Stat(name, type);
      }
      this.stats[name] = result;
    }
    return result;
  }
};
//# sourceMappingURL=index.cjs.map
