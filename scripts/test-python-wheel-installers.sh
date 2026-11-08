#!/usr/bin/env bash
set -euo pipefail

policy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
case "$(uname -s)" in
  Darwin) os_tag=darwin ;;
  Linux) os_tag=linux ;;
  *) exit 1 ;;
esac
case "$(uname -m)" in
  arm64|aarch64) arch_tag=arm64 ;;
  x86_64|amd64) arch_tag=x64 ;;
  *) exit 1 ;;
esac
"${policy_root}/.tools/python/${os_tag}-${arch_tag}/python" -I -B - "${policy_root}" "${os_tag}-${arch_tag}" <<'PY'
import hashlib
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import zipfile

source = pathlib.Path(sys.argv[1])
host = sys.argv[2]
release = (source / "tools/python-version.txt").read_text().strip()


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def executable(path, content):
    path.write_text(content)
    path.chmod(0o755)


def run_installer(root, package, succeeds=True):
    environment = dict(os.environ, PATH=f"{root / 'bin'}:{os.environ['PATH']}")
    result = subprocess.run(
        [str(root / "tools" / f"install-{package}.sh")],
        env=environment, capture_output=True, text=True, check=False,
    )
    require((result.returncode == 0) == succeeds,
            f"{package} unexpected installer result: {result.stdout}{result.stderr}")
    if not succeeds:
        require(bool(result.stderr.strip()), f"{package} failed without explanation")
    return result


def wheel(root, package, kind="valid"):
    artifact = root / "fixture.whl"
    with zipfile.ZipFile(artifact, "w") as archive:
        if kind != "missing":
            archive.writestr(f"{package}/__init__.py", "value = 'new'\n")
        metadata_version = "8.8.8" if kind == "metadata" else "1.2.3"
        archive.writestr(f"{package}-1.2.3.dist-info/METADATA",
                         f"Metadata-Version: 2.1\nName: {package}\nVersion: {metadata_version}\n")
        archive.writestr(f"{package}-1.2.3.dist-info/licenses/LICENSE", "fixture license\n")
        archive.writestr("unrelated.py", "raise RuntimeError('not installed')\n")
        if kind == "traversal":
            archive.writestr(f"{package}/../../escaped", "unsafe")
    digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
    (root / "tools" / f"{package}_wheel_checksums.txt").write_text(
        f"{package}-1.2.3-py3-none-any.whl {digest}\n")


def old_install(root, site, package):
    shutil.rmtree(site / package, ignore_errors=True)
    for metadata in site.glob(f"{package}-*.dist-info"):
        shutil.rmtree(metadata)
    (site / package).mkdir()
    (site / package / "__init__.py").write_text("value = 'old'\n")
    metadata = site / f"{package}-0.0.1.dist-info"
    metadata.mkdir()
    (metadata / "METADATA").write_text(f"Name: {package}\nVersion: 0.0.1\n")
    (root / ".tools/python" / host / f".code-polishy-{package}-release").write_text("0.0.1\n")


def require_old(root, site, package):
    require((site / package / "__init__.py").read_text() == "value = 'old'\n",
            f"{package} previous package was not restored")
    require(sorted(path.name for path in site.glob(f"{package}-*.dist-info"))
            == [f"{package}-0.0.1.dist-info"], f"{package} previous metadata was not restored")
    runtime = root / ".tools/python" / host
    require((runtime / f".code-polishy-{package}-release").read_text() == "0.0.1\n",
            f"{package} previous marker was not restored")
    require(not list(runtime.glob(f".{package}-staging-*")), "staging was retained")
    require(not list(runtime.glob(f".{package}-backup-*")), "restored backup was retained")


