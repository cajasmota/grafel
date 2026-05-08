#!/usr/bin/env bash
# Sample shell deployment script — golden fixture source.

set -euo pipefail

APP_NAME="${APP_NAME:-sample-api}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
REGISTRY="${REGISTRY:-docker.io/example}"
NAMESPACE="${NAMESPACE:-default}"

log() {
    echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] $*" >&2
}

check_prerequisites() {
    for cmd in docker kubectl helm; do
        if ! command -v "$cmd" &>/dev/null; then
            log "ERROR: $cmd is not installed"
            exit 1
        fi
    done
}

build_image() {
    local tag="$REGISTRY/$APP_NAME:$IMAGE_TAG"
    log "Building image: $tag"
    docker build -t "$tag" .
    docker push "$tag"
}

deploy_to_k8s() {
    local tag="$REGISTRY/$APP_NAME:$IMAGE_TAG"
    log "Deploying $APP_NAME to namespace $NAMESPACE"
    helm upgrade --install "$APP_NAME" ./chart \
        --namespace "$NAMESPACE" \
        --set image.tag="$IMAGE_TAG" \
        --set image.repository="$REGISTRY/$APP_NAME" \
        --wait \
        --timeout 5m
}

run_smoke_tests() {
    local endpoint="${1:-http://localhost:8080}"
    log "Running smoke tests against $endpoint"
    if curl -sf "$endpoint/health" | grep -q '"status":"ok"'; then
        log "Smoke test PASSED"
    else
        log "Smoke test FAILED"
        exit 1
    fi
}

main() {
    check_prerequisites
    build_image
    deploy_to_k8s
    run_smoke_tests
    log "Deployment complete"
}

main "$@"
