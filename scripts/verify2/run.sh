#!/usr/bin/env bash
# scripts/verify2/run.sh
#
# VERIFY-2 (Refs #58, #88, #482) — bug-rate / resolution-rate measurement harness.
#
# Clones a small set of public OSS repositories into
# $ARCHIGRAPH_CORPORA_DIR (default: $HOME/Documents/Projects/archigraph-corpora)
# and runs `archigraph index --json-stats` over each. Aggregates the
# per-disposition counts and writes a Markdown report into
# $ARCHIGRAPH_CORPORA_DIR/_reports/<ISO-timestamp>.md.
#
# This script never writes inside the archigraph repo. The corpora and
# reports live entirely outside it so we don't blow up the worktree with
# vendored third-party source.
#
# Usage:
#   scripts/verify2/run.sh [--runs N]
#
# Flag:
#   --runs N   Index each repo N times and report median bug_rate + min/max
#              range per repo (Refs #482). Aggregation uses medians, not
#              single-shot values, eliminating ±3-5pp noise on small repos.
#              N=1 restores single-shot behaviour.  Default: 5.
#              Short-circuit: if the first 3 runs of a repo all land within
#              0.5pp of each other the remaining runs are skipped to avoid
#              the full 5x cost on stable repos.
#
# Env vars:
#   ARCHIGRAPH_CORPORA_DIR   target dir for clones + reports
#                            (default: $HOME/Documents/Projects/archigraph-corpora)
#   ARCHIGRAPH_BIN           path to archigraph binary (default: ./archigraph
#                            built ad-hoc into the corpora dir)
#   ARCHIGRAPH_VERBOSE       set to 1 to forward verbose stderr from indexer
#   ARCHIGRAPH_VERIFY2_RUNS  number of indexer runs per repo (default: 5;
#                            overridden by --runs flag)
set -euo pipefail

# ---------------------------------------------------------------------------
# Parse --runs flag; all other args are ignored.
# ---------------------------------------------------------------------------
RUNS="${ARCHIGRAPH_VERIFY2_RUNS:-5}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --runs)    RUNS="${2:?--runs requires an integer value}"; shift 2 ;;
    --runs=*)  RUNS="${1#--runs=}"; shift ;;
    *)         shift ;;
  esac
done
if ! [[ "$RUNS" =~ ^[0-9]+$ ]] || [[ "$RUNS" -lt 1 ]]; then
  echo "error: --runs must be a positive integer (got '$RUNS')" >&2
  exit 1
fi

CORPORA_DIR="${ARCHIGRAPH_CORPORA_DIR:-$HOME/Documents/Projects/archigraph-corpora}"
REPORTS_DIR="$CORPORA_DIR/_reports"
mkdir -p "$CORPORA_DIR" "$REPORTS_DIR"

