$CodePolishyManagedWrapper = $true
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Fail([string]$Message) {
  [Console]::Error.WriteLine("code-polishyw: $Message")
  exit 1
}

function Show-Usage {
  Write-Output 'Usage:'
  Write-Output '  .\code-polishyw.ps1 setup [--source URL]'
  Write-Output '  .\code-polishyw.ps1 COMMAND [ARG...]'
}

$WrapperArguments = @($args)
$RepositoryRoot = [System.IO.Path]::GetFullPath($PSScriptRoot)
$LockPath = Join-Path $RepositoryRoot '.code-polishy.lock.json'
if (-not (Test-Path -LiteralPath $LockPath -PathType Leaf)) {
  Fail '.code-polishy.lock.json must be a regular file'
}
$LockItem = Get-Item -LiteralPath $LockPath -Force
if (($LockItem.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
  Fail '.code-polishy.lock.json must not be a symbolic link'
}
$LockText = [System.IO.File]::ReadAllText($LockPath)
if ($LockText.Length -eq 0 -or $LockText.Length -gt 16384) {
  Fail '.code-polishy.lock.json exceeds the wrapper input limit'
}
foreach ($RequiredKey in @('lockVersion', 'codePolishyVersion', 'releaseDigest')) {
  if ([regex]::Matches($LockText, '"' + [regex]::Escape($RequiredKey) + '"\s*:').Count -ne 1) {
    Fail ".code-polishy.lock.json must contain exactly one $RequiredKey field"
  }
}
try {
  $Lock = $LockText | ConvertFrom-Json
} catch {
  Fail '.code-polishy.lock.json is not valid JSON'
}
$Version = [string]$Lock.codePolishyVersion
$Digest = [string]$Lock.releaseDigest
if (($Lock.lockVersion -isnot [int]) -and ($Lock.lockVersion -isnot [long])) {
  Fail '.code-polishy.lock.json has an invalid lockVersion type'
}
if ([long]$Lock.lockVersion -ne 1) {
  Fail '.code-polishy.lock.json uses an unsupported lockVersion'
}
if ($Version -cnotmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?$') {
  Fail '.code-polishy.lock.json has an invalid codePolishyVersion'
}
if ($Digest -cnotmatch '^[0-9a-f]{64}$') {
  Fail '.code-polishy.lock.json has an invalid releaseDigest'
}
if (-not $env:LOCALAPPDATA) {
  Fail 'LOCALAPPDATA is required to locate the shared Code Polishy installation'
}
$InstallPrefix = Join-Path $env:LOCALAPPDATA 'CodePolishy'
$ReleaseRoot = Join-Path (Join-Path $InstallPrefix 'releases') "$Version-$Digest"
$Launcher = Join-Path (Join-Path $InstallPrefix 'bin') 'code-polishy.exe'

function Test-InstalledRelease {
  if (-not (Test-Path -LiteralPath $ReleaseRoot -PathType Container) -or
      -not (Test-Path -LiteralPath $Launcher -PathType Leaf)) {
    return $false
  }
  $ReleaseItem = Get-Item -LiteralPath $ReleaseRoot -Force
  $LauncherItem = Get-Item -LiteralPath $Launcher -Force
  if (($ReleaseItem.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0 -or
      ($LauncherItem.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
    return $false
  }
  & $Launcher --repo-root $RepositoryRoot version *> $null
  return $LASTEXITCODE -eq 0
}

if ($WrapperArguments.Count -eq 0) {
  [Console]::Error.WriteLine((Show-Usage | Out-String).TrimEnd())
  exit 2
}

if ($WrapperArguments[0] -cne 'setup') {
  if (-not (Test-InstalledRelease)) {
    Fail 'the locked release is not installed; run .\code-polishyw.ps1 setup'
  }
  & $Launcher --repo-root $RepositoryRoot @WrapperArguments
  exit $LASTEXITCODE
}

$Source = 'https://github.com/riteofstring/code-polishy.git'
$Index = 1
while ($Index -lt $WrapperArguments.Count) {
  $Argument = $WrapperArguments[$Index]
  if ($Argument -ceq '--source') {
    if ($Index + 1 -ge $WrapperArguments.Count) {
      Fail '--source requires a URL or path'
    }
    $Source = $WrapperArguments[$Index + 1]
    $Index += 2
  } elseif ($Argument.StartsWith('--source=', [System.StringComparison]::Ordinal)) {
    $Source = $Argument.Substring(9)
    $Index++
  } elseif ($Argument -in @('--help', '-h')) {
    Show-Usage
    exit 0
  } else {
    Fail "unknown setup option: $Argument"
  }
}
if (-not $Source -or $Source.Contains("`n") -or $Source.Contains("`r")) {
  Fail '--source must be a non-empty single-line URL or path'
}

if (Test-InstalledRelease) {
  Write-Output "Code Polishy $Version is already ready for this repository."
  exit 0
}
if (-not (Get-Command git.exe -CommandType Application -ErrorAction SilentlyContinue)) {
  Fail 'git.exe is required for setup'
}

$Scratch = Join-Path ([System.IO.Path]::GetTempPath()) ("code-polishyw-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $Scratch | Out-Null
try {
  $Checkout = Join-Path $Scratch 'source'
  $Tag = "v$Version"
  & git.exe clone --quiet --depth 1 --single-branch --branch $Tag -- $Source $Checkout
  if ($LASTEXITCODE -ne 0) {
    Fail 'could not clone the exact locked Code Polishy tag'
  }
  $Origin = (& git.exe -C $Checkout remote get-url origin | Out-String).Trim()
  if (-not [System.StringComparer]::Ordinal.Equals($Origin, $Source)) {
    Fail 'the source checkout origin does not match the explicit source'
  }
  $Dirty = (& git.exe -C $Checkout status --porcelain=v1 --untracked-files=all | Out-String).Trim()
  if ($LASTEXITCODE -ne 0 -or $Dirty) {
    Fail 'the source checkout is not clean'
  }
  $HeadCommit = (& git.exe -C $Checkout rev-parse --verify HEAD | Out-String).Trim()
  $TagRef = "refs/tags/$Tag"
  $TagType = (& git.exe -C $Checkout cat-file -t $TagRef | Out-String).Trim()
  if ($LASTEXITCODE -ne 0 -or $TagType -cne 'tag') {
    Fail 'the locked source tag is not annotated'
  }
  $TagRecord = @(& git.exe -C $Checkout cat-file -p $TagRef)
  if ($LASTEXITCODE -ne 0) {
    Fail 'the locked source tag cannot be inspected'
  }
  $ObjectLines = @($TagRecord | Where-Object { $_ -cmatch '^object [0-9a-f]{40,64}$' })
  $TypeLines = @($TagRecord | Where-Object { $_ -cmatch '^type [a-z]+$' })
  if ($ObjectLines.Count -ne 1 -or $TypeLines.Count -ne 1) {
    Fail 'the locked source tag has an invalid tag object'
  }
  $TagTarget = $ObjectLines[0].Substring(7)
  $TagKind = $TypeLines[0].Substring(5)
  $PeeledCommit = (& git.exe -C $Checkout rev-parse --verify "$TagRef^{}" | Out-String).Trim()
  if ($LASTEXITCODE -ne 0 -or $TagKind -cne 'commit' -or
      -not [System.StringComparer]::Ordinal.Equals($TagTarget, $HeadCommit) -or
      -not [System.StringComparer]::Ordinal.Equals($PeeledCommit, $HeadCommit)) {
    Fail 'the locked annotated tag does not point directly to the checked-out commit'
  }
  $VersionPath = Join-Path $Checkout 'VERSION'
  if (-not (Test-Path -LiteralPath $VersionPath -PathType Leaf)) {
    Fail 'the source checkout has no regular VERSION file'
  }
  $VersionItem = Get-Item -LiteralPath $VersionPath -Force
  if (($VersionItem.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
    Fail 'the source checkout VERSION must not be a symbolic link'
  }
  $SourceVersion = [System.IO.File]::ReadAllText($VersionPath).TrimEnd([char[]]"`r`n")
  if (-not [System.StringComparer]::Ordinal.Equals($SourceVersion, $Version)) {
    Fail 'the source checkout VERSION does not match the repository lock'
  }
  $ToolInstaller = Join-Path $Checkout 'tools\install-policy-tools.ps1'
  $ReleaseInstaller = Join-Path $Checkout 'scripts\install.ps1'
  if (-not (Test-Path -LiteralPath $ToolInstaller -PathType Leaf)) {
    Fail 'the source checkout has no policy-tool installer'
  }
  if (-not (Test-Path -LiteralPath $ReleaseInstaller -PathType Leaf)) {
    Fail 'the source checkout has no release installer'
  }
  & $ToolInstaller
  if ($LASTEXITCODE -ne 0) {
    Fail 'policy-tool installation failed'
  }
  & $ReleaseInstaller -Prefix $InstallPrefix -RequireRepository $RepositoryRoot
  if ($LASTEXITCODE -ne 0) {
    Fail 'Code Polishy installation failed'
  }
} finally {
  if (Test-Path -LiteralPath $Scratch) {
    Remove-Item -LiteralPath $Scratch -Recurse -Force
  }
}

if (-not (Test-InstalledRelease)) {
  Fail 'installation completed without making the exact locked release available'
}
Write-Output "Code Polishy $Version is ready for this repository."
