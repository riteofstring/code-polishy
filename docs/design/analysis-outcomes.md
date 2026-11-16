# Analysis Outcomes and Runtime Ownership

Source ownership, runtime dependencies, and diagnostic certainty are different
facts. Neither a generated path nor an analyzer's lack of usage evidence proves
a product defect. The policy keeps uncertain candidates visible without
requiring consumers to delete live interfaces or renew exceptions for supported
architecture. Coverage failures remain failures, not successful empty analyses.

Vulture continues collecting every candidate at its existing 60% floor. Its
scores describe analyzer categories: even a 100% unused argument can be required
by a callback signature. Unused definitions, imports, attributes, and arguments
are therefore warnings. Only 100%-confidence unreachable control flow blocks.
Existing framework and explicit runtime contracts reduce noise and retain stale
declaration checks; new framework inference is not needed to turn uncertain
definitions into passing results. Deletion expansion remains intact because
removing a caller can make an unchanged definition unused. Focused reports say
when complete-project analysis was deferred.

JavaScript semantic lint still parses the whole generated program. Slicing
minified vendor regions would change its meaning, and a producer declaration
does not establish the origin of each expression. The core reports the shared
semantic rules as advisory for generated JavaScript in both native and pack
execution. Authored semantic lint, whole-output syntax, incomplete analysis,
type checking, dependency security and producer verification retain their
existing obligations. Producer details remain attached, but a candidate's
remedy asks for review before changing authoritative inputs. No source-map
engine, permanent exemption, or extra automatic verification run is introduced.

`scope.pythonTypecheckEnvironments` maps exact source paths to a contained
`pyproject.toml` and records the runtime reason. The named project must own a
governed `uv.lock` and a valid `.venv`; its normal supply-chain checks remain
independent. Duplicate paths, stale source, malformed projects, escaping
environments, and missing environments fail rather than choosing a fallback.
There is one complete dependency environment per path, without overlays.

The Python project inventory continues assigning source to its original package.
Only ty invocations are partitioned by the explicit environment, retaining the
original working directory and import roots. Structured diagnostics must name
files in that invocation's partition. A completed response records the runtime
manifest and analyzed files even when it contains type errors. This evidence
does not certify installation integrity or replace frozen-lock verification.
Ruff, Vulture, architecture contracts and module ownership are unchanged.

Runtime contracts express supported behavior and do not expire by date. They
are validated against current source and projects. Temporary risk exceptions
retain their exact, owned, expiring policy.
