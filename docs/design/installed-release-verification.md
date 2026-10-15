# Installed Release Verification

Installed-command verification has two inputs with different identities: the
release under test and the disposable adopting repository. The harness selects
the former through an exact lock and gives each target its own state. It must
not mutate the developer's consuming repository to simulate adoption or rely
on whichever release a global command happens to resolve.

The focused installed-release contract builds the current committed source
into a temporary installation and derives a publication-shaped v2 lock fixture
from that installation's verified manifest. The inert fixture URLs are never
used for acquisition; publication-index resolution has its own release-module
contract. The harness then verifies the SQLite language-pack conformance cases
and runs the first-adoption fixture through the real launcher. Pack runtime
verification uses the installed release's manifest and pinned Node binary. This
keeps the quick boundary meaningful on clean CI hosts where no prior
installation exists, and prevents a stale developer installation from testing
different production code. The test requires a clean committed source because
the installer binds its artifact to that source revision.
The same ordinary contract writes the Rust companion from the authenticated
installation, removes its optional tools, and passes that companion to the Rust
fixture. Missing-tool rejection, explicit installation, removal and repair
therefore execute before release publication as well as in native release jobs.
Windows CI separately selects the Rust bundle integrity and native test suites.

The broader release harness accepts an explicit prefix and lock so final
verification can exercise an already built native artifact without rebuilding
it between scenarios. Its complete run covers behavior review, language
adoption, and Python regressions. The scoped-analysis fixture exercises selected
agent guidance beside Python source and JavaScript generated inside a Python
package. It checks real unused-code findings, generated-byte preservation, and
owner stability across caller directories and unrelated root manifests.
The Python architecture fixture verifies that external loader declarations bind
current source, registry, and direct-dependency evidence while allowing the
repository to own its loader implementation.
The focused first-adoption result proves that
boundary only; the release checklist still requires the complete installed
fixture suite and native platform evidence.

The complete harness repeatedly enters through the stable launcher so every
scenario also exercises the installed manifest boundary. That is intentionally
broader than a source gate or one image smoke test. Run it once per selected
native archive on a matching native executor, with the archive, installed
prefix, and fixtures on an executor-local filesystem. Cross-architecture QEMU
or Rosetta execution is not native platform evidence and magnifies the cost of
repeated manifest verification; a host bind mount adds another avoidable I/O
penalty. Release automation must fail fast or move to a native runner rather
than treating emulation as a fallback. An exact completed native archive run is
the release evidence and must not be repeated merely to obtain another copy of
the same result.

The digest-pinned OCI smoke test likewise writes an inert v2 lock for its
temporary repository before invoking the image's stable launcher. It verifies
the already installed release and never uses those fixture URLs to acquire
bytes; the image digest and installed manifest remain the runtime evidence.
The version-tag workflow restores the already verified Linux x64 publication,
reuses its extracted engine for both release-index and OCI-context creation,
and runs the image smoke test after the registry assigns the immutable digest.
It does not rebuild source or repeat the native archive contract for the image.

Executable test entrypoints have explicit production ownership and primary
focused suites. Sourced scenario helpers remain test-support code selected by
the harness; they are not independent executable test commands. All fixtures
use temporary state, and their synthetic review acceptances test the protocol
without constituting a review of Code Polishy's own candidate.

Astroid is a separately installed, pinned Python library in the carried runtime.
Its wheel and corresponding source archive are verified against checked-in
hashes before installation. The release includes its distribution metadata,
license texts, and `astroid-source.tar.gz`; the SBOM records its exact version. The archive includes its
LGPL-2.1-or-later license. The library remains a separate Python package
that recipients can replace; modified installations do not retain the original
release's verified digest identity. Target project environments are not used
for analyzer imports.

The Rust syntax helper's Tree-sitter distributions share that carried Python
runtime. Native builders carry their exact manifest and lock alongside the
installed wheel metadata and licenses. Release SBOM composition requires both
parser distributions to match the manifest; missing, duplicate, wrong-version
or undeclared Python distributions cannot be silently left out of publication.

