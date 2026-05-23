import { Stats } from '@probe.gl/stats';
/**
 * Helper class managing a collection of probe.gl stats objects
 */
export declare class StatsManager {
    stats: Map<any, any>;
    getStats(name: string): Stats;
    get(name: string): Stats;
}
/** Global stats for all luma.gl devices */
export declare const lumaStats: StatsManager;
//# sourceMappingURL=stats-manager.d.ts.map