package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/hrfee/mediabrowser"
)

const (
	IdentityBackendJellyfin = "jellyfin"
	IdentityBackendLDAP     = "ldap"
	LDAPUserIDPrefix        = "ldap:"
)

var errLDAPUserExists = errors.New("ldap user already exists")

type LDAPIdentityProvider struct {
	Server             string
	BindDN             string
	BindPassword       string
	BaseDN             string
	UsersOU            string
	UIDAttribute       string
	SAMAccountNameAttr string
	UPNSuffix          string
	ObjectClasses      []string
	DisabledAttribute  string
	DisabledValue      string
	EnabledValue       string
	InsecureSkipVerify bool
}

func NewLDAPIdentityProviderFromConfig(c *Config) (*LDAPIdentityProvider, error) {
	section := c.Section("identity_ldap")
	provider := &LDAPIdentityProvider{
		Server:             section.Key("server").String(),
		BindDN:             section.Key("bind_dn").String(),
		BindPassword:       section.Key("bind_password").String(),
		BaseDN:             section.Key("base_dn").String(),
		UsersOU:            section.Key("users_ou").String(),
		UIDAttribute:       section.Key("uid_attribute").MustString("uid"),
		SAMAccountNameAttr: section.Key("sam_attribute").MustString("sAMAccountName"),
		UPNSuffix:          section.Key("upn_suffix").String(),
		ObjectClasses:      section.Key("object_classes").StringsWithShadows("|"),
		DisabledAttribute:  section.Key("disabled_attribute").MustString("userAccountControl"),
		DisabledValue:      section.Key("disabled_value").MustString("true"),
		EnabledValue:       section.Key("enabled_value").MustString("false"),
		InsecureSkipVerify: section.Key("insecure_skip_verify").MustBool(false),
	}

	if provider.Server == "" || provider.BindDN == "" || provider.BaseDN == "" {
		return nil, fmt.Errorf("identity_ldap.server, identity_ldap.bind_dn and identity_ldap.base_dn are required")
	}
	if provider.UIDAttribute == "" {
		provider.UIDAttribute = "uid"
	}
	if len(provider.ObjectClasses) == 0 {
		provider.ObjectClasses = []string{"top", "person", "organizationalPerson", "user"}
	}
	return provider, nil
}

func (app *appContext) configureIdentityBackend() error {
	mode := strings.ToLower(strings.TrimSpace(app.config.Section("identity").Key("backend").MustString(IdentityBackendJellyfin)))
	if v := strings.TrimSpace(strings.ToLower(getFirstEnv("JFA_USER_BACKEND", "USER_BACKEND"))); v != "" {
		mode = v
	}
	if mode != IdentityBackendLDAP {
		app.identityBackendMode = IdentityBackendJellyfin
		app.ldapIdentity = nil
		return nil
	}

	provider, err := NewLDAPIdentityProviderFromConfig(app.config)
	if err != nil {
		return err
	}
	app.identityBackendMode = IdentityBackendLDAP
	app.ldapIdentity = provider
	app.info.Println("using LDAP identity backend")
	return nil
}

func getFirstEnv(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(strings.Trim(os.Getenv(key), "\"")); v != "" {
			return v
		}
	}
	return ""
}

func (app *appContext) usingLDAPIdentity() bool {
	return app.identityBackendMode == IdentityBackendLDAP && app.ldapIdentity != nil
}

func (provider *LDAPIdentityProvider) userID(username string) string {
	return LDAPUserIDPrefix + username
}

func (provider *LDAPIdentityProvider) usernameFromID(id string) string {
	if strings.HasPrefix(id, LDAPUserIDPrefix) {
		return strings.TrimPrefix(id, LDAPUserIDPrefix)
	}
	return ""
}

func (provider *LDAPIdentityProvider) usersBaseDN() string {
	if provider.UsersOU == "" {
		return provider.BaseDN
	}
	return provider.UsersOU + "," + provider.BaseDN
}

