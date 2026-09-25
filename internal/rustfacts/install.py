import hashlib
import json
import pathlib
import platform
import shutil
import stat
import subprocess
import sys
import sysconfig
import tempfile
import tomllib
import urllib.parse
import urllib.request
import zipfile

DISTRIBUTIONS = {"tree-sitter": "tree_sitter", "tree-sitter-rust": "tree_sitter_rust"}
MAXIMUM_WHEEL_BYTES = 8 * 1024 * 1024
MAXIMUM_UNPACKED_BYTES = 32 * 1024 * 1024
PARSER_PROBE = """
import importlib.metadata
import json
import pathlib
import sys
sys.path.insert(0, sys.argv[1])
from tree_sitter import Language, Parser
import tree_sitter
import tree_sitter_rust
root = pathlib.Path(sys.argv[1]).resolve()
for module in (tree_sitter, tree_sitter_rust):
    if not pathlib.Path(module.__file__).resolve().is_relative_to(root):
        raise SystemExit("Rust parser imported an external distribution")
tree = Parser(Language(tree_sitter_rust.language())).parse(b"fn main() {}")
if tree.root_node.has_error or tree.root_node.type != "source_file":
    raise SystemExit("Rust parser did not produce a complete syntax tree")
names = ("tree-sitter", "tree-sitter-rust")
print(json.dumps({name: importlib.metadata.version(name) for name in names}))
"""


def parser_versions(directory):
    result = subprocess.run(
        [sys.executable, "-I", "-B", "-c", PARSER_PROBE, str(directory)],
        capture_output=True,
        text=True,
        timeout=30,
        check=True,
    )
    return json.loads(result.stdout)


def locked_packages(root):
    project = root / "internal" / "rustfacts"
    manifest = tomllib.loads((project / "pyproject.toml").read_text(encoding="utf-8"))
    required = dict(entry.split("==") for entry in manifest["project"]["dependencies"])
    if set(required) != set(DISTRIBUTIONS):
        raise ValueError(
            "Rust parser dependencies differ from the supported distributions"
        )
    lock = tomllib.loads((project / "uv.lock").read_text(encoding="utf-8"))
    if lock["version"] != 1 or lock["requires-python"] != "==3.12.*":
        raise ValueError("Rust parser requires a supported CPython 3.12 lock")
    packages = []
    for package in lock["package"]:
        name = package["name"]
        if name not in required:
            continue
        if package["version"] != required[name] or package["source"] != {
            "registry": "https://pypi.org/simple"
        }:
            raise ValueError(
                "Rust parser lock does not match its exact public dependencies"
            )
        packages.append(package)
    if sorted(package["name"] for package in packages) != sorted(required):
        raise ValueError("Rust parser lock has missing or duplicate packages")
    return packages


def host_wheel(package):
    host = sys.platform
    machine = platform.machine().lower()
    architecture = {
        "amd64": "x86_64",
        "x86_64": "x86_64",
        "arm64": "aarch64",
        "aarch64": "aarch64",
    }.get(machine)
    if (
        host not in {"linux", "darwin", "win32"}
        or architecture is None
        or (host == "win32" and architecture != "x86_64")
    ):
        raise ValueError("Rust syntax tools are unavailable on this host")
    suffix = {
        "linux": architecture,
        "darwin": {"x86_64": "x86_64", "aarch64": "arm64"}[architecture],
        "win32": "amd64",
    }[host]
    platform_tag = {"linux": "-manylinux", "darwin": "-macosx", "win32": "-win_"}[host]
    abi = "-cp312-cp312-" if package["name"] == "tree-sitter" else "-cp39-abi3-"
    wheels = [
        wheel
        for wheel in package["wheels"]
        if abi in wheel["url"]
        and platform_tag in wheel["url"]
        and wheel["url"].endswith(f"_{suffix}.whl")
    ]
    if len(wheels) != 1:
        raise ValueError(f"Rust parser has no unique locked wheel for {host}/{machine}")
    return wheels[0]


def download_wheel(wheel, scratch):
    url = urllib.parse.urlsplit(wheel["url"])
    if (
        url.scheme != "https"
        or url.netloc != "files.pythonhosted.org"
        or url.query
        or url.fragment
    ):
        raise ValueError("Rust parser wheel does not use the approved archive host")
    expected = wheel["hash"].removeprefix("sha256:")
    if (
        not wheel["hash"].startswith("sha256:")
        or len(expected) != 64
        or any(character not in "0123456789abcdef" for character in expected)
    ):
        raise ValueError("Rust parser wheel has no exact SHA-256 identity")
    with urllib.request.urlopen(wheel["url"], timeout=30) as response:
        data = response.read(MAXIMUM_WHEEL_BYTES + 1)
    if (
        len(data) > MAXIMUM_WHEEL_BYTES
        or len(data) != wheel["size"]
        or hashlib.sha256(data).hexdigest() != expected
    ):
        raise ValueError("Rust parser wheel failed archive authentication")
    archive = scratch / pathlib.PurePosixPath(url.path).name
    archive.write_bytes(data)
    return archive


