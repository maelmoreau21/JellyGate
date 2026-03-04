// Package ldap fournit un client LDAPS pour interagir avec Active Directory.
//
// Opérations supportées :
//   - Connexion sécurisée LDAPS (port 636, TLS)
//   - Création d'utilisateur (sAMAccountName, userPrincipalName, displayName)
//   - Définition du mot de passe (unicodePwd en UTF-16LE entre guillemets)
//   - Activation du compte (userAccountControl = 512)
//   - Désactivation du compte (userAccountControl = 514)
//   - Suppression d'utilisateur
//   - Recherche d'utilisateur
//
// Contrainte : la définition du mot de passe via unicodePwd nécessite
// impérativement une connexion LDAPS (port 636) — LDAP simple ne fonctionne pas.
//
// Chaque méthode retourne des erreurs explicites pour permettre le rollback
// lors du flux de création atomique (invitation).
package ldap

import (
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"log/slog"
	"unicode/utf16"

	goldap "github.com/go-ldap/ldap/v3"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

// ── Constantes Active Directory ─────────────────────────────────────────────

const (
	// UAC_NORMAL_ACCOUNT = 512 : compte utilisateur normal activé.
	UAC_NORMAL_ACCOUNT = 512

	// UAC_DISABLED_ACCOUNT = 514 : compte utilisateur désactivé (512 + 2).
	UAC_DISABLED_ACCOUNT = 514

	// objectClassUser est la classe d'objet pour un utilisateur AD.
	objectClassUser = "user"

	// objectClassPerson est la classe d'objet hérité pour un utilisateur.
	objectClassPerson = "person"

	// objectClassOrgPerson est la classe d'objet hérité pour un utilisateur.
	objectClassOrgPerson = "organizationalPerson"

	// objectClassTop est la classe racine de tous les objets AD.
	objectClassTop = "top"
)

// ── Client ──────────────────────────────────────────────────────────────────

// Client encapsule la connexion LDAPS à Active Directory.
type Client struct {
	cfg config.LDAPConfig
}

// New crée un nouveau client LDAP à partir de la configuration.
// La connexion n'est pas établie immédiatement — elle est créée à chaque opération
// pour éviter les problèmes de connexion stale. Chaque opération ouvre,
// utilise, et ferme proprement la connexion.
func New(cfg config.LDAPConfig) *Client {
	return &Client{cfg: cfg}
}

// ── Connexion ───────────────────────────────────────────────────────────────

// connect établit une connexion LDAPS authentifiée au serveur.
// L'appelant DOIT appeler conn.Close() après utilisation.
func (c *Client) connect() (*goldap.Conn, error) {
	addr := fmt.Sprintf("%s:%d", c.cfg.Host, c.cfg.Port)

	var conn *goldap.Conn
	var err error

	if c.cfg.UseTLS {
		// Connexion LDAPS (TLS direct sur le port 636)
		tlsConfig := &tls.Config{
			InsecureSkipVerify: c.cfg.SkipVerify,
			ServerName:         c.cfg.Host,
		}
		conn, err = goldap.DialTLS("tcp", addr, tlsConfig)
	} else {
		// Connexion LDAP simple (non recommandé en production)
		conn, err = goldap.Dial("tcp", addr)
	}

	if err != nil {
		return nil, fmt.Errorf("ldap.connect: impossible de se connecter à %s: %w", addr, err)
	}

	// Bind avec les identifiants de service
	if err := conn.Bind(c.cfg.BindDN, c.cfg.BindPassword); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ldap.connect: échec du bind avec %q: %w", c.cfg.BindDN, err)
	}

	return conn, nil
}

// ── Création d'utilisateur ──────────────────────────────────────────────────

