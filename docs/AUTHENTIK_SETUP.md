# GUIDE DE CONFIGURATION AUTHENTIK POUR JELLYGATE

## 1. Type de Flux : Explicite ou Implicite ?

> [!IMPORTANT]
> Il ne faut pas confondre le **protocole OAuth2** et le **flow de consentement Authentik** :
>
> 1. **Protocole OAuth2 / OIDC (Côté JellyGate)** : **FLUX EXPLICITE (Authorization Code Grant avec PKCE)**
>    - JellyGate utilise exclusivement le flux **Authorization Code avec PKCE (S256)** (`response_type=code`).
>    - Le token JWT n'est **jamais** exposé dans l'URL du navigateur. Le code temporaire est échangé de manière sécurisée de serveur à serveur (JellyGate backend ↔ Authentik).
>    - Le flux *Implicit Flow* (déprécié dans OAuth 2.1) n'est **pas** utilisé.
>
> 2. **Flow d'Autorisation Authentik (Consentement utilisateur)** :
>    - **`default-provider-authorization-implicit-consent` (RECOMMANDÉ)** : Connexion directe et fluide sans écran de consentement intermédiaire ("Autoriser JellyGate à lire mon profil").
>    - **`default-provider-authorization-explicit-consent` (Optionnel)** : Affiche un écran de confirmation où l'utilisateur doit cliquer sur "Accepter" à chaque première connexion.

---

## 2. Configuration du Provider dans Authentik

Dans l'interface administrateur d'Authentik (**Applications > Providers > Créer un Provider**) :

1. **Type de Provider** : `OAuth2/OpenID Provider`
2. **Nom** : `JellyGate Provider`
3. **Authorization Flow** : `default-provider-authorization-implicit-consent` *(Consentement automatique recommandé)*
4. **Client Type** : `Confidential`
5. **Client ID & Client Secret** : Générés automatiquement par Authentik (à copier dans votre `.env`)
6. **Redirect URIs / URLs de redirection (Strict matching)** :
   ```
   https://jellygate.example.com/auth/callback
   ```
   *(ou `http://localhost:8097/auth/callback` pour un test local, ou `http://192.168.1.50:8097/auth/callback`)*
7. **Allowed Origins / Origines (regex) (Pour CORS & redirections)** :
   ```
   ^https://jellygate\.example\.com$
   ```
   *(ou `https://jellygate.example.com` ou `^http://localhost:8097$`)*
8. **Signing Key** : Choisir le certificat / clé RSA par défaut d'Authentik.
9. **Scopes requis** : `openid`, `email`, `profile`, `groups` *(intégrés automatiquement par Authentik)*.

---

## 3. Groupes & Token API dans Authentik

1. **Créer les Groupes** (**Directory > Groups**) :
   - `jellygate-users` : Autorise la connexion au portail JellyGate.
   - `jellygate-admins` : Donne les privilèges administrateur sur JellyGate.
   - `jellyfin-users` : Attribué aux membres pour l'accès Jellyfin (via Outpost LDAP).

2. **Créer un Service Account (Token API)** (**Directory > Service Accounts**) :
   - Nom : `jellygate-service`
   - Créer un Token associé et copier le jeton (il servira pour `AUTHENTIK_API_TOKEN`).

3. **Flow d'Enrollment (Optionnel)** :
   - Flow d'inscription par défaut : `default-enrollment-flow`

---

## 4. Configuration des variables JellyGate (.env)

### 📌 Que mettre dans `OIDC_URL` ?

* **Recommandé (avec le slug de l'application Authentik)** :
  `https://auth.example.com/application/o/jellygate/`
* **Ou simplement le nom de domaine Authentik** :
  `https://auth.example.com` *(JellyGate ajoutera automatiquement le chemin `/application/o/jellygate/` par défaut)*.

```env
# ── Authentik SSO (OIDC) ───────────────────────────────────────────────────
OIDC_ENABLED=true
# URL complète avec le slug Authentik (ou simplement le nom de domaine) :
OIDC_URL=https://auth.example.com/application/o/jellygate/
OIDC_CLIENT_ID=votre_client_id
OIDC_CLIENT_SECRET=votre_client_secret

# ── Authentik API ──────────────────────────────────────────────────────────
AUTHENTIK_ENABLED=true
AUTHENTIK_URL=https://auth.example.com
AUTHENTIK_API_TOKEN=votre_token_api_authentik
```

---

## 5. Test de validation

1. Rendez-vous sur votre JellyGate : **Administration > Authentik** (`/admin/authentik`).
2. Cliquez sur le bouton **[Tester la connexion]**.
3. Les 4 vérifications doivent être vertes (**OK**) :
   - ✅ Connexion API REST
   - ✅ OIDC Discovery
   - ✅ Enrollment Flow
   - ✅ Groupes Requis

---

## 6. Connexion de Secours Locale (`/local`)

Par défaut, l'accès local est **désactivé** pour garantir un environnement 100% SSO Authentik.

Pour activer un compte local de secours (break-glass) en cas d'urgence ou de panne d'Authentik :
1. Définissez dans votre `.env` :
   ```env
   JELLYGATE_LOCAL_ADMIN_USER=admin
   JELLYGATE_LOCAL_ADMIN_PASSWORD=votre_mot_de_passe_de_secours
   ```
2. Accédez à l'URL : `https://jellygate.votredomaine.com/local` (ou via le lien **Connexion de secours** sur la page de connexion).
3. Connectez-vous avec vos identifiants locaux d'urgence pour accéder à la page **Paramètres > Authentik** (`/admin/authentik`), tester la connexion et corriger la configuration.



