# Architecture Review

## Scope

- Concept and authoritative owner:
- Current callers and consumers:
- Superseded owners or duplicate representations:

## Error model

- Invalid states to make unrepresentable:
- Invalid external inputs to reject:
- Partial-write or concurrency failures to prevent:
- External/provider failures to translate:

## Boundary

- Public interface:
- Complexity hidden by the interface:
- Ingress and validation boundary:
- Side-effect and transaction boundary:
- Generated consumers:

## Dependencies

- In-process:
- Local-substitutable:
- Remote but owned (port and adapters):
- Truly external (mock at adapter):

## Enforcement

- `.code-polishy.json` module owner and dependency edges:
- Target-native architecture provider, when applicable:
- Compiler/type invariants:
- Observable boundary tests:
- Deterministic workflow tests:

## Cutover

- Callers to migrate:
- Superseded code, tests, scripts, aliases, and docs to delete:
- Rollback or release constraint, if any:
