package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimitByIP_BasicAndUserAgentBypass(t *testing.T) {
	// Set up a simple rate limiter: 3 requests allowed per minute
	limit := 3
	window := 1 * time.Minute
	mw := RateLimitByIP(limit, window)

	// Create a dummy handler to wrap
	handlerCalledCount := 0
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalledCount++
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw(dummyHandler)

	// Helper to send request
	sendReq := func(ip, userAgent, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = ip
		if userAgent != "" {
			req.Header.Set("User-Agent", userAgent)
		}
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
		return rr
	}

	// 1. Send first 3 requests with different User-Agents from the same IP and path.
	// All should be allowed.
	userAgents := []string{"Mozilla/5.0", "Chrome/110.0", "Safari/16.0"}
	for i, ua := range userAgents {
		rr := sendReq("1.2.3.4:1234", ua, "/login")
		if rr.Code != http.StatusOK {
			t.Errorf("Request %d (UA=%s) should be allowed, got status %d", i+1, ua, rr.Code)
		}
	}

	if handlerCalledCount != 3 {
		t.Fatalf("Expected handler to be called 3 times, got %d", handlerCalledCount)
	}

	// 2. The 4th request from the same IP and path, even with another User-Agent, must be blocked.
	rrBlocked := sendReq("1.2.3.4:1234", "Edge/110.0", "/login")
	if rrBlocked.Code != http.StatusTooManyRequests {
		t.Errorf("4th request should be blocked with 429, got status %d", rrBlocked.Code)
	}

	// Verify headers on blocked request
	if limitHeader := rrBlocked.Header().Get("X-RateLimit-Limit"); limitHeader != "3" {
		t.Errorf("X-RateLimit-Limit = %s, want 3", limitHeader)
	}
	if remainingHeader := rrBlocked.Header().Get("X-RateLimit-Remaining"); remainingHeader != "0" {
		t.Errorf("X-RateLimit-Remaining = %s, want 0", remainingHeader)
	}
	if retryAfter := rrBlocked.Header().Get("Retry-After"); retryAfter == "" {
		t.Error("Retry-After header is missing")
	}

	// 3. A request from a DIFFERENT IP but same path and User-Agent should be allowed (isolated rate limit).
	rrDifferentIP := sendReq("5.6.7.8:1234", "Mozilla/5.0", "/login")
	if rrDifferentIP.Code != http.StatusOK {
		t.Errorf("Request from different IP should be allowed, got status %d", rrDifferentIP.Code)
	}

	// 4. A request from the original IP but on a DIFFERENT path should be allowed (isolated rate limit).
	rrDifferentPath := sendReq("1.2.3.4:1234", "Mozilla/5.0", "/other-path")
	if rrDifferentPath.Code != http.StatusOK {
		t.Errorf("Request to different path should be allowed, got status %d", rrDifferentPath.Code)
	}
}
