# Analysis providers

Code Polishy resolves analysis ownership from the composed configuration, exact
installed pack manifests, selected paths, capability, and execution profile.
Language classification alone never establishes analyzer coverage. Explicit pack
claims replace native work for that capability. Conflicting or unavailable claims
cannot enable a native fallback. Unclaimed native syntax retains its existing
analyzers. Configured project commands retain their execution profiles; a build
command cannot establish structured source-analysis coverage.

Pack manifest and protocol version 2 are an atomic public contract. A response
accounts for every requested file exactly once as analyzed or unsupported, retains
a stable namespaced rule identifier, and returns facts needed for core decisions.
Unsupported work blocks a required capability. An operational failure establishes
neither facts nor coverage. Conformance requires a passing source fixture and a
seeded defect with an expected rule for every claimed capability.

Requests distinguish selected targets from contained read context and carry the
effective quality policy, classifications, entry points, and exact pack identity.
Every selected input must be recorded in the response. Context and reported
additional dependency inputs have SHA-256 identities verified after execution.
Coordinates are one-based UTF-8 byte positions in original source; comment bytes
must match that position. Facts and findings outside selected targets are invalid.
Providers return format edits; the core validates all targets before applying any
edit. Generated source, declared data, and context files cannot become edit targets.

A runtime declaration requests an exact policy-owned tool version. The first
runtime implementation resolves Node from a verified installed release, checks
the binary against that release's manifest, and binds its digest into the analysis.
It never resolves a runtime from the target project or ambient PATH. Providers
carry their own pinned dependencies in the integrity-checked installed pack tree.

The core interprets source-comment and function facts. JavaScript machine
directives use the existing policy grammar. Unsupported directive grammars do not
establish permission to include prose. Function complexity uses the language's
effective limit; new languages use the shared TypeScript limit until a distinct
language policy is justified. Depth and parameter limits retain the existing
production/test distinction. Generated source retains correctness checks while
remaining exempt from handwritten style limits.

Providers contribute imports to the common source graph. The core derives node
ownership and classification and retains dependency-direction and cycle checks.
Graph input records bind the installed provider, declared languages, protocol,
effective policy, inputs, runtime, facts, and resolution. Native Python evidence
continues to require its existing project and analyzer contract. New graph
languages require provider evidence; merely naming a language does not validate it.

Framework interpretation belongs in separately installed providers. The optional
JS/TS provider reads existing package metadata and configuration as data and uses
sealed parsers and checkers. It does not execute target check scripts, compiler
plugins, or lint configurations. Framework-specific parsers and mappings stay
inside that provider. Local reports and diagnostics remain part of Code Polishy;
dashboards, remote telemetry, and aggregate analytics are outside its product scope.

Conformance context comes from the verified pack inventory beneath each declared
fixture project. It never depends on an enclosing checkout's tracked files or
ignore rules. Fixture language classification uses the same manifest declarations
as installed analysis. Operational fixture failures retain the analyzer's reason.

Managed JSON and SARIF reports retain provider rule namespaces and graph evidence.
Their schema accepts declared language and ecosystem identifiers and binds pack
facts to their version 2 protocol and exact provider identity. Native Python fact
variants retain their existing protocol contracts.
