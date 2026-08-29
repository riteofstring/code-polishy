# Isolated Task Sessions

`code-polishy task-session` executes one command in a disposable Git worktree
and can promote a clean committed result to the source branch.

## Boundary

The caller supplies at least one module from the trusted starting commit.
Optional `--allow-path` and `--allow-new-path` values grant individual
non-control artifacts. Each value names one exact path.

Before work begins, the session freezes the trusted base, locked Code Polishy
binary, governed command environment, exact command arguments, and caller-owned
scope. It validates committed, staged, unstaged, deleted, renamed, and untracked
candidate paths against that frozen scope. Policy configuration, lock files,
workflow files, code-owner files, and Git controls remain protected.

Scope validation inspects every commit between the trusted base and the
candidate. A worker therefore cannot commit a forbidden path temporarily and
restore it before promotion to hide the out-of-scope mutation.

## Lifecycle

The task reuses one worktree for setup, implementation, native-subagent work,
and focused tests. `CODE_POLISHY_TASK_SESSION=1` prevents nested sessions.

The session writes its scope, command, environment, state, bounded logs, and any
rejected patch to an external artifact directory. Successful tasks and ordinary
command failures remove the disposable worktree. Interruptions and
workspace-identity failures retain it for manual inspection.

With `--promote`, the source branch advances through a fast-forward after all of
these conditions pass:

- The worker exits successfully.
- The candidate is clean and committed.
- Every changed path is inside the frozen task boundary.
- The source repository remains unchanged on its original branch.
- The worktree identity still matches the registered external worktree.

The task session validates scope and repository state. The worker owns tests,
review, and implementation quality.

## Native process containment

The supervisor uses process groups on Unix and a kill-on-close Job Object on
Windows. Cancellation and normal completion wait for the worker's complete
process tree. Native delegates must remain in that process tree.
