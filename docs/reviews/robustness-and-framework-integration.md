# Robustness and framework integration plan review

Date: 2026-09-07.

Reviewer: Claude Fable 5.1, requested as `claude-fable-5-1`, through Claude Code 2.1.251. The response's initialization, assistant messages, and substantive model usage identify that model.

## Review boundary

One independent advisory review examined the proposed [robustness and framework integration plan](../plans/robustness-and-framework-integration.md) and a complete tracked-source snapshot of Code Polishy `v0.24.10`, commit `754d8d1b87b4aad6147368230fc509f8c278f2d1`. It also received the locked 0.24.2 agent workflow, the three historical issue reports, and relevant earlier fixes from commit `95f0b11f3e85537861e7146a105dea7dc86a9f96`.

The reviewed draft's SHA-256 was `bcfe336acd7abfdc568c8aea54acabc317037cb35f53ddd2bdc0aa69ba0d4759`. The current plan includes the corrections described below and is a later candidate. It has not received a second AI review.

The reviewer had only file-read and search tools. It performed source inspection without executing tests or project reproductions. Its source-based conclusions do not establish that application failures have been freshly reproduced or repaired. The review is advisory and does not replace deterministic policy checks, required verification, or human approval.

## Overall assessment

The reviewer judged the draft mostly accurate, aligned with the policy-owned analyzer model, and focused on deterministic checks. It found one major requested-outcome problem: Astro acceptance depended on optional-tooling participation that the current architecture does not provide. It reported no other major or severe defects.

The reviewer recommended separate milestones for CSS imports, runner labels, Electron ownership, and a concrete Astro packaging/integration choice. General provider work and workflow conveniences should remain deferred until demonstrated needs justify them.

## Major finding and disposition

### Astro packaging and acceptance were inconsistent

The draft deferred packaging while requiring complete Astro analysis through an unspecified optional provider. In the reviewed source:

- `tools/javascript/runner.mjs:12` imports bundled tools directly.
- `tools/javascript/protocol.mjs:69` verifies the complete declared tool inventory; missing tools are not an optional operating mode.
- `tools/javascript/runner.mjs:522` rejects target compiler-plugin loading.
- `internal/pack/protocol.go:37` defines findings/evidence responses without the native import-fact channel.
- `internal/quality/javascript.go:151` groups native lint inputs by supported JS/TS extensions, while `internal/repository/repository.go:569` gives built-in language recognition precedence.

Consequently, an optional-tooling route would require new loading, ownership, and evidence infrastructure. Treating it as an existing small adapter seam concealed a platform expansion.

Disposition: the plan now recommends exact compiler and source-mapping dependencies in the beta's existing sealed JS/TS bundle. It states that these tools are carried even when an installation analyzes a non-Astro project. Project facts determine applicable checks; the complete bundle retains its existing integrity rules. Stable packaging and independently removable tooling are future decisions. If that beta packaging tradeoff is rejected, Astro remains explicitly incomplete and the integration milestone is deferred. No optional loader or public pack-protocol redesign is scheduled.

This addresses the ambiguity in the plan; it is not a claim that the proposed integration has been implemented or independently accepted.

## Additional corrections incorporated

| Review observation                                                                                                                                                      | Correction to the plan                                                                                                                                                                                                 |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Resolving a CSS filename alone still leaves its target outside the executable graph.                                                                                    | Repair resolution, graph target handling, and any changed import-fact validation together. Add a cross-module CSS case that produces `architecture.moduleDependency`, while missing assets remain incomplete coverage. |
| Runner-label configuration affects workflow checking, security-monitoring detection, and final-gate detection. The `workflow` module has no dependency on `repository`. | Cover all consumers with caller-supplied contained inputs or a bounded reader callback. Include generated-workflow validation, ambiguous YAML/YML declarations, exact labels, and rejection of suppression settings.   |
| An Electron exception must not become a generic package allowlist.                                                                                                      | Require evidence of the actual runtime/source role. Package names and a `main` path alone are insufficient; ambiguous roles and invalid renderer imports retain findings.                                              |
| Native lint/complexity grouping silently skips `.astro`, even though other analysis paths report unsupported coverage.                                                  | Record the omission explicitly and require either real analysis or a coverage failure. Include comment/directive behavior in the capability inventory.                                                                 |
| Generic provider work could expand the Astro milestone.                                                                                                                 | Stage 4 now documents deferred contract concerns. The beta uses the existing JS/TS fact boundary and does not claim complete arbitrary-language provider support.                                                      |

The primary author checked the relevant source before incorporating these corrections. The Electron implementation still requires a real reproduction and bounded design; the review did not establish a sufficient runtime-detection algorithm. A compiler transform likewise does not establish complete Astro formatting, typing, template, comment, or complexity coverage by itself.

## Delivery verification

The governing release is Code Polishy 0.24.2 with the exact repository lock digest. Its workflow identifies this as ordinary Markdown work. Behavior and final-state review status is optional with no selected features; the requested advisory review does not create a new gate.

The planning delivery uses the required Markdown formatting check and preserves the unrelated issue reports. It runs no application or supplemental tests. Completed task-owned documents are committed separately from any later source implementation, upstream merge, release, or downstream upgrade.

The original model response, reviewed draft, source/input identities, model metadata, and formatting records are retained locally under `.code-polishy-reports/plan-review/robustness-2026-09-07/`. This document records the substantive result independently of those ignored local artifacts.
