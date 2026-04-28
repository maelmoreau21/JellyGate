// Package config gère le chargement et la validation de la configuration
// de JellyGate à partir des variables d'environnement.
//
// Seules les variables essentielles au démarrage sont gérées ici :
//   - JELLYGATE_*  : Application (port, URL, data, secret)
//   - JELLYFIN_*   : Connexion à Jellyfin
//
// Les paramètres LDAP, SMTP et Webhooks sont stockés en base SQL
// (table `settings`) et gérés via l'interface d'administration.
package config

import (
	"fmt"
	"html"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// SupportedLanguages contient les langues officiellement supportees par l'UI.
// Les cles sont stockees en lowercase pour faciliter la comparaison.
var SupportedLanguages = map[string]bool{
	"fr":    true,
	"en":    true,
	"de":    true,
	"es":    true,
	"it":    true,
	"nl":    true,
	"pl":    true,
	"pt-br": true,
	"ru":    true,
	"zh":    true,
}

// NormalizeLanguageTag normalise un tag de langue vers un code interne stable.
// Exemples: EN-us -> en, pt_BR -> pt-br, zh-CN -> zh.
func NormalizeLanguageTag(lang string) string {
	normalized := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(lang, "_", "-")))
	if normalized == "" {
		return ""
	}

	if SupportedLanguages[normalized] {
		return normalized
	}

	base := strings.SplitN(normalized, "-", 2)[0]
	if base == "pt" {
		return "pt-br"
	}
	if SupportedLanguages[base] {
		return base
	}

	return normalized
}

// IsSupportedLanguage indique si la langue est supportee apres normalisation.
func IsSupportedLanguage(lang string) bool {
	return SupportedLanguages[NormalizeLanguageTag(lang)]
}

// Config contient la configuration chargée depuis les variables d'environnement.
// Ne contient que les paramètres essentiels au démarrage de l'application.
type Config struct {
	// Application
	Port              int    // Port d'écoute HTTP (défaut: 8097)
	BaseURL           string // URL de base publique
	DataDir           string // Répertoire des données (SQLite, etc.)
	SecretKey         string // Clé secrète pour sessions/tokens (min 32 chars)
	TLSCert           string // Chemin vers le certificat TLS
	TLSKey            string // Chemin vers la clé privée TLS
	DefaultLang       string // Langue par défaut de l'interface (défaut: fr)
	EnableDebugRoutes bool   // Active les routes /admin/debug (dev uniquement)

	// Base de donnees (sqlite ou postgres)
	Database DatabaseConfig

	// Jellyfin (seul service externe requis au démarrage)
	Jellyfin JellyfinConfig

	// Intégrations tierces optionnelles (provisionnement compte)
	ThirdParty ThirdPartyConfig
}

// JellyfinConfig contient les paramètres de connexion à Jellyfin.
type JellyfinConfig struct {
	URL    string // URL de l'instance Jellyfin (ex: http://jellyfin:8096)
	APIKey string // Clé API d'administration
}

// ThirdPartyConfig contient les paramètres optionnels pour Jellyseerr et JellyTrack.
type ThirdPartyConfig struct {
	JellyseerrURL    string
	JellyseerrAPIKey string
	JellyTrackURL    string
	JellyTrackAPIKey string
}

