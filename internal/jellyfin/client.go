// Package jellyfin fournit un client REST pour interagir avec l'API Jellyfin.
//
// Opérations supportées :
//   - Création d'utilisateur (POST /Users/New)
//   - Suppression d'utilisateur (DELETE /Users/{Id})
//   - Modification de politique (POST /Users/{Id}/Policy)
//   - Application d'un profil de bibliothèques
//   - Récupération de la liste des utilisateurs et bibliothèques
//
// Chaque méthode retourne des erreurs explicites pour permettre le rollback
// lors du flux de création atomique (invitation).
package jellyfin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

// ── Client ──────────────────────────────────────────────────────────────────

// Client encapsule la communication avec l'API REST de Jellyfin.
type Client struct {
	baseURL    string       // URL de base de l'instance Jellyfin (sans trailing slash)
	apiKey     string       // Clé API d'administration
	httpClient *http.Client // Client HTTP avec timeout
}

// New crée un nouveau client Jellyfin à partir de la configuration.
func New(cfg config.JellyfinConfig) *Client {
	url := strings.TrimRight(cfg.URL, "/")
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
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
	Policy                Policy                 `json:"Policy"`
	Configuration         map[string]interface{} `json:"Configuration"`
}

// Policy représente la politique de droits d'un utilisateur Jellyfin.
type Policy struct {
	IsAdministrator                bool     `json:"IsAdministrator"`
	IsDisabled                     bool     `json:"IsDisabled"`
	EnableAllFolders               bool     `json:"EnableAllFolders"`
	EnabledFolders                 []string `json:"EnabledFolders"`
	EnableAllChannels              bool     `json:"EnableAllChannels"`
	EnableMediaPlayback            bool     `json:"EnableMediaPlayback"`
	EnableAudioPlaybackTranscoding bool     `json:"EnableAudioPlaybackTranscoding"`
	EnableVideoPlaybackTranscoding bool     `json:"EnableVideoPlaybackTranscoding"`
	EnableContentDeletion          bool     `json:"EnableContentDeletion"`
	EnableContentDownloading       bool     `json:"EnableContentDownloading"`
	EnableRemoteAccess             bool     `json:"EnableRemoteAccess"`
	EnableLiveTvAccess             bool     `json:"EnableLiveTvAccess"`
	EnableLiveTvManagement         bool     `json:"EnableLiveTvManagement"`
	EnableSharedDeviceControl      bool     `json:"EnableSharedDeviceControl"`
	ForceRemoteSourceTranscoding   bool     `json:"ForceRemoteSourceTranscoding"`
	EnableSyncTranscoding          bool     `json:"EnableSyncTranscoding"`
	InvalidLoginAttemptCount       int      `json:"InvalidLoginAttemptCount"`
	LoginAttemptsBeforeLockout     int      `json:"LoginAttemptsBeforeLockout"`
	MaxActiveSessions              int      `json:"MaxActiveSessions"`
	RemoteClientBitrateLimit       int      `json:"RemoteClientBitrateLimit"`
}

// Library représente une bibliothèque de médias Jellyfin.
type Library struct {
	ID             string `json:"Id"`
	Name           string `json:"Name"`
	CollectionType string `json:"CollectionType"`
}

// LibrariesResponse est la réponse de l'endpoint /Library/VirtualFolders.
type LibrariesResponse []Library

// InviteProfile contient les droits à appliquer lors d'une invitation.
// Stocké en JSON dans la table invitations.jellyfin_profile.
type InviteProfile struct {
	EnableAllFolders   bool     `json:"enable_all_folders"`
	EnabledFolderIDs   []string `json:"enabled_folder_ids"`
	EnableDownload     bool     `json:"enable_download"`
	RequireEmail       bool     `json:"require_email"`
	EnableRemoteAccess bool     `json:"enable_remote_access"`
	MaxSessions        int      `json:"max_sessions"`
	BitrateLimit       int      `json:"bitrate_limit"`    // 0 = illimité
	UserExpiryDays     int      `json:"user_expiry_days"` // 0 = illimité
	UserExpiresAt      string   `json:"user_expires_at"`
	DisableAfterDays   int      `json:"disable_after_days"`
	ExpiryAction       string   `json:"expiry_action"` // disable|delete|disable_then_delete
	DeleteAfterDays    int      `json:"delete_after_days"`
	GroupName          string   `json:"group_name"`
	UsernameMinLength  int      `json:"username_min_length"`
	UsernameMaxLength  int      `json:"username_max_length"`

	PasswordMinLength      int  `json:"password_min_length"`
	PasswordMaxLength      int  `json:"password_max_length"`
	PasswordRequireUpper   bool `json:"password_require_upper"`
	PasswordRequireLower   bool `json:"password_require_lower"`
	PasswordRequireDigit   bool `json:"password_require_digit"`
	PasswordRequireSpecial bool `json:"password_require_special"`

	// JFA-Go Features
	ForcedUsername string `json:"forced_username"`  // Si rempli (Flux B), l'utilisateur n'a pas le choix du nom
	TemplateUserID string `json:"template_user_id"` // Si fourni, clonage strict des droits de ce profil
	CanInvite      bool   `json:"can_invite"`
}

