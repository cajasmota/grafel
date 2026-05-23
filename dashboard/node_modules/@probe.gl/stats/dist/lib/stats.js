// probe.gl, MIT license
import Stat from "./stat.js";
/** A "bag" of `Stat` objects, can be visualized using `StatsWidget` */
export default class Stats {
    constructor(options) {
        this.stats = {};
        this.id = options.id;
        this.stats = {};
        this._initializeStats(options.stats);
        Object.seal(this);
    }
    /** Acquire a stat. Create if it doesn't exist. */
    get(name, type = 'count') {
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
        this.forEach(stat => {
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
        stats.forEach(stat => this._getOrCreate(stat));
    }
    _getOrCreate(stat) {
        const { name, type } = stat;
        let result = this.stats[name];
        if (!result) {
            if (stat instanceof Stat) {
                result = stat;
            }
            else {
                result = new Stat(name, type);
            }
            this.stats[name] = result;
        }
        return result;
    }
}
//# sourceMappingURL=stats.js.map