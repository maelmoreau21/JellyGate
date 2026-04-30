package config

import (
	"html"
	"strings"
)

// SupportedLanguageOrder conserve un ordre stable pour l'UI et les defaults.
var SupportedLanguageOrder = []string{
	"fr",
	"en",
	"de",
	"es",
	"it",
	"nl",
	"pl",
	"pt-br",
	"ru",
	"zh",
}

type emailTextPack struct {
	ConfirmationSubject      string
	ConfirmationBody         string
	EmailVerificationSubject string
	EmailVerificationBody    string
	ExpiryReminderSubject    string
	ExpiryReminderBody       string
	InvitationSubject        string
	InvitationBody           string
	InviteExpirySubject      string
	InviteExpiryBody         string
	PasswordResetSubject     string
	PasswordResetBody        string
	UserCreationSubject      string
	UserCreationBody         string
	UserDeletionSubject      string
	UserDeletionBody         string
	UserDisabledSubject      string
	UserDisabledBody         string
	UserEnabledSubject       string
	UserEnabledBody          string
	UserExpiredSubject       string
	UserExpiredBody          string
	ExpiryAdjustedSubject    string
	ExpiryAdjustedBody       string
	WelcomeSubject           string
	WelcomeBody              string
	VerifyButtonLabel        string
	ExpiryDateLabel          string
	ExpiresInLabel           string
	CreateAccountButtonLabel string
	DirectLinkLabel          string
	ResetPasswordButtonLabel string
	CodeLabel                string
	OpenServerButtonLabel    string
	DirectAccessLabel        string
	PreviewDuration          string
	PreviewMessage           string
	AutomaticFooter          string
}