def unpack_wheel(archive_path, package, destination):
    name = DISTRIBUTIONS[package["name"]]
    prefixes = {name, f"{name}-{package['version']}.dist-info"}
    total = 0
    with zipfile.ZipFile(archive_path) as archive:
        for entry in archive.infolist():
            path = pathlib.PurePosixPath(entry.filename)
            total += entry.file_size
            if (
                total > MAXIMUM_UNPACKED_BYTES
                or path.is_absolute()
                or ".." in path.parts
                or "\\" in entry.filename
            ):
                raise ValueError("Rust parser wheel exceeds safe extraction bounds")
            if (
                not path.parts
                or path.parts[0] not in prefixes
                or stat.S_IFMT(entry.external_attr >> 16)
                not in {0, stat.S_IFDIR, stat.S_IFREG}
            ):
                raise ValueError(
                    "Rust parser wheel contains an unsupported archive entry"
                )
            target = destination.joinpath(*path.parts)
            if entry.is_dir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                with archive.open(entry) as source, target.open("xb") as output:
                    shutil.copyfileobj(source, output)


def existing_distributions(site_packages):
    paths = []
    for name in DISTRIBUTIONS.values():
        package = site_packages / name
        if package.exists() or package.is_symlink():
            paths.append(package)
        paths.extend(site_packages.glob(f"{name}-*.dist-info"))
    if any(path.is_symlink() or not path.is_dir() for path in paths):
        raise ValueError(
            "Rust parser installation contains linked or non-directory distributions"
        )
    return paths


def replace_distributions(staging, site_packages, expected):
    previous = existing_distributions(site_packages)
    backup = pathlib.Path(
        tempfile.mkdtemp(prefix=".rust-syntax-backup-", dir=site_packages.parent)
    )
    moved = []
    installed = []
    try:
        for path in previous:
            path.rename(backup / path.name)
            moved.append(path)
        for path in staging.iterdir():
            target = site_packages / path.name
            path.rename(target)
            installed.append(target)
        if parser_versions(site_packages) != expected:
            raise ValueError("Installed Rust parser does not match its locked versions")
    except BaseException:
        for path in installed:
            shutil.rmtree(path)
        for path in moved:
            (backup / path.name).rename(path)
        backup.rmdir()
        raise
    shutil.rmtree(backup)


def checked_runtime(root):
    release = (
        (root / "tools" / "python-version.txt").read_text(encoding="utf-8").strip()
    )
    if platform.python_version() != release.split("+", 1)[0] or sys.version_info[
        :2
    ] != (3, 12):
        raise ValueError(
            "Rust parser installation requires the pinned CPython 3.12 runtime"
        )
    runtime = pathlib.Path(sys.prefix).resolve()
    if not runtime.is_relative_to(root / ".tools" / "python"):
        raise ValueError("Rust parser installation requires the policy-owned runtime")
    if (runtime / ".code-polishy-python-release").read_text(
        encoding="utf-8"
    ).strip() != release:
        raise ValueError(
            "Rust parser installation requires an authenticated Python release"
        )
    site_packages = pathlib.Path(sysconfig.get_path("platlib")).resolve()
    if not site_packages.is_relative_to(runtime):
        raise ValueError("Rust parser installation names an external package directory")
    return site_packages


def install(root):
    site_packages = checked_runtime(root)
    packages = locked_packages(root)
    expected = {package["name"]: package["version"] for package in packages}
    previous = existing_distributions(site_packages)
    exact_paths = [site_packages / name for name in DISTRIBUTIONS.values()]
    exact_paths.extend(
        site_packages / f"{DISTRIBUTIONS[name]}-{version}.dist-info"
        for name, version in expected.items()
    )
    try:
        if (
            sorted(previous) == sorted(exact_paths)
            and parser_versions(site_packages) == expected
        ):
            print("The exact Rust syntax distributions are already installed.")
            return
    except (
        subprocess.CalledProcessError,
        subprocess.TimeoutExpired,
        json.JSONDecodeError,
    ):
        pass
    with tempfile.TemporaryDirectory(
        prefix=".rust-syntax-staging-", dir=site_packages.parent
    ) as temporary:
        scratch = pathlib.Path(temporary)
        staging = scratch / "packages"
        staging.mkdir()
        for package in packages:
            archive = download_wheel(host_wheel(package), scratch)
            unpack_wheel(archive, package, staging)
        if parser_versions(staging) != expected:
            raise ValueError("Staged Rust parser does not match its locked versions")
        replace_distributions(staging, site_packages, expected)
    print("Installed the authenticated Rust syntax distributions.")


if __name__ == "__main__":
    install(pathlib.Path(sys.argv[1]).resolve(strict=True))
