package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const emailTemplateDefaultsDir = "web/email_templates/defaults"

// EmailTemplateFileKey decrit un modele e-mail stocke sous forme de fichiers.
type EmailTemplateFileKey struct {
	Key string
	Dir string
}

var emailTemplateFileKeys = []EmailTemplateFileKey{
	{Key: "confirmation", Dir: "confirmation"},
	{Key: "email_verification", Dir: "email_verification"},
	{Key: "expiry_reminder", Dir: "expiry_reminder"},
	{Key: "invitation", Dir: "invitation"},
	{Key: "invite_expiry", Dir: "invite_expiry"},
	{Key: "password_reset", Dir: "password_reset"},
	{Key: "user_creation", Dir: "user_creation"},
	{Key: "user_deletion", Dir: "user_deletion"},
	{Key: "user_disabled", Dir: "user_disabled"},
	{Key: "user_enabled", Dir: "user_enabled"},
	{Key: "user_expired", Dir: "user_expired"},
	{Key: "expiry_adjusted", Dir: "expiry_adjusted"},
	{Key: "welcome", Dir: "welcome"},
}

// EmailTemplateDefaultsDir retourne le dossier lisible contenant les defaults.
func EmailTemplateDefaultsDir() string {
	return emailTemplateDefaultsDir
}

// EmailTemplateDefaultsPath resout le dossier des templates en fichiers.
func EmailTemplateDefaultsPath() string {
	if custom := strings.TrimSpace(os.Getenv("JELLYGATE_EMAIL_TEMPLATE_DEFAULTS_DIR")); custom != "" {
		return custom
	}
	candidates := []string{emailTemplateDefaultsDir}
	if _, file, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Join(filepath.Dir(file), "..", "..", emailTemplateDefaultsDir))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return emailTemplateDefaultsDir
}

// EmailTemplateFileKeys retourne l'ordre canonique des fichiers de modeles.
func EmailTemplateFileKeys() []EmailTemplateFileKey {
	keys := make([]EmailTemplateFileKey, len(emailTemplateFileKeys))
	copy(keys, emailTemplateFileKeys)
	return keys
}

func emailTemplateFileKeyByDir(dir string) (EmailTemplateFileKey, bool) {
	dir = strings.TrimSpace(dir)
	for _, key := range emailTemplateFileKeys {
		if key.Dir == dir || key.Key == dir {
			return key, true
		}
	}
	return EmailTemplateFileKey{}, false
}

// EmailTemplateKeyFromDir resout un nom de dossier import/export vers une cle.
func EmailTemplateKeyFromDir(dir string) (string, bool) {
	key, ok := emailTemplateFileKeyByDir(dir)
	return key.Key, ok
}

func EmailTemplateSubjectByKey(cfg EmailTemplatesConfig, key string) (string, bool) {
	switch strings.TrimSpace(key) {
	case "confirmation":
		return cfg.ConfirmationSubject, true
	case "email_verification":
		return cfg.EmailVerificationSubject, true
	case "expiry_reminder":
		return cfg.ExpiryReminderSubject, true
	case "invitation":
		return cfg.InvitationSubject, true
	case "invite_expiry":
		return cfg.InviteExpirySubject, true
	case "password_reset":
		return cfg.PasswordResetSubject, true
	case "user_creation":
		return cfg.UserCreationSubject, true
	case "user_deletion":
		return cfg.UserDeletionSubject, true
	case "user_disabled":
		return cfg.UserDisabledSubject, true
	case "user_enabled":
		return cfg.UserEnabledSubject, true
	case "user_expired":
		return cfg.UserExpiredSubject, true
	case "expiry_adjusted":
		return cfg.ExpiryAdjustedSubject, true
	case "welcome":
		return cfg.WelcomeSubject, true
	default:
		return "", false
	}
}

