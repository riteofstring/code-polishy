package rustfacts

import "testing"

func TestNextestInstallerSelectsOnlyPinnedSupportedHostArchives(t *testing.T) {
	t.Parallel()
	runInstallerContract(t, `
import install_nextest as nextest
archives = set()
for host, machine, target in [("linux", "x86_64", "x86_64-unknown-linux-gnu.tar.gz"), ("linux", "aarch64", "aarch64-unknown-linux-gnu.tar.gz"), ("darwin", "x86_64", "universal-apple-darwin.tar.gz"), ("darwin", "arm64", "universal-apple-darwin.tar.gz"), ("win32", "AMD64", "x86_64-pc-windows-msvc.zip")]:
    nextest.sys.platform = host
    nextest.platform.machine = lambda: machine
    version, asset, digest = nextest.locked_archive(policy_root)
    assert asset == "cargo-nextest-" + version + "-" + target, asset
    assert len(digest) == 64
    archives.add(asset)
assert len(archives) == 4, archives
nextest.sys.platform = "win32"
nextest.platform.machine = lambda: "ARM64"
try:
    nextest.locked_archive(policy_root)
except ValueError:
    pass
else:
    raise AssertionError("unsupported host borrowed another architecture's binary")
`)
}

func TestNextestInstallerAuthenticatesArchivesBeforeWriting(t *testing.T) {
	t.Parallel()
	runInstallerContract(t, `
import hashlib
import io
import install_nextest as nextest
data = b"nextest archive fixture"
nextest.urllib.request.urlopen = lambda *args, **kwargs: io.BytesIO(data)
asset = "cargo-nextest-fixture.tar.gz"
archive = nextest.download_archive("0.9.138", asset, hashlib.sha256(data).hexdigest(), root)
assert archive.read_bytes() == data
archive.unlink()
try:
    nextest.download_archive("0.9.138", asset, "0" * 64, root)
except ValueError:
    pass
else:
    raise AssertionError("unauthenticated nextest archive passed")
assert not archive.exists()
`)
}

func TestNextestInstallerRejectsLinkedEscapingDuplicateAndOversizedPayloads(t *testing.T) {
	t.Parallel()
	runInstallerContract(t, `
import io
import stat
import tarfile
import zipfile
import install_nextest as nextest
nextest.MAXIMUM_BINARY_BYTES = 16
for name, kind, count, size in [("../escape", tarfile.REGTYPE, 1, 1), ("cargo-nextest", tarfile.SYMTYPE, 1, 0), ("cargo-nextest", tarfile.LNKTYPE, 1, 0), ("cargo-nextest", tarfile.REGTYPE, 2, 1), ("cargo-nextest", tarfile.REGTYPE, 1, 17)]:
    archive = root / "payload.tar.gz"
    with tarfile.open(archive, "w:gz") as output:
        for _ in range(count):
            entry = tarfile.TarInfo(name)
            entry.type, entry.linkname, entry.size = kind, "../escape", size
            output.addfile(entry, io.BytesIO(b"x" * size))
    try:
        nextest.unpack_binary(archive, root / "binary")
    except ValueError:
        pass
    else:
        raise AssertionError("unsafe TAR payload passed")
    assert not (root / "binary").exists()
for name, mode in [("../escape", stat.S_IFREG), ("cargo-nextest.exe", stat.S_IFLNK), ("cargo-nextest.exe", stat.S_IFIFO)]:
    archive = root / "payload.zip"
    with zipfile.ZipFile(archive, "w") as output:
        entry = zipfile.ZipInfo(name)
        entry.external_attr = (mode | 0o755) << 16
        output.writestr(entry, b"x")
    try:
        nextest.unpack_binary(archive, root / "binary")
    except ValueError:
        pass
    else:
        raise AssertionError("unsafe ZIP payload passed")
    assert not (root / "binary").exists()
assert not (root.parent / "escape").exists()
`)
}

func TestNextestInstallerPreservesWorkingBinaryUntilReplacementSucceeds(t *testing.T) {
	t.Parallel()
	runInstallerContract(t, `
import io
import tarfile
import zipfile
import install_nextest as nextest
version, asset, digest = nextest.locked_archive(policy_root)
(root / "tools").mkdir()
(root / "tools/cargo-nextest-version.txt").write_text(version)
(root / "tools/cargo-nextest_checksums.txt").write_text(asset + " " + digest + "\n")
binary = root / ".tools/bin" / ("cargo-nextest.exe" if nextest.sys.platform == "win32" else "cargo-nextest")
binary.parent.mkdir(parents=True)
binary.write_bytes(b"previous working runner")
def fixture_download(version, asset, digest, scratch):
    archive = scratch / asset
    if asset.endswith(".zip"):
        with zipfile.ZipFile(archive, "w") as output:
            output.writestr("cargo-nextest.exe", b"new pinned runner")
    else:
        with tarfile.open(archive, "w:gz") as output:
            entry = tarfile.TarInfo("cargo-nextest")
            entry.size = len(b"new pinned runner")
            output.addfile(entry, io.BytesIO(b"new pinned runner"))
    return archive
nextest.download_archive = fixture_download
nextest.verified_binary = lambda path, version: False
try:
    nextest.install(root)
except ValueError:
    pass
else:
    raise AssertionError("failed staged probe replaced the working runner")
assert binary.read_bytes() == b"previous working runner"
nextest.verified_binary = lambda path, version: path.read_bytes() == b"new pinned runner"
replace = nextest.os.replace
def denied_replace(source, destination):
    raise OSError("replacement is unavailable")
nextest.os.replace = denied_replace
try:
    nextest.install(root)
except OSError:
    pass
else:
    raise AssertionError("failed replacement passed")
assert binary.read_bytes() == b"previous working runner"
nextest.os.replace = replace
nextest.install(root)
assert binary.read_bytes() == b"new pinned runner"
def unexpected_download(*args):
    raise AssertionError("installed release was downloaded again")
nextest.download_archive = unexpected_download
nextest.install(root)
assert not list((root / ".tools").glob(".nextest-installation-*"))
`)
}

func TestNextestInstallerVerifiesTheRealPinnedExecutable(t *testing.T) {
	t.Parallel()
	runInstallerContract(t, `
import install_nextest as nextest
name = "cargo-nextest.exe" if nextest.sys.platform == "win32" else "cargo-nextest"
version = (policy_root / "tools/cargo-nextest-version.txt").read_text().strip()
assert nextest.verified_binary(policy_root / ".tools/bin" / name, version)
assert not nextest.verified_binary(policy_root / ".tools/bin" / name, "0.0.0")
`)
}
