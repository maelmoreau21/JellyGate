# ARSENAL DE SÉCURITÉ JELLYGATE & OIDC PKCE

## 1. Sécurité de l'Authentification OIDC

JellyGate implémente les meilleures pratiques en matière de sécurité OIDC / OAuth2 :

- **Flux Authorization Code + PKCE (S256)** : Prévention des attaques d'interception de code d'autorisation.
- **Crypto-Random Secret Generation** : Les paramètres `state`, `nonce` et `code_verifier` sont générés exclusivement avec `crypto/rand`.
- **Validation JWT Stricte (RS256 & JWKS)** :
  - Rejet systématique de l'algorithme `none` et des algorithmes symétriques HMAC pour les token d'identité.
  - Vérification cryptographique des signatures via les clés publiques RSA publiées sur le document JWKS Authentik (`jwks_uri`).
  - Validation stricte de l'Issuer (`iss`) et de l'Audience (`aud` = `client_id`).
  - Validation du `nonce` en temps constant (`hmac.Equal`) pour prévenir les attaques par rejeu.
  - Validation de la date d'expiration (`exp`) et de création (`iat`) avec une fenêtre de tolérance d'horloge de 60s (clock skew).

## 2. Sécurité des Cookies & Session

- **Headers HTTP Sécurisés** : `HttpOnly`, `SameSite=Lax` (ou `Strict` pour logout), `Secure` (sur HTTPS).
- **Signature Cryptographique HSL** : Les cookies de session JellyGate sont signés avec un HMAC-SHA256 basé sur `JELLYGATE_SECRET` (clé de 32 caractères minimum obligatoire).

## 3. Protection Anti-Abus & Rate Limiting

- **Rate Limiting sur les formulaires d'invitation** : Limitation du nombre de soumissions par IP.
- **CAPTCHA Anti-Bot** : CAPTCHA mathématique optionnel activable dans l'administration.
- **Journalisation de Sécurité (`security_events`)** : Enregistrement des tentatives de connexion échouées et des événements suspects avec adresse IP et métadonnées.

## 4. Validation des Secrets & Compte de Secours Local

- **Validation Stricte de `JELLYGATE_SECRET`** : Clé obligatoire d'au moins 32 caractères, rejet des clés d'exemples connues ou prévisibles, contrôle d'entropie (au moins 6 caractères distincts).
- **Verrouillage du Compte Local (`LocalAdmin`)** : Si activé, mot de passe obligatoire d'au moins 12 caractères et rejet des mots de passe triviaux.

## 5. Proxys de Confiance (Known Proxies) & Anti-Spoofing IP

- **Middleware `TrustedProxyRealIP`** : Filtrage strict par sous-réseaux CIDR (`JELLYGATE_TRUSTED_PROXIES`). Seules les requêtes TCP directes provenant de proxys de confiance voient leurs en-têtes `X-Forwarded-For` ou `X-Real-IP` interprétés. Tout en-tête provenant d'un tiers non autorisé est strictement ignoré.

## 6. En-têtes HTTP Durcis & Filtrage Statique

- **Headers HTTP Sécurisés** : `HSTS` avec `preload`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy` restrictive (`geolocation=(), microphone=(), camera=(), payment=(), usb=(), display-capture=()`).
- **Content-Security-Policy (CSP)** : Isolation stricte avec génération de nonces cryptographiques dynamiques par page.
- **`StaticFileFilter`** : Blocage systématique de l'accès aux fichiers cachés (`.*`), fichiers de configuration (`.env*`), scripts et fichiers de base de données sous `/static/*`.

Pour le guide d'application complet avec templates Nginx, Caddy et Jellyfin, consultez [docs/SECURITY_HARDENING.md](file:///c:/Users/Mael/Documents/GitHub/JellyGate/docs/SECURITY_HARDENING.md).

