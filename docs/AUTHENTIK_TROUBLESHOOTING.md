# AUTHENTIK TROUBLESHOOTING & DIAGNOSTIC

## Diagnostic Rapide via l'Interface Admin

Rendez-vous sur la page `/admin/authentik` pour lancer le test de diagnostic instantané.

### 1. Erreur Connexion API : HTTP 401 / 403
- **Cause** : Le token API `AUTHENTIK_API_TOKEN` est invalide ou expiré, ou le Service Account ne possède pas les rôles nécessaires.
- **Solution** : Régénérez un token API pour votre Service Account sur Authentik (`Authentik -> System -> Tokens & Service Accounts`) et mettez à jour la configuration.

### 2. Erreur OIDC Discovery : Issuer Inaccessible
- **Cause** : L'URL `OIDC_ISSUER_URL` est incorrecte ou Authentik n'est pas joignable par JellyGate sur le réseau.
- **Solution** : Testez la résolution DNS et la connectivité HTTP depuis le conteneur JellyGate vers `https://auth.example.com/application/o/jellygate/.well-known/openid-configuration`.

### 3. Erreur Flow Enrollment : 404 Introuvable
- **Cause** : Le slug de flow renseigné `AUTHENTIK_ENROLLMENT_FLOW_SLUG` n'existe pas dans Authentik.
- **Solution** : Vérifiez le nom exact du slug dans `Authentik -> Flows & Stages -> Flows`.

### 4. Erreur Groupes Manquants
- **Cause** : L'un des groupes (`jellygate-users`, `jellygate-admins`, `jellyfin-users`) n'a pas été créé dans Authentik.
- **Solution** : Créez les groupes correspondants dans `Authentik -> Directory -> Groups`.