// CreateUser crée un nouvel utilisateur dans l'Active Directory.
//
// Paramètres :
//   - username    : nom de connexion (sAMAccountName)
//   - displayName : nom affiché
//   - email       : adresse email (optionnel, chaîne vide si absent)
//   - password    : mot de passe en clair (sera encodé en UTF-16LE)
//
// Le compte est créé EN ACTIVÉ (userAccountControl = 512).
//
// Retourne le DN (Distinguished Name) de l'utilisateur créé.
// En cas d'erreur, aucun nettoyage n'est nécessaire côté LDAP (l'ADD est atomique).
func (c *Client) CreateUser(username, displayName, email, password string) (string, error) {
	conn, err := c.connect()
	if err != nil {
		return "", fmt.Errorf("ldap.CreateUser: %w", err)
	}
	defer conn.Close()

	// Construire le DN de l'utilisateur
	// Ex: CN=jdoe,CN=Users,DC=home,DC=lan
	userDN := fmt.Sprintf("CN=%s,%s,%s",
		goldap.EscapeDN(username),
		c.cfg.UserOU,
		c.cfg.BaseDN,
	)

	// Construire le userPrincipalName : jdoe@home.lan
	upn := fmt.Sprintf("%s@%s", username, c.cfg.Domain)

	// Encoder le mot de passe en UTF-16LE (requis par Active Directory)
	encodedPassword, err := encodeADPassword(password)
	if err != nil {
		return "", fmt.Errorf("ldap.CreateUser: %w", err)
	}

	// Construire la requête d'ajout
	addReq := goldap.NewAddRequest(userDN, nil)

	// Classes d'objet requises pour un utilisateur AD
	addReq.Attribute("objectClass", []string{
		objectClassTop,
		objectClassPerson,
		objectClassOrgPerson,
		objectClassUser,
	})

	// Attributs obligatoires
	addReq.Attribute("sAMAccountName", []string{username})
	addReq.Attribute("userPrincipalName", []string{upn})
	addReq.Attribute("displayName", []string{displayName})
	addReq.Attribute("cn", []string{username})
	addReq.Attribute("name", []string{displayName})

	// Email (si fourni)
	if email != "" {
		addReq.Attribute("mail", []string{email})
	}

	// Mot de passe — DOIT être transmis via LDAPS
	addReq.Attribute("unicodePwd", []string{string(encodedPassword)})

	// Activer le compte immédiatement (512 = NORMAL_ACCOUNT)
	addReq.Attribute("userAccountControl", []string{fmt.Sprintf("%d", UAC_NORMAL_ACCOUNT)})

	// Exécuter la création
	if err := conn.Add(addReq); err != nil {
		return "", fmt.Errorf("ldap.CreateUser: échec de la création de %q: %w", userDN, err)
	}

	slog.Info("Utilisateur AD créé", "dn", userDN, "username", username, "upn", upn)

	// Ajouter au groupe si configuré
	if c.cfg.UserGroup != "" {
		if err := c.addToGroup(conn, userDN); err != nil {
			// Log l'erreur mais ne fait pas échouer la création
			slog.Warn("Impossible d'ajouter l'utilisateur au groupe",
				"dn", userDN,
				"group", c.cfg.UserGroup,
				"error", err,
			)
		}
	}

	return userDN, nil
}

// ── Suppression d'utilisateur ───────────────────────────────────────────────

// DeleteUser supprime un utilisateur de l'AD par son DN.
//
// Utilisé lors du rollback ou de la suppression admin.
func (c *Client) DeleteUser(userDN string) error {
	if userDN == "" {
		return fmt.Errorf("ldap.DeleteUser: DN vide")
	}

	conn, err := c.connect()
	if err != nil {
		return fmt.Errorf("ldap.DeleteUser: %w", err)
	}
	defer conn.Close()

	delReq := goldap.NewDelRequest(userDN, nil)
	if err := conn.Del(delReq); err != nil {
		return fmt.Errorf("ldap.DeleteUser: échec de la suppression de %q: %w", userDN, err)
	}

	slog.Info("Utilisateur AD supprimé", "dn", userDN)
	return nil
}

// ── Activation / Désactivation ──────────────────────────────────────────────

// EnableUser active un compte AD (userAccountControl = 512).
func (c *Client) EnableUser(userDN string) error {
	return c.setUserAccountControl(userDN, UAC_NORMAL_ACCOUNT)
}

// DisableUser désactive un compte AD (userAccountControl = 514).
func (c *Client) DisableUser(userDN string) error {
	return c.setUserAccountControl(userDN, UAC_DISABLED_ACCOUNT)
}

// setUserAccountControl modifie le flag userAccountControl d'un utilisateur.
func (c *Client) setUserAccountControl(userDN string, uac int) error {
	if userDN == "" {
		return fmt.Errorf("ldap.setUserAccountControl: DN vide")
	}

	conn, err := c.connect()
	if err != nil {
		return fmt.Errorf("ldap.setUserAccountControl: %w", err)
	}
	defer conn.Close()

	modReq := goldap.NewModifyRequest(userDN, nil)
	modReq.Replace("userAccountControl", []string{fmt.Sprintf("%d", uac)})

	if err := conn.Modify(modReq); err != nil {
		return fmt.Errorf("ldap.setUserAccountControl: échec pour %q (uac=%d): %w", userDN, uac, err)
	}

	slog.Info("userAccountControl modifié", "dn", userDN, "uac", uac)
	return nil
}

// ── Modification du mot de passe ────────────────────────────────────────────

