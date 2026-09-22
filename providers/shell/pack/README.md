# First-party shell pack

This pack owns Bash and POSIX shell discovery, syntax validation, ShellCheck,
source-comment facts, and portability literal facts for Code Polishy 0.27.10.
It intentionally declares formatting unsupported.

The pack contains one analyzer binary for every declared platform. The exact
policy-owned Node runtime selects that binary, and the analyzer invokes only the
exact policy-owned ShellCheck 0.11.0 executable supplied by Code Polishy.