// ── Opérations CRUD ─────────────────────────────────────────────────────────

// CreateUser crée un nouvel utilisateur dans Jellyfin.
//
// Retourne l'utilisateur créé avec son ID Jellyfin.
// En cas d'erreur, le rollback doit supprimer le compte AD correspondant.
func (c *Client) CreateUser(name, password string) (*User, error) {
	reqBody, err := json.Marshal(CreateUserRequest{
		Name:     name,
		Password: password,
	})
	if err != nil {
		return nil, fmt.Errorf("jellyfin.CreateUser: erreur de sérialisation: %w", err)
	}

	resp, err := c.doRequest(http.MethodPost, "/Users/New", reqBody)
	if err != nil {
		return nil, fmt.Errorf("jellyfin.CreateUser: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jellyfin.CreateUser: HTTP %d — %s", resp.StatusCode, string(body))
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("jellyfin.CreateUser: parse error: %w", err)
	}

	slog.Info("Utilisateur créé dans Jellyfin", "name", name, "id", user.ID)
	return &user, nil
}

// DeleteUser supprime un utilisateur de Jellyfin par son ID.
//
// Utilisé lors du rollback en cas d'échec, ou pour la suppression admin.
func (c *Client) DeleteUser(userID string) error {
	if userID == "" {
		return fmt.Errorf("jellyfin.DeleteUser: userID vide")
	}

	resp, err := c.doRequest(http.MethodDelete, fmt.Sprintf("/Users/%s", userID), nil)
	if err != nil {
		return fmt.Errorf("jellyfin.DeleteUser: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jellyfin.DeleteUser: HTTP %d — %s", resp.StatusCode, string(body))
	}

	slog.Info("Utilisateur Jellyfin supprimé", "id", userID)
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
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jellyfin.SetUserPolicy: HTTP %d — %s", resp.StatusCode, string(body))
	}

	slog.Info("Politique Jellyfin mise à jour", "id", userID, "disabled", policy.IsDisabled)
	return nil
}

// EnableUser active un utilisateur en mettant IsDisabled à false.
func (c *Client) EnableUser(userID string) error {
	// Récupérer la politique actuelle pour ne modifier que IsDisabled
	user, err := c.GetUser(userID)
	if err != nil {
		return fmt.Errorf("jellyfin.EnableUser: %w", err)
	}
	user.Policy.IsDisabled = false
	return c.SetUserPolicy(userID, user.Policy)
}

// DisableUser désactive un utilisateur en mettant IsDisabled à true.
func (c *Client) DisableUser(userID string) error {
	user, err := c.GetUser(userID)
	if err != nil {
		return fmt.Errorf("jellyfin.DisableUser: %w", err)
	}
	user.Policy.IsDisabled = true
	return c.SetUserPolicy(userID, user.Policy)
}

