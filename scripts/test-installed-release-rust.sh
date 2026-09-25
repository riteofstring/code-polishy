#!/usr/bin/env bash

exercise_rust_adoption_fixture() {
  local fixture_root="$1" real_git="$2" release="$3" launcher="$4" lock="$5" python
  python="$(installed_fixture_python "${release}")"
  "${python}" -I -B - "${fixture_root}" "${real_git}" "${release}" "${launcher}" "${lock}" <<'PY'
import json
import pathlib
import shutil
import subprocess
import sys

fixture, git, release, launcher, lock = sys.argv[1:]
root = pathlib.Path(fixture) / "rust-adoption"
root.mkdir()


def write(path, content):
    destination = root / path
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(content, encoding="utf-8")


def install_fixture_lock():
    if lock:
        shutil.copyfile(lock, root / ".code-polishy.lock.json")
        return
    manifest = json.loads((pathlib.Path(release) / "release-manifest.json").read_text(encoding="utf-8"))
    hosts = ["darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "windows-x64"]
    value = {
        "lockVersion": 2,
        "codePolishyVersion": manifest["codePolishyVersion"],
        "releaseDigest": manifest["releaseDigest"],
        "features": manifest["features"],
        "publication": {
            "indexUrl": "https://example.invalid/release-index.json",
            "indexSha256": "a" * 64,
            "archives": [{"host": host, "url": f"https://example.invalid/{host}.zip", "sha256": "b" * 64, "size": 1} for host in hosts],
        },
    }
    write(".code-polishy.lock.json", json.dumps(value) + "\n")


def policy(*arguments, status=0, output="json"):
    result = subprocess.run(
        [launcher, "--repo-root", str(root), *arguments, "--format", output],
        text=True, capture_output=True, timeout=300, check=False,
    )
    if result.returncode != status:
        raise AssertionError(f"{arguments}: exit {result.returncode}, expected {status}: {result.stdout[-8000:]} {result.stderr[-2000:]}")
    return json.loads(result.stdout)


def rust_source(expected):
    return f"""pub fn answer() -> u32 {{
    42
}}

#[cfg(test)]
mod tests {{
    #[test]
    fn answers() {{
        assert_eq!(super::answer(), {expected});
    }}
}}
"""


def assert_native_outcome(report, exit_status, category=""):
    commands = report["testCommands"]
    assert len(commands) == 1, commands
    command = commands[0]
    assert command["name"] == "rust-unit", command
    assert command["result"]["exitStatus"] == exit_status, command
    assert command.get("failureCategory", "") == category, command
    if category != "operational":
        artifacts = command["artifacts"]
        assert len(artifacts) == 1 and artifacts[0]["type"] == "nextest-junit", command
        assert artifacts[0]["size"] > 0 and len(artifacts[0]["sha256"]) == 64, command
    executed = report["execution"]["commands"]
    assert len(executed) == 1, executed
    pathlib.Path(executed[0]["argv"][0]).resolve().relative_to(pathlib.Path(release).resolve())


config = {
    "version": 4,
    "project": {"kind": "content"},
    "modules": [{"name": "library", "paths": ["Cargo.toml", "src/**"]}],
    "tests": {"ownership": [], "suites": [
        {"name": "rust-unit", "kind": "unit", "scope": "module", "modules": ["library"], "rust": {}},
        {"name": "full", "kind": "unit", "scope": "repository", "rust": {}},
    ]},
}
write(".code-polishy.json", json.dumps(config) + "\n")
write(".gitignore", "/.tools/\n/.code-polishy-artifacts/\n")
write("Cargo.toml", '[package]\nname="installed-rust"\nversion="1.0.0"\nedition="2024"\n[workspace]\n')
write("Cargo.lock", 'version=4\n[[package]]\nname="installed-rust"\nversion="1.0.0"\n')
write("src/lib.rs", rust_source(42))
install_fixture_lock()
subprocess.run([git, "-C", str(root), "init", "--quiet"], check=True)
subprocess.run([launcher, "--repo-root", str(root), "agents", "install"], check=True)
policy("format", "--all")
subprocess.run([git, "-C", str(root), "add", "-A"], check=True)
subprocess.run([git, "-C", str(root), "-c", "user.name=Rust Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "fixture"], check=True)

baseline = (root / "src/lib.rs").read_bytes()
write("src/lib.rs", rust_source(42).replace("pub fn answer() -> u32 {", "pub fn answer()->u32{"))
policy("format", "--files", "src/lib.rs")
assert (root / "src/lib.rs").read_bytes() == baseline
clean = policy("check", "--all")
assert clean["summary"]["errors"] == 0, clean
assert_native_outcome(policy("test", "--suite", "rust-unit"), 0)

write("src/lib.rs", rust_source(41))
assert_native_outcome(policy("test", "--suite", "rust-unit", status=1), 100, "command-exit")
write("src/lib.rs", "pub fn answer() -> u32 { 42 }\n")
assert_native_outcome(policy("test", "--suite", "rust-unit", status=1), 4, "operational")

write("src/lib.rs", 'pub fn answer() -> u32 { "not an integer" }\n')
invalid = policy("check", "--files", "src/lib.rs", status=1)
assert any(finding["ruleId"] == "quality.rustCompiler" and finding["path"] == "src/lib.rs" for finding in invalid["findings"]), invalid
sarif = policy("check", "--files", "src/lib.rs", status=1, output="sarif")
assert any(result["ruleId"] == "quality.rustCompiler" for result in sarif["runs"][0]["results"]), sarif
write("src/lib.rs", rust_source(42))
assert_native_outcome(policy("test", "--suite", "rust-unit"), 0)
print("Installed Rust formatting, compiler diagnostics, native evidence, failures, and recovery passed.")
PY
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  if [[ "$#" -ne 2 || ! -d "$1" || ! -x "$2" ]]; then
    echo 'usage: test-installed-release-rust.sh RELEASE_ROOT LAUNCHER' >&2
    exit 2
  fi
  policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
  source "${policy_root}/scripts/test-installed-release-architecture.sh"
  fail() { echo "installed Rust fixture failure: $1" >&2; exit 1; }
  fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/code-polishy-installed-rust.XXXXXX")"
  trap 'rm -rf -- "${fixture_root}"' EXIT
  exercise_rust_adoption_fixture "${fixture_root}" "$(command -v git)" "$1" "$2" ""
fi