# Repo list. Keep entries public and well-known. Each entry is:
#   <name>|<git-url>|<ref>|<primary-language>[|<sparse-path>]
#
# The optional 5th field selects a single sub-tree via partial clone +
# git sparse-checkout (cone mode). This is REQUIRED for monorepos where
# a full clone exceeds ~200 MB at HEAD on the chosen ref — the comment
# next to each entry records the rough estimate at the time of authoring.
#
# Coverage targets the full 32-language extractor matrix + frameworks +
# ORMs + manifests + tools. Stack-characteristic diversity (Refs #87, #96):
#   - ORM-heavy             rails-realworld, django-realworld
#   - HTTP routing          gin, chi, express-realworld, actix-examples, vapor
#   - microservice / RPC    etcd, kafka-streams-examples
#   - CLI tool              click
#   - config-heavy          pandas (mixed), spring-petclinic, nestjs-starter
REPOS=(
  # --- Single-source MUST KEEP (extractor or DSL only-source) ---
  "aspnetcore-docs-samples|https://github.com/dotnet/AspNetCore.Docs.Samples.git|main|razor"
  "tide|https://github.com/IlanCosman/tide.git|main|fish"
  "just|https://github.com/casey/just.git|master|just"
  "http.zig|https://github.com/karlseguin/http.zig.git|master|zig"
  "kickstart.nvim|https://github.com/nvim-lua/kickstart.nvim.git|master|lua"
  "grpc-go-examples|https://github.com/grpc/grpc-go.git|master|proto|examples"
  "apollo-server|https://github.com/apollographql/apollo-server.git|main|graphql"
  "jupyter-notebook|https://github.com/jupyter/notebook.git|main|notebook|docs/source"
  "jaffle_shop|https://github.com/dbt-labs/jaffle_shop.git|main|sql_dbt|models"
  "azure-quickstart-templates|https://github.com/Azure/azure-quickstart-templates.git|master|bicep|quickstarts/microsoft.storage"
  "tilt|https://github.com/tilt-dev/tilt.git|master|starlark|integration"
  "camunda-bpm-examples|https://github.com/camunda/camunda-bpm-examples.git|master|java_bpmn|servicetask"
  "asyncapi-spec|https://github.com/asyncapi/spec.git|master|asyncapi"
  "smithy|https://github.com/smithy-lang/smithy.git|main|smithy|smithy-model"
  "avro|https://github.com/apache/avro.git|main|avro|lang"
  "thrift|https://github.com/apache/thrift.git|master|thrift|tutorial"
  "json-schema-spec|https://github.com/json-schema-org/json-schema-spec.git|main|json-schema"
  "raml-spec|https://github.com/raml-org/raml-spec.git|master|raml"
  "api-blueprint|https://github.com/apiaryio/api-blueprint.git|master|api-blueprint"
  "nginx|https://github.com/nginx/nginx.git|master|nginx-conf|conf"
  "apache-httpd|https://github.com/apache/httpd.git|trunk|apache-httpd-conf|docs/conf"
  "caddy|https://github.com/caddyserver/caddy.git|master|caddyfile|caddyconfig"
  "traefik|https://github.com/traefik/traefik.git|master|traefik-dynamic|integration/fixtures"
  "kong|https://github.com/Kong/kong.git|master|kong-declarative|spec/fixtures"
  "envoy|https://github.com/envoyproxy/envoy.git|main|envoy-yaml|configs"
  "haproxy|https://github.com/haproxy/haproxy.git|master|haproxy-cfg|examples"
  "seleniumhq-examples|https://github.com/SeleniumHQ/seleniumhq.github.io.git|trunk|multi|examples"
  # --- Language primary realworld apps ---
  "requests|https://github.com/psf/requests.git|main|python"
  "flask-realworld|https://github.com/gothinkster/flask-realworld-example-app.git|master|python"
  "click|https://github.com/pallets/click.git|main|python"
  "django-realworld|https://github.com/gothinkster/django-realworld-example-app.git|master|python"
  "pandas|https://github.com/pandas-dev/pandas.git|main|python|pandas/core"
  "gin|https://github.com/gin-gonic/gin.git|master|go"
  "chi|https://github.com/go-chi/chi.git|master|go"
  "etcd|https://github.com/etcd-io/etcd.git|main|go|server/etcdserver"
  "express-realworld|https://github.com/gothinkster/node-express-realworld-example-app.git|master|javascript"
  "nestjs-starter|https://github.com/nestjs/typescript-starter.git|master|typescript"
  "nextjs-commerce|https://github.com/vercel/commerce.git|main|typescript"
  "spring-petclinic|https://github.com/spring-projects/spring-petclinic.git|main|java"
  "kafka-streams-examples|https://github.com/confluentinc/kafka-streams-examples.git|master|java"
  "exposed|https://github.com/JetBrains/Exposed.git|main|kotlin"
  "ktor-samples|https://github.com/ktorio/ktor-samples.git|main|kotlin"
  "play-scala-starter|https://github.com/playframework/play-scala-starter-example.git|2.7.x|scala"
  "usermanager-example|https://github.com/seancorfield/usermanager-example.git|develop|clojure"
  "rails-realworld|https://github.com/gothinkster/rails-realworld-example-app.git|master|ruby"
  "sidekiq|https://github.com/sidekiq/sidekiq.git|main|ruby"
  "laravel-quickstart|https://github.com/laravel/quickstart-basic.git|master|php"
  "symfony-demo|https://github.com/symfony/demo.git|main|php"
  "mini-redis|https://github.com/tokio-rs/mini-redis.git|master|rust"
  "actix-examples|https://github.com/actix/examples.git|main|rust"
  "vapor-api-template|https://github.com/vapor/api-template.git|master|swift"
  "sample-food-truck|https://github.com/apple/sample-food-truck.git|main|swift"
  "aspnetcore-realworld|https://github.com/gothinkster/aspnetcore-realworld-example-app.git|master|csharp"
  "spdlog|https://github.com/gabime/spdlog.git|v1.x|cpp"
  "esp-idf|https://github.com/espressif/esp-idf.git|master|c|examples/get-started"  # pure-C corpus; cpp extractor registers for both "c" and "cpp" languages via tree-sitter-c
  "flutter-samples|https://github.com/flutter/samples.git|main|dart"
  "phoenix-todo-list|https://github.com/dwyl/phoenix-todo-list-tutorial.git|main|elixir"
  # --- Secondary distinctive coverage ---
  "microblog|https://github.com/miguelgrinberg/microblog.git|main|python"                       # SQLAlchemy + Flask-Login (deeper than flask-realworld)
  "fastapi-realworld|https://github.com/nsidnev/fastapi-realworld-example-app.git|master|python" # FastAPI + Pydantic + async SQLAlchemy
  "golang-gin-realworld|https://github.com/gothinkster/golang-gin-realworld-example-app.git|main|go" # GORM
  "actix-diesel-realworld|https://github.com/snamiki1212/realworld-v1-rust-actix-web-diesel.git|main|rust" # Diesel
  "nestjs-realworld-typeorm|https://github.com/lujakob/nestjs-realworld-example-app.git|master|typescript" # TypeORM
  "joal|https://github.com/anthonyraymond/joal.git|master|java"                                 # jOOQ
  "jpetstore-6|https://github.com/mybatis/jpetstore-6.git|master|java"                          # MyBatis
  "ent|https://github.com/ent/ent.git|master|go|entc/integration/ent"                           # Ent codegen
  "sqlc-examples|https://github.com/sqlc-dev/sqlc.git|main|go|examples"                         # sqlc codegen
  "netcore-boilerplate|https://github.com/lkurzyniec/netcore-boilerplate.git|master|csharp"     # Dapper micro-ORM
  "tokio|https://github.com/tokio-rs/tokio.git|master|rust"                                     # Cargo workspace manifest
  "pnpm|https://github.com/pnpm/pnpm.git|main|javascript"                                       # pnpm-workspace.yaml
  "bazel|https://github.com/bazelbuild/bazel.git|master|java"                                   # BUILD Starlark targets
  "cmake|https://github.com/Kitware/CMake.git|master|cpp"                                       # CMake lists
  "mongoose|https://github.com/Automattic/mongoose.git|master|javascript"                       # Mongo Node ODM
  "mongo-go-driver|https://github.com/mongodb/mongo-go-driver.git|master|go"                    # Mongo Go
  "redis-py|https://github.com/redis/redis-py.git|master|python"                                # Redis Python
  "cassandra-java-driver|https://github.com/datastax/java-driver.git|4.x|java"                  # Cassandra CQL
  "aws-sdk-go-v2|https://github.com/aws/aws-sdk-go-v2.git|main|go"                              # DynamoDB / AWS SDK
  "rabbitmq-tutorials|https://github.com/rabbitmq/rabbitmq-tutorials.git|main|python"           # AMQP
  "aws-cdk-examples-typescript|https://github.com/aws-samples/aws-cdk-examples.git|main|typescript|typescript"
  "pulumi-examples-go|https://github.com/pulumi/examples.git|master|go|aws-go-webserver"
  "aws-cloudformation-samples|https://github.com/aws-cloudformation/aws-cloudformation-samples.git|main|yaml"
  "aws-sam-cli-app-templates|https://github.com/aws/aws-sam-cli-app-templates.git|master|yaml|python3.12"
  "serverless-examples|https://github.com/serverless/examples.git|v4|yaml|aws-node-http-api"
  "crossplane|https://github.com/crossplane/crossplane.git|main|yaml|cluster/meta"
  "ansible-for-devops|https://github.com/geerlingguy/ansible-for-devops.git|master|yaml"
  "nomad-pack|https://github.com/hashicorp/nomad-pack.git|main|hcl|registry"
  "terraform-aws-vpc|https://github.com/terraform-aws-modules/terraform-aws-vpc.git|master|hcl"
  "argocd-example-apps|https://github.com/argoproj/argocd-example-apps.git|master|yaml"
  "prometheus-helm|https://github.com/prometheus-community/helm-charts.git|main|yaml|charts/prometheus"
  "starter-workflows|https://github.com/actions/starter-workflows.git|main|yaml"
  "openapi-stripe|https://github.com/APIs-guru/openapi-directory.git|main|yaml|APIs/stripe.com"
  "gitlab-runner|https://gitlab.com/gitlab-org/gitlab-runner.git|main|yaml|ci"
  "circleci-demo-python-django|https://github.com/CircleCI-Public/circleci-demo-python-django.git|master|yaml|.circleci"
  "jenkins|https://github.com/jenkinsci/jenkins.git|master|groovy"
  "tektoncd-pipeline|https://github.com/tektoncd/pipeline.git|main|yaml|examples"
  "alembic|https://github.com/sqlalchemy/alembic.git|main|python"
  "ios-oss|https://github.com/kickstarter/ios-oss.git|main|swift"
  "android-architecture|https://github.com/googlesamples/android-architecture.git|main|java"
  "compose-samples|https://github.com/android/compose-samples.git|main|kotlin"
  "EntityComponentSystemSamples|https://github.com/Unity-Technologies/EntityComponentSystemSamples.git|master|csharp|EntityComponentSystemSamples/ECSSamples"
  "zod|https://github.com/colinhacks/zod.git|main|typescript"
  "pydantic|https://github.com/pydantic/pydantic.git|main|python"
  "aws-lambda-python-runtime-interface-client|https://github.com/aws/aws-lambda-python-runtime-interface-client.git|main|python|awslambdaric"
  "cloudflare-workers-sdk|https://github.com/cloudflare/workers-sdk.git|main|typescript|packages/wrangler"
  "xstate|https://github.com/statelyai/xstate.git|main|typescript|packages/core"
  "hugoDocs|https://github.com/gohugoio/hugoDocs.git|master|go|content"
  "sphinx|https://github.com/sphinx-doc/sphinx.git|master|python|sphinx"
  "pytest|https://github.com/pytest-dev/pytest.git|main|python"
  "socket.io|https://github.com/socketio/socket.io.git|main|typescript|packages/socket.io"
  "airflow|https://github.com/apache/airflow.git|main|python|airflow/example_dags"
  "spark|https://github.com/apache/spark.git|master|scala|examples/src/main/scala"
  "angular-realworld|https://github.com/gothinkster/angular-realworld-example-app.git|main|typescript"
  "sveltekit|https://github.com/sveltejs/kit.git|main|typescript"
  "axum|https://github.com/tokio-rs/axum.git|main|rust"
  "phoenix-live-view|https://github.com/phoenixframework/phoenix_live_view.git|main|elixir"
  "http4k|https://github.com/http4k/http4k.git|master|kotlin"
  # CONDITIONAL: uncomment if Vue SFC extractor is a separate code path
  # "vue-realworld|https://github.com/gothinkster/vue-realworld-example-app.git|master|javascript"
)

