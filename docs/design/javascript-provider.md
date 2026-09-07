# Optional JavaScript provider

The provider is a separate pack artifact with its own frozen dependency graph.
Its build copies the shared, policy-owned lint and formatting rules as pack-owned
bytes. The installed provider imports those bytes inside its verified tree; it
has no import contract with the engine's internal tool bundle. Hoisted installation
preserves the reviewed lock and removes executable launcher links before packing.
The measured initial artifact has 12,562 regular files and about 99 MB of content.
The generic pack inventory permits 20,000 files while retaining its 128 MiB total
and 16 MiB per-file limits. The response cap matches the runner's existing 8 MiB
bound; the larger acceptance project's input identities alone exceed 1 MiB.

Framework dispatch lives inside the provider. Ordinary JS/TS and Astro adapters
produce original-source ASTs, embedded-script mappings, formatting choices, and
project facts. The provider reads existing package metadata and configurations.
It never runs target compiler plugins, ESLint configuration, framework configuration
scripts, or package check commands. Plugin loading starts in the provider's own
directory so dependencies resolve from the sealed artifact.

Type checking uses the pinned Volar and TypeScript services with the provider's
framework plugin. Installed project dependencies supply contained type declarations,
not executable compiler services. Source inputs and additional dependency reads
are hashed. Excluded source, project references, missing framework metadata, and
unavailable source mappings are incomplete coverage. Findings retain the selected
original path and one-based UTF-8 byte coordinates.

Unused-code analysis uses a provider-owned Knip configuration with automatic plugins
disabled. Entries come from manifest exports, declared policy entries, tests, and
statically recognized framework entries. Literal route names are escaped before
becoming analyzer glob inputs. Unsupported entry or import semantics cannot
establish complete reachability. Formatting returns proposed edits to the core;
reading project context does not authorize writing it.

The quick boundary materializes real source in temporary projects. Every claimed
capability has a valid case and a deliberately introduced defect with a stable
expected rule. Installed pack conformance exercises the same cases through the
strict public protocol and policy-owned runtime. Real-project acceptance uses
separate temporary state and preserves the consuming repositories' exact locks.

Function metrics come from the pinned ESLint complexity, depth, and parameter
rules. The provider collects their structured measurements at original source
positions; it does not maintain a parallel branch-counting implementation.
Type checking enables actual JavaScript analysis for claimed JS inputs and reports
project exclusions as incomplete coverage. Node builtins and exported package
stylesheets resolve from metadata and contained files without executing target
package entrypoints.
