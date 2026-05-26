# tools.just — imported utility recipes.

# Format source code.
fmt:
    cargo fmt
    rustfmt --edition 2021 src/**/*.rs

# Check formatting without modifying files.
fmt-check:
    cargo fmt --check

# Update dependencies.
update:
    cargo update

# Generate documentation.
docs:
    cargo doc --no-deps --open