func (provider *LDAPIdentityProvider) dialAndBind() (*ldap.Conn, error) {
	var conn *ldap.Conn
	var err error
	if strings.HasPrefix(strings.ToLower(provider.Server), "ldaps://") {
		conn, err = ldap.DialURL(provider.Server, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: provider.InsecureSkipVerify}))
	} else {
		conn, err = ldap.DialURL(provider.Server)
	}
	if err != nil {
		return nil, err
	}
	conn.SetTimeout(10 * time.Second)
	if err := conn.Bind(provider.BindDN, provider.BindPassword); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (provider *LDAPIdentityProvider) userDN(username string) (string, error) {
	conn, err := provider.dialAndBind()
	if err != nil {
		return "", err
	}
	defer conn.Close()

	search := ldap.NewSearchRequest(
		provider.usersBaseDN(),
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		1,
		0,
		false,
		fmt.Sprintf("(%s=%s)", provider.UIDAttribute, ldap.EscapeFilter(username)),
		[]string{"dn"},
		nil,
	)
	resp, err := conn.Search(search)
	if err != nil {
		return "", err
	}
	if len(resp.Entries) == 0 {
		return "", ldap.NewError(ldap.LDAPResultNoSuchObject, errors.New("user not found"))
	}
	return resp.Entries[0].DN, nil
}

func (provider *LDAPIdentityProvider) UserExists(username string) (bool, error) {
	dn, err := provider.userDN(username)
	if err != nil {
		var ldapErr *ldap.Error
		if errors.As(err, &ldapErr) && ldapErr.ResultCode == ldap.LDAPResultNoSuchObject {
			return false, nil
		}
		return false, err
	}
	return dn != "", nil
}

func (provider *LDAPIdentityProvider) CreateUser(username, password string) (string, error) {
	exists, err := provider.UserExists(username)
	if err != nil {
		return "", err
	}
	if exists {
		return "", errLDAPUserExists
	}

	conn, err := provider.dialAndBind()
	if err != nil {
		return "", err
	}
	defer conn.Close()

	dn := fmt.Sprintf("%s=%s,%s", provider.UIDAttribute, username, provider.usersBaseDN())
	addReq := ldap.NewAddRequest(dn, nil)
	addReq.Attribute("objectClass", provider.ObjectClasses)
	addReq.Attribute(provider.UIDAttribute, []string{username})
	addReq.Attribute("cn", []string{username})
	addReq.Attribute("sn", []string{username})
	if provider.SAMAccountNameAttr != "" {
		addReq.Attribute(provider.SAMAccountNameAttr, []string{username})
	}
	if provider.UPNSuffix != "" {
		addReq.Attribute("userPrincipalName", []string{username + "@" + provider.UPNSuffix})
	}

	if err := conn.Add(addReq); err != nil {
		return "", err
	}
	if err := provider.setPasswordByDN(conn, dn, password); err != nil {
		return "", err
	}
	return provider.userID(username), nil
}

func (provider *LDAPIdentityProvider) VerifyPassword(username, password string) bool {
	dn, err := provider.userDN(username)
	if err != nil {
		return false
	}
	var conn *ldap.Conn
	if strings.HasPrefix(strings.ToLower(provider.Server), "ldaps://") {
		conn, err = ldap.DialURL(provider.Server, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: provider.InsecureSkipVerify}))
	} else {
		conn, err = ldap.DialURL(provider.Server)
	}
	if err != nil {
		return false
	}
	defer conn.Close()
	conn.SetTimeout(10 * time.Second)
	return conn.Bind(dn, password) == nil
}

func (provider *LDAPIdentityProvider) SetPasswordByID(id, _oldPassword, newPassword string) error {
	username := provider.usernameFromID(id)
	if username == "" {
		return fmt.Errorf("invalid ldap user id: %s", id)
	}
	return provider.SetPasswordByUsername(username, newPassword)
}

func (provider *LDAPIdentityProvider) SetPasswordByUsername(username, newPassword string) error {
	conn, err := provider.dialAndBind()
	if err != nil {
		return err
	}
	defer conn.Close()

	dn, err := provider.userDN(username)
	if err != nil {
		return err
	}
	return provider.setPasswordByDN(conn, dn, newPassword)
}

func (provider *LDAPIdentityProvider) DeleteUserByID(id string) error {
	username := provider.usernameFromID(id)
	if username == "" {
		return fmt.Errorf("invalid ldap user id: %s", id)
	}
	dn, err := provider.userDN(username)
	if err != nil {
		return err
	}
	conn, err := provider.dialAndBind()
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Del(ldap.NewDelRequest(dn, nil))
}

