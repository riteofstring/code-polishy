# Repository Operational Handoffs

Declare repository procedures in `.code-polishy.json` so normal context
discovery can surface them when they apply. Managed `AGENTS.md` remains the
locked release's canonical file. It contains no editable local sections,
interpolated document links, or repository policy overrides.

## Declaration

Each handoff has a unique name, a concise description, one repository-owned
Markdown document, and at least one exact trigger:

```json
{
  "documentation": {
    "handoffs": [
      {
        "name": "authentication",
        "description": "Set up the repository's required authentication.",
        "path": "docs/operations/authentication.md",
        "situations": ["authentication"]
      },
      {
        "name": "release",
        "description": "Build, verify, and tag a release candidate.",
        "path": "docs/operations/release.md",
        "situations": ["release"],
        "modules": ["release"]
      },
      {
        "name": "deployment",
        "description": "Deploy and verify the production service.",
        "path": "docs/operations/deployment.md",
        "situations": ["deployment", "deploy-production"],
        "sourcePaths": ["scripts/deploy.sh"]
      }
    ]
  }
}
```

Use only modules declared by the repository and current, contained, governed
source paths. Source triggers are exact files. Use a module trigger for its
complete source scope. Situations are exact identifiers such as
`authentication`, `release`, and `deployment`; repository-specific identifiers
such as `deploy-production` use the same syntax. Prose keywords never select a
situation. A situation, source, or module match selects the handoff; overlapping
matches select it once and retain each reason.

Names and situation identifiers use lowercase letters, digits, periods, and
hyphens, starting with a letter. Names and document paths are unique. Put
multiple triggers on one declaration instead of mapping the same document
through several names.

## Discovery

```sh
code-polishy design-context --situation authentication
code-polishy design-context --module release --situation release
code-polishy design-context --files scripts/deploy.sh --format json
```

Repeat `--situation NAME` for multiple exact situations. When situations are
the only selector, the command selects no source files. An explicit file or
module selector adds its normal current design context. Without a situation or
file/module selector, the normal changed-file selection applies. The actual
`design-context` command is also an exact workflow situation, so repositories
may declare a handoff for every invocation of that workflow.

Human output shows selected handoff names, descriptions, document paths,
SHA-256 identities, and selection reasons. JSON stores exact selected document
contents in the versioned `repositoryContext` category of the complete managed
report. Current design documents and operational handoffs remain distinct
categories. A document edit changes its identity. The hash binds the bytes
that were read; it does not authenticate the document's author or grant an
approval.

Only selected handoff documents are opened for context. A missing unrelated
handoff does not prevent a different procedure's discovery. A selected invalid
handoff prevents publication of usable partial context. `doctor` validates the
entire declared handoff inventory, including procedures the current task did
not select. Configuration shape and declared module references are always
validated when configuration is loaded.

## Bounds and validation

A repository may declare at most 64 handoffs. Each name and situation or module
identifier is at most 128 characters, each description at most 512 UTF-8 bytes,
and each document or source path at most 4,096 characters. Descriptions are
trimmed and contain no control characters. Each situation or module list has
at most 32 entries; each source list has at most 128 exact paths. Empty or
duplicate trigger entries are invalid.

Documents must be existing, readable, nonempty UTF-8 Markdown files. A document
or source reference cannot escape the repository, traverse a symbolic link,
name a directory or special file, or use a stale or ambiguous source owner.
Each selected document is at most 1 MiB. The composed context has at most 128
documents and 8 MiB of total document content. Exceeding a bound fails context
composition; the tool never presents truncated text as complete evidence.

Reference findings identify the exact handoff, configuration, and affected
path. Repair the repository-owned input or its mapping, then rerun `doctor` or
the applicable context command. Handoff failures cannot be waived with policy
exceptions.

## Procedure authority

Handoff discovery reads procedures. It does not run their commands, retrieve
credentials, contact services, or authorize deployment, publication, or any
other action. Follow the caller's authorization and the locked release's
workflow requirements when carrying out a discovered procedure. A handoff
cannot weaken the baseline, waive checks, or replace required approval.
