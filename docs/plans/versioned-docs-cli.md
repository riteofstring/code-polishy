# Versioned Documentation CLI Plan

## Outcome

Add a small read-only Go CLI surface that lets coding agents discover and read
the documentation shipped with the exact Code Polishy release in use.

This command complements the Polishy skill. The skill explains when and why to
use Code Polishy. The CLI returns the full version-matched reference without
requiring the agent to guess file paths, browse the web, or copy large manuals
into `AGENTS.md`.

## Why this is worth doing

- Agents can retrieve the exact docs for the locked release, even offline.
- Root guidance can stay short without hiding important rules.
- Local development and CI refer to the same documentation.
- Documentation changes ship atomically with the code they describe.

This is a convenience and consistency feature. It does not make agents follow
instructions, replace the skill, provide semantic search, or authenticate user
intent.

## Public command contract

Add one top-level command with three actions:

```sh
code-polishy docs list
code-polishy docs find QUERY...
code-polishy docs read TOPIC
```

### `docs list`

- Prints one stable topic identifier and one short title per line.
- Sorts by topic identifier.
- Includes permanent user, agent, policy, architecture, and security docs.
- Excludes temporary plans, generated reports, changelog fragments, and files
  outside the installed release root.
- Supports the repository's normal machine-readable output convention if one
  exists at implementation time.

### `docs find`

- Requires at least one non-empty query term.
- Performs deterministic case-insensitive text matching across topic, title,
  summary, headings, and body.
- Ranks exact topic and title matches first, then heading matches, then body
  matches. Ties use the stable topic order.
- Treats all query terms as required and returns a bounded result list.
- Prints topic, title, and a short matching excerpt. It never emits an entire
  document as a search result.
- Returns success with an explicit no-results message when nothing matches.

The first release deliberately avoids embeddings, network search, fuzzy model
ranking, an index daemon, and provider-specific integration.

### `docs read`

- Accepts one exact topic identifier or one unambiguous documented alias.
- Prints the complete Markdown document to standard output.
- Rejects path separators, traversal, absolute paths, ambiguous aliases, and
  unknown topics.
- Suggests close valid topic identifiers when the correction is deterministic.
- Preserves the checked-in Markdown bytes except for the CLI's normal final
  newline behavior.

### Help and exit behavior

- `code-polishy docs --help`, every action's `--help`, and
  `code-polishy help docs` work without opening a repository.
- Invalid syntax exits with the CLI usage status and one relevant usage line.
- Missing or damaged installed documentation is an operational failure with the
  release root named concisely.
- Reading docs never mutates the repository, report directory, policy cache, or
  network state.

## One authoritative documentation source

Checked-in Markdown under `docs/` remains authoritative. Do not copy document
bodies into Go source, the skill, or generated agent guidance.

Add a small checked-in catalog that contains only routing metadata:

- stable topic identifier;
- source path below `docs/`;
- display title;
- one-sentence summary;
- optional exact aliases;
- whether the document is public in `docs list`.

The catalog is the owner of topic identifiers and aliases. A validation test
must prove that every catalog path is contained, regular, unique, and present;
every public permanent document is either cataloged or explicitly excluded;
and aliases are unambiguous.

The release manifest continues to own file integrity. The docs CLI resolves the
installed release root through the same trusted runtime path used by other
commands and reads only cataloged files from that root.

## Architecture

### Domain package

Add one shallow read-only package responsible for:

- typed catalog loading and validation;
- topic and alias resolution;
- contained document reads with bounded size and UTF-8 validation;
- deterministic search and excerpts;
- typed `unknown`, `ambiguous`, `invalid-catalog`, and `unavailable` failures.

The package depends on standard library path and text primitives. It does not
depend on CLI rendering, Git, repository configuration, policy evaluation,
network providers, or process execution.

### Runtime boundary

Reuse the existing installed/source release-root resolver. Resolve it once at
the CLI boundary and pass an exact root into the docs package. Do not infer the
root from the caller's working directory.

Source checkouts and installed releases must behave the same. A source-only
fallback that silently reads different docs is out of scope.

### CLI rendering

- Route help before repository initialization.
- Keep human output plain, stable, and pipe-friendly.
- Generate any JSON or structured output from typed results rather than parsing
  rendered text.
- Preserve standard broken-pipe handling so `docs read TOPIC | less` and
  `docs find QUERY | head` behave normally.

## Skill and `AGENTS.md` cutover

The Polishy skill remains the compact workflow router. Update it to use the CLI
for detailed versioned references, with examples such as:

```sh
code-polishy docs find behavior review
code-polishy docs read agent-workflows
```

Generate a short canonical `AGENTS.md` that states the durable operating rules
and points agents to the installed skill and `code-polishy docs`. Do not remove
a rule from root guidance until the matching skill and exact versioned document
are installed and discoverable in the same release.

The atomic cutover includes:

- root and template `AGENTS.md`;
- `skills/polishy/SKILL.md`;
- the docs catalog and bundled Markdown;
- CLI help and examples;
- agent-guidance generation and drift checks;
- installation and adoption docs;
- release notes, version, and repository lock.

The old expanded guidance remains canonical until this complete surface passes
the installed-release test.

## Verification

### Package tests

- valid catalog loading and stable ordering;
- duplicate topics, aliases, and paths;
- missing, non-regular, non-UTF-8, oversized, absolute, escaping, and symlinked
  documents;
- exact topic and alias resolution;
- ambiguous and unknown topics with deterministic suggestions;
- case-insensitive all-term search, ranking, excerpt bounds, and no results;
- source and installed root equivalence.

### CLI tests

- every help form before repository initialization;
- concise list, find, read, no-result, ambiguity, and failure output;
- strict arguments and exit statuses;
- output piping and broken-pipe behavior;
- no repository or report mutations;
- execution from outside a Code Polishy-managed repository.

### Release tests

- Install the candidate into an empty prefix and run all three commands through
  only the stable installed launcher.
- Confirm returned content matches the release manifest and locked source docs.
- Run the same smoke contract on Windows using native paths and PowerShell.
- Run `code-polishy agents sync` in a fixture and prove the generated guidance
  references only docs available in that installed candidate.
- Run `code-polishy doctor --strict`, changed tests, the final merge gate, and
  full hosted CI on the exact release candidate.

## Explicit non-goals

- downloading current web documentation;
- editing documentation through the CLI;
- semantic or AI-powered search;
- indexing arbitrary repository files;
- replacing the Polishy skill or repository-owned commands;
- provider-specific chat history or prompt capture;
- a background server, plugin protocol, or new network service.

## Delivery sequence

1. Create the CLI branch from the verified behavior-review commit.
2. Commit this public contract and the catalog schema.
3. Commit the domain package and exact tests.
4. Commit CLI routing, help, rendering, and contract tests.
5. Commit installed-release and Windows coverage.
6. Commit the skill, shortened generated guidance, docs, templates, changelog,
   version, lock, and README cutover together.
7. Run the installed candidate, changed tests, final merge gate, and hosted CI.
8. Remove this temporary plan only after permanent docs own every public
   contract and the worktree is clean.
