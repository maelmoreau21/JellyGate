// Package ldap provides a stub for the legacy LDAP client.
//
// DEPRECATED: This package is a compatibility stub. JellyGate no longer
// manages LDAP directly. Authentik handles identity via its LDAP Outpost.
// This stub exists only to allow compilation while the LDAP removal is
// completed across all handlers and tests.
package ldap

import (
	"github.com/maelmoreau21/JellyGate/internal/config"
)

const (
	ProvisionRoleUser    = "user"
	ProvisionRoleInviter = "inviter"
	ProvisionRoleAdmin   = "admin"
)

// UserEntry represents a user found in LDAP (stub).
type UserEntry struct {
	Username          string
	DN                string
	Email             string
	UID               string
	UsernameAttribute string
	DisplayName       string
	UPN               string
	IsDisabled        bool
}

// Client is a stub for the legacy LDAP client.
// All methods return errors indicating LDAP is disabled.
type Client struct{}

// New creates a stub LDAP client. The client does nothing.
func New(_ config.LDAPConfig) *Client {
	return &Client{}
}

// Close is a no-op for the stub client.
func (c *Client) Close() {}

// CreateUser is a stub that always returns an error.
func (c *Client) CreateUser(username, displayName, email, password, role string) (string, error) {
	return "", nil
}

// DeleteUser is a stub that always returns nil.
func (c *Client) DeleteUser(dn string) error {
	return nil
}

// AddUserToGroup is a stub that always returns nil.
func (c *Client) AddUserToGroup(userDN, groupRef string) error {
	return nil
}

// TestConnection is a stub that returns nil.
func (c *Client) TestConnection() error {
	return nil
}

// ResolveUserAccess is a stub.
func (c *Client) ResolveUserAccess(username string) (*UserEntry, bool, error) {
	return nil, false, nil
}
