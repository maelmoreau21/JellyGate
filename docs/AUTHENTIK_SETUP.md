# Authentik OIDC & Integration Setup Guide for JellyGate v2.0.0

This guide details the step-by-step procedure for configuring Authentik as the Identity Provider (IdP) for JellyGate v2.0.0.

---

## 1. Create Authentik OAuth2/OIDC Provider

1. Log into your Authentik Admin Interface.
2. Go to **Applications** → **Providers** → **Create Provider**.
3. Select **OAuth2/OpenID Provider**:
   - **Name**: `JellyGate Provider`
   - **Authorization flow**: `default-provider-authorization-implicit-consent` (or standard authorization flow)
   - **Client type**: `Confidential`
   - **Client ID**: `jellygate` (or auto-generated)
   - **Client Secret**: Save this value to `OIDC_CLIENT_SECRET`
   - **Redirect URIs**: `https://jellygate.example.com/auth/callback` (or `http://localhost:8097/auth/callback`)
   - **Signing Key**: Select your RSA certificate (e.g. `authentik Self-signed Certificate`)
4. Click **Finish**.

---

## 2. Create Authentik Application

1. Go to **Applications** → **Applications** → **Create Application**.
   - **Name**: `JellyGate`
   - **Slug**: `jellygate`
   - **Provider**: Select `JellyGate Provider`
2. Click **Create**.

---

## 3. Create Authentik Groups

1. Go to **Directory** → **Groups** → **Create Group**.
2. Create the following groups:
   - `jellygate-admins`: Grants administrator access to JellyGate.
   - `jellygate-users`: Grants standard user access to JellyGate.
3. Ensure users are assigned to at least `jellygate-users` or `jellygate-admins`.

---

## 4. Create Authentik Service Account API Token

For JellyGate to create Stage Tokens (invitations) and Recovery Links:
1. Go to **Directory** → **Users** → **Create Service Account**.
   - **Username**: `ak-jellygate-service`
2. Assign the Service Account to the `authentik Admins` group or grant permissions on `/api/v3/stages/invitation/invitations/` and `/api/v3/core/users/`.
3. Copy the generated API Token and set `AUTHENTIK_API_TOKEN` in your JellyGate `.env`.

---

## 5. Configure JellyGate `.env`

Add the credentials to JellyGate `.env`:

```env
AUTHENTIK_URL=https://authentik.example.com
OIDC_ISSUER_URL=https://authentik.example.com/application/o/jellygate/
OIDC_CLIENT_ID=jellygate
OIDC_CLIENT_SECRET=your_client_secret
OIDC_REDIRECT_URL=https://jellygate.example.com/auth/callback
AUTHENTIK_API_TOKEN=your_service_account_api_token
OIDC_USER_GROUP=jellygate-users
OIDC_ADMIN_GROUP=jellygate-admins
```
