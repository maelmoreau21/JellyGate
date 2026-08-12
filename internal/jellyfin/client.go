// Package jellyfin fournit un client REST pour interagir avec l'API Jellyfin.
//
// Opérations supportées :
//   - Modification de politique de streaming (POST /Users/{Id}/Policy)
//   - Application des profils de bibliothèques et limites de transcodage
//   - Récupération des bibliothèques (/Library/VirtualFolders) et infos serveur
//   - Proxy des avatars utilisateurs
//
// L'identité utilisateur est gérée de manière centralisée par Authentik
// et exposée à Jellyfin via son Outpost LDAP.
package jellyfin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

var (
	ErrNotConfigured = errors.New("jellyfin: client non configuré")
	ErrUnavailable   = errors.New("jellyfin: service indisponible")
)

type IntegrationStatus string

const (
	StatusDisabled    IntegrationStatus = "disabled"
	StatusConfigured  IntegrationStatus = "configured"
	StatusAvailable   IntegrationStatus = "available"
	StatusUnavailable IntegrationStatus = "unavailable"
)

// ── Client ──────────────────────────────────────────────────────────────────

// Client encapsule la communication avec l'API REST de Jellyfin.
type Client struct {
	baseURL    string       // URL de base de l'instance Jellyfin (sans trailing slash)
	apiKey     string       // Clé API d'administration
	httpClient *http.Client // Client HTTP avec timeout
	authMu     sync.RWMutex
	lastAuth   AuthAttemptSummary
}

