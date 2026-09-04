# Architecture and Agent Usability Hardening

Status: proposed

Target release: v0.24.0

## Outcome

Prevent a repository from appearing architecturally clean merely because its
code was placed inside one permissive module, make test ownership independent
of production path ownership, close exact Python reachability gaps without
creating symbol allowlists, distinguish requested evaluation scope from the
broader context a sound analyzer must read, make generated-source failures
safely repairable, and make every policy result directly usable by an automated
coding agent.

This plan follows v0.23.0. It must preserve the stricter parser, dependency,
evidence, and publication boundaries delivered there. Before implementation,
reconcile every named internal boundary and schema below against the released
v0.23 tree; do not restore a v0.22 implementation that v0.23 replaced.

The release is one breaking, atomic cutover. It provides no compatibility
alias, fallback reader, dual finding model, automatic waiver, or migration
path for superseded configuration. Code Polishy itself adopts the new contract
before v0.24.0 is complete.

## Evidence baseline

The initial adoption feedback behind this plan was collected from Code Polishy
v0.21.x, with a later Python-focused sample believed to include both v0.21.x
and v0.22. A subsequent live Codex adoption session,
`01a05f8e-20b9-7460-aa19-b160b63da465`, upgraded one repository through
v0.21.4 and v0.22.0 and then attempted to resolve the complete v0.22 finding
set. v0.22 already closed part of the earlier feedback, so this plan implements
the remaining delta instead of describing those capabilities as absent.

| Feedback area           | Already present in v0.22                                                                                                                                                                                                                              | Remaining work                                                                                                                                                                                        |
| ----------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Architecture visibility | `architecture` reports production files, test files, incoming and outgoing dependencies, and quick focused suites per module. Durable guidance already favors concept ownership and deep modules over catch-all ownership and forwarding-only splits. | Enforce cycles below the declared module level, identify suspiciously vacuous topology, and require a graph review before adoption or an architecture rewrite.                                        |
| Test ownership          | Every governed test must resolve to one production module and be included by a quick focused suite.                                                                                                                                                   | Stop deriving ownership implicitly from production module paths. Declare the owner and primary focused suite directly and provide useful unmapped-test diagnostics.                                   |
| Agent diagnostics       | Merge and checkpoint gates write JSON reports and bounded command logs.                                                                                                                                                                               | Make ordinary policy output summary-first and bounded, give every command a complete structured report, add SARIF and stable finding identities, and attach exact remediation and rerun instructions. |
| Evaluation scope        | File selectors retain whole-package context for JavaScript and TypeScript checks whose result cannot be computed file by file.                                                                                                                        | Accept contained directory and module selectors, expose requested selection separately from analyzer context, and stop forcing agents to construct unbounded file argument lists.                     |
| Generated remediation   | Generated executable source receives format validation and semantic checks while format writers correctly preserve generator-owned bytes.                                                                                                             | Bind each project-generated executable output to its source and producer, and never recommend a formatter command that is forbidden from rewriting the failing output.                                |
| Python reachability     | v0.22 infers exact Pydantic import aliases, re-exports, local subclass chains, fields, `model_config`, validators, serializers, and computed fields. It also supports exact dynamic symbols and external attribute writes through typed parameters.   | Infer TypedDict literal-key reads, support exact external writes through typed locals and `self`, and prevent free-standing or generated symbol inventories from suppressing dead-code findings.      |

The later session supplies two exact regression cases. A 15-file `--files`
selection produced 26 findings whose primary paths were all outside that
selection because whole-package analyzers ran without explaining the expanded
context. A generated TypeScript format finding instructed the agent to run
`code-polishy format`; that command reported a clean pass while correctly
leaving the generator-owned file untouched. The agent then searched unrelated
installed releases for a formatter and created a temporary source/output
synchronization script. v0.24 must make both paths unambiguous without weakening
the underlying checks.

v0.23 may improve any of these boundaries while this plan is waiting. Phase 0
must delete or narrow any requirement that became redundant while retaining
the observable outcome.

## Product rules

- Deterministic inventory, parsing, graph, ownership, and policy rules retain
  final authority over pass and fail.
- AI review handles qualitative architecture judgment. It cannot waive a
  cycle, invent missing import evidence, suppress ownership failure, or replace
  a separately required human approval.
- This plan introduces no mandatory human architecture approval. A clean-
  context AI reviewer may accept a coherent one-module design with evidence;
  that result is a review decision, not a policy exception.
- Module count, file count, percentage ownership, and dependency degree remain
  evidence rather than arbitrary pass/fail thresholds.
- A focused selector limits the requested evaluation boundary, not the context
  a sound package or project analyzer may read. Reports identify both sets and
  never present an unselected context finding as though the caller selected its
  path.
- Rendering filters never alter evaluation scope, the complete report, or the
  process exit status.
- Remediation uses repository and lockfile facts already available to the
  evaluation. It never queries for the newest dependency or silently edits,
  installs, upgrades, or executes lifecycle scripts.
- Generated executable source remains governed and generator-owned. A format
  writer never edits it, and a finding never names that writer as a remedy.
- CLI help, human summaries, machine reports, actions taken, and exit status
  describe the same command contract. A warning is counted, and a protected or
  otherwise skipped target is not described as successfully rewritten.
