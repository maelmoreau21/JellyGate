package middleware

import (
	"net"
	"net/http"
	"strings"
)

// TrustedProxyRealIP crée un middleware HTTP qui n'extrait les en-têtes
// X-Forwarded-For et X-Real-IP QUE si la connexion TCP directe provient
// d'une adresse IP ou d'un masque CIDR de confiance (Known Proxies).
//
// Si le client TCP direct n'est pas dans la liste des proxys approuvés,
// les en-têtes de proxy sont strictement ignorés, empêchant toute usurpation
// d'adresse IP (IP spoofing), tout contournement de rate limiting et toute
// altération des journaux de sécurité.
func TrustedProxyRealIP(trustedCIDRs []string) func(http.Handler) http.Handler {
	nets := parseCIDRs(trustedCIDRs)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peerHost, peerPort := extractHostAndPort(r.RemoteAddr)
			peerIP := net.ParseIP(peerHost)

			// Si le pair direct n'est pas un proxy de confiance, on ne touche pas à RemoteAddr.
			if peerIP == nil || !isIPInNets(peerIP, nets) {
				next.ServeHTTP(w, r)
				return
			}

			// Le pair direct est un proxy de confiance : on recherche l'IP réelle du client.
			if clientIP := resolveRealClientIP(r, nets); clientIP != "" {
				if peerPort != "" {
					r.RemoteAddr = net.JoinHostPort(clientIP, peerPort)
				} else {
					r.RemoteAddr = clientIP
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func parseCIDRs(rawList []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, item := range rawList {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}

		// Si c'est une IP sans masque CIDR, ajouter /32 ou /128
		if !strings.Contains(trimmed, "/") {
			parsedIP := net.ParseIP(trimmed)
			if parsedIP == nil {
				continue
			}
			if parsedIP.To4() != nil {
				trimmed += "/32"
			} else {
				trimmed += "/128"
			}
		}

		_, ipNet, err := net.ParseCIDR(trimmed)
		if err == nil && ipNet != nil {
			nets = append(nets, ipNet)
		}
	}
	return nets
}

func isIPInNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func extractHostAndPort(remoteAddr string) (string, string) {
	trimmed := strings.TrimSpace(remoteAddr)
	if trimmed == "" {
		return "", ""
	}
	host, port, err := net.SplitHostPort(trimmed)
	if err == nil {
		return strings.TrimSpace(host), strings.TrimSpace(port)
	}
	return trimmed, ""
}

// resolveRealClientIP extrait l'IP cliente la plus externe non approuvée depuis X-Forwarded-For
// ou se rabat sur X-Real-IP.
func resolveRealClientIP(r *http.Request, trustedNets []*net.IPNet) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		// Parcourt de droite à gauche pour trouver le premier saut non-proxy (le vrai client)
		for i := len(ips) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(ips[i])
			host, _ := extractHostAndPort(candidate)
			if host == "" {
				host = candidate
			}
			parsed := net.ParseIP(host)
			if parsed == nil {
				continue
			}
			// Dès qu'une IP n'est pas un proxy de confiance, c'est l'IP client légitime
			if !isIPInNets(parsed, trustedNets) {
				return parsed.String()
			}
		}
		// Si toutes les IP sont dans des réseaux de confiance, prendre la première (leftmost)
		if len(ips) > 0 {
			leftmost := strings.TrimSpace(ips[0])
			host, _ := extractHostAndPort(leftmost)
			if host == "" {
				host = leftmost
			}
			if parsed := net.ParseIP(host); parsed != nil {
				return parsed.String()
			}
		}
	}

	if xRealIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); xRealIP != "" {
		host, _ := extractHostAndPort(xRealIP)
		if host == "" {
			host = xRealIP
		}
		if parsed := net.ParseIP(host); parsed != nil {
			return parsed.String()
		}
	}

	return ""
}