# Locate or build the archigraph binary. We build into the corpora dir
# (outside the repo) so this script is safe to run from any worktree.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

if [[ -n "${ARCHIGRAPH_BIN:-}" ]]; then
  BIN="$ARCHIGRAPH_BIN"
else
  BIN="$CORPORA_DIR/_bin/archigraph"
  mkdir -p "$(dirname "$BIN")"
  echo "==> building archigraph -> $BIN" >&2
  ( cd "$REPO_ROOT" && go build -o "$BIN" ./cmd/archigraph )
fi

if [[ ! -x "$BIN" ]]; then
  echo "archigraph binary not executable: $BIN" >&2
  exit 1
fi

TIMESTAMP="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
REPORT="$REPORTS_DIR/$TIMESTAMP.md"
# Per-run scratch dir lives under _reports/ (NOT mktemp) so partial state
# survives abort/SIGKILL/timeout — earlier behavior wiped per-repo JSON on
# EXIT, making partial runs unsalvageable. The dir is only deleted at the
# end of a successful run, gated by a `.complete` sentinel written by the
# final aggregation step. Anything without a sentinel is preserved for
# post-mortem; operators can `rm -rf` stale `*-partial/` dirs by hand.
TMPDIR_AGG="$REPORTS_DIR/${TIMESTAMP}-partial"
mkdir -p "$TMPDIR_AGG"
trap '[[ -f "$TMPDIR_AGG/.complete" ]] && rm -rf "$TMPDIR_AGG"' EXIT

