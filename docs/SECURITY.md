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
