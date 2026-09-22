$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$env:GOPATH = Join-Path $projectRoot '.tools\gopath'
$env:GOMODCACHE = Join-Path $projectRoot '.tools\gomodcache'
$env:GOCACHE = Join-Path $projectRoot '.tools\gocache'
$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) { $go = 'C:\Program Files\Go\bin\go.exe' }
if (-not (Test-Path -LiteralPath $go)) { throw 'Go is not installed or discoverable.' }
& $go test ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& $go vet ./...
exit $LASTEXITCODE
