package scripts

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/session"
)

func readSecretFromEnvFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"")
		if key == "JELLYGATE_SECRET_KEY" {
			return val
		}
	}
	return ""
}

// RunGenerateSessionCookie generates and prints a signed session cookie.
// This used to be a standalone `main` in scripts/ but was converted
// to a library-style function so the package compiles with the rest
// of the repository (allowing `go build ./...`).
func RunGenerateSessionCookie() {
	secret := os.Getenv("JELLYGATE_SECRET_KEY")
	if secret == "" {
		secret = readSecretFromEnvFile(".env.local")
	}
	if secret == "" {
		fmt.Fprintln(os.Stderr, "JELLYGATE_SECRET_KEY not set and not found in .env.local")
		os.Exit(2)
	}

	payload := session.Payload{
		UserID:   "1",
		Username: "admin",
		IsAdmin:  true,
		Exp:      time.Now().Add(24 * time.Hour).Unix(),
	}

	cookie, err := session.Sign(payload, secret)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error signing cookie:", err)
		os.Exit(1)
	}

	fmt.Println(cookie)
}