- Dynamic reachability is evidence about one real consumer boundary, not a
  symbol allowlist. A declaration without independently resolvable consumer
  evidence is invalid even when every named symbol currently exists.
- Machine output is versioned, deterministic, path-normalized, size-bounded,
  and independent of terminal prose.

## Canonical source dependency graph

Introduce one Code-Polishy-owned `source-dependency-graph/v1` fact model after
ecosystem parsing and resolution but before declared-module filtering.

Each node identifies one governed executable file by normalized repository
path, language, generated status, test status, project or package root, and
declared module owner. Each edge identifies its source and resolved target,
source location, ecosystem, and semantic kind, including runtime, type-only,
re-export, or statically proven dynamic import.

The graph is an engine fact contract, not a second parser:

- JavaScript and TypeScript consume resolved facts from the sealed language
  adapter.
- Python consumes the released v0.23 Python and Ruff fact boundaries.
- Go retains package semantics. An import edge targets a package rather than an
  invented file; a package cycle reports one real importing file as the witness
  for each edge.
- Other language packs may emit the same bounded graph capability. A language
  without complete resolution continues to require its existing architecture
  provider and cannot claim clean cycle evidence.
- External dependencies are absent from this repository graph. Missing,
  ambiguous, escaping, truncated, or unparsed local edges remain import-
  coverage failures rather than disappearing.
- Production, test, and generated classifications remain explicit. Test edges
  never authorize production dependency direction, but test-only cyclic
  components are still checked and identified separately.

Raw parser or tool types must not enter the graph, policy, reporting, or review
packages. The graph has exact node, edge, depth, and encoded-size limits. The
complete normalized graph identity participates in architecture-review and
reusable-evidence identities wherever those consumers rely on it.

## File and package cycle enforcement

Run cycle detection on the canonical graph before collapsing nodes into
declared modules. A same-module edge is therefore invisible to the module-
dependency check but never invisible to cycle analysis.

Use a small internal strongly connected component implementation over sorted
nodes and edges. Do not add a graph dependency for Tarjan or Kosaraju traversal.
For every cyclic component of two or more nodes, and for every self-loop:

- emit one blocking `architecture.fileCycle` finding for production or
  generated source;
- emit one blocking `testing.fileCycle` finding for a test-only component;
- include every component member and internal edge in the complete report;
- show one deterministic canonical witness cycle in human output;
- identify runtime, type-only, re-export, and proven dynamic edges without
  silently dropping any category;
- retain source locations and module owners for every witness edge; and
- derive the finding fingerprint from the sorted semantic component identity,
  not from traversal order or message text.

One strongly connected component can contain exponentially many simple cycles.
Code Polishy reports every cyclic component and all of its internal edges; it
does not attempt unbounded enumeration of every possible walk. Ordering and the
chosen witness must be identical across supported operating systems.

Cycle findings are not suppressible. A declaration that would turn a cyclic
component back into a green architecture result recreates the original false-
green condition. If an ecosystem cannot distinguish an edge category reliably,
it fails coverage rather than assuming that category is harmless.

## Non-vacuous architecture review

Keep the v0.22 architecture summary and add deterministic review signals. A
signal selects review; it is not itself proof that the architecture is wrong.
Signals include:

- a code-bearing repository with one production module;
- one module spanning multiple independently discovered ecosystem projects,
  workspaces, or packages;
- one module containing multiple disconnected production graph components;
- a repository-wide catch-all path owning otherwise distinguishable source
  roots;
- a declared graph with no inter-module edges while the source graph contains
  material internal dependency structure; and
- a candidate that changes module names, paths, dependencies, test ownership,
  or the production graph topology covered by a prior review.

These signals use exact structural facts and no minimum module or file count.
A small one-module repository may therefore receive a short review and pass;
the tool does not force it to manufacture meaningless modules.

Add an `architecture-review` workflow modeled on the existing bounded behavior
and final-state review infrastructure:

1. `architecture-review prepare --base REF` writes a clean, bounded packet with
   the trusted base, exact candidate, declared module graph, production and test
   ownership, normalized source graph, cycle results, project and package
   roots, per-module summary, topology diff, and only mapped current design
   documents.
2. A clean-context AI reviewer receives only that packet. It evaluates concept
   ownership, boundary depth, graph direction, catch-all ownership,
   disconnected responsibilities, and forwarding-only moves or file splits.
3. The result is strict structured data. An acceptance cites concrete packet
   evidence and explains why the graph represents real ownership. Findings cite
   exact paths, edges, or configuration entries and propose a corrected graph.
4. `architecture-review finalize` validates the result and writes a receipt
   bound to the base, candidate graph, review signals, packet, and result.
5. A required review with findings, missing evidence, malformed output, stale
   inputs, or a mismatched candidate blocks the applicable checkpoint or merge
   gate.

An accepted one-module architecture is not a generated waiver and does not
suppress the review signal from reports. Its receipt remains reusable only
while the module contract, project and package roots, ownership map, and source-
graph topology retain the same identity.

Update version-matched agent guidance so an adopting or restructuring agent
first drafts the proposed module graph, prepares this review, and resolves its
findings before broad source moves or rewrites. Explicitly discourage a change
whose main effect is replacing one deep file with forwarding-only files. A
later candidate that diverges from the reviewed graph invalidates the receipt
and requires a new review.

