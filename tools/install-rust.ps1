$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$PolicyRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
  throw 'Code Polishy Windows Rust tooling currently requires x64.'
}
$RustVersion = (Get-Content -Raw (Join-Path $PolicyRoot 'tools/rust-version.txt')).Trim()
if ($RustVersion -notmatch '^[0-9]+\.[0-9]+\.[0-9]+$') {
  throw 'tools/rust-version.txt must pin an exact Rust release.'
}
$Triple = 'x86_64-pc-windows-msvc'
$Destination = Join-Path $PolicyRoot '.tools/rust/windows-x64'

function Test-RustRuntime([string]$Directory) {
  foreach ($Tool in @('rustc', 'rustdoc', 'cargo', 'rustfmt', 'cargo-fmt', 'clippy-driver', 'cargo-clippy')) {
    if (-not (Test-Path -LiteralPath (Join-Path $Directory "bin/$Tool.exe") -PathType Leaf)) { return $false }
  }
  $Marker = Join-Path $Directory '.code-polishy-rust-release'
  if (-not (Test-Path -LiteralPath $Marker -PathType Leaf)) { return $false }
  if ((Get-Content -Raw -LiteralPath $Marker).Trim() -ne $RustVersion) { return $false }
  if (-not (Test-Path -LiteralPath (Join-Path $Directory "lib/rustlib/$Triple/lib") -PathType Container)) { return $false }
  $Reported = & (Join-Path $Directory 'bin/rustc.exe') --version
  return $LASTEXITCODE -eq 0 -and $Reported -match "^rustc $([regex]::Escape($RustVersion)) "
}

if (Test-RustRuntime $Destination) {
  Write-Host "Rust $RustVersion is already installed at $Destination."
  exit 0
}
$Checksums = @{}
Get-Content (Join-Path $PolicyRoot 'tools/rust_checksums.txt') | ForEach-Object {
  $Parts = $_.Trim() -split '\s+'
  if ($Parts.Count -ne 2 -or $Checksums.ContainsKey($Parts[0]) -or $Parts[1] -notmatch '^[0-9a-f]{64}$') {
    throw 'Malformed Rust checksum inventory.'
  }
  $Checksums[$Parts[0]] = $Parts[1]
}
$Scratch = Join-Path $PolicyRoot ('.tools/rust/.installation.' + [guid]::NewGuid().ToString('N'))
$Staging = Join-Path $Scratch 'runtime'
$Previous = Join-Path $Scratch 'previous'
New-Item -ItemType Directory -Path $Staging -Force | Out-Null
try {
  foreach ($Component in @('rustc', 'rust-std', 'cargo', 'rustfmt', 'clippy')) {
    $Stem = "$Component-$RustVersion-$Triple"
    $Asset = "$Stem.tar.xz"
    if (-not $Checksums.ContainsKey($Asset)) { throw "No pinned checksum for $Asset." }
    $Archive = Join-Path $Scratch $Asset
    Invoke-WebRequest -Uri "https://static.rust-lang.org/dist/$Asset" -OutFile $Archive
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath $Archive).Hash.ToLowerInvariant() -ne $Checksums[$Asset]) {
      throw "Checksum mismatch for $Asset."
    }
    & tar.exe -xJf $Archive -C $Scratch
    if ($LASTEXITCODE -ne 0) { throw "Cannot extract $Asset." }
    $Payload = $Component
    if ($Component -eq 'rust-std') { $Payload = "rust-std-$Triple" }
    if ($Component -in @('rustfmt', 'clippy')) { $Payload = "$Component-preview" }
    $Extracted = Join-Path $Scratch $Stem
    foreach ($Directory in @('bin', 'lib', 'libexec', 'share', 'etc')) {
      $Source = Join-Path $Extracted "$Payload/$Directory"
      if (Test-Path -LiteralPath $Source -PathType Container) {
        $Target = Join-Path $Staging $Directory
        New-Item -ItemType Directory -Path $Target -Force | Out-Null
        Get-ChildItem -LiteralPath $Source -Force | Copy-Item -Destination $Target -Recurse -Force
      }
    }
    $LicenseDirectory = Join-Path $Staging "share/licenses/$Component"
    New-Item -ItemType Directory -Path $LicenseDirectory -Force | Out-Null
    foreach ($License in @('LICENSE-APACHE', 'LICENSE-MIT', 'COPYRIGHT')) {
      $Source = Join-Path $Extracted $License
      if (Test-Path -LiteralPath $Source -PathType Leaf) {
        Copy-Item -LiteralPath $Source -Destination (Join-Path $LicenseDirectory $License)
      }
    }
  }
  Set-Content -LiteralPath (Join-Path $Staging '.code-polishy-rust-release') -Value $RustVersion
  if (-not (Test-RustRuntime $Staging)) { throw 'The staged Rust toolchain is incomplete or has the wrong version.' }
  if (Test-Path -LiteralPath $Destination) { Move-Item -LiteralPath $Destination -Destination $Previous }
  Move-Item -LiteralPath $Staging -Destination $Destination
  Write-Host "Installed Rust $RustVersion at $Destination."
} finally {
  if ((Test-Path -LiteralPath $Previous) -and -not (Test-Path -LiteralPath $Destination)) {
    Move-Item -LiteralPath $Previous -Destination $Destination
  }
  Remove-Item -LiteralPath $Scratch -Recurse -Force
}
