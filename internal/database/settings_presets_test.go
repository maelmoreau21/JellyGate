package database

import (
	"encoding/json"
	"testing"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

func newPresetTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(config.DatabaseConfig{Type: "sqlite"}, t.TempDir(), "test-secret-key-0123456789012345")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func TestGetJellyfinPolicyPresetsBackfillsDisplayDefaults(t *testing.T) {
	db := newPresetTestDB(t)
	raw := `[{"id":"legacy","name":"Legacy","enable_all_folders":true,"enable_download":true,"enable_remote_access":true}]`
	if err := db.SetSetting(SettingJellyfinPresets, raw); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}

	presets, err := db.GetJellyfinPolicyPresets()
	if err != nil {
		t.Fatalf("GetJellyfinPolicyPresets() error = %v", err)
	}
	if len(presets) != 1 {
		t.Fatalf("len(presets) = %d, want 1", len(presets))
	}

	display := presets[0].DisplayPreferences
	if display.ScreenSaver != "none" || display.ScreensaverTime != 180 {
		t.Fatalf("display defaults not backfilled: %+v", display)
	}
	if !display.EnableFastFadeIn || !display.EnableBlurHash || !display.DetailsBanner || !display.UseEpisodeImagesInNextUpResume {
		t.Fatalf("expected Jellyfin Web boolean defaults: %+v", display)
	}
	if got := display.HomeSections; len(got) != 10 || got[0] != "smalllibrarytiles" || got[6] != "latestmedia" || got[9] != "none" {
		t.Fatalf("home section defaults = %#v", got)
	}
}

func TestSaveJellyfinPolicyPresetsNormalizesNewBlocks(t *testing.T) {
	db := newPresetTestDB(t)
	preset := config.JellyfinPolicyPreset{
		ID:                 "bad",
		Name:               "Bad",
		EnableAllFolders:   false,
		EnabledFolderIDs:   []string{"movies", "", "movies", "shows"},
		EnableDownload:     true,
		EnableRemoteAccess: true,
		MaxSessions:        -3,
		BitrateLimit:       -1,
		DisplayPreferences: config.JellyfinPresetDisplayPreferences{
			ScreenSaver:                 " ",
			ScreensaverTime:             -1,
			BackdropScreensaverInterval: -1,
			SlideshowInterval:           -1,
			LibraryPageSize:             -1,
			MaxDaysForNextUp:            -1,
			HomeSections:                []string{"resume", "bogus"},
		},
		UserConfiguration: config.JellyfinPresetUserConfiguration{
			OrderedViews:        []string{"shows", "", "shows", "movies"},
			GroupedFolders:      []string{"shows", "shows"},
			MyMediaExcludes:     []string{"music", ""},
			LatestItemsExcludes: []string{"books", "books"},
		},
	}

	if err := db.SaveJellyfinPolicyPresets([]config.JellyfinPolicyPreset{preset}); err != nil {
		t.Fatalf("SaveJellyfinPolicyPresets() error = %v", err)
	}

	raw, err := db.GetSetting(SettingJellyfinPresets)
	if err != nil {
		t.Fatalf("GetSetting() error = %v", err)
	}
	var saved []config.JellyfinPolicyPreset
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatalf("json.Unmarshal(saved) error = %v", err)
	}
	got := saved[0]
	if got.MaxSessions != 0 || got.BitrateLimit != 0 {
		t.Fatalf("negative policy values not normalized: %+v", got)
	}
	if got.EnabledFolderIDs == nil || len(got.EnabledFolderIDs) != 2 || got.EnabledFolderIDs[0] != "movies" || got.EnabledFolderIDs[1] != "shows" {
		t.Fatalf("EnabledFolderIDs = %#v", got.EnabledFolderIDs)
	}
	if got.DisplayPreferences.ScreenSaver != "none" || got.DisplayPreferences.LibraryPageSize != 100 || got.DisplayPreferences.MaxDaysForNextUp != 365 {
		t.Fatalf("display numeric defaults not normalized: %+v", got.DisplayPreferences)
	}
	if sections := got.DisplayPreferences.HomeSections; len(sections) != 10 || sections[0] != "resume" || sections[1] != "none" {
		t.Fatalf("HomeSections = %#v", sections)
	}
	if got.UserConfiguration.OrderedViews == nil || len(got.UserConfiguration.OrderedViews) != 2 {
		t.Fatalf("OrderedViews not cleaned: %#v", got.UserConfiguration.OrderedViews)
	}
	if got.UserConfiguration.LatestItemsExcludes == nil || len(got.UserConfiguration.LatestItemsExcludes) != 1 {
		t.Fatalf("LatestItemsExcludes not cleaned: %#v", got.UserConfiguration.LatestItemsExcludes)
	}
}