# Optional per-repo wall-clock cap (seconds). Set ARCHIGRAPH_VERIFY2_TIMEOUT=0
# to disable. Uses gtimeout (coreutils) if available, then timeout, then
# silently skips capping on systems with neither.
PER_REPO_TIMEOUT="${ARCHIGRAPH_VERIFY2_TIMEOUT:-600}"
TIMEOUT_BIN=""
if command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_BIN="gtimeout"
elif command -v timeout >/dev/null 2>&1; then
  TIMEOUT_BIN="timeout"
fi

# Write the language manifest used by the per-language aggregation step.
LANG_MANIFEST="$TMPDIR_AGG/_languages.tsv"
: >"$LANG_MANIFEST"
for entry in "${REPOS[@]}"; do
  IFS='|' read -r name url ref lang sparse <<<"$entry"
  printf '%s\t%s\n' "$name" "$lang" >>"$LANG_MANIFEST"
done

# Markdown report header.
{
  echo "# VERIFY-2 bug-rate report"
  echo
  echo "- generated_at: \`$TIMESTAMP\`"
  echo "- corpora_dir: \`$CORPORA_DIR\`"
  echo "- archigraph_bin: \`$BIN\`"
  echo "- runs_per_repo: \`$RUNS\`"
  echo
  echo "## Per-repo results"
  echo
  echo "| repo | files | entities | relationships | bug_rate | resolution_rate | bug_rate_median | bug_rate_range | runs_executed |"
  echo "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |"
} >"$REPORT"

