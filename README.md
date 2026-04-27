<p align="center">
  <img src="logo.svg" width="128" height="128" alt="JellyGate Logo">
</p>

<h1 align="center">JellyGate</h1>

<p align="center">
  <a href="https://github.com/maelmoreau21/Jellygate/actions/workflows/docker-publish.yml"><img src="https://github.com/maelmoreau21/Jellygate/actions/workflows/docker-publish.yml/badge.svg" alt="Docker Build"></a>
  <a href="https://ghcr.io/maelmoreau21/jellygate"><img src="https://img.shields.io/badge/GHCR-ghcr.io%2Fmaelmoreau21%2Fjellygate-blue?logo=github" alt="GHCR Image"></a>
</p>

<p align="center">
  <strong>Portail d'invitations, de récupération de mot de passe et de gestion d'utilisateurs pour Jellyfin, avec LDAP/Active Directory natif.</strong>
</p>

> [!IMPORTANT]
> **Méthode recommandée :** L'installation via **Docker** est la méthode officielle et recommandée pour garantir la stabilité, la sécurité et la facilité de mise à jour.

## Vue d'ensemble

JellyGate remplace jfa-go avec une approche plus intégrée côté infra self-hosted:

- invitations avec quotas, expiration, profils et automatisation
- création de comptes en mode hybride LDAP + Jellyfin ou LDAP only
- récupération de mot de passe unifiée
- administration web complète
- notifications email et webhooks
- audit logs exploitables et exportables
- i18n pilotée par fichiers JSON avec vérification de cohérence en CI

## Démarrage rapide (Docker)

L'utilisation de **Docker Compose** est le moyen le plus simple et recommandé pour déployer JellyGate.

### 1. Préparer l'environnement

```bash
mkdir jellygate && cd jellygate
curl -O https://raw.githubusercontent.com/maelmoreau21/JellyGate/main/docker-compose.yml
curl -O https://raw.githubusercontent.com/maelmoreau21/JellyGate/main/.env.example
cp .env.example .env
```

### 2. Configuration

Éditez le fichier `.env` avec vos paramètres :

```bash
# Obligatoire : Clé secrète pour les sessions
JELLYGATE_SECRET_KEY=générez_une_clé_aléatoire_ici

# Jellyfin
JELLYFIN_URL=http://votre-jellyfin:8096
JELLYFIN_API_KEY=votre_clé_api_admin
```

### 3. Lancement

```bash
docker compose up -d
```

Si vous préférez utiliser **PostgreSQL** au lieu de SQLite :

```bash
docker compose -f docker-compose.postgres.yml up -d
```

### 4. Accès

Rendez-vous sur `http://localhost:8097/admin/login` et connectez-vous avec votre compte Jellyfin administrateur.
JellyGate créera la session et chargera automatiquement votre langue préférée.

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
| Intégrations | SMTP, Discord, Telegram, Matrix, Jellyseerr, JellyTrack |
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


## Variables d'environnement utiles

| Variable | Requis | Défaut | Description |
|---|---|---|---|
| `JELLYGATE_SECRET_KEY` | Oui | - | Clé de signature de session |
| `JELLYGATE_PORT` | Non | `8097` | Port HTTP |
| `JELLYGATE_BASE_URL` | Non | `http://localhost:8097` | URL publique |
| `JELLYGATE_DATA_DIR` | Non | `/data` | Répertoire de persistance |
| `JELLYGATE_DEFAULT_LANG` | Non | `fr` | Langue par défaut |
| `JELLYGATE_TLS_CERT`     | Non | -    | Chemin vers le certificat TLS |
| `JELLYGATE_TLS_KEY`      | Non | -    | Chemin vers la clé privée TLS |
| `JELLYFIN_URL`           | Oui | -    | URL de Jellyfin |
| `JELLYFIN_API_KEY`       | Oui | -    | Clé API Jellyfin |
| `JELLYSEERR_URL`         | Non | -    | URL de Jellyseerr |
| `JELLYSEERR_API_KEY`     | Non | -    | Clé API Jellyseerr |
| `JELLYTRACK_URL`         | Non | -    | URL de JellyTrack |
| `JELLYTRACK_API_KEY`     | Non | -    | Clé API JellyTrack |
| `DB_TYPE` | Non | `sqlite` | `sqlite` ou `postgres` |

LDAP, SMTP, webhooks et templates email se configurent ensuite depuis l'admin et sont stockés en base.

Guide LDAP detaille: [docs/ldap-setup.md](docs/ldap-setup.md)

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
Le seuil de blocage est piloté par la variable de dépôt `I18N_MAX_SAME_AS_BASE`.
Par défaut, le workflow utilise le plafond de non-régression actuel (`195`) et il faut le baisser progressivement jusqu'à `0` à mesure que les locales sont nettoyées.

## Licence

MIT

## Mise à jour

- Ajout de la prise en charge de PostgreSQL dans `docker-compose.postgres.yml`.
- Instructions pour la configuration dans `.env.example`.
- Amélioration des logs d'audit et des intégrations Jellyfin.