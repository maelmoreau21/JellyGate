package main

import (
	"fmt"
	"strings"

	"github.com/hrfee/mediabrowser"
)

func (app *appContext) identityUserByID(userID string) (mediabrowser.User, error) {
	if app.usingLDAPIdentity() && strings.HasPrefix(userID, LDAPUserIDPrefix) {
		username := app.ldapIdentity.usernameFromID(userID)
		if username == "" {
			return mediabrowser.User{}, fmt.Errorf("invalid ldap user id")
		}
		exists, err := app.ldapIdentity.UserExists(username)
		if err != nil {
			return mediabrowser.User{}, err
		}
		if !exists {
			return mediabrowser.User{}, fmt.Errorf("user not found")
		}
		user := mediabrowser.User{}
		user.ID = userID
		user.Name = username
		user.Policy.IsDisabled = false
		return user, nil
	}
	return app.jf.UserByID(userID, false)
}
