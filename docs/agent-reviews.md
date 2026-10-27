# Agent Reviews

Agent reviews report only major or severe, evidence-backed defects. Do not turn
nitpicks, preferences, optional refactors, minor prose issues, or speculative
concerns into blocking findings. Behavior/final-state review retains its existing
explicit-request and repository-configuration opt-ins. Architecture review
requires an explicit caller request. Code Polishy does not launch review agents.

Perform one review for the selected request or configured review event. Present
unresolved findings and their consequences to the caller; do not automatically
repeat review after fixes, choose a different reviewer to obtain acceptance, or
start a new review cycle. Any follow-up requires explicit caller authorization
and stays within the requested scope. A required behavior review remains failed
until resolved; stopping iteration never fabricates acceptance or waives checks.

A requested ordinary agent review binds to an explicit trusted base and exact
candidate. A dirty-worktree review covers committed, staged, unstaged, and
untracked changes so it cannot miss candidate state.

Keep structured evidence in the task's review record. Report actionable
findings or one concise no-findings outcome to the user; omit empty sections,
machine sentinels, and duplicates of automated checks. Distinguish repository
contract concerns from requested-outcome concerns and tie each finding to its
source instruction or objective and affected file or hunk. Agent review is
non-deterministic evidence and does not replace policy checks or human approval.

For selected behavior and final-state review, follow the
[review procedure](policies/behavior-review.md#complete-a-selected-review).
For an explicitly requested architecture review, follow the
[architecture procedure](policies/architecture.md#architecture-review-workflow).
