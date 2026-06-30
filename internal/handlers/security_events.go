package handlers

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/maelmoreau21/JellyGate/internal/database"
)

// requestIP extracts the client IP from the request.
// It only trusts proxy headers (X-Forwarded-For, X-Real-IP) when trustProxy
// is true. Without this guard, any client can forge these headers to
// falsify audit logs and potentially bypass IP-based rate limiting.
func requestIP(r *http.Request) string {
	return requestIPTrusted(r, false)
}

func requestIPTrusted(r *http.Request, trustProxy bool) string {
	if r == nil {
		return ""
	}
	if trustProxy {
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			if len(parts) > 0 {
				if ip := strings.TrimSpace(parts[0]); ip != "" {
					return ip
				}
			}
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			return realIP
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func logSecurityEvent(db *database.DB, r *http.Request, category, eventType, severity, actor, target, message string, metadata map[string]string) {
	if db == nil {
		return
	}
	meta := ""
	if len(metadata) > 0 {
		keys := make([]string, 0, len(metadata))
		for key := range metadata {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s=%s", key, strings.TrimSpace(metadata[key])))
		}
		meta = strings.Join(parts, "; ")
	}
	_ = db.LogSecurityEvent(database.SecurityEvent{
		Category:  category,
		EventType: eventType,
		Severity:  severity,
		Actor:     actor,
		Target:    target,
		IP:        requestIP(r),
		Message:   message,
		Metadata:  meta,
	})
}
