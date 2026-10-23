# Node dependency and runtime admission

Native pnpm protections depend on the pinned manager version. An absent feature
on an older version cannot satisfy a protection, and a configured unsupported
setting must not look effective. Version boundaries follow pnpm's documented
introductions, using semantic version comparison including prereleases. The
workspace YAML parser preserves explicit empty policy declarations and verifies
that `allowBuilds` contains boolean decisions. pnpm 11's removed lifecycle
settings are errors instead of substitutes for its current settings owner.

Peer ranges describe compatibility while exact development pins identify the
tested implementation. Both are needed: a valid exact development version can
still fall outside its peer range. The sealed bundle uses the exact npm
`semver` package already present in its lock, promoted to a direct dependency.
It owns npm range grammar and prerelease behavior; Go owns declaration policy,
coverage validation, and findings. Target code and target-installed parsers
never execute. Missing or unreadable range evidence cannot become a pass.

Modern npm lock entries distinguish installation aliases from published package
names. Release-age lookup and dependency comparison use the entry's canonical
`name`, retaining every installed path when aliases share one exact release.
Lock inventory classifies registry, Git, and local sources and preserves each
observed artifact URL without deriving package identity or registry location
from its path. npm registries may serve arbitrary download paths and use separate
artifact hosts. Extraction does not consult candidate configuration or contact
registries. Candidate admission uses the configured registry's exact-version
metadata to bind the canonical name, version, and declared download URL before
accepting its publication time. The same metadata request supplies both checks;
missing or mismatched artifact metadata fails with the lock and installation
locations. Immutable Git pins and contained local paths retain their own checks.
Git and local sources never become registry age queries; Git release-age coverage
remains explicitly unverified. Ordinary entries
without an explicit name retain their installed package identity. Missing
canonical publication times remain findings, and alias handling does not waive
age or advisory policy.

`internal/supplychain/testdata/npm-custom-artifact-lock.json` was generated with
npm 11.6.2 using `install --package-lock-only --ignore-scripts --no-audit --no-fund`
against a temporary local registry. Only the temporary registry origin was
replaced with `https://registry.example`. It records a scoped package and alias
whose download path has no tarball filename. Tests cover candidate admission
through matching registry metadata, source changes, and historical removal
without contacting the former registry.

Node support is separate from the minimum release age. Existing
`releaseArtifacts` entries remain the authority for exact runtime pins. Common
`.node-version` and `.nvmrc` files and GitHub `setup-node` selections expose
missing coverage rather than silently opting out. Workflow inputs must bind to
an inventoried exact version; arbitrary expressions and matrix interpretation
are outside this boundary. `node-version` takes precedence over
`node-version-file`, matching the action's selection behavior.

Online admission reads the Node project's official release schedule once per
profile and checks each inventoried runtime line against its start and end
dates. Unknown lines, invalid dates, and unavailable metadata fail. This check
does not create another runtime inventory, copy upstream lifecycle dates into
configuration, or permit release-age exceptions to suppress end of life.
Offline admission checks inventory coverage and does not claim live support.

Upstream contracts:

- [pnpm settings](https://pnpm.io/settings)
- [npm semver range grammar](https://github.com/npm/node-semver#ranges)
- [npm package-lock package records](https://docs.npmjs.com/cli/v10/configuring-npm/package-lock-json#packages)
- [Node release schedule](https://github.com/nodejs/Release/blob/main/schedule.json)
- [setup-node inputs](https://github.com/actions/setup-node#usage)
