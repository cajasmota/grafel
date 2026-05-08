# ADR-001: Go-native single-binary distribution

- **Status**: Accepted
- **Date**: 2026-05-08
- **Deciders**: Jorge Cajas

## Context

archigraph needs to be installable across macOS (Intel + Apple Silicon), Linux (x86_64 + arm64), and Windows with the smallest possible install friction. The target users are software developers and AI agents working inside developer machines and CI runners. They expect a tool to be obtainable with a single command and to "just work" without further setup.

A polyglot runtime would require Python 3.11+ with `uv`, Node.js for auxiliary scripts, system-level `pip install` of a dozen dependencies, plus a tree-sitter toolchain compiled at install time. Each of these is a failure point on at least one OS, and the cumulative effect makes "install and forget" unattainable: broken installs, version skew between machines, and tooling drift are routine outcomes for end users on stacks like that.

We want a distribution story where the user runs one command, gets one executable, and never thinks about runtimes again. The tool also needs to embed parsers (tree-sitter) and be fast on cold start, which favors AOT compilation over an interpreter.

## Decision

archigraph is implemented in Go and shipped as a single statically-linked binary per platform. Releases are produced via GoReleaser on GitHub Actions using native runners (`macos-13` for Intel, `macos-14` for Apple Silicon, `ubuntu-latest` for Linux x86_64 and arm64, `windows-latest` for Windows x86_64). CGO is enabled because tree-sitter requires it; using native runners means the C toolchain is the platform-native one, and we never cross-compile with CGO.

Binaries are uploaded to GitHub Releases. End users install via a one-line script (`curl ... | sh`) that detects OS/arch, downloads the matching artifact, verifies a checksum, and drops the binary into `~/.local/bin/archigraph` (or the Windows equivalent). Homebrew tap and Scoop manifest follow as secondary channels. There is no Docker image as a primary install path, and no compile-on-install — end users never touch a compiler.

## Consequences

### Positive
- One-line install on every supported platform.
- Cold start is sub-100ms; no runtime warmup.
- No version skew across machines once a release tag is pinned.
- CI matrix matches platform tooling exactly; no cross-compile CGO surprises.
- Single artifact simplifies signing, notarization, and provenance attestation.

### Negative
- Build matrix runs five jobs in parallel on every release; each one needs a healthy native runner.
- macOS notarization requires an Apple Developer account and adds release latency.
- CGO + tree-sitter means we cannot trivially produce static musl binaries for the smallest Linux footprint; we ship glibc binaries and an alpine-friendly variant separately if demand exists.
- Windows support requires explicit testing per release; the platform is the most likely to surface CGO edge cases.

### Neutral
- Go's standard library covers most of what the indexer needs, so dependency footprint stays small.
- Shipping a binary instead of a library means downstream Go code consumers must vendor or re-implement; out of scope for v1.0.

## Alternatives considered

- **Python + pip / uv** — Rejected: the install-and-forget UX problem this ADR is designed to fix; pip and uv both require a Python runtime on the user's machine and amplify cross-platform install failure modes.
- **Rust + cargo-dist** — viable, similar UX. Rejected because the team's Go fluency is higher and Go's MCP ecosystem (see ADR-002) is more mature for our needs.
- **Node.js / npm global install** — rejected: requires Node runtime on the user's machine, and `npm i -g` is fragile across version managers.
- **Docker image as primary distribution** — rejected: Docker is not universally installed on developer machines, and running a container per indexer invocation is too heavy. We may publish an image as a secondary artifact for CI use.
- **Compile-on-install (`go install`)** — rejected: requires a Go toolchain on the user's machine, defeating the single-binary goal.
