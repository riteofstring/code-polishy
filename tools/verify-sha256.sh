#!/usr/bin/env bash
set -euo pipefail

# Verify one downloaded file against its checked-in SHA-256 entry.
#
# Usage: verify-sha256.sh <checksum-inventory> <entry-name> <file>
#
# The inventory is a checked-in "<entry-name> <sha256>" table. A missing,
# duplicated, or malformed entry is a failure, never an unverified download.

if [[ "$#" -ne 3 ]]; then
  echo "usage: verify-sha256.sh <checksum-inventory> <entry-name> <file>" >&2
  exit 2
fi

inventory="$1"
entry="$2"
target="$3"

if [[ ! -f "${inventory}" ]]; then
  echo "Missing checksum inventory ${inventory}." >&2
  exit 1
fi
if [[ ! -f "${target}" ]]; then
  echo "Missing file to verify: ${target}." >&2
  exit 1
fi

pinned="$(awk -v entry="${entry}" '$1 == entry { print $2 }' "${inventory}")"
if [[ -z "${pinned}" ]]; then
  echo "No pinned SHA-256 for ${entry} in ${inventory}." >&2
  exit 1
fi
if [[ "$(printf '%s\n' "${pinned}" | wc -l | tr -d '[:space:]')" != "1" ]]; then
  echo "Duplicate pinned SHA-256 entries for ${entry} in ${inventory}." >&2
  exit 1
fi
if [[ ! "${pinned}" =~ ^[0-9a-f]{64}$ ]]; then
  echo "Malformed pinned SHA-256 for ${entry} in ${inventory}." >&2
  exit 1
fi

if command -v shasum >/dev/null 2>&1; then
  actual="$(LC_ALL=C LANG=C shasum -a 256 "${target}" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual="$(LC_ALL=C LANG=C sha256sum "${target}" | awk '{print $1}')"
else
  echo "A SHA-256 checksum tool (shasum or sha256sum) is required." >&2
  exit 1
fi

if [[ "${actual}" != "${pinned}" ]]; then
  echo "Checksum mismatch for ${entry}: pinned ${pinned}, downloaded ${actual}." >&2
  exit 1
fi
