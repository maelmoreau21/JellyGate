# JellyGate — Project Context (Bible)

> **Dernière mise à jour :** 2026-03-03
> **Version :** 0.1.0-alpha
> **Auteur :** Mael Moreau

---

## 1. Vision du projet

**JellyGate** est un gestionnaire d'invitations, de récupération de mots de passe et d'utilisateurs pour **Jellyfin / Emby**, écrit entièrement en **Go**.
Il remplace [jfa-go](https://github.com/hrfee/jfa-go) en intégrant nativement la création et la gestion des comptes dans un annuaire **Active Directory (LDAP/LDAPS)**.

### Objectifs principaux

| # | Objectif | Détail |
|---|----------|--------|
| 1 | Invitations avancées | Liens uniques, limites d'utilisation, expiration, profil par défaut |
| 2 | Gestion utilisateurs | Dashboard admin : liste, activation/désactivation, ban, suppression (Jellyfin + AD) |
| 3 | Récupération MDP | Réinitialisation via PIN / lien email → MAJ Jellyfin **ET** Active Directory (LDAPS) |
| 4 | Notifications | SMTP complet + webhooks Discord / Telegram / Matrix |
| 5 | Personnalisation UI | Messages d'accueil, CSS custom depuis l'admin |
| 6 | i18n | Français (défaut) + Anglais via fichiers JSON |

---

## 2. Stack technique

```
┌──────────────────────────────────────────────┐
│                   Docker                     │
│  ┌────────────────────────────────────────┐  │
│  │         Go Binary (JellyGate)         │  │
│  │                                        │  │
│  │  ┌──────────┐  ┌──────────────────┐   │  │
│  │  │ Chi v5   │  │ HTML/JS/CSS      │   │  │
│  │  │ Router   │  │ (Vanilla ou      │   │  │
│  │  │          │  │  Vue.js léger)   │   │  │
│  │  └──────────┘  └──────────────────┘   │  │
│  │                                        │  │
│  │  ┌──────────┐  ┌──────────────────┐   │  │
│  │  │ SQLite   │  │ go-ldap/ldap     │   │  │
│  │  │ (data)   │  │ (Active Directory) │   │  │
│  │  └──────────┘  └──────────────────┘   │  │
│  └────────────────────────────────────────┘  │
│                                              │
│  Services externes :                         │
│  • Jellyfin API REST                         │
│  • Active Directory (LDAPS :636)             │
│  • SMTP (email)                              │
│  • Webhooks (Discord, Telegram, Matrix)      │
└──────────────────────────────────────────────┘
```

| Composant | Technologie | Justification |
|-----------|-------------|---------------|
| Langage | Go 1.22+ | Performance, binaire unique, typage fort |
| Routeur HTTP | [go-chi/chi](https://github.com/go-chi/chi) v5 | Léger, idiomatique, middleware-friendly |
| Base de données | SQLite via [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) | Sans serveur, embarquée, **pure Go (sans CGO)** |
| LDAP | [go-ldap/ldap](https://github.com/go-ldap/ldap) v3 | Client LDAP complet, support LDAPS/StartTLS |
| Frontend | HTML / CSS / JS vanilla | Simple, rapide, pas de build JS nécessaire |
| Templating | `html/template` (stdlib Go) | Sécurisé par défaut (auto-escaping) |
| Emails | `net/smtp` + [go-mail/mail](https://github.com/wneessen/go-mail) | SMTP avec TLS/STARTTLS |
| Config | Variables d'environnement + fichier YAML | 12-factor app, Docker-friendly |
| Déploiement | Docker multi-stage | Image finale légère (~10-15 Mo, sans CGO) |
| i18n | Fichiers JSON (`locales/fr.json`, `locales/en.json`) | Simple, extensible |

---

## 3. Architecture des dossiers

```
JellyGate/
├── cmd/
│   └── jellygate/
│       └── main.go              # Point d'entrée
├── internal/
│   ├── config/
│   │   └── config.go            # Chargement configuration (env + YAML)
│   ├── database/
│   │   ├── database.go          # Connexion SQLite, migrations
│   │   └── models.go            # Structures de données (User, Invitation, etc.)
│   ├── handlers/
│   │   ├── admin.go             # Endpoints admin (dashboard, gestion users)
│   │   ├── auth.go              # Login, sessions, middleware auth
│   │   ├── invitations.go       # CRUD invitations + validation publique
│   │   ├── password_reset.go    # Récupération mot de passe
│   │   └── webhooks.go          # Gestion webhooks sortants
│   ├── jellyfin/
│   │   └── client.go            # Client API REST Jellyfin
│   ├── ldap/
│   │   └── client.go            # Client LDAP/LDAPS (Active Directory)
│   ├── mail/
│   │   └── mailer.go            # Service d'envoi d'emails
│   ├── i18n/
│   │   └── i18n.go              # Chargement et résolution des traductions
│   └── middleware/
│       ├── auth.go              # Middleware d'authentification
│       ├── ratelimit.go         # Rate limiting
│       └── logging.go           # Logging des requêtes
├── web/
│   ├── static/
│   │   ├── css/
│   │   │   └── style.css        # Styles principaux
│   │   └── js/
│   │       └── app.js           # JavaScript principal
│   ├── templates/
│   │   ├── layouts/
│   │   │   └── base.html        # Layout de base
│   │   ├── admin/
│   │   │   ├── dashboard.html   # Dashboard admin
│   │   │   ├── users.html       # Gestion utilisateurs
│   │   │   ├── invitations.html # Gestion invitations
│   │   │   └── settings.html    # Paramètres
│   │   ├── public/
│   │   │   ├── invite.html      # Page d'inscription (invitation)
│   │   │   └── reset.html       # Page de réinitialisation MDP
│   │   └── emails/
│   │       ├── invitation.html  # Template email invitation
│   │       └── reset.html       # Template email reset MDP
│   └── locales/
│       ├── fr.json              # Traductions françaises (défaut)
│       └── en.json              # Traductions anglaises
├── data/                        # Volume Docker : SQLite + config runtime
│   └── jellygate.db
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod
├── go.sum
├── .env.example
├── project_context.md           # ← CE FICHIER (bible du projet)
└── README.md
```

---

## 4. Modèle de données (SQLite)

### Table `users`
| Colonne | Type | Description |
|---------|------|-------------|
| `id` | INTEGER PK | Auto-increment |
| `jellyfin_id` | TEXT UNIQUE | ID Jellyfin de l'utilisateur |
| `username` | TEXT UNIQUE NOT NULL | Nom d'utilisateur |
| `email` | TEXT | Adresse email |
| `ldap_dn` | TEXT | Distinguished Name dans l'AD |
| `invited_by` | TEXT | Code invitation utilisé |
| `is_active` | BOOLEAN | Compte actif/désactivé |
| `is_banned` | BOOLEAN | Compte banni |
| `access_expires_at` | DATETIME | Date d'expiration de l'accès |
| `created_at` | DATETIME | Date de création |
| `updated_at` | DATETIME | Dernière modification |

### Table `invitations`
| Colonne | Type | Description |
|---------|------|-------------|
| `id` | INTEGER PK | Auto-increment |
| `code` | TEXT UNIQUE NOT NULL | Code unique de l'invitation |
| `label` | TEXT | Libellé de l'invitation (admin) |
| `max_uses` | INTEGER | Nombre max d'utilisations (0 = illimité) |
| `used_count` | INTEGER DEFAULT 0 | Nombre d'utilisations actuelles |
| `jellyfin_profile` | TEXT | Profil Jellyfin JSON (droits, bibliothèques) |
| `expires_at` | DATETIME | Date d'expiration |
| `created_by` | TEXT | Admin qui a créé l'invitation |
| `created_at` | DATETIME | Date de création |

### Table `password_resets`
| Colonne | Type | Description |
|---------|------|-------------|
| `id` | INTEGER PK | Auto-increment |
| `user_id` | INTEGER FK | Référence vers `users.id` |
| `code` | TEXT UNIQUE NOT NULL | Code PIN / token unique |
| `expires_at` | DATETIME NOT NULL | Expiration du code |
| `used` | BOOLEAN DEFAULT FALSE | Déjà utilisé ? |
| `created_at` | DATETIME | Date de création |

### ~Table `admin_users`~ → Supprimée

> **Authentification admin déléguée à Jellyfin** : le login admin appelle
> `POST /Users/AuthenticateByName` sur Jellyfin. Seuls les utilisateurs avec
> `Policy.IsAdministrator == true` sont autorisés. La session est maintenue
> via un cookie signé (HMAC-SHA256) côté JellyGate.

### Table `settings`
| Colonne | Type | Description |
|---------|------|-------------|
| `key` | TEXT PK | Clé du paramètre |
| `value` | TEXT | Valeur (JSON ou texte brut) |
| `updated_at` | DATETIME | Dernière modification |

### Table `audit_log`
| Colonne | Type | Description |
|---------|------|-------------|
| `id` | INTEGER PK | Auto-increment |
| `action` | TEXT NOT NULL | Type d'action (ex: `user.created`, `invite.used`) |
| `actor` | TEXT | Qui a effectué l'action |
| `target` | TEXT | Sur qui/quoi porte l'action |
| `details` | TEXT | Détails JSON |
| `created_at` | DATETIME | Horodatage |

---

## 5. Routes API

### Routes publiques (pas d'authentification)

| Méthode | Route | Description |
|---------|-------|-------------|
| `GET` | `/invite/{code}` | Page d'inscription via invitation |
| `POST` | `/invite/{code}` | Soumettre le formulaire d'inscription |
| `GET` | `/reset` | Page de demande de réinitialisation MDP |
| `POST` | `/reset/request` | Envoyer un code de réinitialisation |
| `GET` | `/reset/{code}` | Page de saisie du nouveau MDP |
| `POST` | `/reset/{code}` | Soumettre le nouveau MDP |

### Routes admin (authentification requise)

| Méthode | Route | Description |
|---------|-------|-------------|
| `GET` | `/admin/login` | Page de login admin |
| `POST` | `/admin/login` | Authentification admin |
| `POST` | `/admin/logout` | Déconnexion admin |
| `GET` | `/admin/` | Dashboard principal |
| `GET` | `/admin/users` | Liste des utilisateurs |
| `POST` | `/admin/users/{id}/toggle` | Activer/désactiver un utilisateur |
| `POST` | `/admin/users/{id}/ban` | Bannir un utilisateur |
| `DELETE` | `/admin/users/{id}` | Supprimer un utilisateur (Jellyfin + AD) |
| `POST` | `/admin/users/{id}/extend` | Prolonger l'accès d'un utilisateur |
| `GET` | `/admin/invitations` | Liste des invitations |
| `POST` | `/admin/invitations` | Créer une nouvelle invitation |
| `DELETE` | `/admin/invitations/{id}` | Supprimer une invitation |
| `GET` | `/admin/settings` | Page des paramètres |
| `POST` | `/admin/settings` | Sauvegarder les paramètres |
| `GET` | `/admin/logs` | Journal d'audit |

### Routes statiques

| Méthode | Route | Description |
|---------|-------|-------------|
| `GET` | `/static/*` | Fichiers statiques (CSS, JS, images) |

---

## 6. Variables d'environnement

### Application

| Variable | Défaut | Description |
|----------|--------|-------------|
| `JELLYGATE_PORT` | `8097` | Port d'écoute HTTP |
| `JELLYGATE_BASE_URL` | `http://localhost:8097` | URL de base publique |
| `JELLYGATE_DATA_DIR` | `/data` | Répertoire des données (SQLite, config) |
| `JELLYGATE_SECRET_KEY` | *(requis)* | Clé secrète pour les sessions/tokens |

### Jellyfin

| Variable | Défaut | Description |
|----------|--------|-------------|
| `JELLYFIN_URL` | *(requis)* | URL de l'instance Jellyfin |
| `JELLYFIN_API_KEY` | *(requis)* | Clé API d'administration Jellyfin |

### LDAP / Active Directory

| Variable | Défaut | Description |
|----------|--------|-------------|
| `LDAP_HOST` | *(requis)* | Hostname du serveur LDAP (Active Directory) |
| `LDAP_PORT` | `636` | Port LDAP (636 pour LDAPS) |
| `LDAP_USE_TLS` | `true` | Utiliser LDAPS (TLS) |
| `LDAP_SKIP_VERIFY` | `false` | Ignorer la vérification du certificat TLS |
| `LDAP_BIND_DN` | *(requis)* | DN de l'utilisateur pour se connecter (bind) |
| `LDAP_BIND_PASSWORD` | *(requis)* | Mot de passe de bind |
| `LDAP_BASE_DN` | *(requis)* | Base DN de recherche (ex: `dc=home,dc=lan`) |
| `LDAP_USER_OU` | `CN=Users` | OU pour la création des utilisateurs |
| `LDAP_USER_GROUP` | *(optionnel)* | Groupe AD auquel ajouter les utilisateurs |
| `LDAP_DOMAIN` | *(requis)* | Domaine AD (ex: `home.lan`) — pour `userPrincipalName` |

### SMTP

| Variable | Défaut | Description |
|----------|--------|-------------|
| `SMTP_HOST` | *(requis)* | Serveur SMTP |
| `SMTP_PORT` | `587` | Port SMTP |
| `SMTP_USERNAME` | *(requis)* | Utilisateur SMTP |
| `SMTP_PASSWORD` | *(requis)* | Mot de passe SMTP |
| `SMTP_FROM` | *(requis)* | Adresse expéditeur |
| `SMTP_TLS` | `true` | Utiliser STARTTLS |

### Webhooks (optionnels)

| Variable | Défaut | Description |
|----------|--------|-------------|
| `WEBHOOK_DISCORD_URL` | *(vide)* | URL du webhook Discord |
| `WEBHOOK_TELEGRAM_TOKEN` | *(vide)* | Token du bot Telegram |
| `WEBHOOK_TELEGRAM_CHAT_ID` | *(vide)* | ID du chat Telegram |
| `WEBHOOK_MATRIX_URL` | *(vide)* | URL du serveur Matrix |
| `WEBHOOK_MATRIX_ROOM_ID` | *(vide)* | ID de la room Matrix |
| `WEBHOOK_MATRIX_TOKEN` | *(vide)* | Token d'accès Matrix |

---

## 7. Flux critique : Validation d'une invitation

Ce flux est le cœur de JellyGate. Les opérations doivent être **atomiques** — en cas d'échec d'une étape, les précédentes sont annulées (rollback).

```
Utilisateur soumet le formulaire d'invitation
        │
        ▼
┌─────────────────────────────┐
│ 1. Valider l'invitation     │  → Code valide ? Non expiré ? Quota non atteint ?
│    (base SQLite)            │
└────────────┬────────────────┘
             │ OK
             ▼
┌─────────────────────────────┐
│ 2. Créer le compte dans     │  → sAMAccountName, userPrincipalName, displayName
│    Active Directory (LDAPS:636)│  → Mot de passe encodé UTF-16LE
└────────────┬────────────────┘
             │ OK
             ▼
┌─────────────────────────────┐
│ 3. Créer le compte dans     │  → POST /Users/New (API REST Jellyfin)
│    Jellyfin                 │  → Appliquer le profil d'invitation
└────────────┬────────────────┘
             │ OK
             ▼
┌─────────────────────────────┐
│ 4. Enregistrer l'utilisateur│  → INSERT dans la table `users`
│    dans SQLite              │  → INCREMENT `used_count` sur l'invitation
└────────────┬────────────────┘
             │ OK
             ▼
┌─────────────────────────────┐
│ 5. Notifications            │  → Email de bienvenue à l'utilisateur
│                             │  → Webhook Discord/Telegram/Matrix à l'admin
└─────────────────────────────┘

⚠️  Rollback en cas d'erreur :
    - Étape 3 échoue → Supprimer le compte AD créé à l'étape 2
    - Étape 4 échoue → Supprimer le compte Jellyfin (étape 3) + AD (étape 2)
```

---

## 8. Sécurité

| Mesure | Détail |
|--------|--------|
| Authentification admin | Déléguée à **Jellyfin** (`AuthenticateByName` + vérification `IsAdministrator`) |
| Sessions | Cookie HTTP-Only, Secure, SameSite=Strict |
| CSRF | Token CSRF sur tous les formulaires |
| Rate limiting | Limite sur les routes publiques (login, reset, invite) |
| LDAPS | Connexion obligatoire via TLS (port 636) |
| Mot de passe AD | Encodé en **UTF-16LE** encapsulé dans des guillemets, transmis via LDAPS |
| Injection SQL | Utilisation systématique de requêtes préparées |
| XSS | Auto-escaping via `html/template` |
| Clé secrète | `JELLYGATE_SECRET_KEY` requis, min 32 caractères |
| Audit | Toutes les actions sensibles sont loguées dans `audit_log` |

---

## 9. État d'avancement

| Phase | Statut | Détail |
|-------|--------|--------|
| ✅ Rédaction `project_context.md` | **Terminé** | Ce fichier |
| ✅ `docker-compose.yml` | **Terminé** | Port 8097, toutes les variables |
| ✅ `Dockerfile` | **Terminé** | Multi-stage, pure Go (sans CGO) |
| ✅ Squelette Go (cmd/internal) | **Terminé** | main.go + routeur Chi v5 |
| ✅ Configuration (config.go) | **Refactorisé** | 4 env vars (App+JF), LDAP/SMTP/Webhooks en SQLite via UI admin |
| ✅ Base de données (SQLite) | **Terminé** | modernc.org/sqlite + 6 tables + index |
| ✅ Authentification admin | **Terminé** | Déléguée à Jellyfin + cookie HMAC |
| ✅ Client Jellyfin | **Terminé** | CRUD users, policy, profils, reset MDP |
| ✅ Client LDAP/LDAPS | **Terminé** | LDAPS, unicodePwd UTF-16LE, UAC, rollback-ready |
| ✅ Système d'invitations | **Terminé** | Flux atomique 5 étapes + rollback strict |
| ✅ Gestion utilisateurs | **Terminé** | API JSON : list, toggle AD+JF, delete |
| ✅ Récupération MDP | **Terminé** | Token crypto, dual reset AD+JF, anti-énumération |
| ✅ Service SMTP | **Terminé** | go-mail, templates HTML, Ping au démarrage |
| ✅ Webhooks | **Terminé** | Discord, Telegram, Matrix — async goroutines |
| ✅ Frontend (templates) | **Terminé** | Dark theme, glassmorphism, Tailwind, fetch API, i18n-ready |
| ✅ Internationalisation | **Terminé** | fr.json + en.json, middleware détection, fallback FR, sélecteur UI |
| ⬜ Personnalisation UI | À faire | CSS custom depuis l'admin |
| ⬜ Tests | À faire | Unitaires + intégration |
| ✅ Docker / CI | **Terminé** | Buildx multi-arch, GHCR, semver tags, GHA cache |
