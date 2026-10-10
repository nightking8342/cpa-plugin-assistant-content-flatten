# Build assistant-content-flatten on Windows.
#
# -buildmode=c-shared needs a C compiler; install MinGW-w64 (gcc) and put it on
# PATH first, for example:
#   winget install --id BrechtSanders.WinLibs.POSIX.UCRT
#
# Usage:
#   .\build.ps1
#   .\build.ps1 -Version 0.1.0 -Go 'C:\Go\bin\go.exe'
param(
    [string]$Go = 'go',
    [string]$Version = ''
)

$ErrorActionPreference = 'Stop'

$pluginId = 'assistant-content-flatten'
$goos = (& $Go env GOOS).Trim()
$goarch = (& $Go env GOARCH).Trim()

$ext = switch ($goos) {
    'windows' { '.dll' }
    'darwin' { '.dylib' }
    default { '.so' }
}

$name = if ($Version) { "$pluginId-v$Version" } else { $pluginId }
$outDir = Join-Path 'dist' (Join-Path $goos $goarch)
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

$env:CGO_ENABLED = '1'
& $Go build -trimpath -buildmode=c-shared -ldflags '-s -w' -o (Join-Path $outDir "$name$ext") .
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "built $outDir\$name$ext"
Write-Host "copy it to <cpa>/plugins/$goos/$goarch/ and enable the plugin in config.yaml"