// New crée un nouveau client Jellyfin à partir de la configuration.
func New(cfg config.JellyfinConfig) *Client {
	url := strings.TrimRight(cfg.URL, "/")
	if url != "" && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}

	return &Client{
		baseURL: url,
		apiKey:  cfg.APIKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// IsConfigured indique si le client Jellyfin dispose d'une URL et d'une clé API valides.
func (c *Client) IsConfigured() bool {
	return c != nil && strings.TrimSpace(c.baseURL) != "" && strings.TrimSpace(c.apiKey) != ""
}

// Status retourne le statut courant de l'intégration Jellyfin.
func (c *Client) Status() IntegrationStatus {
	if !c.IsConfigured() {
		return StatusDisabled
	}
	if _, err := c.GetPublicSystemInfo(); err != nil {
		return StatusUnavailable
	}
	return StatusAvailable
}

// ── Structures de données ───────────────────────────────────────────────────

// CreateUserRequest contient les paramètres pour créer un nouvel utilisateur.
type CreateUserRequest struct {
	Name     string `json:"Name"`
	Password string `json:"Password"`
}

// User représente un utilisateur Jellyfin (réponse API).
type User struct {
	ID                    string                 `json:"Id"`
	Name                  string                 `json:"Name"`
	HasPassword           bool                   `json:"HasPassword"`
	HasConfiguredPassword bool                   `json:"HasConfiguredPassword"`
	PrimaryImageTag       string                 `json:"PrimaryImageTag,omitempty"`
	Policy                Policy                 `json:"Policy"`
	Configuration         map[string]interface{} `json:"Configuration"`
}

// Policy représente la politique de droits d'un utilisateur Jellyfin.
type Policy struct {
	IsAdministrator                  bool             `json:"IsAdministrator"`
	IsDisabled                       bool             `json:"IsDisabled"`
	IsHidden                         bool             `json:"IsHidden"`
	EnableAllFolders                 bool             `json:"EnableAllFolders"`
	EnabledFolders                   []string         `json:"EnabledFolders"`
	BlockedMediaFolders              []string         `json:"BlockedMediaFolders"`
	EnableAllDevices                 bool             `json:"EnableAllDevices"`
	EnabledDevices                   []string         `json:"EnabledDevices"`
	EnableAllChannels                bool             `json:"EnableAllChannels"`
	EnabledChannels                  []string         `json:"EnabledChannels"`
	BlockedChannels                  []string         `json:"BlockedChannels"`
	EnableMediaPlayback              bool             `json:"EnableMediaPlayback"`
	EnableAudioPlaybackTranscoding   bool             `json:"EnableAudioPlaybackTranscoding"`
	EnableVideoPlaybackTranscoding   bool             `json:"EnableVideoPlaybackTranscoding"`
	EnablePlaybackRemuxing           bool             `json:"EnablePlaybackRemuxing"`
	EnableContentDeletion            bool             `json:"EnableContentDeletion"`
	EnableContentDeletionFromFolders []string         `json:"EnableContentDeletionFromFolders"`
	EnableContentDownloading         bool             `json:"EnableContentDownloading"`
	EnablePublicSharing              bool             `json:"EnablePublicSharing"`
	EnableRemoteAccess               bool             `json:"EnableRemoteAccess"`
	EnableLiveTvAccess               bool             `json:"EnableLiveTvAccess"`
	EnableLiveTvManagement           bool             `json:"EnableLiveTvManagement"`
	EnableSharedDeviceControl        bool             `json:"EnableSharedDeviceControl"`
	ForceRemoteSourceTranscoding     bool             `json:"ForceRemoteSourceTranscoding"`
	EnableSyncTranscoding            bool             `json:"EnableSyncTranscoding"`
	EnableMediaConversion            bool             `json:"EnableMediaConversion"`
	SyncPlayAccess                   string           `json:"SyncPlayAccess"`
	AllowedTags                      []string         `json:"AllowedTags"`
	BlockedTags                      []string         `json:"BlockedTags"`
	MaxParentalRating                int              `json:"MaxParentalRating"`
	BlockUnratedItems                []string         `json:"BlockUnratedItems"`
	AccessSchedules                  []AccessSchedule `json:"AccessSchedules"`
	AuthenticationProviderID         string           `json:"AuthenticationProviderId"`
	PasswordResetProviderID          string           `json:"PasswordResetProviderId"`
	InvalidLoginAttemptCount         int              `json:"InvalidLoginAttemptCount"`
	LoginAttemptsBeforeLockout       int              `json:"LoginAttemptsBeforeLockout"`
	MaxActiveSessions                int              `json:"MaxActiveSessions"`
	RemoteClientBitrateLimit         int              `json:"RemoteClientBitrateLimit"`
}

type AccessSchedule struct {
	DayOfWeek string `json:"DayOfWeek"`
	StartHour int    `json:"StartHour"`
	EndHour   int    `json:"EndHour"`
}

// Library représente une bibliothèque de médias Jellyfin.
type Library struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	CollectionType string `json:"collection_type"`
}

// LibrariesResponse est la réponse de l'endpoint /Library/VirtualFolders.
type LibrariesResponse []Library

