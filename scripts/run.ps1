$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$env:GOPATH = Join-Path $projectRoot '.tools\gopath'
$env:GOMODCACHE = Join-Path $projectRoot '.tools\gomodcache'
$env:GOCACHE = Join-Path $projectRoot '.tools\gocache'
if (Test-Path (Join-Path $projectRoot '.env')) {
  Get-Content (Join-Path $projectRoot '.env') | Where-Object { $_ -and -not $_.StartsWith('#') } | ForEach-Object {
    $name, $value = $_ -split '=', 2
    [Environment]::SetEnvironmentVariable($name.Trim(), $value.Trim(), 'Process')
  }
}
$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) { $go = 'C:\Program Files\Go\bin\go.exe' }
if (-not (Test-Path -LiteralPath $go)) { throw 'Go is not installed or discoverable.' }
& $go run ./cmd/api
