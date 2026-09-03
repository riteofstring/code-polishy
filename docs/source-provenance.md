# Source Provenance

Code Polishy owns its runtime and policy implementation in this repository.
Separately owned application repositories are external inputs, never hidden
runtime dependencies or public integration contracts.

## Public design references

These public projects informed specific policy concepts:

- [Acceptance Pipeline Specification](https://github.com/unclebob/Acceptance-Pipeline-Specification)
  distinguishes executable Gherkin acceptance from deliberate acceptance-data
  mutation.
- [crap4go](https://github.com/unclebob/crap4go) combines complexity and
  missing coverage to identify risky functions.
- [mutate4go](https://github.com/unclebob/mutate4go) demonstrates focused
  differential mutation and explicit handling of surviving mutations.

Code Polishy implements its own policy engine and uses the independently
maintained Apache-licensed Gremlins release for supplemental Go mutation tests.

## Toolchain origins

Every carried tool is version-pinned in a checked-in file. The online
supply-chain gate resolves each standalone executable's publication timestamp
from its fixed official metadata source and applies the same 30-day admission
rule used for dependency graphs. Installation fetches tools from their official
distribution origins, verifies checked-in checksums, and probes each installed
version before staging a release.

- Go is the policy-engine toolchain. Code Polishy computes cyclomatic
  complexity directly from the standard Go AST.
- Node and pnpm run the sealed JavaScript tool bundle. They are Code Polishy
  runtime dependencies, not target-project toolchain requirements.
- The JavaScript bundle contains Prettier, ESLint,
  `@typescript-eslint/parser`, `eslint-plugin-react-hooks`,
  `eslint-plugin-jsx-a11y`, TypeScript, Knip, `js-yaml`, and `@types/node`.
  `tools/javascript/pnpm-lock.yaml` locks the complete graph and
  `tools/javascript_bundle_inventory.txt` records installed packages and
  licenses. Its bounded `js-yaml` operation reads GitLab control files as data;
  it never executes pipeline configuration or project code.
- ShellCheck, Ruff, `ty`, OSV-Scanner, and Gremlins are downloaded from their
  official release origins and checked against repository-owned versions and
  archive digests. Ruff supplies isolated Python lint, complexity, and
  import-graph facts; `ty` supplies structured Python type diagnostics.
- The release carries Vulture `2.16` and the pinned CPython
  `3.12.13+20260728` distribution from python-build-standalone as separate
  policy inputs. The Vulture PyPI wheel is checksum-verified and unpacked into
  the carried runtime. Their exact pins and checksum inventories ship with the
  release, while the online supply-chain gate resolves their release age from
  fixed upstream metadata services; neither comes from a target environment.
- Trivy is copied from an exact official image digest into the minimal scanner
  image. `artifact-security/scanner-policy.json` records its source,
  configuration, and integrity digests; `artifact-security/scanner.openvex.json`
  records reviewed vulnerability applicability.

Two JavaScript packages intentionally remain on constrained release lines.
Knip stays on the last selected release before its parser and resolver required
native `oxc` add-ons. `eslint-plugin-react-hooks` stays on the selected line
before React Compiler rules introduced the Babel toolchain. These constraints
keep the sealed bundle portable and minimal.

## Implementation boundaries

- Go remains the policy-engine implementation runtime. For Python policy work,
  the release carries CPython `3.12.13+20260728` from python-build-standalone
  to run Vulture `2.16`; Code Polishy never executes target Python to discover
  imports, select a project, or perform dead-code analysis.
- The repository boundary builds one validated Python project inventory from
  contained `pyproject.toml` files and reuses it for dependency, quality, and
  architecture work. Its project, direct `src`, and in-tree PEP 517 backend
  roots are passed explicitly to the consumers. A project-local `.venv` is
  passed only to `ty` when dependencies require it; Vulture always uses carried
  CPython and its pinned built-in whitelists. Ambient Python paths and
  environments are not tool provenance.
- Python manifests and `uv.lock` are target-owned inputs. Exact Git repository
  and commit facts remain source facts, not a fabricated PyPI age or
  vulnerability result when registry evidence is unavailable.
- Generic JavaScript quality checks run through Code Polishy's sealed bundle,
  independent of target-local development dependencies.
- Target-specific commands, paths, external inputs, and exceptions live in the
  target repository's `.code-polishy.json`.
- Third-party tools and packages remain under their own licenses; their origins
  and complete dependency inventories are recorded alongside the release.

## Extension rule

New conditional modules and project-specific providers must preserve exact
inputs, fail-closed coverage, deterministic parsing, and native diagnostic
output. Add a tool only when it can be pinned, verified, and represented in the
release inventory.
