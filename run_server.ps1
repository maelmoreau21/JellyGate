$p = 'R!$P&5TYM3-lc2@34ug$'
$env:DB_TYPE='postgres'
$env:DB_HOST='192.168.20.251'
$env:DB_PORT='5433'
$env:DB_NAME='jellygate'
$env:DB_USER='jellygate'
$env:DB_PASSWORD=$p
$env:JELLYGATE_SECRET_KEY='7f8e9a2b5c4d1e3f0a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f'
$env:JELLYFIN_URL='http://192.168.20.251:8096'
$env:JELLYFIN_API_KEY='c15e88102d48449990bdacd25ae22457'
$env:JELLYGATE_BASE_URL='http://192.168.20.251:8097'

Write-Host "🚀 Starting JellyGate with PostgreSQL at 192.168.20.251 (corrected password)..."
go run ./cmd/jellygate