var emailTextPacks = map[string]emailTextPack{
	"fr": {
		ConfirmationSubject:      `Acces {{.JellyfinServerName}} active`,
		ConfirmationBody:         "Bonjour {{.Username}},\n\nTon acces a {{.JellyfinServerName}} est maintenant actif.\n\nTu peux te connecter quand tu veux. Besoin d'aide ? {{.HelpURL}}",
		EmailVerificationSubject: `Verifie ton adresse e-mail pour {{.JellyfinServerName}}`,
		EmailVerificationBody:    "Bonjour {{.Username}},\n\nMerci de verifier ton adresse e-mail pour securiser ton acces a {{.JellyfinServerName}}.\n\nLe bouton de verification est disponible sous ce message.",
		ExpiryReminderSubject:    `Rappel d'expiration pour {{.JellyfinServerName}}`,
		ExpiryReminderBody:       "Bonjour {{.Username}},\n\nPetit rappel : ton acces a {{.JellyfinServerName}} expirera bientot.\n\nLa date d'expiration est disponible dans cet e-mail.",
		InvitationSubject:        `Invitation a rejoindre {{.JellyfinServerName}}`,
		InvitationBody:           "Bonjour,\n\nTu as recu une invitation pour rejoindre {{.JellyfinServerName}}.\n\nLe bouton de creation de compte est disponible sous ce message.",
		InviteExpirySubject:      `Expiration du lien d'invitation pour {{.JellyfinServerName}}`,
		InviteExpiryBody:         "Bonjour,\n\nTon lien d'invitation vers {{.JellyfinServerName}} expirera bientot.\n\nLa date limite est indiquee dans cet e-mail.",
		PasswordResetSubject:     `Reinitialisation du mot de passe {{.JellyfinServerName}}`,
		PasswordResetBody:        "Bonjour {{.Username}},\n\nNous avons recu une demande de reinitialisation pour ton compte {{.JellyfinServerName}}.\n\nLe bouton de reinitialisation est disponible sous ce message.",
		UserCreationSubject:      `Compte {{.serveurname}} cree`,
		UserCreationBody:         "Bonjour {{.Username}},\n\nUn administrateur vient de creer ton compte {{.serveurname}}.\n\nTu peux utiliser les informations recues pour te connecter.",
		UserDeletionSubject:      `Compte {{.JellyfinServerName}} supprime`,
		UserDeletionBody:         "Bonjour {{.Username}},\n\nTon compte {{.JellyfinServerName}} a ete supprime.\n\nSi cela te semble inattendu, contacte rapidement l'equipe d'administration.",
		UserDisabledSubject:      `Acces {{.JellyfinServerName}} desactive`,
		UserDisabledBody:         "Bonjour {{.Username}},\n\nTon acces a {{.JellyfinServerName}} a ete desactive temporairement.\n\nSi tu penses qu'il s'agit d'une erreur, contacte l'equipe d'administration.",
		UserEnabledSubject:       `Acces {{.JellyfinServerName}} reactive`,
		UserEnabledBody:          "Bonjour {{.Username}},\n\nBonne nouvelle : ton acces a {{.JellyfinServerName}} a ete reactive.\n\nTu peux te reconnecter des maintenant.",
		UserExpiredSubject:       `Acces {{.JellyfinServerName}} expire`,
		UserExpiredBody:          "Bonjour {{.Username}},\n\nTon acces a {{.JellyfinServerName}} a expire et ton compte a ete desactive automatiquement.\n\nContacte l'equipe d'administration si tu souhaites retrouver l'acces.",
		ExpiryAdjustedSubject:    `Expiration de l'acces {{.JellyfinServerName}} ajustee`,
		ExpiryAdjustedBody:       "Bonjour {{.Username}},\n\nLa date d'expiration de ton acces a {{.JellyfinServerName}} a ete mise a jour.\n\nLa nouvelle date apparait dans cet e-mail.",
		WelcomeSubject:           `Bienvenue sur {{.JellyfinServerName}}`,
		WelcomeBody:              "Bonjour {{.Username}},\n\nTon compte {{.JellyfinServerName}} est pret.\n\nLe bouton d'acces direct est disponible sous ce message.",
		VerifyButtonLabel:        "Verifier mon e-mail",
		ExpiryDateLabel:          "Date",
		ExpiresInLabel:           "Expire dans",
		CreateAccountButtonLabel: "Creer mon compte",
		DirectLinkLabel:          "Lien direct",
		ResetPasswordButtonLabel: "Reinitialiser mon mot de passe",
		CodeLabel:                "Code",
		OpenServerButtonLabel:    "Ouvrir {{.JellyfinServerName}}",
		DirectAccessLabel:        "Acces direct",
		PreviewDuration:          "15 minutes",
		PreviewMessage:           "Ton acces a {{.JellyfinServerName}} est pret. Utilise les liens ci-dessous.",
		AutomaticFooter:          "Ceci est un message automatique envoyé par JellyGate.",
	},
	"en": {
		ConfirmationSubject:      `{{.JellyfinServerName}} access activated`,
		ConfirmationBody:         "Hello {{.Username}},\n\nYour access to {{.JellyfinServerName}} is now active.\n\nYou can sign in whenever you want. Need help? {{.HelpURL}}",
		EmailVerificationSubject: `Verify your email for {{.JellyfinServerName}}`,
		EmailVerificationBody:    "Hello {{.Username}},\n\nPlease verify your email address to secure your access to {{.JellyfinServerName}}.\n\nThe verification button is available below this message.",
		ExpiryReminderSubject:    `{{.JellyfinServerName}} access expiry reminder`,
		ExpiryReminderBody:       "Hello {{.Username}},\n\nThis is a quick reminder that your access to {{.JellyfinServerName}} will expire soon.\n\nThe expiry date is available in this email.",
		InvitationSubject:        `Invitation to join {{.JellyfinServerName}}`,
		InvitationBody:           "Hello,\n\nYou have been invited to join {{.JellyfinServerName}}.\n\nThe account creation button is available below this message.",
		InviteExpirySubject:      `Invitation link for {{.JellyfinServerName}} is expiring soon`,
		InviteExpiryBody:         "Hello,\n\nYour invitation link for {{.JellyfinServerName}} will expire soon.\n\nThe deadline is indicated in this email.",
		PasswordResetSubject:     `Reset your {{.JellyfinServerName}} password`,
		PasswordResetBody:        "Hello {{.Username}},\n\nWe received a password reset request for your {{.JellyfinServerName}} account.\n\nThe reset button is available below this message.",
		UserCreationSubject:      `{{.serveurname}} account created`,
		UserCreationBody:         "Hello {{.Username}},\n\nAn administrator has created your {{.serveurname}} account.\n\nYou can now use the details you received to sign in.",
		UserDeletionSubject:      `{{.JellyfinServerName}} account deleted`,
		UserDeletionBody:         "Hello {{.Username}},\n\nYour {{.JellyfinServerName}} account has been deleted.\n\nIf this seems unexpected, please contact the administrators quickly.",
		UserDisabledSubject:      `{{.JellyfinServerName}} access disabled`,
		UserDisabledBody:         "Hello {{.Username}},\n\nYour access to {{.JellyfinServerName}} has been temporarily disabled.\n\nIf you think this is a mistake, please contact the administrators.",
		UserEnabledSubject:       `{{.JellyfinServerName}} access re-enabled`,
		UserEnabledBody:          "Hello {{.Username}},\n\nGood news: your access to {{.JellyfinServerName}} has been restored.\n\nYou can sign in again right away.",
		UserExpiredSubject:       `{{.JellyfinServerName}} access expired`,
		UserExpiredBody:          "Hello {{.Username}},\n\nYour access to {{.JellyfinServerName}} expired and your account was disabled automatically.\n\nPlease contact the administrators if you need access again.",
		ExpiryAdjustedSubject:    `{{.JellyfinServerName}} access expiry updated`,
		ExpiryAdjustedBody:       "Hello {{.Username}},\n\nThe expiry date for your access to {{.JellyfinServerName}} has been updated.\n\nThe new date is available in this email.",
		WelcomeSubject:           `Welcome to {{.JellyfinServerName}}`,
		WelcomeBody:              "Hello {{.Username}},\n\nYour {{.JellyfinServerName}} account is ready.\n\nThe direct access button is available below this message.",
		VerifyButtonLabel:        "Verify my email",
		ExpiryDateLabel:          "Date",
		ExpiresInLabel:           "Expires in",
		CreateAccountButtonLabel: "Create my account",
		DirectLinkLabel:          "Direct link",
		ResetPasswordButtonLabel: "Reset my password",
		CodeLabel:                "Code",
		OpenServerButtonLabel:    "Open {{.JellyfinServerName}}",
		DirectAccessLabel:        "Direct access",
		PreviewDuration:          "15 minutes",
		PreviewMessage:           "Your access to {{.JellyfinServerName}} is ready. Use the links below.",
		AutomaticFooter:          "This is an automated message sent by JellyGate.",
	},
	"de": {
		ConfirmationSubject:      `Zugang zu {{.JellyfinServerName}} aktiviert`,
		ConfirmationBody:         "Hallo {{.Username}},\n\nDein Zugang zu {{.JellyfinServerName}} ist jetzt aktiv.\n\nDu kannst dich jederzeit anmelden. Hilfe findest du unter {{.HelpURL}}.",
		EmailVerificationSubject: `Bestatige deine E-Mail fur {{.JellyfinServerName}}`,
		EmailVerificationBody:    "Hallo {{.Username}},\n\nBitte bestatige deine E-Mail-Adresse, um deinen Zugang zu {{.JellyfinServerName}} zu sichern.\n\nDer Bestatigungsbutton ist unter dieser Nachricht verfugbar.",
		ExpiryReminderSubject:    `Ablauf-Erinnerung fur {{.JellyfinServerName}}`,
		ExpiryReminderBody:       "Hallo {{.Username}},\n\nDies ist eine Erinnerung: dein Zugang zu {{.JellyfinServerName}} lauft bald ab.\n\nDas Ablaufdatum ist in dieser E-Mail verfugbar.",
		InvitationSubject:        `Einladung zu {{.JellyfinServerName}}`,
		InvitationBody:           "Hallo,\n\nDu wurdest zu {{.JellyfinServerName}} eingeladen.\n\nDer Button zur Kontoerstellung ist unter dieser Nachricht verfugbar.",
		InviteExpirySubject:      `Einladungslink fur {{.JellyfinServerName}} lauft bald ab`,
		InviteExpiryBody:         "Hallo,\n\nDein Einladungslink fur {{.JellyfinServerName}} lauft bald ab.\n\nDie Frist ist in dieser E-Mail angegeben.",
		PasswordResetSubject:     `Passwort fur {{.JellyfinServerName}} zurucksetzen`,
		PasswordResetBody:        "Hallo {{.Username}},\n\nWir haben eine Anfrage zum Zurucksetzen des Passworts fur dein {{.JellyfinServerName}}-Konto erhalten.\n\nDer Reset-Button ist unter dieser Nachricht verfugbar.",
		UserCreationSubject:      `{{.serveurname}}-Konto erstellt`,
		UserCreationBody:         "Hallo {{.Username}},\n\nEin Administrator hat dein {{.serveurname}}-Konto erstellt.\n\nDu kannst dich jetzt mit den erhaltenen Informationen anmelden.",
		UserDeletionSubject:      `{{.JellyfinServerName}}-Konto geloscht`,
		UserDeletionBody:         "Hallo {{.Username}},\n\nDein {{.JellyfinServerName}}-Konto wurde geloscht.\n\nFalls das unerwartet ist, kontaktiere bitte schnell die Administratoren.",
		UserDisabledSubject:      `Zugang zu {{.JellyfinServerName}} deaktiviert`,
		UserDisabledBody:         "Hallo {{.Username}},\n\nDein Zugang zu {{.JellyfinServerName}} wurde vorubergehend deaktiviert.\n\nFalls das ein Fehler ist, kontaktiere bitte die Administratoren.",
		UserEnabledSubject:       `Zugang zu {{.JellyfinServerName}} wieder aktiviert`,
		UserEnabledBody:          "Hallo {{.Username}},\n\nGute Nachricht: dein Zugang zu {{.JellyfinServerName}} wurde wieder aktiviert.\n\nDu kannst dich sofort erneut anmelden.",
		UserExpiredSubject:       `Zugang zu {{.JellyfinServerName}} abgelaufen`,
		UserExpiredBody:          "Hallo {{.Username}},\n\nDein Zugang zu {{.JellyfinServerName}} ist abgelaufen und dein Konto wurde automatisch deaktiviert.\n\nBitte kontaktiere die Administratoren, wenn du wieder Zugang brauchst.",
		ExpiryAdjustedSubject:    `Ablaufdatum fur {{.JellyfinServerName}} aktualisiert`,
		ExpiryAdjustedBody:       "Hallo {{.Username}},\n\nDas Ablaufdatum deines Zugangs zu {{.JellyfinServerName}} wurde aktualisiert.\n\nDas neue Datum ist in dieser E-Mail verfugbar.",
		WelcomeSubject:           `Willkommen bei {{.JellyfinServerName}}`,
		WelcomeBody:              "Hallo {{.Username}},\n\nDein Konto fur {{.JellyfinServerName}} ist bereit.\n\nDer Direktzugriff ist unter dieser Nachricht verfugbar.",
		VerifyButtonLabel:        "E-Mail bestatigen",
		ExpiryDateLabel:          "Datum",
		ExpiresInLabel:           "Lauft ab in",
		CreateAccountButtonLabel: "Konto erstellen",
		DirectLinkLabel:          "Direktlink",
		ResetPasswordButtonLabel: "Passwort zurucksetzen",
		CodeLabel:                "Code",
		OpenServerButtonLabel:    "{{.JellyfinServerName}} offnen",
		DirectAccessLabel:        "Direktzugang",
		PreviewDuration:          "15 Minuten",
		PreviewMessage:           "Dein Zugang zu {{.JellyfinServerName}} ist bereit. Nutze die Links unten.",
		AutomaticFooter:          "Dies ist eine automatische Nachricht von JellyGate.",
	},
	"es": {
		ConfirmationSubject:      `Acceso a {{.JellyfinServerName}} activado`,
		ConfirmationBody:         "Hola {{.Username}},\n\nTu acceso a {{.JellyfinServerName}} ya esta activo.\n\nPuedes iniciar sesion cuando quieras. Si necesitas ayuda, usa {{.HelpURL}}.",
		EmailVerificationSubject: `Verifica tu correo para {{.JellyfinServerName}}`,
		EmailVerificationBody:    "Hola {{.Username}},\n\nVerifica tu direccion de correo para asegurar tu acceso a {{.JellyfinServerName}}.\n\nEl boton de verificacion y el tiempo de validez se anaden automaticamente debajo de este mensaje.",
		ExpiryReminderSubject:    `Recordatorio de expiracion para {{.JellyfinServerName}}`,
		ExpiryReminderBody:       "Hola {{.Username}},\n\nEste es un recordatorio: tu acceso a {{.JellyfinServerName}} expirara pronto.\n\nLa fecha exacta se anade automaticamente en este correo.",
		InvitationSubject:        `Invitacion para unirte a {{.JellyfinServerName}}`,
		InvitationBody:           "Hola,\n\nHas recibido una invitacion para unirte a {{.JellyfinServerName}}.\n\nEl boton para crear la cuenta y el enlace directo se anaden automaticamente debajo de este mensaje.",
		InviteExpirySubject:      `El enlace de invitacion para {{.JellyfinServerName}} expirara pronto`,
		InviteExpiryBody:         "Hola,\n\nTu enlace de invitacion para {{.JellyfinServerName}} expirara pronto.\n\nLa fecha limite se anade automaticamente en este correo.",
		PasswordResetSubject:     `Restablece tu contrasena de {{.JellyfinServerName}}`,
		PasswordResetBody:        "Hola {{.Username}},\n\nHemos recibido una solicitud para restablecer la contrasena de tu cuenta de {{.JellyfinServerName}}.\n\nEl boton de restablecimiento, el enlace directo y el codigo se anaden automaticamente debajo de este mensaje.",
		UserCreationSubject:      `Cuenta de {{.serveurname}} creada`,
		UserCreationBody:         "Hola {{.Username}},\n\nUn administrador ha creado tu cuenta de {{.serveurname}}.\n\nYa puedes iniciar sesion con los datos recibidos.",
		UserDeletionSubject:      `Cuenta de {{.JellyfinServerName}} eliminada`,
		UserDeletionBody:         "Hola {{.Username}},\n\nTu cuenta de {{.JellyfinServerName}} ha sido eliminada.\n\nSi no lo esperabas, contacta rapidamente con los administradores.",
		UserDisabledSubject:      `Acceso a {{.JellyfinServerName}} desactivado`,
		UserDisabledBody:         "Hola {{.Username}},\n\nTu acceso a {{.JellyfinServerName}} ha sido desactivado temporalmente.\n\nSi crees que es un error, contacta con los administradores.",
		UserEnabledSubject:       `Acceso a {{.JellyfinServerName}} reactivado`,
		UserEnabledBody:          "Hola {{.Username}},\n\nBuenas noticias: tu acceso a {{.JellyfinServerName}} ha sido reactivado.\n\nPuedes volver a iniciar sesion ahora mismo.",
		UserExpiredSubject:       `Acceso a {{.JellyfinServerName}} caducado`,
		UserExpiredBody:          "Hola {{.Username}},\n\nTu acceso a {{.JellyfinServerName}} ha caducado y tu cuenta se ha desactivado automaticamente.\n\nContacta con los administradores si necesitas recuperar el acceso.",
		ExpiryAdjustedSubject:    `Expiracion de {{.JellyfinServerName}} actualizada`,
		ExpiryAdjustedBody:       "Hola {{.Username}},\n\nLa fecha de expiracion de tu acceso a {{.JellyfinServerName}} ha sido actualizada.\n\nLa nueva fecha se anade automaticamente en este correo.",
		WelcomeSubject:           `Bienvenido a {{.JellyfinServerName}}`,
		WelcomeBody:              "Hola {{.Username}},\n\nTu cuenta de {{.JellyfinServerName}} esta lista.\n\nEl boton de acceso directo se anade automaticamente debajo de este mensaje.",
		VerifyButtonLabel:        "Verificar mi correo",
		ExpiryDateLabel:          "Fecha",
		ExpiresInLabel:           "Caduca en",
		CreateAccountButtonLabel: "Crear mi cuenta",
		DirectLinkLabel:          "Enlace directo",
		ResetPasswordButtonLabel: "Restablecer mi contrasena",
		CodeLabel:                "Codigo",
		OpenServerButtonLabel:    "Abrir {{.JellyfinServerName}}",
		DirectAccessLabel:        "Acceso directo",
		PreviewDuration:          "15 minutos",
		PreviewMessage:           "Tu acceso a {{.JellyfinServerName}} esta listo. Usa los enlaces de abajo.",
		AutomaticFooter:          "Este es un mensaje automático enviado por JellyGate.",
	},
	"it": {
		ConfirmationSubject:      `Accesso a {{.JellyfinServerName}} attivato`,
		ConfirmationBody:         "Ciao {{.Username}},\n\nIl tuo accesso a {{.JellyfinServerName}} e ora attivo.\n\nPuoi accedere quando vuoi. Se ti serve aiuto, usa {{.HelpURL}}.",
		EmailVerificationSubject: `Verifica la tua e-mail per {{.JellyfinServerName}}`,
		EmailVerificationBody:    "Ciao {{.Username}},\n\nVerifica il tuo indirizzo e-mail per proteggere l'accesso a {{.JellyfinServerName}}.\n\nIl pulsante di verifica e la durata vengono aggiunti automaticamente sotto questo messaggio.",
		ExpiryReminderSubject:    `Promemoria scadenza per {{.JellyfinServerName}}`,
		ExpiryReminderBody:       "Ciao {{.Username}},\n\nQuesto e un promemoria: il tuo accesso a {{.JellyfinServerName}} scadra presto.\n\nLa data esatta viene aggiunta automaticamente in questa e-mail.",
		InvitationSubject:        `Invito a unirti a {{.JellyfinServerName}}`,
		InvitationBody:           "Ciao,\n\nHai ricevuto un invito per unirti a {{.JellyfinServerName}}.\n\nIl pulsante per creare l'account e il link diretto vengono aggiunti automaticamente sotto questo messaggio.",
		InviteExpirySubject:      `Il link di invito per {{.JellyfinServerName}} scadra presto`,
		InviteExpiryBody:         "Ciao,\n\nIl tuo link di invito per {{.JellyfinServerName}} scadra presto.\n\nLa scadenza viene aggiunta automaticamente in questa e-mail.",
		PasswordResetSubject:     `Reimposta la password di {{.JellyfinServerName}}`,
		PasswordResetBody:        "Ciao {{.Username}},\n\nAbbiamo ricevuto una richiesta di reimpostazione della password per il tuo account {{.JellyfinServerName}}.\n\nIl pulsante di reset, il link diretto e il codice vengono aggiunti automaticamente sotto questo messaggio.",
		UserCreationSubject:      `Account {{.serveurname}} creato`,
		UserCreationBody:         "Ciao {{.Username}},\n\nUn amministratore ha creato il tuo account {{.serveurname}}.\n\nOra puoi accedere con le informazioni ricevute.",
		UserDeletionSubject:      `Account {{.JellyfinServerName}} eliminato`,
		UserDeletionBody:         "Ciao {{.Username}},\n\nIl tuo account {{.JellyfinServerName}} e stato eliminato.\n\nSe ti sembra inatteso, contatta rapidamente gli amministratori.",
		UserDisabledSubject:      `Accesso a {{.JellyfinServerName}} disattivato`,
		UserDisabledBody:         "Ciao {{.Username}},\n\nIl tuo accesso a {{.JellyfinServerName}} e stato disattivato temporaneamente.\n\nSe pensi sia un errore, contatta gli amministratori.",
		UserEnabledSubject:       `Accesso a {{.JellyfinServerName}} riattivato`,
		UserEnabledBody:          "Ciao {{.Username}},\n\nBuone notizie: il tuo accesso a {{.JellyfinServerName}} e stato riattivato.\n\nPuoi accedere di nuovo subito.",
		UserExpiredSubject:       `Accesso a {{.JellyfinServerName}} scaduto`,
		UserExpiredBody:          "Ciao {{.Username}},\n\nIl tuo accesso a {{.JellyfinServerName}} e scaduto e il tuo account e stato disattivato automaticamente.\n\nContatta gli amministratori se hai bisogno di nuovo accesso.",
		ExpiryAdjustedSubject:    `Scadenza di {{.JellyfinServerName}} aggiornata`,
		ExpiryAdjustedBody:       "Ciao {{.Username}},\n\nLa data di scadenza del tuo accesso a {{.JellyfinServerName}} e stata aggiornata.\n\nLa nuova data viene aggiunta automaticamente in questa e-mail.",
		WelcomeSubject:           `Benvenuto su {{.JellyfinServerName}}`,
		WelcomeBody:              "Ciao {{.Username}},\n\nIl tuo account {{.JellyfinServerName}} e pronto.\n\nIl pulsante di accesso diretto viene aggiunto automaticamente sotto questo messaggio.",
		VerifyButtonLabel:        "Verifica la mia e-mail",
		ExpiryDateLabel:          "Data",
		ExpiresInLabel:           "Scade tra",
		CreateAccountButtonLabel: "Crea il mio account",
		DirectLinkLabel:          "Link diretto",
		ResetPasswordButtonLabel: "Reimposta la password",
		CodeLabel:                "Codice",
		OpenServerButtonLabel:    "Apri {{.JellyfinServerName}}",
		DirectAccessLabel:        "Accesso diretto",
		PreviewDuration:          "15 minuti",
		PreviewMessage:           "Il tuo accesso a {{.JellyfinServerName}} e pronto. Usa i link qui sotto.",
		AutomaticFooter:          "Questo è un messaggio automatico inviato da JellyGate.",
	},
	"nl": {
		ConfirmationSubject:      `Toegang tot {{.JellyfinServerName}} geactiveerd`,
		ConfirmationBody:         "Hallo {{.Username}},\n\nJe toegang tot {{.JellyfinServerName}} is nu actief.\n\nJe kunt inloggen wanneer je wilt. Hulp vind je via {{.HelpURL}}.",
		EmailVerificationSubject: `Bevestig je e-mail voor {{.JellyfinServerName}}`,
		EmailVerificationBody:    "Hallo {{.Username}},\n\nBevestig je e-mailadres om je toegang tot {{.JellyfinServerName}} te beveiligen.\n\nDe bevestigingsknop en geldigheidsduur worden automatisch onder dit bericht toegevoegd.",
		ExpiryReminderSubject:    `Verloopherinnering voor {{.JellyfinServerName}}`,
		ExpiryReminderBody:       "Hallo {{.Username}},\n\nDit is een herinnering: je toegang tot {{.JellyfinServerName}} verloopt binnenkort.\n\nDe exacte datum wordt automatisch in deze e-mail toegevoegd.",
		InvitationSubject:        `Uitnodiging voor {{.JellyfinServerName}}`,
		InvitationBody:           "Hallo,\n\nJe bent uitgenodigd om lid te worden van {{.JellyfinServerName}}.\n\nDe knop om je account te maken en de directe link worden automatisch onder dit bericht toegevoegd.",
		InviteExpirySubject:      `Uitnodigingslink voor {{.JellyfinServerName}} verloopt binnenkort`,
		InviteExpiryBody:         "Hallo,\n\nJe uitnodigingslink voor {{.JellyfinServerName}} verloopt binnenkort.\n\nDe deadline wordt automatisch in deze e-mail toegevoegd.",
		PasswordResetSubject:     `Wachtwoord van {{.JellyfinServerName}} opnieuw instellen`,
		PasswordResetBody:        "Hallo {{.Username}},\n\nWe hebben een verzoek ontvangen om het wachtwoord van je {{.JellyfinServerName}}-account opnieuw in te stellen.\n\nDe resetknop, directe link en code worden automatisch onder dit bericht toegevoegd.",
		UserCreationSubject:      `{{.serveurname}}-account aangemaakt`,
		UserCreationBody:         "Hallo {{.Username}},\n\nEen beheerder heeft je {{.serveurname}}-account aangemaakt.\n\nJe kunt nu inloggen met de gegevens die je hebt ontvangen.",
		UserDeletionSubject:      `{{.JellyfinServerName}}-account verwijderd`,
		UserDeletionBody:         "Hallo {{.Username}},\n\nJe {{.JellyfinServerName}}-account is verwijderd.\n\nNeem snel contact op met de beheerders als dit onverwacht is.",
		UserDisabledSubject:      `Toegang tot {{.JellyfinServerName}} uitgeschakeld`,
		UserDisabledBody:         "Hallo {{.Username}},\n\nJe toegang tot {{.JellyfinServerName}} is tijdelijk uitgeschakeld.\n\nAls je denkt dat dit een fout is, neem dan contact op met de beheerders.",
		UserEnabledSubject:       `Toegang tot {{.JellyfinServerName}} opnieuw ingeschakeld`,
		UserEnabledBody:          "Hallo {{.Username}},\n\nGoed nieuws: je toegang tot {{.JellyfinServerName}} is hersteld.\n\nJe kunt meteen opnieuw inloggen.",
		UserExpiredSubject:       `Toegang tot {{.JellyfinServerName}} verlopen`,
		UserExpiredBody:          "Hallo {{.Username}},\n\nJe toegang tot {{.JellyfinServerName}} is verlopen en je account is automatisch uitgeschakeld.\n\nNeem contact op met de beheerders als je weer toegang nodig hebt.",
		ExpiryAdjustedSubject:    `Vervaldatum van {{.JellyfinServerName}} bijgewerkt`,
		ExpiryAdjustedBody:       "Hallo {{.Username}},\n\nDe vervaldatum van je toegang tot {{.JellyfinServerName}} is bijgewerkt.\n\nDe nieuwe datum wordt automatisch in deze e-mail toegevoegd.",
		WelcomeSubject:           `Welkom bij {{.JellyfinServerName}}`,
		WelcomeBody:              "Hallo {{.Username}},\n\nJe {{.JellyfinServerName}}-account is klaar.\n\nDe knop voor directe toegang wordt automatisch onder dit bericht toegevoegd.",
		VerifyButtonLabel:        "Bevestig mijn e-mail",
		ExpiryDateLabel:          "Datum",
		ExpiresInLabel:           "Verloopt over",
		CreateAccountButtonLabel: "Maak mijn account",
		DirectLinkLabel:          "Directe link",
		ResetPasswordButtonLabel: "Wachtwoord resetten",
		CodeLabel:                "Code",
		OpenServerButtonLabel:    "Open {{.JellyfinServerName}}",
		DirectAccessLabel:        "Directe toegang",
		PreviewDuration:          "15 minuten",
		PreviewMessage:           "Je toegang tot {{.JellyfinServerName}} is klaar. Gebruik de links hieronder.",
		AutomaticFooter:          "Dit is een automatisch bericht verzonden door JellyGate.",
	},
	"pl": {
		ConfirmationSubject:      `Dostep do {{.JellyfinServerName}} aktywowany`,
		ConfirmationBody:         "Czesc {{.Username}},\n\nTwoj dostep do {{.JellyfinServerName}} jest teraz aktywny.\n\nMozesz zalogowac sie w dowolnym momencie. Pomoc znajdziesz pod {{.HelpURL}}.",
		EmailVerificationSubject: `Potwierdz adres e-mail dla {{.JellyfinServerName}}`,
		EmailVerificationBody:    "Czesc {{.Username}},\n\nPotwierdz swoj adres e-mail, aby zabezpieczyc dostep do {{.JellyfinServerName}}.\n\nPrzycisk potwierdzenia i czas waznosci sa dodawane automatycznie pod ta wiadomoscia.",
		ExpiryReminderSubject:    `Przypomnienie o wygasnieciu dostepu do {{.JellyfinServerName}}`,
		ExpiryReminderBody:       "Czesc {{.Username}},\n\nTo szybkie przypomnienie: Twoj dostep do {{.JellyfinServerName}} wkrotce wygasnie.\n\nDokladna data jest dodawana automatycznie w tym e-mailu.",
		InvitationSubject:        `Zaproszenie do {{.JellyfinServerName}}`,
		InvitationBody:           "Czesc,\n\nOtrzymales zaproszenie do {{.JellyfinServerName}}.\n\nPrzycisk tworzenia konta i bezposredni link sa dodawane automatycznie pod ta wiadomoscia.",
		InviteExpirySubject:      `Link zaproszenia do {{.JellyfinServerName}} wkrotce wygasnie`,
		InviteExpiryBody:         "Czesc,\n\nTwoj link zaproszenia do {{.JellyfinServerName}} wkrotce wygasnie.\n\nTermin jest dodawany automatycznie w tym e-mailu.",
		PasswordResetSubject:     `Reset hasla do {{.JellyfinServerName}}`,
		PasswordResetBody:        "Czesc {{.Username}},\n\nOtrzymalismy prosbe o reset hasla do Twojego konta {{.JellyfinServerName}}.\n\nPrzycisk resetu, bezposredni link i kod sa dodawane automatycznie pod ta wiadomoscia.",
		UserCreationSubject:      `Konto {{.serveurname}} utworzone`,
		UserCreationBody:         "Czesc {{.Username}},\n\nAdministrator utworzyl Twoje konto {{.serveurname}}.\n\nMozesz teraz zalogowac sie uzywajac otrzymanych danych.",
		UserDeletionSubject:      `Konto {{.JellyfinServerName}} usuniete`,
		UserDeletionBody:         "Czesc {{.Username}},\n\nTwoje konto {{.JellyfinServerName}} zostalo usuniete.\n\nJesli to niespodziewane, skontaktuj sie szybko z administratorami.",
		UserDisabledSubject:      `Dostep do {{.JellyfinServerName}} wylaczony`,
		UserDisabledBody:         "Czesc {{.Username}},\n\nTwoj dostep do {{.JellyfinServerName}} zostal tymczasowo wylaczony.\n\nJesli uwazasz, ze to blad, skontaktuj sie z administratorami.",
		UserEnabledSubject:       `Dostep do {{.JellyfinServerName}} przywrocony`,
		UserEnabledBody:          "Czesc {{.Username}},\n\nDobra wiadomosc: Twoj dostep do {{.JellyfinServerName}} zostal przywrocony.\n\nMozesz zalogowac sie ponownie od razu.",
		UserExpiredSubject:       `Dostep do {{.JellyfinServerName}} wygasl`,
		UserExpiredBody:          "Czesc {{.Username}},\n\nTwoj dostep do {{.JellyfinServerName}} wygasl, a konto zostalo automatycznie wylaczone.\n\nSkontaktuj sie z administratorami, jesli chcesz odzyskac dostep.",
		ExpiryAdjustedSubject:    `Data wygasniecia dostepu do {{.JellyfinServerName}} zaktualizowana`,
		ExpiryAdjustedBody:       "Czesc {{.Username}},\n\nData wygasniecia Twojego dostepu do {{.JellyfinServerName}} zostala zaktualizowana.\n\nNowa data jest dodawana automatycznie w tym e-mailu.",
		WelcomeSubject:           `Witaj w {{.JellyfinServerName}}`,
		WelcomeBody:              "Czesc {{.Username}},\n\nTwoje konto {{.JellyfinServerName}} jest gotowe.\n\nPrzycisk bezposredniego dostepu jest dodawany automatycznie pod ta wiadomoscia.",
		VerifyButtonLabel:        "Potwierdz moj e-mail",
		ExpiryDateLabel:          "Data",
		ExpiresInLabel:           "Wygasa za",
		CreateAccountButtonLabel: "Utworz moje konto",
		DirectLinkLabel:          "Link bezposredni",
		ResetPasswordButtonLabel: "Resetuj haslo",
		CodeLabel:                "Kod",
		OpenServerButtonLabel:    "Otworz {{.JellyfinServerName}}",
		DirectAccessLabel:        "Dostep bezposredni",
		PreviewDuration:          "15 minut",
		PreviewMessage:           "Twoj dostep do {{.JellyfinServerName}} jest gotowy. Uzyj ponizszych linkow.",
		AutomaticFooter:          "To jest automatyczna wiadomość wysłana przez JellyGate.",
	},
	"pt-br": {
		ConfirmationSubject:      `Acesso ao {{.JellyfinServerName}} ativado`,
		ConfirmationBody:         "Ola {{.Username}},\n\nSeu acesso ao {{.JellyfinServerName}} agora esta ativo.\n\nVoce pode entrar quando quiser. Se precisar de ajuda, use {{.HelpURL}}.",
		EmailVerificationSubject: `Confirme seu e-mail para {{.JellyfinServerName}}`,
		EmailVerificationBody:    "Ola {{.Username}},\n\nConfirme seu endereco de e-mail para proteger seu acesso ao {{.JellyfinServerName}}.\n\nO botao de confirmacao e o prazo sao adicionados automaticamente abaixo desta mensagem.",
		ExpiryReminderSubject:    `Lembrete de expiracao para {{.JellyfinServerName}}`,
		ExpiryReminderBody:       "Ola {{.Username}},\n\nEste e um lembrete rapido: seu acesso ao {{.JellyfinServerName}} vai expirar em breve.\n\nA data exata e adicionada automaticamente neste e-mail.",
		InvitationSubject:        `Convite para entrar no {{.JellyfinServerName}}`,
		InvitationBody:           "Ola,\n\nVoce recebeu um convite para entrar no {{.JellyfinServerName}}.\n\nO botao para criar a conta e o link direto sao adicionados automaticamente abaixo desta mensagem.",
		InviteExpirySubject:      `Link de convite do {{.JellyfinServerName}} expirando em breve`,
		InviteExpiryBody:         "Ola,\n\nSeu link de convite para o {{.JellyfinServerName}} vai expirar em breve.\n\nO prazo e adicionado automaticamente neste e-mail.",
		PasswordResetSubject:     `Redefina sua senha do {{.JellyfinServerName}}`,
		PasswordResetBody:        "Ola {{.Username}},\n\nRecebemos um pedido para redefinir a senha da sua conta {{.JellyfinServerName}}.\n\nO botao de redefinicao, o link direto e o codigo sao adicionados automaticamente abaixo desta mensagem.",
		UserCreationSubject:      `Conta {{.serveurname}} criada`,
		UserCreationBody:         "Ola {{.Username}},\n\nUm administrador criou sua conta {{.serveurname}}.\n\nAgora voce pode entrar com os dados recebidos.",
		UserDeletionSubject:      `Conta {{.JellyfinServerName}} removida`,
		UserDeletionBody:         "Ola {{.Username}},\n\nSua conta {{.JellyfinServerName}} foi removida.\n\nSe isso parecer inesperado, fale rapidamente com os administradores.",
		UserDisabledSubject:      `Acesso ao {{.JellyfinServerName}} desativado`,
		UserDisabledBody:         "Ola {{.Username}},\n\nSeu acesso ao {{.JellyfinServerName}} foi desativado temporariamente.\n\nSe voce acha que isso e um erro, fale com os administradores.",
		UserEnabledSubject:       `Acesso ao {{.JellyfinServerName}} reativado`,
		UserEnabledBody:          "Ola {{.Username}},\n\nBoa noticia: seu acesso ao {{.JellyfinServerName}} foi restaurado.\n\nVoce pode entrar novamente agora mesmo.",
		UserExpiredSubject:       `Acesso ao {{.JellyfinServerName}} expirado`,
		UserExpiredBody:          "Ola {{.Username}},\n\nSeu acesso ao {{.JellyfinServerName}} expirou e sua conta foi desativada automaticamente.\n\nFale com os administradores se precisar recuperar o acesso.",
		ExpiryAdjustedSubject:    `Expiracao do acesso ao {{.JellyfinServerName}} atualizada`,
		ExpiryAdjustedBody:       "Ola {{.Username}},\n\nA data de expiracao do seu acesso ao {{.JellyfinServerName}} foi atualizada.\n\nA nova data e adicionada automaticamente neste e-mail.",
		WelcomeSubject:           `Bem-vindo ao {{.JellyfinServerName}}`,
		WelcomeBody:              "Ola {{.Username}},\n\nSua conta {{.JellyfinServerName}} esta pronta.\n\nO botao de acesso direto e adicionado automaticamente abaixo desta mensagem.",
		VerifyButtonLabel:        "Confirmar meu e-mail",
		ExpiryDateLabel:          "Data",
		ExpiresInLabel:           "Expira em",
		CreateAccountButtonLabel: "Criar minha conta",
		DirectLinkLabel:          "Link direto",
		ResetPasswordButtonLabel: "Redefinir minha senha",
		CodeLabel:                "Codigo",
		OpenServerButtonLabel:    "Abrir {{.JellyfinServerName}}",
		DirectAccessLabel:        "Acesso direto",
		PreviewDuration:          "15 minutos",
		PreviewMessage:           "Seu acesso ao {{.JellyfinServerName}} esta pronto. Use os links abaixo.",
		AutomaticFooter:          "Esta é uma mensagem automática enviada pelo JellyGate.",
	},
	"ru": {
		ConfirmationSubject:      `Доступ к {{.JellyfinServerName}} активирован`,
		ConfirmationBody:         "Здравствуйте, {{.Username}}.\n\nВаш доступ к {{.JellyfinServerName}} уже активирован.\n\nВы можете войти в любое время. Если нужна помощь, откройте {{.HelpURL}}.",
		EmailVerificationSubject: `Подтвердите e-mail для {{.JellyfinServerName}}`,
		EmailVerificationBody:    "Здравствуйте, {{.Username}}.\n\nПодтвердите ваш адрес e-mail, чтобы защитить доступ к {{.JellyfinServerName}}.\n\nКнопка подтверждения и срок действия автоматически добавляются ниже.",
		ExpiryReminderSubject:    `Скоро истечет доступ к {{.JellyfinServerName}}`,
		ExpiryReminderBody:       "Здравствуйте, {{.Username}}.\n\nНапоминаем, что ваш доступ к {{.JellyfinServerName}} скоро истечет.\n\nТочная дата автоматически добавляется в это письмо.",
		InvitationSubject:        `Приглашение в {{.JellyfinServerName}}`,
		InvitationBody:           "Здравствуйте.\n\nВы получили приглашение в {{.JellyfinServerName}}.\n\nКнопка создания аккаунта и прямая ссылка автоматически добавляются ниже.",
		InviteExpirySubject:      `Ссылка-приглашение в {{.JellyfinServerName}} скоро истечет`,
		InviteExpiryBody:         "Здравствуйте.\n\nСрок действия вашей ссылки-приглашения в {{.JellyfinServerName}} скоро закончится.\n\nКрайний срок автоматически добавляется в это письмо.",
		PasswordResetSubject:     `Сброс пароля {{.JellyfinServerName}}`,
		PasswordResetBody:        "Здравствуйте, {{.Username}}.\n\nМы получили запрос на сброс пароля для вашей учетной записи {{.JellyfinServerName}}.\n\nКнопка сброса, прямая ссылка и код автоматически добавляются ниже.",
		UserCreationSubject:      `Аккаунт {{.serveurname}} создан`,
		UserCreationBody:         "Здравствуйте, {{.Username}}.\n\nАдминистратор создал вашу учетную запись {{.serveurname}}.\n\nТеперь вы можете войти с полученными данными.",
		UserDeletionSubject:      `Аккаунт {{.JellyfinServerName}} удален`,
		UserDeletionBody:         "Здравствуйте, {{.Username}}.\n\nВаша учетная запись {{.JellyfinServerName}} была удалена.\n\nЕсли это неожиданно, пожалуйста, свяжитесь с администраторами.",
		UserDisabledSubject:      `Доступ к {{.JellyfinServerName}} отключен`,
		UserDisabledBody:         "Здравствуйте, {{.Username}}.\n\nВаш доступ к {{.JellyfinServerName}} был временно отключен.\n\nЕсли вы считаете это ошибкой, свяжитесь с администраторами.",
		UserEnabledSubject:       `Доступ к {{.JellyfinServerName}} восстановлен`,
		UserEnabledBody:          "Здравствуйте, {{.Username}}.\n\nХорошая новость: ваш доступ к {{.JellyfinServerName}} снова активен.\n\nВы можете войти снова прямо сейчас.",
		UserExpiredSubject:       `Доступ к {{.JellyfinServerName}} истек`,
		UserExpiredBody:          "Здравствуйте, {{.Username}}.\n\nСрок действия вашего доступа к {{.JellyfinServerName}} истек, и учетная запись была отключена автоматически.\n\nСвяжитесь с администраторами, если вам нужен доступ снова.",
		ExpiryAdjustedSubject:    `Срок доступа к {{.JellyfinServerName}} обновлен`,
		ExpiryAdjustedBody:       "Здравствуйте, {{.Username}}.\n\nДата окончания доступа к {{.JellyfinServerName}} была обновлена.\n\nНовая дата автоматически добавляется в это письмо.",
		WelcomeSubject:           `Добро пожаловать в {{.JellyfinServerName}}`,
		WelcomeBody:              "Здравствуйте, {{.Username}}.\n\nВаш аккаунт {{.JellyfinServerName}} готов.\n\nКнопка прямого доступа автоматически добавляется ниже.",
		VerifyButtonLabel:        "Подтвердить e-mail",
		ExpiryDateLabel:          "Дата",
		ExpiresInLabel:           "Истекает через",
		CreateAccountButtonLabel: "Создать аккаунт",
		DirectLinkLabel:          "Прямая ссылка",
		ResetPasswordButtonLabel: "Сбросить пароль",
		CodeLabel:                "Код",
		OpenServerButtonLabel:    "Открыть {{.JellyfinServerName}}",
		DirectAccessLabel:        "Прямой доступ",
		PreviewDuration:          "15 минут",
		PreviewMessage:           "Ваш доступ к {{.JellyfinServerName}} готов. Используйте ссылки ниже.",
		AutomaticFooter:          "Это автоматическое сообщение, отправленное JellyGate.",
	},
	"zh": {
		ConfirmationSubject:      `已启用 {{.JellyfinServerName}} 访问`,
		ConfirmationBody:         "你好 {{.Username}}，\n\n你现在已经可以访问 {{.JellyfinServerName}}。\n\n你可以随时登录。如需帮助，请打开 {{.HelpURL}}。",
		EmailVerificationSubject: `请验证你的 {{.JellyfinServerName}} 邮箱`,
		EmailVerificationBody:    "你好 {{.Username}}，\n\n请验证你的邮箱地址，以保护你对 {{.JellyfinServerName}} 的访问。\n\n验证按钮和有效期会自动显示在这条消息下方。",
		ExpiryReminderSubject:    `{{.JellyfinServerName}} 访问即将过期`,
		ExpiryReminderBody:       "你好 {{.Username}}，\n\n提醒一下：你对 {{.JellyfinServerName}} 的访问即将过期。\n\n准确日期会自动显示在这封邮件中。",
		InvitationSubject:        `加入 {{.JellyfinServerName}} 的邀请`,
		InvitationBody:           "你好，\n\n你收到了加入 {{.JellyfinServerName}} 的邀请。\n\n创建账号按钮和直接链接会自动显示在这条消息下方。",
		InviteExpirySubject:      `{{.JellyfinServerName}} 邀请链接即将过期`,
		InviteExpiryBody:         "你好，\n\n你的 {{.JellyfinServerName}} 邀请链接即将过期。\n\n截止时间会自动显示在这封邮件中。",
		PasswordResetSubject:     `重置 {{.JellyfinServerName}} 密码`,
		PasswordResetBody:        "你好 {{.Username}}，\n\n我们收到了重置你的 {{.JellyfinServerName}} 账号密码的请求。\n\n重置按钮、直接链接和验证码会自动显示在这条消息下方。",
		UserCreationSubject:      `已创建 {{.serveurname}} 账号`,
		UserCreationBody:         "你好 {{.Username}}，\n\n管理员已经为你创建了 {{.serveurname}} 账号。\n\n你现在可以使用收到的信息登录。",
		UserDeletionSubject:      `{{.JellyfinServerName}} 账号已删除`,
		UserDeletionBody:         "你好 {{.Username}}，\n\n你的 {{.JellyfinServerName}} 账号已被删除。\n\n如果这不是你预期的，请尽快联系管理员。",
		UserDisabledSubject:      `{{.JellyfinServerName}} 访问已停用`,
		UserDisabledBody:         "你好 {{.Username}}，\n\n你对 {{.JellyfinServerName}} 的访问已被临时停用。\n\n如果你认为这是错误，请联系管理员。",
		UserEnabledSubject:       `{{.JellyfinServerName}} 访问已恢复`,
		UserEnabledBody:          "你好 {{.Username}}，\n\n好消息：你对 {{.JellyfinServerName}} 的访问已经恢复。\n\n你现在可以重新登录。",
		UserExpiredSubject:       `{{.JellyfinServerName}} 访问已过期`,
		UserExpiredBody:          "你好 {{.Username}}，\n\n你对 {{.JellyfinServerName}} 的访问已过期，账号也已被自动停用。\n\n如果你需要重新获得访问权限，请联系管理员。",
		ExpiryAdjustedSubject:    `{{.JellyfinServerName}} 访问到期时间已更新`,
		ExpiryAdjustedBody:       "你好 {{.Username}}，\n\n你对 {{.JellyfinServerName}} 的访问到期时间已更新。\n\n新的日期会自动显示在这封邮件中。",
		WelcomeSubject:           `欢迎使用 {{.JellyfinServerName}}`,
		WelcomeBody:              "你好 {{.Username}}，\n\n你的 {{.JellyfinServerName}} 账号已经准备好了。\n\n直接访问按钮会自动显示在这条消息下方。",
		VerifyButtonLabel:        "验证我的邮箱",
		ExpiryDateLabel:          "日期",
		ExpiresInLabel:           "将在以下时间后过期",
		CreateAccountButtonLabel: "创建我的账号",
		DirectLinkLabel:          "直接链接",
		ResetPasswordButtonLabel: "重置我的密码",
		CodeLabel:                "验证码",
		OpenServerButtonLabel:    "打开 {{.JellyfinServerName}}",
		DirectAccessLabel:        "直接访问",
		PreviewDuration:          "15 分钟",
		PreviewMessage:           "你对 {{.JellyfinServerName}} 的访问已经准备好了。请使用下面的链接。",
		AutomaticFooter:          "这是由 JellyGate 发送的自动消息。",
	},
}

