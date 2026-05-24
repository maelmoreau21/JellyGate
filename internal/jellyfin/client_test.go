package jellyfin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

func assertModernJellyfinAuth(t *testing.T, r *http.Request, token string) {
	t.Helper()

	if got := r.Header.Get("X-Emby-Token"); got != "" {
		t.Fatalf("X-Emby-Token should not be sent, got %q", got)
	}
	if got := r.Header.Get("X-MediaBrowser-Token"); got != "" {
		t.Fatalf("X-MediaBrowser-Token should not be sent, got %q", got)
	}
	if got := r.Header.Get("X-Emby-Authorization"); got != "" {
		t.Fatalf("X-Emby-Authorization should not be sent, got %q", got)
	}

	auth := r.Header.Get("Authorization")
	required := []string{
		`MediaBrowser `,
		`Client="JellyGate"`,
		`Device="Server"`,
		`DeviceId="jellygate-server"`,
		`Version="1.4.0"`,
	}
	if token != "" {
		required = append(required, `Token="`+token+`"`)
	} else if strings.Contains(auth, `Token=`) {
		t.Fatalf("Authorization header %q should not include a token", auth)
	}
	for _, part := range required {
		if !strings.Contains(auth, part) {
			t.Fatalf("Authorization header %q missing %q", auth, part)
		}
	}
}

