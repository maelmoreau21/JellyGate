module github.com/maelmoreau21/JellyGate/scripts/ini

replace github.com/maelmoreau21/JellyGate/common => ../../common

replace github.com/maelmoreau21/JellyGate/logmessages => ../../logmessages

go 1.22.4

require (
	github.com/goccy/go-yaml v1.18.0
	github.com/maelmoreau21/JellyGate/common v0.0.0-00010101000000-000000000000
	gopkg.in/ini.v1 v1.67.0
)

require (
	github.com/maelmoreau21/JellyGate/logmessages v0.0.0-20240806200606-6308db495a0a // indirect
	github.com/stretchr/testify v1.11.1 // indirect
)
