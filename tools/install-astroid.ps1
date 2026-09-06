param(
  [Parameter(Mandatory=$true)][string]$Python,
  [Parameter(Mandatory=$true)][string]$PythonRoot,
  [Parameter(Mandatory=$true)][string]$Wheel,
  [Parameter(Mandatory=$true)][string]$Source,
  [Parameter(Mandatory=$true)][string]$Version
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$Installer = @'
import importlib.metadata
import pathlib
import shutil
import sys
import sysconfig
import tempfile
import zipfile

root, wheel, source = map(pathlib.Path, sys.argv[1:4])
version = sys.argv[4]
site = pathlib.Path(sysconfig.get_paths()["purelib"])
if not site.resolve().is_relative_to(root.resolve()):
    raise SystemExit("Astroid installation escapes the carried runtime")
with tempfile.TemporaryDirectory(dir=root) as temporary:
    staging = pathlib.Path(temporary) / "staging"
    backup = pathlib.Path(temporary) / "backup"
    staging.mkdir()
    backup.mkdir()
    names = ["astroid", f"astroid-{version}.dist-info"]
    with zipfile.ZipFile(wheel) as archive:
        for entry in archive.infolist():
            path = pathlib.PurePosixPath(entry.filename)
            if path.is_absolute() or ".." in path.parts:
                raise SystemExit("Astroid wheel contains an unsafe path")
            if path.parts[0] not in names:
                continue
            destination = staging.joinpath(*path.parts)
            if entry.is_dir():
                destination.mkdir(parents=True, exist_ok=True)
            else:
                destination.parent.mkdir(parents=True, exist_ok=True)
                with archive.open(entry) as incoming, destination.open("wb") as outgoing:
                    shutil.copyfileobj(incoming, outgoing)
    if any(not (staging / name).is_dir() for name in names):
        raise SystemExit("Astroid wheel omits its package or metadata")
    previous = list(site.glob("astroid-*.dist-info"))
    if (site / "astroid").exists():
        previous.append(site / "astroid")
    moved = []
    installed = []
    try:
        for path in previous:
            shutil.move(path, backup / path.name)
            moved.append(path)
        for name in names:
            shutil.move(staging / name, site / name)
            installed.append(site / name)
        if importlib.metadata.version("astroid") != version:
            raise ValueError("Astroid version verification failed")
        shutil.copyfile(source, root / "astroid-source.tar.gz")
        (root / ".code-polishy-astroid-release").write_text(version + "\n", encoding="utf-8")
    except BaseException:
        for path in installed:
            shutil.rmtree(path)
        for path in moved:
            shutil.move(backup / path.name, path)
        raise
'@
& $Python -I -B -c $Installer $PythonRoot $Wheel $Source $Version
if ($LASTEXITCODE -ne 0) { throw 'Pinned Astroid installation failed.' }
