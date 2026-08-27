param(
  [string]$Prefix = '',
  [switch]$AddToUserPath
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$PolicyRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$CallerRoot = (Get-Location).Path
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
  throw 'Code Polishy source installation currently supports Windows x64.'
}
if (-not $Prefix) {
  if (-not $env:LOCALAPPDATA) { throw 'LOCALAPPDATA is required for the default installation prefix.' }
  $Prefix = Join-Path $env:LOCALAPPDATA 'CodePolishy'
} elseif (-not [System.IO.Path]::IsPathRooted($Prefix)) {
  $Prefix = Join-Path $CallerRoot $Prefix
}
$Prefix = [System.IO.Path]::GetFullPath($Prefix)
if ($AddToUserPath -and $Prefix.Contains(';')) {
  throw '-AddToUserPath cannot store an installation prefix containing a semicolon.'
}

function Add-CodePolishyUserPath {
  param([string]$Directory)

  $Comparison = [System.StringComparer]::OrdinalIgnoreCase
  $NormalizedDirectory = $Directory.TrimEnd([System.IO.Path]::DirectorySeparatorChar)
  $UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  $Entries = @($UserPath -split ';' | Where-Object { $_ })
  foreach ($Entry in $Entries) {
    $NormalizedEntry = [Environment]::ExpandEnvironmentVariables($Entry.Trim()).TrimEnd([System.IO.Path]::DirectorySeparatorChar)
    if ($Comparison.Equals($NormalizedEntry, $NormalizedDirectory)) {
      return $false
    }
  }
  $Updated = if ($UserPath) { "$Directory;$UserPath" } else { $Directory }
  [Environment]::SetEnvironmentVariable('Path', $Updated, 'User')
  return $true
}

$GitRootOutput = & git.exe -C $PolicyRoot rev-parse --show-toplevel 2>$null
if ($LASTEXITCODE -ne 0) { throw "Install Code Polishy from a Git checkout; $PolicyRoot is not one." }
$GitRoot = [System.IO.Path]::GetFullPath($GitRootOutput.Trim())
if (-not [System.StringComparer]::OrdinalIgnoreCase.Equals($GitRoot, $PolicyRoot)) {
  throw "Install Code Polishy from its own checkout root, not from $GitRoot."
}
$Status = & git.exe -C $PolicyRoot status --porcelain=v1 --untracked-files=all
if ($LASTEXITCODE -ne 0) { throw 'Reading the Code Polishy checkout state failed.' }
if ($Status) { throw "The Code Polishy checkout at $PolicyRoot is not clean." }
$SourceRevision = (& git.exe -C $PolicyRoot rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $SourceRevision -notmatch '^[0-9a-f]{40}$') {
  throw 'The Code Polishy checkout has no exact committed source revision.'
}

$Scratch = Join-Path ([System.IO.Path]::GetTempPath()) ("code-polishy-install-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $Scratch | Out-Null
try {
  $Bundle = Join-Path $Scratch 'code-polishy-windows-x64.zip'
  & (Join-Path $PSScriptRoot 'build-release.ps1') -Output $Bundle -SourceRevision $SourceRevision
  if ($LASTEXITCODE -ne 0) { throw 'Building the local native release failed.' }
  $BundleDigest = (Get-FileHash -Algorithm SHA256 $Bundle).Hash.ToLowerInvariant()

  $BootstrapRoot = Join-Path $Scratch 'bootstrap'
  Expand-Archive -LiteralPath $Bundle -DestinationPath $BootstrapRoot
  $Bootstrap = Join-Path $BootstrapRoot 'bin\code-polishy.exe'
  if (-not (Test-Path -LiteralPath $Bootstrap -PathType Leaf)) {
    throw 'The local native release contains no installer bootstrap.'
  }
  & $Bootstrap install-bundle --source $Bundle --sha256 $BundleDigest --prefix $Prefix
  if ($LASTEXITCODE -ne 0) { throw 'Installing the local native release failed.' }

  $ManifestPath = Join-Path $BootstrapRoot 'release-manifest.json'
  $Manifest = Get-Content -LiteralPath $ManifestPath -Raw | ConvertFrom-Json
  if ($Manifest.codePolishyVersion -notmatch '^[0-9]+\.[0-9]+\.[0-9]+$' -or
      $Manifest.releaseDigest -notmatch '^[0-9a-f]{64}$') {
    throw 'The installed release reported no exact identity.'
  }
  $ReleaseRoot = Join-Path (Join-Path $Prefix 'releases') "$($Manifest.codePolishyVersion)-$($Manifest.releaseDigest)"
  $ReleaseEngine = Join-Path $ReleaseRoot 'bin\code-polishy.exe'
  $LauncherRoot = Join-Path $Prefix 'bin'
  $Launcher = Join-Path $LauncherRoot 'code-polishy.exe'
  if (-not (Test-Path -LiteralPath $ReleaseEngine -PathType Leaf) -or
      -not (Test-Path -LiteralPath $Launcher -PathType Leaf)) {
    throw 'The verified release or stable launcher is missing after installation.'
  }

  if ($AddToUserPath) {
    if (Add-CodePolishyUserPath -Directory $LauncherRoot) {
      Write-Output "Persistent PATH: added $LauncherRoot to the user PATH; open a new shell to use it."
    } else {
      Write-Output "Persistent PATH: the user PATH already includes $LauncherRoot."
    }
  }

  Write-Output "Installed Code Polishy $($Manifest.codePolishyVersion) at $ReleaseRoot."
  Write-Output "Release digest: $($Manifest.releaseDigest)"
  Write-Output "Launcher: $Launcher"
  $Discovered = Get-Command code-polishy.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
  if (-not $Discovered -or
      -not [System.StringComparer]::OrdinalIgnoreCase.Equals([System.IO.Path]::GetFullPath($Discovered.Path), $Launcher)) {
    Write-Output 'Command discovery: the installed launcher is not first on this PowerShell session''s PATH.'
    Write-Output "  `$env:Path = `"$LauncherRoot;`$env:Path`""
  } else {
    Write-Output 'Command discovery: code-polishy already resolves to the installed launcher.'
  }
  Write-Output 'Require this release in a target repository with:'
  Write-Output "  & `"$ReleaseEngine`" lock"
} finally {
  if (Test-Path -LiteralPath $Scratch) {
    Remove-Item -LiteralPath $Scratch -Recurse -Force
  }
}
