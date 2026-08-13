# GUIDE DE CONFIGURATION AUTHENTIK POUR JELLYGATE

## 1. Prérequis dans Authentik

Avant de connecter JellyGate, assurez-vous de disposer sur votre instance Authentik :

1. **Un Provider OIDC OAuth2** :
   - Application Slug : `jellygate`
   - Redirect URIs : `https://jellygate.example.com/auth/callback`
   - Authorization Flow : `default-provider-authorization-implicit-consent` (ou explicite)
   - Signing Key : Sélectionner la clé RSA par défaut d'Authentik.

2. **Trois Groupes d'Accès** :
   - `jellygate-users` : Autorise la connexion utilisateur à JellyGate.
   - `jellygate-admins` : Autorise l'accès administration à JellyGate.
   - `jellyfin-users` : Affecté automatiquement aux inscrits pour autoriser l'accès Jellyfin via l'Outpost LDAP.

3. **Un Token d'API REST** :
   - Créer un Service Account (ex: `jellygate-service`) avec les clés d'accès API.
   - Copier le Token API Bearer généré.

4. **Un Flow d'Enrollment (Inscription)** :
   - Slug recommandé : `default-enrollment-flow`
   - Configurer l'stage d'invitation (`Invitation Stage`) et l'stage d'écriture utilisateur (`User Write Stage`) configuré pour ajouter l'utilisateur au groupe `jellyfin-users`.

## 2. Configuration dans JellyGate

Renseignez les variables d'environnement dans votre fichier `.env` ou directement sur la page d'administration `/admin/authentik` :

```env
AUTHENTIK_ENABLED=true
AUTHENTIK_URL=https://auth.example.com
AUTHENTIK_API_TOKEN=ak_api_token_here

OIDC_ISSUER_URL=https://auth.example.com/application/o/jellygate/
OIDC_CLIENT_ID=jellygate-client-id
OIDC_CLIENT_SECRET=jellygate-client-secret
OIDC_REDIRECT_URL=https://jellygate.example.com/auth/callback

AUTHENTIK_USER_GROUP=jellygate-users
AUTHENTIK_ADMIN_GROUP=jellygate-admins
JELLYFIN_USER_GROUP=jellyfin-users
AUTHENTIK_ENROLLMENT_FLOW_SLUG=default-enrollment-flow
```

## 3. Test de santé

Accédez à `Administration -> Authentik` sur JellyGate et cliquez sur **[Tester la connexion]**.
Vérifiez que les 4 cartes de diagnostic affichent l'état **OK** :
- Connexion API REST
- OIDC Discovery
- Enrollment Flow
- Groupes Requis