with tempfile.TemporaryDirectory(prefix="code-polishy-wheel-test-") as temporary:
    for package in ("vulture", "packaging"):
        root = pathlib.Path(temporary) / package
        (root / "tools").mkdir(parents=True)
        (root / "bin").mkdir()
        for filename in (f"install-{package}.sh", "install-python-wheel.sh", "verify-sha256.sh"):
            shutil.copy2(source / "tools" / filename, root / "tools" / filename)
        (root / "tools/python-version.txt").write_text(release + "\n")
        (root / "tools" / f"{package}-version.txt").write_text("1.2.3\n")
        runtime = root / ".tools/python" / host
        subprocess.run([sys.executable, "-I", "-B", "-m", "venv", "--without-pip", str(runtime)], check=True)
        executable(runtime / "python", '#!/usr/bin/env bash\nexec "$(dirname "$0")/bin/python" "$@"\n')
        (runtime / ".code-polishy-python-release").write_text(release + "\n")
        site = pathlib.Path(subprocess.check_output(
            [str(runtime / "python"), "-I", "-B", "-c",
             'import sysconfig; print(sysconfig.get_paths()["purelib"])'], text=True).strip())
        (site / "sitecustomize.py").write_text(
            f"import pathlib, sys\nwith pathlib.Path({str(root / 'bytecode.log')!r}).open('a') as output:\n"
            "    output.write(str(sys.dont_write_bytecode) + '\\n')\n")
        executable(root / "bin/curl", f'''#!/usr/bin/env bash
set -euo pipefail
printf '%s\\n' "$2" >>"{root}/downloads"
[[ "$2" == https://files.pythonhosted.org/packages/*/{package}-1.2.3-py3-none-any.whl ]]
cp "{root}/fixture.whl" "$4"
''')
        wheel(root, package)
        run_installer(root, package)
        require((site / package / "__init__.py").read_text() == "value = 'new'\n", "package missing")
        require((site / f"{package}-1.2.3.dist-info/licenses/LICENSE").is_file(), "license missing")
        require(not (site / "unrelated.py").exists(), "unrelated wheel member installed")
        expected_bytecode = "True" if package == "packaging" else "False"
        require(set((root / "bytecode.log").read_text().splitlines()) == {expected_bytecode},
                f"{package} changed Python bytecode behavior")
        require((runtime / f".code-polishy-{package}-release").read_text() == "1.2.3\n", "marker missing")
        run_installer(root, package)
        require(len((root / "downloads").read_text().splitlines()) == 1, "exact installation downloaded again")
        (site / f"{package}-0.0.1.dist-info").mkdir()
        run_installer(root, package)
        require(not (site / f"{package}-0.0.1.dist-info").exists(), "duplicate metadata not repaired")
        for kind in ("checksum", "traversal", "missing", "metadata"):
            old_install(root, site, package)
            wheel(root, package, kind)
            if kind == "checksum":
                with (root / "fixture.whl").open("ab") as artifact:
                    artifact.write(b"tampered")
            run_installer(root, package, succeeds=False)
            require_old(root, site, package)
            require(not (runtime / "escaped").exists(), "unsafe member escaped staging")
        wheel(root, package)
        real_mv = shutil.which("mv")
        executable(root / "bin/mv", f'''#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == */.{package}-staging-*/marker ]]; then
  echo 'injected marker publication failure' >&2
  exit 1
fi
exec "{real_mv}" "$@"
''')
        run_installer(root, package, succeeds=False)
        require_old(root, site, package)
        executable(root / "bin/mv", f'''#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == */.{package}-staging-*/marker || "$1" == */.{package}-backup-*/{package} ]]; then
  echo 'injected publication and restoration failure' >&2
  exit 1
fi
exec "{real_mv}" "$@"
''')
        result = run_installer(root, package, succeeds=False)
        require("could not be restored" in result.stderr, "failed restoration was not reported")
        backups = list(runtime.glob(f".{package}-backup-*"))
        require(len(backups) == 1, "failed restoration discarded the backup")
        require((backups[0] / package / "__init__.py").read_text() == "value = 'old'\n",
                "failed restoration lost the previous package")
        (root / "bin/mv").unlink()
        shutil.move(str(backups[0] / package), site / package)
        shutil.rmtree(backups[0])
        run_installer(root, package)
        for invalid_pin in ("0.0.0+20000101", "invalid"):
            (root / "tools/python-version.txt").write_text(invalid_pin + "\n")
            run_installer(root, package, succeeds=False)
        (root / "tools/python-version.txt").write_text(release + "\n")
        (runtime / ".code-polishy-python-release").write_text("wrong\n")
        run_installer(root, package, succeeds=False)
        (runtime / ".code-polishy-python-release").write_text(release + "\n")
        executable(runtime / "python", f'''#!/usr/bin/env bash
if [[ "$*" == *sysconfig* ]]; then
  echo "{root}/external"
else
  exec "$(dirname "$0")/bin/python" "$@"
fi
''')
        run_installer(root, package, succeeds=False)
        print(f"{package}: installation, reuse, repair, authentication, containment, rollback and runtime checks passed")
PY
