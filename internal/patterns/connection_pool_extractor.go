package patterns

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// connectionPoolExtractor detects database connection pool configuration.
// Matches Python connection_pool_extractor.py.
type connectionPoolExtractor struct{}

var (
	cpHikariTriggerRE      = regexp.MustCompile(`(?:HikariConfig|HikariDataSource|HikariCP|hikari\.)`)
	cpDruidTriggerRE       = regexp.MustCompile(`(?:DruidDataSource|DruidPooledConnection|com\.alibaba\.druid)`)
	cpPgbouncerTriggerRE   = regexp.MustCompile(`(?:pgbouncer|pool_mode|max_client_conn)`)
	cpSQLAlchemyTriggerRE  = regexp.MustCompile(`(?:create_engine\s*\(|pool_size|pool_timeout|NullPool|QueuePool)`)
	cpDjangoTriggerRE      = regexp.MustCompile(`(?:CONN_MAX_AGE|django_db_geventpool|django-db-geventpool)`)
	cpHikariMaxPoolRE      = regexp.MustCompile(`(?:maximumPoolSize|setMaximumPoolSize)\s*[=(]\s*(\d+)`)
	cpHikariMinIdleRE      = regexp.MustCompile(`(?:minimumIdle|setMinimumIdle)\s*[=(]\s*(\d+)`)
	cpSQLAlchemyPoolSizeRE = regexp.MustCompile(`pool_size\s*=\s*(\d+)`)
	cpSQLAlchemyTimeoutRE  = regexp.MustCompile(`pool_timeout\s*=\s*(\d+)`)
)

func (c *connectionPoolExtractor) Category() string { return "connection_pool" }

func (c *connectionPoolExtractor) AppliesTo(src string) bool {
	return cpHikariTriggerRE.MatchString(src) ||
		cpDruidTriggerRE.MatchString(src) ||
		cpPgbouncerTriggerRE.MatchString(src) ||
		cpSQLAlchemyTriggerRE.MatchString(src) ||
		cpDjangoTriggerRE.MatchString(src)
}

func (c *connectionPoolExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	if cpHikariTriggerRE.MatchString(src) {
		key := "hikari:0"
		if !seen[key] {
			seen[key] = true
			props := map[string]string{"kind": "connection_pool", "pool_type": "hikari"}
			if m := cpHikariMaxPoolRE.FindStringSubmatch(src); m != nil {
				props["max_pool_size"] = m[1]
			}
			if m := cpHikariMinIdleRE.FindStringSubmatch(src); m != nil {
				props["min_idle"] = m[1]
			}
			results = append(results, makeEntity(filePath,
				"connection_pool_hikari", "SCOPE.Config", "connection_pool", language, 1, props))
		}
	}

	if cpDruidTriggerRE.MatchString(src) {
		key := "druid:0"
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"connection_pool_druid", "SCOPE.Config", "connection_pool", language, 1,
				map[string]string{"kind": "connection_pool", "pool_type": "druid"}))
		}
	}

	if cpPgbouncerTriggerRE.MatchString(src) {
		key := "pgbouncer:0"
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"connection_pool_pgbouncer", "SCOPE.Config", "connection_pool", language, 1,
				map[string]string{"kind": "connection_pool", "pool_type": "pgbouncer"}))
		}
	}

	if cpSQLAlchemyTriggerRE.MatchString(src) {
		key := "sqlalchemy:0"
		if !seen[key] {
			seen[key] = true
			props := map[string]string{"kind": "connection_pool", "pool_type": "sqlalchemy"}
			if m := cpSQLAlchemyPoolSizeRE.FindStringSubmatch(src); m != nil {
				props["pool_size"] = m[1]
			}
			if m := cpSQLAlchemyTimeoutRE.FindStringSubmatch(src); m != nil {
				props["pool_timeout"] = m[1]
			}
			results = append(results, makeEntity(filePath,
				"connection_pool_sqlalchemy", "SCOPE.Config", "connection_pool", language, 1, props))
		}
	}

	if cpDjangoTriggerRE.MatchString(src) {
		key := "django:0"
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"connection_pool_django", "SCOPE.Config", "connection_pool", language, 1,
				map[string]string{"kind": "connection_pool", "pool_type": "django"}))
		}
	}

	return results
}

func init() {
	Register(&connectionPoolExtractor{})
}