// ApplyInviteProfile applique un profil d'invitation à un utilisateur.
//
// Configure les bibliothèques autorisées, le téléchargement,
// l'accès distant, les sessions max et la limite de débit.
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
	policy.IsAdministrator = false // Jamais admin via invitation
	policy.IsDisabled = false
	policy.EnableAllFolders = profile.EnableAllFolders
	policy.EnabledFolders = profile.EnabledFolderIDs
	policy.EnableContentDownloading = profile.EnableDownload
	policy.EnableRemoteAccess = profile.EnableRemoteAccess
	policy.MaxActiveSessions = profile.MaxSessions
	policy.RemoteClientBitrateLimit = profile.BitrateLimit

	// Activer les capacités de lecture par défaut
	policy.EnableMediaPlayback = true
	policy.EnableAudioPlaybackTranscoding = true
	policy.EnableVideoPlaybackTranscoding = true

	// Si un profil modèle est défini (Flux JFA-Go avancé), écraser avec la politique du modèle
	if profile.TemplateUserID != "" {
		slog.Debug("Clonage de la politique et configuration depuis le modèle Jellyfin", "template", profile.TemplateUserID, "target", userID)
		templateUser, err := c.GetUser(profile.TemplateUserID)
		if err == nil {
			policy = templateUser.Policy
			policy.IsAdministrator = false // Interdire formellement qu'un modèle leak des droits d'admin

			// Le clonage de 'Configuration' complète du modèle peut se faire ici
			// Ex: AudioLanguagePreference, SubtitleMode, etc.
			reqBody, configErr := json.Marshal(templateUser.Configuration)
			if configErr == nil {
				_, _ = c.doRequest(http.MethodPost, fmt.Sprintf("/Users/%s/Configuration", userID), reqBody)
			}
		} else {
			slog.Warn("Echec du chargement du profil modèle Jellyfin, application des droits restreints standards", "template", profile.TemplateUserID)
		}
	}

	return c.SetUserPolicy(userID, policy)
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
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jellyfin.GetUser: HTTP %d — %s", resp.StatusCode, string(body))
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("jellyfin.GetUser: erreur de décodage: %w", err)
	}

	return &user, nil
}

// GetUsers récupère la liste de tous les utilisateurs Jellyfin.
func (c *Client) GetUsers() ([]User, error) {
	resp, err := c.doRequest(http.MethodGet, "/Users", nil)
	if err != nil {
		return nil, fmt.Errorf("jellyfin.GetUsers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jellyfin.GetUsers: HTTP %d — %s", resp.StatusCode, string(body))
	}

	var users []User
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, fmt.Errorf("jellyfin.GetUsers: erreur de décodage: %w", err)
	}

	return users, nil
}

// GetLibraries récupère la liste des bibliothèques de médias.
func (c *Client) GetLibraries() ([]Library, error) {
	resp, err := c.doRequest(http.MethodGet, "/Library/VirtualFolders", nil)
	if err != nil {
		return nil, fmt.Errorf("jellyfin.GetLibraries: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jellyfin.GetLibraries: HTTP %d — %s", resp.StatusCode, string(body))
	}

	var libs []Library
	if err := json.NewDecoder(resp.Body).Decode(&libs); err != nil {
		return nil, fmt.Errorf("jellyfin.GetLibraries: erreur de décodage: %w", err)
	}

	return libs, nil
}

// ResetPassword réinitialise le mot de passe d'un utilisateur Jellyfin.
//
// Utilisé lors de la récupération de mot de passe (en complément de l'AD).
func (c *Client) ResetPassword(userID, newPassword string) error {
	if userID == "" {
		return fmt.Errorf("jellyfin.ResetPassword: userID vide")
	}

	// Étape 1 : Réinitialiser le mot de passe (le supprime)
	resetBody, _ := json.Marshal(map[string]bool{"ResetPassword": true})
	resp, err := c.doRequest(http.MethodPost, fmt.Sprintf("/Users/%s/Password", userID), resetBody)
	if err != nil {
		return fmt.Errorf("jellyfin.ResetPassword: reset — %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("jellyfin.ResetPassword: reset — HTTP %d", resp.StatusCode)
	}

	// Étape 2 : Définir le nouveau mot de passe
	newPwBody, _ := json.Marshal(map[string]string{
		"CurrentPw": "",
		"NewPw":     newPassword,
	})
	resp2, err := c.doRequest(http.MethodPost, fmt.Sprintf("/Users/%s/Password", userID), newPwBody)
	if err != nil {
		return fmt.Errorf("jellyfin.ResetPassword: set — %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp2.Body)
		return fmt.Errorf("jellyfin.ResetPassword: set — HTTP %d — %s", resp2.StatusCode, string(body))
	}

	slog.Info("Mot de passe Jellyfin réinitialisé", "id", userID)
	return nil
}

// ── Méthode interne ─────────────────────────────────────────────────────────

// doRequest exécute une requête HTTP vers l'API Jellyfin.
// Ajoute automatiquement le header d'authentification API key.
func (c *Client) doRequest(method, path string, body []byte) (*http.Response, error) {
	url := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("erreur de création de la requête %s %s: %w", method, path, err)
	}

	// Authentification par clé API
	req.Header.Set("X-Emby-Token", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erreur de connexion à Jellyfin %s %s: %w", method, path, err)
	}

	return resp, nil
}