// DatabaseConfig contient la configuration de la base SQL principale.
type DatabaseConfig struct {
	Type     string
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

// ── Types de configuration stockés en base (table settings) ─────────────────
// Ces structs sont utilisées par database/settings.go et handlers/settings.go
// pour sérialiser/désérialiser les paramètres depuis SQLite.

// LDAPConfig contient les paramètres de connexion annuaire (LDAP/LDAPS).
type LDAPConfig struct {
	Enabled              bool   `json:"enabled"`                // Intégration LDAP activée
	Host                 string `json:"host"`                   // Hostname du serveur LDAP
	Port                 int    `json:"port"`                   // Port (défaut: 636 pour LDAPS)
	UseTLS               bool   `json:"use_tls"`                // Utiliser LDAPS (TLS)
	SkipVerify           bool   `json:"skip_verify"`            // Ignorer la vérification du certificat TLS
	BindDN               string `json:"bind_dn"`                // DN de l'utilisateur pour le bind
	BindPassword         string `json:"bind_password"`          // Mot de passe de bind
	BaseDN               string `json:"base_dn"`                // Base DN de recherche
	SearchFilter         string `json:"search_filter"`          // Filtre de recherche LDAP (supporte {username})
	SearchAttributes     string `json:"search_attributes"`      // Attributs de recherche (liste CSV)
	UIDAttribute         string `json:"uid_attribute"`          // Attribut UID LDAP (ex: uid)
	UsernameAttribute    string `json:"username_attribute"`     // Attribut de nom d'utilisateur LDAP
	AdminFilter          string `json:"admin_filter"`           // Filtre administrateur LDAP
	AdminFilterMemberUID bool   `json:"admin_filter_memberuid"` // Active le mode memberUid pour le filtre admin
	UserObjectClass      string `json:"user_object_class"`      // objectClass utilisateur (auto|user|person|posixAccount|...)
	GroupMemberAttr      string `json:"group_member_attr"`      // Attribut membre groupe (auto|member|memberUid|...)
	UserOU               string `json:"user_ou"`                // OU pour la création des utilisateurs
	UserGroup            string `json:"user_group"`             // Legacy: fallback groupe utilisateur

	// Mode de provisioning: "hybrid" (LDAP + Jellyfin) ou "ldap_only".
	ProvisionMode string `json:"provision_mode"`

	// Groupes LDAP cibles pour l'affectation automatique des comptes.
	JellyfinGroup       string `json:"jellyfin_group"`
	InviterGroup        string `json:"inviter_group"`
	AdministratorsGroup string `json:"administrators_group"`

	Domain string `json:"domain"` // Domaine AD (ex: home.lan)
}

// SMTPConfig contient les paramètres d'envoi d'emails.
type SMTPConfig struct {
	Host     string `json:"host"`     // Serveur SMTP
	Port     int    `json:"port"`     // Port SMTP (défaut: 587)
	Username string `json:"username"` // Utilisateur SMTP
	Password string `json:"password"` // Mot de passe SMTP
	From     string `json:"from"`     // Adresse expéditeur
	UseTLS   bool   `json:"use_tls"`  // Utiliser STARTTLS
}

// BackupConfig contient la configuration des sauvegardes automatiques.
type BackupConfig struct {
	Enabled        bool `json:"enabled"`
	Hour           int  `json:"hour"`            // 0-23
	Minute         int  `json:"minute"`          // 0-59
	RetentionCount int  `json:"retention_count"` // Nombre de sauvegardes à conserver
}

// DefaultBackupConfig retourne une configuration backup par défaut.
func DefaultBackupConfig() BackupConfig {
	return BackupConfig{
		Enabled:        false,
		Hour:           3,
		Minute:         0,
		RetentionCount: 7,
	}
}

// EmailTemplatesConfig contient les modèles de courriels personnalisés configurables (JFA-Go).
type EmailTemplatesConfig struct {
	BaseTemplateHeader          string `json:"base_template_header"`
	BaseTemplateFooter          string `json:"base_template_footer"`
	EmailLogoURL                string `json:"email_logo_url"`
	Confirmation                string `json:"confirmation"`
	ConfirmationSubject         string `json:"confirmation_subject"`
	DisableConfirmationEmail    bool   `json:"disable_confirmation_email"`
	EmailVerificationSubject    string `json:"email_verification_subject"`
	EmailVerification           string `json:"email_verification"`
	ExpiryReminder              string `json:"expiry_reminder"`
	ExpiryReminderSubject       string `json:"expiry_reminder_subject"`
	DisableExpiryReminderEmails bool   `json:"disable_expiry_reminder_emails"`
	ExpiryReminderDays          int    `json:"expiry_reminder_days"`
	ExpiryReminder14            string `json:"expiry_reminder_14"`
	ExpiryReminder7             string `json:"expiry_reminder_7"`
	ExpiryReminder1             string `json:"expiry_reminder_1"`
	Invitation                  string `json:"invitation"`
	InvitationSubject           string `json:"invitation_subject"`
	InviteExpiry                string `json:"invite_expiry"`
	InviteExpirySubject         string `json:"invite_expiry_subject"`
	DisableInviteExpiryEmail    bool   `json:"disable_invite_expiry_email"`
	PasswordReset               string `json:"password_reset"`
	PasswordResetSubject        string `json:"password_reset_subject"`
	PreSignupHelp               string `json:"pre_signup_help"`
	DisablePreSignupHelpEmail   bool   `json:"disable_pre_signup_help_email"`
	PostSignupHelp              string `json:"post_signup_help"`
	DisablePostSignupHelpEmail  bool   `json:"disable_post_signup_help_email"`
	UserCreation                string `json:"user_creation"`
	UserCreationSubject         string `json:"user_creation_subject"`
	DisableUserCreationEmail    bool   `json:"disable_user_creation_email"`
	UserDeletion                string `json:"user_deletion"`
	UserDeletionSubject         string `json:"user_deletion_subject"`
	DisableUserDeletionEmail    bool   `json:"disable_user_deletion_email"`
	UserDisabled                string `json:"user_disabled"`
	UserDisabledSubject         string `json:"user_disabled_subject"`
	DisableUserDisabledEmail    bool   `json:"disable_user_disabled_email"`
	UserEnabled                 string `json:"user_enabled"`
	UserEnabledSubject          string `json:"user_enabled_subject"`
	DisableUserEnabledEmail     bool   `json:"disable_user_enabled_email"`
	UserExpired                 string `json:"user_expired"`
	UserExpiredSubject          string `json:"user_expired_subject"`
	DisableUserExpiredEmail     bool   `json:"disable_user_expired_email"`
	ExpiryAdjusted              string `json:"expiry_adjusted"`
	ExpiryAdjustedSubject       string `json:"expiry_adjusted_subject"`
	DisableExpiryAdjustedEmail  bool   `json:"disable_expiry_adjusted_email"`
	Welcome                     string `json:"welcome"`
	WelcomeSubject              string `json:"welcome_subject"`
	DisableWelcomeEmail         bool   `json:"disable_welcome_email"`
}

var emailTagPattern = regexp.MustCompile(`(?is)<[^>]+>`)
var emailAnchorPattern = regexp.MustCompile(`(?is)<a\b[^>]*href=(?:"([^"]*)"|'([^']*)'|([^\s>]+))[^>]*>(.*?)</a>`)
var plainEmailVariablePattern = regexp.MustCompile(`{{\s*\.[A-Za-z0-9_]+\s*}}`)

const defaultEmailLogoPath = "/static/img/logos/jellygate.svg"

const legacyEmailLogoPath = "/static/img/logos/jellyfin.svg"

const legacyGradientEmailHeader = `
<div style="font-family:Segoe UI,Arial,sans-serif;background:#f3f6fb;padding:24px;">
	<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:640px;margin:0 auto;background:#ffffff;border:1px solid #dde3ec;border-radius:12px;overflow:hidden;">
		<tr>
			<td style="background:linear-gradient(135deg,#22d3ee,#10b981);color:#000000;padding:22px 28px;font-size:20px;font-weight:700;">JellyGate</td>
		</tr>
		<tr>
			<td style="padding:24px 28px;color:#0f172a;line-height:1.6;font-size:15px;">
`

const defaultEmailCardStyle = `
<div style="font-family:Segoe UI,Arial,sans-serif;background:#f3f6fb;padding:24px;">
	<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:640px;margin:0 auto;background:#ffffff;border:1px solid #dde3ec;border-radius:12px;overflow:hidden;">
		<tr>
			<td style="background:linear-gradient(135deg,#22d3ee,#10b981);padding:18px 24px;">
				<table role="presentation" width="100%" cellspacing="0" cellpadding="0">
					<tr>
						<td style="color:#ffffff;font-size:22px;font-weight:800;letter-spacing:-0.02em;">JellyGate</td>
						<td align="right">
							<div style="display:inline-block;background:rgba(255,255,255,0.15);border:1px solid rgba(255,255,255,0.25);border-radius:12px;padding:6px 10px;backdrop-filter:blur(8px);">
								<img src="{{.EmailLogoURL}}" alt="JellyGate" style="max-height:24px;width:auto;display:block;">
							</div>
						</td>
					</tr>
				</table>
			</td>
		</tr>
		<tr>
			<td style="padding:24px 28px;color:#0f172a;line-height:1.6;font-size:15px;">
`

const defaultEmailCardEnd = `
			</td>
		</tr>
		<tr>
			<td style="padding:16px 28px;background:#f8fafc;color:#64748b;font-size:12px;border-top:1px solid #e2e8f0;text-align:center;">{{.AutomaticFooter}}</td>
		</tr>
	</table>
</div>`

func defaultEmailBody(content string) string {
	return defaultEmailCardStyle + content + defaultEmailCardEnd
}

func DefaultEmailBaseHeader() string {
	return strings.TrimSpace(defaultEmailCardStyle)
}

func DefaultEmailBaseFooter() string {
	return strings.TrimSpace(defaultEmailCardEnd)
}

func defaultEmailParagraphs(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	chunks := strings.Split(normalized, "\n\n")
	paragraphs := make([]string, 0, len(chunks))

	for _, chunk := range chunks {
		trimmed := strings.TrimSpace(chunk)
		if trimmed == "" {
			continue
		}
		lines := strings.Split(trimmed, "\n")
		for idx := range lines {
			lines[idx] = escapePlainEmailLine(strings.TrimSpace(lines[idx]))
		}
		paragraphs = append(paragraphs, "<p>"+strings.Join(lines, "<br>")+"</p>")
	}

	return strings.Join(paragraphs, "\n")
}

func escapePlainEmailLine(line string) string {
	if strings.TrimSpace(line) == "" {
		return ""
	}

	placeholders := make([]string, 0, 4)
	protected := plainEmailVariablePattern.ReplaceAllStringFunc(line, func(match string) string {
		token := fmt.Sprintf("JELLYGATEVAR%03dTOKEN", len(placeholders))
		placeholders = append(placeholders, strings.TrimSpace(match))
		return token
	})

	escaped := html.EscapeString(protected)
	for idx, original := range placeholders {
		token := fmt.Sprintf("JELLYGATEVAR%03dTOKEN", idx)
		escaped = strings.ReplaceAll(escaped, token, original)
	}

	return escaped
}

func looksLikeStandaloneEmailHTML(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "<html") ||
		strings.Contains(lower, "<body") ||
		strings.Contains(lower, "<table") ||
		strings.Contains(lower, "</table>") ||
		strings.Contains(lower, "<tr") ||
		strings.Contains(lower, "<td") ||
		strings.Contains(lower, "</td>")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func defaultBaseTemplateHeaderOrFallback(header string) string {
	trimmed := strings.TrimSpace(header)
	if trimmed == "" {
		return DefaultEmailBaseHeader()
	}
	return trimmed
}

func defaultBaseTemplateFooterOrFallback(footer string) string {
	trimmed := strings.TrimSpace(footer)
	if trimmed == "" {
		return DefaultEmailBaseFooter()
	}
	return trimmed
}

func wrapEmailBody(content, header, footer string) string {
	return defaultBaseTemplateHeaderOrFallback(header) + content + defaultBaseTemplateFooterOrFallback(footer)
}

func normalizePlainTextEmailBody(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	cleaned := make([]string, 0, len(lines))
	blankCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			blankCount++
			if blankCount > 1 {
				continue
			}
			cleaned = append(cleaned, "")
			continue
		}
		blankCount = 0
		cleaned = append(cleaned, trimmed)
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func htmlToPlainEmailText(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	replacedAnchors := emailAnchorPattern.ReplaceAllStringFunc(normalized, func(match string) string {
		parts := emailAnchorPattern.FindStringSubmatch(match)
		if len(parts) < 5 {
			return match
		}
		href := strings.TrimSpace(firstNonEmpty(parts[1], parts[2], parts[3]))
		label := strings.TrimSpace(htmlToPlainEmailText(parts[4]))
		if href == "" {
			return label
		}
		if label == "" || label == href {
			return href
		}
		return label + "\n" + href
	})

	replacedAnchors = strings.NewReplacer(
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
		"</p>", "\n\n",
		"</div>", "\n",
		"</section>", "\n",
		"</article>", "\n",
		"</li>", "\n",
		"</ul>", "\n",
		"</ol>", "\n",
		"</h1>", "\n\n",
		"</h2>", "\n\n",
		"</h3>", "\n\n",
		"</h4>", "\n\n",
		"</tr>", "\n",
		"</table>", "\n",
		"</td>", " ",
	).Replace(replacedAnchors)

	stripped := emailTagPattern.ReplaceAllString(replacedAnchors, "")
	stripped = html.UnescapeString(stripped)
	return normalizePlainTextEmailBody(stripped)
}

func defaultNoCodeEmailBody(key string) string {
	return DefaultNoCodeEmailTemplateBodyForLanguage("fr", key)
}

func emailTemplateValueByKey(cfg EmailTemplatesConfig, templateKey string) string {
	switch templateKey {
	case "confirmation":
		return cfg.Confirmation
	case "email_verification":
		return cfg.EmailVerification
	case "expiry_reminder":
		return cfg.ExpiryReminder
	case "invitation":
		return cfg.Invitation
	case "invite_expiry":
		return cfg.InviteExpiry
	case "password_reset":
		return cfg.PasswordReset
	case "user_creation":
		return cfg.UserCreation
	case "user_deletion":
		return cfg.UserDeletion
	case "user_disabled":
		return cfg.UserDisabled
	case "user_enabled":
		return cfg.UserEnabled
	case "user_expired":
		return cfg.UserExpired
	case "expiry_adjusted":
		return cfg.ExpiryAdjusted
	case "welcome":
		return cfg.Welcome
	default:
		return ""
	}
}

func isKnownDefaultEmailBody(templateKey, content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	defaults := DefaultEmailTemplatesForLanguage("fr")
	legacy := legacyEmailTemplates()
	return trimmed == strings.TrimSpace(emailTemplateValueByKey(defaults, templateKey)) || trimmed == strings.TrimSpace(emailTemplateValueByKey(legacy, templateKey))
}

func buildAutomaticEmailBlock(templateKey string) string {
	return automaticEmailBlockForLanguage("fr", templateKey)
}

// EditableEmailTemplateBody retire l'habillage HTML standard pour presenter
// uniquement le contenu utile dans l'interface d'administration.
func EditableEmailTemplateBody(content string) string {
	return EditableEmailTemplateBodyWithBase(content, "", "")
}

func EditableEmailTemplateBodyWithBase(content, baseHeader, baseFooter string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	prefix := defaultBaseTemplateHeaderOrFallback(baseHeader)
	suffix := defaultBaseTemplateFooterOrFallback(baseFooter)
	if strings.HasPrefix(trimmed, prefix) && strings.HasSuffix(trimmed, suffix) {
		inner := strings.TrimPrefix(trimmed, prefix)
		inner = strings.TrimSuffix(inner, suffix)
		return strings.TrimSpace(inner)
	}

	legacyPrefix := DefaultEmailBaseHeader()
	legacySuffix := DefaultEmailBaseFooter()
	if strings.HasPrefix(trimmed, legacyPrefix) && strings.HasSuffix(trimmed, legacySuffix) {
		inner := strings.TrimPrefix(trimmed, legacyPrefix)
		inner = strings.TrimSuffix(inner, legacySuffix)
		return strings.TrimSpace(inner)
	}

	return trimmed
}

func EditableNoCodeEmailTemplateBody(templateKey, content, baseHeader, baseFooter string) string {
	return EditableNoCodeEmailTemplateBodyForLanguage("fr", templateKey, content, baseHeader, baseFooter)
}

func isKnownDefaultEmailBodyForLanguage(lang, templateKey, content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	defaults := DefaultEmailTemplatesForLanguage(lang)
	legacy := legacyEmailTemplates()
	return trimmed == strings.TrimSpace(emailTemplateValueByKey(defaults, templateKey)) || trimmed == strings.TrimSpace(emailTemplateValueByKey(legacy, templateKey))
}

func EditableNoCodeEmailTemplateBodyForLanguage(lang, templateKey, content, baseHeader, baseFooter string) string {
	if isKnownDefaultEmailBodyForLanguage(lang, templateKey, content) {
		return DefaultNoCodeEmailTemplateBodyForLanguage(lang, templateKey)
	}

	inner := EditableEmailTemplateBodyWithBase(content, baseHeader, baseFooter)
	if !strings.Contains(inner, "<") || !strings.Contains(inner, ">") {
		return normalizePlainTextEmailBody(inner)
	}
	return htmlToPlainEmailText(inner)
}

// PrepareEmailTemplateBody accepte soit un contenu simple, soit un HTML deja
// complet. Le contenu simple est injecte dans la carte email standard.
func PrepareEmailTemplateBody(content string) string {
	return PrepareEmailTemplateBodyFor("", content, "", "")
}

func PrepareEmailTemplateBodyFor(templateKey, content, baseHeader, baseFooter string) string {
	return PrepareEmailTemplateBodyForLanguage("fr", templateKey, content, baseHeader, baseFooter)
}

func PrepareEmailTemplateBodyForLanguage(lang, templateKey, content, baseHeader, baseFooter string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	if looksLikeStandaloneEmailHTML(trimmed) {
		return trimmed
	}

	if strings.Contains(trimmed, "<") && strings.Contains(trimmed, ">") {
		return wrapEmailBody(trimmed+automaticEmailBlockForLanguage(lang, templateKey), baseHeader, baseFooter)
	}

	body := defaultEmailParagraphs(normalizePlainTextEmailBody(trimmed))
	body += automaticEmailBlockForLanguage(lang, templateKey)
	return wrapEmailBody(body, baseHeader, baseFooter)
}

func legacyEmailTemplates() EmailTemplatesConfig {
	return EmailTemplatesConfig{
		EmailLogoURL: defaultEmailLogoPath,
		Confirmation: defaultEmailBody(`
<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Inscription confirmee</h2>
<p>Bonjour <strong>{{.Username}}</strong>,</p>
<p>Ton inscription est bien validee. Ton acces JellyGate est actif.</p>
<p style="margin:20px 0 0 0;">Si besoin, tu peux contacter l'equipe via <a href="{{.HelpURL}}" style="color:#0284c7;">ce lien d'aide</a>.</p>
`),
		ConfirmationSubject: `Inscription confirmee - JellyGate`,
		EmailVerification: defaultEmailBody(`
<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Verifie ton adresse e-mail</h2>
<p>Bonjour <strong>{{.Username}}</strong>,</p>
<p>Confirme ton adresse e-mail pour finaliser la securisation de ton compte JellyGate.</p>
<p style="margin:20px 0;"><a href="{{.VerificationLink}}" style="display:inline-block;background:#0ea5e9;color:#ffffff;text-decoration:none;padding:12px 18px;border-radius:8px;font-weight:600;">Verifier mon e-mail</a></p>
<p style="font-size:13px;color:#475569;">Expire dans {{.ExpiresIn}}</p>
`),
		EmailVerificationSubject: `Verifie ton adresse e-mail - JellyGate`,
		ExpiryReminder: defaultEmailBody(`
<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Rappel d'expiration</h2>
<p>Bonjour <strong>{{.Username}}</strong>,</p>
<p>Ton compte expirera prochainement.</p>
<p>Date previsionnelle: <strong>{{.ExpiryDate}}</strong></p>
`),
		ExpiryReminderSubject: `Rappel d'expiration - JellyGate`,
		Invitation: defaultEmailBody(`
<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Invitation JellyGate</h2>
<p>Bonjour,</p>
<p>Tu es invite a rejoindre le serveur. Clique sur le bouton ci-dessous pour creer ton compte.</p>
<p style="margin:20px 0;"><a href="{{.InviteLink}}" style="display:inline-block;background:#0ea5e9;color:#ffffff;text-decoration:none;padding:12px 18px;border-radius:8px;font-weight:600;">Creer mon compte</a></p>
`),
		InvitationSubject:    `Invitation a rejoindre JellyGate`,
		InviteExpirySubject:  `Expiration du lien d'invitation - JellyGate`,
		PasswordResetSubject: `Reinitialisation de votre mot de passe - JellyGate`,
		PostSignupHelp: defaultEmailBody(`
<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Premiere connexion</h2>
<p>Ton compte est pret. Utilise maintenant l'identifiant et le mot de passe que tu viens de definir pour te connecter.</p>
<p>Tu peux ensuite acceder directement a Jellyfin depuis l'interface principale.</p>
`),
		UserCreation: defaultEmailBody(`
<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Compte cree</h2>
<p>Bonjour <strong>{{.Username}}</strong>,</p>
<p>Ton compte a ete cree avec succes par un administrateur.</p>
`),
		UserCreationSubject: `Compte cree - JellyGate`,
		UserDeletion: defaultEmailBody(`
<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Compte supprime</h2>
<p>Bonjour <strong>{{.Username}}</strong>,</p>
<p>Ton compte a ete supprime.</p>
`),
		UserDeletionSubject: `Compte supprime - JellyGate`,
		UserDisabled: defaultEmailBody(`
<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Compte desactive</h2>
<p>Bonjour <strong>{{.Username}}</strong>,</p>
<p>Ton compte a ete desactive.</p>
`),
		UserDisabledSubject: `Compte desactive - JellyGate`,
		UserEnabled: defaultEmailBody(`
<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Compte reactive</h2>
<p>Bonjour <strong>{{.Username}}</strong>,</p>
<p>Ton compte a ete reactive.</p>
`),
		UserEnabledSubject: `Compte reactive - JellyGate`,
		UserExpired: defaultEmailBody(`
<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Acces expire</h2>
<p>Bonjour <strong>{{.Username}}</strong>,</p>
<p>Ton acces a expire et ton compte a ete desactive.</p>
`),
		UserExpiredSubject: `Compte expire - JellyGate`,
		ExpiryAdjusted: defaultEmailBody(`
<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Expiration mise a jour</h2>
<p>Bonjour <strong>{{.Username}}</strong>,</p>
<p>La date d'expiration de ton acces Jellyfin a ete mise a jour.</p>
<p>Nouvelle date: <strong>{{.ExpiryDate}}</strong></p>
`),
		ExpiryAdjustedSubject: `Expiration de l'acces Jellyfin ajustee`,
		Welcome: defaultEmailBody(`
<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Bienvenue {{.Username}}</h2>
<p>Ton compte JellyGate est pret.</p>
<p>Tu peux maintenant acceder a Jellyfin: <a href="{{.JellyfinURL}}" style="color:#0284c7;">{{.JellyfinURL}}</a></p>
`),
		WelcomeSubject: `Bienvenue sur JellyGate`,
	}
}

func replaceLegacyEmailField(current *string, legacy, updated string) {
	if strings.TrimSpace(*current) == strings.TrimSpace(legacy) {
		*current = updated
	}
}

// UpgradeLegacyEmailTemplates remplace uniquement les anciens textes par defaut
// enregistres en base afin d'aligner le wording avec un compte/acces Jellyfin.
func UpgradeLegacyEmailTemplates(cfg *EmailTemplatesConfig) {
	legacy := legacyEmailTemplates()
	updated := DefaultEmailTemplates()

	replaceLegacyEmailField(&cfg.Confirmation, legacy.Confirmation, updated.Confirmation)
	replaceLegacyEmailField(&cfg.ConfirmationSubject, legacy.ConfirmationSubject, updated.ConfirmationSubject)
	replaceLegacyEmailField(&cfg.EmailVerification, legacy.EmailVerification, updated.EmailVerification)
	replaceLegacyEmailField(&cfg.EmailVerificationSubject, legacy.EmailVerificationSubject, updated.EmailVerificationSubject)
	replaceLegacyEmailField(&cfg.ExpiryReminder, legacy.ExpiryReminder, updated.ExpiryReminder)
	replaceLegacyEmailField(&cfg.ExpiryReminderSubject, legacy.ExpiryReminderSubject, updated.ExpiryReminderSubject)
	replaceLegacyEmailField(&cfg.Invitation, legacy.Invitation, updated.Invitation)
	replaceLegacyEmailField(&cfg.InvitationSubject, legacy.InvitationSubject, updated.InvitationSubject)
	replaceLegacyEmailField(&cfg.InviteExpirySubject, legacy.InviteExpirySubject, updated.InviteExpirySubject)
	replaceLegacyEmailField(&cfg.PasswordResetSubject, legacy.PasswordResetSubject, updated.PasswordResetSubject)
	replaceLegacyEmailField(&cfg.PostSignupHelp, legacy.PostSignupHelp, updated.PostSignupHelp)
	replaceLegacyEmailField(&cfg.UserCreation, legacy.UserCreation, updated.UserCreation)
	replaceLegacyEmailField(&cfg.UserCreationSubject, legacy.UserCreationSubject, updated.UserCreationSubject)
	replaceLegacyEmailField(&cfg.UserDeletion, legacy.UserDeletion, updated.UserDeletion)
	replaceLegacyEmailField(&cfg.UserDeletionSubject, legacy.UserDeletionSubject, updated.UserDeletionSubject)
	replaceLegacyEmailField(&cfg.UserDisabled, legacy.UserDisabled, updated.UserDisabled)
	replaceLegacyEmailField(&cfg.UserDisabledSubject, legacy.UserDisabledSubject, updated.UserDisabledSubject)
	replaceLegacyEmailField(&cfg.UserEnabled, legacy.UserEnabled, updated.UserEnabled)
	replaceLegacyEmailField(&cfg.UserEnabledSubject, legacy.UserEnabledSubject, updated.UserEnabledSubject)
	replaceLegacyEmailField(&cfg.UserExpired, legacy.UserExpired, updated.UserExpired)
	replaceLegacyEmailField(&cfg.UserExpiredSubject, legacy.UserExpiredSubject, updated.UserExpiredSubject)
	replaceLegacyEmailField(&cfg.ExpiryAdjusted, legacy.ExpiryAdjusted, updated.ExpiryAdjusted)
	replaceLegacyEmailField(&cfg.ExpiryAdjustedSubject, legacy.ExpiryAdjustedSubject, updated.ExpiryAdjustedSubject)
	replaceLegacyEmailField(&cfg.Welcome, legacy.Welcome, updated.Welcome)
	replaceLegacyEmailField(&cfg.WelcomeSubject, legacy.WelcomeSubject, updated.WelcomeSubject)

	if strings.TrimSpace(cfg.BaseTemplateHeader) == strings.TrimSpace(legacyGradientEmailHeader) {
		cfg.BaseTemplateHeader = DefaultEmailBaseHeader()
	}

	logoURL := strings.TrimSpace(cfg.EmailLogoURL)
	if logoURL == "" || logoURL == legacyEmailLogoPath {
		cfg.EmailLogoURL = defaultEmailLogoPath
	}
}

// DefaultEmailTemplates retourne les traductions de base des modèles d'emails
func DefaultEmailTemplates() EmailTemplatesConfig {
	return DefaultEmailTemplatesForLanguage("fr")
}

// JellyfinPolicyPreset décrit un preset réutilisable pour les politiques Jellyfin.
type JellyfinPolicyPreset struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	EnableAllFolders   bool     `json:"enable_all_folders"`
	EnabledFolderIDs   []string `json:"enabled_folder_ids"`
	EnableDownload     bool     `json:"enable_download"`
	EnableRemoteAccess bool     `json:"enable_remote_access"`
	MaxSessions        int      `json:"max_sessions"`
	BitrateLimit       int      `json:"bitrate_limit"`
	TemplateUserID     string   `json:"template_user_id"`
	UsernameMinLength  int      `json:"username_min_length"`
	UsernameMaxLength  int      `json:"username_max_length"`
	PasswordMinLength  int      `json:"password_min_length"`
	PasswordMaxLength  int      `json:"password_max_length"`
	RequireUpper       bool     `json:"require_upper"`
	RequireLower       bool     `json:"require_lower"`
	RequireDigit       bool     `json:"require_digit"`
	RequireSpecial     bool     `json:"require_special"`
	DisableAfterDays   int      `json:"disable_after_days"`
	ExpiryAction       string   `json:"expiry_action"`
	DeleteAfterDays    int      `json:"delete_after_days"`

	// Parrainage / Sponsorship
	CanInvite              bool   `json:"can_invite"`
	TargetPresetID         string `json:"target_preset_id"`          // Le preset assigne aux personnes invitees
	InviteQuota            int    `json:"invite_quota"`              // Legacy: quota mensuel d'invitations
	InviteQuotaDay         int    `json:"invite_quota_day"`          // Quota journalier d'invitations
	InviteQuotaMonth       int    `json:"invite_quota_month"`        // Quota mensuel d'invitations
	InviteMaxUses          int    `json:"invite_max_uses"`           // Nombre d'utilisations par lien d'invitation
	InviteMaxLinkHours     int    `json:"invite_max_link_hours"`     // Legacy: duree de validite d'un lien en heures
	InviteLinkValidityDays int    `json:"invite_link_validity_days"` // Duree de validite d'un lien en jours
	InviteAllowLanguage    bool   `json:"invite_allow_language"`     // Si vrai, le parrain peut choisir la langue de l'invitation
}