clone_or_update() {
  local name="$1" url="$2" ref="$3" sparse="${4:-}"
  local dest="$CORPORA_DIR/$name"
  if [[ -d "$dest/.git" ]]; then
    echo "==> updating $name" >&2
    ( cd "$dest" && git fetch --depth 1 origin "$ref" >/dev/null 2>&1 && git checkout -q FETCH_HEAD ) || true
    return 0
  fi
  if [[ -n "$sparse" ]]; then
    echo "==> sparse-cloning $name @ $ref (subset: $sparse)" >&2
    # Blob-less partial clone + cone-mode sparse checkout. We deliberately
    # do not pass --depth here because partial clones with --depth+--branch
    # are flaky on older git versions; the blob filter alone keeps the
    # working set small.
    if ! git clone --filter=blob:none --no-checkout --branch "$ref" "$url" "$dest" >/dev/null 2>&1; then
      git clone --filter=blob:none --no-checkout "$url" "$dest" >/dev/null 2>&1
    fi
    ( cd "$dest" \
      && git sparse-checkout init --cone >/dev/null 2>&1 \
      && git sparse-checkout set "$sparse" >/dev/null 2>&1 \
      && git checkout -q "$ref" 2>/dev/null \
        || git checkout -q FETCH_HEAD 2>/dev/null \
        || git checkout -q ) || true
  else
    echo "==> cloning $name @ $ref" >&2
    git clone --depth 1 --branch "$ref" "$url" "$dest" >/dev/null 2>&1 || \
      git clone --depth 1 "$url" "$dest" >/dev/null 2>&1
  fi
}

# run_one_pass: index $name once, writing JSON stats to $out.
# Returns 0 on success, 1 on timeout/indexer error.
run_one_pass() {
  local name="$1" out="$2" stderr_log="$3"
  local dest="$CORPORA_DIR/$name"
  local rc=0
  if [[ -n "$TIMEOUT_BIN" && "$PER_REPO_TIMEOUT" != "0" ]]; then
    "$TIMEOUT_BIN" --foreground "${PER_REPO_TIMEOUT}s" \
      "$BIN" index --json-stats "$dest" >"$out" 2>"$stderr_log" || rc=$?
  else
    "$BIN" index --json-stats "$dest" >"$out" 2>"$stderr_log" || rc=$?
  fi
  if [[ $rc -ne 0 ]]; then
    if [[ -n "$TIMEOUT_BIN" && $rc -eq 124 ]]; then
      echo "  ! indexer timed out after ${PER_REPO_TIMEOUT}s for $name" >&2
    else
      echo "  ! indexer failed (rc=$rc); see $stderr_log" >&2
    fi
    return 1
  fi
  return 0
}

