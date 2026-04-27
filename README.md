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

---

## 🚀 Installation (Méthode Recommandée : Docker)

L'utilisation de **Docker Compose** est le moyen le plus simple et recommandé pour déployer JellyGate. Cela garantit la stabilité, la sécurité et la facilité de mise à jour.

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

> [!TIP]
> Si vous préférez utiliser **PostgreSQL** au lieu de SQLite, utilisez :
> `docker compose -f docker-compose.postgres.yml up -d`

### 4. Accès

Rendez-vous sur `http://localhost:8097/admin/login` et connectez-vous avec votre compte Jellyfin administrateur.

---

## 🌟 Fonctionnalités principales

| Domaine | Détail |
|---|---|
| **Invitations** | Liens uniques, expiration, quotas, groupe cible, preset Jellyfin, provisioning tiers |
| **Comptes** | Création atomique LDAP + Jellyfin + SQL, rollback en cas d'échec |
| **Utilisateurs** | Listing, toggle, suppression, synchronisation Jellyfin, profil utilisateur |
| **Reset mot de passe** | Demande publique, lien/token, mise à jour Jellyfin + LDAP |
| **Sécurité** | CSRF, rate limiting, headers HTTP centralisés, cookies signés |
| **Audit** | Filtres avancés, export CSV/JSON, corrélation par `request_id` |
| **i18n** | Système multilingue complet (`fr`, `en`, etc.) |
| **Intégrations** | SMTP, Discord, Telegram, Matrix, Jellyseerr, JellyTrack |

---

## ⚙️ Variables d'environnement

| Variable | Requis | Défaut | Description |
|---|---|---|---|
| `JELLYGATE_SECRET_KEY` | Oui | - | Clé de signature de session |
| `JELLYGATE_PORT` | Non | `8097` | Port HTTP |
| `JELLYGATE_BASE_URL` | Non | `http://localhost:8097` | URL publique |
| `JELLYFIN_URL` | Oui | - | URL de Jellyfin |
| `JELLYFIN_API_KEY` | Oui | - | Clé API Jellyfin |
| `DB_TYPE` | Non | `sqlite` | `sqlite` ou `postgres` |

> [!NOTE]
> LDAP, SMTP, webhooks et templates email se configurent ensuite directement depuis l'interface d'administration.

---

## 🌍 Langues

Le changement de langue est automatique selon la priorité : cookie `lang` > header `Accept-Language` > paramètre `default_lang`. Un sélecteur est également disponible dans l'interface.

---

## 🛠️ Développement & Vérifications

```bash
npm run build:css     # Compiler Tailwind
go build ./...        # Vérifier la compilation
go test ./...         # Lancer les tests
go run ./cmd/i18ncheck # Vérifier les traductions
```

---

## 📄 Licence

Distribué sous licence **MIT**.