// InvitationProfileConfig contient la politique appliquee a chaque nouvelle invitation.
// Les champs correspondent aux options de "Profil utilisateur" cote interface admin.
type InvitationProfileConfig struct {
	PolicyPresetID           string `json:"policy_preset_id"`
	TemplateUserID           string `json:"template_user_id"`
	EnableDownloads          bool   `json:"enable_downloads"`
	RequireEmail             bool   `json:"require_email"`
	RequireEmailVerification bool   `json:"require_email_verification"`
	EmailVerificationPolicy  string `json:"email_verification_policy"`
	AutoDeleteClosedLinks    bool   `json:"auto_delete_closed_links"`
	DisableAfterDays         int    `json:"disable_after_days"`
	DeleteAfterDays          int    `json:"delete_after_days"`
	ExpiryAction             string `json:"expiry_action"`
	AllowInviterGrant        bool   `json:"allow_inviter_grant_invite"`
	AllowInviterUserExpiry   bool   `json:"allow_inviter_user_expiry"`
	InviterMaxUses           int    `json:"inviter_max_uses"`
	InviterMaxLinkHours      int    `json:"inviter_max_link_hours"`
	InviterQuotaDay          int    `json:"inviter_quota_day"`
	InviterQuotaWeek         int    `json:"inviter_quota_week"`
	InviterQuotaMonth        int    `json:"inviter_quota_month"`
	UsernameMinLength        int    `json:"username_min_length"`
	UsernameMaxLength        int    `json:"username_max_length"`
	PasswordMinLength        int    `json:"password_min_length"`
	PasswordMaxLength        int    `json:"password_max_length"`
	PasswordRequireUpper     bool   `json:"password_require_upper"`
	PasswordRequireLower     bool   `json:"password_require_lower"`
	PasswordRequireDigit     bool   `json:"password_require_digit"`
	PasswordRequireSpecial   bool   `json:"password_require_special"`
}

