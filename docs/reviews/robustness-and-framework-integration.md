# Robustness and framework integration plan reviews

Date: 2026-09-07.

Reviewer: Claude Fable 5.1, requested as `claude-fable-5-1`, through Claude Code 2.1.251. The recorded assistant model identities and substantive model usage confirm the requested model.

## Review boundary

The first advisory review examined the proposed [robustness and framework integration plan](../plans/robustness-and-framework-integration.md), a complete tracked-source snapshot of `v0.24.10` at `754d8d1b87b4aad6147368230fc509f8c278f2d1`, the locked 0.24.2 agent workflow, historical issue reports, and selected earlier fixes from `95f0b11f3e85537861e7146a105dea7dc86a9f96`.

The first reviewed draft had SHA-256 `bcfe336acd7abfdc568c8aea54acabc317037cb35f53ddd2bdc0aa69ba0d4759`. Its resulting documentation was committed as `ab57506`. The caller then explicitly requested a fresh consultation with generalizability as the requirement. That follow-up inspected the same source baseline and the committed plan, SHA-256 `436ab4e97ad85ec1e98f7ebccfbf348d8bb92046a66f29402fed7333390c0877`.

Both consultations used file-read and search tools only. Neither executed tests or project reproductions. Source inspection does not establish that reported application failures have been freshly reproduced or repaired. These are advisory consultations, not mandatory architecture or behavior gate receipts. The current plan incorporates the primary author's assessment of the follow-up; that later revision has not received another AI review.

## First consultation: retained findings and superseded decision

The first review identified a real inconsistency: the draft required complete Astro analysis through optional tooling that the existing pack and native dispatch interfaces could not express. The primary author accepted its recommendation to put the required Astro tooling into the beta's sealed JS/TS bundle and defer generic provider work.

That packaging decision and blanket deferral are superseded. They did not satisfy the caller's requirement for general support outside the core. General provider ownership, policy inputs, facts, coverage, and exact tooling are necessary work for this requirement, not optional additions to an Astro-only milestone.

Other useful first-review findings remain in the active plan:

- CSS resolution and graph target handling must change together, with strict import-fact validation and a real prohibited cross-module import case.
- Runner-label declarations affect workflow checks, monitoring detection, and final-gate detection. Preserve the `workflow` module's dependency direction and cover all consumers.
- Electron runtime treatment needs actual source-role evidence. A package name or `main` path alone cannot exempt arbitrary shipped imports.
- Framework coverage needs explicit capability accounting. An extension classified as TypeScript does not establish successful linting, typing, comments, imports, or dead-code analysis.

## Follow-up recommendation

Fable recommended evolving the existing installable pack mechanism into a general provider contract, rather than introducing a second extension system. Its central recommendations were:

1. Resolve one owner per path/capability before native or pack dispatch; reject conflicts and missing ownership.
2. Require explicit coverage for selected inputs and preserve stable provider rule identities.
3. Feed provider source facts into the existing architecture checks.
4. Pass necessary project context and reuse an exactly governed host runtime for ecosystem tooling.
5. Demonstrate the mechanism with a provider outside the engine, while leaving complete built-in extraction deferred.

Fable agreed that the earlier blanket deferral was incompatible with the corrected objective. The primary author agrees with that conclusion and the choice to extend the existing pack mechanism. An installable pack can contain a broader ecosystem provider and its internal framework adapters; a standalone Astro pack is not required.

## Independent assessment and corrections

The primary author checked the recommendation against the actual source. The direction is useful, but several proposed details are insufficient or outside the authorized requirements.

