$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest



$PolicyRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
  throw 'Code Polishy Windows releases currently require x64.'
}

function Read-Pin([string]$Relative) {
  return (Get-Content -Raw (Join-Path $PolicyRoot $Relative)).Trim()
}

$Checksums = @{}
function Add-Checksums([string]$Relative) {
  Get-Content (Join-Path $PolicyRoot $Relative) | ForEach-Object {
    $Line = $_.Trim()
    if ($Line -and -not $Line.StartsWith('#')) {
      $Parts = $Line -split '\s+'
      if ($Parts.Count -ne 2 -or $Checksums.ContainsKey($Parts[0]) -or $Parts[1] -notmatch '^[0-9a-f]{64}$') {
        throw "Malformed checksum inventory line in ${Relative}: $Line"
      }
      $Checksums[$Parts[0]] = $Parts[1]
    }
  }
}
Add-Checksums 'tools/windows_tool_checksums.txt'
Add-Checksums 'tools/python_runtime_checksums.txt'
Add-Checksums 'tools/vulture_wheel_checksums.txt'
Add-Checksums 'tools/packaging_wheel_checksums.txt'

$Scratch = Join-Path ([System.IO.Path]::GetTempPath()) ("code-polishy-tools-" + [guid]::NewGuid().ToString('N'))
$BundleInstallation = $null
New-Item -ItemType Directory -Path $Scratch | Out-Null
try {
  function Get-Verified([string]$Asset, [string]$Url) {
    if (-not $Checksums.ContainsKey($Asset)) { throw "No checked-in checksum for $Asset" }
    $Destination = Join-Path $Scratch $Asset
    Invoke-WebRequest -Uri $Url -OutFile $Destination
    $Actual = (Get-FileHash -Algorithm SHA256 $Destination).Hash.ToLowerInvariant()
    if ($Actual -ne $Checksums[$Asset]) { throw "Checksum mismatch for $Asset`: $Actual" }
    return $Destination
  }

  function Replace-Directory([string]$Staging, [string]$Destination) {
    if (Test-Path -LiteralPath $Destination) { Remove-Item -LiteralPath $Destination -Recurse -Force }
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Destination) | Out-Null
    Move-Item -LiteralPath $Staging -Destination $Destination
  }

  function Restore-Vulture(
    [string]$SitePackages,
    [string]$Marker,
    [string]$Backup,
    [string]$Version,
    [bool]$NewPackageInstalled,
    [bool]$NewMetadataInstalled,
    [bool]$NewMarkerInstalled
  ) {
    $Package = Join-Path $SitePackages 'vulture'
    if ($NewPackageInstalled -and (Test-Path -LiteralPath $Package -PathType Container)) {
      Remove-Item -LiteralPath $Package -Recurse -Force
    }
    $Metadata = Join-Path $SitePackages "vulture-$Version.dist-info"
    if ($NewMetadataInstalled -and (Test-Path -LiteralPath $Metadata -PathType Container)) {
      Remove-Item -LiteralPath $Metadata -Recurse -Force
    }
    if ($NewMarkerInstalled -and (Test-Path -LiteralPath $Marker -PathType Leaf)) {
      Remove-Item -LiteralPath $Marker -Force
    }
    $PreviousPackage = Join-Path $Backup 'vulture'
    if (Test-Path -LiteralPath $PreviousPackage -PathType Container) {
      Move-Item -LiteralPath $PreviousPackage -Destination $Package
    }
    foreach ($Previous in @(Get-ChildItem -LiteralPath $Backup -Directory -Filter 'vulture-*.dist-info')) {
      Move-Item -LiteralPath $Previous.FullName -Destination $SitePackages
    }
    $PreviousMarker = Join-Path $Backup 'marker'
    if (Test-Path -LiteralPath $PreviousMarker -PathType Leaf) {
      Move-Item -LiteralPath $PreviousMarker -Destination $Marker
    }
  }

  $GoVersion = (Read-Pin 'scripts/go_version.txt').TrimStart('g','o')
  $GoAsset = "go$GoVersion.windows-amd64.zip"
  $GoArchive = Get-Verified $GoAsset "https://go.dev/dl/$GoAsset"
  $GoExtract = Join-Path $Scratch 'go-extract'
  Expand-Archive -LiteralPath $GoArchive -DestinationPath $GoExtract
  $GoRoot = Join-Path $PolicyRoot '.tools/go/windows-amd64'
  Replace-Directory (Join-Path $GoExtract 'go') (Join-Path $GoRoot 'go')
  $Go = Join-Path $GoRoot 'go/bin/go.exe'
  if ((& $Go version) -notmatch "go$([regex]::Escape($GoVersion)) windows/amd64") { throw 'Pinned Go verification failed.' }

  $Bin = Join-Path $PolicyRoot '.tools/bin'
  New-Item -ItemType Directory -Force -Path $Bin | Out-Null
  $SavedGoBin = $env:GOBIN
  $env:GOBIN = $Bin
  try {
    & $Go install "honnef.co/go/tools/cmd/staticcheck@$(Read-Pin 'tools/staticcheck-version.txt')"
    & $Go install "golang.org/x/vuln/cmd/govulncheck@$(Read-Pin 'tools/govulncheck-version.txt')"
  } finally { $env:GOBIN = $SavedGoBin }

  $NodeVersion = Read-Pin 'tools/node-version.txt'
  $NodeAsset = "node-v$NodeVersion-win-x64.zip"
  $NodeArchive = Get-Verified $NodeAsset "https://nodejs.org/dist/v$NodeVersion/$NodeAsset"
  $NodeExtract = Join-Path $Scratch 'node-extract'
  Expand-Archive -LiteralPath $NodeArchive -DestinationPath $NodeExtract
  $JavascriptRoot = Join-Path $PolicyRoot '.tools/javascript'
  $RuntimeRoot = Join-Path $JavascriptRoot 'windows-x64'
  $NodeStaging = Join-Path $NodeExtract "node-v$NodeVersion-win-x64"
  foreach ($Relative in @('npm.cmd','npx.cmd','corepack.cmd','node_modules/npm','node_modules/corepack')) {
    $Target = Join-Path $NodeStaging $Relative
    if (Test-Path -LiteralPath $Target) { Remove-Item -LiteralPath $Target -Recurse -Force }
  }
  $NormalizedNode = Join-Path $Scratch 'node-normalized'
  New-Item -ItemType Directory -Force -Path (Join-Path $NormalizedNode 'bin') | Out-Null
  Copy-Item -LiteralPath (Join-Path $NodeStaging 'node.exe') -Destination (Join-Path $NormalizedNode 'bin/node.exe')
  Replace-Directory $NormalizedNode (Join-Path $RuntimeRoot 'node')
  $Node = Join-Path $RuntimeRoot 'node/bin/node.exe'
  if ((& $Node --version) -ne "v$NodeVersion") { throw 'Pinned Node verification failed.' }

  $PnpmVersion = Read-Pin 'tools/pnpm-version.txt'
  $PnpmAsset = "pnpm-$PnpmVersion.tgz"
  $PnpmArchive = Get-Verified $PnpmAsset "https://registry.npmjs.org/pnpm/-/$PnpmAsset"
  $PnpmExtract = Join-Path $Scratch 'pnpm-extract'
  New-Item -ItemType Directory -Path $PnpmExtract | Out-Null
  & tar.exe -xzf $PnpmArchive -C $PnpmExtract
  if ($LASTEXITCODE -ne 0) { throw 'Pinned pnpm extraction failed.' }
  Replace-Directory (Join-Path $PnpmExtract 'package') (Join-Path $RuntimeRoot 'pnpm')
  $Pnpm = Join-Path $RuntimeRoot 'pnpm/bin/pnpm.cjs'
  if ((& $Node $Pnpm --version) -ne $PnpmVersion) { throw 'Pinned pnpm verification failed.' }

  $ShellcheckVersion = (Read-Pin 'tools/shellcheck-version.txt').TrimStart('v')
  $ShellcheckAsset = "shellcheck-v$ShellcheckVersion.zip"
  $ShellcheckArchive = Get-Verified $ShellcheckAsset "https://github.com/koalaman/shellcheck/releases/download/v$ShellcheckVersion/$ShellcheckAsset"
  $ShellcheckExtract = Join-Path $Scratch 'shellcheck-extract'
  Expand-Archive -LiteralPath $ShellcheckArchive -DestinationPath $ShellcheckExtract
  $Shellcheck = @(Get-ChildItem -Path $ShellcheckExtract -Filter 'shellcheck.exe' -File -Recurse)
  if ($Shellcheck.Count -ne 1) { throw 'ShellCheck archive did not contain exactly one shellcheck.exe.' }
  $ShellcheckRoot = Join-Path $PolicyRoot '.tools/shellcheck/windows-x86_64'
  New-Item -ItemType Directory -Force -Path $ShellcheckRoot | Out-Null
  Copy-Item -LiteralPath $Shellcheck[0].FullName -Destination (Join-Path $ShellcheckRoot 'shellcheck.exe') -Force
  Set-Content -NoNewline -Path (Join-Path $ShellcheckRoot 'version.txt') -Value $ShellcheckVersion

  $RuffVersion = Read-Pin 'tools/ruff-version.txt'
  $RuffAsset = 'ruff-x86_64-pc-windows-msvc.zip'
  $RuffArchive = Get-Verified $RuffAsset "https://github.com/astral-sh/ruff/releases/download/$RuffVersion/$RuffAsset"
  $RuffExtract = Join-Path $Scratch 'ruff-extract'
  Expand-Archive -LiteralPath $RuffArchive -DestinationPath $RuffExtract
  $Ruff = @(Get-ChildItem -Path $RuffExtract -Filter 'ruff.exe' -File -Recurse)
  if ($Ruff.Count -ne 1) { throw 'Ruff archive did not contain exactly one ruff.exe.' }
  Copy-Item -LiteralPath $Ruff[0].FullName -Destination (Join-Path $Bin 'ruff.exe') -Force

  $TyVersion = Read-Pin 'tools/ty-version.txt'
  $TyAsset = 'ty-x86_64-pc-windows-msvc.zip'
  $TyArchive = Get-Verified $TyAsset "https://github.com/astral-sh/ty/releases/download/$TyVersion/$TyAsset"
  $TyExtract = Join-Path $Scratch 'ty-extract'
  Expand-Archive -LiteralPath $TyArchive -DestinationPath $TyExtract
  $Ty = Join-Path $TyExtract 'ty.exe'
  if (-not (Test-Path -LiteralPath $Ty -PathType Leaf)) { throw 'ty archive did not contain the expected executable.' }
  $TyDestination = Join-Path $Bin 'ty.exe'
  Copy-Item -LiteralPath $Ty -Destination $TyDestination -Force
  $TyReported = (& $TyDestination --version)
  if ($LASTEXITCODE -ne 0 -or $TyReported -notmatch "^ty $([regex]::Escape($TyVersion))(?:\s|$)") {
    throw 'Pinned ty verification failed.'
  }

  $PythonRelease = Read-Pin 'tools/python-version.txt'
  if ($PythonRelease -notmatch '^(?<version>[0-9]+\.[0-9]+\.[0-9]+)\+(?<tag>[0-9]{8})$') {
    throw 'tools/python-version.txt must pin CPython and a python-build-standalone tag.'
  }
  $PythonVersion = $Matches.version
  $PythonTag = $Matches.tag
  $PythonRoot = Join-Path $PolicyRoot '.tools/python/windows-x64'
  $Python = Join-Path $PythonRoot 'python.exe'
  $PythonMarker = Join-Path $PythonRoot '.code-polishy-python-release'
  $PythonMarkerValue = ''
  if (Test-Path -LiteralPath $PythonMarker -PathType Leaf) {
    $PythonMarkerValue = (Get-Content -Raw -LiteralPath $PythonMarker).Trim()
  }
  $PythonReported = ''
  if (Test-Path -LiteralPath $Python -PathType Leaf) {
    $PythonProbe = @(& $Python -I -B -c 'import sys; print(".".join(str(value) for value in sys.version_info[:3]))')
    if ($LASTEXITCODE -eq 0 -and $PythonProbe.Count -eq 1) { $PythonReported = $PythonProbe[0].Trim() }
  }
  $PythonCarriesPip = @('Scripts/pip.exe','Scripts/pip3.exe','Scripts/pip3.12.exe','Lib/ensurepip','Lib/site-packages/pip') |
    Where-Object { Test-Path -LiteralPath (Join-Path $PythonRoot $_) }
  if ($PythonReported -ne $PythonVersion -or $PythonMarkerValue -ne $PythonRelease -or $PythonCarriesPip.Count -ne 0) {
    $PythonAsset = "cpython-$PythonRelease-x86_64-pc-windows-msvc-install_only.tar.gz"
    $PythonArchive = Get-Verified $PythonAsset "https://github.com/astral-sh/python-build-standalone/releases/download/$PythonTag/$PythonAsset"
    $PythonExtract = Join-Path $Scratch 'python-extract'
    New-Item -ItemType Directory -Path $PythonExtract | Out-Null
    & tar.exe -xzf $PythonArchive -C $PythonExtract
    if ($LASTEXITCODE -ne 0) { throw 'Pinned CPython extraction failed.' }
    $PythonStaging = Join-Path $PythonExtract 'python'
    if (-not (Test-Path -LiteralPath (Join-Path $PythonStaging 'python.exe') -PathType Leaf)) {
      throw 'Pinned CPython archive did not contain python.exe.'
    }
    foreach ($Relative in @('Scripts/pip.exe','Scripts/pip3.exe','Scripts/pip3.12.exe','Lib/ensurepip','Lib/site-packages/pip')) {
      $Target = Join-Path $PythonStaging $Relative
      if (Test-Path -LiteralPath $Target) { Remove-Item -LiteralPath $Target -Recurse -Force }
    }
    foreach ($Metadata in @(Get-ChildItem -LiteralPath (Join-Path $PythonStaging 'Lib/site-packages') -Directory -Filter 'pip-*.dist-info')) {
      Remove-Item -LiteralPath $Metadata.FullName -Recurse -Force
    }
    Replace-Directory $PythonStaging $PythonRoot
    $Python = Join-Path $PythonRoot 'python.exe'
    $PythonProbe = @(& $Python -I -B -c 'import sys; print(".".join(str(value) for value in sys.version_info[:3]))')
    if ($LASTEXITCODE -ne 0 -or $PythonProbe.Count -ne 1 -or $PythonProbe[0].Trim() -ne $PythonVersion) {
      throw 'Pinned CPython verification failed.'
    }
    Set-Content -NoNewline -Path $PythonMarker -Value $PythonRelease
  }

  $VultureVersion = Read-Pin 'tools/vulture-version.txt'
  if ($VultureVersion -notmatch '^[0-9]+\.[0-9]+(?:\.[0-9]+)?$') { throw 'tools/vulture-version.txt must pin a Vulture release.' }
  $VultureAsset = "vulture-$VultureVersion-py3-none-any.whl"
  $SitePackagesProbe = @(& $Python -I -B -c 'import sysconfig; print(sysconfig.get_paths()["purelib"])')
  if ($LASTEXITCODE -ne 0 -or $SitePackagesProbe.Count -ne 1) { throw 'Pinned CPython did not resolve one site-packages directory.' }
  $SitePackages = $SitePackagesProbe[0].Trim()
  $PythonPrefix = ((Resolve-Path -LiteralPath $PythonRoot).Path.TrimEnd('\') + '\')
  if (-not $SitePackages.StartsWith($PythonPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'Pinned CPython names an external site-packages directory.'
  }
  $VultureMarker = Join-Path $PythonRoot '.code-polishy-vulture-release'
  $VultureMarkerValue = ''
  if (Test-Path -LiteralPath $VultureMarker -PathType Leaf) {
    $VultureMarkerValue = (Get-Content -Raw -LiteralPath $VultureMarker).Trim()
  }
  $VultureProbe = @(& $Python -I -B -c 'import importlib.metadata; print(importlib.metadata.version("vulture"))')
  $VultureExitCode = $LASTEXITCODE
  $VultureMetadata = @()
  if (Test-Path -LiteralPath $SitePackages -PathType Container) {
    $VultureMetadata = @(Get-ChildItem -LiteralPath $SitePackages -Directory -Filter 'vulture-*.dist-info')
  }
  $VultureInstalled = $VultureMarkerValue -eq $VultureVersion -and
    $VultureExitCode -eq 0 -and $VultureProbe.Count -eq 1 -and $VultureProbe[0].Trim() -eq $VultureVersion -and
    $VultureMetadata.Count -eq 1 -and $VultureMetadata[0].Name -eq "vulture-$VultureVersion.dist-info"
  if (-not $VultureInstalled) {
    $VultureArchive = Get-Verified $VultureAsset "https://files.pythonhosted.org/packages/f5/be/f935130312330614811dae2ea9df3f395f6d63889eb6c2e68c14507152ee/$VultureAsset"
    $VultureStaging = Join-Path $Scratch 'vulture-extract'
    New-Item -ItemType Directory -Path $VultureStaging | Out-Null
    $VultureExtractor = @'
import pathlib
import shutil
import sys
import zipfile

wheel = pathlib.Path(sys.argv[1])
destination = pathlib.Path(sys.argv[2])
version = sys.argv[3]
prefixes = ("vulture/", f"vulture-{version}.dist-info/")
with zipfile.ZipFile(wheel) as archive:
    for entry in archive.infolist():
        name = entry.filename
        if not name.startswith(prefixes):
            continue
        relative = pathlib.PurePosixPath(name)
        if relative.is_absolute() or ".." in relative.parts:
            raise SystemExit("Vulture wheel contains an unsafe path")
        target = destination.joinpath(*relative.parts)
        if name.endswith("/"):
            target.mkdir(parents=True, exist_ok=True)
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        with archive.open(entry) as source, target.open("wb") as output:
            shutil.copyfileobj(source, output)
'@
    & $Python -I -B -c $VultureExtractor $VultureArchive $VultureStaging $VultureVersion
    if ($LASTEXITCODE -ne 0) { throw 'Pinned Vulture extraction failed.' }
    $VulturePackage = Join-Path $VultureStaging 'vulture'
    $VultureMetadata = Join-Path $VultureStaging "vulture-$VultureVersion.dist-info"
    if (-not (Test-Path -LiteralPath $VulturePackage -PathType Container) -or -not (Test-Path -LiteralPath $VultureMetadata -PathType Container)) {
      throw 'Pinned Vulture wheel did not contain the expected package.'
    }
    New-Item -ItemType Directory -Force -Path $SitePackages | Out-Null
    $VultureBackup = Join-Path $PythonRoot ('.vulture-backup-' + [guid]::NewGuid().ToString('N'))
    $VultureReplacing = $false
    $NewVulturePackageInstalled = $false
    $NewVultureMetadataInstalled = $false
    $NewVultureMarkerInstalled = $false
    $VultureMarkerStaging = Join-Path $VultureStaging 'marker'
    Set-Content -NoNewline -Path $VultureMarkerStaging -Value $VultureVersion
    try {
      New-Item -ItemType Directory -Path $VultureBackup | Out-Null
      $VultureReplacing = $true
      $InstalledVulture = Join-Path $SitePackages 'vulture'
      if (Test-Path -LiteralPath $InstalledVulture -PathType Container) {
        Move-Item -LiteralPath $InstalledVulture -Destination $VultureBackup
      }
      foreach ($Existing in @(Get-ChildItem -LiteralPath $SitePackages -Directory -Filter 'vulture-*.dist-info')) {
        Move-Item -LiteralPath $Existing.FullName -Destination $VultureBackup
      }
      if (Test-Path -LiteralPath $VultureMarker -PathType Leaf) {
        Move-Item -LiteralPath $VultureMarker -Destination (Join-Path $VultureBackup 'marker')
      }
      Move-Item -LiteralPath $VulturePackage -Destination $InstalledVulture
      $NewVulturePackageInstalled = $true
      Move-Item -LiteralPath $VultureMetadata -Destination (Join-Path $SitePackages "vulture-$VultureVersion.dist-info")
      $NewVultureMetadataInstalled = $true
      $VultureProbe = @(& $Python -I -B -c 'import importlib.metadata; print(importlib.metadata.version("vulture"))')
      $VultureExitCode = $LASTEXITCODE
      $VultureMetadata = @(Get-ChildItem -LiteralPath $SitePackages -Directory -Filter 'vulture-*.dist-info')
      if ($VultureExitCode -ne 0 -or $VultureProbe.Count -ne 1 -or $VultureProbe[0].Trim() -ne $VultureVersion -or
          $VultureMetadata.Count -ne 1 -or $VultureMetadata[0].Name -ne "vulture-$VultureVersion.dist-info") {
        throw 'Pinned Vulture verification failed.'
      }
      Move-Item -LiteralPath $VultureMarkerStaging -Destination $VultureMarker
      $NewVultureMarkerInstalled = $true
      Remove-Item -LiteralPath $VultureBackup -Recurse -Force
      $VultureReplacing = $false
    } catch {
      $VultureFailure = $_
      if ($VultureReplacing) {
        try {
          Restore-Vulture $SitePackages $VultureMarker $VultureBackup $VultureVersion $NewVulturePackageInstalled $NewVultureMetadataInstalled $NewVultureMarkerInstalled
          Remove-Item -LiteralPath $VultureBackup -Recurse -Force
          $VultureReplacing = $false
        } catch {
          throw "Pinned Vulture replacement failed and restoration failed: $($_.Exception.Message)"
        }
      }
      throw $VultureFailure
    }
  }

  $PackagingVersion = Read-Pin 'tools/packaging-version.txt'
  if ($PackagingVersion -notmatch '^[0-9]+\.[0-9]+(?:\.[0-9]+)?$') { throw 'tools/packaging-version.txt must pin a packaging release.' }
  $PackagingAsset = "packaging-$PackagingVersion-py3-none-any.whl"
  $PackagingMarker = Join-Path $PythonRoot '.code-polishy-packaging-release'
  $PackagingMarkerValue = ''
  if (Test-Path -LiteralPath $PackagingMarker -PathType Leaf) {
    $PackagingMarkerValue = (Get-Content -Raw -LiteralPath $PackagingMarker).Trim()
  }
  $PackagingProbe = @(& $Python -I -B -c 'import importlib.metadata; print(importlib.metadata.version("packaging"))')
  $PackagingExitCode = $LASTEXITCODE
  $PackagingMetadata = @()
  if (Test-Path -LiteralPath $SitePackages -PathType Container) {
    $PackagingMetadata = @(Get-ChildItem -LiteralPath $SitePackages -Directory -Filter 'packaging-*.dist-info')
  }
  $PackagingInstalled = $PackagingMarkerValue -eq $PackagingVersion -and
    $PackagingExitCode -eq 0 -and $PackagingProbe.Count -eq 1 -and $PackagingProbe[0].Trim() -eq $PackagingVersion -and
    $PackagingMetadata.Count -eq 1 -and $PackagingMetadata[0].Name -eq "packaging-$PackagingVersion.dist-info"
  if (-not $PackagingInstalled) {
    $PackagingArchive = Get-Verified $PackagingAsset "https://files.pythonhosted.org/packages/63/34/ba1c580383c9eada3711951fef0795c80b829a078d72188184bcab9dd527/$PackagingAsset"
    $PackagingStaging = Join-Path $Scratch 'packaging-extract'
    New-Item -ItemType Directory -Path $PackagingStaging | Out-Null
    $PackagingExtractor = @'
import pathlib
import shutil
import sys
import zipfile

wheel = pathlib.Path(sys.argv[1])
destination = pathlib.Path(sys.argv[2])
version = sys.argv[3]
prefixes = ("packaging/", f"packaging-{version}.dist-info/")
with zipfile.ZipFile(wheel) as archive:
    for entry in archive.infolist():
        name = entry.filename
        if not name.startswith(prefixes):
            continue
        relative = pathlib.PurePosixPath(name)
        if relative.is_absolute() or ".." in relative.parts:
            raise SystemExit("packaging wheel contains an unsafe path")
        target = destination.joinpath(*relative.parts)
        if name.endswith("/"):
            target.mkdir(parents=True, exist_ok=True)
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        with archive.open(entry) as source, target.open("wb") as output:
            shutil.copyfileobj(source, output)
'@
    & $Python -I -B -c $PackagingExtractor $PackagingArchive $PackagingStaging $PackagingVersion
    if ($LASTEXITCODE -ne 0) { throw 'Pinned packaging extraction failed.' }
    $PackagingPackage = Join-Path $PackagingStaging 'packaging'
    $PackagingMetadata = Join-Path $PackagingStaging "packaging-$PackagingVersion.dist-info"
    if (-not (Test-Path -LiteralPath $PackagingPackage -PathType Container) -or -not (Test-Path -LiteralPath $PackagingMetadata -PathType Container)) {
      throw 'Pinned packaging wheel did not contain the expected distribution.'
    }
    $PackagingBackup = Join-Path $PythonRoot ('.packaging-backup-' + [guid]::NewGuid().ToString('N'))
    $InstalledPackaging = Join-Path $SitePackages 'packaging'
    New-Item -ItemType Directory -Path $PackagingBackup | Out-Null
    try {
      if (Test-Path -LiteralPath $InstalledPackaging -PathType Container) {
        Move-Item -LiteralPath $InstalledPackaging -Destination $PackagingBackup
      }
      foreach ($Existing in @(Get-ChildItem -LiteralPath $SitePackages -Directory -Filter 'packaging-*.dist-info')) {
        Move-Item -LiteralPath $Existing.FullName -Destination $PackagingBackup
      }
      if (Test-Path -LiteralPath $PackagingMarker -PathType Leaf) {
        Move-Item -LiteralPath $PackagingMarker -Destination (Join-Path $PackagingBackup 'marker')
      }
      Move-Item -LiteralPath $PackagingPackage -Destination $InstalledPackaging
      Move-Item -LiteralPath $PackagingMetadata -Destination (Join-Path $SitePackages "packaging-$PackagingVersion.dist-info")
      Set-Content -NoNewline -Path $PackagingMarker -Value $PackagingVersion
      $PackagingProbe = @(& $Python -I -B -c 'import importlib.metadata; print(importlib.metadata.version("packaging"))')
      $PackagingMetadata = @(Get-ChildItem -LiteralPath $SitePackages -Directory -Filter 'packaging-*.dist-info')
      if ($LASTEXITCODE -ne 0 -or $PackagingProbe.Count -ne 1 -or $PackagingProbe[0].Trim() -ne $PackagingVersion -or
          $PackagingMetadata.Count -ne 1 -or $PackagingMetadata[0].Name -ne "packaging-$PackagingVersion.dist-info") {
        throw 'Pinned packaging verification failed.'
      }
      Remove-Item -LiteralPath $PackagingBackup -Recurse -Force
    } catch {
      $PackagingFailure = $_
      if (Test-Path -LiteralPath $InstalledPackaging) { Remove-Item -LiteralPath $InstalledPackaging -Recurse -Force }
      $CurrentMetadata = Join-Path $SitePackages "packaging-$PackagingVersion.dist-info"
      if (Test-Path -LiteralPath $CurrentMetadata) { Remove-Item -LiteralPath $CurrentMetadata -Recurse -Force }
      if (Test-Path -LiteralPath $PackagingMarker) { Remove-Item -LiteralPath $PackagingMarker -Force }
      $PreviousPackage = Join-Path $PackagingBackup 'packaging'
      if (Test-Path -LiteralPath $PreviousPackage) { Move-Item -LiteralPath $PreviousPackage -Destination $InstalledPackaging }
      foreach ($Previous in @(Get-ChildItem -LiteralPath $PackagingBackup -Directory -Filter 'packaging-*.dist-info')) {
        Move-Item -LiteralPath $Previous.FullName -Destination $SitePackages
      }
      $PreviousMarker = Join-Path $PackagingBackup 'marker'
      if (Test-Path -LiteralPath $PreviousMarker) { Move-Item -LiteralPath $PreviousMarker -Destination $PackagingMarker }
      Remove-Item -LiteralPath $PackagingBackup -Recurse -Force
      throw $PackagingFailure
    }
  }

  $FactsEnvironment = Join-Path $PolicyRoot 'internal/pythonfacts/.venv'
  $FactsPython = Join-Path $FactsEnvironment 'Scripts/python.exe'
  function Test-PythonFactsEnvironment {
    if (-not (Test-Path -LiteralPath (Join-Path $FactsEnvironment 'pyvenv.cfg') -PathType Leaf) -or
        -not (Test-Path -LiteralPath $FactsPython -PathType Leaf) -or
        (Test-Path -LiteralPath (Join-Path $FactsEnvironment 'Scripts/pip.exe'))) {
      return $false
    }
    $FactsProbe = @(& $FactsPython -I -B -c 'import importlib.metadata,sys; print(".".join(str(value) for value in sys.version_info[:3])); print(importlib.metadata.version("packaging"))')
    return $LASTEXITCODE -eq 0 -and $FactsProbe.Count -eq 2 -and
      $FactsProbe[0].Trim() -eq $PythonVersion -and $FactsProbe[1].Trim() -eq $PackagingVersion
  }
  if (-not (Test-PythonFactsEnvironment)) {
    if (Test-Path -LiteralPath $FactsEnvironment) {
      Remove-Item -LiteralPath $FactsEnvironment -Recurse -Force
    }
    & $Python -I -B -m venv --without-pip --system-site-packages $FactsEnvironment
    if ($LASTEXITCODE -ne 0 -or -not (Test-PythonFactsEnvironment)) {
      if (Test-Path -LiteralPath $FactsEnvironment) {
        Remove-Item -LiteralPath $FactsEnvironment -Recurse -Force
      }
      throw 'The contained Python facts environment failed verification.'
    }
  }

  $OsvVersion = Read-Pin 'tools/osv-scanner-version.txt'
  $OsvAsset = 'osv-scanner_windows_amd64.exe'
  $Osv = Get-Verified $OsvAsset "https://github.com/google/osv-scanner/releases/download/$OsvVersion/$OsvAsset"
  Copy-Item -LiteralPath $Osv -Destination (Join-Path $Bin 'osv-scanner.exe') -Force





  New-Item -ItemType Directory -Force -Path $JavascriptRoot | Out-Null
  $BundleDestination = Join-Path $JavascriptRoot 'bundle'
  $BundleSource = Join-Path $PolicyRoot 'tools/javascript'
  $BundleSourceFiles = @(Get-Content -LiteralPath (Join-Path $BundleSource 'source-files.txt'))
  if ($BundleSourceFiles.Count -eq 0 -or
      @($BundleSourceFiles | Where-Object { -not $_ -or $_ -in @('.', '..') -or $_ -match '[/\\]' }).Count -gt 0 -or
      @($BundleSourceFiles | Group-Object | Where-Object { $_.Count -gt 1 }).Count -gt 0) {
    throw 'The JavaScript bundle source inventory is invalid.'
  }
  foreach ($SourceFile in $BundleSourceFiles) {
    if (-not (Test-Path -LiteralPath (Join-Path $BundleSource $SourceFile) -PathType Leaf)) {
      throw "JavaScript bundle source is missing: $SourceFile"
    }
  }
  if (Test-Path -LiteralPath $BundleDestination) {
    Remove-Item -LiteralPath $BundleDestination -Recurse -Force
  }
  $BundleInstallation = $BundleDestination
  New-Item -ItemType Directory -Path $BundleInstallation | Out-Null
  foreach ($SourceFile in $BundleSourceFiles) {
    Copy-Item -LiteralPath (Join-Path $BundleSource $SourceFile) -Destination (Join-Path $BundleInstallation $SourceFile)
  }
  $Store = Join-Path $JavascriptRoot 'store'
  New-Item -ItemType Directory -Force -Path $Store | Out-Null
  $SavedHome = $env:USERPROFILE
  $env:USERPROFILE = Join-Path $Scratch 'home'
  New-Item -ItemType Directory -Path $env:USERPROFILE | Out-Null
  try {
    & $Node $Pnpm --dir $BundleInstallation --store-dir $Store fetch
    if ($LASTEXITCODE -ne 0) { throw 'Pinned JavaScript fetch failed.' }
    & $Node $Pnpm --dir $BundleInstallation --store-dir $Store install --offline --frozen-lockfile --ignore-scripts
    if ($LASTEXITCODE -ne 0) { throw 'Pinned JavaScript materialization failed.' }
  } finally { $env:USERPROFILE = $SavedHome }
  & $Node (Join-Path $PolicyRoot 'tools/javascript/bundle-manifest.mjs') write $BundleInstallation $PolicyRoot
  if ($LASTEXITCODE -ne 0) { throw 'Pinned JavaScript manifest creation failed.' }
  $BundleInstallation = $null

  & (Join-Path $Bin 'staticcheck.exe') -version
  & (Join-Path $Bin 'govulncheck.exe') -version
  & (Join-Path $Bin 'osv-scanner.exe') --version
  & (Join-Path $Bin 'ruff.exe') --version
  & (Join-Path $Bin 'ty.exe') --version
  & $Python --version
  & $Python -I -B -c 'import importlib.metadata; print("vulture " + importlib.metadata.version("vulture"))'
  & (Join-Path $ShellcheckRoot 'shellcheck.exe') --version
  Write-Host 'Installed the checksum-pinned Code Polishy Windows x64 toolchain.'
} finally {
  if ($BundleInstallation -and (Test-Path -LiteralPath $BundleInstallation)) {
    Remove-Item -LiteralPath $BundleInstallation -Recurse -Force
  }
  if (Test-Path -LiteralPath $Scratch) { Remove-Item -LiteralPath $Scratch -Recurse -Force }
}