func EmailTemplateBodyByKey(cfg EmailTemplatesConfig, key string) (string, bool) {
	switch strings.TrimSpace(key) {
	case "confirmation":
		return cfg.Confirmation, true
	case "email_verification":
		return cfg.EmailVerification, true
	case "expiry_reminder":
		return cfg.ExpiryReminder, true
	case "invitation":
		return cfg.Invitation, true
	case "invite_expiry":
		return cfg.InviteExpiry, true
	case "password_reset":
		return cfg.PasswordReset, true
	case "user_creation":
		return cfg.UserCreation, true
	case "user_deletion":
		return cfg.UserDeletion, true
	case "user_disabled":
		return cfg.UserDisabled, true
	case "user_enabled":
		return cfg.UserEnabled, true
	case "user_expired":
		return cfg.UserExpired, true
	case "expiry_adjusted":
		return cfg.ExpiryAdjusted, true
	case "welcome":
		return cfg.Welcome, true
	default:
		return "", false
	}
}

func SetEmailTemplateSubjectByKey(cfg *EmailTemplatesConfig, key, value string) bool {
	if cfg == nil {
		return false
	}
	switch strings.TrimSpace(key) {
	case "confirmation":
		cfg.ConfirmationSubject = value
	case "email_verification":
		cfg.EmailVerificationSubject = value
	case "expiry_reminder":
		cfg.ExpiryReminderSubject = value
	case "invitation":
		cfg.InvitationSubject = value
	case "invite_expiry":
		cfg.InviteExpirySubject = value
	case "password_reset":
		cfg.PasswordResetSubject = value
	case "user_creation":
		cfg.UserCreationSubject = value
	case "user_deletion":
		cfg.UserDeletionSubject = value
	case "user_disabled":
		cfg.UserDisabledSubject = value
	case "user_enabled":
		cfg.UserEnabledSubject = value
	case "user_expired":
		cfg.UserExpiredSubject = value
	case "expiry_adjusted":
		cfg.ExpiryAdjustedSubject = value
	case "welcome":
		cfg.WelcomeSubject = value
	default:
		return false
	}
	return true
}

func SetEmailTemplateBodyByKey(cfg *EmailTemplatesConfig, key, value string) bool {
	if cfg == nil {
		return false
	}
	switch strings.TrimSpace(key) {
	case "confirmation":
		cfg.Confirmation = value
	case "email_verification":
		cfg.EmailVerification = value
	case "expiry_reminder":
		cfg.ExpiryReminder = value
	case "invitation":
		cfg.Invitation = value
	case "invite_expiry":
		cfg.InviteExpiry = value
	case "password_reset":
		cfg.PasswordReset = value
	case "user_creation":
		cfg.UserCreation = value
	case "user_deletion":
		cfg.UserDeletion = value
	case "user_disabled":
		cfg.UserDisabled = value
	case "user_enabled":
		cfg.UserEnabled = value
	case "user_expired":
		cfg.UserExpired = value
	case "expiry_adjusted":
		cfg.ExpiryAdjusted = value
	case "welcome":
		cfg.Welcome = value
	default:
		return false
	}
	return true
}

type emailTemplateMetaFile struct {
	VerifyButtonLabel        string `json:"verify_button_label"`
	ExpiryDateLabel          string `json:"expiry_date_label"`
	ExpiresInLabel           string `json:"expires_in_label"`
	CreateAccountButtonLabel string `json:"create_account_button_label"`
	DirectLinkLabel          string `json:"direct_link_label"`
	ResetPasswordButtonLabel string `json:"reset_password_button_label"`
	CodeLabel                string `json:"code_label"`
	OpenServerButtonLabel    string `json:"open_server_button_label"`
	DirectAccessLabel        string `json:"direct_access_label"`
	PreviewDuration          string `json:"preview_duration"`
	PreviewMessage           string `json:"preview_message"`
	AutomaticFooter          string `json:"automatic_footer"`
	UsefulLinksTitle         string `json:"useful_links_title"`
	JellyseerrLinkLabel      string `json:"jellyseerr_link_label"`
	JellyseerrLinkDesc       string `json:"jellyseerr_link_desc"`
	JellyTrackLinkLabel      string `json:"jellytrack_link_label"`
	JellyTrackLinkDesc       string `json:"jellytrack_link_desc"`
}

var (
	resolvedEmailTextPacksOnce sync.Once
	resolvedEmailTextPacksData map[string]emailTextPack
)

