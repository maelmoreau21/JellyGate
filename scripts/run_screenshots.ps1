# Helper to generate a signed admin session cookie and run the Puppeteer screenshots
param(
    [string]$Host = $(throw 'Please provide -Host or set JELLYGATE_URL/JELLYGATE_HOST environment variable')
)

if (-not $Host) {
    if ($env:JELLYGATE_URL) { $Host = $env:JELLYGATE_URL }
    elseif ($env:JELLYGATE_HOST) { $Host = $env:JELLYGATE_HOST }
    else { Write-Error 'No host provided and JELLYGATE_URL / JELLYGATE_HOST not set'; exit 2 }
}

Write-Host "Generating session cookie with go run ./cmd/generate_session..."
$cookie = & go run ./cmd/generate_session 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to generate cookie: $cookie"
    exit $LASTEXITCODE
}
$cookie = $cookie.Trim()
if (-not $cookie) { Write-Error 'Empty cookie received'; exit 3 }

Write-Host "Cookie generated (length $($cookie.Length)). Running screenshots against $Host"
$env:SESSION_COOKIE = $cookie
node ./scripts/screenshots.js --host $Host

if ($LASTEXITCODE -ne 0) { Write-Error 'Screenshots script failed'; exit $LASTEXITCODE }
Write-Host 'Screenshots complete.'
