# Shell provider

The first-party `shell` pack owns Bash and POSIX source recognition, dialect
parsing, syntax findings, ShellCheck diagnostics, comment facts, and portability
literal facts. Core continues to own repository containment, exact input
identity, comment policy, portability policy, diagnostic authority, tool
authentication, and capability coverage.

Static discovery creates one bounded repository shell scope. Its members and
context contain only provider-owned shell source, allowing focused ShellCheck
runs to follow declared source relationships without reading unrelated target
files. The adapter materializes that authenticated context into an isolated
temporary tree before invoking ShellCheck with target configuration disabled.

One pack artifact contains a cross-compiled analyzer for every supported
platform. The manifest declares each platform binary as executable, so its
authenticated installation receipt preserves the mode required by the exact
policy-owned Node runtime that selects it. The analyzer receives the exact
policy-owned ShellCheck path through the sealed tool environment. No ambient
interpreter, PATH lookup, target configuration, network access, or target command
execution participates.

Formatting is an explicit absence. A formatter will be added only through a
separate product decision with write-safety and idempotence evidence.

Installed conformance cases separately exercise discovery and ShellCheck,
comments and portability facts, POSIX/Bash syntax, and format absence. Exact
accepted differences record the native command evidence and deliberate gains in
diagnostic precision; they do not relax candidate assertions. The active local
evidence covers `darwin-arm64`. The ledger keeps the other declared platforms
and unavailable-tool/host cases blocked until their real installed runtimes are
executed; cross-compilation is build evidence, not runtime evidence.
