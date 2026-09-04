#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

case "$(uname -s)" in
  Darwin) os_tag="darwin" ;;
  Linux) os_tag="linux" ;;
  *) echo "Unsupported OS for Python facts lock verification: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch_tag="arm64" ;;
  x86_64|amd64) arch_tag="x64" ;;
  *) echo "Unsupported architecture for Python facts lock verification: $(uname -m)" >&2; exit 1 ;;
esac

python_bin="${policy_root}/.tools/python/${os_tag}-${arch_tag}/python"
if [[ ! -x "${python_bin}" ]]; then
  echo "The policy-owned CPython runtime is unavailable at ${python_bin}." >&2
  exit 1
fi

"${python_bin}" -I -B - "${policy_root}" <<'PY'
import pathlib
import re
import sys
import tomllib

root = pathlib.Path(sys.argv[1])

def read(relative, maximum):
    data = root.joinpath(relative).read_bytes()
    if not data or len(data) > maximum:
        raise SystemExit(f"{relative} has an invalid size")
    return data

manifest = tomllib.loads(read("internal/pythonfacts/pyproject.toml", 1024 * 1024).decode("utf-8"))
lock = tomllib.loads(read("internal/pythonfacts/uv.lock", 8 * 1024 * 1024).decode("utf-8"))
packaging_version = read("tools/packaging-version.txt", 128).decode("ascii").strip()
checksums = read("tools/packaging_wheel_checksums.txt", 64 * 1024).decode("utf-8")

if set(manifest) != {"project", "tool"} or set(manifest["project"]) != {"name", "version", "requires-python", "dependencies"}:
    raise SystemExit("the Python facts manifest has an unexpected shape")
project = manifest["project"]
if project["name"] != "code-polishy-python-facts" or project["requires-python"] != "==3.12.*" or project["dependencies"] != [f"packaging=={packaging_version}"]:
    raise SystemExit("the Python facts manifest does not declare the exact carried dependency")
if manifest["tool"] != {"uv": {"package": False}}:
    raise SystemExit("the Python facts manifest has unexpected uv settings")
if set(lock) != {"version", "revision", "requires-python", "package"} or lock["version"] != 1 or lock["revision"] != 3 or lock["requires-python"] != "==3.12.*":
    raise SystemExit("the Python facts lock has an unexpected identity")
packages = lock["package"]
if not isinstance(packages, list) or len(packages) != 2 or {item.get("name") for item in packages} != {"code-polishy-python-facts", "packaging"}:
    raise SystemExit("the Python facts lock does not contain exactly the complete graph")
by_name = {item["name"]: item for item in packages}
root_package = by_name["code-polishy-python-facts"]
if root_package.get("version") != project["version"] or root_package.get("source") != {"virtual": "."} or root_package.get("dependencies") != [{"name": "packaging"}]:
    raise SystemExit("the Python facts root lock entry does not match its manifest")
metadata = root_package.get("metadata")
if metadata != {"requires-dist": [{"name": "packaging", "specifier": f"=={packaging_version}"}]}:
    raise SystemExit("the Python facts lock metadata does not match its manifest")
distribution = by_name["packaging"]
if distribution.get("version") != packaging_version or distribution.get("source") != {"registry": "https://pypi.org/simple"}:
    raise SystemExit("the packaging lock entry has an invalid registry identity")
wheel_name = f"packaging-{packaging_version}-py3-none-any.whl"
inventory = {}
for raw in checksums.splitlines():
    line = raw.strip()
    if not line or line.startswith("#"):
        continue
    parts = line.split()
    if len(parts) != 2 or parts[0] in inventory or not re.fullmatch(r"[0-9a-f]{64}", parts[1]):
        raise SystemExit("the packaging wheel checksum inventory is malformed")
    inventory[parts[0]] = parts[1]
if set(inventory) != {wheel_name}:
    raise SystemExit("the packaging wheel checksum inventory is incomplete")
wheels = distribution.get("wheels")
if not isinstance(wheels, list) or len(wheels) != 1:
    raise SystemExit("the packaging lock must identify exactly one wheel")
wheel = wheels[0]
if not wheel.get("url", "").endswith("/" + wheel_name) or wheel.get("hash") != "sha256:" + inventory[wheel_name] or not isinstance(wheel.get("size"), int) or wheel["size"] <= 0 or not wheel.get("upload-time"):
    raise SystemExit("the packaging wheel lock identity does not match its checksum inventory")
sdist = distribution.get("sdist")
if not isinstance(sdist, dict) or not sdist.get("url", "").endswith(f"/packaging-{packaging_version}.tar.gz") or not re.fullmatch(r"sha256:[0-9a-f]{64}", sdist.get("hash", "")) or not isinstance(sdist.get("size"), int) or sdist["size"] <= 0 or not sdist.get("upload-time"):
    raise SystemExit("the packaging source-distribution lock identity is malformed")
PY