The workflow does not embed an AI provider SDK or give Code Polishy network
credentials. The calling harness supplies the clean-context reviewer and saves
its result, as it does for other selected agent reviews. Local digests prove
candidate consistency, not reviewer identity; documentation must preserve that
trust limit.

## Explicit test ownership

Move test classification and ownership into one coherent `tests` contract:

```json
{
  "tests": {
    "paths": ["spec/**/*.py"],
    "ownership": [
      {
        "paths": ["internal/policy/**/*_test.go"],
        "module": "policy",
        "focusedSuite": "policy-unit"
      }
    ],
    "suites": []
  }
}
```

`tests.paths` adds unconventional test locations to built-in ecosystem test
classification. `tests.ownership` answers which production boundary each test
verifies and names its primary quick focused suite. Production `modules[].paths`
no longer assigns test ownership, although path containment remains useful as
non-authoritative diagnostic evidence.

Enforce all of the following:

- every governed executable test matches exactly one ownership entry;
- every ownership entry names one existing production module and one existing
  quick module-scoped suite for that same module;
- the named suite runs in focused, recommended, and full profiles and its
  execution paths include every test assigned to it;
- ownership patterns are contained, non-overlapping, and non-stale;
- an ownership pattern cannot convert production source into a test;
- repository-scoped suites may additionally execute owned tests but cannot own
  them; and
- test imports remain excluded from production module edges.

Remove `scope.tests` and the implicit module-path ownership behavior in the same
schema cutover. The new schema version rejects those old forms. Do not retain a
deprecated alias or silently synthesize `tests.ownership` entries.

For each unmapped test, diagnostics use bounded evidence in this order:

1. an exact paired production file under an existing module;
2. one existing module containing the test path;
3. resolved production imports that all converge on one module; and
4. otherwise, the bounded set of candidate modules with the evidence for each.

When one owner is provable, the finding names it as `expectedOwner` and includes
a ready-to-copy ownership object using the exact test path and a compatible
focused suite. When ownership is genuinely ambiguous, Code Polishy says so
instead of inventing certainty, lists the candidates, and includes one concrete
configuration alternative per candidate. The structured remediation always
states that an agent must choose based on the behavior the test verifies, not
merely its directory.

## Exact Python reachability

Preserve v0.22's Pydantic behavior as frozen regression coverage. Exact imports
and aliases of supported Pydantic bases and decorators, local subclasses and
re-exports, model fields, `Field` and `PrivateAttr` declarations,
`model_config`, validators, serializers, and computed fields remain inferred
without target configuration. Lookalikes, unresolved aliases, wildcard
imports, `ClassVar` members, and ordinary methods remain visible to dead-code
analysis.

Treat v0.23's `scope.pythonComputedImports` as a separate prerequisite. It owns
architecture evidence for one computed import callsite and must continue to
reject an undeclared, ambiguous, wildcard, escaping, or stale local import.
Dead-code reachability cannot satisfy that architecture check, and a computed-
import declaration alone cannot preserve a Vulture symbol.

### TypedDict literal-key reads

Extend the released Python AST facts rather than scanning strings or adding a
second parser. Recognize exact local TypedDict definitions created through
`typing.TypedDict` or `typing_extensions.TypedDict`, including statically
resolvable import aliases, re-exports, local inheritance, and the class and
functional declaration forms.

When a receiver's annotation, exact constructor, or supported local alias
resolves to one TypedDict, a subscript load with one plain string literal keeps
only that exact declared key reachable:

```python
payload: RequestPayload
name = payload["name"]
```

The same key spelling in another TypedDict, an unrelated string constant, a
dynamic key, an unresolved or union receiver, `Any`, a wildcard import, and a
mapping whose type cannot be proven provide no exemption. Duplicate keys,
ambiguous definitions, escaping re-exports, unsupported type expressions, or a
fact response that omits a selected file fail coverage rather than preserving
all fields.

Do not infer broader dictionary API semantics in this phase. Calls such as
`get`, `pop`, `setdefault`, iteration, unpacking, or serialization require their
own exact fact contract and fixtures before they can preserve a field. This
keeps a literal-key improvement from becoming name-based reachability.

### Exact external writes

Replace the flat `scope.pythonExternalAttributes` receiver fields with one
discriminated, exact receiver binding. Each declaration continues to name one
contained project, module, containing callable, written attribute, and external
consumer contract. It additionally binds the write line and column and one of
these receiver forms:

- `parameter`: one exactly annotated callable parameter at its signature
  location;
- `local`: one exactly annotated local binding at a stated line and column,
  with no ambiguous rebinding before the write; or
- `self`: the exact receiver of one resolved method whose enclosing local class
  has a statically proven external base, protocol, decorator, or registration
  callsite named by the declaration.

The parameter or local annotation must resolve through exact imports and
aliases to the declared external type. A `self` declaration must prove the
external consumer boundary; merely naming a local class or writing
`self.attribute` is insufficient. Each form resolves one AST assignment with
the expected receiver and attribute at the exact source location. Moving the
binding or write, changing its type or consumer, shadowing or rebinding the
receiver, changing the callable, or producing more than one match makes the
declaration stale or ambiguous.

