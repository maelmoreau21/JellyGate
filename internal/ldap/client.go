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
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
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

	// Roles de provisioning LDAP utilises pour l'affectation automatique de groupe.
	ProvisionRoleUser    = "user"
	ProvisionRoleInviter = "inviter"
	ProvisionRoleAdmin   = "admin"

	// Groupes LDAP par defaut appliques lors du provisioning.
	defaultLDAPUsersGroup   = "jellyfin"
	defaultLDAPInviterGroup = "jellyfin-Parrainage"
	defaultLDAPAdminGroup   = "jellyfin-administrateur"

	// defaultUsernameAttribute est l'attribut AD historique utilise pour le login.
	defaultUsernameAttribute = "sAMAccountName"

	// directoryProfile types detectes depuis RootDSE.
	directoryProfileUnknown = "unknown"
	directoryProfileAD      = "active_directory"
	directoryProfileLDAP    = "openldap"
)

var ldapAttrNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
var ldapUsernameTokenPattern = regexp.MustCompile(`(?i)\{username\}`)

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

// TestConnection vérifie la connectivité réseau + bind LDAP.
func (c *Client) TestConnection() error {
	conn, err := c.connect()
	if err != nil {
		return fmt.Errorf("ldap.TestConnection: %w", err)
	}
	defer conn.Close()
	return nil
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
func (c *Client) CreateUser(username, displayName, email, password, role string) (string, error) {
	conn, err := c.connect()
	if err != nil {
		return "", fmt.Errorf("ldap.CreateUser: %w", err)
	}
	defer conn.Close()

	profile := c.detectDirectoryProfile(conn)
	loginAttr := c.effectiveUsernameAttribute(profile)
	if loginAttr == "" {
		loginAttr = defaultUsernameAttribute
	}

	rdnAttr := "cn"
	if strings.EqualFold(loginAttr, "uid") && profile != directoryProfileAD {
		rdnAttr = "uid"
	}

	// Construire le DN de l'utilisateur.
	// Ex: CN=jdoe,CN=Users,DC=home,DC=lan ou uid=jdoe,OU=People,dc=home,dc=lan
	userDN := fmt.Sprintf("%s=%s,%s,%s",
		rdnAttr,
		goldap.EscapeDN(username),
		c.cfg.UserOU,
		c.cfg.BaseDN,
	)

	objectClassCandidates := c.createObjectClassCandidates(profile)
	var lastCreateErr error

	for _, objectClass := range objectClassCandidates {
		classes := objectClassHierarchy(objectClass)
		addReq := goldap.NewAddRequest(userDN, nil)
		addReq.Attribute("objectClass", classes)

		addedAttrs := map[string]struct{}{}
		addAttribute := func(attr string, values ...string) {
			attr = strings.TrimSpace(attr)
			if attr == "" || len(values) == 0 {
				return
			}
			key := strings.ToLower(attr)
			if _, exists := addedAttrs[key]; exists {
				return
			}
			cleanValues := make([]string, 0, len(values))
			for _, value := range values {
				v := strings.TrimSpace(value)
				if v != "" {
					cleanValues = append(cleanValues, v)
				}
			}
			if len(cleanValues) == 0 {
				return
			}
			addReq.Attribute(attr, cleanValues)
			addedAttrs[key] = struct{}{}
		}

		// Attributs communs, valides sur AD/OpenLDAP/Synology.
		addAttribute("cn", username)
		addAttribute("displayName", displayName)
		addAttribute("sn", username)
		addAttribute(loginAttr, username)
		if email != "" {
			addAttribute("mail", email)
		}

		if objectClassLooksLikeAD(objectClass) {
			addAttribute("sAMAccountName", username)
			addAttribute("name", displayName)

			if domain := strings.TrimSpace(c.cfg.Domain); domain != "" {
				addAttribute("userPrincipalName", fmt.Sprintf("%s@%s", username, domain))
			}

			// AD impose unicodePwd encode en UTF-16LE via LDAPS.
			encodedPassword, err := encodeADPassword(password)
			if err != nil {
				return "", fmt.Errorf("ldap.CreateUser: %w", err)
			}
			addReq.Attribute("unicodePwd", []string{string(encodedPassword)})
			addAttribute("userAccountControl", fmt.Sprintf("%d", UAC_NORMAL_ACCOUNT))
		} else {
			// Sur OpenLDAP/Synology LDAP, userPassword est l'attribut standard.
			addAttribute("userPassword", password)
		}

		err = conn.Add(addReq)
		if err == nil {
			slog.Info("Utilisateur LDAP créé",
				"dn", userDN,
				"username", username,
				"profile", profile,
				"object_class", objectClass,
				"login_attr", loginAttr,
			)
			lastCreateErr = nil
			break
		}

		lastCreateErr = err
		if !isObjectClassFallbackError(err) {
			return "", fmt.Errorf("ldap.CreateUser: échec de la création de %q: %w", userDN, err)
		}

		slog.Warn("Echec creation LDAP, tentative avec objectClass alternatif",
			"dn", userDN,
			"object_class", objectClass,
			"error", err,
		)
	}

	if lastCreateErr != nil {
		return "", fmt.Errorf("ldap.CreateUser: échec de la création de %q: %w", userDN, lastCreateErr)
	}

	if err := c.assignUserToDefaultGroup(conn, userDN, role); err != nil {
		// Log l'erreur mais ne fait pas échouer la création
		slog.Warn("Impossible d'ajouter l'utilisateur au groupe LDAP cible",
			"dn", userDN,
			"role", role,
			"error", err,
		)
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

// UpdateUserContact met a jour les attributs de contact LDAP pour un utilisateur.
// Les attributs cibles sont:
//   - mail
//   - telephoneNumber/mobile (si un numero est fourni)
func (c *Client) UpdateUserContact(userDN, email, phone string) error {
	userDN = strings.TrimSpace(userDN)
	if userDN == "" {
		return fmt.Errorf("ldap.UpdateUserContact: DN vide")
	}

	conn, err := c.connect()
	if err != nil {
		return fmt.Errorf("ldap.UpdateUserContact: %w", err)
	}
	defer conn.Close()

	if err := c.replaceUserAttribute(conn, userDN, "mail", strings.TrimSpace(email)); err != nil {
		return fmt.Errorf("ldap.UpdateUserContact: echec mise a jour mail: %w", err)
	}

	phone = strings.TrimSpace(phone)
	if phone == "" {
		_ = c.replaceUserAttribute(conn, userDN, "telephoneNumber", "")
		_ = c.replaceUserAttribute(conn, userDN, "mobile", "")
		return nil
	}

	phoneAttrs := []string{"telephoneNumber", "mobile"}
	phoneUpdated := false
	var lastPhoneErr error
	for _, attr := range phoneAttrs {
		if err := c.replaceUserAttribute(conn, userDN, attr, phone); err != nil {
			lastPhoneErr = err
			slog.Debug("Attribut telephone LDAP non applicable", "dn", userDN, "attr", attr, "error", err)
			continue
		}
		phoneUpdated = true
	}

	if !phoneUpdated && lastPhoneErr != nil {
		return fmt.Errorf("ldap.UpdateUserContact: echec mise a jour telephone: %w", lastPhoneErr)
	}

	return nil
}

func (c *Client) replaceUserAttribute(conn *goldap.Conn, userDN, attribute, value string) error {
	if conn == nil {
		return fmt.Errorf("ldap.replaceUserAttribute: connexion LDAP nil")
	}

	attr := strings.TrimSpace(attribute)
	if attr == "" {
		return fmt.Errorf("ldap.replaceUserAttribute: attribut vide")
	}

	modReq := goldap.NewModifyRequest(userDN, nil)
	cleanValue := strings.TrimSpace(value)
	if cleanValue == "" {
		modReq.Delete(attr, nil)
	} else {
		modReq.Replace(attr, []string{cleanValue})
	}

	if err := conn.Modify(modReq); err != nil {
		if cleanValue == "" && isIgnorableLDAPDeleteError(err) {
			return nil
		}
		return err
	}

	return nil
}

func isIgnorableLDAPDeleteError(err error) bool {
	if err == nil {
		return false
	}

	var ldapErr *goldap.Error
	if errors.As(err, &ldapErr) {
		switch ldapErr.ResultCode {
		case goldap.LDAPResultNoSuchAttribute,
			goldap.LDAPResultUndefinedAttributeType:
			return true
		}
	}

	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "no such attribute") || strings.Contains(errMsg, "undefined attribute")
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
	DN                string
	Username          string // Attribut identifiant resolu (sAMAccountName/uid/...)
	UID               string
	UsernameAttribute string
	DisplayName       string
	Email             string
	UPN               string // userPrincipalName
	UAC               string // userAccountControl
	IsDisabled        bool
}

// FindUser recherche un utilisateur LDAP via les attributs login compatibles
// en appliquant le filtre de recherche LDAP configure.
// Retourne nil si l'utilisateur n'est pas trouvé (pas d'erreur).
func (c *Client) FindUser(username string) (*UserEntry, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}

	conn, err := c.connect()
	if err != nil {
		return nil, fmt.Errorf("ldap.FindUser: %w", err)
	}
	defer conn.Close()

	searchDN := strings.TrimSpace(c.cfg.BaseDN)
	profile := c.detectDirectoryProfile(conn)
	lookupAttrs := c.configuredSearchAttributes(profile)
	filter := c.buildUserLookupFilter(username, profile, lookupAttrs)

	requestAttrs := []string{"dn", "displayName", "mail", "userPrincipalName", "userAccountControl"}
	requestAttrs = append(requestAttrs, lookupAttrs...)
	uidAttr := c.effectiveUIDAttribute()
	if uidAttr != "" {
		requestAttrs = append(requestAttrs, uidAttr)
	}

	searchReq := goldap.NewSearchRequest(
		searchDN,
		goldap.ScopeWholeSubtree,
		goldap.NeverDerefAliases,
		1,  // Limit à 1 résultat
		10, // Timeout 10 secondes
		false,
		filter,
		requestAttrs,
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
	resolvedUsername, resolvedAttr := resolveEntryUsername(entry, lookupAttrs)
	uidValue := strings.TrimSpace(entry.GetAttributeValue(uidAttr))
	if uidValue == "" {
		uidValue = resolvedUsername
	}

	return &UserEntry{
		DN:                entry.DN,
		Username:          resolvedUsername,
		UID:               uidValue,
		UsernameAttribute: resolvedAttr,
		DisplayName:       entry.GetAttributeValue("displayName"),
		Email:             entry.GetAttributeValue("mail"),
		UPN:               entry.GetAttributeValue("userPrincipalName"),
		UAC:               uac,
		IsDisabled:        uac == fmt.Sprintf("%d", UAC_DISABLED_ACCOUNT),
	}, nil
}

// ResolveUserAccess applique le filtre de recherche LDAP et retourne aussi
// si l'utilisateur est administrateur via le filtre administrateur LDAP.
func (c *Client) ResolveUserAccess(username string) (*UserEntry, bool, error) {
	entry, err := c.FindUser(username)
	if err != nil || entry == nil {
		return entry, false, err
	}

	isAdmin, err := c.IsUserAdmin(username, entry)
	if err != nil {
		return entry, false, err
	}

	return entry, isAdmin, nil
}

// IsUserAdmin valide l'acces administrateur via LDAP Admin Filter.
// Si aucun filtre admin n'est configure, retourne false sans erreur.
func (c *Client) IsUserAdmin(username string, user *UserEntry) (bool, error) {
	adminFilter := normalizeLDAPFilter(c.cfg.AdminFilter)
	if adminFilter == "" {
		return false, nil
	}

	conn, err := c.connect()
	if err != nil {
		return false, fmt.Errorf("ldap.IsUserAdmin: %w", err)
	}
	defer conn.Close()

	searchDN := strings.TrimSpace(c.cfg.BaseDN)
	profile := c.detectDirectoryProfile(conn)
	escapedUsername := goldap.EscapeFilter(strings.TrimSpace(username))
	replacedAdminFilter, _ := replaceUsernameToken(adminFilter, escapedUsername)

	if c.cfg.AdminFilterMemberUID {
		uidCandidates := resolveUIDCandidates(username, user)
		return c.matchAdminFilterWithMemberUID(conn, searchDN, replacedAdminFilter, uidCandidates)
	}

	identityFilter := c.buildIdentityFilter(profile, escapedUsername, user)
	combinedFilter := fmt.Sprintf("(&%s%s)", replacedAdminFilter, identityFilter)

	searchReq := goldap.NewSearchRequest(
		searchDN,
		goldap.ScopeWholeSubtree,
		goldap.NeverDerefAliases,
		1,
		10,
		false,
		combinedFilter,
		[]string{"dn"},
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil {
		return false, fmt.Errorf("ldap.IsUserAdmin: échec de la recherche admin pour %q: %w", username, err)
	}

	return len(result.Entries) > 0, nil
}

func normalizeLDAPAttrName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if !ldapAttrNamePattern.MatchString(name) {
		return ""
	}
	return name
}

func normalizeLDAPObjectClass(name string) string {
	return normalizeLDAPAttrName(name)
}

func isAutoLDAPValue(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	return v == "" || v == "auto"
}

func normalizeLDAPFilter(filter string) string {
	trimmed := strings.TrimSpace(filter)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "(") {
		return trimmed
	}
	return "(" + trimmed + ")"
}

func replaceUsernameToken(filter, escapedUsername string) (string, bool) {
	if strings.TrimSpace(filter) == "" {
		return "", false
	}
	hasToken := ldapUsernameTokenPattern.MatchString(filter)
	return ldapUsernameTokenPattern.ReplaceAllString(filter, escapedUsername), hasToken
}

func parseLDAPAttributesCSV(raw string) []string {
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), ";", ",")
	if normalized == "" {
		return nil
	}

	parts := strings.Split(normalized, ",")
	attrs := make([]string, 0, len(parts))
	for _, part := range parts {
		attr := normalizeLDAPAttrName(part)
		if attr == "" {
			continue
		}
		attrs = append(attrs, attr)
	}

	return attrs
}

