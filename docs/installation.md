# Installation

Code Polishy runs from one verified native release built from an exact reviewed
source version. The version tag is the distribution contract; GitHub Release
assets are not part of installation. This page describes source installation,
what it produces, how a target selects the exact release it requires, and what
makes that release acceptable to run. Target configuration is covered in
[Adoption](adoption.md).

## Agent-first workflow

The normal user experience starts with the one bootstrap request in the root
[README](../README.md#set-it-up). After adoption, the installed `AGENTS.md`
owns recurring operating guidance.

The agent follows [AI-Agent Setup and Adoption](ai-adoption.md). It preserves an
existing target lock. For a new adoption it honors a caller-specified exact tag
or resolves the highest strict stable `v<MAJOR>.<MINOR>.<PATCH>` tag, then makes
a temporary shallow clone of that tag. The clone must prove that the selected
ref is annotated, points directly at `HEAD`, and matches `VERSION`. The default
branch supplies current instructions; it is never an installable release.

A private repository URL or clean local checkout may be supplied explicitly for
private development. The same exact-source rules apply, and the agent never
substitutes another repository or revision. Published tags are immutable: never
move or reuse one after consumers can select it.

## Native source installers

Source acquisition stays outside the installers. They never select a version,
fetch, pull, switch revisions, invoke `gh`, or call a GitHub API. They require a
clean Git checkout, read its exact commit, and build only what is already there.
Maintainers can therefore use the same installers for a clean candidate commit
before its version tag exists.

From a verified checkout on Linux or macOS:

```sh
./tools/install-policy-tools.sh
./scripts/install.sh
```

From a verified checkout on Windows x64, in PowerShell:

```powershell
.\tools\install-policy-tools.ps1
.\scripts\install.ps1
```

Windows requires Git and PowerShell but does not need WSL or Git Bash. The
PowerShell installer locally builds a temporary native ZIP, verifies its
checksum and complete release manifest, installs it atomically, and deletes the
ZIP. That archive is an internal staging boundary, not a downloaded or
published release asset.

When Windows does not grant symlink creation privilege, pnpm materializes its
isolated dependency graph with absolute directory junctions. The tool installer
therefore builds that graph only at its stable transaction-owned location and
removes incomplete output on failure; moving a completed graph from a different
staging path would leave its junction targets pointing at the wrong tree.
The native release builder uses the same frozen lock and offline store to ask
the pinned pnpm for its portable hoisted layout, converts its remaining links
to regular entries, writes the bundle manifest, and proves the staged runner can
report provenance before creating the ZIP. The portable layout is confined to
the sealed release bundle and never resolves dependencies from a target.

The tool installer is the only networked installation stage. It admits only
the checked-in pinned tool artifacts, verifies every archive before extraction,
builds the pinned Go analyzers with the pinned Go toolchain, and installs the
JavaScript graph from its frozen lock with lifecycle scripts disabled. The
source installer then refuses to run until the sealed runtime, bundle, and every
other pinned tool are present and verified. Its manifest records every staged
byte, so the launcher verifies the carried tools with the rest of the release
before it runs.

The release gate separately resolves every standalone tool pin against its
fixed upstream publication metadata and enforces the shared 30-day minimum.
Checksum verification proves which bytes were acquired; release-age admission
proves the selected upstream release has matured or carries one exact,
expiring assessment.

The Unix prefix defaults to `~/.local/share/code-polishy`; the Windows prefix
defaults to `%LOCALAPPDATA%\CodePolishy`. Releases are installed under
`<prefix>/releases`, and the stable launcher is installed under `<prefix>/bin`.
The default Unix install also creates the guarded command link
`~/.local/bin/code-polishy`. It refuses to replace an unrelated path there.

The installers report command discovery and print a session-local `PATH`
command when needed. Persistent `PATH` setup is explicit:

```sh
./scripts/install.sh --add-to-path
```

```powershell
.\scripts\install.ps1 -AddToUserPath
```

On Unix, `--add-to-path` adds one owned entry to the supported shell startup
file for future shells. `--path-profile <file>` selects a specific startup file
when needed. On Windows, `-AddToUserPath` adds the launcher directory to the
user `PATH` for future processes. Repeating either operation is idempotent.

A custom Unix `--prefix` alone keeps installation writes under that prefix. Add
`--command-dir <directory>` to create a guarded command link elsewhere.
`code-polishy doctor` reports command discovery for an installed release.

## What a release contains

One release identity has one self-contained policy root per supported host:

- the `code-polishy` binary built from the reviewed commit with the pinned Go
  toolchain;
- the sealed Node runtime and JavaScript tool bundle;
- every other pinned tool the engine runs: the Go toolchain, ShellCheck,
  staticcheck, govulncheck, OSV-Scanner, Ruff, Vulture `2.16`, `ty`, and the
  carried CPython `3.12.13+20260728` runtime from python-build-standalone. A
  target installs no policy tooling, and none of these is ever taken from an
  ambient `PATH`, a host installation, or an environment override, so a check
  decides the same thing on every machine that has the matching host release;
- the version-matched `README.md`, `CHANGELOG.md`, permanent `docs/` tree, and
  documentation catalog;
- the configuration schema, templates, canonical guidance, pinned tool versions,
  and native workflow contracts the engine reads at runtime;
- the bundle's dependency and license inventory;
- the launcher, which the installer also copies to `<prefix>/bin/code-polishy`;
  the default Unix installer links `~/.local/bin/code-polishy` to that stable
  path; and
- `release-manifest.json`, which records everything above.

From a managed repository, read its exact locked guides without locating the
release directory:

```sh
code-polishy docs list
code-polishy docs find dependency review
code-polishy docs read installation
```

The files remain available below
`<prefix>/releases/<version>-<releaseDigest>/docs/`. Documentation and catalog
bytes are recorded in the release manifest like runtime inputs, so a changed,
missing, or added file makes the installed release invalid. Agent guidance is
delivered through generated root files and the versioned documentation CLI.

## Community language packs

Language packs are installed separately from engine releases and never change
`.code-polishy.lock.json`. From a repository already using the intended locked
engine release, verify and install a reviewed local source directory:

```sh
code-polishy pack verify --source ./code-polishy-example
code-polishy pack install --source ./code-polishy-example
code-polishy pack root
```

Installation prints the exact name, semantic version, and content digest to add
under the target configuration's `packs` array. It performs no download and
executes no pack code. See `code-polishy docs read adding-a-language` for the
manifest, protocol, trust, and contributor contracts.

A release carries no source checkout, no history, and no build inputs.
Installing does not modify the checkout it was built from.

## The release manifest

The manifest answers two different questions with two different digests.

`releaseDigest` names which reviewed commit, which version, which capabilities,
and the exact version of every executable the release carries. Nothing host
specific contributes to it, so Linux, macOS, and Windows releases from the same
commit carry the same value and a target lock can require an exact release
without naming a platform. Their entry lists and `contentDigest` values differ
because executable formats and other carried bytes are host specific.

`contentDigest` and the per-entry list name the exact installed bytes on this
host. Every installed file contributes its SHA-256. On hosts that support
release symlinks, every link contributes its exact target: the bundle is linked
together by pnpm's isolated linker, so retargeting one link would otherwise
swap the code a release runs without changing an installed file.

Each manifest version has an exact schema and identity calculation. The engine
accepts only its own version. The stable launcher keeps explicit readers for
installed manifest versions 2 through 4 so installing a newer release does not
strand repositories still locked to an older one. It verifies each older
release using that version's original fields and never invents evidence added
by a later schema.

Manifest version 4 adds `tools.python` for the carried CPython
`3.12.13+20260728` runtime and `tools.vulture` for Vulture `2.16`. Those fields
contribute only to version 4 identities; a version 3 release remains bound to
the smaller tool inventory it originally recorded. The target configuration
version is a separate contract.

`code-polishy release-manifest verify --root <release-dir>` recomputes the
installed entry evidence. A release that was truncated, changed after
installation, or copied from another host fails verification and is reinstalled
rather than executed. The launcher makes the same native judgment before every
run on Linux, macOS, and Windows.

## Installing is all-or-nothing

Before it stages anything, the installer asks every tool the release will carry
what version it is and requires the answer to be the version the checked-in pin
beside it names — the Go toolchain, Node, pnpm, ShellCheck, staticcheck,
govulncheck, OSV-Scanner, Ruff, Vulture, `ty`, and carried CPython. The manifest
records those identities, and a present file and a byte inventory cannot show
that a local tool cache holds the version the manifest would claim. The two Go
analyzers are read out of their binaries with the pinned toolchain rather than
asked: `govulncheck -version` contacts the vulnerability database, and
installation reaches no network. The engine and the launcher are built here
from the reviewed commit rather than acquired, so what they are is the source
revision the manifest already records.

The installer verifies the CPython archive before extraction and the Vulture
`2.16` wheel before unpacking its pure-Python package into that carried runtime.
It does not use `pip`, a target `.venv`, or a target Python installation for
Vulture.

The installer stages a complete release, verifies the staged tree against the
manifest it just wrote, and only then moves it into place under one name. A
previously installed release is untouched until a verified replacement is ready.

An installation that ends after staging has begun — a step that fails, or an
interrupt — removes its own partial tree. A staging tree is never a release a
target could run, because no lock can name one, but a release store holds only
complete releases.

Before the manifest is written, the installer searches the staged tree for
retired distribution mechanisms that must never reach a target: a
`check_policy.sh` or `.gitmodules` entry, a checkout, a retired workflow wrapper,
or policy-owned text that still tells a target to run a submodule command. A
release is the whole Code Polishy interface a target gets, so one of those
reaching a target through a template, canonical guidance, or workflow script is
an installation failure rather than something to find later. The sealed
runtime and bundle are not searched for Code Polishy's own
retired commands; they are third-party bytes, and the bundle inventory and
manifest govern them.

Release directories are named `<version>-<releaseDigest>`, so releases from
different reviewed commits coexist and each target selects the exact one it
requires. Reinstalling a commit whose release is already installed and verified
keeps the installed bytes rather than replacing them.

## The target lock

A target repository names the release it requires in `.code-polishy.lock.json`:

```json
{
  "lockVersion": 1,
  "codePolishyVersion": "0.6.0",
  "releaseDigest": "…",
  "features": ["javascript-bundle"]
}
```

That is the whole file. It carries no path, credential, URL, channel, fallback
version, or platform-specific digest, so the same lock selects the same release
on every supported host.

Writing the lock is an explicit operation, and only the release being required
performs it. Run `lock` from that release, in the target repository:

```sh
"${HOME}/.local/share/code-polishy/releases/<version>-<releaseDigest>/bin/code-polishy" lock
```

The installer prints that command when it finishes. Afterwards the target runs
`code-polishy`, and no other command rewrites the lock.

## Selecting a release

`<prefix>/bin/code-polishy` is the launcher, and it is the only Code Polishy
command a target runs. It reads the target's lock, resolves the one release that
lock names, and confirms that release records the same version, digest, and
required features and was built for this host.

It then verifies the release itself before running any of it. It recomputes the
release the manifest describes from the commit, version, capabilities, and pins
recorded in it, so a release cannot be installed or copied under another
release's name; it reads every recorded file and confirms the recorded bytes,
reads every recorded link and confirms the recorded target, and refuses anything
installed under the release that the release does not record. Only then does it
hand the release the target and the caller's arguments. The engine reads the
schema, templates, pinned versions, workflow scripts, and sealed JavaScript
bundle beside it, so checking the engine binary alone would not be checking what
runs, and verifying at installation time cannot answer what a release is made of
now.

There is no channel, version range, newest-wins rule, fallback release, or
download. A target that names a release this host does not have is told the
digest it requires and to install it locally; it is never given a different one.
`--policy-root` is refused, because the lock decides which release runs.

Running a release binary directly does not get around the lock: an installed
release refuses to govern a repository whose lock names another release, or a
repository with no lock at all. The exceptions are `lock`, which writes the
missing lock, and `version` and `help`, which report what the executable is.

An installed release is not a supported development environment, and the source
runner in this repository's `bin/` is not a supported target installation. It is
not a release, so no target lock can name it.

## Self-hosting

This repository checks in its own `.code-polishy.lock.json`, so the release it
produces is exercised against a real target rather than only against fixtures:
the launcher resolves that release, verifies every recorded byte, and governs
this repository with the pinned tools the release carries.

A release digest names the commit it was built from, so this repository's lock
names the release of an earlier reviewed commit and never of the commit that
carries the lock: writing the lock changes the tree, and the tree it changes
would build a different release. Refresh it the way any target does — install a
release from the reviewed commit, run `lock` in this checkout from that release,
and commit the lock as its own reviewed change — and keep that release installed
for as long as the lock names it, because the launcher resolves that one and no
other.

Development still runs the source runner. This repository's `AGENTS.md` is the
same exact canonical file every target receives, so the installed release
governs the checkout without a guidance exception. `agents sync` replaces that
whole file when a later locked release changes the canonical contract and
repairs the root `.gitignore` rule that keeps report artifacts workspace-local.

## Exercising an installed release

Self-hosting covers one target shape: a Go repository that also owns the sealed
bundle's source. `./scripts/test-installed-release.sh` covers the others. It
builds disposable repositories — Go with no JavaScript, a pnpm application, a
pnpm workspace, TypeScript, and React — gives each the lock this checkout
requires, and governs them with the installed launcher. The Go target also runs
the complete non-documentation flow: pre-coding intent capture, review
preparation, red/green proof, finalization, checkpoint, and merge. It verifies
requested and preserved changes and confirms that an unintended change blocks.

Each target is first brought to a clean pass, then given one defect and
required to produce the exact finding for it, because a pass that no defect can
disturb is a check that did not run. The script installs nothing and reaches no
network; it needs an installed release named by the selected lock. `--prefix`
selects the release store, and `--lock` can select a temporary candidate lock
without changing this repository's checked-in lock.

What it proves is the release the selected lock names. By default that is this
checkout's lock, which must be refreshed before validating a newly published
release. A temporary lock can name a clean installed candidate before the public
cutover. The script is not part of the ordinary test suite because a release
store is outside the checkout and CI normally builds the engine from source.