func SupportedLanguageTags() []string {
	langs := make([]string, len(SupportedLanguageOrder))
	copy(langs, SupportedLanguageOrder)
	return langs
}

func emailTextPackFor(lang string) emailTextPack {
	normalized := NormalizeLanguageTag(lang)
	if pack, ok := emailTextPacks[normalized]; ok {
		return pack
	}
	if pack, ok := emailTextPacks["en"]; ok {
		return pack
	}
	return emailTextPacks["fr"]
}

func DefaultNoCodeEmailTemplateBodyForLanguage(lang, key string) string {
	pack := emailTextPackFor(lang)
	switch strings.TrimSpace(key) {
	case "confirmation":
		return pack.ConfirmationBody
	case "email_verification":
		return pack.EmailVerificationBody
	case "expiry_reminder":
		return pack.ExpiryReminderBody
	case "invitation":
		return pack.InvitationBody
	case "invite_expiry":
		return pack.InviteExpiryBody
	case "password_reset":
		return pack.PasswordResetBody
	case "user_creation":
		return pack.UserCreationBody
	case "user_deletion":
		return pack.UserDeletionBody
	case "user_disabled":
		return pack.UserDisabledBody
	case "user_enabled":
		return pack.UserEnabledBody
	case "user_expired":
		return pack.UserExpiredBody
	case "expiry_adjusted":
		return pack.ExpiryAdjustedBody
	case "welcome":
		return pack.WelcomeBody
	default:
		return ""
	}
}