Rust is a built-in release capability. Each native core archive authenticates a separately
installed Rust SDK (Cargo, rustc, rustdoc, rustfmt, Clippy, and host standard
library) and the pinned Nextest executable. Its companion archive carries those
tools; the default core archive omits them. Builders probe these tools before
sealing their inventory; installed commands never acquire a toolchain from rustup or the target
project. Manifest version 7 binds the Rust and Nextest pins into the cross-host
release identity. The launcher's existing historical-manifest reader continues
to interpret versions 2 through 6 with their original tool fields and identity
rules. An older engine does not execute a version 7 release.

The SDK retains upstream component licenses and copyright inventories; parser
wheels retain their distribution license files. Nextest's binary-only archive
requires separately carried upstream notices. The existing Nextest license file
retains its pinned MIT text and a deduplicated source notice inventory for the
362 registry packages in the union of the five supported targets' default-feature
normal and build dependency closures. Registry archive checksums come from the
exact upstream source lock; supplemental notice URLs name the immutable revisions
recorded in the crates' VCS metadata. Numbered shared texts preserve each package's
source location without repeating identical license bodies. The file is bounded
at 2 MiB and remains in the authenticated core alongside SDK notices.

Five packages publish standard license declarations without separate notice files.
Their original package declarations and selected standard license terms are
carried alongside the recovered notices. Apache-2.0 is selected where offered as
an alternative; sigchld declares MIT. Published author metadata is retained,
without inventing copyright holders or years or presenting license templates
as recovered upstream notices. Cargo's published license field identifies the
license under which a package is released; the separate text-file field is an
alternative for nonstandard licenses. The [Cargo manifest contract](https://doc.rust-lang.org/cargo/reference/manifest.html#the-license-and-license-file-fields)
defines those fields. This evidence does not certify all redistribution obligations.

The beta distributes the checksum-pinned official binaries with this source
license evidence and explicitly incomplete native dependency compositions.
This is not an exact linked-binary graph: the upstream build recipe does not
request locked resolution, and its release assets provide no dependency SBOM.
Source paths in all four reviewed Nextest archives corroborate package versions
in the resolved source closure, but cannot establish completeness. A complete
native binary dependency attestation remains follow-up release engineering;
the beta makes no claim of exhaustive vulnerability coverage inside those tools.
This distribution-evidence limit does not exempt target Cargo dependencies or
Rust source from any required policy checks.

The SBOM links each Rust tool to the selected host's reviewed upstream archive
URLs and SHA-256 identities, reusing the installer's checksum inventory. Archive
digests describe those downloads, not the extracted executable. Missing,
duplicate, foreign-version or malformed archive rows fail composition. The
selected SDK's primary licenses and upstream copyright inventories must remain
nonempty regular files in the sealed manifest; Nextest's primary notice must name
its exact version and immutable source revision. Notice locations are recorded
on their components. CycloneDX compositions explicitly mark both native tools'
transitive inventories incomplete until linked-dependency evidence exists.
An upstream source lock is not an exact linked graph, and a dependency's declared
license expression is not its copyright notice. These references authenticate
reviewed inputs, not the upstream builder's identity.

The installed Rust adoption fixture uses a dependency-free Cargo library in a
temporary repository. Every policy operation enters through the exact installed
launcher and lock. It exercises selected formatting, compiler diagnostics in JSON
and SARIF, a passing native suite, an assertion failure, and an empty selection.
Separate doctest cases cover runtime, expected-panic, compile-only, compile-fail
and ignored examples, assertion failure, empty evidence, crate compilation failure
and recovery through the installed compiler and result adapter.
The fixture checks the retained structured test outcome rather than treating the
CLI's nonzero exit as sufficient evidence. It never downloads target packages or
changes the caller's repository authority.

Each native release job also invokes this fixture against its freshly installed
archive. The standalone fixture constructs an inert publication-shaped lock from
the selected manifest, uses the installed Python and launcher, and keeps target
state in a temporary directory. The same scenario runs on all five native hosts;
it does not rebuild the archive or repeat the ordinary source gate. The fixture's
URLs cannot acquire a release and never replace a consuming repository's lock.

The [optional tools contract](optional-rust-tools.md) preserves required Rust
coverage across absent, damaged and repaired installations. The native archive
fixture first verifies rejection without tools, installs the companion against
the core's authority, then exercises Rust behavior and removal/recovery. Small
SDK notice copies remain in the core under `tools/rust-notices/HOST` so release
publication retains their evidence without requiring the optional SDK payload.
