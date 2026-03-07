[![Docker Build](https://github.com/maelmoreau21/Jellygate/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/maelmoreau21/Jellygate/actions/workflows/docker-publish.yml)
[![GHCR Image](https://img.shields.io/badge/GHCR-ghcr.io%2Fmaelmoreau21%2Fjellygate-blue?logo=github)](https://ghcr.io/maelmoreau21/jellygate)

<h1 align="center">JellyGate</h1>

<p align="center">
  <strong>Portail d'invitations, de récupération de mot de passe et de gestion d'utilisateurs pour Jellyfin, avec LDAP/Active Directory natif.</strong>
</p>

## Vue d'ensemble

JellyGate remplace jfa-go avec une approche plus intégrée côté infra self-hosted:

- invitations avec quotas, expiration, profils et automatisation
- création de comptes en mode hybride LDAP + Jellyfin ou LDAP only
- récupération de mot de passe unifiée
- administration web complète
- notifications email et webhooks
- audit logs exploitables et exportables
- i18n pilotée par fichiers JSON avec vérification de cohérence en CI

## Fonctionnalités principales

| Domaine | Détail |
|---|---|
| Invitations | Liens uniques, expiration, quotas, groupe cible, preset Jellyfin, provisioning tiers |
| Comptes | Création atomique LDAP + Jellyfin + SQL, rollback en cas d'échec |
| Utilisateurs | Listing, toggle, suppression, synchronisation Jellyfin, profil utilisateur |
| Reset mot de passe | Demande publique, lien/token, mise à jour Jellyfin + LDAP |
| Sécurité | CSRF sur routes admin mutables, rate limiting, headers HTTP centralisés, cookies signés |
| Audit | Filtres avancés, export CSV/JSON, corrélation par `request_id` |
| i18n | `web/i18n/*.json`, fallback `lang demandée -> en -> fr`, check CI |
| Frontend | HTML, Tailwind build local, JS vanilla, CSS custom |
| Intégrations | SMTP, Discord, Telegram, Matrix, Jellyseerr, Ombi, JellyTulli |
| Base de données | SQLite par défaut, PostgreSQL supporté |

## Langues

Le changement de langue fonctionne selon cette priorité:

1. cookie `lang`
2. header `Accept-Language`
3. paramètre global `default_lang`

Le sélecteur est disponible dans l'interface admin et reste visible sur les pages publiques. Le moteur de rendu applique ensuite les traductions côté serveur sur chaque requête.

Note: plusieurs locales non `fr`/`en` existent déjà, mais certaines chaînes restent encore proches de l'anglais dans les fichiers JSON. Le mécanisme fonctionne, mais la qualité de traduction dépend du contenu de chaque locale.

## Sécurité actuellement en place

- authentification admin déléguée à Jellyfin
- session signée HMAC-SHA256
- token CSRF pour les routes admin d'écriture
- rate limiting mémoire sur `/admin/login`, `/invite/*`, `/reset/request`, `/reset/*`
- headers `Content-Security-Policy`, `Strict-Transport-Security` quand HTTPS, `X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`
- journaux d'audit avec `request_id`

Reste à durcir côté production:

- cookies `Secure` derrière reverse proxy TLS non généralisés partout
- secrets LDAP/SMTP/Webhooks encore stockés en clair dans `settings`
- PostgreSQL TLS à imposer explicitement en prod

## Docker et publication GHCR

Le workflow GitHub Actions publie une image multi-arch pour:

- `linux/amd64`
- `linux/arm64`

Tags publiés:

- `latest` sur la branche par défaut
- `vX.Y.Z` sur les tags Git semver

Il n'y a plus de tags `sha-*`, `vX.Y` ou `vX`.

## Démarrage rapide

### 1. Prérequis

- Docker
- une instance Jellyfin avec clé API admin
- LDAP/Active Directory si tu utilises le mode annuaire
- SMTP si tu veux les emails transactionnels

### 2. Configuration

```bash
git clone https://github.com/maelmoreau21/JellyGate.git
cd JellyGate
cp .env.example .env
```

Variables minimales:

```bash
JELLYGATE_SECRET_KEY=...
JELLYFIN_URL=http://jellyfin:8096
JELLYFIN_API_KEY=...
```

### 3. Lancement

```bash
docker compose up -d
```

Pour PostgreSQL:

```bash
docker compose -f docker-compose.postgres.yml up -d
```

### 4. Connexion admin

1. ouvre `/admin/login`
2. connecte-toi avec un compte Jellyfin administrateur
3. JellyGate crée la session et charge la langue préférée utilisateur si elle existe

## Variables d'environnement utiles

| Variable | Requis | Défaut | Description |
|---|---|---|---|
| `JELLYGATE_SECRET_KEY` | Oui | - | Clé de signature de session |
| `JELLYGATE_PORT` | Non | `8097` | Port HTTP |
| `JELLYGATE_BASE_URL` | Non | `http://localhost:8097` | URL publique |
| `JELLYGATE_DATA_DIR` | Non | `/data` | Répertoire de persistance |
| `JELLYGATE_DEFAULT_LANG` | Non | `fr` | Langue par défaut |
| `JELLYFIN_URL` | Oui | - | URL Jellyfin |
| `JELLYFIN_API_KEY` | Oui | - | Clé API Jellyfin |
| `DB_TYPE` | Non | `sqlite` | `sqlite` ou `postgres` |
| `DB_HOST` | Non | `postgres` | Hôte PostgreSQL |
| `DB_PORT` | Non | `5432` | Port PostgreSQL |
| `DB_USER` | Non | `jellygate` | Utilisateur PostgreSQL |
| `DB_PASSWORD` | Non | - | Mot de passe PostgreSQL |
| `DB_NAME` | Non | `jellygate` | Base PostgreSQL |
| `DB_SSLMODE` | Non | `disable` | Mode SSL PostgreSQL |
| `JELLYSEERR_URL` | Non | - | URL Jellyseerr |
| `JELLYSEERR_API_KEY` | Non | - | Clé API Jellyseerr |
| `OMBI_URL` | Non | - | URL Ombi |
| `OMBI_API_KEY` | Non | - | Clé API Ombi |
| `JELLYTULLI_URL` | Non | - | URL JellyTulli |

LDAP, SMTP, webhooks et templates email se configurent ensuite depuis l'admin et sont stockés en base.

## Structure du projet

```text
cmd/
  i18ncheck/         # Vérification i18n pour CI
  i18ncoverage/      # Couverture de traduction (valeurs identiques à en)
  jellygate/         # Entrée principale
internal/
  backup/
  config/
  database/
  handlers/
  integrations/
  jellyfin/
  ldap/
  mail/
  middleware/
  notify/
  render/
  scheduler/
  session/
web/
  i18n/
  static/
  templates/
.github/workflows/
```

## Vérifications utiles

```bash
npm run build:css
go build ./...
go test ./...
go run ./cmd/i18ncheck
go run ./cmd/i18ncoverage
```

Le workflow `.github/workflows/i18n-quality.yml` exécute `i18ncheck` et `i18ncoverage`.
Le seuil de blocage est piloté par la variable de dépôt `I18N_MAX_SAME_AS_BASE` (mettre `0` quand les locales sont nettoyées).

## Licence

MIT