func resolvedEmailTextPacks() map[string]emailTextPack {
	resolvedEmailTextPacksOnce.Do(func() {
		resolvedEmailTextPacksData = loadEmailTextPacksFromFiles(emailTextPacks)
	})
	return resolvedEmailTextPacksData
}

func loadEmailTextPacksFromFiles(fallback map[string]emailTextPack) map[string]emailTextPack {
	packs := make(map[string]emailTextPack, len(fallback))
	for lang, pack := range fallback {
		packs[lang] = pack
	}

	for _, lang := range SupportedLanguageTags() {
		pack := packs[lang]
		langDir := filepath.Join(EmailTemplateDefaultsPath(), lang)
		applyEmailTemplateMetaFile(&pack, filepath.Join(langDir, "_meta.json"))
		for _, key := range emailTemplateFileKeys {
			if subject, ok := readEmailTemplateTextFile(filepath.Join(langDir, key.Dir, "subject.txt")); ok {
				setEmailTextPackSubject(&pack, key.Key, subject)
			}
			if body, ok := readEmailTemplateTextFile(filepath.Join(langDir, key.Dir, "body.txt")); ok {
				setEmailTextPackBody(&pack, key.Key, body)
			}
		}
		packs[lang] = pack
	}
	return packs
}

func readEmailTemplateTextFile(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSpace(text), strings.TrimSpace(text) != ""
}

func applyEmailTemplateMetaFile(pack *emailTextPack, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var meta emailTemplateMetaFile
	if err := json.Unmarshal(raw, &meta); err != nil {
		return
	}
	applyEmailTemplateMeta(pack, meta)
}

func applyEmailTemplateMeta(pack *emailTextPack, meta emailTemplateMetaFile) {
	if strings.TrimSpace(meta.VerifyButtonLabel) != "" {
		pack.VerifyButtonLabel = strings.TrimSpace(meta.VerifyButtonLabel)
	}
	if strings.TrimSpace(meta.ExpiryDateLabel) != "" {
		pack.ExpiryDateLabel = strings.TrimSpace(meta.ExpiryDateLabel)
	}
	if strings.TrimSpace(meta.ExpiresInLabel) != "" {
		pack.ExpiresInLabel = strings.TrimSpace(meta.ExpiresInLabel)
	}
	if strings.TrimSpace(meta.CreateAccountButtonLabel) != "" {
		pack.CreateAccountButtonLabel = strings.TrimSpace(meta.CreateAccountButtonLabel)
	}
	if strings.TrimSpace(meta.DirectLinkLabel) != "" {
		pack.DirectLinkLabel = strings.TrimSpace(meta.DirectLinkLabel)
	}
	if strings.TrimSpace(meta.ResetPasswordButtonLabel) != "" {
		pack.ResetPasswordButtonLabel = strings.TrimSpace(meta.ResetPasswordButtonLabel)
	}
	if strings.TrimSpace(meta.CodeLabel) != "" {
		pack.CodeLabel = strings.TrimSpace(meta.CodeLabel)
	}
	if strings.TrimSpace(meta.OpenServerButtonLabel) != "" {
		pack.OpenServerButtonLabel = strings.TrimSpace(meta.OpenServerButtonLabel)
	}
	if strings.TrimSpace(meta.DirectAccessLabel) != "" {
		pack.DirectAccessLabel = strings.TrimSpace(meta.DirectAccessLabel)
	}
	if strings.TrimSpace(meta.PreviewDuration) != "" {
		pack.PreviewDuration = strings.TrimSpace(meta.PreviewDuration)
	}
	if strings.TrimSpace(meta.PreviewMessage) != "" {
		pack.PreviewMessage = strings.TrimSpace(meta.PreviewMessage)
	}
	if strings.TrimSpace(meta.AutomaticFooter) != "" {
		pack.AutomaticFooter = strings.TrimSpace(meta.AutomaticFooter)
	}
	if strings.TrimSpace(meta.UsefulLinksTitle) != "" {
		pack.UsefulLinksTitle = strings.TrimSpace(meta.UsefulLinksTitle)
	}
	if strings.TrimSpace(meta.JellyseerrLinkLabel) != "" {
		pack.JellyseerrLinkLabel = strings.TrimSpace(meta.JellyseerrLinkLabel)
	}
	if strings.TrimSpace(meta.JellyseerrLinkDesc) != "" {
		pack.JellyseerrLinkDesc = strings.TrimSpace(meta.JellyseerrLinkDesc)
	}
	if strings.TrimSpace(meta.JellyTrackLinkLabel) != "" {
		pack.JellyTrackLinkLabel = strings.TrimSpace(meta.JellyTrackLinkLabel)
	}
	if strings.TrimSpace(meta.JellyTrackLinkDesc) != "" {
		pack.JellyTrackLinkDesc = strings.TrimSpace(meta.JellyTrackLinkDesc)
	}
}