// DefaultInvitationProfileConfig retourne la configuration par defaut appliquee
// quand aucune politique d'invitation n'est encore enregistree.
func DefaultInvitationProfileConfig() InvitationProfileConfig {
	return InvitationProfileConfig{
		PolicyPresetID:           "",
		TemplateUserID:           "",
		EnableDownloads:          true,
		RequireEmail:             true,
		RequireEmailVerification: true,
		EmailVerificationPolicy:  "required",
		AutoDeleteClosedLinks:    false,
		DisableAfterDays:         0,
		DeleteAfterDays:          0,
		ExpiryAction:             "disable",
		AllowInviterGrant:        false,
		AllowInviterUserExpiry:   true,
		InviterMaxUses:           0,
		InviterMaxLinkHours:      0,
		InviterQuotaDay:          0,
		InviterQuotaWeek:         0,
		InviterQuotaMonth:        0,
		UsernameMinLength:        3,
		UsernameMaxLength:        32,
		PasswordMinLength:        8,
		PasswordMaxLength:        128,
		PasswordRequireUpper:     false,
		PasswordRequireLower:     false,
		PasswordRequireDigit:     false,
		PasswordRequireSpecial:   false,
	}
}

// GroupPolicyMapping lie un groupe (interne ou LDAP) à un preset Jellyfin.
type GroupPolicyMapping struct {
	GroupName      string `json:"group_name"`
	Source         string `json:"source"` // internal|ldap
	LDAPGroupDN    string `json:"ldap_group_dn"`
	PolicyPresetID string `json:"policy_preset_id"`
}

