<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# php

**Frameworks**: 16 · **Tools**: 6 · **ORMs**: 14 · **Other**: 0

Back to [summary](../summary.md).

### Legend

Each group column shows `glyph covered/applicable` — **covered** = capabilities with extraction, **applicable** = covered + missing (not-applicable capabilities are excluded from both). The glyph is the group's **support level**:

| Glyph | Level | Meaning |
|---|---|---|
| ✅ | **Comprehensive** | every applicable capability is `full` — fixture-proven, resolves the general case |
| 🟢 | **Supported** | every applicable capability is extracted; some only *heuristically* (detected by pattern, not full AST/data-flow resolution) |
| 🟡 | **Partial** | some capabilities extracted, some still missing |
| 🔴 | **Not extracted** | nothing extracted yet |
| — | **N/A** | capability does not apply to this framework |

Examples: `🟢 20/20` = fully supported, some capabilities heuristic · `🟡 12/20` = 8 not yet extracted. Detail pages use the same palette **per cell** (✅ full · 🟢 heuristic/partial · 🔴 missing · — n/a).

## Frameworks


### Backend HTTP

| Name | Routing | Auth | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|---|
| [API Platform](../detail/lang.php.framework.api-platform.md) | 🟡 3/6 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 1/24 | 🟡 1/12 | |
| [API Platform GraphQL](../detail/lang.php.framework.api-platform-graphql.md) | 🟡 3/6 | ✅ 1/1 | 🔴 0/4 | 🔴 0/1 | 🟡 2/24 | 🟡 1/13 | |
| [CakePHP](../detail/lang.php.framework.cakephp.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟡 22/25 | 🟡 6/11 | |
| [CodeIgniter](../detail/lang.php.framework.codeigniter.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟡 22/25 | 🟡 6/11 | |
| [Drupal](../detail/lang.php.framework.drupal.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟡 22/25 | 🟡 6/11 | |
| [Laminas (formerly Zend)](../detail/lang.php.framework.laminas.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟡 22/25 | 🟡 7/11 | |
| [Laravel](../detail/lang.php.framework.laravel.md) | 🟡 4/6 | ✅ 1/1 | ✅ 3/3 | ✅ 1/1 | 🟡 23/25 | 🟡 10/11 | |
| [Lighthouse](../detail/lang.php.framework.lighthouse.md) | 🟡 3/6 | ✅ 1/1 | 🔴 0/4 | 🔴 0/1 | 🟡 3/24 | 🟡 1/13 | |
| [Lumen](../detail/lang.php.framework.lumen.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟡 22/25 | 🟡 7/11 | |
| [Magento](../detail/lang.php.framework.magento.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟡 22/25 | 🟡 7/11 | |
| [Phalcon](../detail/lang.php.framework.phalcon.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟡 22/25 | 🟡 6/11 | |
| [Slim](../detail/lang.php.framework.slim.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟡 22/25 | 🟡 7/11 | |
| [Symfony](../detail/lang.php.framework.symfony.md) | 🟡 3/6 | ✅ 1/1 | ✅ 3/3 | ✅ 1/1 | 🟡 23/25 | 🟡 8/11 | |
| [WordPress](../detail/lang.php.framework.wordpress.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟡 22/25 | 🟡 6/11 | |
| [Yii](../detail/lang.php.framework.yii.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟡 22/25 | 🟡 6/11 | |
| [graphql-php](../detail/lang.php.framework.graphql-php.md) | 🟡 3/6 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 3/24 | 🟡 1/13 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [Behat](../detail/test.behat.md) | 🟢 | — | — | — | 🟢 | |
| [Codeception](../detail/test.codeception.md) | 🟢 | — | — | — | 🟢 | |
| [Composer](../detail/build.composer.md) | ✅ | — | — | — | ✅ | |
| [PHPUnit](../detail/test.phpunit.md) | ✅ | — | — | — | ✅ | |
| [Pest](../detail/test.pest.md) | 🟢 | — | — | — | 🟢 | |
| [composer.json](../detail/pkg.composer.md) | — | — | ✅ | ✅ | — | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [AWS SDK DynamoDB (PHP)](../detail/lang.php.driver.dynamodb.md) | 🟡 1/4 | |
| [CycleORM](../detail/lang.php.orm.cycleorm.md) | 🟡 8/11 | |
| [Doctrine ORM](../detail/lang.php.orm.doctrine.md) | 🟡 8/11 | |
| [Eloquent (Laravel)](../detail/lang.php.orm.eloquent.md) | 🟡 8/11 | |
| [PDO MySQL / mysqli](../detail/lang.php.driver.mysql.md) | 🟡 1/5 | |
| [PDO PostgreSQL](../detail/lang.php.driver.postgres.md) | 🟡 1/5 | |
| [PDO SQLite](../detail/lang.php.driver.sqlite.md) | 🟡 1/5 | |
| [Propel](../detail/lang.php.orm.propel.md) | 🟡 8/11 | |
| [RedBeanPHP](../detail/lang.php.orm.redbeanphp.md) | 🟡 5/8 | |
| [datastax/php-driver (Cassandra)](../detail/lang.php.driver.cassandra.md) | 🟡 1/4 | |
| [elasticsearch-php](../detail/lang.php.driver.elastic.md) | 🟡 1/4 | |
| [mongodb (PHP driver)](../detail/lang.php.driver.mongodb.md) | 🟡 2/5 | |
| [neo4j-php-client](../detail/lang.php.driver.neo4j.md) | 🟡 3/6 | |
| [phpredis / Predis](../detail/lang.php.driver.redis.md) | 🟡 1/4 | |