func DefaultEmailTemplateSubjectForLanguage(lang, key string) string {
	pack := emailTextPackFor(lang)
	switch strings.TrimSpace(key) {
	case "confirmation":
		return pack.ConfirmationSubject
	case "email_verification":
		return pack.EmailVerificationSubject
	case "expiry_reminder":
		return pack.ExpiryReminderSubject
	case "invitation":
		return pack.InvitationSubject
	case "invite_expiry":
		return pack.InviteExpirySubject
	case "password_reset":
		return pack.PasswordResetSubject
	case "user_creation":
		return pack.UserCreationSubject
	case "user_deletion":
		return pack.UserDeletionSubject
	case "user_disabled":
		return pack.UserDisabledSubject
	case "user_enabled":
		return pack.UserEnabledSubject
	case "user_expired":
		return pack.UserExpiredSubject
	case "expiry_adjusted":
		return pack.ExpiryAdjustedSubject
	case "welcome":
		return pack.WelcomeSubject
	default:
		return ""
	}
}

func automaticEmailBlockForLanguage(lang, templateKey string) string {
	pack := emailTextPackFor(lang)
	switch strings.TrimSpace(templateKey) {
	case "email_verification":
		return `
<div style="margin:22px 0 0 0;">
	<a href="{{.VerificationLink}}" style="display:inline-block;background:#0ea5e9;color:#ffffff;text-decoration:none;padding:12px 18px;border-radius:999px;font-weight:700;">` + html.EscapeString(pack.VerifyButtonLabel) + `</a>
</div>
<p style="font-size:13px;color:#475569;">` + html.EscapeString(pack.ExpiresInLabel) + ` {{.ExpiresIn}}</p>`
	case "expiry_reminder", "invite_expiry", "expiry_adjusted":
		return `
<div style="margin:22px 0 0 0;padding:14px 16px;border:1px solid #dbe4f0;border-radius:14px;background:#f8fafc;color:#0f172a;">
	<div style="font-size:12px;letter-spacing:0.06em;text-transform:uppercase;color:#64748b;margin-bottom:6px;">` + html.EscapeString(pack.ExpiryDateLabel) + `</div>
	<div style="font-size:16px;font-weight:700;">{{.ExpiryDate}}</div>
</div>`
	case "invitation":
		return `
<div style="margin:22px 0 0 0;">
	<a href="{{.InviteLink}}" style="display:inline-block;background:#0ea5e9;color:#ffffff;text-decoration:none;padding:12px 18px;border-radius:999px;font-weight:700;">` + html.EscapeString(pack.CreateAccountButtonLabel) + `</a>
</div>`
	case "password_reset":
		return `
<div style="margin:22px 0 0 0;">
	<a href="{{.ResetLink}}" style="display:inline-block;background:#0ea5e9;color:#ffffff;text-decoration:none;padding:12px 18px;border-radius:999px;font-weight:700;">` + html.EscapeString(pack.ResetPasswordButtonLabel) + `</a>
</div>
<p style="font-size:13px;color:#475569;">` + html.EscapeString(pack.ExpiresInLabel) + ` {{.ExpiresIn}}</p>`
	case "welcome":
		return `
<div style="margin:22px 0 0 0;">
	<a href="{{.JellyfinURL}}" style="display:inline-block;background:#0ea5e9;color:#ffffff;text-decoration:none;padding:12px 18px;border-radius:999px;font-weight:700;">` + html.EscapeString(pack.OpenServerButtonLabel) + `</a>
</div>`
	default:
		return ""
	}
}

