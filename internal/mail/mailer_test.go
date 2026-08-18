package mail

import (
	"strings"
	"testing"
)

func TestStripHTMLTags(t *testing.T) {
	htmlInput := `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <style>
    body { background-color: #0b0f19; color: #fff; }
    .card { padding: 32px; }
  </style>
  <script>console.log("secret");</script>
</head>
<body>
  <div class="badge">JellyGate · Test SMTP</div>
  <h1>Connexion SMTP réussie !</h1>
  <p>Bonjour <strong>mmoreau</strong>,<br>Ce message confirme que la configuration est prête.</p>
  <table>
    <tr><td>Destinataire</td><td>user@example.com</td></tr>
  </table>
</body>
</html>`

	plain := stripHTMLTags(htmlInput)

	if strings.Contains(plain, "background-color") || strings.Contains(plain, "body {") || strings.Contains(plain, ".card") {
		t.Fatalf("stripHTMLTags failed: CSS code leaked into plain text: %q", plain)
	}

	if strings.Contains(plain, "console.log") {
		t.Fatalf("stripHTMLTags failed: JavaScript leaked into plain text: %q", plain)
	}

	if !strings.Contains(plain, "Connexion SMTP réussie !") {
		t.Fatalf("stripHTMLTags failed: missing expected content, got: %q", plain)
	}

	if !strings.Contains(plain, "Bonjour mmoreau") {
		t.Fatalf("stripHTMLTags failed: missing expected body content, got: %q", plain)
	}

	if !strings.Contains(plain, "Destinataire") || !strings.Contains(plain, "user@example.com") {
		t.Fatalf("stripHTMLTags failed: missing table content, got: %q", plain)
	}
}
