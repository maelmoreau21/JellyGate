package authentik

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

func TestAuthentikClient(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v3/core/users/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var req UserCreatePayload
			_ = json.NewDecoder(r.Body).Decode(&req)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(UserResponse{
				PK:       42,
				ID:       "uuid-test-1234",
				Username: req.Username,
				Email:    req.Email,
				IsActive: true,
			})
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"results": []map[string]interface{}{
					{
						"pk":        42,
						"uid":       "uuid-test-1234",
						"username":  "testuser",
						"name":      "Test User",
						"email":     "test@example.com",
						"is_active": true,
						"groups_obj": []map[string]string{
							{"pk": "grp-1", "name": "jellygate-users"},
							{"pk": "grp-2", "name": "jellygate-admins"},
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v3/core/users/42/recovery/link/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"link": "https://auth.example.com/recovery/use-token/token123",
		})
	})

	mux.HandleFunc("/api/v3/core/users/42/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	mux.HandleFunc("/api/v3/core/groups/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"pk": "11111111-2222-3333-4444-555555555555", "name": "jellyfin-users"},
				{"pk": "22222222-3333-4444-5555-666666666666", "name": "jellygate-users"},
			},
		})
	})

	mux.HandleFunc("/api/v3/core/groups/11111111-2222-3333-4444-555555555555/add_user/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/v3/stages/invitation/invitations/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"pk": "inv-uuid-999"})
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"results": []map[string]interface{}{
					{"pk": "inv-uuid-999", "name": "JG-TEST"},
				},
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v3/stages/invitation/invitations/inv-uuid-999/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	cfg := config.AuthentikConfig{
		URL:      server.URL,
		APIToken: "mock-token",
	}

	cli := NewClient(cfg)

	t.Run("ResolveGroupID", func(t *testing.T) {
		// Existing UUID passes through
		const directUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
		resolved, err := cli.ResolveGroupID(context.Background(), directUUID)
		if err != nil || resolved != directUUID {
			t.Fatalf("Expected direct UUID pass-through %s, got %s (err: %v)", directUUID, resolved, err)
		}

		// Named group gets resolved
		resolvedName, err := cli.ResolveGroupID(context.Background(), "jellyfin-users")
		if err != nil || resolvedName != "11111111-2222-3333-4444-555555555555" {
			t.Fatalf("Expected resolved UUID 11111111-2222-3333-4444-555555555555, got %s (err: %v)", resolvedName, err)
		}
	})

	t.Run("CreateUserWithNamedGroups", func(t *testing.T) {
		resp, err := cli.CreateUser(context.Background(), UserCreatePayload{
			Username: "newuserwithgroup",
			Email:    "groupuser@example.com",
			IsActive: true,
			Groups:   []string{"jellyfin-users"},
		})
		if err != nil {
			t.Fatalf("CreateUser failed: %v", err)
		}
		if resp.PK != 42 {
			t.Errorf("Unexpected user response: %+v", resp)
		}
	})

	t.Run("AddUserToGroupNamed", func(t *testing.T) {
		err := cli.AddUserToGroup(context.Background(), 42, "jellyfin-users")
		if err != nil {
			t.Fatalf("AddUserToGroup with name failed: %v", err)
		}
	})

	t.Run("CreateUser", func(t *testing.T) {
		resp, err := cli.CreateUser(context.Background(), UserCreatePayload{
			Username: "newuser",
			Email:    "new@example.com",
			IsActive: true,
		})
		if err != nil {
			t.Fatalf("CreateUser failed: %v", err)
		}
		if resp.PK != 42 || resp.ID != "uuid-test-1234" {
			t.Errorf("Unexpected user response: %+v", resp)
		}
	})

	t.Run("CreateRecoveryLink", func(t *testing.T) {
		link, err := cli.CreateRecoveryLink(context.Background(), 42)
		if err != nil {
			t.Fatalf("CreateRecoveryLink failed: %v", err)
		}
		if link != "https://auth.example.com/recovery/use-token/token123" {
			t.Errorf("Unexpected recovery link: %s", link)
		}
	})

	t.Run("SetUserActiveStatus", func(t *testing.T) {
		err := cli.SetUserActiveStatus(context.Background(), 42, false)
		if err != nil {
			t.Fatalf("SetUserActiveStatus failed: %v", err)
		}
	})

	t.Run("ListUsers", func(t *testing.T) {
		users, err := cli.ListUsers(context.Background())
		if err != nil {
			t.Fatalf("ListUsers failed: %v", err)
		}
		if len(users) != 1 || users[0].Username != "testuser" {
			t.Errorf("Unexpected users list: %+v", users)
		}
	})

	t.Run("GetUserByUsername", func(t *testing.T) {
		user, err := cli.GetUserByUsername(context.Background(), "testuser")
		if err != nil {
			t.Fatalf("GetUserByUsername failed: %v", err)
		}
		if user.Username != "testuser" || user.PK != 42 || len(user.Groups) != 2 {
			t.Errorf("Unexpected user details: %+v", user)
		}
	})

	t.Run("CreateInvitationStageToken", func(t *testing.T) {
		invID, err := cli.CreateInvitationStageToken(context.Background(), "JG-TEST", time.Now().Add(24*time.Hour), map[string]interface{}{"sponsor": "bob"}, true, "enrollment-flow-slug")
		if err != nil {
			t.Fatalf("CreateInvitationStageToken failed: %v", err)
		}
		if invID != "inv-uuid-999" {
			t.Errorf("Expected inv-uuid-999, got %s", invID)
		}
	})

	t.Run("ListInvitationStageTokens", func(t *testing.T) {
		tokens, err := cli.ListInvitationStageTokens(context.Background())
		if err != nil {
			t.Fatalf("ListInvitationStageTokens failed: %v", err)
		}
		if len(tokens) != 1 || tokens[0].PK != "inv-uuid-999" {
			t.Errorf("Unexpected invitation tokens list: %+v", tokens)
		}
	})

	t.Run("DeleteInvitationStageToken", func(t *testing.T) {
		err := cli.DeleteInvitationStageToken(context.Background(), "inv-uuid-999")
		if err != nil {
			t.Fatalf("DeleteInvitationStageToken failed: %v", err)
		}
	})

	t.Run("CreateRecoveryLinkByString", func(t *testing.T) {
		link, err := cli.CreateRecoveryLinkByString(context.Background(), "42")
		if err != nil {
			t.Fatalf("CreateRecoveryLinkByString failed: %v", err)
		}
		if link != "https://auth.example.com/recovery/use-token/token123" {
			t.Errorf("Unexpected recovery link: %s", link)
		}
	})

	t.Run("CreateInvitationStageToken_30Days", func(t *testing.T) {
		expiry30d := time.Now().AddDate(0, 0, 30)
		invID, err := cli.CreateInvitationStageToken(context.Background(), "JellyGate - eba500a91f82df6e79fb3c96e65df4fc (admin)", expiry30d, map[string]interface{}{"sponsor": "alice"}, false, "custom-enrollment-flow")
		if err != nil {
			t.Fatalf("CreateInvitationStageToken 30 days failed: %v", err)
		}
		if invID != "inv-uuid-999" {
			t.Errorf("Expected inv-uuid-999, got %s", invID)
		}
	})

	t.Run("SlugifyName", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"JellyGate - eba500a91f82df6e79fb3c96e65df4fc", "JellyGate-eba500a91f82df6e79fb3c96e65df4fc"},
			{"JellyGate - abc (user)", "JellyGate-abc-user"},
			{"jellygate-123_456", "jellygate-123_456"},
			{"", "jellygate-invitation"},
			{"   ", "jellygate-invitation"},
		}
		for _, tc := range tests {
			out := slugifyName(tc.input)
			if out != tc.expected {
				t.Errorf("slugifyName(%q) = %q; expected %q", tc.input, out, tc.expected)
			}
		}
	})

	t.Run("ResolveBaseURL", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"https://auth.example.com/application/o/jellygate/", "https://auth.example.com"},
			{"https://auth.example.com/application/o/jellygate", "https://auth.example.com"},
			{"https://auth.example.com/if/flow/default-enrollment-flow/?itoken=123", "https://auth.example.com"},
			{"https://auth.example.com/api/v3/core/users/", "https://auth.example.com"},
			{"https://authentik.mydomain.local:9000/", "https://authentik.mydomain.local:9000"},
			{"http://192.168.1.50:9000/application/o/test", "http://192.168.1.50:9000"},
			{"https://myhost.com/authentik/application/o/jellygate/", "https://myhost.com/authentik"},
			{"", ""},
			{"   ", ""},
		}
		for _, tc := range tests {
			out := ResolveBaseURL(tc.input)
			if out != tc.expected {
				t.Errorf("ResolveBaseURL(%q) = %q; expected %q", tc.input, out, tc.expected)
			}
		}
	})

	t.Run("GetBaseURL", func(t *testing.T) {
		if cli.GetBaseURL() != server.URL {
			t.Errorf("GetBaseURL() = %q; expected %q", cli.GetBaseURL(), server.URL)
		}
	})

	t.Run("CheckHealth", func(t *testing.T) {
		res := cli.CheckHealth(context.Background(), cfg)
		if res == nil {
			t.Fatal("CheckHealth returned nil")
		}
		if res.OverallStatus == "" {
			t.Errorf("OverallStatus should not be empty")
		}
		if _, ok := res.Components["api"]; !ok {
			t.Errorf("Components missing 'api' component")
		}
	})
}
