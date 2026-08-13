<p align="center">
  <img src="logo.svg" width="128" height="128" alt="JellyGate Logo">
</p>

<h1 align="center">JellyGate v2.0.0</h1>

<p align="center">
  <a href="https://github.com/maelmoreau21/Jellygate/actions/workflows/docker-publish.yml"><img src="https://github.com/maelmoreau21/Jellygate/actions/workflows/docker-publish.yml/badge.svg" alt="Docker Build"></a>
  <a href="https://ghcr.io/maelmoreau21/jellygate"><img src="https://img.shields.io/badge/GHCR-ghcr.io%2Fmaelmoreau21%2Fjellygate-blue?logo=github" alt="GHCR Image"></a>
</p>

<p align="center">
  <strong>Modern admin and onboarding portal for Jellyfin with Authentik OIDC Single Sign-On and Sponsorship/Referrals system.</strong>
</p>

---

## Overview

JellyGate v2.0.0 is the administrative and user onboarding platform for Jellyfin. It delegates identity, Single Sign-On (SSO), and authentication to **Authentik OIDC**, while maintaining full business logic for **user invitations**, **sponsorship trees (parrainage)**, **quota management**, **automations**, and **audit logging**.

## Key Features

- **Authentik OIDC SSO**: Authorization Code Flow with **PKCE (S256)**, JWKS token validation, JIT account syncing, and group-based access control (`jellygate-admins`, `jellygate-users`).
- **Sponsorship & Referral Engine (Parrainage)**:
  - Multi-level referral tree tracking godchildren to sponsors via immutable `authentik_id`.
  - Quota calculation formula: $\text{TotalQuota} = \text{GlobalDefault} + \text{CustomOverride} + \text{Bonus} - \text{Malus}$.
  - Real-time conversion tracking and active link statuses (`pending`, `accepted`, `active`, `expired`, `cancelled`, `revoked`).
- **Authentik Stage Invitation Integration**: Automatically creates Authentik Stage Tokens when sponsors generate invitation links.
- **Jellyfin Integration**: Automatic policy profiles, user creation, library permissions, and group mappings.
- **Third-Party Integrations**: Provisioning for Jellyseerr, Ombi, and JellyTrack.
- **Automations & Scheduled Tasks**: Automated cleanup of expired links, backups, and user access expirations.
- **Multi-language Support (i18n)**: Fully internationalized UI (English, French, German, Spanish, Italian, Dutch, Polish, Portuguese, Russian, Chinese).
- **Storage**: Native support for **SQLite** and **PostgreSQL**.

---

## Installation & Deployment

### 1. Prepare Environment

```bash
mkdir jellygate && cd jellygate
curl -O https://raw.githubusercontent.com/maelmoreau21/JellyGate/main/docker-compose.yml
curl -O https://raw.githubusercontent.com/maelmoreau21/JellyGate/main/.env.example
cp .env.example .env
```

### 2. Configure Environment Variables (`.env`)

```env
# Application Secret
JELLYGATE_SECRET=your_random_32_character_secret_key
JELLYGATE_BASE_URL=http://localhost:8097

# Authentik OIDC Settings
AUTHENTIK_URL=https://authentik.example.com
OIDC_ISSUER_URL=https://authentik.example.com/application/o/jellygate/
OIDC_CLIENT_ID=jellygate_client_id
OIDC_CLIENT_SECRET=jellygate_client_secret
OIDC_REDIRECT_URL=http://localhost:8097/auth/callback
AUTHENTIK_API_TOKEN=ak_api_token_here

# Jellyfin Connection
JELLYFIN_URL=http://jellyfin:8096
JELLYFIN_API_KEY=your_jellyfin_api_key
```

### 3. Start Container

```bash
docker compose up -d
```

> [!TIP]
> For PostgreSQL deployment, use `docker compose -f docker-compose.postgres.yml up -d`.

---

## Documentation

- [Authentik Setup Guide](docs/AUTHENTIK_SETUP.md)
- [Migration Procedure (v1.x to v2.0.0)](docs/PROCEDURE_MIGRATION_V2.md)
- [Backup & Rollback Procedures](docs/PROCEDURE_BACKUP_ROLLBACK.md)
- [Authentik Audit Report](docs/RAPPORT_AUDIT_AUTHENTIK.md)
- [Target Architecture Specification](docs/ARCHITECTURE_CIBLE_AUTHENTIK.md)

---

## License

Released under the [MIT License](LICENSE).