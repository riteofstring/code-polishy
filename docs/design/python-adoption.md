# Python Runtime and Release Evidence

[Published Python dependency contracts](python-library-dependencies.md) define
the separate version-specifier operation and exact development-pin pairing.

PyPI release spelling is not package version identity. Locked wheel versions
and registry release keys are compared with the carried `packaging` PEP 440
implementation, including trailing release zeros, epochs, prereleases, and local
versions. The result retains the original lock spelling for findings and
assessments. Missing artifacts or timestamps remain missing evidence; multiple
dated registry releases for the requested normalized identity remain ambiguous.
An empty historical alias contributes no date and cannot invalidate a dated
release. Unrelated
invalid historical release keys do not manufacture a match. The original
earliest artifact upload time remains the release date. Normalization batches
stay within the existing Python facts request limit.

The project's minimum supported Python version selects a Ruff target, independently
of the interpreter running Code Polishy's source-facts adapter. Pinned Ruff
supports Python 3.7 through 3.15. The carried CPython 3.12 parser remains the
source-syntax boundary for source facts and dead-code analysis. A newer runtime
minimum alone does not imply newer syntax. Unsupported syntax fails with an
explicit analyzer-upgrade diagnostic; accepting its runtime minimum never
turns a failed parse into successful analysis.

A project-local virtualenv is an installed tool environment, not a checked-in
script. On Unix its Python launcher normally links to the environment manager's
external interpreter. The runner and doctor share executable resolution. They
allow that link only for a contained `.venv/bin/python`, `python3`, or
`python3.MINOR` whose contained `pyvenv.cfg` binds the interpreter home and
exact version to the adjacent project `.python-version`. Ordinary escaping
scripts and escaping environment directories remain rejected. This is local
environment consistency, not authentication of a Python distribution; dependency
and tool acquisition retain their separate policy boundaries. The interpreter
entry in the declared home must resolve to the virtualenv's interpreter target.
The home can contain symlinks into a framework installation, as Homebrew does;
its directory need not be the final executable's parent. Invocation keeps
the virtualenv path because resolving the final link before execution loses
Python's virtualenv identity. Regular contained executables retain their normal
behavior without a new pin requirement.

Unix child lifetime is established by the inherited witness pipe, not merely
the leader's exit or its original process group. A daemon may call `setsid()`
and finish bounded shared-memory cleanup after its parent exits. The existing
five-second cleanup budget now covers group termination, witness closure, and
output drainage together. A daemon that closes every inherited descriptor
within that budget permits quiescence; persistent holders still fail. Output
readers also close at the deadline so an escaped stdout/stderr holder cannot
block failure reporting indefinitely. TERM receives the first half of the
budget before escalation to KILL, reserving the remaining half for process exit,
witness closure, and output drainage. Waiting until the final deadline to kill
a process can misclassify its pending pipe EOF as an escaped descendant.
Permission errors from probes, TERM, or KILL remain retryable until the full
cleanup deadline while the witness stays open. A descendant running under
another UID may close its descriptors during that interval even when the runner
cannot signal it. A still-open witness at the deadline retains the failure.
Diagnostics retain the launched command,
leader PID and process group, and elapsed cleanup time. The witness establishes
that a descendant retains a descriptor but does not identify that descendant's
PID; the diagnostic does not invent one.