func (l *Library) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID             string `json:"Id"`
		ItemID         string `json:"ItemId"`
		Name           string `json:"Name"`
		CollectionType string `json:"CollectionType"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	l.ID = strings.TrimSpace(raw.ItemID)
	if l.ID == "" {
		l.ID = strings.TrimSpace(raw.ID)
	}
	l.Name = raw.Name
	l.CollectionType = raw.CollectionType
	return nil
}

// InviteProfile contient les droits à appliquer lors d'une invitation.
// Stocké en JSON dans la table invitations.jellyfin_profile.
type InviteProfile struct {
	IsAdministrator                  bool     `json:"is_administrator"`
	IsHidden                         bool     `json:"is_hidden"`
	IsDisabled                       bool     `json:"is_disabled"`
	EnableAllFolders                 bool     `json:"enable_all_folders"`
	EnabledFolderIDs                 []string `json:"enabled_folder_ids"`
	BlockedMediaFolders              []string `json:"blocked_media_folders"`
	EnableAllDevices                 bool     `json:"enable_all_devices"`
	EnabledDevices                   []string `json:"enabled_devices"`
	EnableAllChannels                bool     `json:"enable_all_channels"`
	EnabledChannels                  []string `json:"enabled_channels"`
	BlockedChannels                  []string `json:"blocked_channels"`
	EnableDownload                   bool     `json:"enable_download"`
	EnableMediaPlayback              bool     `json:"enable_media_playback"`
	EnableAudioPlaybackTranscoding   bool     `json:"enable_audio_playback_transcoding"`
	EnableVideoPlaybackTranscoding   bool     `json:"enable_video_playback_transcoding"`
	EnablePlaybackRemuxing           bool     `json:"enable_playback_remuxing"`
	RequireEmail                     bool     `json:"require_email"`
	RequireEmailVerification         bool     `json:"require_email_verification"`
	EnableRemoteAccess               bool     `json:"enable_remote_access"`
	EnableLiveTvAccess               bool     `json:"enable_live_tv_access"`
	EnableLiveTvManagement           bool     `json:"enable_live_tv_management"`
	EnableSharedDeviceControl        bool     `json:"enable_shared_device_control"`
	EnableContentDeletion            bool     `json:"enable_content_deletion"`
	EnableContentDeletionFromFolders []string `json:"enable_content_deletion_from_folders"`
	EnablePublicSharing              bool     `json:"enable_public_sharing"`
	EnableSyncTranscoding            bool     `json:"enable_sync_transcoding"`
	EnableMediaConversion            bool     `json:"enable_media_conversion"`
	ForceRemoteSourceTranscoding     bool     `json:"force_remote_source_transcoding"`
	SyncPlayAccess                   string   `json:"syncplay_access"`
	InvalidLoginAttemptCount         int      `json:"invalid_login_attempt_count"`
	LoginAttemptsBeforeLockout       int      `json:"login_attempts_before_lockout"`
	MaxSessions                      int      `json:"max_sessions"`
	BitrateLimit                     int      `json:"bitrate_limit"`    // 0 = illimité
	UserExpiryDays                   int      `json:"user_expiry_days"` // 0 = illimité
	UserExpiresAt                    string   `json:"user_expires_at"`
	DisableAfterDays                 int      `json:"disable_after_days"`
	ExpiryAction                     string   `json:"expiry_action"` // disable|delete|disable_then_delete
	DeleteAfterDays                  int      `json:"delete_after_days"`
	GroupName                        string   `json:"group_name"`
	UsernameMinLength                int      `json:"username_min_length"`
	UsernameMaxLength                int      `json:"username_max_length"`

	PasswordMinLength      int  `json:"password_min_length"`
	PasswordMaxLength      int  `json:"password_max_length"`
	PasswordRequireUpper   bool `json:"password_require_upper"`
	PasswordRequireLower   bool `json:"password_require_lower"`
	PasswordRequireDigit   bool `json:"password_require_digit"`
	PasswordRequireSpecial bool `json:"password_require_special"`

	AllowedTags       []string         `json:"allowed_tags"`
	BlockedTags       []string         `json:"blocked_tags"`
	MaxParentalRating int              `json:"max_parental_rating"`
	BlockUnratedItems []string         `json:"block_unrated_items"`
	AccessSchedules   []AccessSchedule `json:"access_schedules"`

	// JFA-Go Features
	ForcedUsername      string `json:"forced_username"`  // Si rempli (Flux B), l'utilisateur n'a pas le choix du nom
	TemplateUserID      string `json:"template_user_id"` // Legacy, conserve pour compatibilite JSON.
	CanInvite           bool   `json:"can_invite"`
	PresetID            string `json:"preset_id"` // Identifiant du preset (Parrainage)
	IsTemporary         bool   `json:"is_temporary"`
	AccountDurationDays int    `json:"account_duration_days"`

	UserConfiguration  config.JellyfinPresetUserConfiguration  `json:"user_configuration"`
	DisplayPreferences config.JellyfinPresetDisplayPreferences `json:"display_preferences"`
}

// ── Opérations CRUD ─────────────────────────────────────────────────────────

// CreateUser crée un nouvel utilisateur dans Jellyfin.
//
// Retourne l'utilisateur créé avec son ID Jellyfin.
// En cas d'erreur, le rollback doit supprimer le compte AD correspondant.
// InviteProfileFromPolicyPreset convertit un profil JellyGate en snapshot
// immuable applicable a Jellyfin.
func InviteProfileFromPolicyPreset(preset *config.JellyfinPolicyPreset) InviteProfile {
	if preset == nil {
		return InviteProfile{}
	}

	accountDurationDays := preset.DefaultAccountDurationDays
	if accountDurationDays <= 0 {
		accountDurationDays = preset.DisableAfterDays
	}
	if preset.MaxAccountDurationDays > 0 && (accountDurationDays == 0 || accountDurationDays > preset.MaxAccountDurationDays) {
		accountDurationDays = preset.MaxAccountDurationDays
	}

	return InviteProfile{
		IsAdministrator:                  preset.IsAdministrator,
		IsHidden:                         preset.IsHidden,
		IsDisabled:                       preset.IsDisabled,
		EnableAllFolders:                 preset.EnableAllFolders,
		EnabledFolderIDs:                 append([]string(nil), preset.EnabledFolderIDs...),
		BlockedMediaFolders:              append([]string(nil), preset.BlockedMediaFolders...),
		EnableAllDevices:                 preset.EnableAllDevices,
		EnabledDevices:                   append([]string(nil), preset.EnabledDevices...),
		EnableAllChannels:                preset.EnableAllChannels,
		EnabledChannels:                  append([]string(nil), preset.EnabledChannels...),
		BlockedChannels:                  append([]string(nil), preset.BlockedChannels...),
		EnableDownload:                   preset.EnableDownload,
		EnableMediaPlayback:              preset.EnableMediaPlayback,
		EnableAudioPlaybackTranscoding:   preset.EnableAudioPlaybackTranscoding,
		EnableVideoPlaybackTranscoding:   preset.EnableVideoPlaybackTranscoding,
		EnablePlaybackRemuxing:           preset.EnablePlaybackRemuxing,
		EnableRemoteAccess:               preset.EnableRemoteAccess,
		EnableLiveTvAccess:               preset.EnableLiveTvAccess,
		EnableLiveTvManagement:           preset.EnableLiveTvManagement,
		EnableSharedDeviceControl:        preset.EnableSharedDeviceControl,
		EnableContentDeletion:            preset.EnableContentDeletion,
		EnableContentDeletionFromFolders: append([]string(nil), preset.EnableContentDeletionFromFolders...),
		EnablePublicSharing:              preset.EnablePublicSharing,
		EnableSyncTranscoding:            preset.EnableSyncTranscoding,
		EnableMediaConversion:            preset.EnableMediaConversion,
		ForceRemoteSourceTranscoding:     preset.ForceRemoteSourceTranscoding,
		SyncPlayAccess:                   strings.TrimSpace(preset.SyncPlayAccess),
		InvalidLoginAttemptCount:         preset.InvalidLoginAttemptCount,
		LoginAttemptsBeforeLockout:       preset.LoginAttemptsBeforeLockout,
		MaxSessions:                      preset.MaxSessions,
		BitrateLimit:                     preset.BitrateLimit,
		DisableAfterDays:                 preset.DisableAfterDays,
		UserExpiryDays:                   accountDurationDays,
		ExpiryAction:                     normalizeInviteProfileExpiryAction(preset.ExpiryAction),
		DeleteAfterDays:                  preset.DeleteAfterDays,
		UsernameMinLength:                preset.UsernameMinLength,
		UsernameMaxLength:                preset.UsernameMaxLength,
		PasswordMinLength:                preset.PasswordMinLength,
		PasswordMaxLength:                preset.PasswordMaxLength,
		PasswordRequireUpper:             preset.RequireUpper,
		PasswordRequireLower:             preset.RequireLower,
		PasswordRequireDigit:             preset.RequireDigit,
		PasswordRequireSpecial:           preset.RequireSpecial,
		AllowedTags:                      append([]string(nil), preset.AllowedTags...),
		BlockedTags:                      append([]string(nil), preset.BlockedTags...),
		MaxParentalRating:                preset.MaxParentalRating,
		BlockUnratedItems:                append([]string(nil), preset.BlockUnratedItems...),
		AccessSchedules:                  accessSchedulesFromPreset(preset.AccessSchedules),
		CanInvite:                        preset.CanInvite || preset.CanCreateInvitations,
		PresetID:                         strings.TrimSpace(strings.ToLower(preset.ID)),
		IsTemporary:                      preset.IsTemporary,
		AccountDurationDays:              accountDurationDays,
		UserConfiguration:                preset.UserConfiguration,
		DisplayPreferences:               preset.DisplayPreferences,
	}
}

func accessSchedulesFromPreset(schedules []config.JellyfinPresetAccessSchedule) []AccessSchedule {
	if len(schedules) == 0 {
		return nil
	}
	out := make([]AccessSchedule, 0, len(schedules))
	for _, schedule := range schedules {
		out = append(out, AccessSchedule{
			DayOfWeek: schedule.DayOfWeek,
			StartHour: schedule.StartHour,
			EndHour:   schedule.EndHour,
		})
	}
	return out
}

func normalizeInviteProfileExpiryAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "delete", "disable_then_delete":
		return strings.ToLower(strings.TrimSpace(action))
	default:
		return "disable"
	}
}

// GetUserImage récupère l'image de profil d'un utilisateur.
// Retourne les octets de l'image, le type MIME et une erreur.
func (c *Client) GetUserImage(userID string) ([]byte, string, error) {
	if userID == "" {
		return nil, "", fmt.Errorf("jellyfin.GetUserImage: userID vide")
	}

	path := fmt.Sprintf("/Users/%s/Images/Primary", userID)
	resp, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, "", fmt.Errorf("jellyfin.GetUserImage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, "", fmt.Errorf("jellyfin.GetUserImage: image non trouvée (404)")
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, "", fmt.Errorf("jellyfin.GetUserImage: HTTP %d — %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB max for images
	if err != nil {
		return nil, "", fmt.Errorf("jellyfin.GetUserImage: erreur lecture flux: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/png" // Fallback raisonnable
	}

	return data, contentType, nil
}

// SetUserImage met à jour l'image de profil d'un utilisateur.
func (c *Client) SetUserImage(userID string, contentType string, data []byte) error {
	if userID == "" {
		return fmt.Errorf("jellyfin.SetUserImage: userID vide")
	}

	path := fmt.Sprintf("/Users/%s/Images/Primary", userID)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", AuthorizationHeader(c.apiKey))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("jellyfin.SetUserImage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("jellyfin.SetUserImage: HTTP %d — %s", resp.StatusCode, string(body))
	}

	return nil
}

// SetUserPolicy met à jour la politique de droits d'un utilisateur.
//
// Utilisé pour activer/désactiver un compte ou appliquer des restrictions.
func (c *Client) SetUserPolicy(userID string, policy Policy) error {
	if userID == "" {
		return fmt.Errorf("jellyfin.SetUserPolicy: userID vide")
	}

	reqBody, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("jellyfin.SetUserPolicy: erreur de sérialisation: %w", err)
	}

	resp, err := c.doRequest(http.MethodPost, fmt.Sprintf("/Users/%s/Policy", userID), reqBody)
	if err != nil {
		return fmt.Errorf("jellyfin.SetUserPolicy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("jellyfin.SetUserPolicy: HTTP %d — %s", resp.StatusCode, string(body))
	}

	slog.Info("Politique Jellyfin mise à jour", "id", userID, "disabled", policy.IsDisabled)
	return nil
}

// SetUserConfiguration met a jour la configuration utilisateur Jellyfin.
func (c *Client) SetUserConfiguration(userID string, configuration map[string]interface{}) error {
	if userID == "" {
		return fmt.Errorf("jellyfin.SetUserConfiguration: userID vide")
	}

	reqBody, err := json.Marshal(configuration)
	if err != nil {
		return fmt.Errorf("jellyfin.SetUserConfiguration: erreur de serialisation: %w", err)
	}

	path := "/Users/Configuration?userId=" + url.QueryEscape(userID)
	resp, err := c.doRequest(http.MethodPost, path, reqBody)
	if err != nil {
		return fmt.Errorf("jellyfin.SetUserConfiguration: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("jellyfin.SetUserConfiguration: HTTP %d — %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *Client) getDisplayPreferences(userID string) (map[string]interface{}, error) {
	path := "/DisplayPreferences/usersettings?userId=" + url.QueryEscape(userID) + "&client=emby"
	resp, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("jellyfin.GetDisplayPreferences: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("jellyfin.GetDisplayPreferences: HTTP %d — %s", resp.StatusCode, string(body))
	}

	var preferences map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&preferences); err != nil {
		return nil, fmt.Errorf("jellyfin.GetDisplayPreferences: erreur de decodage: %w", err)
	}
	if preferences == nil {
		preferences = map[string]interface{}{}
	}
	return preferences, nil
}

func (c *Client) setDisplayPreferences(userID string, preferences map[string]interface{}) error {
	reqBody, err := json.Marshal(preferences)
	if err != nil {
		return fmt.Errorf("jellyfin.SetDisplayPreferences: erreur de serialisation: %w", err)
	}

	path := "/DisplayPreferences/usersettings?userId=" + url.QueryEscape(userID) + "&client=emby"
	resp, err := c.doRequest(http.MethodPost, path, reqBody)
	if err != nil {
		return fmt.Errorf("jellyfin.SetDisplayPreferences: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("jellyfin.SetDisplayPreferences: HTTP %d — %s", resp.StatusCode, string(body))
	}

	return nil
}

// buildUserConfigurationPayload merge la configuration existante avec le preset.
func buildUserConfigurationPayload(base map[string]interface{}, cfg config.JellyfinPresetUserConfiguration) map[string]interface{} {
	cfg = config.NormalizeJellyfinPresetUserConfiguration(cfg)
	payload := map[string]interface{}{}
	for key, value := range base {
		payload[key] = value
	}
	payload["DisplayMissingEpisodes"] = cfg.DisplayMissingEpisodes
	payload["HidePlayedInLatest"] = cfg.HidePlayedInLatest
	payload["OrderedViews"] = cfg.OrderedViews
	payload["GroupedFolders"] = cfg.GroupedFolders
	payload["MyMediaExcludes"] = cfg.MyMediaExcludes
	payload["LatestItemsExcludes"] = cfg.LatestItemsExcludes
	return payload
}

func buildDisplayPreferencesCustomPrefs(cfg config.JellyfinPresetDisplayPreferences) map[string]string {
	cfg = config.NormalizeJellyfinPresetDisplayPreferences(cfg)
	values := map[string]string{
		"screensaver":                       cfg.ScreenSaver,
		"backdropScreensaverInterval":       strconv.Itoa(cfg.BackdropScreensaverInterval),
		"slideshowInterval":                 strconv.Itoa(cfg.SlideshowInterval),
		"screensaverTime":                   strconv.Itoa(cfg.ScreensaverTime),
		"fastFadein":                        strconv.FormatBool(cfg.EnableFastFadeIn),
		"blurhash":                          strconv.FormatBool(cfg.EnableBlurHash),
		"enableBackdrops":                   strconv.FormatBool(cfg.EnableBackdrops),
		"enableThemeSongs":                  strconv.FormatBool(cfg.EnableThemeSongs),
		"enableThemeVideos":                 strconv.FormatBool(cfg.EnableThemeVideos),
		"detailsBanner":                     strconv.FormatBool(cfg.DetailsBanner),
		"libraryPageSize":                   strconv.Itoa(cfg.LibraryPageSize),
		"maxDaysForNextUp":                  strconv.Itoa(cfg.MaxDaysForNextUp),
		"enableRewatchingInNextUp":          strconv.FormatBool(cfg.EnableRewatchingInNextUp),
		"useEpisodeImagesInNextUpAndResume": strconv.FormatBool(cfg.UseEpisodeImagesInNextUpResume),
	}
	for idx, section := range cfg.HomeSections {
		if idx >= 10 {
			break
		}
		values[fmt.Sprintf("homesection%d", idx)] = section
	}
	return values
}

func (c *Client) applyDisplayPreferences(userID string, cfg config.JellyfinPresetDisplayPreferences) error {
	preferences, err := c.getDisplayPreferences(userID)
	if err != nil {
		return err
	}
	preferences["Id"] = "usersettings"
	preferences["Client"] = "emby"

	customPrefs, ok := preferences["CustomPrefs"].(map[string]interface{})
	if !ok || customPrefs == nil {
		customPrefs = map[string]interface{}{}
	}
	for key, value := range buildDisplayPreferencesCustomPrefs(cfg) {
		customPrefs[key] = value
	}
	preferences["CustomPrefs"] = customPrefs

	return c.setDisplayPreferences(userID, preferences)
}

func (c *Client) ApplyInviteProfile(userID string, profile InviteProfile) error {
	if userID == "" {
		return fmt.Errorf("jellyfin.ApplyInviteProfile: userID vide")
	}

	// Récupérer la politique actuelle comme base
	user, err := c.GetUser(userID)
	if err != nil {
		return fmt.Errorf("jellyfin.ApplyInviteProfile: %w", err)
	}

	// Appliquer les paramètres du profil d'invitation
	policy := user.Policy
	policy.IsAdministrator = profile.IsAdministrator
	policy.IsDisabled = profile.IsDisabled
	policy.IsHidden = profile.IsHidden
	policy.EnableAllFolders = profile.EnableAllFolders
	policy.EnabledFolders = profile.EnabledFolderIDs
	policy.BlockedMediaFolders = profile.BlockedMediaFolders
	policy.EnableAllDevices = profile.EnableAllDevices
	policy.EnabledDevices = profile.EnabledDevices
	policy.EnableAllChannels = profile.EnableAllChannels
	policy.EnabledChannels = profile.EnabledChannels
	policy.BlockedChannels = profile.BlockedChannels
	policy.EnableContentDownloading = profile.EnableDownload
	policy.EnableMediaPlayback = profile.EnableMediaPlayback
	policy.EnableAudioPlaybackTranscoding = profile.EnableAudioPlaybackTranscoding
	policy.EnableVideoPlaybackTranscoding = profile.EnableVideoPlaybackTranscoding
	policy.EnablePlaybackRemuxing = profile.EnablePlaybackRemuxing
	policy.EnableRemoteAccess = profile.EnableRemoteAccess
	policy.EnableLiveTvAccess = profile.EnableLiveTvAccess
	policy.EnableLiveTvManagement = profile.EnableLiveTvManagement
	policy.EnableSharedDeviceControl = profile.EnableSharedDeviceControl
	policy.EnableContentDeletion = profile.EnableContentDeletion
	policy.EnableContentDeletionFromFolders = profile.EnableContentDeletionFromFolders
	policy.EnablePublicSharing = profile.EnablePublicSharing
	policy.EnableSyncTranscoding = profile.EnableSyncTranscoding
	policy.EnableMediaConversion = profile.EnableMediaConversion
	policy.ForceRemoteSourceTranscoding = profile.ForceRemoteSourceTranscoding
	policy.SyncPlayAccess = strings.TrimSpace(profile.SyncPlayAccess)
	policy.InvalidLoginAttemptCount = profile.InvalidLoginAttemptCount
	policy.LoginAttemptsBeforeLockout = profile.LoginAttemptsBeforeLockout
	policy.MaxActiveSessions = profile.MaxSessions
	policy.RemoteClientBitrateLimit = profile.BitrateLimit
	policy.AllowedTags = profile.AllowedTags
	policy.BlockedTags = profile.BlockedTags
	policy.MaxParentalRating = profile.MaxParentalRating
	policy.BlockUnratedItems = profile.BlockUnratedItems
	policy.AccessSchedules = profile.AccessSchedules

	// Activer les capacités de lecture par défaut
	if !policy.EnableMediaPlayback &&
		!policy.EnableAudioPlaybackTranscoding &&
		!policy.EnableVideoPlaybackTranscoding &&
		!policy.EnablePlaybackRemuxing {
		policy.EnableMediaPlayback = true
		policy.EnableAudioPlaybackTranscoding = true
		policy.EnableVideoPlaybackTranscoding = true
		policy.EnablePlaybackRemuxing = true
	}

	if err := c.SetUserPolicy(userID, policy); err != nil {
		return err
	}

	configuration := buildUserConfigurationPayload(user.Configuration, profile.UserConfiguration)
	if err := c.SetUserConfiguration(userID, configuration); err != nil {
		return err
	}

	if err := c.applyDisplayPreferences(userID, profile.DisplayPreferences); err != nil {
		return err
	}

	return nil
}

// ── Lecture ──────────────────────────────────────────────────────────────────

// GetUser récupère les informations d'un utilisateur par son ID.
func (c *Client) GetUser(userID string) (*User, error) {
	if userID == "" {
		return nil, fmt.Errorf("jellyfin.GetUser: userID vide")
	}

	resp, err := c.doRequest(http.MethodGet, fmt.Sprintf("/Users/%s", userID), nil)
	if err != nil {
		return nil, fmt.Errorf("jellyfin.GetUser: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("jellyfin.GetUser: HTTP %d — %s", resp.StatusCode, string(body))
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("jellyfin.GetUser: erreur de décodage: %w", err)
	}

	return &user, nil
}

// GetLibraries récupère la liste des bibliothèques de médias.
func (c *Client) GetLibraries() ([]Library, error) {
	resp, err := c.doRequest(http.MethodGet, "/Library/VirtualFolders", nil)
	if err != nil {
		return nil, fmt.Errorf("jellyfin.GetLibraries: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("jellyfin.GetLibraries: HTTP %d — %s", resp.StatusCode, string(body))
	}

	var libs []Library
	if err := json.NewDecoder(resp.Body).Decode(&libs); err != nil {
		return nil, fmt.Errorf("jellyfin.GetLibraries: erreur de décodage: %w", err)
	}

	return libs, nil
}

// GetPublicSystemInfo récupère les informations publiques du serveur (pour health check).
func (c *Client) GetPublicSystemInfo() (map[string]interface{}, error) {
	resp, err := c.doRequest(http.MethodGet, "/System/Info/Public", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var info map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return info, nil
}

// GetSystemInfo récupère les informations détaillées du serveur (authentifié).
func (c *Client) GetSystemInfo() (map[string]interface{}, error) {
	resp, err := c.doRequest(http.MethodGet, "/System/Info", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var info map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return info, nil
}

func readHTTPDetail(r io.Reader) string {
	if r == nil {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(r, 2048))
	return strings.TrimSpace(string(b))
}

// ── Méthode interne ─────────────────────────────────────────────────────────

// doRequest exécute une requête HTTP vers l'API Jellyfin.
// Ajoute automatiquement le header d'authentification API key.
func (c *Client) doRequest(method, path string, body []byte) (*http.Response, error) {
	if !c.IsConfigured() {
		return nil, ErrNotConfigured
	}
	url := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("erreur de création de la requête %s %s: %w", method, path, err)
	}

	req.Header.Set("Authorization", AuthorizationHeader(c.apiKey))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erreur de connexion à Jellyfin %s %s: %w", method, path, err)
	}

	return resp, nil
}
