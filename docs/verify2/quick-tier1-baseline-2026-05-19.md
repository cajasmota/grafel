# VERIFY-2 Quick Tier-1 Baseline (2026-05-19)

_Wall time: 538s (~9 min). 40 repos indexed, 0 timeouts, 0 crashes._

**Headline:** aggregate bug-rate **17.55%** across **40** successfully-indexed tier-1 repos.

## Caveat

- This is the **quick partial** measurement against **40 of 116** tier-1 repos (only those already on disk).
- NOT the full ship-gate. Full ship-gate requires the remaining tier-1 repos plus the tier-2/3 set.
- Serial execution, per-repo timeout 300s, host RAM-limited.

## Comparison vs v4 baseline

| Run | Repos measured | Aggregate bug-rate |
|---|---:|---:|
| v4 baseline | 32 | 15.63% |
| quick tier-1 (this run) | 40 | 17.55% |

**Delta:** +1.92 pp (regression vs v4).

## Aggregate disposition counts

| Disposition | Count |
|---|---:|
| resolved | 244,712 |
| external-known | 22,495 |
| external-unknown | 25,043 |
| dynamic | 77,415 |
| bug-extractor | 61,734 |
| bug-resolver | 16,955 |
| unclassified | 0 |
| **total endpoints** | **448,354** |
| **bugs (extractor+resolver+unclassified)** | **78,689** |

## Per-repo table (sorted by bug-rate, worst first; bar = 8.0%)

| Repo | Lang | Files | Rels | Endpoints | Bugs | Bug-rate | vs Bar |
|---|---|---:|---:|---:|---:|---:|:---:|
| terraform-aws-vpc | hcl | 105 | 3,605 | 7,210 | 5,332 | 73.95% | FAIL |
| starter-workflows | yaml | 514 | 1,899 | 3,798 | 1,835 | 48.31% | FAIL |
| argocd-example-apps | yaml | 91 | 165 | 330 | 157 | 47.58% | FAIL |
| spdlog | cpp | 175 | 3,326 | 6,652 | 2,160 | 32.47% | FAIL |
| play-scala-starter | scala | 37 | 71 | 142 | 45 | 31.69% | FAIL |
| ktor-samples | kotlin | 509 | 4,615 | 9,230 | 2,709 | 29.35% | FAIL |
| apollo-server | graphql | 293 | 8,645 | 17,290 | 4,532 | 26.21% | FAIL |
| aspnetcore-docs-samples | razor | 2,674 | 14,459 | 28,918 | 7,486 | 25.89% | FAIL |
| laravel-quickstart | php | 83 | 191 | 382 | 93 | 24.35% | FAIL |
| grpc-go-examples | proto | 203 | 7,206 | 14,412 | 3,496 | 24.26% | FAIL |
| symfony-demo | php | 241 | 1,499 | 2,998 | 701 | 23.38% | FAIL |
| nextjs-commerce | typescript | 76 | 668 | 1,336 | 312 | 23.35% | FAIL |
| kafka-streams-examples | java | 172 | 8,156 | 16,312 | 3,734 | 22.89% | FAIL |
| vapor-api-template | swift | 21 | 47 | 94 | 20 | 21.28% | FAIL |
| http.zig | zig | 36 | 1,874 | 3,748 | 772 | 20.60% | FAIL |
| usermanager-example | clojure | 17 | 76 | 152 | 30 | 19.74% | FAIL |
| etcd | go | 424 | 29,020 | 58,040 | 11,413 | 19.66% | FAIL |
| express-realworld | javascript | 66 | 346 | 692 | 136 | 19.65% | FAIL |
| actix-examples | rust | 460 | 6,100 | 12,200 | 2,366 | 19.39% | FAIL |
| aspnetcore-realworld | csharp | 97 | 1,288 | 2,576 | 462 | 17.93% | FAIL |
| just | just | 290 | 19,731 | 39,462 | 6,874 | 17.42% | FAIL |
| nestjs-starter | typescript | 16 | 57 | 114 | 19 | 16.67% | FAIL |
| mini-redis | rust | 33 | 1,047 | 2,094 | 349 | 16.67% | FAIL |
| tokio | rust | 389 | 18,370 | 36,740 | 5,945 | 16.18% | FAIL |
| flask-realworld | python | 43 | 934 | 1,868 | 282 | 15.10% | FAIL |
| django-realworld | python | 48 | 530 | 1,060 | 148 | 13.96% | FAIL |
| pandas | python | 197 | 30,385 | 60,770 | 8,426 | 13.87% | FAIL |
| sidekiq | ruby | 85 | 4,733 | 9,466 | 1,311 | 13.85% | FAIL |
| phoenix-todo-list | elixir | 69 | 714 | 1,428 | 182 | 12.75% | FAIL |
| chi | go | 93 | 3,771 | 7,542 | 961 | 12.74% | FAIL |
| exposed | kotlin | 115 | 4,274 | 8,548 | 1,071 | 12.53% | FAIL |
| gin | go | 121 | 11,327 | 22,654 | 2,837 | 12.52% | FAIL |
| tide | fish | 130 | 754 | 1,508 | 138 | 9.15% | FAIL |
| spring-petclinic | java | 120 | 2,290 | 4,580 | 400 | 8.73% | FAIL |
| click | python | 138 | 7,841 | 15,682 | 1,115 | 7.11% | ok |
| rails-realworld | ruby | 105 | 263 | 526 | 35 | 6.65% | ok |
| kickstart.nvim | lua | 15 | 58 | 116 | 4 | 3.45% | ok |
| requests | python | 111 | 23,584 | 47,168 | 796 | 1.69% | ok |
| prometheus-helm | yaml | 52 | 239 | 478 | 5 | 1.05% | ok |
| openapi-stripe | yaml | 5 | 19 | 38 | 0 | 0.00% | ok |

