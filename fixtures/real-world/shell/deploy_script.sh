#!/usr/bin/env bash
# Source: https://github.com/kamikazechaser/common-scripts (synthetic based on real production deploy patterns) | License: MIT
#
# deploy.sh — Zero-downtime deploy script for containerized services

set -euo pipefail

# ============================================================
# Configuration
# ============================================================
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.yml"
SERVICE_NAME="${SERVICE_NAME:-app}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
HEALTH_CHECK_URL="${HEALTH_CHECK_URL:-http://localhost:8080/health}"
HEALTH_CHECK_RETRIES=30
HEALTH_CHECK_INTERVAL=2
ROLLBACK_ON_FAILURE="${ROLLBACK_ON_FAILURE:-true}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# ============================================================
# Logging
# ============================================================
log_info()  { echo -e "${GREEN}[INFO]${NC}  $(date +%T) $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $(date +%T) $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $(date +%T) $*" >&2; }

# ============================================================
# Functions
# ============================================================
check_dependencies() {
    local deps=("docker" "docker-compose" "curl" "jq")
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &>/dev/null; then
            log_error "Required dependency '$dep' is not installed."
            exit 1
        fi
    done
}

get_current_image() {
    docker-compose -f "$COMPOSE_FILE" ps -q "$SERVICE_NAME" 2>/dev/null | \
        xargs -I{} docker inspect {} --format '{{.Config.Image}}' 2>/dev/null || echo ""
}

pull_image() {
    local image="$1"
    log_info "Pulling image: $image"
    if ! docker pull "$image"; then
        log_error "Failed to pull image: $image"
        return 1
    fi
}

health_check() {
    local url="$1"
    local retries="$2"
    local interval="$3"

    log_info "Running health check against: $url"
    for i in $(seq 1 "$retries"); do
        if curl -sf --max-time 5 "$url" | jq -e '.status == "ok"' &>/dev/null; then
            log_info "Health check passed (attempt $i/$retries)"
            return 0
        fi
        log_warn "Health check attempt $i/$retries failed, retrying in ${interval}s..."
        sleep "$interval"
    done
    log_error "Health check failed after $retries attempts"
    return 1
}

rollback() {
    local previous_image="$1"
    log_warn "Rolling back to image: $previous_image"

    export IMAGE_TAG="$previous_image"
    if docker-compose -f "$COMPOSE_FILE" up -d --no-deps "$SERVICE_NAME"; then
        log_info "Rollback successful"
    else
        log_error "Rollback failed! Manual intervention required."
        exit 2
    fi
}

cleanup_old_images() {
    log_info "Cleaning up dangling images..."
    docker image prune -f --filter "until=24h" || true
}

deploy() {
    local new_image="${SERVICE_NAME}:${IMAGE_TAG}"

    log_info "Starting deployment of $new_image"
    log_info "Environment: ${DEPLOY_ENV:-unknown}"

    check_dependencies

    local previous_image
    previous_image=$(get_current_image)
    log_info "Current image: ${previous_image:-none}"

    pull_image "$new_image"

    log_info "Deploying new version..."
    if ! docker-compose -f "$COMPOSE_FILE" up -d --no-deps --remove-orphans "$SERVICE_NAME"; then
        log_error "Deployment failed"
        if [[ "$ROLLBACK_ON_FAILURE" == "true" && -n "$previous_image" ]]; then
            rollback "$previous_image"
        fi
        exit 1
    fi

    if ! health_check "$HEALTH_CHECK_URL" "$HEALTH_CHECK_RETRIES" "$HEALTH_CHECK_INTERVAL"; then
        log_error "Post-deployment health check failed"
        if [[ "$ROLLBACK_ON_FAILURE" == "true" && -n "$previous_image" ]]; then
            rollback "$previous_image"
        fi
        exit 1
    fi

    cleanup_old_images
    log_info "Deployment complete. Running: $new_image"
}

# ============================================================
# Entry point
# ============================================================
case "${1:-deploy}" in
    deploy)   deploy ;;
    rollback) rollback "${2:?Usage: $0 rollback <image>}" ;;
    health)   health_check "$HEALTH_CHECK_URL" 1 0 ;;
    *)        echo "Usage: $0 [deploy|rollback <image>|health]"; exit 1 ;;
esac