# run_one: run the indexer RUNS times, compute median bug_rate + min/max range,
# write a canonical merged JSON to $TMPDIR_AGG/$name.json, and append the
# per-repo row to $REPORT.  Returns 0 on success, 1 if all runs failed.
run_one() {
  local name="$1"
  local out="$TMPDIR_AGG/$name.json"
  local stderr_log="$TMPDIR_AGG/$name.stderr"
  echo "==> indexing $name  (runs=$RUNS)" >&2

  # Collect per-run JSON into a scratch directory.
  local rundir="$TMPDIR_AGG/${name}-runs"
  mkdir -p "$rundir"

  local run_idx=0
  local succeeded=0
  while [[ $run_idx -lt $RUNS ]]; do
    local rjson="$rundir/run${run_idx}.json"
    local rlog="$rundir/run${run_idx}.stderr"
    if run_one_pass "$name" "$rjson" "$rlog"; then
      succeeded=$((succeeded + 1))
    fi
    run_idx=$((run_idx + 1))

    # Short-circuit: once 3+ runs succeeded, check if they are within 0.5pp.
    if [[ $succeeded -ge 3 ]]; then
      local stable
      stable="$(python3 - "$rundir" <<'PY'
import json, glob, sys, os
tmp = sys.argv[1]
paths = sorted(glob.glob(os.path.join(tmp, "run*.json")))
rates = []
for p in paths:
    try:
        with open(p) as fh:
            d = json.load(fh)
        rates.append(float(d.get("bug_rate", 0.0)))
    except Exception:
        pass
if len(rates) < 3:
    print("no"); sys.exit(0)
if max(rates) - min(rates) <= 0.005:
    print("yes")
else:
    print("no")
PY
)"
      if [[ "$stable" == "yes" ]]; then
        echo "    short-circuit: $run_idx runs stable (±0.5pp), skipping remaining" >&2
        break
      fi
    fi
  done

  if [[ $succeeded -eq 0 ]]; then
    echo "  ! all $run_idx run(s) failed for $name" >&2
    return 1
  fi

  # Median aggregation: merge per-run JSON into canonical $out for this repo.
  python3 - "$rundir" "$out" "$name" "$REPORT" <<'PY' || return 1
import json, glob, sys, os, statistics

rundir, out_path, repo_name, report = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]

paths = sorted(glob.glob(os.path.join(rundir, "run*.json")))
reports = []
for p in paths:
    try:
        with open(p) as fh:
            reports.append(json.load(fh))
    except Exception:
        pass

if not reports:
    print(f"  ! no readable run JSON for {repo_name}", file=sys.stderr)
    sys.exit(1)

def med(key, default=0.0):
    return statistics.median(float(r.get(key, default)) for r in reports)

def med_int(key, default=0):
    return int(statistics.median(int(r.get(key, default)) for r in reports))

# Use the last run's data as the canonical base for disposition_counts etc.
base = reports[-1]

bug_rate_median    = med("bug_rate")
bug_rate_min       = min(float(r.get("bug_rate", 0)) for r in reports)
bug_rate_max       = max(float(r.get("bug_rate", 0)) for r in reports)
res_rate_median    = med("resolution_rate")
runs_executed      = len(reports)

merged = dict(base)
merged["bug_rate"]          = bug_rate_median
merged["bug_rate_median"]   = bug_rate_median
merged["bug_rate_range"]    = f"{bug_rate_min:.4%}–{bug_rate_max:.4%}"
merged["resolution_rate"]   = res_rate_median
merged["runs_executed"]     = runs_executed

with open(out_path, "w") as fh:
    json.dump(merged, fh, indent=2)
    fh.write("\n")

# Append to the markdown report.
row = "| {name} | {files} | {ent} | {rel} | {br:.2%} | {rr:.2%} | {med:.2%} | {rng} | {runs} |\n".format(
    name=repo_name,
    files=med_int("files"),
    ent=med_int("entities"),
    rel=med_int("relationships"),
    br=bug_rate_median,
    rr=res_rate_median,
    med=bug_rate_median,
    rng=f"{bug_rate_min:.2%}–{bug_rate_max:.2%}",
    runs=runs_executed,
)
with open(report, "a") as fh:
    fh.write(row)
PY
}

for entry in "${REPOS[@]}"; do
  IFS='|' read -r name url ref lang sparse <<<"$entry"
  clone_or_update "$name" "$url" "$ref" "${sparse:-}"
  if ! run_one "$name"; then
    echo "| $name | ERROR | - | - | - | - | - | - | - |" >>"$REPORT"
    continue
  fi
done

# Fail-fast: if no per-repo JSON files were produced, or every produced
# JSON had files=0, exit 1 instead of writing an empty report. This guards
# against silent corpus drift (e.g., clone failures, every clone empty).
# Note: skip *-runs/ subdirectories (per-run scratch) and _* sentinel files.
python3 - "$TMPDIR_AGG" <<'PY' || { echo "VERIFY-2: empty corpus — no repos indexed or all repos had files=0" >&2; exit 1; }
import json, os, sys, glob
tmp = sys.argv[1]
paths = [p for p in sorted(glob.glob(os.path.join(tmp, "*.json")))
         if not os.path.basename(p).startswith("_")]
if not paths:
    sys.exit(1)
