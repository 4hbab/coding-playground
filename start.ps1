# PyPlayground Startup Script
# Loads .env file and starts the server

if (-Not (Test-Path ".env")) {
    Write-Host "ERROR: .env file not found!" -ForegroundColor Red
    Write-Host ""
    Write-Host "To set up:" -ForegroundColor Yellow
    Write-Host "  1. Copy the example: copy .env.example .env"
    Write-Host "  2. Create a GitHub OAuth App at: https://github.com/settings/developers"
    Write-Host "     - Application name: PyPlayground"
    Write-Host "     - Homepage URL: http://localhost:8080"
    Write-Host "     - Callback URL: http://localhost:8080/auth/github/callback"
    Write-Host "  3. Edit .env and fill in your credentials"
    Write-Host "  4. Run this script again: .\start.ps1"
    exit 1
}

# Load .env file
Get-Content ".env" | ForEach-Object {
    if ($_ -match '^\s*#' -or $_ -match '^\s*$') { return }
    $parts = $_ -split '=', 2
    if ($parts.Length -eq 2) {
        $key = $parts[0].Trim()
        $value = $parts[1].Trim()
        [System.Environment]::SetEnvironmentVariable($key, $value, "Process")
        Write-Host "  Loaded: $key" -ForegroundColor DarkGray
    }
}

Write-Host ""
Write-Host "Starting PyPlayground..." -ForegroundColor Green
Write-Host "  URL: http://localhost:$($env:PORT)" -ForegroundColor Cyan
Write-Host ""

go run ./cmd/server/