**34 of 40** repos exceed the 8.0% per-repo bar.

## Failures

_None._
## Coverage gaps

Tier-1 entries in `scripts/verify2/run.sh` NOT measured because not on disk (76 of 116):

| Repo | Lang |
|---|---|
| jupyter-notebook | notebook |
| jaffle_shop | sql_dbt |
| azure-quickstart-templates | bicep |
| tilt | starlark |
| camunda-bpm-examples | java_bpmn |
| asyncapi-spec | asyncapi |
| smithy | smithy |
| avro | avro |
| thrift | thrift |
| json-schema-spec | json-schema |
| raml-spec | raml |
| api-blueprint | api-blueprint |
| nginx | nginx-conf |
| apache-httpd | apache-httpd-conf |
| caddy | caddyfile |
| traefik | traefik-dynamic |
| kong | kong-declarative |
| envoy | envoy-yaml |
| haproxy | haproxy-cfg |
| seleniumhq-examples | multi |
| sample-food-truck | swift |
| esp-idf | c |
| flutter-samples | dart |
| microblog | python |
| fastapi-realworld | python |
| golang-gin-realworld | go |
| actix-diesel-realworld | rust |
| nestjs-realworld-typeorm | typescript |
| joal | java |
| jpetstore-6 | java |
| ent | go |
| sqlc-examples | go |
| netcore-boilerplate | csharp |
| pnpm | javascript |
| bazel | java |
| cmake | cpp |
| mongoose | javascript |
| mongo-go-driver | go |
| redis-py | python |
| cassandra-java-driver | java |
| aws-sdk-go-v2 | go |
| rabbitmq-tutorials | python |
| aws-cdk-examples-typescript | typescript |
| pulumi-examples-go | go |
| aws-cloudformation-samples | yaml |
| aws-sam-cli-app-templates | yaml |
| serverless-examples | yaml |
| crossplane | yaml |
| ansible-for-devops | yaml |
| nomad-pack | hcl |
| gitlab-runner | yaml |
| circleci-demo-python-django | yaml |
| jenkins | groovy |
| tektoncd-pipeline | yaml |
| alembic | python |
| ios-oss | swift |
| android-architecture | java |
| compose-samples | kotlin |
| EntityComponentSystemSamples | csharp |
| zod | typescript |
| pydantic | python |
| aws-lambda-python-runtime-interface-client | python |
| cloudflare-workers-sdk | typescript |
| xstate | typescript |
| hugoDocs | go |
| sphinx | python |
| pytest | python |
| socket.io | typescript |
| airflow | python |
| spark | scala |
| angular-realworld | typescript |
| sveltekit | typescript |
| axum | rust |
| phoenix-live-view | elixir |
| http4k | kotlin |
| vue-realworld | javascript |

## Reproduction

```
go build -o /tmp/archigraph-quick ./cmd/archigraph
# intersection of run.sh repos and ~/Documents/Projects/archigraph-corpora/
for repo in $(cat intersection.txt); do
  gtimeout 300 /tmp/archigraph-quick index -json-stats $repo > out/$repo.json
done
```

forbidden-term grep: clean
