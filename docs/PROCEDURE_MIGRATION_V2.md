# JellyGate v2.0.0 Migration Procedure

This document provides instructions for upgrading existing JellyGate v1.x instances to **JellyGate v2.0.0** with Authentik OIDC and Sponsorship integration.

---

## Migration Overview

1. **Database Schema Upgrade**: Database schema migrations execute automatically upon application startup. New columns (`authentik_id`, `custom_quota`, `bonus_quota`, `malus_quota`, `invited_by_id`, `authentik_invitation_id`) and the `referrals` table are created transparently.
2. **User Account Linking (JIT / Reconciliation)**:
   - When existing users log in via Authentik OIDC for the first time, JellyGate matches their Authentik `sub` (UUID) with existing local accounts by `username` or `email`.
   - The user's `authentik_id` is linked automatically, preserving their history, quotas, and invitation logs.
3. **Authentication Redirection**:
   - Legacy local password login forms are deprecated. Logins are redirected to `/auth/login` (Authentik OIDC PKCE flow).
   - Password reset links are delegated to Authentik's native recovery flow.

---

## Step-by-Step Migration Steps

### Step 1: Backup Existing Database & Configuration

```bash
# SQLite backup
cp /data/jellygate.db /data/jellygate.db.bak_v1

# Copy existing configuration
cp .env .env.v1.bak
```

### Step 2: Configure Authentik OIDC & Service Account

Follow [AUTHENTIK_SETUP.md](AUTHENTIK_SETUP.md) to set up:
- OAuth2/OIDC Provider & Application
- User & Admin Groups (`jellygate-users`, `jellygate-admins`)
- Service Account API Token

### Step 3: Update `.env` File

Add the required Authentik environment variables to your `.env`:

```env
JELLYGATE_SECRET_KEY=your_existing_secret_key
JELLYGATE_BASE_URL=https://jellygate.example.com

AUTHENTIK_URL=https://authentik.example.com
OIDC_ISSUER_URL=https://authentik.example.com/application/o/jellygate/
OIDC_CLIENT_ID=jellygate
OIDC_CLIENT_SECRET=your_oidc_client_secret
OIDC_REDIRECT_URL=https://jellygate.example.com/auth/callback
AUTHENTIK_API_TOKEN=your_authentik_api_token
OIDC_USER_GROUP=jellygate-users
OIDC_ADMIN_GROUP=jellygate-admins
```

### Step 4: Deploy Container Update

```bash
docker compose pull
docker compose up -d --force-recreate
```

### Step 5: Verify Startup Logs

```bash
docker logs jellygate --tail 100
```

Verify that database migrations completed cleanly:
```text
INFO Execution des migrations de la base de donnees... driver=sqlite
INFO Migrations terminees count=25 driver=sqlite
```
