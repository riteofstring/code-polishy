# AI-Agent Setup and Adoption

This is the single agent-facing entry point for Code Polishy setup. A repository
owner starts an AI coding agent in the repository to govern and points it at the
Code Polishy repository. The agent resolves one exact version tag, installs its
release when necessary, then adopts or repairs Code Polishy in the target.

Code Polishy remains deterministic. The agent discovers project facts, writes
the target configuration and missing project-specific entry points, and repairs
findings. The policy engine decides whether the result is complete.

## Invocation

The one bootstrap request appears in the root [README](../README.md#get-running).
After adoption, the installed `AGENTS.md` owns recurring operating guidance; a
user does not need to repeat Code Polishy instructions in later prompts.

A caller may name a particular `v<MAJOR.MINOR.PATCH>` version tag. Without one,
the agent preserves an existing target lock; for a new adoption it selects the
highest strict stable version tag published by the canonical repository. It
never installs from the default branch.

For private or offline development, the caller may instead provide another
repository URL, optionally with an exact version tag, or the absolute path to a
clean local Code Polishy checkout. A caller-supplied source is authoritative for
that task; the agent must not silently substitute the public repository or
another revision.

The invocation authorizes read-only Git discovery, a temporary exact-tag source
clone, normal checksum-verified policy-tool acquisition, local release
installation, target configuration, and the policy-owned verification stages
below. `gate` performs ordinary verification. Local supplemental hardening uses
a separately invoked stage. The invocation does not authorize changing the
selected Code Polishy checkout, invoking `gh`, calling GitHub APIs, adding or
updating target-local dependencies, pushing, changing repository visibility,
provisioning credentials, live-provider or other destructive probes, or
unrelated product refactors.

## Completion contract

Adoption is complete only when:

- the target's exact release is installed locally and verifies successfully;
- `.code-polishy.lock.json` names one exact installed release;
- `.code-polishy.json` describes actual project modules, edges, capabilities,
  generated paths, commands, and test suites;
- every governed executable file has exactly one module owner;
- conditional policy modules activate from real repository evidence;
- every required capability has a real deterministic provider;
- focused tests exist for every module and full evidence exists for the
  repository and declared application capabilities;
- every external input has explicit ownership, resolution precedence,
  compatibility diagnostics, and quick/full behavior evidence;
- declared local supplemental suites are executable and pass as a separately
  recorded local hardening stage;
- strict doctor and the ordinary gate pass, or a genuine blocker is reported
  precisely;
- completed task-owned changes are committed unless the caller explicitly
  requests an uncommitted handoff;
- the final handoff names the locked release, commands run, findings repaired,
  local supplemental hardening, and any remaining external action.

A config that merely parses is not an adoption. A green placeholder command is
not evidence.

## First-install setup wizard

For a target with no `.code-polishy.json`, inspect the repository read-only long
enough to tailor recommendations, then ask one bundled setup question before the
first target write. Show every deliberate choice, its default, the recommended
selection for this repository, and the evidence behind that recommendation.
Wait for one response and record the selected configuration explicitly. This is
a one-time policy setup, not recurring authorization for the agent to run normal
adoption work.

Present at least these choices:

| Choice                                                                        | Default                                           | Recommendation rule                                                                                                                                                                                                                                                                                                       |
| ----------------------------------------------------------------------------- | ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Source comments (`quality.allowComments`)                                     | `true`                                            | Keep `true` for human-written code or carefully curated existing comments. Recommend `false` only for a repository whose code is written and read entirely by AI and whose durable rationale belongs in mapped design documents. State that choosing `false` makes existing prose comments blocking before changing them. |
| Adaptive application merge gate (`verification.mergeGate.recommendedModules`) | Omitted, so application changes use the full gate | Configure it only when read-only inventory identifies clearly bounded content modules that can safely use the recommended lane; shared escalation rules still force full when impact expands.                                                                                                                             |
| Behavior-regression receipt (`verification.behaviorReview.required`)          | Omitted                                           | Configure it only when applicable merges can isolate a fresh packet-only reviewer and keep its reports under trusted custody; the merge gate replays cited proofs but cannot authenticate reviewer identity or context.                                                                                                   |
| Supplemental mutation or risk suites                                          | None                                              | Keep the default during initial adoption unless the repository already requires a supported suite or the owner deliberately chooses the additional cost.                                                                                                                                                                  |
| Gherkin methodology                                                           | Do not introduce it                               | Preserve and execute existing governed `.feature` files. Recommend adding Gherkin only when the owner wants executable behavior specifications as a working method.                                                                                                                                                       |

Write the selected `allowComments` value even when it is `true`, so a new
repository records the decision instead of relying on omission. Once the user
answers, continue installation, configuration, repair, verification, and commit
work without asking for permission at each step.

Do not present mandatory or fact-triggered controls as optional. Python Ruff
complexity, `ty` type checking, formatting, ordinary tests, dependency policy,
and vulnerability checks activate automatically. Modules, capabilities,
external inputs, artifact targets, product-input Markdown, and current design
mappings describe repository facts; declare them whenever those facts exist.
Conditional policy modules and their exact, expiring overrides are policy
mechanisms, not setup preferences. Agent reviews and task sessions remain
optional workflows selected only when requested or operationally applicable.
The behavior-regression receipt is a separate explicit opt-in and becomes
required for applicable non-documentation merge candidates when enabled.

For a repair or upgrade, preserve every explicit choice. Ask only about a newly
introduced material choice that the existing configuration does not already
answer; do not replay the entire wizard.

## 1. Establish both repository boundaries

Before changing anything:

1. Read the target's `AGENTS.md` files and contribution instructions.
2. Inspect `git status`, the current branch, and remotes in the target.
3. Treat all pre-existing target changes as user work. Do not overwrite, reset, or
   reformat unrelated files.
4. Detect an existing `.code-polishy.json`, `.code-polishy.lock.json`, or
   unsupported older integration. Replace unsupported integration directly;
   repair a current installation rather than installing a duplicate.
5. Check whether the target is a nested worktree or has repository-specific
   commit and verification requirements.
6. Select the Code Polishy version before cloning source:

   - If the target has a lock, select `v<codePolishyVersion>` from that lock.
     Do not upgrade it during setup.
   - Otherwise, honor an exact version tag named by the caller.
   - Otherwise, use read-only Git tag discovery against the caller-supplied
     repository URL. Consider only names matching strict
     `v<MAJOR>.<MINOR>.<PATCH>`, select the highest semantic version, and do not
     fall back if that selected tag later fails verification.
   - If the caller supplied a local checkout instead, use its exact clean
     current commit. A maintainer candidate need not already have a tag.

Use `git ls-remote --tags --refs "<code-polishy-repository-url>"` for remote
tag discovery. Do not infer a release from GitHub page ordering, a branch name,
or a GitHub API response.

For a repository source, create a unique temporary directory outside the
target and make a shallow, single-branch clone of the selected tag:

```sh
git clone --depth 1 --single-branch --branch "<selected-tag>" \
  "<code-polishy-repository-url>" "<temporary-code-polishy-checkout>"
```

Treat that clone as read-only. Before using it, require all of these facts:

- its `origin` is the exact caller-supplied repository;
- its worktree is clean and `HEAD` is one lowercase full commit ID;
- the selected ref is an annotated tag that points directly at `HEAD`;
- peeling the selected tag resolves to `HEAD`; and
- the tag is exactly `v` followed by the strict contents of `VERSION`.

If the tag is signed and the environment has an established trusted signing
configuration, verify the signature too. Do not invent a trust root during
installation. After verification, never fetch, pull, switch, reset, or edit the
selected checkout. Delete a temporary clone after installation and target
configuration are complete; never delete a caller-supplied checkout.

Use ordinary Git authentication already available for the caller-supplied
repository. Never invoke `gh`, call a GitHub API, request or expose a token, or
probe credentials merely to see whether they work. Stop when the target or
selected checkout is not a Git repository, version discovery is ambiguous or
empty, exact tag verification fails, the checkout is dirty when a release must
be installed, the required locked release cannot be produced from that tag, or
a material product or architecture decision cannot be derived safely.

## 2. Install or select the exact release

Use `~/.local/share/code-polishy` on Linux and macOS or
`%LOCALAPPDATA%\CodePolishy` on Windows unless the user names another prefix.
Inspect the target before installing:

- If `.code-polishy.lock.json` exists, preserve it and use only the exact
  release it names. Never replace it with a newer or merely available release.
- If no lock exists, use the release built from the selected tag or explicit
  local checkout. Do not choose another installed release merely because it is
  newer.

First look for that required release under `<prefix>/releases`. For an
existing lock, match its exact Code Polishy version and release digest. Without
a lock, require the manifest's `sourceRevision` to equal the selected checkout's
`HEAD`. Verify the candidate and stable launcher using the matching installed
release. On Unix, the manifest can also be verified directly with:

```sh
<code-polishy-checkout>/scripts/release-manifest.sh verify <release-directory>
```

If the release and launcher are already valid, reuse them without rebuilding or
downloading anything.

When the required release is absent, run the platform-native commands from the
clean selected checkout.

On Linux or macOS:

```sh
./tools/install-policy-tools.sh
./scripts/install.sh
```

On Windows x64, in PowerShell:

```powershell
.\tools\install-policy-tools.ps1
.\scripts\install.ps1
```

The tool installer acquires only the exactly pinned policy tools through the
checked-in checksum-verifying installers. It may use unauthenticated HTTPS
artifact downloads; it must not use a GitHub API, token, or ambient substitute.
The source installer performs no network access, builds the verified checkout,
verifies every staged byte, and atomically installs the native release. The
Windows installer uses a locally built temporary ZIP as an internal staging
boundary and deletes it; it never downloads or publishes a release bundle. If
either command fails, report the exact failure rather than weakening pins or
using another tool.

For an existing target lock, the installed release digest must equal the lock.
If installing its version tag produces another digest, leave the target lock
unchanged and report the identity mismatch.

For a target without a lock, run `lock` from the exact release path selected
above. Each installer prints this path. On Linux or macOS:

```sh
"${HOME}/.local/share/code-polishy/releases/<version>-<releaseDigest>/bin/code-polishy" lock
```

On Windows:

```powershell
& "$env:LOCALAPPDATA\CodePolishy\releases\<version>-<releaseDigest>\bin\code-polishy.exe" lock
```

After the lock exists, use `code-polishy` when it resolves to the installed
launcher. If the bare command is unavailable, use a caller-specified prefix or
probe `~/.local/bin/code-polishy`, then
`~/.local/share/code-polishy/bin/code-polishy` on Unix, or
`%LOCALAPPDATA%\CodePolishy\bin\code-polishy.exe` on Windows. Treat a bare
command failure as a discovery issue until these stable locations are checked.

Run `./scripts/install.sh --add-to-path` or
`.\scripts\install.ps1 -AddToUserPath` only when the caller explicitly requests
persistent `PATH` setup. The target records `.code-polishy.lock.json` and
nothing about the local source or installation path. Never vendor the engine,
add a submodule, or copy individual checker files. Report the locked version,
release digest, whether installation was reused or performed, and any remaining
`PATH` action in the final handoff.

## 3. Inventory before configuring

Explore the repository rather than guessing from its name. At minimum,
inventory:

- executable source languages and custom source extensions;
- manifests, lockfiles, package/workspace roots, nested Go modules, containers,
  and CI workflows;
- generated, vendored, archived, fixture, and build-output paths;
- existing formatter, compiler, linter, type, dead-code, architecture,
  dependency, security, and build commands;
- test frameworks and exact focused, integration, contract, browser, visual,
  E2E, content, performance, and live-provider entry points;
- meaningful concept owners and runtime/deployment boundaries;
- actual import/dependency direction and generated cross-language contracts;
- product capabilities such as backend, frontend, UI, visual, content, or CLI;
- local-substitutable, remotely owned, and truly external dependencies;
- separately owned repository/directory/file/service inputs, every resolver
  source and fallback, and the runtime behavior when each is unavailable;
- checked-in instructions governing test execution, external approvals, or
  releases.

Read existing scripts rather than trusting their names. Confirm that a green
command really selects and executes the evidence it claims.

## 4. Model real architecture

Start from `templates/minimal/.code-polishy.json` in the Code Polishy checkout.
The `typescript-go` template is an example for a genuinely mixed application,
not a directory convention to impose blindly.

Replace every placeholder. Define modules around concept ownership and stable
boundaries, not one module per arbitrary directory. Every executable file must
belong to exactly one module. `dependsOn` contains allowed direct internal
dependencies and must remain acyclic.

Use actual project capabilities. Do not declare `ui`, `visual`, `content`, or
another capability solely to silence or trigger a preferred test. Capabilities
describe the product surface and compile into evidence obligations.

Generated source remains governed. Exclude only dependencies, immutable vendor
trees, archives, and outputs genuinely governed elsewhere. Do not hide source,
manifests, lockfiles, or workflows to make inventory green.

Run early and repeatedly:

```sh
code-polishy list-files --all
code-polishy doctor --strict
```

Use their exact findings to refine ownership and coverage.

Declare separately owned repositories, directories, files, and services under
`portability.externalInputs`. Do not preserve an implicit sibling-checkout
assumption merely because it works on the current machine. If an optional input
uses warning behavior, require visible resolved-source/compatibility diagnostics
and disable only the affected feature; never render a failed integration as a
legitimate empty result. Attach a quick fixture contract suite and a distinct
ordinary full behavior suite as described in
`docs/policies/portability.md`.

## 5. Reuse policy-owned tools and add only project-specific providers

Allow source and dependency evidence to activate the shared Go, Shell, Ruff,
`ty`, Node/TypeScript, React, Electron, and OSV behavior. Do not copy equivalent
commands into target JSON.

Add target `checks` only for facts that cannot be shared safely, such as:

- an alias-aware non-Go architecture graph;
- a production build;
- a frozen workspace install or generated-lock consistency check;
- a domain-specific content/schema validator;
- a repository secret/SAST, license, or provenance scanner;
- an unusual artifact producer that emits the first-class manifest contract.

Every provider must execute a real contained argument-array command, declare
the modules and capability it covers, have a bounded timeout, and fail when its
evidence is missing. It must not auto-install packages during a policy check.

The installed release supplies the Go toolchain, ShellCheck, staticcheck,
govulncheck, Ruff, `ty`, OSV-Scanner, and the sealed JavaScript tooling. Do not
add target-local copies of those tools or ask a target dependency to satisfy
shared coverage. A missing or corrupted shared tool means the locked release
must be installed or repaired; it is not a target dependency gap.

Use existing target tools where honest for genuinely project-specific
providers. If such a provider requires a new or updated target dependency,
that dependency change is not authorized by the adoption request. First show:

- the project-specific capability the dependency would provide;
- the exact proposed version and dependency group;
- the repository's pinned package-manager command;
- the manifest and lockfiles that would change; and
- why existing target dependencies cannot provide the capability.

Wait for explicit approval before editing the target manifest or lockfile. If
approved, generate the candidate lock without lifecycle scripts and run
`code-polishy dependency-review --base <merge-target>` before the normal frozen
installation.

Optional supplemental mutation engines are not part of baseline adoption. The
one-time setup wizard presents the choice, but the agent installs or configures
no engine unless the repository already requires its mutation kind or the owner
selects mutation hardening. After the wizard, do not re-propose it. A new runtime
architecture, commercial service, credential, or substantial product dependency
remains a separate material choice: report it and request direction rather than
silently choosing one.

## 6. Define honest tests at each scale

Every module needs a quick focused suite exercising its stable boundary. Every
repository needs a repository-scoped full suite. Declared capabilities add
their documented integration, contract, component, browser, visual, E2E, or
content requirements.

Prefer existing modular commands. When the test runner supports safe path,
package, project, tag, or workspace selection, expose it through module suites.
Do not create a source-text assertion, empty test, unconditional skip,
pass-with-no-tests flag, or `echo` wrapper to satisfy coverage.

Mutation testing is optional. Do not install a mutation engine merely to make
an adoption complete. Reuse an already pinned target mutation tool when the
repository already declares mutation suites, or configure mutation only after
the owner explicitly chooses that supplemental hardening.

If a selected mutation engine cannot be installed without violating the
target's dependency, release-age, provenance, trust, or build-script policy,
skip mutation testing. Do not add an exception, weaken the supply-chain policy,
or build a repository-owned mutation runner as an adoption workaround. An
unavailable optional engine is not a blocker unless the target explicitly lists
its mutation kind in `tests.requiredSupplementalKinds`; in that case, report
the conflict and ask the owner whether to remove the opt-in requirement.

When a mutation suite is deliberately declared, it must:

- pin the mutation engine exactly;
- prove baseline tests run and pass;
- mutate production code in a disposable copy or worktree;
- invoke the tests capable of observing the mutations;
- fail its configured efficacy or mutant-coverage threshold;
- report survived and uncovered mutants;
- restore or discard all mutated state reliably.

Declaring the suite makes it policy-owned local hardening. The ordinary gate
deliberately excludes supplemental work. This direct adoption workflow runs
the declared local stage separately after the ordinary gate.

If governed `.feature` files exist, connect them to a real full acceptance suite
so the specifications execute. Supplemental acceptance-data mutation remains
optional local hardening and, when declared, follows the same separate direct
execution. Credentialed, destructive, production-mutating, and live-provider
probes remain external approval gates. Do not keep decorative Gherkin.

## 7. Install canonical agent guidance

Install the canonical guidance once:

```sh
code-polishy agents install
```

The locked release owns every byte of `AGENTS.md`. The command creates the
canonical file when missing and accepts an exact existing copy without a
rewrite. A noncanonical existing file is preserved and reported as a conflict.
It also creates the release's exact one-line `CLAUDE.md` redirect when absent,
keeping `AGENTS.md` as the single guidance authority. If `CLAUDE.md` differs,
the command preserves its bytes and changes neither guidance file; resolve that
explicit conflict before retrying. Use `agents sync` after later Code Polishy
upgrades; it requires an existing file and replaces the entire stale
`AGENTS.md` while preserving its mode. Do not hand-copy or duplicate the
canonical policy text.

Keep canonical guidance compact and limited to durable rules used across tasks.
The release-owned `polishy` skill and permanent docs carry command procedures,
policy rationale, and edge-case detail.

The resulting guidance should make these execution boundaries clear:

- exact/module/changed tests are routine during implementation;
- ordinary Markdown-only changes are formatted and validated without
  application tests or a user authorization prompt;
- ordinary interactive edits use the caller's checkout and finish with a
  coherent verified commit; task sessions are for requested isolation or
  unattended work;
- `test-levels` (and the `test-plan` compatibility alias) is read-only;
- agents resolve a trusted merge target and run `merge-gate --base <merge-target>`
  so Code Polishy selects documentation, recommended, or full execution without
  a user choice, and they do not immediately precede it with `test --changed`
  for the same candidate;
- when `verification.behaviorReview.required` is enabled, non-documentation
  merge candidates use the packet-only fresh-reviewer, red/green proof, and
  receipt workflow before `merge-gate`; the gate independently reruns cited
  proofs, while the runtime enforces reviewer isolation and the reports stay in
  the same workspace or move through an explicit trusted CI handoff;
- local supplemental mutation and risk work runs through its separate direct
  stage after ordinary acceptance; only credentialed, destructive,
  production-mutating, and live-provider probes remain external approval
  gates;
- policy upgrades rerun the complete ordinary gate.

## 8. Integrate CI without inventing credentials

Inspect existing CI before adding a workflow. Reuse the repository's frozen
dependency bootstrap, then run the same installed command used locally:

```sh
code-polishy gate
```

All actions and containers must satisfy Code Polishy pinning rules. Do not add a
floating GitHub Action merely for convenience.

The runner must already have the locked release installed, because a check
never downloads Code Polishy. If that runner image is not prepared, make the
expectation explicit and report the one required human administrative action;
do not weaken the lock or add a download step.

## 9. Establish the baseline without hiding debt

Iterate from narrow deterministic checks toward the policy-selected ordinary
gate. When adoption work changes only ordinary Markdown, run
`code-polishy format --git-changes` and skip the application commands below:

```sh
code-polishy doctor --strict
code-polishy check --all
code-polishy test --changed
code-polishy supply-chain --offline
code-polishy gate
```

`gate` runs ordinary verification and leaves local supplemental hardening to
its separate command. If it fails,
diagnose and rerun the smallest failing command until repaired; do not
repeatedly rerun every expensive suite while debugging.

Fix safe deterministic defects within the adoption change. Do not undertake a
large architectural migration, replace a test framework, or change product
behavior merely to obtain a green result without making the decision visible.

Do not invent broad exceptions. If existing debt cannot be repaired safely in
the adoption, propose an exact finding/path/subject exception with a named
owner, concrete rationale, and near expiry. Because ownership and debt policy
are human facts, request approval before using that exception to complete the
baseline.

## 10. Run local supplemental hardening

After the ordinary gate is green, inspect the declared supplemental suites and
run the separate policy-owned local hardening stage:

```sh
code-polishy test-levels --base origin/main
code-polishy test --supplemental
```

`test-levels` is read-only and executes no supplemental suites.
`test --supplemental` runs every declared supplemental suite. Credentialed,
destructive, production-mutating, and live-provider probes remain external
approval gates.

## 11. Verify and hand off

Before finishing:

1. Confirm `git diff --check` and target-specific formatting are clean.
2. Confirm `.code-polishy.lock.json` names the reported version and release
   digest and that `code-polishy version` agrees.
3. Confirm no placeholder command, broad exclusion, credential, generated
   mutation, or policy tool output was committed accidentally.
4. Commit all completed task-owned changes after required verification unless
   the caller explicitly requests an uncommitted handoff. Do not push or
   publish unless explicitly requested.

The final response must include:

- locked Code Polishy version and release digest;
- modules, dependency edges, capabilities, and conditional activations;
- project-specific providers and why they remain local;
- test profiles and exact commands actually run;
- ordinary gate result;
- local supplemental hardening result and any external approval gate;
- approved exceptions and expiry, if any;
- remaining external blockers such as a CI runner without the locked release.

## Upgrades

When asked to upgrade Code Polishy, the AI agent should:

1. read the target instructions and preserve its working tree;
2. honor an exact version requested by the caller or resolve the highest stable
   annotated version tag from the authoritative repository; never upgrade from
   floating `main`;
3. clone and verify that exact tag as described above, then reuse or install its
   exact native release;
4. read every intervening `CHANGELOG.md` entry;
5. rewrite `.code-polishy.lock.json` by running `lock` from that exact release;
6. update target configuration directly for changed requirements;
7. run strict doctor, inspect the test plan, run the ordinary gate, then run
   the separate policy-owned local supplemental stage;
8. report the supplemental hardening result separately from external gates;
9. commit the new lock and required target changes together unless the caller
   explicitly requests an uncommitted handoff;
10. delete the temporary source clone; and
11. never let CI or an application install choose a release the lock does not
    name.
