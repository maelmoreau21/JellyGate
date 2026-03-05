<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go" alt="Go">
  <img src="https://img.shields.io/github/v/release/maelmoreau21/JellyGate?style=flat-square&color=8b5cf6" alt="Release">
  <img src="https://img.shields.io/badge/Docker-Multi--arch-2496ED?style=flat-square&logo=docker" alt="Docker">
  <img src="https://img.shields.io/badge/Platforms-amd64%20%7C%20arm64-informational?style=flat-square" alt="Platforms">
  <img src="https://img.shields.io/github/license/maelmoreau21/JellyGate?style=flat-square" alt="License">
</p>

<h1 align="center">🎬 JellyGate</h1>

<p align="center">
  <strong>Gestionnaire d'invitations, de récupération de mots de passe et d'utilisateurs pour Jellyfin.</strong><br>
  Alternative moderne à <a href="https://github.com/hrfee/jfa-go">jfa-go</a>, avec support natif de <strong>Active Directory</strong> (LDAP).
</p>

---

## ✨ Fonctionnalités

| Fonctionnalité | Description |
|---|---|
| 🎫 **Invitations** | Liens d'invitation avec quotas, expiration et profils Jellyfin personnalisés |
| 🔐 **Active Directory natif** | Création automatique des comptes dans l'annuaire LDAP/LDAPS (unicodePwd UTF-16LE) |
| 👥 **Gestion utilisateurs** | Dashboard admin : activation, désactivation, suppression (AD + Jellyfin + SQLite) |
| 🔑 **Réinitialisation MDP** | Flux sécurisé par email avec reset simultané AD + Jellyfin |
| 📨 **Notifications** | Discord, Telegram et Matrix en temps réel (webhooks asynchrones) |
| 🌍 **i18n** | Français / Anglais avec détection automatique (cookie, `Accept-Language`, défaut) |
| 🎨 **UI moderne** | Dark theme, glassmorphism, Tailwind CSS, animations fluides |
| 🐳 **Docker multi-arch** | Images `amd64` + `arm64` (~15 Mo), CI/CD via GitHub Actions |
| 📧 **Emails** | Templates HTML modernes (bienvenue, reset, invitation) via SMTP |
| 🔒 **Sécurité** | Sessions HMAC-SHA256, cookies HttpOnly/Secure/SameSite, auth déléguée à Jellyfin |

## 🏗️ Architecture

```
JellyGate
│
├── Jellyfin (API REST) ←── Authentification admin + gestion utilisateurs
├── Active Directory (LDAPS) ←── Création comptes + mots de passe (unicodePwd)
├── SQLite (local)      ←── Invitations, utilisateurs, logs, tokens
├── SMTP                ←── Emails transactionnels
└── Webhooks            ←── Discord / Telegram / Matrix
```

## 🚀 Démarrage rapide

### 1. Prérequis

- Docker et Docker Compose installés
- Un serveur **Jellyfin** accessible avec une **clé API**
- Un serveur **Active Directory** 
- Un serveur **SMTP** fonctionnel

### 2. Configuration

```bash
# Cloner le dépôt
git clone https://github.com/maelmoreau21/JellyGate.git
cd JellyGate

# Copier et éditer la configuration
cp .env.example .env
nano .env

# Générer la clé secrète
echo "JELLYGATE_SECRET_KEY=$(openssl rand -hex 32)" >> .env
```

### 3. Lancement

```yaml
# docker-compose.yml
services:
  jellygate:
    image: ghcr.io/maelmoreau21/jellygate:latest
    container_name: jellygate
    restart: unless-stopped
    ports:
      - "8097:8097"
    volumes:
      - jellygate_data:/data
    env_file:
      - .env
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8097/"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s

volumes:
  jellygate_data:
    driver: local
```

```bash
docker compose up -d
```

### 4. Connexion admin

1. Accédez à `http://votre-ip:8097/admin/login`
2. Connectez-vous avec vos **identifiants Jellyfin administrateur**
3. JellyGate vérifie vos identifiants via l'API Jellyfin et crée une session sécurisée

> **Note** : JellyGate n'a **pas de base d'utilisateurs admin propre**. L'authentification est entièrement déléguée à Jellyfin. Tout utilisateur Jellyfin ayant `Policy.IsAdministrator = true` peut accéder au dashboard.

### 5. Créer une invitation

1. Dans le dashboard, créez une nouvelle invitation
2. Partagez le lien généré (`http://votre-ip:8097/invite/{code}`)
3. L'utilisateur remplit le formulaire → son compte est créé atomiquement dans **AD + Jellyfin + SQLite**

## ⚙️ Variables d'environnement

Voir [`.env.example`](.env.example) pour la liste complète.

> LDAP, SMTP et Webhooks se configurent désormais dans l'interface admin (stockage SQLite), pas via `.env`.

