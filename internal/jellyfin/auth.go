package jellyfin

import (
	"net/url"
	"strings"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

const (
	jellygateClientName = "JellyGate"
	jellygateDeviceName = "Server"
	jellygateDeviceID   = "jellygate-server"
)

// AuthorizationHeader builds the modern Jellyfin MediaBrowser Authorization value.
func AuthorizationHeader(token string) string {
	params := []string{
		authParam("Client", jellygateClientName),
		authParam("Device", jellygateDeviceName),
		authParam("DeviceId", jellygateDeviceID),
		authParam("Version", config.AppVersion),
	}
	if token = strings.TrimSpace(token); token != "" {
		params = append(params, authParam("Token", token))
	}
	return "MediaBrowser " + strings.Join(params, ", ")
}

func authParam(key, value string) string {
	return key + `="` + url.PathEscape(value) + `"`
}
