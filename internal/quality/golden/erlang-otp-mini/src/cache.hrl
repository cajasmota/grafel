%% cache.hrl — shared record definitions for the cache application.

-record(cache_entry, {key, value, expires_at}).
-record(cache_stats, {hits = 0, misses = 0, size = 0}).