func (c *Client) configuredSearchAttributes(profile string) []string {
	configured := parseLDAPAttributesCSV(c.cfg.SearchAttributes)
	if len(configured) == 0 {
		return c.lookupAttributes(profile)
	}

	seen := map[string]struct{}{}
	attrs := make([]string, 0, len(configured)+3)

	add := func(attr string) {
		normalized := normalizeLDAPAttrName(attr)
		if normalized == "" {
			return
		}
		key := strings.ToLower(normalized)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		attrs = append(attrs, normalized)
	}

	for _, attr := range configured {
		add(attr)
	}
	add(c.effectiveUsernameAttribute(profile))
	add(c.effectiveUIDAttribute())
	add("mail")

	if len(attrs) == 0 {
		return c.lookupAttributes(profile)
	}

	return attrs
}

func (c *Client) buildUsernameLookupClause(lookupAttrs []string, escapedUsername string) string {
	filters := make([]string, 0, len(lookupAttrs))
	for _, attr := range lookupAttrs {
		normalized := normalizeLDAPAttrName(attr)
		if normalized == "" {
			continue
		}
		filters = append(filters, fmt.Sprintf("(%s=%s)", normalized, escapedUsername))
	}

	if len(filters) == 0 {
		filters = append(filters, fmt.Sprintf("(%s=%s)", defaultUsernameAttribute, escapedUsername))
	}

	if len(filters) == 1 {
		return filters[0]
	}

	return fmt.Sprintf("(|%s)", strings.Join(filters, ""))
}