total_files = 0
for p in paths:
    try:
        with open(p) as fh:
            d = json.load(fh)
        total_files += d.get("files", 0)
    except Exception:
        pass
if total_files == 0:
    sys.exit(1)
sys.exit(0)
PY

# Aggregate dispositions across every per-repo JSON file. Adds:
#   - aggregate row inside the per-repo table
#   - per-repo disposition table for each repo
#   - corpus-wide aggregate metric table (uses median bug_rate per repo)
#   - corpus-wide disposition breakdown
#   - per-language aggregate (using the LANG_MANIFEST written above)
#   - ship-gate check
python3 - "$TMPDIR_AGG" "$REPORT" "$LANG_MANIFEST" <<'PY'
import json, os, sys, glob, statistics

tmp, report, manifest = sys.argv[1], sys.argv[2], sys.argv[3]

DISPOSITIONS = [
    "resolved",
    "external-known",
    "external-unknown",
    "dynamic",
    "bug-extractor",
    "bug-resolver",
    "unclassified",
]

# Load language manifest: name -> language.
lang_of = {}
with open(manifest) as fh:
    for line in fh:
        line = line.rstrip("\n")
        if not line:
            continue
        parts = line.split("\t")
        if len(parts) != 2:
            continue
        lang_of[parts[0]] = parts[1]

per_repo = {}
# Skip *-runs scratch subdirectories (they contain per-run raw JSON).
for p in sorted(glob.glob(os.path.join(tmp, "*.json"))):
    try:
        with open(p) as fh:
            d = json.load(fh)
    except Exception:
        continue
    name = os.path.splitext(os.path.basename(p))[0]
    # Skip the language manifest (*.tsv) and sentinel (*.complete).
    if name.startswith("_"):
        continue
    per_repo[name] = d

# Aggregate row in the per-repo table (still inside ## Per-repo results).
# Bug-rate aggregation uses per-repo medians (bug_rate_median field when
# present, falling back to bug_rate for N=1 / legacy runs).
totals = {"files": 0, "entities": 0, "relationships": 0}
endpoints_total = 0
endpoints_resolved = 0
endpoints_bug = 0
agg_dispo = {k: 0 for k in DISPOSITIONS}
median_bug_rates = []  # one per-repo median for corpus-level median-of-medians
runs_list = []
for name, d in per_repo.items():
    totals["files"] += d.get("files", 0)
    totals["entities"] += d.get("entities", 0)
    totals["relationships"] += d.get("relationships", 0)
    for k, v in d.get("disposition_counts", {}).items():
        agg_dispo[k] = agg_dispo.get(k, 0) + v
        endpoints_total += v
        if k == "resolved":
            endpoints_resolved += v
        if k in ("bug-extractor", "bug-resolver"):
            endpoints_bug += v
    # Prefer the explicit median field written by run_one (multi-run path).
    median_bug_rates.append(float(d.get("bug_rate_median", d.get("bug_rate", 0.0))))
    runs_list.append(int(d.get("runs_executed", 1)))

# Corpus bug_rate: aggregate from raw disposition counts (same as before) so
# the number is comparable across report versions; we ALSO surface the
# median-of-medians as bug_rate_median for the multi-run path.
agg_br = (endpoints_bug / endpoints_total) if endpoints_total else 0.0
agg_rr = (endpoints_resolved / endpoints_total) if endpoints_total else 0.0
agg_br_median = statistics.median(median_bug_rates) if median_bug_rates else 0.0
total_runs = sum(runs_list)

