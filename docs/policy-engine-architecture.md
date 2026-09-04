# Policy Engine Architecture

Code Polishy exposes a small CLI over target facts compiled together with
auto-detected shared policy modules. The target declares facts; the pinned
engine owns their interpretation and the shared baseline.

```text
cmd/code-polishy
      |
      v
internal/engine -------------------------------------- orchestration and one report
   |        |         |          |              |          |             |
   v        v         v          v              v          v             v
quality  architecture  testing  behaviorreview  supplychain  policymodule  portability
   \        |         /          /              /          /             /
    +-------+--------+----------+--------------+----------+-------------+
            |
   policy + repository + runner + javascript
```

Language packs add one generic `internal/pack` boundary beside
conditional modules. It validates local manifests and installation receipts,
compiles exact selected providers into the ordinary capability profiles, and
exchanges one bounded JSON request and response with each contained adapter.
Pack code always remains in a child process; no language-specific implementation
or third-party code is loaded into the Go engine.

## Deep boundaries

`internal/policy` decodes JSON with unknown-field rejection, applies immutable
defaults, validates module graphs and command safety, and returns a compiled
configuration. Invalid version, path, graph, suite, exception, or weakening
never reaches execution.

`internal/repository` owns Git selection, deletions, staged/worktree safety,
segment-aware globs, containment, language detection, module ownership,
immutable-base change boundaries, nested Go module discovery, the shared
Python project inventory, and the exact clean-candidate, ancestor, binary-patch,
and disposable-worktree primitives. The Python inventory parses each contained
`pyproject.toml` once, assigns `.py` and `.pyi` files to their nearest project,
and provides project roots, `src` roots, namespace candidates, and project-local
environment facts to every consumer.

`internal/behaviorreview` owns the pre-implementation intent journal, packet
preparation and its marker, strict review-result and receipt validation,
candidate-material re-derivation, behavior-proof records, disposable
baseline/candidate replay, and the atomic accepted-checkpoint receipt. It
depends only on policy, repository, and runner. The agent harness supplies the
user's request at a clean task boundary, then later starts a review subagent
with no inherited conversation and gives it the bounded packet. The module
gives both checkpoint and merge gates candidate-bound executable evidence.
Local artifacts bind bytes and commits; they do not authenticate the request's
source, subagent identity, or context.

`internal/runner` is the subprocess boundary for target-declared commands. It
accepts argument arrays, resolves working directories and checked-in
executables, rejects symlink escapes, streams native output, and enforces
timeouts. It also owns ordered advisory resource leases: every performance
suite receives the non-removable `performance` lease, and same-user sessions
share a private `/tmp` namespace keyed by Unix user ID. Waiting is cancellable,
does not consume command timeouts, and reports periodic progress through the
shared execution renderer. On Unix it contains each command in its own process
group, so timeout or cancellation cannot leave a descendant running after the
command’s receipt closes.

The runner also owns the one host-runtime interface used by every product
process boundary. `Run(ctx, command)` starts a contained command and returns its
exact leader exit status only after the contained process tree is quiescent.
`Handoff(ctx, command)` transfers the current delivery process to a command, or
uses the native equivalent on a host without process replacement, while
preserving argv, Unicode, working directory, the complete supplied environment,
stdin, and separate stdout and stderr. Startup failure is distinct from a
started command's exit. Cancellation requests graceful termination, escalates
after the owned grace period, and does not return while descendants or
descendant-held streams remain. Platform process IDs, process-group IDs, Job
handles, signals, and control operations are private implementation details of
this boundary. Callers may select Run or Handoff and consume the observable
result; they may not perform host-specific process control.

`internal/javascript` is the one adapter to the sealed, policy-owned JavaScript
tool bundle, and is launched only for a target that actually bears JavaScript or
TypeScript. Its direct operations exchange one bounded JSON request and one
bounded JSON response with the fixed runner entry point, started by the pinned
Node runtime at one policy-owned path, in a scratch working directory it deletes,
under an environment built from nothing rather than filtered — so credential,
proxy, loader, debugger, package-manager, and user-configuration variables are
absent by construction. The native audit renders its same closed request as a
sealed common-runner command, so its progress, timeout, resource wait, duration,
logs, and receipts are visible with every other governed command. The protocol
version and the operation enum are closed, an unknown response field is rejected,
responses are size bounded, and a cancelled or overrun operation kills the
runner's whole process group. Tool-native objects never cross the boundary and
the adapter decides nothing: the owning Go packages turn its facts into findings.