func (c *Client) buildUserLookupFilter(username, profile string, lookupAttrs []string) string {
	escapedUsername := goldap.EscapeFilter(strings.TrimSpace(username))
	usernameClause := c.buildUsernameLookupClause(lookupAttrs, escapedUsername)

	configuredFilter := normalizeLDAPFilter(c.cfg.SearchFilter)
	if configuredFilter != "" {
		replacedFilter, hasToken := replaceUsernameToken(configuredFilter, escapedUsername)
		if hasToken {
			return replacedFilter
		}
		return fmt.Sprintf("(&%s%s)", replacedFilter, usernameClause)
	}

	userObjectClass := c.effectiveUserObjectClass(profile)
	if userObjectClass != "" {
		return fmt.Sprintf("(&(objectClass=%s)%s)", goldap.EscapeFilter(userObjectClass), usernameClause)
	}

	return fmt.Sprintf(
		"(&(|(objectClass=user)(objectClass=person)(objectClass=organizationalPerson)(objectClass=inetOrgPerson)(objectClass=posixAccount))%s)",
		usernameClause,
	)
}

func resolveUIDCandidates(username string, user *UserEntry) []string {
	seen := map[string]struct{}{}
	values := make([]string, 0, 4)

	add := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		values = append(values, trimmed)
	}

	add(username)
	if user != nil {
		add(user.UID)
		add(user.Username)
		if at := strings.Index(strings.TrimSpace(user.UPN), "@"); at > 0 {
			add(user.UPN[:at])
		}
	}

	return values
}

