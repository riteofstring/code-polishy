param(
  [Parameter(Mandatory = $true)][string]$Output,
  [string]$SourceRevision = '',
  [string]$PublicationDirectory = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$PolicyRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') { throw 'Local Windows release builds currently require x64.' }
if (-not [System.IO.Path]::IsPathRooted($Output)) { $Output = [System.IO.Path]::GetFullPath((Join-Path (Get-Location) $Output)) }
if (Test-Path -LiteralPath $Output) { throw "Local release output already exists: $Output" }
if ($PublicationDirectory -and -not [System.IO.Path]::IsPathRooted($PublicationDirectory)) {
  $PublicationDirectory = [System.IO.Path]::GetFullPath((Join-Path (Get-Location) $PublicationDirectory))
}
if ($PublicationDirectory -and (Test-Path -LiteralPath $PublicationDirectory)) {
  throw "Release publication directory already exists: $PublicationDirectory"
}

if (-not $SourceRevision) { $SourceRevision = (& git.exe -C $PolicyRoot rev-parse HEAD).Trim() }
if ($SourceRevision -notmatch '^[0-9a-f]{40}$') { throw 'A release requires an exact lowercase source revision.' }
if ((& git.exe -C $PolicyRoot status --porcelain=v1 --untracked-files=all)) { throw 'Build a release only from a clean checkout.' }

$Go = Join-Path $PolicyRoot '.tools/go/windows-amd64/go/bin/go.exe'
if (-not (Test-Path -LiteralPath $Go -PathType Leaf)) { throw 'Run .\tools\install-policy-tools.ps1 first.' }
$Scratch = Join-Path ([System.IO.Path]::GetTempPath()) ("code-polishy-release-" + [guid]::NewGuid().ToString('N'))
$Stage = Join-Path $Scratch 'stage'
New-Item -ItemType Directory -Path $Stage | Out-Null
try {
  function Copy-ReleasePath([string]$Relative) {
    $Source = Join-Path $PolicyRoot $Relative
    if (-not (Test-Path -LiteralPath $Source)) { throw "Release input is missing: $Relative" }
    $Destination = Join-Path $Stage $Relative
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Destination) | Out-Null
    Copy-Item -LiteralPath $Source -Destination $Destination -Recurse
  }

  function Assert-ReleasePythonCarrier([string]$Root) {
    $PythonRelease = (Get-Content -Raw -LiteralPath (Join-Path $Root 'tools/python-version.txt')).Trim()
    if ($PythonRelease -notmatch '^(?<version>[0-9]+\.[0-9]+\.[0-9]+)\+[0-9]{8}$') {
      throw 'The release stage has an invalid CPython carrier pin.'
    }
    $PythonVersion = $Matches.version
    $PythonRoot = Join-Path $Root '.tools/python/windows-x64'
    $Python = Join-Path $PythonRoot 'python.exe'
    $PythonMarker = Join-Path $PythonRoot '.code-polishy-python-release'
    if (-not (Test-Path -LiteralPath $Python -PathType Leaf) -or -not (Test-Path -LiteralPath $PythonMarker -PathType Leaf)) {
      throw 'The release stage has no policy-owned CPython carrier.'
    }
    $PythonReported = @(& $Python -I -B -c 'import sys; print(".".join(str(value) for value in sys.version_info[:3]))')
    if ($LASTEXITCODE -ne 0 -or $PythonReported.Count -ne 1 -or $PythonReported[0].Trim() -ne $PythonVersion -or
        (Get-Content -Raw -LiteralPath $PythonMarker).Trim() -ne $PythonRelease) {
      throw "The release stage does not carry CPython $PythonRelease."
    }
    $VultureRelease = (Get-Content -Raw -LiteralPath (Join-Path $Root 'tools/vulture-version.txt')).Trim()
    if ($VultureRelease -notmatch '^[0-9]+\.[0-9]+(?:\.[0-9]+)?$') {
      throw 'The release stage has an invalid Vulture carrier pin.'
    }
    $VultureMarker = Join-Path $PythonRoot '.code-polishy-vulture-release'
    if (-not (Test-Path -LiteralPath $VultureMarker -PathType Leaf) -or
        (Get-Content -Raw -LiteralPath $VultureMarker).Trim() -ne $VultureRelease) {
      throw "The release stage does not carry Vulture $VultureRelease."
    }
    $SitePackagesProbe = @(& $Python -I -B -c 'import sysconfig; print(sysconfig.get_paths()["purelib"])')
    if ($LASTEXITCODE -ne 0 -or $SitePackagesProbe.Count -ne 1) {
      throw 'The release stage CPython carrier did not resolve one site-packages directory.'
    }
    $SitePackages = $SitePackagesProbe[0].Trim()
    $PythonPrefix = ((Resolve-Path -LiteralPath $PythonRoot).Path.TrimEnd('\') + '\')
    if (-not $SitePackages.StartsWith($PythonPrefix, [System.StringComparison]::OrdinalIgnoreCase) -or
        -not (Test-Path -LiteralPath $SitePackages -PathType Container)) {
      throw 'The release stage CPython carrier names an external site-packages directory.'
    }
    $VultureReported = @(& $Python -I -B -c 'import importlib.metadata; print(importlib.metadata.version("vulture"))')
    $VultureExitCode = $LASTEXITCODE
    $VultureMetadata = @(Get-ChildItem -LiteralPath $SitePackages -Directory -Filter 'vulture-*.dist-info')
    if ($VultureExitCode -ne 0 -or $VultureReported.Count -ne 1 -or $VultureReported[0].Trim() -ne $VultureRelease -or
        $VultureMetadata.Count -ne 1 -or $VultureMetadata[0].Name -ne "vulture-$VultureRelease.dist-info") {
      throw "The release stage does not carry Vulture $VultureRelease."
    }
    $PackagingRelease = (Get-Content -Raw -LiteralPath (Join-Path $Root 'tools/packaging-version.txt')).Trim()
    if ($PackagingRelease -notmatch '^[0-9]+\.[0-9]+(?:\.[0-9]+)?$') {
      throw 'The release stage has an invalid packaging carrier pin.'
    }
    $PackagingMarker = Join-Path $PythonRoot '.code-polishy-packaging-release'
    $PackagingReported = @(& $Python -I -B -c 'import importlib.metadata; print(importlib.metadata.version("packaging"))')
    $PackagingExitCode = $LASTEXITCODE
    $PackagingMetadata = @(Get-ChildItem -LiteralPath $SitePackages -Directory -Filter 'packaging-*.dist-info')
    if (-not (Test-Path -LiteralPath $PackagingMarker -PathType Leaf) -or
        (Get-Content -Raw -LiteralPath $PackagingMarker).Trim() -ne $PackagingRelease -or
        $PackagingExitCode -ne 0 -or $PackagingReported.Count -ne 1 -or $PackagingReported[0].Trim() -ne $PackagingRelease -or
        $PackagingMetadata.Count -ne 1 -or $PackagingMetadata[0].Name -ne "packaging-$PackagingRelease.dist-info") {
      throw "The release stage does not carry packaging $PackagingRelease."
    }
    foreach ($Relative in @('Scripts/pip.exe','Scripts/pip3.exe','Scripts/pip3.12.exe','Lib/ensurepip','Lib/site-packages/pip')) {
      if (Test-Path -LiteralPath (Join-Path $PythonRoot $Relative)) {
        throw "The release stage carries the ungoverned Python installer $Relative."
      }
    }
  }

  foreach ($Relative in @(
    'VERSION','LICENSE','README.md','CHANGELOG.md','docs','schema','templates','artifact-security',
    'scripts/go_version.txt','tools/govulncheck-version.txt','tools/node-version.txt','tools/osv-scanner-version.txt',
    'internal/pythonfacts/pyproject.toml','internal/pythonfacts/uv.lock','tools/packaging-version.txt','tools/packaging_wheel_checksums.txt',
    'tools/pnpm-version.txt','tools/python-version.txt','tools/python_runtime_checksums.txt','tools/ruff-version.txt',
    'tools/shellcheck-version.txt','tools/staticcheck-version.txt','tools/ty-version.txt','tools/ty.toml',
    'tools/vulture-version.txt','tools/vulture_wheel_checksums.txt',
    'tools/trivy-version.txt','tools/javascript_bundle_inventory.txt','tools/javascript_runtime_binaries.txt',
    'tools/javascript_runtime_checksums.txt','tools/windows_tool_checksums.txt',
    '.tools/go/windows-amd64','.tools/shellcheck/windows-x86_64','.tools/javascript/windows-x64','.tools/python/windows-x64'
  )) { Copy-ReleasePath $Relative }
  Assert-ReleasePythonCarrier $Stage

  $StageBin = Join-Path $Stage '.tools/bin'
  New-Item -ItemType Directory -Force -Path $StageBin | Out-Null
  foreach ($Tool in @('staticcheck.exe','govulncheck.exe','osv-scanner.exe','ruff.exe','ty.exe')) {
    Copy-Item -LiteralPath (Join-Path $PolicyRoot ".tools/bin/$Tool") -Destination (Join-Path $StageBin $Tool)
  }

  $Bin = Join-Path $Stage 'bin'
  New-Item -ItemType Directory -Path $Bin | Out-Null
  $Engine = Join-Path $Bin 'code-polishy.exe'
  $Launcher = Join-Path $Bin 'code-polishy-launcher.exe'
  Push-Location $PolicyRoot
  try {
    & $Go build -trimpath -o $Engine ./cmd/code-polishy
    if ($LASTEXITCODE -ne 0) { throw 'Native engine build failed.' }
    & $Go build -trimpath -o $Launcher ./cmd/code-polishy-launcher
    if ($LASTEXITCODE -ne 0) { throw 'Native launcher build failed.' }
  } finally { Pop-Location }

  $PortableBundle = Join-Path $Scratch 'portable-javascript-bundle'
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
  New-Item -ItemType Directory -Path $PortableBundle | Out-Null
  foreach ($SourceFile in $BundleSourceFiles) {
    Copy-Item -LiteralPath (Join-Path $BundleSource $SourceFile) -Destination (Join-Path $PortableBundle $SourceFile)
  }
  $Node = Join-Path $PolicyRoot '.tools/javascript/windows-x64/node/bin/node.exe'
  $Pnpm = Join-Path $PolicyRoot '.tools/javascript/windows-x64/pnpm/bin/pnpm.cjs'
  $Store = Join-Path $PolicyRoot '.tools/javascript/store'
  $SavedHome = $env:USERPROFILE
  $env:USERPROFILE = Join-Path $Scratch 'home'
  New-Item -ItemType Directory -Path $env:USERPROFILE | Out-Null
  try {
    & $Node $Pnpm --dir $PortableBundle --store-dir $Store install --offline --frozen-lockfile --ignore-scripts --config.nodeLinker=hoisted
    if ($LASTEXITCODE -ne 0) { throw 'Portable JavaScript materialization failed.' }
  } finally { $env:USERPROFILE = $SavedHome }
  $StagedBundle = Join-Path $Stage '.tools/javascript/bundle'
  & $Engine --policy-root $Stage release-manifest materialize --source $PortableBundle --destination $StagedBundle
  if ($LASTEXITCODE -ne 0) { throw 'JavaScript bundle release materialization failed.' }
  & $Node (Join-Path $PolicyRoot 'tools/javascript/bundle-manifest.mjs') write $StagedBundle $PolicyRoot
  if ($LASTEXITCODE -ne 0) { throw 'JavaScript bundle release manifest creation failed.' }
  $ProvenanceOutput = @('{"protocolVersion":3,"operation":"provenance"}' | & $Node (Join-Path $StagedBundle 'runner.mjs'))
  if ($LASTEXITCODE -ne 0 -or $ProvenanceOutput.Count -ne 1) { throw 'Portable JavaScript bundle provenance failed.' }
  $Provenance = $ProvenanceOutput[0] | ConvertFrom-Json
  if ($Provenance.protocolVersion -ne 3 -or $Provenance.operation -ne 'provenance' -or -not $Provenance.result.bundleDigest) {
    throw 'Portable JavaScript bundle returned incomplete provenance.'
  }
  $ReleaseDigest = (& $Engine --policy-root $Stage release-manifest write --root $Stage --source-revision $SourceRevision).Trim()
  if ($LASTEXITCODE -ne 0 -or $ReleaseDigest -notmatch '^[0-9a-f]{64}$') { throw 'Native release manifest creation failed.' }
  & $Engine --policy-root $Stage release-manifest verify --root $Stage
  if ($LASTEXITCODE -ne 0) { throw 'Native release verification failed.' }

  $BundleDigest = (& $Engine --policy-root $Stage release-manifest archive --root $Stage --output $Output).Trim()
  if ($LASTEXITCODE -ne 0 -or $BundleDigest -notmatch '^[0-9a-f]{64}$') { throw 'Native release archive creation failed.' }
  if ($PublicationDirectory) {
    & $Engine --policy-root $Stage release-manifest publish --archive $Output --destination $PublicationDirectory
    if ($LASTEXITCODE -ne 0) { throw 'Native release publication metadata creation failed.' }
  }
  Write-Output "releaseDigest=$ReleaseDigest"
  Write-Output "archiveSHA256=$BundleDigest"
  Write-Output "archive=$Output"
} finally {
  if (Test-Path -LiteralPath $Scratch) { Remove-Item -LiteralPath $Scratch -Recurse -Force }
}
