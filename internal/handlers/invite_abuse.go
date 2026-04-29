package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

type inviteAbuseState struct {
	failures  []time.Time
	blockedTo time.Time
}

type inviteAbuseTracker struct {
	mu       sync.Mutex
	attempts map[string]*inviteAbuseState
}

func newInviteAbuseTracker() *inviteAbuseTracker {
	return &inviteAbuseTracker{attempts: map[string]*inviteAbuseState{}}
}

func (h *InvitationHandler) inviteAntiAbuseConfig() config.AntiAbuseConfig {
	productCfg, err := h.db.GetProductFeaturesConfig()
	if err != nil {
		return config.DefaultProductFeaturesConfig().AntiAbuse
	}
	return config.NormalizeProductFeaturesConfig(productCfg).AntiAbuse
}

func inviteClientKey(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func (h *InvitationHandler) isInviteBlocked(r *http.Request, cfg config.AntiAbuseConfig) (bool, time.Duration) {
	if h == nil || h.abuse == nil || !cfg.Enabled {
		return false, 0
	}

	key := inviteClientKey(r)
	now := time.Now()
	h.abuse.mu.Lock()
	defer h.abuse.mu.Unlock()

	state := h.abuse.attempts[key]
	if state == nil {
		return false, 0
	}
	if state.blockedTo.After(now) {
		return true, state.blockedTo.Sub(now)
	}
	if !state.blockedTo.IsZero() {
		state.blockedTo = time.Time{}
	}
	return false, 0
}

func (h *InvitationHandler) recordInviteFailure(r *http.Request, cfg config.AntiAbuseConfig) {
	if h == nil || h.abuse == nil || !cfg.Enabled {
		return
	}

	key := inviteClientKey(r)
	now := time.Now()
	windowStart := now.Add(-time.Duration(cfg.WindowMinutes) * time.Minute)

	h.abuse.mu.Lock()
	defer h.abuse.mu.Unlock()

	state := h.abuse.attempts[key]
	if state == nil {
		state = &inviteAbuseState{}
		h.abuse.attempts[key] = state
	}

	filtered := state.failures[:0]
	for _, failureAt := range state.failures {
		if failureAt.After(windowStart) {
			filtered = append(filtered, failureAt)
		}
	}
	state.failures = append(filtered, now)
	if len(state.failures) >= cfg.MaxFailures {
		state.blockedTo = now.Add(time.Duration(cfg.BlockMinutes) * time.Minute)
	}
}

func (h *InvitationHandler) recordInviteSuccess(r *http.Request) {
	if h == nil || h.abuse == nil {
		return
	}
	key := inviteClientKey(r)
	h.abuse.mu.Lock()
	delete(h.abuse.attempts, key)
	h.abuse.mu.Unlock()
}

type inviteCaptchaPayload struct {
	A       int    `json:"a"`
	B       int    `json:"b"`
	Expires int64  `json:"expires"`
	Nonce   string `json:"nonce"`
}

func (h *InvitationHandler) newInviteCaptchaChallenge() (string, string) {
	a := secureSmallInt(2, 9)
	b := secureSmallInt(2, 9)
	payload := inviteCaptchaPayload{
		A:       a,
		B:       b,
		Expires: time.Now().Add(10 * time.Minute).Unix(),
		Nonce:   randomTokenFragment(),
	}
	raw, _ := json.Marshal(payload)
	sig := h.signInviteCaptcha(raw)
	token := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(sig)
	return fmt.Sprintf("%d + %d", a, b), token
}

func (h *InvitationHandler) verifyInviteCaptcha(r *http.Request, cfg config.AntiAbuseConfig) error {
	if !cfg.Enabled || !cfg.Captcha {
		return nil
	}
	token := strings.TrimSpace(r.FormValue("captcha_token"))
	answerRaw := strings.TrimSpace(r.FormValue("captcha_answer"))
	if token == "" || answerRaw == "" {
		return fmt.Errorf("captcha requis")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return fmt.Errorf("captcha invalide")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("captcha invalide")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("captcha invalide")
	}
	expectedSig := h.signInviteCaptcha(raw)
	if !hmac.Equal(sig, expectedSig) {
		return fmt.Errorf("captcha invalide")
	}

	var payload inviteCaptchaPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("captcha invalide")
	}
	if payload.Expires < time.Now().Unix() {
		return fmt.Errorf("captcha expire")
	}

	answer, err := strconv.Atoi(answerRaw)
	if err != nil || answer != payload.A+payload.B {
		return fmt.Errorf("captcha incorrect")
	}
	return nil
}

func (h *InvitationHandler) signInviteCaptcha(raw []byte) []byte {
	secret := ""
	if h != nil && h.cfg != nil {
		secret = h.cfg.SecretKey
	}
	mac := hmac.New(sha256.New, []byte("jellygate-invite-captcha:"+secret))
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}

func secureSmallInt(minValue, maxValue int) int {
	if maxValue <= minValue {
		return minValue
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(maxValue-minValue+1)))
	if err != nil {
		return minValue
	}
	return minValue + int(n.Int64())
}

func randomTokenFragment() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
