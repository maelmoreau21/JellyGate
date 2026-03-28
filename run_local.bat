@echo off
set DB_TYPE=postgres
set DB_HOST=192.168.20.251
set DB_PORT=5433
set DB_NAME=jellygate
set DB_USER=jellygate
set DB_PASSWORD=R!$P^&5TYM3-lc2@34ug$
set DB_SSLMODE=disable
set JELLYGATE_SECRET_KEY=7f8e9a2b5c4d1e3f0a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f
set JELLYFIN_URL=http://192.168.20.251:8096
set JELLYFIN_API_KEY=c15e88102d48449990bdacd25ae22457
set JELLYGATE_BASE_URL=http://192.168.20.251:8097
set JELLYGATE_PORT=8097
set JELLYGATE_DATA_DIR=./data

echo 🚀 Starting JellyGate...
go run ./cmd/jellygate
