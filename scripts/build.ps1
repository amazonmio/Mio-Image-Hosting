$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
Push-Location $taskRoot
try {
  Push-Location web
  try {
    npm.cmd ci
    if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
    npm.cmd test
    if ($LASTEXITCODE -ne 0) { throw 'Frontend tests failed' }
    npm.cmd run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }
  } finally { Pop-Location }
  go test ./...
  if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
  New-Item -ItemType Directory -Force bin | Out-Null
  go build -trimpath -ldflags '-s -w' -o bin/mio-image-hosting.exe .
  if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
  Write-Host 'Built bin/mio-image-hosting.exe. Run it from the project directory.'
} finally { Pop-Location }
