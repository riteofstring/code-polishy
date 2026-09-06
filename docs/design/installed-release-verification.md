# Installed Release Verification

Installed-command verification has two inputs with different identities: the
release under test and the disposable adopting repository. The harness selects
the former through an exact lock and gives each target its own state. It must
not mutate the developer's consuming repository to simulate adoption or rely
on whichever release a global command happens to resolve.

The focused installed-release contract builds the current committed source
into a temporary installation and obtains a lock from that installation. It
then runs the first-adoption fixture through the real launcher. This keeps the
quick boundary meaningful on clean CI hosts where no prior installation
exists, and prevents a stale developer installation from testing different
production code. The test requires a clean committed source because the
installer binds its artifact to that source revision.

The broader release harness accepts an explicit prefix and lock so final
verification can exercise an already built native artifact without rebuilding
it between scenarios. Its complete run covers behavior review, language
adoption, and Python regressions. The scoped-analysis fixture exercises selected
agent guidance beside Python source and JavaScript generated inside a Python
package. It checks real unused-code findings, generated-byte preservation, and
owner stability across caller directories and unrelated root manifests.
The focused first-adoption result proves that
boundary only; the release checklist still requires the complete installed
fixture suite and native platform evidence.

Executable test entrypoints have explicit production ownership and primary
focused suites. Sourced scenario helpers remain test-support code selected by
the harness; they are not independent executable test commands. All fixtures
use temporary state, and their synthetic review acceptances test the protocol
without constituting a review of Code Polishy's own candidate.

Astroid is a separately installed, pinned Python library in the carried runtime.
Its wheel and corresponding source archive are verified against checked-in
hashes before installation. The release includes its distribution metadata,
license texts, and `astroid-source.tar.gz`; the SBOM records its exact version
and LGPL-2.1-or-later license. The library remains a separate Python package
that recipients can replace; modified installations do not retain the original
release's verified digest identity. Target project environments are not used
for analyzer imports.