Only the identified assignment is externally consumed. Adjacent writes, the
same attribute on another receiver, nested receiver chains, unannotated locals,
local-only types, and inferred duck typing remain visible. The new schema
rejects v0.22's parameter-only flat object; there is no compatibility decoder.

### Evidence-bound dynamic symbols

Replace free-standing `scope.pythonDynamicReferences`
`{project,module,symbol}` entries with two discriminated declaration forms that
identify an independently resolvable consumer. Support only evidence kinds
with strict syntax and ownership contracts:

- `target` binds one exact module and symbol to either a governed callsite or an
  external protocol, base, decorator, or entry-point contract. A callsite names
  its project, module, callable, line, column, recognized call shape, and target
  argument. An external contract names its admitted distribution, qualified
  type or decorator, member, and exact local implementation binding.
- `registry` binds one contained governed configuration or registry path, its
  structural selector, and the exact consuming callsite. Code Polishy derives
  the current bounded target set from that source; configuration does not list
  the targets again.

PEP 621 entry points, in-tree build hooks, Pydantic contracts, and other facts
Code Polishy can already infer remain inferred. A target must not repeat them
as configured reachability. A consumer and target must belong to the declared
project, resolve exactly once, and still be connected by the declared contract.
Missing, duplicate, stale, ambiguous, wildcard, unbounded, external-to-project,
or unsupported evidence is a non-suppressible `policy.pythonReachability`
failure.

The resulting registry fact set records every resolved target and participates
in dead-code evidence identity.

No setup, adoption, remediation, or autofix path may translate Vulture findings
into `pythonDynamicReferences`, `pythonExternalAttributes`, `scope.entryPoints`,
or another reachability list. Dead-code remediation recommends deletion unless
the engine has already found one concrete consumer boundary; in that case it
may show only the single evidence-bound declaration for that boundary.

Do not use an arbitrary small item limit as the semantic defense. A legitimate
registry can have many entries and an agent can work around a numeric ceiling.
Protocol-level count and byte ceilings still prevent resource exhaustion, while
the required one-to-one or one-to-bounded-registry consumer evidence prevents a
large ungrounded inventory from changing the result.

## Evaluation selection and analyzer context

Every policy invocation records three distinct concepts:

- `requestedSelection` is the exact governed file or module boundary selected
  by the caller or change-aware command;
- `analysisContext` is the additional governed inventory a sound analyzer had
  to read to interpret that selection, with the analyzer, project or package
  root, and expansion reason; and
- each finding's `selectionRelation` is `selected`, `related`, `context`, or
  `global`.

`selected` means the primary subject belongs to the requested selection.
`related` means an analyzer provides a concrete edge from a selected subject to
an affected subject outside it. `context` means the analyzer found a package-
or project-level problem outside the selection without proving that the
selected candidate introduced it. `global` is reserved for one repository- or
policy-wide occurrence. Code Polishy does not fabricate causality when no base
or analyzer evidence proves it.

Whole-package dead-code and type analysis may therefore remain strict and may
fail a focused invocation, but its output must say that it expanded analysis,
why it did so, and which findings are contextual. This classification is not a
baseline, exception, suppression, or weaker exit status. It lets an agent
distinguish its requested work from the broader clean-repository requirement
without treating either as green.

Make evaluation selectors usable without shell-generated path inventories:

- every policy or format command that exposes `--files PATH...` accepts
  contained regular files and contained directories;
- a directory expands recursively to its governed descendant files in
  normalized deterministic order without following symlinks or including
  excluded paths;
- repeated, overlapping, missing, escaping, special-file, and empty selections
  receive bounded specific diagnostics rather than an accidental clean pass;
- `--module NAME...` selects one or more declared modules for evaluation and is
  mutually exclusive with file, change-aware, staged, and all-repository
  selectors; and
- reports preserve the selector operands and expanded requested selection so a
  rerun never needs command substitution or one argument per repository file.

Evaluation selectors are separate from display filters. Use
`--filter-rule`, `--filter-module`, `--filter-path`, and `--filter-relation` for
rendering only. Do not overload `--module` as both an evaluation selector and a
display filter. Structured remediation prefers the shortest stable exact
selector, such as one module or contained directory, instead of emitting an
unbounded file argument list.

## Generated-source ownership and remediation

Add one exact producer contract for project-generated executable source while
retaining `scope.generated` as the engine's path classification:

```json
{
  "generation": {
    "producers": [
      {
        "name": "python-canvas-contract",
        "outputs": ["apps/python-canvas/src/backend/generatedContract.ts"],
        "inputs": ["apps/python-canvas/backend/internal/contracts/**"],
        "generate": {
          "argv": [
            "pnpm",
            "--filter",
            "@setta/python-canvas",
            "run",
            "contract:generate"
          ],
          "cwd": "."
        },
        "verify": {
          "argv": [
            "pnpm",
            "--filter",
            "@setta/python-canvas",
            "run",
            "contract:check"
          ],
          "cwd": "."
        }
      }
    ]
  }
}
```

Every project-declared generated executable output must resolve to exactly one
producer. Outputs and inputs are contained, bounded, non-overlapping, and
governed. The producer name is unique; its argument arrays, working directories,
environment names, and timeouts use the existing contained command contract.
An output outside `scope.generated`, a handwritten input classified as an
output, a missing or multiply owned current output, a producer cycle, a broad
escaping pattern, or a declaration with no possible output is a specific
non-suppressible configuration finding.

