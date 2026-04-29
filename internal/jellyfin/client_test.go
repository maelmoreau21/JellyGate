package jellyfin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

func TestApplyInviteProfileAppliesPolicyConfigurationAndDisplayPreferences(t *testing.T) {
	var policyPayload Policy
	var userConfigPayload map[string]interface{}
	var displayPayload map[string]interface{}
	requests := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		requests[key]++

		if r.URL.Path == "/Users/template" {
			t.Fatalf("template user should not be fetched")
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/Users/user":
			_ = json.NewEncoder(w).Encode(User{
				ID:   "user",
				Name: "user",
				Policy: Policy{
					IsAdministrator:          true,
					IsDisabled:               true,
					EnableAllFolders:         true,
					EnableContentDownloading: true,
					EnableRemoteAccess:       true,
				},
				Configuration: map[string]interface{}{
					"AudioLanguagePreference": "fr",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/Users/user/Policy":
			if err := json.NewDecoder(r.Body).Decode(&policyPayload); err != nil {
				t.Fatalf("decode policy payload: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/Users/Configuration":
			if got := r.URL.Query().Get("userId"); got != "user" {
				t.Fatalf("configuration userId = %q, want user", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&userConfigPayload); err != nil {
				t.Fatalf("decode user configuration payload: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/DisplayPreferences/usersettings":
			if got := r.URL.Query().Get("userId"); got != "user" {
				t.Fatalf("display userId = %q, want user", got)
			}
			if got := r.URL.Query().Get("client"); got != "emby" {
				t.Fatalf("display client = %q, want emby", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"Id":          "usersettings",
				"CustomPrefs": map[string]string{"existing": "kept"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/DisplayPreferences/usersettings":
			if err := json.NewDecoder(r.Body).Decode(&displayPayload); err != nil {
				t.Fatalf("decode display payload: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := New(config.JellyfinConfig{URL: server.URL, APIKey: "secret"})
	profile := InviteProfile{
		TemplateUserID:     "template",
		EnableAllFolders:   false,
		EnabledFolderIDs:   []string{"movies", "shows"},
		EnableDownload:     false,
		EnableRemoteAccess: true,
		MaxSessions:        2,
		BitrateLimit:       4000,
		UserConfiguration: config.JellyfinPresetUserConfiguration{
			DisplayMissingEpisodes: true,
			HidePlayedInLatest:     true,
			OrderedViews:           []string{"shows", "movies"},
			GroupedFolders:         []string{"shows"},
			MyMediaExcludes:        []string{"music"},
			LatestItemsExcludes:    []string{"books"},
		},
		DisplayPreferences: config.JellyfinPresetDisplayPreferences{
			ScreenSaver:                    "none",
			ScreensaverTime:                120,
			BackdropScreensaverInterval:    7,
			SlideshowInterval:              9,
			EnableFastFadeIn:               true,
			EnableBlurHash:                 true,
			EnableBackdrops:                true,
			EnableThemeSongs:               true,
			EnableThemeVideos:              false,
			DetailsBanner:                  true,
			LibraryPageSize:                50,
			MaxDaysForNextUp:               30,
			EnableRewatchingInNextUp:       true,
			UseEpisodeImagesInNextUpResume: true,
			HomeSections:                   []string{"resume", "nextup"},
		},
	}

	if err := client.ApplyInviteProfile("user", profile); err != nil {
		t.Fatalf("ApplyInviteProfile() error = %v", err)
	}

	if !policyPayload.EnableMediaPlayback || !policyPayload.EnableAudioPlaybackTranscoding || !policyPayload.EnableVideoPlaybackTranscoding {
		t.Fatalf("playback capabilities should be enabled: %+v", policyPayload)
	}
	if policyPayload.IsAdministrator || policyPayload.IsDisabled {
		t.Fatalf("invited user should not be admin or disabled: %+v", policyPayload)
	}
	if policyPayload.EnableAllFolders {
		t.Fatalf("EnableAllFolders = true, want false")
	}
	if got := policyPayload.EnabledFolders; len(got) != 2 || got[0] != "movies" || got[1] != "shows" {
		t.Fatalf("EnabledFolders = %#v", got)
	}
	if policyPayload.MaxActiveSessions != 2 || policyPayload.RemoteClientBitrateLimit != 4000 {
		t.Fatalf("policy limits not applied: %+v", policyPayload)
	}

	if userConfigPayload["AudioLanguagePreference"] != "fr" {
		t.Fatalf("existing user configuration should be preserved: %#v", userConfigPayload)
	}
	if userConfigPayload["DisplayMissingEpisodes"] != true || userConfigPayload["HidePlayedInLatest"] != true {
		t.Fatalf("user configuration booleans not applied: %#v", userConfigPayload)
	}
	if got := stringSliceFromInterface(userConfigPayload["OrderedViews"]); len(got) != 2 || got[0] != "shows" || got[1] != "movies" {
		t.Fatalf("OrderedViews = %#v", got)
	}

	customPrefs, ok := displayPayload["CustomPrefs"].(map[string]interface{})
	if !ok {
		t.Fatalf("CustomPrefs missing: %#v", displayPayload)
	}
	if customPrefs["existing"] != "kept" {
		t.Fatalf("existing CustomPrefs should be preserved: %#v", customPrefs)
	}
	expectedPrefs := map[string]string{
		"screensaver":                       "none",
		"screensaverTime":                   "120",
		"backdropScreensaverInterval":       "7",
		"slideshowInterval":                 "9",
		"fastFadein":                        "true",
		"enableBackdrops":                   "true",
		"enableThemeSongs":                  "true",
		"enableThemeVideos":                 "false",
		"libraryPageSize":                   "50",
		"maxDaysForNextUp":                  "30",
		"enableRewatchingInNextUp":          "true",
		"useEpisodeImagesInNextUpAndResume": "true",
		"homesection0":                      "resume",
		"homesection1":                      "nextup",
		"homesection2":                      "none",
	}
	for key, want := range expectedPrefs {
		if got := customPrefs[key]; got != want {
			t.Fatalf("CustomPrefs[%s] = %#v, want %q", key, got, want)
		}
	}

	if requests[http.MethodPost+" /Users/user/Policy"] != 1 ||
		requests[http.MethodPost+" /Users/Configuration"] != 1 ||
		requests[http.MethodPost+" /DisplayPreferences/usersettings"] != 1 {
		t.Fatalf("unexpected request counts: %#v", requests)
	}
}

func stringSliceFromInterface(value interface{}) []string {
	raw, ok := value.([]interface{})
	if !ok {
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok {
			values = append(values, value)
		}
	}
	return values
}
