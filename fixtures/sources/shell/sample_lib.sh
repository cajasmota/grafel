#!/usr/bin/env bash
# sample_lib.sh — utility library sourced by sample_deploy.sh.
# Exercises cross-file source patterns for the resolver test corpus.

source ./config.sh
source /etc/environment
. /usr/share/bash-completion/bash_completion

# Shared colour constants.
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

colour_log() {
    local colour="$1"; shift
    echo -e "${colour}[LOG] $*${NC}" >&2
}

info() {
    colour_log "${GREEN}" "$@"
}

error() {
    colour_log "${RED}" "$@"
}

retry() {
    local max="$1"; shift
    local n=0
    until [ "$n" -ge "$max" ]; do
        "$@" && return 0
        n=$((n + 1))
        info "Retry $n/$max..."
        sleep 2
    done
    error "Command failed after $max retries"
    return 1
}
