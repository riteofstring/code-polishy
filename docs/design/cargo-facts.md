# Cargo Project Facts

The `rustfacts` boundary owns Cargo command preparation and metadata validation;
the repository layer owns data-only manifest parsing and tool paths. Quality,
architecture and dependency policy consume facts without acquiring their own
Cargo resolvers. Commands use the common structured runner so limits, cancellation
and command evidence have one owner.

Workspace discovery runs `cargo metadata --format-version 1 --no-deps --frozen`.
It can discover members, targets, inherited metadata, declared dependencies and
features before dependencies are materialized. A null `resolve` is deliberately
retained as absent dependency-resolution evidence. Discovery neither compiles
code nor proves that dependencies or any target configuration have been checked.
The selected manifest and Cargo's actual workspace membership determine scope;
directory naming is never a package identifier.

Cargo runs from the repository root with a sealed environment, absolute pinned
compiler paths and repository-local Cargo home and target output under `.tools`.
Ambient rustup selection, wrappers and user Cargo home are not inherited. Ancestor
Cargo configuration outside the checkout must not silently enter analysis.
Repository configuration is parsed before execution; unsupported execution-altering
settings produce a diagnostic. This controls discovery inputs, not operating-system
access by future build scripts or procedural macros.

[Rust input coverage](rust-inputs.md) defines control input selection and the
governed toolchain. The [implementation plan](../plans/code-polishy-rust-support-plan.md)
records the remaining Rust capabilities and acceptance work.