// PortalLinksConfig contient les URLs publiques exposees dans l'UI et les emails.
type PortalLinksConfig struct {
	JellyGateURL       string `json:"jellygate_url"`
	JellyfinURL        string `json:"jellyfin_url"`
	JellyfinServerName string `json:"jellyfin_server_name"`
	JellyseerrURL      string `json:"jellyseerr_url"`
	JellyTrackURL      string `json:"jellytrack_url"`
}

// DefaultPortalLinks retourne une configuration de liens vide.
func DefaultPortalLinks() PortalLinksConfig {
	return PortalLinksConfig{JellyfinServerName: "Jellyfin"}
}

// DefaultJellyfinPolicyPresets retourne un ensemble de presets initiaux.
func DefaultJellyfinPolicyPresets() []JellyfinPolicyPreset {
	return []JellyfinPolicyPreset{
		{
			ID:                 "standard",
			Name:               "Standard",
			Description:        "Profil par defaut: acces distant actif, telechargement actif.",
			EnableAllFolders:   true,
			EnableDownload:     true,
			EnableRemoteAccess: true,
			MaxSessions:        0,
			BitrateLimit:       0,
			UsernameMinLength:  3,
			UsernameMaxLength:  32,
			PasswordMinLength:  8,
			PasswordMaxLength:  128,
			DisableAfterDays:   0,
			ExpiryAction:       "disable",
			DeleteAfterDays:    0,
		},
		{
			ID:                 "limited",
			Name:               "Limite",
			Description:        "Profil restreint: telechargement coupe, 2 sessions max.",
			EnableAllFolders:   true,
			EnableDownload:     false,
			EnableRemoteAccess: true,
			MaxSessions:        2,
			BitrateLimit:       4000,
			UsernameMinLength:  3,
			UsernameMaxLength:  32,
			PasswordMinLength:  10,
			PasswordMaxLength:  128,
			RequireDigit:       true,
			DisableAfterDays:   0,
			ExpiryAction:       "disable",
			DeleteAfterDays:    0,
		},
	}
}

