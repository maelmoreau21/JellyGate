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
	if len(presets) < 2 {
		t.Fatalf("len(presets) = %d, want legacy plus defaults", len(presets))
	}

	var legacy *config.JellyfinPolicyPreset
	for i := range presets {
		if presets[i].ID == "legacy" {
			legacy = &presets[i]
			break
		}
	}
	if legacy == nil {
		t.Fatalf("legacy preset missing from merged presets: %#v", presets)
	}

	display := legacy.DisplayPreferences
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

func TestGetJellyfinPolicyPresetsMergesDefaultsWithoutOverwriting(t *testing.T) {
	db := newPresetTestDB(t)
	raw := `[{"id":"admin","name":"Admin custom","description":"keep me","enable_all_folders":false,"enable_download":false,"enable_remote_access":true}]`
	if err := db.SetSetting(SettingJellyfinPresets, raw); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}

	presets, err := db.GetJellyfinPolicyPresets()
	if err != nil {
		t.Fatalf("GetJellyfinPolicyPresets() error = %v", err)
	}
	byID := map[string]config.JellyfinPolicyPreset{}
	for _, preset := range presets {
		byID[preset.ID] = preset
	}
	if got := byID["admin"]; got.Name != "Admin custom" || got.IsAdministrator {
		t.Fatalf("existing admin preset overwritten: %+v", got)
	}
	for _, id := range []string{"family", "temporary_guest", "sponsor", "standard", "limited"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("default preset %q missing from merged presets", id)
		}
	}
}

