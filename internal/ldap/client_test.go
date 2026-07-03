package ldap

import "testing"

func TestValidateLDAPFilter(t *testing.T) {
	t.Run("accepts templated filters", func(t *testing.T) {
		if err := validateLDAPFilter("(&(objectClass=user)(uid={username}))"); err != nil {
			t.Fatalf("validateLDAPFilter() error = %v", err)
		}
	})

	t.Run("rejects malformed filters", func(t *testing.T) {
		for _, filter := range []string{"(&(", "(&(uid=foo))extra", "(&(uid=foo)*)"} {
			if err := validateLDAPFilter(filter); err == nil {
				t.Fatalf("validateLDAPFilter(%q) error = nil, want error", filter)
			}
		}
	})
}