The ordinary `check`, `format`, and `doctor` commands never execute a declared
producer. The mapping supplies ownership and safe remediation; it does not turn
formatting into arbitrary repository-code execution. Any future command that
runs a producer requires its own explicit execution contract and is outside
this plan.

When generated executable source fails format validation or another repairable
source check, its finding identifies:

- the generator-owned output and exact failed rule;
- the producer and authoritative input locations;
- the required source-of-truth change;
- the exact declared generation and verification commands; and
- unavailable command or toolchain evidence when Code Polishy can establish it.

A generated format failure uses a distinct remediation from a handwritten
`quality.format` failure. It never says to run `code-polishy format`, because
that writer is forbidden from changing the output. If the producer cannot run,
the remediation reports the blocked prerequisite and explicitly forbids a
manual output edit or an unrelated formatter fallback.

`format` continues to preserve generated bytes. Its summary counts explicitly
selected generator-owned outputs as protected and untouched and names their
producer. It applies the same read-only format validation used by `check` to
those outputs: a nonconforming output returns the same generated-format finding
and failing status, while a conforming output reports that it was validated but
not rewritten. It never claims that an earlier generated-format finding was
repaired merely because the writer skipped the file. A selection containing
only protected outputs receives this specific validation result rather than
`PASS ... without findings`.

## Canonical finding contract

Replace message-shaped findings with a versioned Code-Polishy-owned finding
model used by direct commands, gates, managed reports, JSON, and SARIF.

Each finding contains:

- a stable rule ID such as `architecture.fileCycle` or
  `policy.testOwnership`;
- a stable instance fingerprint derived from the rule's canonical semantic
  identity;
- severity and evaluation status;
- scope (`repository`, `module`, `path`, dependency, or graph component);
- selection relation and any exact selected-to-related evidence;
- primary and related normalized locations;
- subject, owning module when known, and machine fields specific to the rule;
- generated producer identity when the subject is a declared generated output;
- one concise message rendered from those fields; and
- structured remediation with a summary, optional exact replacement or
  configuration example, and a next command represented as an argument array
  plus contained working directory.

Message wording, ordering, terminal width, and operating-system separators do
not participate in the fingerprint. A semantic change to the affected rule,
path, dependency, owner, or cyclic component does.

Producers must emit one semantic occurrence. The engine centrally coalesces
duplicate occurrences by canonical identity and retains their related
locations. Repository- or policy-global failures appear once rather than once
per file or downstream consumer. Coalescing must never merge distinct affected
paths, dependency versions, module edges, or graph components.

All checks define their remediation at the policy owner. CLI code renders that
data but does not infer fixes from English messages. A finding without a safe
automatic replacement still provides a precise explanation and the narrowest
valid rerun command.

## Human, JSON, and SARIF output

Every policy-producing command writes its complete versioned JSON report below
`.code-polishy-reports/<command>/<execution-id>/`, even when the terminal view
is filtered or truncated. Reports retain the existing gate identity and digest
properties and add the canonical finding fields, requested selection, expanded
selection, analyzer context, evaluation totals by selection relation, display
filters, and suppressed or reviewed outcomes.

Human output becomes summary-first:

1. print pass, fail, or review-required status and exact error, warning, and
   informational finding totals;
2. print bounded counts grouped by rule, module, and selection relation;
3. print at most 20 findings by default, with remediation and the next command;
4. state exactly how many findings are omitted; and
5. print the complete report path.

Allow a caller to lower the display limit or raise it to a fixed safe maximum.
No human-output option prints an unbounded finding list. Existing bounded gate
logs remain separate from policy findings.

Add a common output contract:

- `--format human`, `--format json`, or `--format sarif`;
- `--output PATH` for an explicit regular output file, with atomic replacement
  and no repository escape;
- repeatable `--filter-rule`, `--filter-module`, `--filter-path`, and
  `--filter-relation` display filters; and
- `--group-by rule`, `--group-by module`, `--group-by path`, or
  `--group-by relation` for human and JSON views.

Machine formats write one document to stdout when no output path is supplied;
progress and operational diagnostics use stderr. A filtered document records
both total and displayed counts. Exit status always reflects the complete
unfiltered evaluation.

JSON uses the canonical report directly. SARIF targets version 2.1.0 and maps
stable rule IDs, fingerprints, primary and related locations, severity,
remediation, and suppressed status without losing Code Polishy's complete JSON
report. Validate generated SARIF against the pinned official schema and golden
fixtures accepted by representative consumers.

Human success text must agree with the report. A command that emitted one
warning says `PASS with 1 warning`, not `PASS ... without findings`. A command
that selected only protected generated outputs says that no writable file was
selected and points to the producer contract. Every subcommand accepts
`--help`, and its output is semantically identical to `code-polishy help
COMMAND`. Usage text distinguishes files, directories, modules, evaluation
selectors, and display filters precisely; `PATH...` cannot imply directory
support that the command rejects.