| Variable | Requis | Défaut | Description |
|---|---|---|---|
| `JELLYGATE_SECRET_KEY` | ✅ | — | Clé de signature des sessions (min. 32 car.) |
| `JELLYFIN_URL` | ✅ | — | URL du serveur Jellyfin |
| `JELLYFIN_API_KEY` | ✅ | — | Clé API Jellyfin |
| `JELLYGATE_PORT` | ❌ | `8097` | Port d'écoute |
| `JELLYGATE_BASE_URL` | ❌ | `http://localhost:8097` | URL publique JellyGate |
| `JELLYGATE_DATA_DIR` | ❌ | `/data` | Dossier de persistance |
| `JELLYGATE_DEFAULT_LANG` | ❌ | `fr` | Langue par défaut (`fr`/`en`) |
| `JELLYSEERR_URL` | ❌ | — | URL Jellyseerr (provisionnement automatique) |
| `JELLYSEERR_API_KEY` | ❌ | — | Clé API Jellyseerr |
| `OMBI_URL` | ❌ | — | URL Ombi (provisionnement automatique) |
| `OMBI_API_KEY` | ❌ | — | Clé API Ombi |

## 🏠 Mode Home Server (Groupes + Expiration + Provisioning)

### 1. Activer Jellyseerr/Ombi (optionnel)

Ajoutez dans `.env` au moins un couple URL + API key :

```bash
JELLYSEERR_URL=http://jellyseerr:5055
JELLYSEERR_API_KEY=...

# ou
OMBI_URL=http://ombi:3579
OMBI_API_KEY=...
```

Si ces variables sont absentes, l'inscription continue normalement sans provisioning tiers.

### 2. Configurer les presets et mappings de groupes

1. Ouvrez `Admin > Automatisation`.
2. Créez/éditez un preset Jellyfin (bibliothèques, remote access, bitrate, etc.).
3. Renseignez au besoin `disable_after_days` et `delete_after_days`.
4. Dans `Mapping de groupes`, liez un `group_name` à un `policy_preset_id`.
5. Pour une source LDAP, renseignez aussi le DN du groupe LDAP.

### 3. Créer une invitation alignée sur le groupe

Dans `Admin > Invitations`, renseignez :

- `group_name` (ex: `famille`, `amis`, `kids`)
- `disable_after_days` (désactivation auto)
- `delete_after_days` (suppression auto)

À la validation de l'invitation, JellyGate applique le preset mappé, enregistre les dates d'expiration, et provisionne Jellyseerr/Ombi si activé.

## 🧪 Test E2E (5 minutes)

1. Définissez un mapping `kids -> limited` dans `Admin > Automatisation`.
2. Créez une invitation avec `group_name=kids`, `disable_after_days=2`, `delete_after_days=5`.
3. Inscrivez un compte test via le lien d'invitation.
4. Vérifiez dans `Admin > Utilisateurs` : colonne groupe = `kids`.
5. Vérifiez dans Jellyfin : la policy du compte correspond au preset `limited`.
6. Vérifiez les logs admin : présence de l'action `invite.integration.provisioned` si Jellyseerr/Ombi est configuré.

Résultat attendu : utilisateur créé et mappé correctement, expiration planifiée, provisioning tiers effectué sans bloquer le flux d'inscription.

## 🛠️ Stack technique

| Composant | Technologie |
|---|---|
| Backend | Go 1.22 + Chi v5 |
| Base de données | SQLite via `modernc.org/sqlite` (pure Go, sans CGO) |
| LDAP | `go-ldap/ldap/v3` (LDAPS, unicodePwd) |
| Email | `wneessen/go-mail` (STARTTLS / TLS) |
| Frontend | HTML/CSS/JS vanilla + Tailwind CDN |
| Conteneurisation | Docker multi-stage (~15 Mo), Buildx multi-arch |
| CI/CD | GitHub Actions → GHCR |

## 📂 Structure du projet

```
JellyGate/
├── cmd/jellygate/         # Point d'entrée Go
├── internal/
│   ├── config/            # Chargement des variables d'environnement
│   ├── database/          # SQLite (migrations, CRUD)
│   ├── handlers/          # Handlers HTTP (auth, invitations, admin, reset)
│   ├── jellyfin/          # Client API Jellyfin
│   ├── ldap/              # Client LDAPS (Active Directory)
│   ├── mail/              # Client SMTP (go-mail)
│   ├── middleware/        # Auth, i18n, rate limiting
│   ├── notify/            # Webhooks (Discord, Telegram, Matrix)
│   ├── render/            # Moteur de templates HTML + i18n
│   └── session/           # Gestion des sessions (HMAC-SHA256)
├── web/
│   ├── i18n/              # Traductions (fr.json, en.json)
│   ├── static/            # CSS, JS
│   └── templates/         # Templates HTML (Go html/template)
├── .github/workflows/     # CI/CD
├── Dockerfile             # Multi-stage, multi-arch
├── docker-compose.yml
└── .env.example
```

## 📄 Licence

Ce projet est sous licence MIT. Voir le fichier [LICENSE](LICENSE) pour plus de détails.

---

<p align="center">
  <sub>Construit avec ❤️ pour la communauté Jellyfin</sub>
</p>
