package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

func userKey(id int) string       { return fmt.Sprintf("user:%d", id) }
func sessionKey(id string) string { return "session:" + id }

func run(ctx context.Context, rdb *redis.Client) error {
	// Namespaced key patterns => coarse keyspaces.
	_ = "user:%d"
	_ = "session:{id}"
	_ = "cache:user:profile"

	if err := rdb.Set(ctx, userKey(1), "Ada", 0).Err(); err != nil {
		return err
	}

	if _, err := rdb.Get(ctx, userKey(1)).Result(); err != nil {
		return err
	}

	if err := rdb.HSet(ctx, "session:abc", "uid", "1").Err(); err != nil {
		return err
	}

	if _, err := rdb.HGetAll(ctx, sessionKey("abc")).Result(); err != nil {
		return err
	}

	rdb.Incr(ctx, "cache:user:profile")
	rdb.Expire(ctx, userKey(1), 0)
	rdb.Del(ctx, userKey(1))
	return nil
}
