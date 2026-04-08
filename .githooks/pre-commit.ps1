#!/usr/bin/env pwsh
# PowerShell pre-commit hook for Windows
try {
  $node = Get-Command node -ErrorAction SilentlyContinue
  if ($null -ne $node) {
    node scripts/prevent_secrets_commit.js
    if ($LASTEXITCODE -ne 0) {
      Write-Error 'Commit blocked by pre-commit hook: secrets found.'
      exit $LASTEXITCODE
    }
  }
} catch {
  # If anything fails, allow the commit (do not block due to hook errors)
}
exit 0
