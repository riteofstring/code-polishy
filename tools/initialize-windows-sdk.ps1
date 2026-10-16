$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
$installations = @(& $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath)
if ($LASTEXITCODE -ne 0 -or $installations.Count -ne 1 -or -not $installations[0]) {
  throw 'A Visual Studio installation with the C++ x64 build tools is required.'
}
$installation = [System.IO.Path]::GetFullPath($installations[0])
$developerShell = Join-Path $installation 'Common7\Tools\Launch-VsDevShell.ps1'
& $developerShell -Arch amd64 -HostArch amd64 -SkipAutomaticLocation
if (-not $env:LIB -or -not $env:INCLUDE) {
  throw 'The Visual Studio developer shell did not initialize the native SDK libraries and headers.'
}
$linker = (Get-Command link.exe -CommandType Application -ErrorAction Stop).Source
$installationPrefix = $installation.TrimEnd('\') + '\'
if (-not $linker.StartsWith($installationPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
  throw "The active linker is outside the selected Visual Studio installation: $linker"
}
Write-Output "Initialized Windows x64 SDK with linker $linker"
