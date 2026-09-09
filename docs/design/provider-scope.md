# Analysis ownership and operation scope

Analyzer routing uses exact source, capability, and execution-profile claims.
An unavailable pack blocks authenticated retained claims without suppressing
unclaimed native languages or capabilities. A wholly missing pack still fails the
repository's pack check; absence of its manifest supplies no additional claims.

Generated JavaScript keeps its physical path for reads, findings, and write
protection. Its declared source package supplies module ownership, package
metadata, compiler configuration, and effective lint activation. Native lint and
provider requests share the same resolved activation, including React overrides
and generated-source exemptions.

Local style operations retain the selected files. Type checking can report
unchanged members of the selected compilation program. Unused-code analysis
retains package-wide reachability across actual package trees, including when
only package metadata or entry policy changed. These diagnostic scopes never
expand format-write authority.

Provider architecture starts with selected project units and follows resolved
imports to connected units. Core validates the reported closure, then adds
native targets to native graph discovery. Unrelated provider projects cannot
contribute failures to focused language selections. Graph fact records partition
sources by containing package root; inherited sources outside their effective
package retain a containing physical root while request evidence binds their
effective source package.

The [provider protocol](analysis-providers.md) carries these decisions explicitly.
The [JavaScript provider](javascript-provider.md) parses compiler and framework
semantics using that context and reports additional reads with verified identities.
