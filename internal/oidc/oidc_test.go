package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

func TestGenerateAuthURL(t *testing.T) {
	cfg := config.AuthentikConfig{
		URL:          "https://auth.example.com",
		IssuerURL:    "https://auth.example.com/application/o/jellygate/",
		ClientID:     "jellygate-client-id",
		ClientSecret: "secret",
		RedirectURL:  "http://localhost:8097/auth/callback",
		UserGroup:    "jellygate-users",
		AdminGroup:   "jellygate-admins",
	}

	client := NewClient(cfg)
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()

	authURL, err := client.GenerateAuthURL(rec, req)
	if err != nil {
		t.Fatalf("GenerateAuthURL failed: %v", err)
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("Failed to parse generated auth URL: %v", err)
	}

	q := u.Query()
	if q.Get("client_id") != "jellygate-client-id" {
		t.Errorf("Expected client_id jellygate-client-id, got %s", q.Get("client_id"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("Expected response_type code, got %s", q.Get("response_type"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("Expected code_challenge_method S256, got %s", q.Get("code_challenge_method"))
	}
	if q.Get("state") == "" {
		t.Error("Expected non-empty state in query")
	}
	if q.Get("nonce") == "" {
		t.Error("Expected non-empty nonce in query")
	}
	if q.Get("code_challenge") == "" {
		t.Error("Expected non-empty code_challenge in query")
	}

	cookies := rec.Result().Cookies()
	foundState := false
	foundNonce := false
	foundVerifier := false
	for _, c := range cookies {
		if c.Name == CookieState {
			foundState = true
		}
		if c.Name == CookieNonce {
			foundNonce = true
		}
		if c.Name == CookieVerifier {
			foundVerifier = true
		}
	}

	if !foundState || !foundNonce || !foundVerifier {
		t.Errorf("Cookies missing. foundState=%v, foundNonce=%v, foundVerifier=%v", foundState, foundNonce, foundVerifier)
	}
}

func createSignedRS256JWT(claims Claims, privKey *rsa.PrivateKey, kid string) string {
	header := map[string]string{"alg": "RS256", "typ": "JWT", "kid": kid}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)

	hEnc := base64.RawURLEncoding.EncodeToString(headerJSON)
	cEnc := base64.RawURLEncoding.EncodeToString(claimsJSON)

	payload := hEnc + "." + cEnc
	hashed := sha256.Sum256([]byte(payload))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hashed[:])
	sEnc := base64.RawURLEncoding.EncodeToString(sig)

	return payload + "." + sEnc
}

func TestValidateIDToken(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	kid := "test-key-id-1"
	nStr := base64.RawURLEncoding.EncodeToString(privKey.N.Bytes())
	eBytes := big.NewInt(int64(privKey.E)).Bytes()
	eStr := base64.RawURLEncoding.EncodeToString(eBytes)

	cfg := config.AuthentikConfig{
		IssuerURL: "https://auth.example.com/application/o/jellygate/",
		ClientID:  "test-client-id",
	}

	cImpl := &oidcClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		jwksCache:  make(map[string]*rsa.PublicKey),
	}
	cImpl.jwksCache[kid] = &privKey.PublicKey
	cImpl.jwksLast = time.Now()

	client := Client(cImpl)

	validClaims := Claims{
		Sub:               "user-uuid-1234",
		PreferredUsername: "john_doe",
		Email:             "john@example.com",
		EmailVerified:     true,
		Groups:            []string{"jellygate-users"},
		Issuer:            "https://auth.example.com/application/o/jellygate",
		Audience:          "test-client-id",
		Expiration:        time.Now().Add(1 * time.Hour).Unix(),
		Nonce:             "test-nonce-123",
	}

	t.Run("Valid Token RS256", func(t *testing.T) {
		tokenStr := createSignedRS256JWT(validClaims, privKey, kid)
		claims, err := client.ValidateIDToken(context.Background(), tokenStr, "test-nonce-123")
		if err != nil {
			t.Fatalf("Validation failed for valid RS256 token: %v", err)
		}
		if claims.Sub != "user-uuid-1234" {
			t.Errorf("Expected sub user-uuid-1234, got %s", claims.Sub)
		}
	})

	t.Run("Reject alg none", func(t *testing.T) {
		header := map[string]string{"alg": "none", "typ": "JWT", "kid": kid}
		headerJSON, _ := json.Marshal(header)
		claimsJSON, _ := json.Marshal(validClaims)
		tokenStr := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON) + "."

		_, err := client.ValidateIDToken(context.Background(), tokenStr, "test-nonce-123")
		if err == nil || !strings.Contains(err.Error(), "unsupported JWT algorithm") {
			t.Errorf("Expected unsupported JWT algorithm error, got %v", err)
		}
	})

	t.Run("Expired Token", func(t *testing.T) {
		expiredClaims := validClaims
		expiredClaims.Expiration = time.Now().Add(-2 * time.Hour).Unix()
		tokenStr := createSignedRS256JWT(expiredClaims, privKey, kid)

		_, err := client.ValidateIDToken(context.Background(), tokenStr, "test-nonce-123")
		if err == nil || !strings.Contains(err.Error(), "expired") {
			t.Errorf("Expected expired token error, got %v", err)
		}
	})

	t.Run("Bad Issuer", func(t *testing.T) {
		badIssClaims := validClaims
		badIssClaims.Issuer = "https://malicious-issuer.com"
		tokenStr := createSignedRS256JWT(badIssClaims, privKey, kid)

		_, err := client.ValidateIDToken(context.Background(), tokenStr, "test-nonce-123")
		if err == nil || !strings.Contains(err.Error(), "issuer") {
			t.Errorf("Expected issuer error, got %v", err)
		}
	})

	t.Run("Bad Nonce", func(t *testing.T) {
		tokenStr := createSignedRS256JWT(validClaims, privKey, kid)
		_, err := client.ValidateIDToken(context.Background(), tokenStr, "mismatched-nonce")
		if err == nil || !strings.Contains(err.Error(), "nonce") {
			t.Errorf("Expected nonce error, got %v", err)
		}
	})

	t.Run("Missing Nonce when expected", func(t *testing.T) {
		noNonceClaims := validClaims
		noNonceClaims.Nonce = ""
		tokenStr := createSignedRS256JWT(noNonceClaims, privKey, kid)

		_, err := client.ValidateIDToken(context.Background(), tokenStr, "expected-some-nonce")
		if err == nil || !strings.Contains(err.Error(), "missing nonce") {
			t.Errorf("Expected missing nonce error, got %v", err)
		}
	})

	_ = nStr
	_ = eStr
}

