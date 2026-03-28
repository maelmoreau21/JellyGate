# Simple run script
$env_file = ".env.local"
if (Test-Path $env_file) {
    Write-Host "📂 Loading $env_file..."
    Get-Content $env_file | ForEach-Object {
        $line = $_.Trim()
        if ($line -and -not $line.StartsWith("#")) {
            if ($line -match '^([^=]+)=(.*)$') {
                $k = $Matches[1].Trim()
                $v = $Matches[2].Trim()
                # Clean quotes
                $v = $v -replace '^["'']|["'']$', ''
                [System.Environment]::SetEnvironmentVariable($k, $v, "Process")
            }
        }
    }
}
Write-Host "🚀 Starting JellyGate..."
go run ./cmd/jellygate
