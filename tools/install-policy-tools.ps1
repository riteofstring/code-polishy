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
Get-Content (Join-Path $PolicyRoot 'tools/windows_tool_checksums.txt') | ForEach-Object {
  $Line = $_.Trim()
  if ($Line -and -not $Line.StartsWith('#')) {
    $Parts = $Line -split '\s+'
    if ($Parts.Count -ne 2 -or $Checksums.ContainsKey($Parts[0])) { throw "Malformed Windows checksum inventory line: $Line" }
    $Checksums[$Parts[0]] = $Parts[1]
  }
}

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

  $OsvVersion = Read-Pin 'tools/osv-scanner-version.txt'
  $OsvAsset = 'osv-scanner_windows_amd64.exe'
  $Osv = Get-Verified $OsvAsset "https://github.com/google/osv-scanner/releases/download/$OsvVersion/$OsvAsset"
  Copy-Item -LiteralPath $Osv -Destination (Join-Path $Bin 'osv-scanner.exe') -Force





  New-Item -ItemType Directory -Force -Path $JavascriptRoot | Out-Null
  $BundleDestination = Join-Path $JavascriptRoot 'bundle'
  if (Test-Path -LiteralPath $BundleDestination) {
    Remove-Item -LiteralPath $BundleDestination -Recurse -Force
  }
  $BundleInstallation = $BundleDestination
  Copy-Item -LiteralPath (Join-Path $PolicyRoot 'tools/javascript') -Destination $BundleInstallation -Recurse
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
  $BundleInstallation = $null

  & (Join-Path $Bin 'staticcheck.exe') -version
  & (Join-Path $Bin 'govulncheck.exe') -version
  & (Join-Path $Bin 'osv-scanner.exe') --version
  & (Join-Path $Bin 'ruff.exe') --version
  & (Join-Path $Bin 'ty.exe') --version
  & (Join-Path $ShellcheckRoot 'shellcheck.exe') --version
  Write-Host 'Installed the checksum-pinned Code Polishy Windows x64 toolchain.'
} finally {
  if ($BundleInstallation -and (Test-Path -LiteralPath $BundleInstallation)) {
    Remove-Item -LiteralPath $BundleInstallation -Recurse -Force
  }
  if (Test-Path -LiteralPath $Scratch) { Remove-Item -LiteralPath $Scratch -Recurse -Force }
}