Evaluate an exact release of `github.com/owenrumney/go-sarif/v2` behind one
narrow serializer adapter. Adopt it only if the v0.23 dependency-admission,
release-age, vulnerability, license, provenance, platform, and transitive-
inventory checks pass. If it does not pass, implement only Code Polishy's
required SARIF subset with `encoding/json` and the pinned official schema. The
evaluation and resulting single implementation must be complete before
v0.24.0; do not retain two serializers.

## Exact-version remediation

Exact-version findings must prefer the version already selected by the current
frozen lock rather than suggesting a newer release.

Resolve the recommendation in the declaring manifest's exact package-manager,
workspace, project, and importer scope. The structured finding records the
declared specifier, lock source, locked version, and a ready-to-apply exact
replacement when one unique current resolution exists. This applies to every
supported direct-dependency ecosystem for which Code Polishy has authoritative
lock facts, including the policy-owned Python inventory introduced for v0.23.

Never choose the highest transitive occurrence or a similarly named package.
If the lock is missing, stale, ambiguous, or contains multiple valid
resolutions for that declaration, do not guess. Report why no replacement is
safe, show the bounded candidate resolutions when useful, and direct the agent
through the repository's dependency-review workflow before installation.

The recommendation performs no registry request and says nothing about whether
the locked version is desirable. Vulnerability, release-age, license, and
dependency-review checks continue to decide whether that existing version may
be adopted as an exact declaration.

## Implementation boundaries

- Extend the released parser and language-pack adapters to emit normalized
  dependency facts; do not parse source again in the reporting or review layer.
- Keep graph traversal, canonicalization, and cycle policy in a small internal
  package using the Go standard library.
- Put finding identity, aggregation, remediation, and rendering behind one
  engine-owned reporting boundary. Gate reports adapt that same model rather
  than defining a lossy second finding type.
- Extend the existing repository selection boundary with normalized directory
  and module expansion. Analyzer adapters receive the requested selection and
  read-only context separately; they do not rediscover or redefine either.
- Use the v0.23 runtime JSON Schema authority for the new test and report
  contracts and for exact generated-source producer ownership.
- Extend the released batched Python AST facts and Vulture adapter for
  TypedDict, receiver-binding, and consumer evidence. Do not add another
  Python parser, dead-code analyzer, typechecker, or runtime dependency for
  these cases.
- Reuse the bounded clean-context review and receipt primitives where their
  trust and identity contracts match. Do not fork a general plugin or AI
  framework.
- Any new dependency or pinned standards data follows the full v0.23 supply-
  chain lanes before it enters the candidate and appears in the release SBOM
  and authenticated publication evidence.

## Implementation sequence

### Phase 0: Reconcile the released baseline

1. Land or select the released v0.23 tree and inventory its parser, finding,
   report, schema, review, dependency, and receipt boundaries.
2. Convert the v0.21.x and v0.22 adoption examples into exact regression
   fixtures, retaining the source release for each example.
3. Preserve from session `01a05f8e-20b9-7460-aa19-b160b63da465` the exact
   selected-versus-reported path sets, generated-format remediation, explicit
   generated-only format result, directory-selector failures, help behavior,
   warning summary, and v0.22 command exit statuses.
4. Mark each v0.21 concern as closed by v0.22, closed by v0.23, or still open.
5. Remove redundant work from this plan without weakening its outcomes.
6. Freeze current exit codes, gate identities, report custody, and supported-
   platform behavior that the cutover must deliberately preserve or replace.

### Phase 1: Establish one finding and report model

1. Define the canonical finding, semantic identity, remediation, occurrence,
   summary, and report schemas.
2. Define requested selection, expanded selection, analyzer context, and
   selection-relation facts once at the engine boundary.
3. Convert every built-in producer and gate adapter to that model.
4. Add semantic coalescing and global-finding ownership.
5. Add contained directory and module evaluation selectors with exact empty,
   invalid, overlapping, and special-file behavior.
6. Add managed reports for direct policy commands.
7. Implement bounded summary-first human rendering and display-only filters.
8. Make warning, protected-output, no-write, exit-status, and subcommand-help
   text derive from the same structured result.
9. Implement and schema-test JSON and the selected single SARIF serializer.

### Phase 2: Enforce source-level cycles

1. Define and bound `source-dependency-graph/v1`.
2. Adapt each supported language's existing resolved facts once.
3. Add deterministic strongly connected component analysis before module
   projection.
4. Emit production, generated, and test-only cycle findings with complete
   structured component evidence and canonical witnesses.
5. Bind graph identity into downstream reports, reviews, and reusable evidence.

### Phase 3: Cut over test ownership

1. Add `tests.paths`, `tests.ownership`, and `focusedSuite` to the runtime and
   shipped schema.
2. Add exact coverage, overlap, staleness, module, suite, and profile
   validation.
3. Add evidence-based expected-owner diagnostics and concrete configuration
   alternatives.
4. Remove `scope.tests` and implicit production-path ownership.
5. Convert Code Polishy's own tests and fixtures atomically.

### Phase 4: Add architecture review

1. Implement deterministic review signals from the normalized graph and
   discovered project structure.
2. Implement prepare, strict result validation, finalization, receipt identity,
   invalidation, and gate selection.
3. Add clean-context reviewer instructions for concept ownership, catch-all
   modules, disconnected responsibilities, and forwarding-only rewrites.
