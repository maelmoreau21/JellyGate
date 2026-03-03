module github.com/maelmoreau21/JellyGate/scripts/yaml

replace github.com/maelmoreau21/JellyGate/common => ../../common

replace github.com/maelmoreau21/JellyGate/logmessages => ../../logmessages

go 1.22.4

require (
	github.com/fatih/color v1.18.0
	github.com/goccy/go-yaml v1.18.0
	github.com/maelmoreau21/JellyGate/common v0.0.0-20251123201034-b1c578ccf49f
)

require (
	github.com/maelmoreau21/JellyGate/logmessages v0.0.0-20240806200606-6308db495a0a // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	golang.org/x/sys v0.25.0 // indirect
)
