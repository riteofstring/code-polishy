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
unavailable source mappings are incomplete coverage. Findings retain the reportable
original path and one-based UTF-8 byte coordinates.

Unused-code analysis uses a provider-owned Knip configuration with automatic plugins
disabled. Entries come from manifest exports, core-normalized policy entries, root/src
cli/index/main conventions, configuration files, tests, and statically recognized
framework entries. Knip runs separately at each actual package tree, including
repositories without a root manifest. Its configuration and workspace ownership
come from resolved units; target Knip configuration is never loaded. Literal route names are escaped before
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

Embedded CSS block comments are tokenized with the pinned CSS language service
scanner and mapped back to the original framework source. Strings remain data;
unterminated strings or comments and unsupported stylesheet languages remain
incomplete. This retains the core's comment policy without adding a framework
parser to the engine.

Static script-source references are adapter facts with original byte locations.
The Astro adapter recognizes processed relative script sources according to the
[framework's script processing rules](https://docs.astro.build/en/guides/client-side-scripts/).
Literal HTTP(S) URLs, protocol-relative URLs, and templates whose static prefix
proves a complete external authority establish browser-loaded boundaries. They
produce visible informational notes, no local import edges, and no remote fetches.
Local expressions retain lint and type checking at original source locations.
Unknown origins, spread attributes, and unprocessed local script references remain
incomplete for dependency and reachability analysis. The Knip compiler retains recognized
script-source imports in its derived code so only reachable framework components
make their scripts reachable.

Ordinary TypeScript import-equals declarations retain runtime or type-only edges.
Contained asset resolution covers CSS, JSON, images, common media, fonts, and PDF
files, while Astro components remain executable source. A declaration file cannot
replace a missing asset. Unknown executable formats and import parameters require
further supported interpretation; they do not become data merely because a file
exists. These distinctions retain core module checks without claiming asset bytes
were analyzed as JavaScript.

The native JS/TS runner and optional provider share deterministic rules for
unreachable code, duplicate conditions and cases, constant binary expressions,
unsafe finally blocks, invalid typeof comparisons, and incorrect NaN comparisons.
These checks run without a provider selection or target ESLint configuration.
Function metrics remain measurements; the core alone applies their thresholds.

Formatting and analysis have separate manifest commands. Only the formatting
capability runs in the format profile; check and gate retain all six capabilities.
The core continues honoring every provider's explicit manifest profiles.

Compilation units use core-resolved configuration paths: the nearest tsconfig.json
or jsconfig.json, or a unique tsconfig*.json when neither standard name exists.
Generated source uses its declared source package for configuration and package
resolution while keeping its original read and diagnostic path.
Required allowJs and checkJs overrides apply while discovering root files and
while compiling them; noEmit and noCheck overrides retain actual analysis without
writes. Strictness and library checking retain the selected project's settings.
Bounded informational notes identify the config, root-file count, and effective
options. Selected sources excluded by that config remain incomplete; sibling
configs are not merged or guessed. Nested configs can own tooling or test source.

Lint consumes core-resolved React activation, including disabled overrides and
source-package inheritance. Ordinary source lint does not parse package metadata.
Architecture retains parsed imports and uses default resolution with a visible
note when compiler configuration is invalid. Type checking continues to require
valid compilation configuration. Unsupported framework contracts remain explicit.

The provider remains opt-in. Its regression fixtures establish the covered
behaviors, without claiming complete parity with native analysis or arbitrary
framework configurations.

Dead-code module resolution uses each source's resolved compilation options,
including nested configurations within one package. Generated bare and package
imports resolve from the declared source package; relative imports keep their
physical source directory. Resolvers remain scoped to these compilation
options so overlapping aliases cannot exchange targets between units.

For type checking, generated source inherits the effective package's module
format before TypeScript creates its source document. Explicit `.mjs` and `.cjs`
extensions retain their normal precedence. Conditional package exports use the
import's resulting resolution mode. The document registry retains the physical
filename and original text, so diagnostics and input evidence keep their source
coordinates without creating or rewriting source files.
