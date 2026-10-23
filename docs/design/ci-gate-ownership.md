# CI final-gate ownership

`verification.finalGateOwner: ci` requires a dedicated gate step in a checked-in
merge workflow. This is a declaration check with a bounded command format. It
does not interpret setup scripts or prove every possible shell execution path.

Run setup in earlier GitHub steps or earlier GitLab jobs. The gate step contains
one command, with no assignments, comments, extra commands, command chains,
redirections, or shell option changes:

```yaml
- name: Final gate
  shell: bash
  run: ./code-polishyw merge-gate --base=${{ github.event.pull_request.base.sha || github.event.before }}
```

## Supported command format

`EXECUTABLE merge-gate --base REF` and `EXECUTABLE merge-gate --base=REF`
are accepted, optionally followed by `--resume`. Spaces and tabs separate
arguments. The command occupies one line; surrounding blank space is ignored.

| Shell                | Executables                                                                      |
| -------------------- | -------------------------------------------------------------------------------- |
| `bash`, `sh`         | `code-polishy`, `./code-polishy`, `./code-polishyw`                              |
| `pwsh`, `powershell` | `code-polishy`, `code-polishy.exe`, `./code-polishyw.ps1`, `.\code-polishyw.ps1` |
| `cmd`                | `code-polishy`, `code-polishy.exe`                                               |

The effective shell must be one of these standard names. GitHub step declarations
override job defaults, which override workflow defaults and known runner defaults.
A custom runner with an unresolved default needs an explicit shell declaration.
Custom shell templates and other executable paths are outside the supported form.

A literal base starts with an ASCII letter or digit and contains only letters,
digits, dots, underscores, slashes, and hyphens. It may be double-quoted; shells
other than CMD also allow single quotes around the complete literal base.

GitHub commit-SHA expressions are allowed only as the complete base value:
`github.sha`, `github.event.before`, `github.event.after`, pull-request
`base.sha` or `head.sha`, and merge-group `base_sha` or `head_sha`. A `||` fallback
may combine those references. Their hexadecimal values cannot introduce shell
syntax; an absent value causes the gate's argument validation to fail.

Bash and sh also accept exactly `--base="$BASE"`, with any ordinary scalar
variable name. The fixed option prefix and double quotes keep its expansion
inside one argument, including a value such as `--help`. Separate expanded
arguments, concatenated values, arrays, substitutions, and other expressions
are outside the contract. PowerShell arrays and CMD quoting differences cannot
turn an accepted literal into additional arguments.

The validator matches this format directly. It does not parse shell syntax,
decode escapes, simulate builtins, track options, or maintain exceptions for
commands thought to be unreachable. A valid shell script can still be unsupported;
move its setup to another step and leave the gate invocation by itself.

## Workflow context

GitHub requires a push, pull-request, or merge-queue trigger and a job and step
that are statically active for that event. Optional failures and unknown conditions
do not establish ownership. Job IDs and dependency references use actionlint's
case-insensitive form. Completed dependency results are cached per event, so
shared prerequisites do not cause exponential traversal.

GitLab uses the existing sealed YAML boundary. A visible unconditional job must
have one script entry and no effective `before_script`. An explicit empty job
setup clears inherited setup. Local includes, defaults, variables, and matrix
overrides stay scoped to their root configuration; independent root files do not
merge. Unknown setup, matrix, workflow rules, external includes, and inherited or
conditional jobs cannot establish coverage. Workflow names and auto-cancel
metadata do not filter execution.

Declared startup hooks remain part of the configuration check. Effective
`BASH_ENV`, `SHELLOPTS`, and deferred `PS4` settings are resolved through the
existing environment facts. Unsupported values prevent that step or job from
establishing ownership; supported empty overrides retain their normal precedence.
The checker does not execute hook files or infer runner state from setup scripts.

CI enforcement still depends on trusted runners, launchers, earlier steps, and
required CI checks. This declaration check does not establish branch protection,
inspect unreported runtime environment changes, or prove that a remote run
completed. The default owner remains local, and selecting CI ownership never
selects an extra local merge gate.

If no supported declaration establishes ownership, findings identify the
workflow file, job, and GitHub step location where available. Rejection reasons
retain the failed contract: trigger or condition, optional failure, shell startup
settings, command format, or GitLab setup inheritance. GitLab facts retain the
job name and rejection reason through the sealed parser boundary instead of
discarding rejected scripts. An accepted gate suppresses other candidates'
ownership rejections; these diagnostics do not add execution requirements.
