# Adding a Language Pack

Language packs connect an independently owned toolchain to Code
Polishy's existing policy engine. A pack is executable third-party input. Code
Polishy validates and runs it; it does not endorse, download, update, or support
the tools it contains.

## Pack contract

A pack is one local directory containing `code-polishy-pack.json`, `README.md`,
contained adapter executables, and conformance fixtures. The manifest uses
`schema/code-polishy-pack.schema.json` and protocol version 1. It declares an
exact semantic version, supported native platforms, language source patterns,
dependency manifest patterns, commands, capabilities, profiles, timeouts,
environment names, and fixture expectations.

Commands may provide `format`, `lint`, `typecheck`, `complexity`, `dead-code`,
`architecture`, `build`, `dependency-policy`, `lock-sync`, `release-age`, and
`security`. Their profiles use the corresponding normal Code Polishy profiles.
The manifest cannot declare project modules, project exceptions, credentials,
remote installation instructions, or ambient executable searches.

Every capability needs at least one fixture returning `pass` and one returning
`findings` or `operational-failure`. A fixture names a contained project,
selected files, one command and capability, and its expected status.

## Adapter protocol

Code Polishy writes exactly one JSON request to standard input. It contains
`protocolVersion`, `operation`, `capability`, the exact `projectRoot`, selected
repository-relative `files`, project `modules` with dependency direction,
format `mode`, active `profile`, and an optional bounded `outputDirectory`.

The adapter writes exactly one JSON response to standard output:

```json
{
  "protocolVersion": 1,
  "status": "findings",
  "findings": [
    {
      "capability": "lint",
      "path": "src/example.rs",
      "line": 12,
      "column": 4,
      "subject": "unused binding",
      "message": "remove the unused binding"
    }
  ]
}
```

`status` is `pass`, `findings`, or `operational-failure`. A pass includes
non-empty `evidence`; findings include structured findings; an operational
failure includes `failure`. Standard error is diagnostic log output and is not
policy evidence. Unknown fields, extra JSON values, escaping paths, malformed
findings, oversized output, and evidence-free success are rejected.

## Verify and install

Obtain and review a pack directory through your own trusted channel. Then run:

```sh
code-polishy pack verify --source ./code-polishy-example
code-polishy pack install --source ./code-polishy-example
code-polishy pack root
```

Verification executes only the declared conformance fixtures. Installation
executes no pack code. It validates the complete source before writing,
rejects links and special files, hashes every file, writes an installation
receipt, and atomically publishes an immutable content-addressed directory in
the local Code Polishy user-data root.

The default root is
`${XDG_DATA_HOME:-$HOME/.local/share}/code-polishy/packs/` on Unix and
`%LOCALAPPDATA%\CodePolishy\packs\` on Windows. Each installed tree is stored
under its name, version, and digest. There is no unversioned `latest` path.

The install command prints the name, version, and digest. Select that exact
identity in the target `.code-polishy.json`:

```json
{
  "packs": [
    {
      "name": "example-rust",
      "version": "1.0.0",
      "digest": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    }
  ]
}
```

The repository lock continues to select only the Code Polishy engine release.
A selected pack is unavailable until its exact installed receipt exists on the
current machine. No absolute installation path is stored in project policy.

## Contributor workflow

1. Inventory the language, dependency ecosystem, native tools, supported
   platforms, and existing conventions.
2. Map real tools to every required capability. State unsupported cases.
3. Pin or bundle pack-owned dependencies with source and license provenance.
4. Implement protocol version 1 without auto-installing tools during checks.
5. Add passing and deliberately failing fixtures for every capability.
6. Run `pack verify`, install the candidate, select its exact digest in a
   disposable project, and run `doctor --strict` plus the normal gates.
7. Publish the directory as an independently owned repository if desired.

Normal execution verifies the installed receipt before starting an adapter.
`doctor --strict`, checkpoint gates, and merge gates verify each selected pack
tree. Pack findings use ordinary Code Polishy reports and exceptions; missing,
corrupt, unsupported, or incomplete packs fail visibly.
