param(
  [Parameter(Mandatory = $true)][string]$Output,
  [string]$SourceRevision = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$PolicyRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') { throw 'Local Windows release builds currently require x64.' }
if (-not [System.IO.Path]::IsPathRooted($Output)) { $Output = [System.IO.Path]::GetFullPath((Join-Path (Get-Location) $Output)) }
if (Test-Path -LiteralPath $Output) { throw "Local release output already exists: $Output" }

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

  foreach ($Relative in @(
    'VERSION','LICENSE','README.md','CHANGELOG.md','docs','schema','templates','skills','artifact-security',
    'scripts/go_version.txt','tools/govulncheck-version.txt','tools/node-version.txt','tools/osv-scanner-version.txt',
    'tools/pnpm-version.txt','tools/ruff-version.txt','tools/shellcheck-version.txt','tools/staticcheck-version.txt',
    'tools/trivy-version.txt','tools/javascript_bundle_inventory.txt','tools/javascript_runtime_binaries.txt',
    'tools/javascript_runtime_checksums.txt','tools/windows_tool_checksums.txt',
    '.tools/go/windows-amd64','.tools/shellcheck/windows-x86_64','.tools/javascript/windows-x64'
  )) { Copy-ReleasePath $Relative }

  $StageBin = Join-Path $Stage '.tools/bin'
  New-Item -ItemType Directory -Force -Path $StageBin | Out-Null
  foreach ($Tool in @('staticcheck.exe','govulncheck.exe','osv-scanner.exe','ruff.exe')) {
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

  & $Engine --policy-root $Stage release-manifest materialize --source (Join-Path $PolicyRoot '.tools/javascript/bundle') --destination (Join-Path $Stage '.tools/javascript/bundle')
  if ($LASTEXITCODE -ne 0) { throw 'JavaScript bundle release materialization failed.' }
  $ReleaseDigest = (& $Engine --policy-root $Stage release-manifest write --root $Stage --source-revision $SourceRevision).Trim()
  if ($LASTEXITCODE -ne 0 -or $ReleaseDigest -notmatch '^[0-9a-f]{64}$') { throw 'Native release manifest creation failed.' }
  & $Engine --policy-root $Stage release-manifest verify --root $Stage
  if ($LASTEXITCODE -ne 0) { throw 'Native release verification failed.' }

  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Output) | Out-Null
  Compress-Archive -Path (Join-Path $Stage '*') -DestinationPath $Output -CompressionLevel Optimal
  $BundleDigest = (Get-FileHash -Algorithm SHA256 $Output).Hash.ToLowerInvariant()
  Write-Output "releaseDigest=$ReleaseDigest"
  Write-Output "archiveSHA256=$BundleDigest"
  Write-Output "archive=$Output"
} finally {
  if (Test-Path -LiteralPath $Scratch) { Remove-Item -LiteralPath $Scratch -Recurse -Force }
}
