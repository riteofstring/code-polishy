# Rust and shader input coverage

Rust compilation settings affect which code and diagnostics exist. Cargo
configuration can change source registries, rustflags, wrappers, linkers and
runners; rustup declarations change the selected toolchain; rustfmt and Clippy
settings change analysis. Their standard filenames are therefore sensitive
control inputs at every repository depth, alongside Cargo manifests and locks.
This includes both `.cargo/config` and `.cargo/config.toml`, `rust-toolchain`
and `rust-toolchain.toml`, `rustfmt.toml` and `.rustfmt.toml`, and `clippy.toml`
and `.clippy.toml`.

The existing repository selection boundary expands changed or deleted control
inputs to full governed scope. This is deliberately conservative until Cargo
workspace and configuration resolution can prove a narrower affected closure.
Recognizing a filename does not parse, approve or execute its settings, inspect
configuration outside the repository, or establish reproducible Cargo execution.

WGSL is executable shader source with its own built-in language identifier,
`wgsl`. It participates in module ownership, source-comment coverage and the
existing required analysis capabilities. A shader cannot be protected as data
or silently acquire Rust analysis coverage. Generated shader source retains
correctness and ownership obligations under the existing generated-source rules.
Recognition supplies no analyzer: absent structured evidence still produces
coverage findings, even when a generic Cargo command is configured.

Ordinary shader changes retain focused file selection and the module dependency
closure. Projects express renderer-to-shader ownership through existing module
dependencies. Compiler-derived shader consumer edges and complete Rust receipt
inputs belong to later semantic integration; file recognition alone proves
neither shader validity nor GPU execution correctness.

The [Rust support plan](../plans/code-polishy-rust-support-plan.md) records the
remaining discovery, analysis, dependency, test and installed-release work.