func TestSecurityEventsMigrationAndInsert(t *testing.T) {
	db := newPresetTestDB(t)
	if err := db.LogSecurityEvent(SecurityEvent{
		Category:  "captcha",
		EventType: "invite.captcha.failed",
		Severity:  "warning",
		Actor:     "guest",
		IP:        "203.0.113.10",
		Message:   "captcha failed",
	}); err != nil {
		t.Fatalf("LogSecurityEvent() error = %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(1) FROM security_events WHERE category = ? AND severity = ?`, "captcha", "warning").Scan(&count); err != nil {
		t.Fatalf("query security_events: %v", err)
	}
	if count != 1 {
		t.Fatalf("security_events count = %d, want 1", count)
	}
}

func TestSaveJellyfinPolicyPresetsNormalizesNewBlocks(t *testing.T) {
	db := newPresetTestDB(t)
	preset := config.JellyfinPolicyPreset{
		ID:                        "bad",
		Name:                      "Bad",
		EnableAllFolders:          false,
		EnabledFolderIDs:          []string{"movies", "", "movies", "shows"},
		BlockedMediaFolders:       []string{"secret", "secret", ""},
		EnableDownload:            true,
		EnableRemoteAccess:        true,
		AllowedTargetPresetIDs:    []string{"family", "", "family", "temporary_guest"},
		AllowedTemporaryPresetIDs: []string{"temporary_guest", "temporary_guest"},
		AccessSchedules: []config.JellyfinPresetAccessSchedule{
			{DayOfWeek: "Monday", StartHour: 8, EndHour: 20},
			{DayOfWeek: "", StartHour: -1, EndHour: 40},
		},
		IsTemporary:                true,
		DefaultAccountDurationDays: 14,
		MaxAccountDurationDays:     30,
		MaxSessions:                -3,
		BitrateLimit:               -1,
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
	if got.BlockedMediaFolders == nil || len(got.BlockedMediaFolders) != 1 || got.BlockedMediaFolders[0] != "secret" {
		t.Fatalf("BlockedMediaFolders = %#v", got.BlockedMediaFolders)
	}
	if got.AllowedTargetPresetIDs == nil || len(got.AllowedTargetPresetIDs) != 2 {
		t.Fatalf("AllowedTargetPresetIDs not normalized: %#v", got.AllowedTargetPresetIDs)
	}
	if got.AllowedTemporaryPresetIDs == nil || len(got.AllowedTemporaryPresetIDs) != 1 {
		t.Fatalf("AllowedTemporaryPresetIDs not normalized: %#v", got.AllowedTemporaryPresetIDs)
	}
	if len(got.AccessSchedules) != 1 || got.AccessSchedules[0].DayOfWeek != "Monday" || got.AccessSchedules[0].StartHour != 8 || got.AccessSchedules[0].EndHour != 20 {
		t.Fatalf("AccessSchedules not normalized: %#v", got.AccessSchedules)
	}
	if !got.IsTemporary || got.DefaultAccountDurationDays != 14 || got.MaxAccountDurationDays != 30 {
		t.Fatalf("temporary lifecycle not preserved: %+v", got)
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

func TestGroupPolicyMappingsSortedByPriority(t *testing.T) {
	db := newPresetTestDB(t)
	if err := db.SaveGroupPolicyMappings([]config.GroupPolicyMapping{
		{GroupName: "media", Source: "ldap", LDAPGroupDN: "cn=low", PolicyPresetID: "limited", Priority: 1},
		{GroupName: "media", Source: "ldap", LDAPGroupDN: "cn=high", PolicyPresetID: "admin", Priority: 50},
	}); err != nil {
		t.Fatalf("SaveGroupPolicyMappings() error = %v", err)
	}
	mappings, err := db.GetGroupPolicyMappings()
	if err != nil {
		t.Fatalf("GetGroupPolicyMappings() error = %v", err)
	}
	if len(mappings) < 2 || mappings[0].Priority != 50 || mappings[0].PolicyPresetID != "admin" {
		t.Fatalf("mappings not sorted by priority: %#v", mappings)
	}
}

func TestSeedDefaultTasksDoesNotCreateBackupAutomationDuplicate(t *testing.T) {
	db := newPresetTestDB(t)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM scheduled_tasks WHERE task_type = ?`, "create_backup").Scan(&count); err != nil {
		t.Fatalf("count create_backup tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("create_backup default task count = %d, want 0", count)
	}
}

func TestDisableDefaultBackupAutomationTaskOnceOnlyTouchesSystemDuplicate(t *testing.T) {
	db := newPresetTestDB(t)
	if _, err := db.Exec(`DELETE FROM settings WHERE key = ?`, SettingDefaultBackupTaskCleanupV1); err != nil {
		t.Fatalf("delete cleanup flag: %v", err)
	}
	now := "2026-04-30 12:00:00"
	if _, err := db.Exec(
		`INSERT INTO scheduled_tasks (name, task_type, enabled, hour, minute, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"Sauvegarde Automatique", "create_backup", true, 5, 0, "system", now, now,
	); err != nil {
		t.Fatalf("insert system duplicate: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO scheduled_tasks (name, task_type, enabled, hour, minute, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"Sauvegarde personnelle", "create_backup", true, 6, 0, "admin", now, now,
	); err != nil {
		t.Fatalf("insert user task: %v", err)
	}

	if err := db.disableDefaultBackupAutomationTaskOnce(); err != nil {
		t.Fatalf("disableDefaultBackupAutomationTaskOnce() error = %v", err)
	}

	var systemEnabled, userEnabled int
	if err := db.QueryRow(`SELECT enabled FROM scheduled_tasks WHERE name = ? AND created_by = ?`, "Sauvegarde Automatique", "system").Scan(&systemEnabled); err != nil {
		t.Fatalf("read system duplicate: %v", err)
	}
	if err := db.QueryRow(`SELECT enabled FROM scheduled_tasks WHERE name = ? AND created_by = ?`, "Sauvegarde personnelle", "admin").Scan(&userEnabled); err != nil {
		t.Fatalf("read user task: %v", err)
	}
	if systemEnabled != 0 {
		t.Fatalf("system default backup task should be disabled")
	}
	if userEnabled == 0 {
		t.Fatalf("user-created backup task should stay enabled")
	}
}