// WebhooksConfig contient les paramètres des webhooks sortants (optionnels).
type WebhooksConfig struct {
	Discord  DiscordWebhook  `json:"discord"`
	Telegram TelegramWebhook `json:"telegram"`
	Matrix   MatrixWebhook   `json:"matrix"`
}

// DiscordWebhook contient la configuration du webhook Discord.
type DiscordWebhook struct {
	URL string `json:"url"`
}

// TelegramWebhook contient la configuration du bot Telegram.
type TelegramWebhook struct {
	Token  string `json:"token"`
	ChatID string `json:"chat_id"`
}

// MatrixWebhook contient la configuration de la connexion Matrix.
type MatrixWebhook struct {
	URL    string `json:"url"`
	RoomID string `json:"room_id"`
	Token  string `json:"token"`
}

// ── Chargement depuis l'environnement ───────────────────────────────────────

// Load charge la configuration depuis les variables d'environnement,
// applique les valeurs par défaut, et valide les champs requis.
//
// Seuls les paramètres App + Jellyfin sont chargés ici.
// LDAP, SMTP et Webhooks sont chargés depuis la base de données.
func Load() (*Config, error) {
	cfg := &Config{
		Port:              getEnvInt("JELLYGATE_PORT", 8097),
		BaseURL:           getEnv("JELLYGATE_BASE_URL", "http://localhost:8097"),
		DataDir:           getEnv("JELLYGATE_DATA_DIR", "/data"),
		SecretKey:         getEnv("JELLYGATE_SECRET_KEY", ""),
		TLSCert:           getEnv("JELLYGATE_TLS_CERT", ""),
		TLSKey:            getEnv("JELLYGATE_TLS_KEY", ""),
		DefaultLang:       NormalizeLanguageTag(getEnv("JELLYGATE_DEFAULT_LANG", "")),
		EnableDebugRoutes: getEnvBool("JELLYGATE_ENABLE_DEBUG_ROUTES", false),

		Database: DatabaseConfig{
			Type:     strings.TrimSpace(strings.ToLower(getEnv("DB_TYPE", "sqlite"))),
			Host:     strings.TrimSpace(getEnv("DB_HOST", "")),
			Port:     getEnvInt("DB_PORT", 5432),
			User:     strings.TrimSpace(getEnv("DB_USER", "")),
			Password: getEnv("DB_PASSWORD", ""),
			Name:     strings.TrimSpace(getEnv("DB_NAME", "jellygate")),
			SSLMode:  strings.TrimSpace(strings.ToLower(getEnv("DB_SSLMODE", "disable"))),
		},

		Jellyfin: JellyfinConfig{
			URL:    getEnv("JELLYFIN_URL", ""),
			APIKey: getEnv("JELLYFIN_API_KEY", ""),
		},

		ThirdParty: ThirdPartyConfig{
			JellyseerrURL:    strings.TrimSpace(getEnv("JELLYSEERR_URL", "")),
			JellyseerrAPIKey: strings.TrimSpace(getEnv("JELLYSEERR_API_KEY", "")),
			JellyTrackURL:    strings.TrimSpace(getEnv("JELLYTRACK_URL", "")),
			JellyTrackAPIKey: strings.TrimSpace(getEnv("JELLYTRACK_API_KEY", "")),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("configuration invalide: %w", err)
	}

	return cfg, nil
}

// validate vérifie que tous les champs requis sont renseignés.
func (c *Config) validate() error {
	var errs []string

	// Application
	if c.SecretKey == "" {
		errs = append(errs, "JELLYGATE_SECRET_KEY est requis")
	} else if len(c.SecretKey) < 32 {
		errs = append(errs, "JELLYGATE_SECRET_KEY doit faire au minimum 32 caractères")
	}

	// Jellyfin
	if c.Jellyfin.URL == "" {
		errs = append(errs, "JELLYFIN_URL est requis")
	}
	if c.Jellyfin.APIKey == "" {
		errs = append(errs, "JELLYFIN_API_KEY est requis")
	}

	// Validation de la langue par défaut si fournie
	if c.DefaultLang != "" && !IsSupportedLanguage(c.DefaultLang) {
		errs = append(errs, fmt.Sprintf("JELLYGATE_DEFAULT_LANG '%s' n'est pas une langue supportée", c.DefaultLang))
	}

	if c.Database.Type == "" {
		c.Database.Type = "sqlite"
	}
	if c.Database.Type != "sqlite" && c.Database.Type != "postgres" {
		errs = append(errs, "DB_TYPE doit etre 'sqlite' ou 'postgres'")
	}
	if c.Database.Type == "postgres" {
		if c.Database.Host == "" {
			errs = append(errs, "DB_HOST est requis quand DB_TYPE=postgres")
		}
		if c.Database.User == "" {
			errs = append(errs, "DB_USER est requis quand DB_TYPE=postgres")
		}
		if c.Database.Name == "" {
			errs = append(errs, "DB_NAME est requis quand DB_TYPE=postgres")
		}
		if c.Database.Port <= 0 {
			errs = append(errs, "DB_PORT doit etre superieur a 0")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%d erreur(s):\n  - %s", len(errs), strings.Join(errs, "\n  - "))
	}

	return nil
}

// ── Fonctions utilitaires pour lire les variables d'environnement ───────────

// getEnv renvoie la valeur de la variable d'environnement key,
// ou defaultVal si la variable est vide ou absente.
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// getEnvInt renvoie la valeur entière de la variable d'environnement key,
// ou defaultVal si la variable est vide, absente ou invalide.
func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return n
}

// getEnvBool renvoie la valeur booléenne de la variable d'environnement key,
// ou defaultVal si la variable est vide, absente ou invalide.
// Valeurs acceptées : "true", "1", "yes" → true ; "false", "0", "no" → false.
func getEnvBool(key string, defaultVal bool) bool {
	val := strings.ToLower(os.Getenv(key))
	switch val {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return defaultVal
	}
}