// ResetPassword réinitialise le mot de passe d'un utilisateur dans l'AD.
//
// Le mot de passe est encodé en UTF-16LE entre guillemets, comme requis
// par l'attribut unicodePwd d'Active Directory.
// Cette opération NÉCESSITE une connexion LDAPS (port 636).
func (c *Client) ResetPassword(userDN, newPassword string) error {
	if userDN == "" {
		return fmt.Errorf("ldap.ResetPassword: DN vide")
	}
	if !c.cfg.UseTLS {
		return fmt.Errorf("ldap.ResetPassword: LDAPS (TLS) est obligatoire pour modifier un mot de passe AD")
	}

	conn, err := c.connect()
	if err != nil {
		return fmt.Errorf("ldap.ResetPassword: %w", err)
	}
	defer conn.Close()

	encodedPassword, err := encodeADPassword(newPassword)
	if err != nil {
		return fmt.Errorf("ldap.ResetPassword: %w", err)
	}

	modReq := goldap.NewModifyRequest(userDN, nil)
	modReq.Replace("unicodePwd", []string{string(encodedPassword)})

	if err := conn.Modify(modReq); err != nil {
		return fmt.Errorf("ldap.ResetPassword: échec pour %q: %w", userDN, err)
	}

	slog.Info("Mot de passe AD réinitialisé", "dn", userDN)
	return nil
}

// ── Recherche ───────────────────────────────────────────────────────────────

// UserEntry contient les attributs pertinents d'un utilisateur AD.
type UserEntry struct {
	DN          string
	Username    string // sAMAccountName
	DisplayName string
	Email       string
	UPN         string // userPrincipalName
	UAC         string // userAccountControl
	IsDisabled  bool
}

// FindUser recherche un utilisateur dans l'AD par son sAMAccountName.
// Retourne nil si l'utilisateur n'est pas trouvé (pas d'erreur).
func (c *Client) FindUser(username string) (*UserEntry, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, fmt.Errorf("ldap.FindUser: %w", err)
	}
	defer conn.Close()

	searchDN := fmt.Sprintf("%s,%s", c.cfg.UserOU, c.cfg.BaseDN)
	filter := fmt.Sprintf("(&(objectClass=user)(sAMAccountName=%s))", goldap.EscapeFilter(username))

	searchReq := goldap.NewSearchRequest(
		searchDN,
		goldap.ScopeWholeSubtree,
		goldap.NeverDerefAliases,
		1,  // Limit à 1 résultat
		10, // Timeout 10 secondes
		false,
		filter,
		[]string{"dn", "sAMAccountName", "displayName", "mail", "userPrincipalName", "userAccountControl"},
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("ldap.FindUser: échec de la recherche pour %q: %w", username, err)
	}

	if len(result.Entries) == 0 {
		return nil, nil // Utilisateur non trouvé — pas une erreur
	}

	entry := result.Entries[0]
	uac := entry.GetAttributeValue("userAccountControl")

	return &UserEntry{
		DN:          entry.DN,
		Username:    entry.GetAttributeValue("sAMAccountName"),
		DisplayName: entry.GetAttributeValue("displayName"),
		Email:       entry.GetAttributeValue("mail"),
		UPN:         entry.GetAttributeValue("userPrincipalName"),
		UAC:         uac,
		IsDisabled:  uac == fmt.Sprintf("%d", UAC_DISABLED_ACCOUNT),
	}, nil
}

// ── Opérations internes ─────────────────────────────────────────────────────

// addToGroup ajoute un utilisateur à un groupe AD.
// Utilise une connexion déjà établie.
func (c *Client) addToGroup(conn *goldap.Conn, userDN string) error {
	groupDN := fmt.Sprintf("CN=%s,%s,%s", c.cfg.UserGroup, c.cfg.UserOU, c.cfg.BaseDN)

	modReq := goldap.NewModifyRequest(groupDN, nil)
	modReq.Add("member", []string{userDN})

	if err := conn.Modify(modReq); err != nil {
		return fmt.Errorf("échec de l'ajout de %q au groupe %q: %w", userDN, groupDN, err)
	}

	slog.Info("Utilisateur ajouté au groupe AD", "user_dn", userDN, "group", c.cfg.UserGroup)
	return nil
}

// ── Encodage du mot de passe AD ─────────────────────────────────────────────

// encodeADPassword encode un mot de passe pour l'attribut unicodePwd
// d'Active Directory.
//
// Active Directory exige que le mot de passe soit :
//  1. Entouré de guillemets doubles : "motdepasse"
//  2. Encodé en UTF-16LE (Little-Endian)
//
// Référence : https://docs.microsoft.com/en-us/openspecs/windows_protocols/ms-adts/
func encodeADPassword(password string) ([]byte, error) {
	if password == "" {
		return nil, fmt.Errorf("encodeADPassword: mot de passe vide")
	}

	// Étape 1 : Entourer de guillemets
	quoted := "\"" + password + "\""

	// Étape 2 : Convertir en UTF-16LE
	// D'abord encoder en UTF-16 (runes → uint16)
	utf16Chars := utf16.Encode([]rune(quoted))

	// Puis convertir en bytes Little-Endian
	result := make([]byte, len(utf16Chars)*2)
	for i, char := range utf16Chars {
		binary.LittleEndian.PutUint16(result[i*2:], char)
	}

	return result, nil
}
