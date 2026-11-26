# Behavior Review Intent Custody

Intent is review evidence, not general task memory. Capturing every request when
behavior review is optional creates plaintext custody without improving an
ordinary gate. `design-context` therefore reports review selection for existing and explicitly
planned paths or modules without publishing intent. Configured merge or
checkpoint review and explicitly selected features require a clean initial
request boundary through `behavior-review capture-intent`; optional work
requires no journal. Explicit feature operands record only caller-selected
features, never an invented request for policy-selected features.

Direct capture validates bounded input, acquires the journal lock, validates
its current entries and request boundary, rechecks the candidate, and publishes
one append atomically. Initial capture needs a clean base. Later
artifact-affecting corrections may bind a dirty candidate. No separate
preparation or publication API is needed by context discovery.

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