func (provider *LDAPIdentityProvider) SetDisabledByID(id string, disabled bool) error {
	username := provider.usernameFromID(id)
	if username == "" {
		return fmt.Errorf("invalid ldap user id: %s", id)
	}
	conn, err := provider.dialAndBind()
	if err != nil {
		return err
	}
	defer conn.Close()

	dn, err := provider.userDN(username)
	if err != nil {
		return err
	}

	attr := strings.TrimSpace(provider.DisabledAttribute)
	if attr == "" {
		attr = "userAccountControl"
	}

	if strings.EqualFold(attr, "userAccountControl") {
		search := ldap.NewSearchRequest(
			dn,
			ldap.ScopeBaseObject,
			ldap.NeverDerefAliases,
			1,
			0,
			false,
			"(objectClass=*)",
			[]string{"userAccountControl"},
			nil,
		)
		resp, err := conn.Search(search)
		if err != nil {
			return err
		}
		if len(resp.Entries) == 0 {
			return fmt.Errorf("ldap user not found")
		}
		current := 512
		if raw := resp.Entries[0].GetAttributeValue("userAccountControl"); raw != "" {
			_, _ = fmt.Sscanf(raw, "%d", &current)
		}
		if disabled {
			current = current | 2
		} else {
			current = current &^ 2
		}
		modify := ldap.NewModifyRequest(dn, nil)
		modify.Replace("userAccountControl", []string{fmt.Sprintf("%d", current)})
		return conn.Modify(modify)
	}

	value := provider.EnabledValue
	if disabled {
		value = provider.DisabledValue
	}
	modify := ldap.NewModifyRequest(dn, nil)
	modify.Replace(attr, []string{value})
	return conn.Modify(modify)
}

func (provider *LDAPIdentityProvider) setPasswordByDN(conn *ldap.Conn, dn, newPassword string) error {
	modify := ldap.NewModifyRequest(dn, nil)
	modify.Replace("userPassword", []string{newPassword})
	if err := conn.Modify(modify); err == nil {
		return nil
	}

	pm := ldap.NewPasswordModifyRequest(dn, "", newPassword)
	_, err := conn.PasswordModify(pm)
	if err != nil {
		return fmt.Errorf("failed to set LDAP password: %w", err)
	}
	return nil
}

func (provider *LDAPIdentityProvider) isEntryDisabled(entry *ldap.Entry) bool {
	attr := strings.TrimSpace(provider.DisabledAttribute)
	if attr == "" {
		attr = "userAccountControl"
	}

	if strings.EqualFold(attr, "userAccountControl") {
		raw := entry.GetAttributeValue("userAccountControl")
		if raw == "" {
			return false
		}
		flags, err := strconv.Atoi(raw)
		if err != nil {
			return false
		}
		return (flags & 2) == 2
	}

	val := strings.TrimSpace(strings.ToLower(entry.GetAttributeValue(attr)))
	disabledVal := strings.TrimSpace(strings.ToLower(provider.DisabledValue))
	if disabledVal == "" {
		disabledVal = "true"
	}
	return val == disabledVal
}

func (provider *LDAPIdentityProvider) ListUsers() ([]mediabrowser.User, error) {
	conn, err := provider.dialAndBind()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	search := ldap.NewSearchRequest(
		provider.usersBaseDN(),
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		0,
		false,
		fmt.Sprintf("(%s=*)", provider.UIDAttribute),
		[]string{provider.UIDAttribute, "cn", provider.DisabledAttribute, "userAccountControl"},
		nil,
	)
	resp, err := conn.Search(search)
	if err != nil {
		return nil, err
	}

	users := make([]mediabrowser.User, 0, len(resp.Entries))
	for _, entry := range resp.Entries {
		username := strings.TrimSpace(entry.GetAttributeValue(provider.UIDAttribute))
		if username == "" {
			username = strings.TrimSpace(entry.GetAttributeValue("cn"))
		}
		if username == "" {
			continue
		}
		user := mediabrowser.User{}
		user.ID = provider.userID(username)
		user.Name = username
		user.Policy.IsDisabled = provider.isEntryDisabled(entry)
		users = append(users, user)
	}
	return users, nil
}