An operation that reads the target names it explicitly. The request carries one
normal absolute target root and clean repository-relative paths inside it, both
sides refuse a path that could reach another tree, and the selection is count
bounded. Containment is then decided by what a path really names rather than by
how it is spelled: every read is checked against the resolved target root, so a
selected path, a resolved module, a type root, or an extension chain that lands
outside the repository through a linked directory is refused instead of read.
Writing is its own operation rather than a flag on a read, so the bundle only
rewrites a file when a Go caller asked it to, and only inside the target.

An operation that runs rules carries them. A lint request states the exact
allowed maximums and the closed framework activation Go resolved, so the bundle
never defaults, translates, or discovers a policy threshold, and a target
configuration file or inline directive changes nothing about which rules ran.
The lint response carries bounded parser comment facts and whether each fact is
complete; Go alone owns the closed machine-directive registry and turns every
other fact into a source-comment finding when the target selects the strict
comment policy.
A typecheck request names the one contained project the selection is checked
under, so the bundle resolves no project of its own; Go owns which project
governs which file and requires that a governed file was actually covered.
A deadcode request names one tree of packages, the exact governed files each
package contributes, and which of those files are entry points, so the bundle
discovers no package, entry point, or configuration; a target analyzer
configuration is reported rather than read, and every analyzer plug-in is
disabled because a plug-in would load target configuration to learn a
framework's entry points.
An imports request names only the selected source, because which file a written
specifier names is the compiler's answer rather than a path rewrite Go could
approximate, and which specifiers name a module the runtime provides is the
pinned Node's; resolution reads inside the target root alone, and Go owns which
resolved edge crosses a module boundary it did not declare and which reached
package the owning manifest never declared.
A packages request names one pnpm project, because its lockfile is YAML and a
standard-library-only engine could only approximate the format the bundle reads
with the parser pnpm itself uses; the bundle reports what each workspace package
declared, what the lock resolved for it, and the kind of source each resolution
came from, and Go owns whether that is drift, an inadmissible source, an
unaged release, or a vulnerability.
A workspace request names the pnpm settings file that governs a project, for
the same reason and with the same split: every setting crosses as the text it
was written as, and Go owns which file may own those settings, which native
protections the pinned pnpm version must carry, and what each declared value
has to be. That file is found in the governed tree rather than in the
selection, so changing a manifest alone still reads the settings that govern
it.
A licenses request names one pnpm project because a lockfile records what a
target installs, never what those packages may be used under: that fact exists
only in each installed manifest, which the bundle reads from the project's own
virtual store. Every stored release crosses as the exact expression it declared,
and Go parses that expression and decides it against the target's allowed
licenses; a release the tree has no metadata for, and a tree the reader could
not read, are missing coverage rather than releases with nothing against them.
An audit request names one pnpm project and is the one operation that contacts
a registry and the one that starts a second process. That process is the pinned
pnpm installed beside the bundle, launched by the runtime already running the
runner, against one policy-owned registry named on the command line; it audits
from the lockfile and installs nothing, so no lifecycle script and no target
code runs. The registry's advisory objects do not cross: an advisory becomes an
identity, a package, a severity, and the exact releases it was reported against,
and Go owns the severity threshold, the assessments, and how the result
reconciles with the independent OSV lane.

`internal/quality`, `internal/architecture`, `internal/testing`, and
`internal/supplychain` each hide their parsing and tool choreography behind
structured findings. None decides process exit status or prints final policy
results.

The Python consumers share the repository inventory rather than independently
guessing roots. The inventory derives the Ruff target from
`project.requires-python`, records project, direct `src`, and in-tree PEP 517
backend roots, and fixes the shared line length at 88. `internal/quality` runs
the carried Ruff baseline, C901, target additions, Vulture `2.16` full-project
dead-code analysis, and structured `ty` diagnostics with those same roots.
Vulture runs through carried CPython `3.12.13+20260728` from
python-build-standalone, loads its version-matched standard whitelists, derives
PEP 621 entry-point and in-tree backend-hook symbols, and accepts only validated
exact `scope.pythonDynamicReferences` for remaining dynamic symbols.
`internal/architecture` asks the same carried Ruff for an
isolated import graph and decides module direction in Go; `internal/supplychain`
parses PEP 508 and `uv.lock` facts from the same validated manifest boundary.
Target Ruff configuration cannot alter the managed Python target, roots, line
length, baseline, or architecture graph; target Vulture configuration is
ignored, and a dependency-bearing project gives `ty` only its explicit
contained `.venv`, never an ambient Python environment.

