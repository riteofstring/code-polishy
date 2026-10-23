#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 2 || -z "$1" || -z "$2" ]]; then
  echo "usage: release-changelog.sh <version> <changelog-file>" >&2
  exit 2
fi
version="$1"
changelog_file="$2"

if [[ ! -f "${changelog_file}" ]]; then
  echo "The candidate has no CHANGELOG.md to record the ${version} release in." >&2
  exit 1
fi

python3 - "${version}" "${changelog_file}" <<'PY'
import re
import sys
from pathlib import Path

version = sys.argv[1]
try:
    changelog = Path(sys.argv[2]).read_text(encoding="utf-8")
except UnicodeError:
    raise SystemExit("CHANGELOG.md must be valid UTF-8") from None
sections = re.split(r"(?m)^## ", changelog)
matching = [section for section in sections[1:] if section.startswith(f"{version} - ")]
if len(matching) > 1:
    raise SystemExit(f"CHANGELOG.md must contain exactly one release section for {version}")
heading = re.escape(f"{version} - ") + r"[0-9]{4}-[0-9]{2}-[0-9]{2}"
if len(matching) != 1 or not re.fullmatch(heading, matching[0].partition("\n")[0]):
    raise SystemExit(
        f"CHANGELOG.md has no released-version heading for {version}.\n"
        "Move this release's entries out of `## Unreleased` and under an exact "
        f"`## {version} - <YYYY-MM-DD>` heading."
    )
changes = matching[0].partition("\n")[2].strip()
if not changes:
    raise SystemExit(f"CHANGELOG.md release section for {version} is empty")
sys.stdout.buffer.write((changes + "\n").encode("utf-8"))
PY
