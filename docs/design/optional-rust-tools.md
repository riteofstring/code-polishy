# Optional Rust Tool Installation

Rust analysis and its policy remain native engine capabilities. The Rust SDK
and native test runner are distributed separately from the default installation
because repositories without Rust do not need their large executable payloads.
The existing provider protocol does not yet express Cargo workspace discovery
and native test evidence completely. A tool distribution boundary meets the
installation requirement without introducing an incomplete Rust provider.

Installation optionality never changes required coverage. Governed Rust source,
Cargo inputs, and native Rust test declarations retain their ordinary checks.
An absent or damaged tool installation blocks the relevant operation and names
the setup command. Configuration cannot disable Rust recognition, select an
ambient compiler, change the approved tool versions, or substitute an arbitrary
successful command for native evidence. Unsupported semantics remain failures.

Task-session environment receipts include Nextest with the installed Rust SDK,
not with the core tools that share its executable directory. A core-only
installation can start a task session without Rust. Once the SDK is present,
the receipt requires its complete compiler and Nextest inventory. Rust checks
retain their separate tool-integrity validation before execution.

The authenticated core release owns the exact Rust tool inventory, versions,
host, and file identities. Its default archive omits the corresponding tool
payload; a companion archive carries it. Installing that archive validates its
complete contents against the already selected core release. The archive cannot
declare its own authority, change the repository lock, or supply core files.
Transport location does not replace the release-owned file identities.
Installation assigns executable permissions to the authenticated `bin` and
`libexec` paths and read-only shared permissions to other files. Archive-supplied
permissions cannot make installed tools writable by other users or remove their
required execution bits.

Ordinary checks never download or repair tools. Explicit setup stages and
verifies the companion before replacing installed tools, preserving a working
installation on failure. Installed Rust execution requires complete tool
integrity, including libraries and supporting files, before any compiler,
formatter, test runner, or semantic helper runs. Removing tools cannot turn a
required check into a pass. An incomplete install can still enter setup to
repair itself without weakening subsequent analysis.

Release verification covers a clean installation without Rust, required Rust
checks before installation, installation of the exact companion, real Rust
checks afterward, rejected foreign or modified payloads, interruption recovery,
and removal followed by a blocking required check. All supported hosts require
native execution evidence. Rust tool notices and dependency inventories retain
their publication obligations even though the payload is separately installed.