func DefaultEmailTemplateMetaJSONForLanguage(lang string) ([]byte, error) {
	pack := emailTextPackFor(lang)
	meta := emailTemplateMetaFile{
		VerifyButtonLabel:        pack.VerifyButtonLabel,
		ExpiryDateLabel:          pack.ExpiryDateLabel,
		ExpiresInLabel:           pack.ExpiresInLabel,
		CreateAccountButtonLabel: pack.CreateAccountButtonLabel,
		DirectLinkLabel:          pack.DirectLinkLabel,
		ResetPasswordButtonLabel: pack.ResetPasswordButtonLabel,
		CodeLabel:                pack.CodeLabel,
		OpenServerButtonLabel:    pack.OpenServerButtonLabel,
		DirectAccessLabel:        pack.DirectAccessLabel,
		PreviewDuration:          pack.PreviewDuration,
		PreviewMessage:           pack.PreviewMessage,
		AutomaticFooter:          pack.AutomaticFooter,
		UsefulLinksTitle:         pack.UsefulLinksTitle,
		JellyseerrLinkLabel:      pack.JellyseerrLinkLabel,
		JellyseerrLinkDesc:       pack.JellyseerrLinkDesc,
		JellyTrackLinkLabel:      pack.JellyTrackLinkLabel,
		JellyTrackLinkDesc:       pack.JellyTrackLinkDesc,
	}
	return json.MarshalIndent(meta, "", "  ")
}

func setEmailTextPackSubject(pack *emailTextPack, key, value string) {
	switch strings.TrimSpace(key) {
	case "confirmation":
		pack.ConfirmationSubject = value
	case "email_verification":
		pack.EmailVerificationSubject = value
	case "expiry_reminder":
		pack.ExpiryReminderSubject = value
	case "invitation":
		pack.InvitationSubject = value
	case "invite_expiry":
		pack.InviteExpirySubject = value
	case "password_reset":
		pack.PasswordResetSubject = value
	case "user_creation":
		pack.UserCreationSubject = value
	case "user_deletion":
		pack.UserDeletionSubject = value
	case "user_disabled":
		pack.UserDisabledSubject = value
	case "user_enabled":
		pack.UserEnabledSubject = value
	case "user_expired":
		pack.UserExpiredSubject = value
	case "expiry_adjusted":
		pack.ExpiryAdjustedSubject = value
	case "welcome":
		pack.WelcomeSubject = value
	}
}

func setEmailTextPackBody(pack *emailTextPack, key, value string) {
	switch strings.TrimSpace(key) {
	case "confirmation":
		pack.ConfirmationBody = value
	case "email_verification":
		pack.EmailVerificationBody = value
	case "expiry_reminder":
		pack.ExpiryReminderBody = value
	case "invitation":
		pack.InvitationBody = value
	case "invite_expiry":
		pack.InviteExpiryBody = value
	case "password_reset":
		pack.PasswordResetBody = value
	case "user_creation":
		pack.UserCreationBody = value
	case "user_deletion":
		pack.UserDeletionBody = value
	case "user_disabled":
		pack.UserDisabledBody = value
	case "user_enabled":
		pack.UserEnabledBody = value
	case "user_expired":
		pack.UserExpiredBody = value
	case "expiry_adjusted":
		pack.ExpiryAdjustedBody = value
	case "welcome":
		pack.WelcomeBody = value
	}
}
