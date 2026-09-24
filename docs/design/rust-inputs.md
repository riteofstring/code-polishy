# Rust and Shader Input Coverage

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

Cargo manifest inspection uses a pinned Go TOML parser and the existing bounded,
contained regular-file reader. It parses package and virtual-workspace headers
without invoking Cargo, importing project code or executing build scripts.
Unknown manifest fields remain available to Cargo rather than being rejected
by an incomplete schema. Malformed selected manifests produce original-path
diagnostics without discarding successfully parsed siblings or inspecting
unrelated projects during a focused check. This inventory is not a resolved
workspace, target, feature or dependency graph; those facts belong to Cargo.

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

## Governed toolchain

The initial compiler is Rust 1.97.1, released 2026-07-16. Cargo, rustc, rustdoc,
rustfmt and Clippy come from that same official release, alongside the host
standard library. The pin is independent of a project's declared minimum Rust
version. The beta targets editions 2015, 2018, 2021 and 2024 on the existing
five Code Polishy installation hosts; additional compilation targets require
separate component and SDK evidence.

The [upstream 1.98 regression](https://github.com/rust-lang/rust/issues/161441)
does not reproduce on 1.97.1. The initial pin avoids both that affected release
and an under-30-day toolchain admission. Checksums come from the
[official 1.97.1 manifest](https://static.rust-lang.org/dist/channel-rust-1.97.1.toml).
Installers authenticate component archives, copy their payloads without executing
installation scripts, preserve bundled licenses, and stage a complete runtime
before replacing the previous installation. They do not install rustup or select
an ambient channel. Release publication must carry the runtime and its exact pin;
the developer installer alone does not establish installed-release coverage.

[Cargo project facts](cargo-facts.md) defines workspace discovery and its execution
boundary separately from source classification.
