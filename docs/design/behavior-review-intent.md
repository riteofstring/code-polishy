# Behavior Review Intent Custody

Intent is review evidence, not general task memory. Capturing every request when
behavior review is optional creates plaintext custody without improving an
ordinary gate. `task-start` therefore projects the selected files or module
through current behavior-review policy before publishing intent. Configured
merge or checkpoint review and explicitly selected features require capture;
an optional selection does not. The task packet states whether intent was
captured and whether the selected review will consume it.

Standard input and regular files are equivalent transports into the same
bounded UTF-8 validation and journal append. Standard input lets an agent
harness preserve an exact request without leaving a second plaintext file.
File input remains useful when an existing durable request artifact is the
authority. Neither transport changes feature selection or infers requirements
from prose.

Only messages that change the requested artifacts, observable behavior, or
acceptance criteria belong in the journal as corrections. Status questions,
approvals, authentication and publication coordination, and other operational
instructions are conversation state rather than final-state review evidence.
This distinction remains a harness responsibility because semantic intent
cannot be classified safely from keywords.

Review evidence must remain complete from capture through any selected gate,
so Code Polishy does not age or partially prune it automatically. The explicit
cleanup command removes the complete behavior-review artifact directory after
the caller no longer needs review, proof, or receipt reuse. Cleanup is bounded
to that managed directory and is idempotent; deleting it before a selected gate
deliberately discards the evidence needed by that gate.