4. Update adoption and agent workflows to review the proposed graph before a
   broad rewrite and verify the same graph at delivery.
5. Complete one self-hosting architecture review for Code Polishy.

### Phase 5: Harden Python reachability

1. Freeze v0.22's Pydantic inference and v0.23's computed-import boundaries as
   regression contracts.
2. Add exact TypedDict definition, alias, re-export, inheritance, receiver, and
   literal-subscript facts to the existing Python adapter.
3. Replace external-attribute receiver strings with exact parameter, typed-
   local, and proven-`self` binding variants.
4. Replace free-standing Python dynamic symbols with the required callsite,
   governed-registry, or external-contract consumer evidence.
5. Remove every parameter-only decoder and unbound
   `{project,module,symbol}` configuration path.
6. Update adoption and remediation guidance so no agent or command turns dead-
   code findings into a generated reachability inventory.

### Phase 6: Complete remediation coverage

1. Add the exact generated-source producer schema and convert every
   project-generated executable output in Code Polishy itself.
2. Validate producer containment, ownership, staleness, command shape, and
   acyclic input/output relationships.
3. Give generated format and semantic findings source-of-truth remediation,
   exact generation and verification commands, and explicit blocked-toolchain
   behavior without executing the producer.
4. Give every remaining built-in finding an owned remediation and valid next
   command.
5. Resolve exact direct versions from each authoritative lock scope.
6. Add locked-version replacements and ambiguity evidence.
7. Verify that suggestions never perform network resolution or bypass
   dependency review.

### Phase 7: Release the atomic contract

1. Update permanent policy, adoption, agent, schema, CLI, and release docs.
2. Remove superseded types, renderers, schema fields, messages-as-identities,
   and compatibility fixtures.
3. Run supported-platform conformance and self-hosting gates.
4. Publish release notes that identify the breaking configuration and output
   contracts without offering a legacy mode.
5. Cut v0.24.0 only when every completion criterion is satisfied together.

## Verification

Graph and architecture fixtures must prove:

- a JavaScript or Python cycle inside one declared module fails;
- an allowed declared-module edge cannot hide a source-level cycle;
- generated-source and test-only cycles are classified and fail separately;
- Go package cycles retain real file witnesses without inventing file imports;
- self-loops, type-only edges, re-exports, and proven dynamic imports remain
  visible;
- unresolved or incomplete edges fail coverage before cycle analysis can claim
  a clean graph;
- each cyclic component has one stable finding, complete internal edges, and a
  canonical witness on Windows, macOS, and Linux; and
- large cyclic components remain bounded without attempting exponential cycle
  enumeration.

Architecture-review fixtures must prove:

- a one-module code repository selects review without an arbitrary size test;
- coherent small and genuinely monolithic designs can receive a cited AI
  acceptance;
- multiple project roots collapsed into a catch-all module and forwarding-only
  rewrites produce actionable findings;
- AI acceptance cannot suppress cycle, coverage, ownership, or dependency-
  direction failures;
- a changed graph, owner map, project root, packet, or result invalidates the
  receipt; and
- missing or malformed AI output fails only when review is selected and never
  masquerades as deterministic proof.

Test-ownership fixtures must prove:

- built-in and custom-path tests each require exactly one explicit owner and
  primary focused suite;
- production module paths no longer assign test ownership;
- overlapping, stale, missing, cross-module, repository-suite, and uncovered
  declarations fail specifically;
- unmapped tests with convergent evidence receive the correct expected owner
  and a valid configuration object;
- ambiguous tests show bounded alternatives without a fabricated owner; and
- test imports cannot authorize a production architecture edge.

Python-reachability fixtures must prove:

- every v0.22 Pydantic alias, re-export, local subclass, field,
  `model_config`, validator, serializer, and computed-field case remains live,
  while lookalikes and unrelated members remain dead;
- every v0.23 computed-import declaration remains exact, bounded, stale-
  checked, and separate from dead-code reachability;
- a literal subscript read keeps only the matching field of one exactly
  resolved local TypedDict;
- identical keys in another TypedDict, unrelated string literals, dynamic
  keys, `Any`, unions, unresolved aliases, and unsupported mapping operations
  preserve nothing;
- class and functional TypedDict declarations, exact aliases, local
  inheritance, and re-exports agree across supported platforms;
- parameter, typed-local, and `self` external writes each resolve one receiver
  binding, one write, and one independently proven external consumer;
- a moved binding or write, rebinding, shadowing, local consumer, adjacent
  attribute, nested receiver, or ambiguous AST match fails specifically;
- every configured dynamic symbol resolves through one current exact callsite,
  governed registry, or admitted external contract;
- one governed registry declaration derives its bounded current target set
  without copying those targets into configuration;
- a free-standing, duplicated, wildcard, stale, unsupported, or unbound symbol
  declaration fails and cannot be suppressed; and
- neither setup, remediation, nor agent guidance offers bulk generation from
  Vulture findings or broad entry-point inventories.

Reporting fixtures must prove:

- human output starts with complete totals, remains within its selected bound,
  states omitted counts, and points to a complete report;
- pass text reports warnings and protected no-write selections truthfully and
  never says `without findings` when the structured report contains one;