`internal/portability` performs linear, subprocess-free scans for
high-confidence machine/checkout assumptions and validates ownership coverage
for declared external inputs. It returns structured non-blocking advisories;
target suites prove runtime resolution and degraded behavior.

`internal/policymodule` detects Python, Node, React, Electron, and supported
dependency evidence across package roots. Python activation starts from the
shared inventory rather than a target tool configuration. It compiles centrally
owned commands, framework requirements, and test obligations into the same
capability model before orchestration. Generated commands cannot be authored
through JSON and receive only their matching selected files when a tool supports
safe focused execution.

`internal/engine` composes checks, applies exceptions exactly once, and returns
one report. Merge-gate reports also carry the selected policy level, trusted
base label, and deterministic reasons; the CLI renders that disclosure before
findings. Checkpoint reports carry their scope, supplied base, exact candidate,
and acceptance-receipt path. Behavior-regression review runs before ordinary
non-documentation checkpoint or merge work. The CLI translates reports to exit
statuses:

- `0`: the requested profile completed without findings;
- `1`: policy or behavior findings;
- `2`: invalid invocation/configuration or an operational prerequisite that
  prevented a valid run.

Advisories render as `WARN` alongside a successful report and do not alter
these exit statuses.

`internal/engine` also owns native task-session worktrees, worker process
lifecycle, artifact retention, boundary validation, and optional fast-forward
promotion. The CLI parses caller authority and forwards cancellation signals.
The worker owns implementation, native-subagent coordination, tests, and
review.

## Language-agnostic core

The core does not contain project names or layouts. It recognizes executable
languages for fail-closed inventory, provides especially strong native Go and
Shell checks, and accepts target-native capability providers for everything
else.

Standard ecosystems use conditional policy modules rather than copied target
commands. Genuinely project-specific extensions remain executable argument-array
commands rather than dynamically imported Go, Python, or JavaScript. This keeps
the engine independent from target runtimes and makes process, timeout, and
path safety uniform.

## Module and test planning

Modules are named DAG nodes. Every executable file has exactly one owner.
Focused test suites attach to nodes:

```text
explicit module ---> that module's focused suites

changed files ---> changed modules ---> reverse dependency closure
                                      ---> impacted focused suites

merge-base delta ---> impacted modules + matching standard suites
                 ---> recommended option
                 ---> risk/size heuristic ---> suggested option

merge-gate delta ---> exact candidate classifier
                 ---> ordinary Markdown ---> built-in documentation contract
                 ---> behavior decision ---> optional ---> no review artifacts
                                          ---> selected ---> receipt + proof replay
                 ---> shared escalation rules + target module allowlist
                 ---> recommended pipeline OR complete full gate

checkpoint delta ---> unchanged ---> no-op
                 ---> ordinary Markdown ---> built-in documentation contract
                 ---> behavior decision ---> optional ---> no review artifacts
                                          ---> selected ---> receipt + proof replay
                 ---> affected checks + focused changed tests
                 ---> gate-run owner ---> bounded command logs + versioned JSON report
                 ---> complete pass ---> accepted-HEAD receipt

merge execution ---> gate-run owner ---> immutable command attempts + failure evidence
                                      ---> explicit resume ---> matching passed ordinary tests only
                                      ---> proofs/checks/builds/security always execute

full profile ---> every suite marked full

supplemental profile ---> mutation / acceptance mutation / CRAP / risk suites
                     ---> selected only by caller, event-specific workflow, or stable release checklist
                     ---> declaration never implies execution
```

This makes narrow iteration possible without allowing a foundation change to
skip unchanged consumers. Quick focused suites nest inside the recommended and
full profiles. Standard cross-module suites may join recommended; expensive
browser, visual, E2E, performance, and live workflows remain full-only. The
read-only planner reports both profiles and, with a trusted base, the same
selected policy and `merge-gate` path that the executable merge gate will use.
It lists supplemental strength suites separately so their lifecycle stage and
runtime cost are not hidden inside the word “full,” and does not select them
because they are declared.