func TestDetermineUserRole(t *testing.T) {
	cfg := config.AuthentikConfig{
		UserGroup:  "jellygate-users",
		AdminGroup: "jellygate-admins",
	}
	client := NewClient(cfg)

	t.Run("Admin Group", func(t *testing.T) {
		isAdmin, hasAccess := client.DetermineUserRole([]string{"jellygate-users", "jellygate-admins"})
		if !isAdmin || !hasAccess {
			t.Errorf("Expected isAdmin=true, hasAccess=true; got isAdmin=%v, hasAccess=%v", isAdmin, hasAccess)
		}
	})

	t.Run("User Group Only", func(t *testing.T) {
		isAdmin, hasAccess := client.DetermineUserRole([]string{"jellygate-users", "other-group"})
		if isAdmin || !hasAccess {
			t.Errorf("Expected isAdmin=false, hasAccess=true; got isAdmin=%v, hasAccess=%v", isAdmin, hasAccess)
		}
	})

	t.Run("Unauthorized Group", func(t *testing.T) {
		isAdmin, hasAccess := client.DetermineUserRole([]string{"some-unrelated-group"})
		if isAdmin || hasAccess {
			t.Errorf("Expected isAdmin=false, hasAccess=false; got isAdmin=%v, hasAccess=%v", isAdmin, hasAccess)
		}
	})

	t.Run("Empty Groups List", func(t *testing.T) {
		isAdmin, hasAccess := client.DetermineUserRole([]string{})
		if isAdmin || hasAccess {
			t.Errorf("Expected isAdmin=false, hasAccess=false for empty groups; got isAdmin=%v, hasAccess=%v", isAdmin, hasAccess)
		}
	})
}