func DefaultEmailPreviewDurationForLanguage(lang string) string {
	return emailTextPackFor(lang).PreviewDuration
}

func DefaultEmailPreviewMessageForLanguage(lang string) string {
	return emailTextPackFor(lang).PreviewMessage
}

func DefaultEmailAutomaticFooterForLanguage(lang string) string {
	return emailTextPackFor(lang).AutomaticFooter
}

func DefaultEmailTemplatesForLanguage(lang string) EmailTemplatesConfig {
	normalized := NormalizeLanguageTag(lang)
	if normalized == "" {
		normalized = "fr"
	}
	header := DefaultEmailBaseHeader()
	footer := DefaultEmailBaseFooter()

	return EmailTemplatesConfig{
		BaseTemplateHeader:          header,
		BaseTemplateFooter:          footer,
		EmailLogoURL:                defaultEmailLogoPath,
		Confirmation:                PrepareEmailTemplateBodyForLanguage(normalized, "confirmation", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "confirmation"), header, footer),
		ConfirmationSubject:         DefaultEmailTemplateSubjectForLanguage(normalized, "confirmation"),
		DisableConfirmationEmail:    false,
		EmailVerification:           PrepareEmailTemplateBodyForLanguage(normalized, "email_verification", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "email_verification"), header, footer),
		EmailVerificationSubject:    DefaultEmailTemplateSubjectForLanguage(normalized, "email_verification"),
		ExpiryReminder:              PrepareEmailTemplateBodyForLanguage(normalized, "expiry_reminder", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "expiry_reminder"), header, footer),
		ExpiryReminderSubject:       DefaultEmailTemplateSubjectForLanguage(normalized, "expiry_reminder"),
		DisableExpiryReminderEmails: false,
		ExpiryReminderDays:          3,
		ExpiryReminder14:            PrepareEmailTemplateBodyForLanguage(normalized, "expiry_reminder", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "expiry_reminder"), header, footer),
		ExpiryReminder7:             PrepareEmailTemplateBodyForLanguage(normalized, "expiry_reminder", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "expiry_reminder"), header, footer),
		ExpiryReminder1:             PrepareEmailTemplateBodyForLanguage(normalized, "expiry_reminder", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "expiry_reminder"), header, footer),
		Invitation:                  PrepareEmailTemplateBodyForLanguage(normalized, "invitation", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "invitation"), header, footer),
		InvitationSubject:           DefaultEmailTemplateSubjectForLanguage(normalized, "invitation"),
		InviteExpiry:                PrepareEmailTemplateBodyForLanguage(normalized, "invite_expiry", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "invite_expiry"), header, footer),
		InviteExpirySubject:         DefaultEmailTemplateSubjectForLanguage(normalized, "invite_expiry"),
		DisableInviteExpiryEmail:    false,
		PasswordReset:               PrepareEmailTemplateBodyForLanguage(normalized, "password_reset", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "password_reset"), header, footer),
		PasswordResetSubject:        DefaultEmailTemplateSubjectForLanguage(normalized, "password_reset"),
		PreSignupHelp:               "",
		DisablePreSignupHelpEmail:   true,
		PostSignupHelp:              "",
		DisablePostSignupHelpEmail:  true,
		UserCreation:                PrepareEmailTemplateBodyForLanguage(normalized, "user_creation", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "user_creation"), header, footer),
		UserCreationSubject:         DefaultEmailTemplateSubjectForLanguage(normalized, "user_creation"),
		DisableUserCreationEmail:    false,
		UserDeletion:                PrepareEmailTemplateBodyForLanguage(normalized, "user_deletion", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "user_deletion"), header, footer),
		UserDeletionSubject:         DefaultEmailTemplateSubjectForLanguage(normalized, "user_deletion"),
		DisableUserDeletionEmail:    false,
		UserDisabled:                PrepareEmailTemplateBodyForLanguage(normalized, "user_disabled", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "user_disabled"), header, footer),
		UserDisabledSubject:         DefaultEmailTemplateSubjectForLanguage(normalized, "user_disabled"),
		DisableUserDisabledEmail:    false,
		UserEnabled:                 PrepareEmailTemplateBodyForLanguage(normalized, "user_enabled", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "user_enabled"), header, footer),
		UserEnabledSubject:          DefaultEmailTemplateSubjectForLanguage(normalized, "user_enabled"),
		DisableUserEnabledEmail:     false,
		UserExpired:                 PrepareEmailTemplateBodyForLanguage(normalized, "user_expired", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "user_expired"), header, footer),
		UserExpiredSubject:          DefaultEmailTemplateSubjectForLanguage(normalized, "user_expired"),
		DisableUserExpiredEmail:     false,
		ExpiryAdjusted:              PrepareEmailTemplateBodyForLanguage(normalized, "expiry_adjusted", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "expiry_adjusted"), header, footer),
		ExpiryAdjustedSubject:       DefaultEmailTemplateSubjectForLanguage(normalized, "expiry_adjusted"),
		DisableExpiryAdjustedEmail:  false,
		Welcome:                     PrepareEmailTemplateBodyForLanguage(normalized, "welcome", DefaultNoCodeEmailTemplateBodyForLanguage(normalized, "welcome"), header, footer),
		WelcomeSubject:              DefaultEmailTemplateSubjectForLanguage(normalized, "welcome"),
		DisableWelcomeEmail:         false,
	}
}
