package rustfacts

import (
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustParserInstallerSelectsEachSupportedHostWithoutSourceBuilds(t *testing.T) {
	t.Parallel()
	runInstallerContract(t, `
packages = installer.locked_packages(policy_root)
hosts = [("linux", "x86_64"), ("linux", "aarch64"), ("darwin", "x86_64"), ("darwin", "arm64"), ("win32", "AMD64")]
urls = set()
for host, machine in hosts:
    installer.sys.platform = host
    installer.platform.machine = lambda: machine
    for package in packages:
        wheel = installer.host_wheel(package)
        assert wheel["url"].endswith(".whl"), wheel
        assert wheel["hash"].startswith("sha256:"), wheel
        urls.add(wheel["url"])
assert len(urls) == 10, urls
installer.sys.platform = "win32"
installer.platform.machine = lambda: "ARM64"
try:
    installer.host_wheel(packages[0])
except ValueError:
    pass
else:
    raise AssertionError("unsupported host borrowed a foreign wheel")
`)
}

func TestRustParserInstallerAuthenticatesBeforeWriting(t *testing.T) {
	t.Parallel()
	runInstallerContract(t, `
import hashlib
import io
payload = b"authenticated wheel fixture"
installer.urllib.request.urlopen = lambda *args, **kwargs: io.BytesIO(payload)
wheel = {"url": "https://files.pythonhosted.org/packages/parser.whl", "hash": "sha256:" + hashlib.sha256(payload).hexdigest(), "size": len(payload)}
archive = installer.download_wheel(wheel, root)
assert archive.read_bytes() == payload
archive.unlink()
wheel["hash"] = "sha256:" + "0" * 64
try:
    installer.download_wheel(wheel, root)
except ValueError:
    pass
else:
    raise AssertionError("wrong archive identity passed")
assert not archive.exists()
`)
}

func TestRustParserInstallerRejectsUnsafeWheelEntries(t *testing.T) {
	t.Parallel()
	runInstallerContract(t, `
import stat
import zipfile
package = {"name": "tree-sitter", "version": "1.0.0"}
cases = [("../escape", stat.S_IFREG), ("tree_sitter/link", stat.S_IFLNK), ("other/file", stat.S_IFREG), ("tree_sitter/pipe", stat.S_IFIFO)]
for index, (name, mode) in enumerate(cases):
    archive = root / f"bad-{index}.whl"
    with zipfile.ZipFile(archive, "w") as output:
        entry = zipfile.ZipInfo(name)
        entry.external_attr = (mode | 0o644) << 16
        output.writestr(entry, b"outside")
    destination = root / f"unpacked-{index}"
    destination.mkdir()
    try:
        installer.unpack_wheel(archive, package, destination)
    except ValueError:
        pass
    else:
        raise AssertionError(f"unsafe entry accepted: {name}")
    assert not list(destination.iterdir())
assert not (root / "escape").exists()
`)
}

func TestRustParserInstallerRestoresPreviousPackagesAfterFailedProbe(t *testing.T) {
	t.Parallel()
	runInstallerContract(t, `
site = root / "site-packages"
staging = root / "staging"
site.mkdir()
staging.mkdir()
old = site / "tree_sitter"
old.mkdir()
(old / "original").write_bytes(b"previous package")
metadata = site / "tree_sitter-0.25.0.dist-info"
metadata.mkdir()
(metadata / "METADATA").write_text("Name: tree-sitter\nVersion: 0.25.0\n", encoding="utf-8")
replacement = staging / "tree_sitter"
replacement.mkdir()
(replacement / "__init__.py").write_text("raise RuntimeError('broken parser')\n", encoding="utf-8")
try:
    installer.replace_distributions(staging, site, {"tree-sitter": "0.26.0", "tree-sitter-rust": "0.24.2"})
except installer.subprocess.CalledProcessError:
    pass
else:
    raise AssertionError("a broken parser was installed")
assert (old / "original").read_bytes() == b"previous package"
assert (metadata / "METADATA").read_text(encoding="utf-8") == "Name: tree-sitter\nVersion: 0.25.0\n"
assert not (old / "__init__.py").exists()
assert not list(root.glob(".rust-syntax-backup-*"))
`)
}

func TestRustParserInstallerVerifiesRealStagedDistributions(t *testing.T) {
	t.Parallel()
	runInstallerContract(t, `
import shutil
site = root / "site-packages"
staging = root / "staging"
site.mkdir()
staging.mkdir()
installed = installer.checked_runtime(policy_root)
packages = installer.locked_packages(policy_root)
expected = {package["name"]: package["version"] for package in packages}
for path in installer.existing_distributions(installed):
    shutil.copytree(path, staging / path.name)
assert installer.parser_versions(staging) == expected
installer.replace_distributions(staging, site, expected)
assert installer.parser_versions(site) == expected
for name, version in expected.items():
    metadata = site / f"{installer.DISTRIBUTIONS[name]}-{version}.dist-info"
    assert (metadata / "METADATA").is_file()
    assert list(metadata.rglob("LICENSE*")), metadata
assert not list(root.glob(".rust-syntax-backup-*"))
`)
}

func runInstallerContract(t *testing.T, body string) {
	t.Helper()
	repo := rustRepository(t)
	program := "import pathlib, sys\nsys.path.insert(0, sys.argv[1])\nimport install as installer\npolicy_root = pathlib.Path(sys.argv[2])\nroot = pathlib.Path(sys.argv[3])\n" + body
	command := policy.Command{
		Name: "rust-parser-installer-contract", Cwd: ".", Managed: true, SealedEnvironment: true, TimeoutSeconds: 30,
		Argv: repo.PythonCommand(program, filepath.Join(repo.PolicyRoot, "internal", "rustfacts"), repo.PolicyRoot, repo.Root),
	}
	_, output, err := runner.RunCaptured(t.Context(), runner.OSRunner{}, repo.Root, command, runner.CaptureStructured)
	if err != nil {
		t.Fatalf("installer contract: %v; %s %s", err, output.Stdout, output.Stderr)
	}
}