- JSON and SARIF validate against their pinned schemas and contain equivalent
  rule, location, fingerprint, severity, and remediation facts;
- fingerprints survive message-only changes, finding reordering, terminal
  width, and platform path spelling but change with semantic identity;
- a global failure is emitted once while distinct local occurrences remain
  distinct or appear as related locations;
- filters and grouping never change complete totals, report contents, or exit
  status; and
- every next command is a bounded argument array valid for the current command
  and repository scope.

Selection and command-surface fixtures must prove:

- selecting the same files individually or through a contained directory
  produces the same normalized requested selection on every supported platform;
- module evaluation selects exactly the declared module while package analyzers
  receive only the separately recorded context they require;
- missing, escaping, empty, overlapping, symlink, and special-file operands
  receive specific bounded outcomes and cannot produce an accidental pass;
- findings inside the selection, connected through exact analyzer evidence,
  merely discovered in package context, and global to policy receive the
  correct distinct relation;
- the v0.22 15-file adoption fixture identifies all 26 unselected findings as
  context rather than pretending their paths were selected;
- strict contextual findings still affect the unfiltered result while display
  filters alter neither evaluation nor exit status;
- remediation for a directory or module selection emits that bounded selector,
  never a shell command substitution or repository-sized argument list;
- every subcommand's `--help` and `help COMMAND` forms agree; and
- human, JSON, and SARIF status and warning totals agree with the process exit
  status.

Generated-source remediation fixtures must prove:

- every project-generated executable output has exactly one current producer
  with contained inputs and exact generation and verification commands;
- missing, stale, overlapping, escaping, multiply owned, and cyclic producer
  declarations fail specifically and cannot be suppressed;
- an unformatted generated TypeScript file remains a blocking format finding,
  but its remediation names the producer and source-of-truth change and never
  recommends `code-polishy format`;
- explicit formatting of that generated file preserves its bytes, reports it as
  protected and untouched, reruns read-only validation, and returns the same
  failure rather than claiming the earlier finding was repaired;
- ordinary checks, formatting, reporting, and doctor never execute the
  repository-controlled producer;
- an unavailable producer or toolchain yields blocked-prerequisite remediation
  that forbids a manual generated-output edit and an unrelated installed
  formatter fallback; and
- the exact v0.22 generated-contract adoption fixture no longer permits the
  misleading check-fail, format-pass sequence.

Dependency-remediation fixtures must prove:

- a broad direct declaration recommends its exact importer-scoped locked
  version;
- workspaces, aliases, optional and development groups, and Python project
  scopes cannot borrow another declaration's resolution;
- missing, stale, or ambiguous locks produce no guessed replacement;
- no suggestion requests a latest version or performs network access; and
- the suggested exact version still receives all ordinary vulnerability,
  release-age, license, and dependency-review checks.

The final candidate must also prove that old configuration fields, unbound
Python reachability declarations, parameter-only external-write objects, and
output types are absent, the Code Polishy repository passes under explicit test
ownership, and every new dependency or standards snapshot is present in the
complete release inventory and authenticated evidence.

## Completion criteria

- Every supported resolved repository dependency graph is represented once in
  `source-dependency-graph/v1` with bounded, fail-closed coverage.
- Every cyclic source component is reported before module projection, including
  cycles wholly inside one declared module.
- Suspicious topology selects a clean-context AI review without a minimum module
  or file count and without generating a waiver.
- Adopting and restructuring agents review a proposed module graph before broad
  source rewrites, and delivery proves the candidate retained that graph.
- Every governed test has one explicit production owner and primary quick
  focused suite independent of production module path matching.
- TypedDict literal-key access preserves only the exact field reached through a
  statically proven local type.
- Externally consumed Python writes support exact parameters, typed locals, and
  `self` receivers without preserving adjacent or unproven attributes.
- Every configured Python dynamic symbol is bound to one current independently
  resolvable consumer; a free-standing or bulk-generated reference inventory
  cannot change the dead-code result.
- Python computed-import architecture evidence remains exact and separate from
  dead-code reachability.
- File, contained-directory, and module evaluation selectors are bounded and
  deterministic, and every report distinguishes requested selection from
  analyzer context and finding relation.
- Human output is bounded and summary-first; complete JSON reports always
  remain available.
- JSON and SARIF expose stable rule IDs, semantic fingerprints, related
  locations, grouping fields, and structured remediation.
- Global findings appear once, and display filters cannot turn a failed full
  evaluation into a green result.
- Exact-version findings recommend the current unique locked version, never an
  unsolicited upgrade.
- Every project-generated executable output has one exact producer, generated
  bytes remain non-rewritable, and generated findings point only to the
  authoritative source and producer workflow.
- CLI help, warning totals, protected-output summaries, reports, and exit
  statuses cannot contradict one another.
- Every finding provides a precise remediation and the narrowest safe next
  command.
- No v0.21 or v0.22 compatibility path, old test-ownership inference, unbound
  Python reachability form, or dual report model remains.
- Every phase and completion criterion is delivered together in v0.24.0.

## Related plans

- Standards Parsing and Evidence Hardening, targeted for v0.23.0
- [Language-Pack Discovery and Universal Capabilities](universal-language-pack-capabilities.md)
- [Installable First-Party Language Packs](installable-first-party-language-packs.md)