func (c *Client) matchAdminFilterWithMemberUID(conn *goldap.Conn, searchDN, adminFilter string, uidCandidates []string) (bool, error) {
	if strings.TrimSpace(adminFilter) == "" || len(uidCandidates) == 0 {
		return false, nil
	}

	candidateSet := map[string]struct{}{}
	for _, candidate := range uidCandidates {
		candidateSet[strings.ToLower(strings.TrimSpace(candidate))] = struct{}{}
	}

	searchReq := goldap.NewSearchRequest(
		searchDN,
		goldap.ScopeWholeSubtree,
		goldap.NeverDerefAliases,
		0,
		10,
		false,
		adminFilter,
		[]string{"memberUid"},
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil {
		return false, fmt.Errorf("ldap.matchAdminFilterWithMemberUID: %w", err)
	}

	for _, entry := range result.Entries {
		for _, memberUID := range entry.GetAttributeValues("memberUid") {
			if _, ok := candidateSet[strings.ToLower(strings.TrimSpace(memberUID))]; ok {
				return true, nil
			}
		}
	}

	return false, nil
}

func (c *Client) lookupAttributes(profile string) []string {
	seen := map[string]struct{}{}
	attrs := make([]string, 0, 5)

	add := func(attr string) {
		normalized := normalizeLDAPAttrName(attr)
		if normalized == "" {
			return
		}
		key := strings.ToLower(normalized)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		attrs = append(attrs, normalized)
	}

	add(c.effectiveUsernameAttribute(profile))
	add(defaultUsernameAttribute)
	add("uid")
	add("cn")
	add("userPrincipalName")
	add("mail")

	if len(attrs) == 0 {
		attrs = append(attrs, defaultUsernameAttribute)
	}

	return attrs
}

func (c *Client) effectiveUIDAttribute() string {
	configured := normalizeLDAPAttrName(c.cfg.UIDAttribute)
	if configured != "" && !isAutoLDAPValue(configured) {
		return configured
	}
	return "uid"
}

func (c *Client) effectiveUsernameAttribute(profile string) string {
	configured := normalizeLDAPAttrName(c.cfg.UsernameAttribute)
	if configured != "" && !isAutoLDAPValue(configured) {
		return configured
	}

	switch profile {
	case directoryProfileLDAP:
		return "uid"
	case directoryProfileAD:
		return defaultUsernameAttribute
	default:
		return defaultUsernameAttribute
	}
}

func (c *Client) buildIdentityFilter(profile, escapedUsername string, user *UserEntry) string {
	parts := make([]string, 0, 8)
	seen := map[string]struct{}{}

	add := func(filter string) {
		trimmed := strings.TrimSpace(filter)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		parts = append(parts, trimmed)
	}

	for _, attr := range c.configuredSearchAttributes(profile) {
		normalized := normalizeLDAPAttrName(attr)
		if normalized == "" {
			continue
		}
		add(fmt.Sprintf("(%s=%s)", normalized, escapedUsername))
	}

	if user != nil {
		uidAttr := c.effectiveUIDAttribute()
		if uidAttr != "" && strings.TrimSpace(user.UID) != "" {
			add(fmt.Sprintf("(%s=%s)", uidAttr, goldap.EscapeFilter(strings.TrimSpace(user.UID))))
		}

		usernameAttr := c.effectiveUsernameAttribute(profile)
		if usernameAttr != "" && strings.TrimSpace(user.Username) != "" {
			add(fmt.Sprintf("(%s=%s)", usernameAttr, goldap.EscapeFilter(strings.TrimSpace(user.Username))))
		}

		if strings.TrimSpace(user.DN) != "" {
			add(fmt.Sprintf("(distinguishedName=%s)", goldap.EscapeFilter(strings.TrimSpace(user.DN))))
		}
	}

	if len(parts) == 0 {
		return "(objectClass=*)"
	}
	if len(parts) == 1 {
		return parts[0]
	}

	return fmt.Sprintf("(|%s)", strings.Join(parts, ""))
}

func (c *Client) effectiveUserObjectClass(profile string) string {
	configured := normalizeLDAPObjectClass(c.cfg.UserObjectClass)
	if configured != "" && !isAutoLDAPValue(configured) {
		return configured
	}

	switch profile {
	case directoryProfileLDAP:
		return "person"
	case directoryProfileAD:
		return "user"
	default:
		return ""
	}
}

func (c *Client) effectiveGroupMemberAttribute(profile string) string {
	configured := normalizeLDAPAttrName(c.cfg.GroupMemberAttr)
	if configured != "" && !isAutoLDAPValue(configured) {
		return configured
	}

	switch profile {
	case directoryProfileLDAP:
		return "memberUid"
	default:
		return "member"
	}
}

func (c *Client) detectDirectoryProfile(conn *goldap.Conn) string {
	searchReq := goldap.NewSearchRequest(
		"",
		goldap.ScopeBaseObject,
		goldap.NeverDerefAliases,
		1,
		5,
		false,
		"(objectClass=*)",
		[]string{"vendorName", "supportedCapabilities", "defaultNamingContext", "namingContexts"},
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil || len(result.Entries) == 0 {
		return directoryProfileUnknown
	}

	entry := result.Entries[0]
	vendor := strings.ToLower(strings.TrimSpace(entry.GetAttributeValue("vendorName")))
	defaultNC := strings.TrimSpace(entry.GetAttributeValue("defaultNamingContext"))

	if defaultNC != "" {
		return directoryProfileAD
	}

	for _, cap := range entry.GetAttributeValues("supportedCapabilities") {
		if strings.TrimSpace(cap) == "1.2.840.113556.1.4.800" {
			return directoryProfileAD
		}
	}

	if strings.Contains(vendor, "microsoft") {
		return directoryProfileAD
	}

	if strings.Contains(vendor, "openldap") || strings.Contains(vendor, "synology") || strings.Contains(vendor, "389") {
		return directoryProfileLDAP
	}

	if len(entry.GetAttributeValues("namingContexts")) > 0 {
		return directoryProfileLDAP
	}

	return directoryProfileUnknown
}

func resolveEntryUsername(entry *goldap.Entry, attrs []string) (string, string) {
	for _, attr := range attrs {
		value := strings.TrimSpace(entry.GetAttributeValue(attr))
		if value != "" {
			return value, attr
		}
	}

	fallback := strings.TrimSpace(entry.GetAttributeValue("displayName"))
	if fallback != "" {
		return fallback, "displayName"
	}

	return "", ""
}

// GetGroupMembers récupère la liste de tous les utilisateurs membres d'un groupe LDAP.
func (c *Client) GetGroupMembers(groupDN string) ([]UserEntry, error) {
	if groupDN == "" {
		return nil, fmt.Errorf("ldap.GetGroupMembers: groupDN vide")
	}

	conn, err := c.connect()
	if err != nil {
		return nil, fmt.Errorf("ldap.GetGroupMembers: %w", err)
	}
	defer conn.Close()

	profile := c.detectDirectoryProfile(conn)
	searchDN := strings.TrimSpace(c.cfg.BaseDN)
	lookupAttrs := c.lookupAttributes(profile)

	// Filtre AD optimisé : tous les objets 'user' ayant l'attribut memberOf = groupDN
	filter := fmt.Sprintf("(&(objectClass=user)(memberOf=%s))", goldap.EscapeFilter(groupDN))
	if profile != directoryProfileAD {
		// Filtre plus générique pour les autres types d'annuaires
		// Note: Certains annuaires utilisent 'member' ou 'uniqueMember' sur l'objet groupe
		// Plutôt que de chercher les utilisateurs, on peut aussi chercher le groupe
		// et extraire ses attributs 'member'.
		return c.getGroupMembersGeneric(conn, groupDN, profile)
	}

	requestAttrs := []string{"dn", "displayName", "mail", "userPrincipalName", "userAccountControl"}
	requestAttrs = append(requestAttrs, lookupAttrs...)

	searchReq := goldap.NewSearchRequest(
		searchDN,
		goldap.ScopeWholeSubtree,
		goldap.NeverDerefAliases,
		0,  // Pas de limite
		30, // Timeout 30 secondes pour les gros groupes
		false,
		filter,
		requestAttrs,
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("ldap.GetGroupMembers: échec de la recherche pour %q: %w", groupDN, err)
	}

	users := make([]UserEntry, 0, len(result.Entries))
	for _, entry := range result.Entries {
		uac := entry.GetAttributeValue("userAccountControl")
		resolvedUsername, resolvedAttr := resolveEntryUsername(entry, lookupAttrs)
		if resolvedUsername == "" {
			continue
		}

		users = append(users, UserEntry{
			DN:                entry.DN,
			Username:          resolvedUsername,
			UsernameAttribute: resolvedAttr,
			DisplayName:       entry.GetAttributeValue("displayName"),
			Email:             entry.GetAttributeValue("mail"),
			UPN:               entry.GetAttributeValue("userPrincipalName"),
			UAC:               uac,
			IsDisabled:        uac == fmt.Sprintf("%d", UAC_DISABLED_ACCOUNT),
		})
	}

	return users, nil
}

func (c *Client) getGroupMembersGeneric(conn *goldap.Conn, groupDN string, profile string) ([]UserEntry, error) {
	// 1. Chercher le groupe pour extraire les DNs des membres
	memberAttr := c.effectiveGroupMemberAttribute(profile)
	searchReq := goldap.NewSearchRequest(
		groupDN,
		goldap.ScopeBaseObject,
		goldap.NeverDerefAliases,
		1, 10, false,
		"(objectClass=*)",
		[]string{memberAttr},
		nil,
	)

	res, err := conn.Search(searchReq)
	if err != nil || len(res.Entries) == 0 {
		return nil, fmt.Errorf("impossible de trouver le groupe %q : %w", groupDN, err)
	}

	memberDNs := res.Entries[0].GetAttributeValues(memberAttr)
	if len(memberDNs) == 0 {
		return []UserEntry{}, nil
	}

	// 2. Pour chaque DN, récupérer les infos utilisateur
	// Note: Pour être performant, on fait un OR massif si possible ou des recherches individuelles
	lookupAttrs := c.lookupAttributes(profile)
	users := make([]UserEntry, 0, len(memberDNs))

	for _, mdn := range memberDNs {
		// Recherche par DN exact
		uSearch := goldap.NewSearchRequest(
			mdn,
			goldap.ScopeBaseObject,
			goldap.NeverDerefAliases,
			1, 5, false,
			"(objectClass=*)",
			append([]string{"dn", "displayName", "mail", "userPrincipalName", "userAccountControl"}, lookupAttrs...),
			nil,
		)
		uRes, err := conn.Search(uSearch)
		if err != nil || len(uRes.Entries) == 0 {
			continue
		}

		entry := uRes.Entries[0]
		uac := entry.GetAttributeValue("userAccountControl")
		resolvedUsername, resolvedAttr := resolveEntryUsername(entry, lookupAttrs)
		if resolvedUsername == "" {
			continue
		}

		users = append(users, UserEntry{
			DN:                entry.DN,
			Username:          resolvedUsername,
			UsernameAttribute: resolvedAttr,
			DisplayName:       entry.GetAttributeValue("displayName"),
			Email:             entry.GetAttributeValue("mail"),
			UAC:               uac,
			IsDisabled:        uac == fmt.Sprintf("%d", UAC_DISABLED_ACCOUNT),
		})
	}

	return users, nil
}

func (c *Client) createObjectClassCandidates(profile string) []string {
	configured := normalizeLDAPObjectClass(c.cfg.UserObjectClass)
	if configured != "" && !isAutoLDAPValue(configured) {
		return []string{configured}
	}

	switch profile {
	case directoryProfileAD:
		return []string{"user", "person"}
	case directoryProfileLDAP:
		return []string{"person", "inetOrgPerson", "organizationalPerson", "user"}
	default:
		return []string{"person", "inetOrgPerson", "user"}
	}
}

func objectClassHierarchy(objectClass string) []string {
	switch strings.ToLower(strings.TrimSpace(objectClass)) {
	case "user":
		return []string{objectClassTop, objectClassPerson, objectClassOrgPerson, objectClassUser}
	case "inetorgperson":
		return []string{objectClassTop, objectClassPerson, objectClassOrgPerson, "inetOrgPerson"}
	case "organizationalperson":
		return []string{objectClassTop, objectClassPerson, objectClassOrgPerson}
	case "person":
		return []string{objectClassTop, objectClassPerson}
	case "top":
		return []string{objectClassTop}
	default:
		clean := strings.TrimSpace(objectClass)
		if clean == "" {
			return []string{objectClassTop, objectClassPerson}
		}
		return []string{objectClassTop, clean}
	}
}

func objectClassLooksLikeAD(objectClass string) bool {
	return strings.EqualFold(strings.TrimSpace(objectClass), "user")
}

func isObjectClassFallbackError(err error) bool {
	if err == nil {
		return false
	}

	var ldapErr *goldap.Error
	if errors.As(err, &ldapErr) {
		switch ldapErr.ResultCode {
		case goldap.LDAPResultInvalidAttributeSyntax,
			goldap.LDAPResultObjectClassViolation,
			goldap.LDAPResultUndefinedAttributeType,
			goldap.LDAPResultInvalidDNSyntax:
			return true
		}
	}

	errMsg := strings.ToLower(err.Error())
	if strings.Contains(errMsg, "objectclass") && (strings.Contains(errMsg, "invalid") || strings.Contains(errMsg, "syntax") || strings.Contains(errMsg, "violation")) {
		return true
	}

	return false
}

// ── Opérations internes ─────────────────────────────────────────────────────

// assignUserToDefaultGroup ajoute un utilisateur au groupe cible selon son role.
// user -> jellyfin, inviter -> jellyfin-Parrainage, admin -> jellyfin-administrateur.
func (c *Client) assignUserToDefaultGroup(conn *goldap.Conn, userDN, role string) error {
	normalizedRole := strings.ToLower(strings.TrimSpace(role))
	groups := make([]string, 0, 2)

	baseGroup := strings.TrimSpace(c.cfg.JellyfinGroup)
	if baseGroup == "" {
		baseGroup = strings.TrimSpace(c.cfg.UserGroup)
	}
	if baseGroup == "" {
		baseGroup = defaultLDAPUsersGroup
	}
	groups = append(groups, baseGroup)

	switch normalizedRole {
	case ProvisionRoleAdmin:
		adminGroup := strings.TrimSpace(c.cfg.AdministratorsGroup)
		if adminGroup == "" {
			adminGroup = defaultLDAPAdminGroup
		}
		groups = append(groups, adminGroup)
	case ProvisionRoleInviter:
		inviterGroup := strings.TrimSpace(c.cfg.InviterGroup)
		if inviterGroup == "" {
			inviterGroup = defaultLDAPInviterGroup
		}
		groups = append(groups, inviterGroup)
	}

	seen := make(map[string]struct{}, len(groups))
	for _, groupRef := range groups {
		trimmed := strings.TrimSpace(groupRef)
		if trimmed == "" {
			continue
		}

		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		if err := c.addToGroupByRef(conn, userDN, trimmed); err != nil {
			return err
		}
	}

	return nil
}

// addToGroupByRef ajoute un utilisateur a un groupe AD (DN complet ou nom simple).
func (c *Client) addToGroupByRef(conn *goldap.Conn, userDN, groupRef string) error {
	groupRef = strings.TrimSpace(groupRef)
	if groupRef == "" {
		return fmt.Errorf("groupe vide")
	}

	groupDN := groupRef
	if !strings.Contains(strings.ToLower(groupRef), "dc=") {
		groupDN = fmt.Sprintf("CN=%s,%s,%s", groupRef, c.cfg.UserOU, c.cfg.BaseDN)
	}

	profile := c.detectDirectoryProfile(conn)
	memberAttr := c.effectiveGroupMemberAttribute(profile)
	memberValue := resolveGroupMemberValue(memberAttr, userDN)

	modReq := goldap.NewModifyRequest(groupDN, nil)
	modReq.Add(memberAttr, []string{memberValue})

	if err := conn.Modify(modReq); err != nil {
		if isGroupMemberAlreadyPresentError(err) {
			slog.Debug("Utilisateur deja present dans le groupe LDAP", "member_attr", memberAttr, "member_value", memberValue, "group_dn", groupDN)
			return nil
		}
		return fmt.Errorf("échec de l'ajout de %q au groupe %q via %q: %w", memberValue, groupDN, memberAttr, err)
	}

	slog.Info("Utilisateur ajoute au groupe LDAP", "member_attr", memberAttr, "member_value", memberValue, "group_dn", groupDN)
	return nil
}

func isGroupMemberAlreadyPresentError(err error) bool {
	if err == nil {
		return false
	}

	var ldapErr *goldap.Error
	if errors.As(err, &ldapErr) {
		if ldapErr.ResultCode == goldap.LDAPResultAttributeOrValueExists {
			return true
		}
	}

	errMsg := strings.ToLower(err.Error())
	if strings.Contains(errMsg, "attribute or value exists") || strings.Contains(errMsg, "type or value exists") {
		return true
	}

	return false
}

func resolveGroupMemberValue(memberAttr, userDN string) string {
	attr := strings.ToLower(strings.TrimSpace(memberAttr))
	if attr == "member" || strings.Contains(attr, "dn") {
		return userDN
	}

	parsed, err := goldap.ParseDN(userDN)
	if err != nil || len(parsed.RDNs) == 0 || len(parsed.RDNs[0].Attributes) == 0 {
		return userDN
	}

	value := strings.TrimSpace(parsed.RDNs[0].Attributes[0].Value)
	if value == "" {
		return userDN
	}

	return value
}

// AddUserToGroup ajoute un utilisateur à un groupe AD spécifique.
// groupRef peut être un DN complet (CN=...,DC=...) ou un nom simple de groupe.
func (c *Client) AddUserToGroup(userDN, groupRef string) error {
	userDN = strings.TrimSpace(userDN)
	groupRef = strings.TrimSpace(groupRef)
	if userDN == "" {
		return fmt.Errorf("ldap.AddUserToGroup: userDN vide")
	}
	if groupRef == "" {
		return fmt.Errorf("ldap.AddUserToGroup: groupe vide")
	}

	conn, err := c.connect()
	if err != nil {
		return fmt.Errorf("ldap.AddUserToGroup: %w", err)
	}
	defer conn.Close()

	if err := c.addToGroupByRef(conn, userDN, groupRef); err != nil {
		return fmt.Errorf("ldap.AddUserToGroup: %w", err)
	}
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
