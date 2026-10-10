# Bounded JavaScript lint results

The native lint request carries the core's resolved complexity and comment
activation alongside its framework activation. Generated source disables
complexity, depth, parameter, and prose-comment collection before those facts
can consume the transport budget. Authored source retains its applicable rules.
Syntax and semantic lint still analyze every selected generated file, and
producer verification remains independent. Parser comment locations are still
collected to distinguish inline-configuration warnings from incomplete analysis.

Lint responses contain at most 5,000 records and 4 MiB of serialized records,
leaving room for the response envelope below the existing 8 MiB transport limit.
Findings, comments, and unsupported-file records all participate in those bounds.
A response explicitly returns either the next file/result cursor or completion.
The core follows advancing cursors under one overall lint deadline and combines
every page. A missing completion marker, invalid cursor, or failed later page
fails the operation instead of accepting a partial analysis.

Each source file is parsed as a complete unit. A continuation inside one file
re-evaluates that file and resumes its deterministic sequence of findings,
comments, and unsupported records. Completed files are not re-evaluated.
This avoids persistent analyzer state and source splitting while retaining
complete findings when one file exceeds a page. Repositories must remain stable
during analysis, as they must for the other file-based checks; cursors are local
to one operation and are never reusable evidence. Existing input-size, path,
process, and deadline limits remain in force.

The engine and sealed runner are shipped together with the same strict request
and response shapes. The optional provider shares semantic rule selection but
continues obtaining function measurements through its separate metrics pass.
