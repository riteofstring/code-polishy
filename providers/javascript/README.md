# JavaScript and TypeScript analysis provider

This optional protocol v3 pack adds framework-aware analysis to Code Polishy.
Its JS/TS provider includes an Astro adapter and retains ordinary JavaScript,
TypeScript, JSX, and TSX support. Framework parsing and tooling stay in this
separately installed artifact. The core release does not include its dependencies.

The provider supplies formatting, deterministic lint rules, type checking,
function metrics, unused-code analysis, and authored import facts. Code Polishy
retains comment policy, metric limits, module boundaries, cycles, coverage,
selected-file writes, exceptions, and exact execution identities.

Build with the repository's pinned Node and pnpm installations:

```sh
./scripts/build-javascript-provider.sh /absolute/path/to/javascript-pack
code-polishy pack verify --source /absolute/path/to/javascript-pack
code-polishy pack install --source /absolute/path/to/javascript-pack
```

Select the printed exact name, version, and digest in the target's existing
`packs` list. The beta engine must supply the exact Node runtime named by the
manifest. Keep the ordinary engine lock and project dependency installation.
No separate framework fragment or project check script is needed.

The pack owns its parser, lint configuration, compiler services, and dependencies.
Existing package manifests, TypeScript configurations, and installed dependency
types provide project facts. Project lint configuration, compiler plugins,
framework configuration scripts, and check scripts are never executed.

Coverage is explicit. Unsupported syntax, computed module loads without a proven
target, excluded type-check inputs, project references, and unavailable mappings
remain incomplete. The initial adapter recognizes static Astro source directories
and conventional framework entry points; it does not claim arbitrary build-system
or framework-plugin semantics. Every claimed capability has valid source and a
seeded defect in the pack's conformance fixtures.
