import hashlib
import os
import pathlib
import platform
import re
import stat
import subprocess
import sys
import tarfile
import tempfile
import urllib.request
import zipfile

MAXIMUM_ARCHIVE_BYTES = 64 * 1024 * 1024
MAXIMUM_BINARY_BYTES = 128 * 1024 * 1024


def host_archive(version):
    architecture = {
        "x86_64": "x86_64",
        "amd64": "x86_64",
        "arm64": "aarch64",
        "aarch64": "aarch64",
    }.get(platform.machine().lower())
    if architecture is None or sys.platform not in {"linux", "darwin", "win32"}:
        raise ValueError("cargo-nextest is unavailable on this host")
    if sys.platform == "win32" and architecture != "x86_64":
        raise ValueError("cargo-nextest Windows support requires x64")
    target = {
        "linux": architecture + "-unknown-linux-gnu.tar.gz",
        "darwin": "universal-apple-darwin.tar.gz",
        "win32": "x86_64-pc-windows-msvc.zip",
    }[sys.platform]
    return "cargo-nextest-" + version + "-" + target


def locked_archive(root):
    version = (root / "tools/cargo-nextest-version.txt").read_text().strip()
    if re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version) is None:
        raise ValueError("cargo-nextest requires an exact release version")
    asset = host_archive(version)
    checksums = {}
    for line in (root / "tools/cargo-nextest_checksums.txt").read_text().splitlines():
        parts = line.split()
        if (
            len(parts) != 2
            or parts[0] in checksums
            or re.fullmatch(r"[0-9a-f]{64}", parts[1]) is None
        ):
            raise ValueError(
                "cargo-nextest checksum inventory is malformed or duplicated"
            )
        checksums[parts[0]] = parts[1]
    if asset not in checksums:
        raise ValueError("cargo-nextest has no pinned archive for this host")
    return version, asset, checksums[asset]


def download_archive(version, asset, digest, scratch):
    url = (
        "https://github.com/nextest-rs/nextest/releases/download/cargo-nextest-"
        + version
        + "/"
        + asset
    )
    with urllib.request.urlopen(url, timeout=30) as response:
        data = response.read(MAXIMUM_ARCHIVE_BYTES + 1)
    if len(data) > MAXIMUM_ARCHIVE_BYTES or hashlib.sha256(data).hexdigest() != digest:
        raise ValueError("cargo-nextest archive failed authentication")
    destination = scratch / asset
    destination.write_bytes(data)
    return destination


def unpack_binary(archive_path, destination):
    if archive_path.suffix == ".zip":
        data = zip_payload(archive_path)
    else:
        data = tar_payload(archive_path)
    with destination.open("xb") as output:
        output.write(data)
    destination.chmod(0o755)


def read_binary(payload, size):
    if not 0 < size <= MAXIMUM_BINARY_BYTES:
        raise ValueError("cargo-nextest executable is empty or exceeds its size bound")
    data = payload.read(MAXIMUM_BINARY_BYTES + 1)
    if len(data) != size:
        raise ValueError(
            "cargo-nextest executable size differs from its archive record"
        )
    return data


def zip_payload(archive_path):
    with zipfile.ZipFile(archive_path) as archive:
        entries = archive.infolist()
        if (
            len(entries) != 1
            or entries[0].filename != "cargo-nextest.exe"
            or stat.S_IFMT(entries[0].external_attr >> 16) not in {0, stat.S_IFREG}
        ):
            raise ValueError(
                "cargo-nextest archive omitted its unique regular executable"
            )
        with archive.open(entries[0]) as payload:
            return read_binary(payload, entries[0].file_size)


def tar_payload(archive_path):
    with tarfile.open(archive_path, "r:gz") as archive:
        entry = archive.next()
        if (
            entry is None
            or entry.name != "cargo-nextest"
            or entry.type not in {tarfile.REGTYPE, tarfile.AREGTYPE}
        ):
            raise ValueError(
                "cargo-nextest archive omitted its unique regular executable"
            )
        payload = archive.extractfile(entry)
        if payload is None:
            raise ValueError("cargo-nextest archive omitted its executable payload")
        with payload:
            data = read_binary(payload, entry.size)
        if archive.next() is not None:
            raise ValueError(
                "cargo-nextest archive contains unexpected additional entries"
            )
        return data


def verified_binary(path, version):
    if path.is_symlink() or not path.is_file():
        return False
    try:
        result = subprocess.run(
            [str(path), "--version"],
            capture_output=True,
            text=True,
            check=False,
            timeout=10,
        )
        first = result.stdout.splitlines()[0].split()
        return result.returncode == 0 and first[:2] == ["cargo-nextest", version]
    except (OSError, subprocess.SubprocessError, IndexError, UnicodeError):
        return False


def install(root):
    version, asset, digest = locked_archive(root)
    for directory in (root / ".tools", root / ".tools/bin"):
        if directory.is_symlink():
            raise ValueError(
                "cargo-nextest installation directory must not be a symlink"
            )
        directory.mkdir(exist_ok=True)
    name = "cargo-nextest.exe" if sys.platform == "win32" else "cargo-nextest"
    destination = root / ".tools/bin" / name
    if destination.is_symlink() or (destination.exists() and not destination.is_file()):
        raise ValueError("cargo-nextest destination must be a regular executable")
    if verified_binary(destination, version):
        return
    with tempfile.TemporaryDirectory(
        prefix=".nextest-installation-", dir=root / ".tools"
    ) as temporary:
        scratch = pathlib.Path(temporary)
        archive = download_archive(version, asset, digest, scratch)
        staged = scratch / name
        unpack_binary(archive, staged)
        if not verified_binary(staged, version):
            raise ValueError("staged cargo-nextest failed its exact release probe")
        os.replace(staged, destination)


if __name__ == "__main__":
    install(pathlib.Path(sys.argv[1]).resolve(strict=True))