func TestAuthenticateByNameUsesModernHeaderAndRefreshesPolicy(t *testing.T) {
	requests := map[string]int{}
	var authPayload authenticateByNameRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		requests[key]++

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/Users/AuthenticateByName":
			assertModernJellyfinAuth(t, r, "")
			if err := json.NewDecoder(r.Body).Decode(&authPayload); err != nil {
				t.Fatalf("decode auth payload: %v", err)
			}
			_ = json.NewEncoder(w).Encode(authenticateByNameResponse{
				User:        User{ID: "admin-id", Name: "admin"},
				AccessToken: "session-token",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/Users/admin-id":
			assertModernJellyfinAuth(t, r, "session-token")
			_ = json.NewEncoder(w).Encode(User{
				ID:   "admin-id",
				Name: "admin",
				Policy: Policy{
					IsAdministrator: true,
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := New(config.JellyfinConfig{URL: server.URL, APIKey: "admin-api-key"})
	user, err := client.AuthenticateByName("admin", "secret")
	if err != nil {
		t.Fatalf("AuthenticateByName() error = %v", err)
	}
	if authPayload.Username != "admin" || authPayload.Pw != "secret" {
		t.Fatalf("auth payload = %+v, want username and password", authPayload)
	}
	if user.ID != "admin-id" || user.Name != "admin" || !user.Policy.IsAdministrator {
		t.Fatalf("authenticated user = %+v, want refreshed admin policy", user)
	}
	if requests[http.MethodPost+" /Users/AuthenticateByName"] != 1 || requests[http.MethodGet+" /Users/admin-id"] != 1 {
		t.Fatalf("unexpected request counts: %#v", requests)
	}
}

func TestAuthenticateByNameUsesAPIKeyFallbackWhenTokenPolicyRefreshFails(t *testing.T) {
	requests := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path + " " + r.Header.Get("Authorization")
		requests[key]++

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/Users/AuthenticateByName":
			assertModernJellyfinAuth(t, r, "")
			_ = json.NewEncoder(w).Encode(authenticateByNameResponse{
				User:        User{ID: "admin-id", Name: "admin"},
				AccessToken: "session-token",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/Users/admin-id" && strings.Contains(r.Header.Get("Authorization"), `Token="session-token"`):
			assertModernJellyfinAuth(t, r, "session-token")
			http.Error(w, "token cannot read policy", http.StatusForbidden)
		case r.Method == http.MethodGet && r.URL.Path == "/Users/admin-id" && strings.Contains(r.Header.Get("Authorization"), `Token="admin-api-key"`):
			assertModernJellyfinAuth(t, r, "admin-api-key")
			_ = json.NewEncoder(w).Encode(User{
				ID:   "admin-id",
				Name: "admin",
				Policy: Policy{
					IsAdministrator: true,
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := New(config.JellyfinConfig{URL: server.URL, APIKey: "admin-api-key"})
	user, err := client.AuthenticateByName("admin", "secret")
	if err != nil {
		t.Fatalf("AuthenticateByName() error = %v", err)
	}
	if !user.Policy.IsAdministrator {
		t.Fatalf("AuthenticateByName() IsAdministrator = false, want API-key-confirmed admin")
	}
	if len(requests) != 3 {
		t.Fatalf("request count = %d, want 3: %#v", len(requests), requests)
	}
}

func TestAuthenticateByNameAllowsLoginWithoutAdminWhenPolicyRefreshFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/Users/AuthenticateByName":
			assertModernJellyfinAuth(t, r, "")
			_ = json.NewEncoder(w).Encode(authenticateByNameResponse{
				User:        User{ID: "admin-id", Name: "admin", Policy: Policy{IsAdministrator: true}},
				AccessToken: "session-token",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/Users/admin-id":
			assertModernJellyfinAuth(t, r, "session-token")
			http.Error(w, "policy unavailable", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := New(config.JellyfinConfig{URL: server.URL})
	user, err := client.AuthenticateByName("admin", "secret")
	if err != nil {
		t.Fatalf("AuthenticateByName() error = %v", err)
	}
	if user.Policy.IsAdministrator {
		t.Fatalf("AuthenticateByName() granted admin with unconfirmed policy")
	}
}

func TestAuthenticateByNameAllowsLoginWithoutAdminWhenAPIKeyInvalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/Users/AuthenticateByName":
			assertModernJellyfinAuth(t, r, "")
			_ = json.NewEncoder(w).Encode(authenticateByNameResponse{
				User:        User{ID: "admin-id", Name: "admin", Policy: Policy{IsAdministrator: true}},
				AccessToken: "session-token",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/Users/admin-id" && strings.Contains(r.Header.Get("Authorization"), `Token="session-token"`):
			assertModernJellyfinAuth(t, r, "session-token")
			http.Error(w, "token cannot read policy", http.StatusForbidden)
		case r.Method == http.MethodGet && r.URL.Path == "/Users/admin-id" && strings.Contains(r.Header.Get("Authorization"), `Token="bad-api-key"`):
			assertModernJellyfinAuth(t, r, "bad-api-key")
			http.Error(w, "api key invalid", http.StatusUnauthorized)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := New(config.JellyfinConfig{URL: server.URL, APIKey: "bad-api-key"})
	user, err := client.AuthenticateByName("admin", "secret")
	if err != nil {
		t.Fatalf("AuthenticateByName() error = %v", err)
	}
	if user.Policy.IsAdministrator {
		t.Fatalf("AuthenticateByName() granted admin with invalid API-key confirmation")
	}
}

func TestAuthenticateByNameTriesJellyTrackCompatibleVariants(t *testing.T) {
	var lowerPayloadSeen bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/Users/AuthenticateByName":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/Users/authenticatebyname":
			assertModernJellyfinAuth(t, r, "")
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode auth payload: %v", err)
			}
			if payload["username"] != "admin" || payload["password"] != "secret" {
				http.Error(w, "wrong payload shape", http.StatusBadRequest)
				return
			}
			lowerPayloadSeen = true
			_ = json.NewEncoder(w).Encode(authenticateByNameResponse{
				User: User{ID: "admin-id", Name: "admin"},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := New(config.JellyfinConfig{URL: server.URL})
	user, err := client.AuthenticateByName("admin", "secret")
	if err != nil {
		t.Fatalf("AuthenticateByName() error = %v", err)
	}
	if !lowerPayloadSeen {
		t.Fatalf("lowercase compatible payload was not attempted")
	}
	if user.ID != "admin-id" || user.Name != "admin" || user.Policy.IsAdministrator {
		t.Fatalf("authenticated user = %+v, want non-admin fallback user", user)
	}
}

func TestDiagnosticsReportsPublicInfoAndAPIKeyValidity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/System/Info/Public":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"ServerName": "Jellyfin Beta",
				"Version":    "12.0.0-beta",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/System/Info":
			assertModernJellyfinAuth(t, r, "admin-api-key")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"ServerName": "Jellyfin Beta",
				"Version":    "12.0.0-beta",
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := New(config.JellyfinConfig{URL: server.URL, APIKey: "admin-api-key"})
	diag := client.Diagnostics()
	if diag.BaseURL != server.URL || diag.PublicStatus != http.StatusOK || diag.AuthStatus != http.StatusOK {
		t.Fatalf("Diagnostics() = %+v, want successful statuses", diag)
	}
	if !diag.APIKeyValid || diag.Version != "12.0.0-beta" || diag.ServerName != "Jellyfin Beta" {
		t.Fatalf("Diagnostics() = %+v, want server info and valid key", diag)
	}
	if diag.APIKeyFingerprint == "" || strings.Contains(diag.APIKeyFingerprint, "admin-api-key") {
		t.Fatalf("API key fingerprint = %q, want short non-secret fingerprint", diag.APIKeyFingerprint)
	}
}

func TestDiagnosticsReportsInvalidAPIKeyWithoutSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/System/Info/Public":
			_ = json.NewEncoder(w).Encode(map[string]string{"Version": "12.0.0-beta"})
		case r.Method == http.MethodGet && r.URL.Path == "/System/Info":
			http.Error(w, `{"AccessToken":"bad-api-key","Password":"secret"}`, http.StatusUnauthorized)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := New(config.JellyfinConfig{URL: server.URL, APIKey: "bad-api-key"})
	diag := client.Diagnostics()
	if diag.APIKeyValid || diag.AuthStatus != http.StatusUnauthorized {
		t.Fatalf("Diagnostics() = %+v, want invalid API key", diag)
	}
	if strings.Contains(diag.AuthError, "bad-api-key") || strings.Contains(diag.AuthError, "secret") {
		t.Fatalf("AuthError leaked secret: %q", diag.AuthError)
	}
	if !strings.Contains(diag.AuthError, "[REDACTED]") {
		t.Fatalf("AuthError = %q, want redacted detail", diag.AuthError)
	}
}

func TestClientUsesModernAuthorizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertModernJellyfinAuth(t, r, "secret")
		if r.Method != http.MethodGet || r.URL.Path != "/Users" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		_ = json.NewEncoder(w).Encode([]User{})
	}))
	defer server.Close()

	client := New(config.JellyfinConfig{URL: server.URL, APIKey: "secret"})
	if _, err := client.GetUsers(); err != nil {
		t.Fatalf("GetUsers() error = %v", err)
	}
}

func TestSetUserImageUsesModernAuthorizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertModernJellyfinAuth(t, r, "secret")
		if r.Method != http.MethodPost || r.URL.Path != "/Users/user/Images/Primary" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := New(config.JellyfinConfig{URL: server.URL, APIKey: "secret"})
	if err := client.SetUserImage("user", "image/png", []byte("png")); err != nil {
		t.Fatalf("SetUserImage() error = %v", err)
	}
}

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