with open(report, "a") as fh:
    # Aggregate row at the bottom of the per-repo table.
    fh.write(
        "| **AGGREGATE** | **{f}** | **{e}** | **{r}** | **{br:.2%}** | **{rr:.2%}** "
        "| **{med:.2%}** | — | **{runs}** |\n".format(
            f=totals["files"], e=totals["entities"], r=totals["relationships"],
            br=agg_br, rr=agg_rr, med=agg_br_median, runs=total_runs))

    # Per-repo disposition tables.
    fh.write("\n## Per-repo disposition breakdown\n")
    for name in sorted(per_repo):
        d = per_repo[name]
        counts = d.get("disposition_counts", {})
        repo_total = sum(counts.get(k, 0) for k in DISPOSITIONS)
        runs_ex = d.get("runs_executed", 1)
        br_range = d.get("bug_rate_range", "—")
        fh.write(f"\n### {name}\n\n")
        fh.write(f"- runs_executed: {runs_ex}\n")
        fh.write(f"- bug_rate_median: {float(d.get('bug_rate_median', d.get('bug_rate', 0))):.4%}\n")
        fh.write(f"- bug_rate_range: {br_range}\n\n")
        fh.write("| disposition | count | pct |\n| --- | ---: | ---: |\n")
        for k in DISPOSITIONS:
            v = counts.get(k, 0)
            pct = (v / repo_total) if repo_total else 0.0
            fh.write(f"| {k} | {v} | {pct:.2%} |\n")
        fh.write(f"| **total** | **{repo_total}** | **100.00%** |\n")

    # Corpus-wide aggregate metric table.
    fh.write("\n## Aggregate\n\n")
    fh.write("| metric | value |\n| --- | ---: |\n")
    fh.write(f"| total_files | {totals['files']} |\n")
    fh.write(f"| total_entities | {totals['entities']} |\n")
    fh.write(f"| total_relationships | {totals['relationships']} |\n")
    fh.write(f"| endpoints_classified | {endpoints_total} |\n")
    fh.write(f"| bug_rate | {agg_br:.4%} |\n")
    fh.write(f"| bug_rate_median | {agg_br_median:.4%} |\n")
    fh.write(f"| resolution_rate | {agg_rr:.4%} |\n")
    fh.write(f"| total_runs_executed | {total_runs} |\n")

    # Corpus-wide disposition breakdown.
    fh.write("\n## Aggregate disposition breakdown\n\n")
    fh.write("| disposition | count | pct |\n| --- | ---: | ---: |\n")
    for k in DISPOSITIONS:
        v = agg_dispo.get(k, 0)
        pct = (v / endpoints_total) if endpoints_total else 0.0
        fh.write(f"| {k} | {v} | {pct:.2%} |\n")
    fh.write(f"| **total** | **{endpoints_total}** | **100.00%** |\n")

    # Per-language aggregate.
    fh.write("\n## Per-language aggregate\n\n")
    fh.write("| language | repos | files | entities | relationships | endpoints | bug_rate | bug_rate_median | resolution_rate |\n")
    fh.write("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
    by_lang = {}
    for name, d in per_repo.items():
        lang = lang_of.get(name, "unknown")
        bucket = by_lang.setdefault(lang, {
            "repos": 0, "files": 0, "entities": 0, "relationships": 0,
            "endpoints": 0, "resolved": 0, "bug": 0, "medians": [],
        })
        bucket["repos"] += 1
        bucket["files"] += d.get("files", 0)
        bucket["entities"] += d.get("entities", 0)
        bucket["relationships"] += d.get("relationships", 0)
        for k, v in d.get("disposition_counts", {}).items():
            bucket["endpoints"] += v
            if k == "resolved":
                bucket["resolved"] += v
            if k in ("bug-extractor", "bug-resolver"):
                bucket["bug"] += v
        bucket["medians"].append(float(d.get("bug_rate_median", d.get("bug_rate", 0.0))))
    for lang in sorted(by_lang):
        b = by_lang[lang]
        br = (b["bug"] / b["endpoints"]) if b["endpoints"] else 0.0
        rr = (b["resolved"] / b["endpoints"]) if b["endpoints"] else 0.0
        br_med = statistics.median(b["medians"]) if b["medians"] else 0.0
        fh.write(f"| {lang} | {b['repos']} | {b['files']} | {b['entities']} | "
                 f"{b['relationships']} | {b['endpoints']} | {br:.4%} | {br_med:.4%} | {rr:.4%} |\n")

    # Ship-gate check — uses median-of-medians when multi-run, raw aggregate otherwise.
    gate_rate = agg_br_median if total_runs > len(per_repo) else agg_br
    fh.write("\n## Ship-gate check (target bug_rate <= 1%)\n\n")
    status = "PASS" if gate_rate <= 0.01 else "FAIL"
    fh.write(f"- status: **{status}** (bug_rate_median={agg_br_median:.4%}, bug_rate={agg_br:.4%})\n")
    if total_runs > len(per_repo):
        fh.write(f"- measurement: median-of-medians from {total_runs} total indexer runs\n")
    else:
        fh.write(f"- measurement: single-shot (runs=1)\n")
PY

# Mark this run complete — the EXIT trap will now garbage-collect
# $TMPDIR_AGG. Without this sentinel the per-repo JSON survives so a
# partial/aborted run can be inspected or resumed by hand.
: >"$TMPDIR_AGG/.complete"

echo "==> wrote report: $REPORT"
echo "$REPORT"