| Fable proposal or claim                                                        | Assessment and active-plan disposition                                                                                                                                                                                                                                                                                       |
| ------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Baseline passing and comment facts can be optional later additions.            | Disagree. Effective baseline enforcement and comment/directive coverage are prerequisites whenever those policies apply. Otherwise the provider cannot satisfy the locked policy. A generic directive marker cannot establish an allowed directive.                                                                          |
| Keep v1 packs loadable in a degraded mode.                                     | Disagree. No compatibility requirement was requested. Plan a coherent public cutover with clear rejection of unsupported versions, without a second degraded behavior path.                                                                                                                                                  |
| A Node compatibility range and absolute runtime path suffice.                  | Tighten. Resolve and verify an exact runtime identity from the locked installation. A version range or path alone does not preserve execution identity. Measure the provider's remaining dependencies; sharing Node does not establish that package limits are sufficient.                                                   |
| No pack can reach any runtime under an empty `PATH`.                           | Too broad. The source demonstrates that ambient lookup is unavailable and a supported pinned host-runtime contract is missing. It does not establish that contained executables or absolute-interpreter mechanisms cannot execute. The plan adds an explicit verified contract instead of relying on those incidental paths. |
| A new analyzer string and JS import facts can enter the common graph directly. | Incomplete. The graph also restricts language/ecosystem identifiers and requires Python-specific fact projects and coverage. Generalize the whole validation boundary while preserving its evidence guarantees; generic consumers should not depend on JS wire types.                                                        |
| Removing claimed files from several JS input builders solves dispatch.         | Incomplete. Formatting, comments, graph analysis, tool prerequisites, coverage, execution profiles, and module accounting must consume consistent ownership. Project-wide type/reachability analysis also needs coherent project context.                                                                                    |
| All framework quality checks silently pass today.                              | Overstated. Some paths skip unsupported extensions, but dead-code inventory explicitly reports unsupported TypeScript-classified files when selected. Comments and architecture also have incomplete-coverage paths. Establish each capability's full/focused behavior before claiming a reproduction.                       |
| A made-up fixture language proves generality.                                  | Useful for protocol failure cases, insufficient for the whole outcome. Add one small real-language integration for a meaningful operation through a pinned analyzer; keep other missing capabilities visible. This does not schedule full Rust, Java, Zig, or C packs.                                                       |
| Missing command module bindings can prevent pack capability credit.            | A credible source-based inference. Reconcile module/profile accounting with actual owned paths and prove it in a focused integration test; do not give blanket module credit by merely filling an array.                                                                                                                     |

Relevant source evidence in the assessed 0.24.10 snapshot:

- `internal/quality/quality.go:24` dispatches native checks before configured pack commands; `:343` credits capabilities through language/module/provider declarations; `:819` requires module membership for configured providers.
- `internal/pack/runtime.go:69` appends pack declarations to policy configuration; `:75` compiles manifests into commands. These steps do not replace native dispatch.
- `internal/pack/protocol.go:37` defines unstructured evidence and capability-level findings; `:170` validates status without per-file semantic coverage; `:280` collapses check identity to `pack.<capability>`.
- `internal/repository/repository.go:569` gives built-in language recognition precedence over configured patterns.
- `internal/quality/javascript.go:151` filters native lint inputs; `:548` inventories dead-code inputs and reports unsupported extensions.
- `internal/architecture/sourcegraph/normalize.go:219` restricts ecosystems and languages. `sourcegraph/facts.go:38` validates Python-specific project identities; `:66` restricts analyzer protocols; `:96` binds fact coverage to Python nodes.
- `internal/quality/comments.go:127` identifies unsupported framework comment analysis. `docs/policy-engine-architecture.md:98` describes the current sealed fact boundary and policy-owned decisions.
- `internal/runner/runner.go:282` constructs the sealed environment. `internal/pack/install.go:17` sets pack size limits. Neither establishes OS sandboxing or proves analyzer correctness.

The active plan therefore schedules the smallest complete provider boundary and concrete acceptance cases. Registries, complete built-in extraction, and universal build-system work remain outside this delivery. Some general engine changes are necessary; framework semantics remain in providers.

The earlier summary incorrectly described observability as deferred work. Application dashboards, remote telemetry, aggregate analytics, and tracing are outside Code Polishy's product scope. The plan directs projects to established observability libraries and services and contains no future observability phase or integration-hook work. Local diagnostics and evidence needed to explain Code Polishy's own checks remain part of reliable analysis.

## Verification and evidence

The governing release is the exact locked Code Polishy 0.24.2 installation. This is ordinary Markdown work. The requested consultation creates no new mandatory AI gate. Delivery uses the required Markdown formatting check, preserves the unrelated issue reports, and runs no application or supplemental tests. Task-owned documents are committed separately from later source implementation, upstream integration, releases, or downstream upgrades.

The first review's original response and identities remain under `.code-polishy-reports/plan-review/robustness-2026-09-07/`. The separately authorized follow-up's request, reviewed candidate, source/input identities, original response, model metadata, and delivery records are under `.code-polishy-reports/plan-review/generalization-2026-09-07/`. These ignored local records preserve the raw recommendation; this document records the primary author's independent disposition.