The executable merge gate is distinct from that read-only planner. Its only
caller input is a Git base. Repository selection preserves the exact candidate
delta separately from any repository-wide analysis expansion.
`internal/testing.BuildMergeDecision` makes the deterministic ordinary level
decision from that selection and compiled policy. `internal/engine` separately
builds one behavior-review decision from ordered intents, additive task
requests, base policy, candidate policy, and repository-owned path/module
impact. Optional review loads no receipt and runs no proof. Selected review
validates observable behavior plus evidence-bound final-state findings, replays
every behavior proof, and forces its ordinary feature suites into the
deduplicated test plan before later commands. Both decisions and the review
status are bound into the gate-run identity and report. Report artifacts are excluded
from the candidate delta so they cannot change either decision. The recommended
branch runs strict doctor, applicable gate checks and builds, recommended tests,
and offline supply-chain verification. For non-documentation candidates, any
repository-wide expansion,
path-ownership failure, non-allowlisted module, broad planner impact, or missing
adaptive configuration selects the existing complete gate.

`checkpoint-gate` is a separate task boundary. It requires an explicit previous
checkpoint and a clean committed candidate. Unchanged work is a no-op. The same
behavior decision reports optional review as `NOT RUN`, enforces task-requested
or checkpoint-required features, and leaves merge-only features for final
merge. Selected evidence is validated and replayed before the normal
change-aware check and focused impacted tests. Only a finding-free run with an
unchanged HEAD writes the accepted-checkpoint receipt. A two-phase publication
binds that receipt to the exact behavior status, passed run identity, execution,
and report digest; the receipt is accepted only while the report remains current
and validates. The receipt records state but does not select the next base
automatically.

The gate-run owner gives checkpoint and merge execution a single durable
artifact contract. It binds the exact candidate, base, loaded policy, locked
release, platform, effective command environment, and complete command plan to
bounded logs and a versioned report. A normal run executes every phase. An
explicit merge resume may reuse only a validated passed ordinary-test receipt
from an otherwise identical failed run; behavior proofs, checks, builds,
supply-chain work, and artifact security remain fresh. A reused suite receives
a receipt in the current execution with validated provenance to the original
executed suite, preserving safe chained retries after repeated late failures.

## Fail-closed planning

`doctor --strict` builds a coverage matrix from actual governed files:

```text
executable file -> exactly one module
module/language -> built-in or declared quality capabilities
source/dependency evidence -> conditional shared policy modules
external input -> owning module + quick contract + ordinary full behavior
module             -> focused boundary test
project            -> repository full suite
capability         -> required full test kind
declared supplemental kind -> matching supplemental suite, never automatic execution
governed .feature  -> full executable acceptance
manifest           -> supported built-in/conditional path or security provider
command            -> contained cwd, executable, timeout, and profile
```

Unsupported code, ambiguous custom-language mappings, missing tools, empty
placeholder modules, hidden protected inputs, and missing test layers are
findings. Targets cannot configure a universal exclude or suppress a
`policy.*` finding.

## Target and policy isolation

A target checks in `.code-polishy.json` and `.code-polishy.lock.json` and
nothing else about Code Polishy. The installed launcher resolves the one release
that lock names, verifies it, and passes the target's repository root to it
separately. The engine reads config and source from the target while resolving
policy-owned tools from the release beside it. A release carries every pinned
tool the engine runs — the sealed Node runtime and JavaScript bundle, the Go
toolchain, ShellCheck, staticcheck, govulncheck, OSV-Scanner, Ruff, Vulture,
`ty`, and carried CPython — and none of them is resolved from an ambient `PATH`,
a host installation, or an environment override, so what a check decides does
not depend on the machine it ran on. The release manifest records the exact
version of each one, the installer probes every local tool against its
checked-in pin before staging a release, and a check compares what a tool
reports against that same pin rather than against a version compiled into the
engine.

This repository also keeps a development-only source runner at
`bin/code-polishy`, which executes the engine with the pinned Go version via
`scripts/go.sh`. It is not a release and no target lock can name it. The
release-owned Python runtime is policy tooling for Vulture, never a target
runtime.

## Testing strategy

Tests exercise public package boundaries with disposable directories and Git
repositories. They cover config compilation, glob semantics, selection,
symlink containment, process timeouts, module impact closure, architecture
edges, source quality, obvious vacuous-test rejection, explicit supplemental
kind requirements, executable Gherkin requirements, dependency declarations,
and CLI parsing. The repository's own opt-in mutation runner applies production
mutations in a disposable worktree rather than risking the source worktree.

Registry and advisory services remain external boundaries. Deterministic
parsing is tested locally; live checks use HTTPS-only redirect policy, bounded
responses and timeouts, or pinned ecosystem tools.
